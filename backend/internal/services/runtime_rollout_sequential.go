package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
)

var errRuntimePoolOccupied = errors.New("in-place Runtime replacement requires an empty pool")

type rolloutCheckpointStore interface {
	LoadRolloutCheckpoint(context.Context, int64) (string, error)
	SaveRolloutCheckpoint(context.Context, int64, string) error
}

type RuntimePoolProgress struct {
	Version             int                        `json:"version"`
	Targets             []k8s.RuntimeRolloutTarget `json:"targets"`
	Completed           int                        `json:"completed"`
	Applying            bool                       `json:"applying"`
	Submitted           bool                       `json:"submitted"`
	StepStarted         time.Time                  `json:"step_started"`
	VerificationStarted time.Time                  `json:"verification_started,omitempty"`
}

func LoadRuntimePoolProgress(ctx context.Context, repo any, id int64) (*RuntimePoolProgress, error) {
	store, ok := repo.(rolloutCheckpointStore)
	if !ok {
		return nil, nil
	}
	raw, err := store.LoadRolloutCheckpoint(ctx, id)
	if err != nil || raw == "" {
		return nil, err
	}
	var p RuntimePoolProgress
	var kind struct {
		Kind string `json:"kind"`
	}
	_ = json.Unmarshal([]byte(raw), &kind)
	if kind.Kind == "opencode-native-preserve" {
		return nil, nil
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, err
	}
	if p.Version == 2 && kind.Kind == "openclaw-pool-replacement" {
		return nil, nil
	}
	if p.Version != 1 || len(p.Targets) == 0 || p.Completed < 0 || p.Completed > len(p.Targets) {
		return nil, fmt.Errorf("invalid runtime rollout checkpoint")
	}
	return &p, nil
}

func (s *RuntimeScheduler) savePoolProgress(ctx context.Context, id int64, p *RuntimePoolProgress) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if len(raw) > 1<<20 {
		return fmt.Errorf("runtime rollout inventory too large")
	}
	return s.rolloutRepo.(rolloutCheckpointStore).SaveRolloutCheckpoint(ctx, id, string(raw))
}

// OpenClaw retains its data-safe preflight/migration/rollback state machine.
// Only the elected scheduler calls this path; HTTP handlers merely enqueue.
func (s *RuntimeScheduler) startPoolRollout(ctx context.Context, r *models.RuntimeRollout) (bool, error) {
	if r.RuntimeType == RuntimeTypeOpenClaw {
		return false, nil
	}
	inventory, ok := s.deployments.(k8s.RuntimeRolloutInventory)
	if !ok {
		return false, nil
	}
	if _, ok := s.rolloutRepo.(rolloutCheckpointStore); !ok {
		return true, fmt.Errorf("runtime rollout checkpoint storage unavailable")
	}
	active, err := s.rolloutRepo.ListActive(ctx, r.RuntimeType)
	if err != nil {
		return true, err
	}
	for _, other := range active {
		if other.ID != r.ID && (other.Status == "running" || other.ID < r.ID) {
			return true, nil
		}
	}
	p, err := LoadRuntimePoolProgress(ctx, s.rolloutRepo, r.ID)
	if err != nil {
		return true, err
	}
	if p == nil {
		targets, err := inventory.RolloutTargets(ctx, s.runtimeNamespace, r.RuntimeType)
		if err != nil {
			return true, err
		}
		if len(targets) == 0 {
			return true, fmt.Errorf("no eligible live runtime deployment")
		}
		for _, source := range targets {
			if source.Image == r.TargetImageRef {
				continue
			}
			// A known occupied source is an immediate rejection. A failed read
			// must be retried under the checkpointed apply deadline and guard.
			if err := s.requireEmptyPoolRolloutSource(ctx, *r, source, false); errors.Is(err, errRuntimePoolOccupied) {
				now, message := time.Now().UTC(), err.Error()
				return true, s.rolloutRepo.UpdateStatus(ctx, r.ID, "error", r.StartedAt, &now, &message)
			}
		}
		p = &RuntimePoolProgress{Version: 1, Targets: targets}
		if err := s.savePoolProgress(ctx, r.ID, p); err != nil {
			return true, err
		}
	}
	now := time.Now().UTC()
	return true, s.rolloutRepo.UpdateStatus(ctx, r.ID, "running", &now, nil, nil)
}

