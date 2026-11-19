package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/upper/db/v4"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// allocatableGatewaySlots uses the same physical reservations and SQL ownership
// as admission. A missing observation never supplies free capacity.
func (s *RuntimeScheduler) allocatableGatewaySlots(ctx context.Context, pod models.RuntimePod) (int, error) {
	return s.inspectAllocatableGatewaySlots(ctx, pod, false)
}

func (s *RuntimeScheduler) inspectAllocatableGatewaySlots(ctx context.Context, pod models.RuntimePod, readOnly bool) (int, error) {
	free := maxInt(0, pod.Capacity-pod.UsedSlots)
	inventory, ok := s.agentClient.(interface {
		reservedGatewayPorts(context.Context, models.RuntimePod) (map[int]struct{}, error)
	})
	if !ok || !requiresConfirmedGatewayStop(s.agentClient) {
		return free, nil
	}
	var reserved map[int]struct{}
	var err error
	if client, ok := s.agentClient.(*slotRuntimeAgentClient); ok {
		reserved, err = client.inspectReservedGatewayPorts(ctx, pod, true, readOnly)
	} else {
		reserved, err = inventory.reservedGatewayPorts(ctx, pod)
	}
	if err != nil {
		return 0, err
	}
	fenced, err := s.slotResetReservedPorts(ctx, pod)
	if err != nil {
		return 0, err
	}
	bindings, err := s.bindingRepo.ListByRuntimePodID(ctx, pod.ID)
	if err != nil {
		return 0, err
	}
	var physical map[int]struct{}
	if layout, ok := s.agentClient.(interface {
		physicalGatewayPorts(context.Context, models.RuntimePod) (map[int]struct{}, error)
	}); ok {
		physical, err = layout.physicalGatewayPorts(ctx, pod)
		if err != nil {
			return 0, err
		}
		if physical == nil {
			return 0, fmt.Errorf("physical slot layout is unavailable")
		}
	}
	block := RuntimeGatewayPortBlockSize(pod.RuntimeType)
	if block < 1 {
		return 0, fmt.Errorf("invalid Runtime port block")
	}
	available, configured := 0, 0
	for port := s.gatewayPortStart; port+block-1 <= s.gatewayPortEndForPod(pod, pod.RuntimeType); port += block {
		configured++
		if physical != nil {
			if _, ok := physical[port]; !ok {
				return 0, fmt.Errorf("allocator port range differs from physical layout")
			}
		}
		_, blocked := reserved[port]
		_, resetting := fenced[port]
		if !blocked && !resetting && !runtimeGatewayPortBlockOccupied(bindings, port, block) {
			available++
		}
	}
	if physical != nil && len(physical) != configured {
		return 0, fmt.Errorf("allocator capacity differs from complete physical layout")
	}
	return minInt(free, available), nil
}

const maxConsecutiveExpansionFailures = 3

var errRuntimeSlotObservation = errors.New("runtime slot observation unavailable")

func runtimeCapacityDeficit(waiting, verifiedFree, verifiedStarting int) int {
	unmet := maxInt(0, maxInt(0, waiting)-maxInt(0, verifiedFree))
	return maxInt(0, unmet-maxInt(0, verifiedStarting))
}

type runtimeScaleNativeProbe interface {
	observeRuntimeSlots(context.Context, models.RuntimePod) ([]runtimeSlotAuditObservation, error)
}

func (s *RuntimeScheduler) verifyScaleSourceProtocol(ctx context.Context, client runtimeScaleNativeProbe, pod models.RuntimePod) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	observations, err := client.observeRuntimeSlots(ctx, pod)
	if err != nil {
		return err
	}
	if len(observations) != pod.Capacity || len(observations) == 0 {
		return fmt.Errorf("native worker protocol proof does not cover the physical slots")
	}
	block := RuntimeGatewayPortBlockSize(pod.RuntimeType)
	for index, observation := range observations {
		if observation.Err != nil {
			return fmt.Errorf("serving slot %s cannot establish Runtime protocol approval: %w", observation.Name, observation.Err)
		}
		if block < 1 || observation.Port != s.gatewayPortStart+index*block || observation.Port+block-1 > s.gatewayPortEndForPod(pod, pod.RuntimeType) {
			return fmt.Errorf("allocator capacity or range differs from serving slot layout")
		}
	}
	return nil
}