func (s *RuntimeScheduler) advancePoolRollout(ctx context.Context, r models.RuntimeRollout, p *RuntimePoolProgress) error {
	fail := func(err error) error {
		message, now := err.Error(), time.Now().UTC()
		if saveErr := s.rolloutRepo.UpdateStatus(ctx, r.ID, "error", r.StartedAt, &now, &message); saveErr != nil {
			return saveErr
		}
		return err
	}
	if p.Completed == len(p.Targets) {
		return s.finishLiveRuntimeRollout(ctx, r, p)
	}
	live, err := s.runtimeRolloutTargets(ctx, r, p.Targets)
	if err != nil {
		return err
	} // transient inspection failure never means an empty pool
	source := p.Targets[p.Completed]
	var target *k8s.RuntimeRolloutTarget
	for i := range live {
		if live[i].Namespace == source.Namespace && live[i].Name == source.Name {
			target = &live[i]
			break
		}
	}
	if target == nil || target.UID != source.UID {
		return fail(fmt.Errorf("rollout target %s/%s disappeared or was replaced", source.Namespace, source.Name))
	}
	if target.Image != source.Image && target.Image != r.TargetImageRef {
		return fail(fmt.Errorf("rollout target %s image changed outside this operation", source.Name))
	}
	if !p.Applying {
		p.Applying, p.StepStarted = true, time.Now().UTC()
		return s.savePoolProgress(ctx, r.ID, p)
	}
	if time.Since(p.StepStarted) > 10*time.Minute {
		return fail(fmt.Errorf("rollout target %s readiness timed out; remaining targets unchanged", source.Name))
	}
	if !p.Submitted {
		if s.leader != nil && !s.leader.IsLeader(ctx) {
			return fmt.Errorf("runtime rollout leadership lost before mutation")
		}
		hermesWeb := r.RuntimeType == RuntimeTypeHermes && r.Phase == k8s.HermesWebRolloutPhase
		guard := func(ctx context.Context) error {
			if s.leader != nil && !s.leader.IsLeader(ctx) {
				return fmt.Errorf("runtime rollout leadership lost before mutation")
			}
			return s.requireEmptyPoolRolloutSource(ctx, r, source, true)
		}
		if guarded, ok := s.deployments.(k8s.GuardedRuntimePoolRollout); ok {
			err = guarded.RolloutGuardedPoolImage(ctx, source, r.RuntimeType, r.TargetImageRef, hermesWeb, guard)
		} else {
			// Legacy deployment adapters cannot compare templates. Treat any
			// write as a possible replacement for the protected Runtime types.
			if err = guard(ctx); err == nil {
				if hermesWeb {
					web, ok := s.deployments.(k8s.HermesWebDeploymentService)
					if !ok {
						return fail(fmt.Errorf("Hermes Web deployment configuration unavailable"))
					}
					err = web.RolloutHermesWebImage(ctx, source.Namespace, source.Name, r.TargetImageRef, 0, 1)
				} else {
					err = s.deployments.RolloutImage(ctx, source.Namespace, source.Name, r.TargetImageRef, "", 0, 1)
				}
			}
		}
		if errors.Is(err, errRuntimePoolOccupied) {
			return fail(err)
		}
		if err != nil {
			return err
		}
		p.Submitted = true
		return s.savePoolProgress(ctx, r.ID, p)
	}
	if target.Image != r.TargetImageRef || !target.Ready {
		return nil
	}
	// Verify registered Runtime capabilities/bindings through the existing
	// service after every pool is ready, before marking the operation complete.
	p.Completed++
	p.Applying, p.Submitted = false, false
	p.StepStarted = time.Time{}
	if p.Completed == len(p.Targets) {
		p.VerificationStarted = time.Now().UTC()
	}
	return s.savePoolProgress(ctx, r.ID, p)
}

func (s *RuntimeScheduler) requireEmptyPoolRolloutSource(ctx context.Context, r models.RuntimeRollout, source k8s.RuntimeRolloutTarget, verifyPhysical bool) error {
	if r.RuntimeType != RuntimeTypeHermes && r.RuntimeType != RuntimeTypeDeepSeekHarness {
		return nil
	}
	if s.bindingRepo == nil || s.podRepo == nil {
		return fmt.Errorf("Runtime source binding inventory unavailable")
	}
	pods, err := s.podRepo.List(ctx, r.RuntimeType)
	if err != nil {
		return err
	}
	for _, pod := range pods {
		if pod.Namespace != source.Namespace || pod.DeploymentName != source.Name || pod.ID <= 0 {
			continue
		}
		bindings, err := s.bindingRepo.ListByRuntimePodID(ctx, pod.ID)
		if err != nil {
			return err
		}
		if len(bindings) > 0 || pod.UsedSlots > 0 {
			return fmt.Errorf("Runtime pool %s has bindings or slot claims: %w", source.Name, errRuntimePoolOccupied)
		}
	}
	live, err := s.deployments.ListDeploymentPods(ctx, []k8s.RuntimeDeploymentRef{{Namespace: source.Namespace, Name: source.Name}})
	if err != nil {
		return err
	}
	if len(live) == 0 {
		return fmt.Errorf("Runtime pool %s has no verifiable source Pods", source.Name)
	}
	for _, livePod := range live {
		var agent *models.RuntimePod
		for i := range pods {
			pod := &pods[i]
			if livePod.Namespace == source.Namespace && livePod.DeploymentName == source.Name &&
				runtimeRolloutAgentMatchesPod(*pod, livePod) && pod.ID > 0 && pod.PodUID != nil && *pod.PodUID != "" {
				if agent != nil {
					return fmt.Errorf("Runtime pool %s source Pod %s registration is ambiguous", source.Name, livePod.PodName)
				}
				agent = pod
			}
		}
		if agent == nil {
			return fmt.Errorf("Runtime pool %s source Pod %s has no verified registration", source.Name, livePod.PodName)
		}
		if !verifyPhysical || len(livePod.Slots) == 0 {
			continue
		}
		client, ok := s.agentClient.(*slotRuntimeAgentClient)
		if !ok {
			return fmt.Errorf("Runtime pool %s has no slot proof client", source.Name)
		}
		slots, err := client.observeRuntimeSlots(ctx, *agent)
		if err != nil {
			return err
		}
		for _, slot := range slots {
			if slot.Err != nil {
				return fmt.Errorf("Runtime pool %s slot %s: %w", source.Name, slot.Name, slot.Err)
			}
			if !slot.free() {
				return fmt.Errorf("Runtime pool %s slot %s is still occupied: %w", source.Name, slot.Name, errRuntimePoolOccupied)
			}
		}
		ports, err := client.runtimeAuditReservedPorts(ctx, *agent)
		if err != nil {
			return err
		}
		if len(ports) > 0 {
			return fmt.Errorf("Runtime pool %s has unpublished or retained assignments: %w", source.Name, errRuntimePoolOccupied)
		}
	}
	return nil
}