type runtimeObservedCapacity struct {
	verifiedFree     int
	occupied         int
	observationError error
	verifiedPods     map[string]struct{}
}

func (s *RuntimeScheduler) observeExpansionCapacity(ctx context.Context, target k8s.RuntimePodScaleTarget, startup k8s.RuntimeExpansionObservation) (runtimeObservedCapacity, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	pool := runtimeObservedCapacity{verifiedPods: map[string]struct{}{}}
	reports, err := s.podRepo.List(ctx, target.RuntimeType)
	if err != nil {
		return pool, err
	}
	inventory, err := s.RuntimeDeploymentPods(ctx, target.RuntimeType)
	if err != nil {
		return pool, err
	}
	current := map[string]models.RuntimePod{}
	for _, pod := range s.currentOrdinaryRuntimePods(reports, inventory) {
		current[runtimePodIdentity(pod)] = pod
	}
	// Determine whether the last attempt delivered capacity before spending
	// the probe window on older Pods.
	sort.SliceStable(inventory, func(i, j int) bool {
		_, left := startup.NewPodUIDs[stringValue(inventory[i].PodUID)]
		_, right := startup.NewPodUIDs[stringValue(inventory[j].PodUID)]
		return left && !right
	})
	for _, live := range inventory {
		if live.Namespace != target.Namespace || live.DeploymentName != target.DeploymentName {
			continue
		}
		pod, usable := current[runtimePodIdentity(live)]
		uid := stringValue(live.PodUID)
		_, newPod := startup.NewPodUIDs[uid]
		free := 0
		if usable && pod.Capacity > pod.UsedSlots && ctx.Err() == nil {
			free, err = s.allocatableGatewaySlots(ctx, pod)
			if err != nil {
				pool.observationError = err
			}
		}
		if newPod && usable && (free > 0 || s.expansionPodServing(ctx, pod, startup.StartedAt)) {
			pool.verifiedPods[uid] = struct{}{}
		}
		pool.verifiedFree += free
		pool.occupied += maxInt(live.Capacity-free, 0)
	}
	return pool, nil
}

// A newly added Pod may have been filled between two observations. Only a
// current worker and matching SQL binding can prove it actually served demand.
func (s *RuntimeScheduler) expansionPodServing(ctx context.Context, pod models.RuntimePod, startedAt time.Time) bool {
	client, ok := s.agentClient.(runtimeScaleNativeProbe)
	if !ok || startedAt.IsZero() || ctx.Err() != nil || s.bindingRepo == nil {
		return false
	}
	bindings, err := s.bindingRepo.ListByRuntimePodID(ctx, pod.ID)
	if err != nil {
		return false
	}
	serving := map[int]models.InstanceRuntimeBinding{}
	for _, b := range bindings {
		if b.State == "running" && !b.CreatedAt.Before(startedAt.Truncate(time.Second)) && b.RuntimeType == pod.RuntimeType {
			serving[b.GatewayPort] = b
		}
	}
	if len(serving) == 0 {
		return false
	}
	fenced, err := s.slotResetReservedPorts(ctx, pod)
	if err != nil {
		return false
	}
	observations, err := client.observeRuntimeSlots(ctx, pod)
	if err != nil {
		return false
	}
	for _, slot := range observations {
		binding, exists := serving[slot.Port]
		_, resetting := fenced[slot.Port]
		if !exists || resetting || slot.Err != nil || !(pod.KubernetesReady || pod.ReadyGatewayPorts[slot.Port]) {
			continue
		}
		report := slot.Snapshot
		owner := report.Assignment
		if report.Quarantined == nil || *report.Quarantined || report.MaintenanceOperationID != "" || owner == nil ||
			owner.InstanceID != binding.InstanceID || owner.Generation != binding.Generation || owner.GatewayID != binding.GatewayID {
			continue
		}
		for _, gateway := range report.Gateways {
			if gateway.GatewayID == binding.GatewayID && gateway.State == "running" {
				return true
			}
		}
	}
	return false
}

func (s *RuntimeScheduler) hasApprovedExpansion() bool {
	_, ok := s.deployments.(k8s.RuntimeScaleApprovalDeploymentService)
	return ok && s.management != nil && s.management.sess != nil
}

const maxRuntimeScaleSourcesPerPass = 2

func (s *RuntimeScheduler) nextScaleSourceCandidates(target k8s.RuntimePodScaleTarget, pods []models.RuntimePod) []models.RuntimePod {
	if len(pods) <= maxRuntimeScaleSourcesPerPass {
		return pods
	}
	key := target.Namespace + "/" + target.DeploymentName
	s.scaleSourceCursorMu.Lock()
	defer s.scaleSourceCursorMu.Unlock()
	if s.scaleSourceCursor == nil {
		s.scaleSourceCursor = map[string]string{}
	}
	last := s.scaleSourceCursor[key]
	start := sort.Search(len(pods), func(i int) bool { return pods[i].PodName > last })
	candidates := make([]models.RuntimePod, 0, maxRuntimeScaleSourcesPerPass)
	for offset := range maxRuntimeScaleSourcesPerPass {
		candidates = append(candidates, pods[(start+offset)%len(pods)])
	}
	s.scaleSourceCursor[key] = candidates[len(candidates)-1].PodName
	return candidates
}

// A source inspection can wait on an unavailable worker. Keep it outside
// instance scheduling and give every Runtime type its own finite pass.
func (s *RuntimeScheduler) refreshRuntimeScaleApprovals(ctx context.Context, now time.Time) {
	if !s.hasApprovedExpansion() || now.Sub(s.lastScaleApprovalRefreshAt) < time.Minute {
		return
	}
	if _, native := s.agentClient.(runtimeScaleNativeProbe); !native {
		return
	}
	s.lastScaleApprovalRefreshAt = now
	maintenance := &RuntimePodMaintenanceService{sess: s.management.sess, scheduler: s}
	kube := s.deployments.(k8s.RuntimePodScaleDeploymentService)
	for _, runtimeType := range []string{RuntimeTypeOpenClaw, RuntimeTypeHermes, RuntimeTypeOpenCode, RuntimeTypeDeepSeekHarness} {
		go func() {
			if _, _, err := maintenance.defaultScaleSnapshot(ctx, maintenance.sess, kube, runtimeType, scaleAllowApproval); err != nil && !apierrors.IsNotFound(err) {
				log.Printf("runtime scheduler configuration approval runtime_type=%s: %v", runtimeType, err)
			}
		}()
	}
}

type runtimeExpansionBatchKey struct{}

// Concurrent creating instances in the same batch share one expansion
// observation per type. The separate backlog pass can examine aggregate demand.
func (s *RuntimeScheduler) scaleOutApprovedForDemand(ctx context.Context, runtimeType string, demand int) (bool, error) {
	if batch, ok := ctx.Value(runtimeExpansionBatchKey{}).(*sync.Map); ok && demand == 1 {
		observe := sync.OnceValues(func() (bool, error) {
			return s.reconcileApprovedExpansion(ctx, runtimeType, demand)
		})
		shared, _ := batch.LoadOrStore(runtimeType, observe)
		return shared.(func() (bool, error))()
	}
	return s.reconcileApprovedExpansion(ctx, runtimeType, demand)
}

func (s *RuntimeScheduler) reconcileApprovedExpansion(ctx context.Context, runtimeType string, demand int) (bool, error) {
	maintenance := &RuntimePodMaintenanceService{sess: s.management.sess, scheduler: s}
	kube := s.deployments.(k8s.RuntimePodScaleDeploymentService)
	approvalKube := s.deployments.(k8s.RuntimeScaleApprovalDeploymentService)
	// Live worker observations run before serialized admission. An approval
	// already recorded survives a failed observation of current capacity.
	previewTarget, preview, err := maintenance.defaultScaleSnapshot(ctx, maintenance.sess, kube, runtimeType, scaleAllowApproval)
	if err != nil {
		return false, err
	}
	previewStartup, err := inspectRuntimeExpansion(ctx, maintenance.sess, approvalKube, previewTarget, preview)
	if err != nil {
		if errors.Is(err, k8s.ErrRuntimeScaleOutPending) {
			return false, fmt.Errorf("%w: %v", errRuntimeControlPlanePending, err)
		}
		return false, err
	}
	capacity, observeErr := s.observeExpansionCapacity(ctx, previewTarget, previewStartup)
	if observeErr != nil {
		log.Printf("runtime scheduler scale-out observation runtime_type=%s: %v", runtimeType, observeErr)
	}
	var target k8s.RuntimePodScaleTarget
	var waiting bool
	var floor int32
	var unconfirmedWrite error
	err = maintenance.createAdmission(ctx, runtimeType, func(tx db.Session) error {
		latestDemand, err := repository.CountRuntimeSlotDemand(ctx, tx, runtimeType)
		if err != nil {
			return err
		}
		demand = min(demand, latestDemand)
		if runtimeCapacityDeficit(demand, capacity.verifiedFree, 0) == 0 {
			return nil
		}
		var snapshot *k8s.RuntimePodScaleSnapshot
		target, snapshot, err = maintenance.defaultScaleSnapshot(ctx, tx, kube, runtimeType, scaleRequireApproval)
		if err != nil {
			return err
		}
		if snapshot.ApprovalDigest != preview.ApprovalDigest || snapshot.ExpansionAttemptID() != preview.ExpansionAttemptID() || snapshot.Option.Replicas != preview.Option.Replicas {
			return k8s.ErrRuntimeScaleOutPending
		}
		startup, err := inspectRuntimeExpansion(ctx, tx, approvalKube, target, snapshot)
		if err != nil {
			return err
		}
		// Registered ready Pods have already contributed their proven capacity.
		// Other Pods in the new attempt contribute startup capacity only until
		// registration, expiry, or a terminal observation.
		registered, err := repository.NewRuntimePodRepository(tx).List(ctx, runtimeType)
		if err != nil {
			return err
		}
		for _, pod := range registered {
			if stringValue(pod.PodUID) != "" {
				delete(startup.StartingPodUIDs, *pod.PodUID)
			}
		}
		if startup.ResourceBlockReason != "" {
			return fmt.Errorf("%w: cluster cannot schedule more %s/%s Pods: %s", errRuntimeControlPlanePending, target.Namespace, target.DeploymentName, startup.ResourceBlockReason)
		}
		if runtimeCapacityDeficit(demand, capacity.verifiedFree, len(startup.StartingPodUIDs)*snapshot.Option.SlotCount) == 0 || startup.AwaitingPods || len(startup.StartingPodUIDs) > 0 {
			waiting = true
			return nil
		}
		failures := 0
		if startup.Attempted {
			success := false
			for uid := range startup.NewPodUIDs {
				if _, verified := capacity.verifiedPods[uid]; verified {
					success = true
					break
				}
			}
			if !success {
				failures = startup.FailedAttempts + 1
				if failures >= maxConsecutiveExpansionFailures {
					return fmt.Errorf("%w: %s/%s has %d consecutive Runtime expansion attempts without verified new capacity", errRuntimeControlPlanePending, target.Namespace, target.DeploymentName, failures)
				}
				if time.Now().UTC().Before(startup.RetryAfter) {
					waiting = true
					return nil
				}
			}
		}
		if snapshot.Option.Replicas == math.MaxInt32 {
			return fmt.Errorf("Runtime expansion replica count overflow for %s/%s", target.Namespace, target.DeploymentName)
		}
		floor = snapshot.Option.Replicas + 1
		waiting = true
		if err := prepareRuntimeExpansion(ctx, tx, snapshot, startup); err != nil {
			return err
		}
		err = approvalKube.ScaleOutApproved(ctx, target, snapshot, floor, failures)
		if errors.Is(err, k8s.ErrRuntimeScaleWriteUnconfirmed) {
			unconfirmedWrite = err
			return nil
		}
		return err
	})
	err = errors.Join(err, unconfirmedWrite)
	if errors.Is(err, k8s.ErrRuntimeScaleOutPending) {
		return false, fmt.Errorf("%w: %v", errRuntimeControlPlanePending, err)
	}
	if err != nil {
		return false, err
	}
	if floor > 0 && s.events != nil {
		event := map[string]any{
			"runtime_type": runtimeType, "namespace": target.Namespace, "deployment": target.DeploymentName,
			"replicas": floor, "reason": "pending_backlog", "backlog": demand,
			"verified_free": capacity.verifiedFree, "unavailable_slots": capacity.occupied,
		}
		if capacity.observationError != nil {
			event["observation_error"] = capacity.observationError.Error()
		}
		if err := s.events.Publish(ctx, "runtime.pool.scaleout", event); err != nil {
			log.Printf("runtime scheduler publish scale-out event failed: %v", err)
		}
	}
	return waiting, nil
}
