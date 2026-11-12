package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"clawreef/internal/backupgate"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	k8sservice "clawreef/internal/services/k8s"
	"k8s.io/client-go/kubernetes"
)

type InstanceAutoStopService struct {
	teamPower    *teamLifecycleCoordinator
	initialized  atomic.Bool
	store        *repository.InstanceAutoStopRepository
	lifecycle    *instanceService
	kube         kubernetes.Interface
	appNamespace string
	appPodName   string
}

func (s *InstanceAutoStopService) SetApplicationPodIdentity(namespace, podName string) {
	s.appNamespace, s.appPodName = strings.TrimSpace(namespace), strings.TrimSpace(podName)
}

func NewInstanceAutoStopService(store *repository.InstanceAutoStopRepository, instances InstanceService, kube kubernetes.Interface) *InstanceAutoStopService {
	lifecycle, _ := instances.(*instanceService)
	if lifecycle != nil {
		lifecycle.autoStopRepo = store
	}
	return &InstanceAutoStopService{store: store, lifecycle: lifecycle, kube: kube}
}
func (s *InstanceAutoStopService) Run(ctx context.Context) {
	if models.AutoStopTestTiming {
		log.Print("automatic Lite stop: TEST BUILD; legacy defaults: scan interval=1m, idle timeout=5m; explicitly configured seconds take precedence")
	}
	// Capacity deadlines and expired model leases are control-plane recovery, independent of the
	// administrator's idle scan and potentially slow native stops.
	deadlineCtx, deadlineCancel := context.WithCancel(ctx)
	defer deadlineCancel()
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-deadlineCtx.Done():
				return
			case <-ticker.C:
				release, err := backupgate.HoldWorkUnit(deadlineCtx, "instance.start-deadline")
				if err != nil {
					continue
				}
				if err := s.store.ReconcileModelActivities(deadlineCtx, nil); err != nil {
					log.Printf("model activity lease recovery: %v", err)
				}
				_ = s.expireStarts(deadlineCtx)
				release()
			}
		}
	}()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		if err := s.Tick(ctx); err != nil && ctx.Err() == nil {
			log.Printf("automatic Lite stop: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-s.teamKick():
		}
	}
}
func (s *InstanceAutoStopService) teamKick() <-chan struct{} {
	if s.teamPower != nil {
		return s.teamPower.kick
	}
	return nil
}
func (s *InstanceAutoStopService) Tick(ctx context.Context) error {
	release, err := backupgate.HoldWorkUnit(ctx, "instance.auto-stop")
	if err != nil {
		return err
	}
	defer release()
	if s.lifecycle == nil {
		return fmt.Errorf("instance lifecycle unavailable")
	}
	initializedNow := false
	if !s.initialized.Load() {
		if err = s.store.Initialize(ctx); err != nil {
			return err
		}
		s.initialized.Store(true)
		initializedNow = true
	}
	// Expired leases must not wait for the administrator's daily idle scan,
	// or for a potentially slow Team/native stop below. Legacy scopes still
	// require the independent application-exit proof collected by the idle scan.
	if err = s.store.ReconcileModelActivities(ctx, nil); err != nil {
		return err
	}
	if s.teamPower != nil {
		if err := s.teamPower.reconcile(ctx); err != nil {
			return err
		}
	}
	pending, err := s.store.PendingStops(ctx)
	if err != nil {
		return err
	}
	if err = s.expireStarts(ctx); err != nil {
		return err
	}
	jobs := make(chan models.InstanceIdleStopOperation)
	var workers sync.WaitGroup
	for n := 0; n < min(3, len(pending)); n++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for op := range jobs {
				s.resume(ctx, op)
			}
		}()
	}
	for _, op := range pending {
		if ctx.Err() != nil {
			break
		}
		jobs <- op
	}
	close(jobs)
	workers.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	cfg, err := s.store.ClaimCheck(ctx, time.Now().UTC())
	if err != nil || cfg == nil {
		return err
	}
	// Establish missing observation windows at startup and on due scans, rather
	// than traversing every Lite instance during each one-second deadline poll.
	if !initializedNow {
		if err = s.store.Initialize(ctx); err != nil {
			return err
		}
	}
	var legacyCutoff *time.Time
	var legacyProofErr error
	legacy, err := s.store.HasLegacyModelActivities(ctx)
	if err != nil {
		return err
	}
	if legacy {
		proofCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		legacyCutoff, legacyProofErr = modelActivityLegacyCutoff(proofCtx, s.kube, s.appNamespace, s.appPodName)
		cancel()
	}
	if err = s.store.ReconcileModelActivities(ctx, legacyCutoff); err != nil {
		return err
	}
	if s.teamPower != nil {
		if err := s.teamPower.scan(ctx, *cfg); err != nil {
			return err
		}
	}
	after := cfg.ScanCursor
	for {
		candidates, err := s.store.CandidatesForTimeout(ctx, time.Now().UTC(), cfg.IdleTimeout(), after, 100)
		if err != nil {
			return err
		}
		if len(candidates) == 0 {
			return s.store.CompleteScan(ctx, *cfg)
		}
		for _, a := range candidates {
			if err = s.store.CheckpointScan(ctx, *cfg, after); err != nil {
				return err
			}
			after = a.InstanceID
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err = s.tryStop(ctx, *cfg, a); err != nil {
				if errors.Is(err, models.ErrLegacyModelActivityUnverified) && legacyProofErr != nil {
					err = fmt.Errorf("%w: %v", err, legacyProofErr)
				}
				_ = s.store.RecordBlocker(ctx, a.InstanceID, err.Error())
			}
			if err = s.store.CheckpointScan(ctx, *cfg, after); err != nil {
				return err
			}
		}
	}
}
func (s *InstanceAutoStopService) tryStop(ctx context.Context, cfg models.InstanceAutoStopSettings, a models.InstanceModelActivity) error {
	release, err := acquireInstanceLifecycleOperation(ctx, s.lifecycle.instanceRepo, a.InstanceID)
	if err != nil {
		return err
	}
	defer release()
	return s.withInstanceMutation(ctx, a.InstanceID, func() error { return s.tryStopGuarded(ctx, cfg, a) })
}

// The lifecycle lock and workspace/upgrade mutation fence both span live
// preflight, the durable stop claim, Runtime cleanup, and binding release.
func (s *InstanceAutoStopService) tryStopGuarded(ctx context.Context, cfg models.InstanceAutoStopSettings, a models.InstanceModelActivity) error {
	if s.teamPower != nil {
		team, err := s.teamPower.store.InstanceTeam(ctx, a.InstanceID)
		if err != nil {
			return err
		}
		if team != 0 {
			allowed, _ := ctx.Value(teamIdleStopContextKey{}).(int)
			if allowed != team {
				return fmt.Errorf("Team members only stop through a Team operation")
			}
			if err = s.teamPower.store.ValidateStopMember(ctx, team, a.InstanceID, a.Generation); err != nil {
				return err
			}
		}
	}
	i, err := s.lifecycle.instanceRepo.GetByID(a.InstanceID)
	if err != nil {
		return err
	}
	if i == nil {
		return models.ErrAutoStopConflict
	}
	if i.InstanceMode != "lite" || i.Status != "running" || i.RuntimeGeneration != a.Generation {
		return models.ErrAutoStopConflict
	}
	runtimeType, ok := v2RuntimeTypeForInstance(i)
	if !ok {
		return fmt.Errorf("this Lite runtime is unsupported")
	}
	if err = s.lifecycle.verifyOriginalWorkspace(ctx, runtimeType, i); err != nil {
		return err
	}
	b, err := s.lifecycle.bindingRepo.GetByInstanceID(ctx, i.ID)
	if err != nil {
		return err
	}
	if b == nil || b.Generation != i.RuntimeGeneration {
		return fmt.Errorf("current runtime binding unavailable")
	}
	pod, err := s.lifecycle.runtimePodRepo.GetByID(ctx, b.RuntimePodID)
	if err != nil {
		return err
	}
	if pod == nil || pod.PodUID == nil || pod.AgentEndpoint == nil {
		return fmt.Errorf("runtime Pod identity unavailable")
	}
	storage, err := k8sservice.ResolveRuntimePersistentStorage(ctx, s.kube, pod.Namespace, pod.PodName, *pod.PodUID, b.WorkspacePath)
	if err != nil {
		return err
	}
	if i.WorkspacePath == nil || b.WorkspacePath != *i.WorkspacePath {
		return fmt.Errorf("workspace binding identity changed")
	}
	client, ok := s.lifecycle.agentClient.(RuntimeIdleStopClient)
	if !ok {
		return fmt.Errorf("runtime does not support graceful idle stop")
	}
	// Check protocol availability and the exact source before committing a stop.
	// Native execution state is deliberately not an eligibility condition: all
	// Lite types share the durable Gateway model-activity window.
	req := RuntimeIdleStopRequest{InstanceID: i.ID, Generation: i.RuntimeGeneration, PodUID: *pod.PodUID}
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	proof, err := client.IdlePreflight(probeCtx, *pod.AgentEndpoint, b.GatewayID, req)
	cancel()
	if err != nil {
		return err
	}
	if proof == nil {
		return fmt.Errorf("runtime stop protocol response is unavailable")
	}
	if proof.DefaultsPolicyStatus == "" {
		proof.DefaultsPolicyStatus = "unknown"
	}
	if err = s.store.RecordDefaultsPolicy(ctx, repository.InstanceAutoStopPolicyStatus{InstanceID: i.ID, Generation: i.RuntimeGeneration, PodUID: *pod.PodUID, DefaultsPolicyVersion: proof.DefaultsPolicyVersion, DefaultsPolicyStatus: proof.DefaultsPolicyStatus}); err != nil {
		return err
	}
	op, err := s.store.ClaimStop(ctx, cfg.Version, a.ActivityVersion, models.InstanceIdleStopOperation{InstanceID: i.ID, UserID: i.UserID, Generation: i.RuntimeGeneration, RuntimePodID: b.RuntimePodID, GatewayID: b.GatewayID, PodUID: *pod.PodUID, Endpoint: *pod.AgentEndpoint, WorkspacePath: b.WorkspacePath, StorageIdentity: storage})
	if err != nil {
		return err
	}
	return s.complete(ctx, *op)
}
func verifyOriginalIdleWorkspace(root, runtimeType string, i *models.Instance) error {
	expected := filepath.Clean(RuntimeWorkspacePathWithRoot(root, runtimeType, i.UserID, i.ID))
	if i.WorkspacePath == nil || filepath.Clean(strings.TrimSpace(*i.WorkspacePath)) != expected {
		return fmt.Errorf("original workspace identity cannot be verified")
	}
	actual, err := filepath.EvalSymlinks(expected)
	if err != nil {
		return fmt.Errorf("original workspace is unavailable: %w", err)
	}
	resolvedRoot, rootErr := filepath.EvalSymlinks(root)
	if rootErr != nil {
		return rootErr
	}
	canonical := filepath.Clean(RuntimeWorkspacePathWithRoot(resolvedRoot, runtimeType, i.UserID, i.ID))
	if actual != canonical {
		return fmt.Errorf("workspace identity resolves outside its original directory")
	}
	resolvedHome, homeErr := filepath.EvalSymlinks(filepath.Join(expected, "home"))
	if homeErr != nil || resolvedHome != filepath.Join(canonical, "home") {
		return fmt.Errorf("original workspace home identity is unavailable")
	}
	for _, p := range []string{expected, filepath.Join(expected, "home")} {
		info, err := os.Stat(p)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("original workspace directory is unavailable")
		}
	}
	return nil
}
func (s *InstanceAutoStopService) resume(ctx context.Context, op models.InstanceIdleStopOperation) {
	if s.store != nil {
		touchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		touchErr := s.store.TouchStopRetry(touchCtx, op.ID)
		cancel()
		if touchErr != nil {
			return
		}
	}
	release, err := acquireInstanceLifecycleOperation(ctx, s.lifecycle.instanceRepo, op.InstanceID)
	if err != nil {
		return
	}
	defer release()
	if err = s.withInstanceMutation(ctx, op.InstanceID, func() error { s.resumeGuarded(ctx, op); return nil }); err != nil && s.store != nil {
		op.ErrorMessage = err.Error()
		failureCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = s.store.FailStop(failureCtx, op)
	}
}

func (s *InstanceAutoStopService) withInstanceMutation(ctx context.Context, id int, run func() error) error {
	if guard, ok := s.lifecycle.runtimeUpgradeGuard.(interface {
		WithInstanceMutation(context.Context, int, func() error) error
	}); ok {
		return guard.WithInstanceMutation(ctx, id, run)
	}
	return run()
}

func (s *InstanceAutoStopService) resumeGuarded(ctx context.Context, op models.InstanceIdleStopOperation) {
	current, readErr := s.store.GetStopOperation(ctx, op.ID)
	if readErr != nil || current == nil || current.Phase == "completed" {
		return
	}
	op = *current
	if op.Kind == "restart" && op.Phase == "restart_ready" {
		_ = s.lifecycle.resumeRestartFollowup(ctx, op.InstanceID)
		return
	}
	a, err := s.store.GetActivity(ctx, op.InstanceID)
	if err != nil || a == nil || a.StopOperationID != op.ID {
		return
	}
	// The pending snapshot may predate another controller's successful receipt.
	op.Phase = a.StopPhase
	if op.Phase != "teardown_confirmed" {
		if completed, recoveryErr := s.store.CompleteStopFromRecovery(ctx, op); recoveryErr != nil {
			op.ErrorMessage = recoveryErr.Error()
			_ = s.store.FailStop(ctx, op)
			return
		} else if completed {
			return
		}
	}
	if op.Kind == "manual" || op.Kind == "restart" {
		if err = s.lifecycle.resumeManualStop(ctx, op); err != nil {
			op.ErrorMessage = err.Error()
			_ = s.store.FailStop(ctx, op)
		} else if op.Kind == "restart" {
			_ = s.lifecycle.resumeRestartFollowup(ctx, op.InstanceID)
		}
		return
	}
	_ = s.complete(ctx, op)
}
func (s *InstanceAutoStopService) complete(ctx context.Context, op models.InstanceIdleStopOperation) (err error) {
	defer func() {
		if err != nil {
			op.ErrorMessage = err.Error()
			failureCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			_ = s.store.FailStop(failureCtx, op)
		}
	}()
	client, ok := s.lifecycle.agentClient.(RuntimeIdleStopClient)
	if !ok {
		return fmt.Errorf("runtime graceful idle stop unavailable")
	}
	b, err := s.lifecycle.bindingRepo.GetByInstanceID(ctx, op.InstanceID)
	if err != nil {
		return err
	}
	if b != nil && (b.RuntimePodID != op.RuntimePodID || b.Generation != op.Generation || b.GatewayID != op.GatewayID || b.WorkspacePath != op.WorkspacePath) {
		return fmt.Errorf("idle-stop source binding changed")
	}
	req := RuntimeIdleStopRequest{InstanceID: op.InstanceID, Generation: op.Generation, PodUID: op.PodUID}
	if op.Phase != "teardown_confirmed" {
		if b == nil {
			return fmt.Errorf("binding disappeared before persisted graceful teardown proof")
		}
		// Reverify the immutable source on retries; a replaced Pod cannot stand in
		// for the writer named in the operation.
		pod, err := s.lifecycle.runtimePodRepo.GetByID(ctx, op.RuntimePodID)
		if err != nil {
			return err
		}
		if pod == nil || pod.PodUID == nil || *pod.PodUID != op.PodUID {
			return fmt.Errorf("idle-stop source Pod changed")
		}
		storage, err := k8sservice.ResolveRuntimePersistentStorage(ctx, s.kube, pod.Namespace, pod.PodName, op.PodUID, op.WorkspacePath)
		if err != nil {
			return err
		}
		if storage != op.StorageIdentity {
			return fmt.Errorf("workspace backing storage changed")
		}
		stopCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		err = client.IdleStop(stopCtx, op.Endpoint, op.GatewayID, req)
		cancel()
		if err != nil {
			return err
		}
		if err = s.store.ConfirmTeardown(ctx, op); err != nil {
			return err
		}
	}
	if b != nil {
		if err = client.ConfirmIdleStop(ctx, op.Endpoint, op.GatewayID, req); err != nil {
			return err
		}
		if err = s.lifecycle.bindingRepo.DeleteByInstanceIDAndReleaseSlot(ctx, op.InstanceID, op.RuntimePodID); err != nil {
			return err
		}
	}
	if err = s.store.CompleteStop(ctx, op); err != nil {
		return err
	}
	i, readErr := s.lifecycle.instanceRepo.GetByID(op.InstanceID)
	if readErr == nil && i != nil {
		GetHub().BroadcastInstanceStatus(i.UserID, i)
	}
	return nil
}
func (s *InstanceAutoStopService) expireStarts(ctx context.Context) error {
	ids, err := s.store.ExpiredStarts(ctx, time.Now().UTC())
	if err != nil {
		return err
	}
	for _, id := range ids {
		release, err := acquireInstanceLifecycleOperation(ctx, s.lifecycle.instanceRepo, id)
		if err != nil {
			continue
		}
		func() {
			defer release()
			activity, e := s.store.GetActivity(ctx, id)
			if e != nil || activity == nil || activity.StartDeadlineAt == nil {
				return
			}
			changed, e := s.store.FenceExpiredStart(ctx, id)
			if e != nil || !changed {
				return
			}
			i, e := s.lifecycle.instanceRepo.GetByID(id)
			if e != nil || i == nil {
				return
			}
			// An explicit Start already authorizes cancellation of its unpublished
			// reservation. Keep the deadline/fence if teardown cannot be confirmed.
			if s.lifecycle.bindingRepo == nil {
				return
			}
			b, e := s.lifecycle.bindingRepo.GetByInstanceID(ctx, id)
			if e != nil {
				return
			}
			if verifier, ok := s.lifecycle.agentClient.(interface {
				VerifyFailedStartCleanup(context.Context, *models.Instance) error
			}); b == nil && ok {
				// Current-generation stop proof is sufficient to withdraw Start;
				// unrelated historical delete protection must remain untouched.
				e = verifier.VerifyFailedStartCleanup(ctx, i)
				if errors.Is(e, k8sservice.ErrCurrentStartPublicationUnavailable) {
					e = s.lifecycle.cleanupV2GatewayBinding(ctx, i)
				}
			} else {
				e = s.lifecycle.cleanupV2GatewayBinding(ctx, i)
			}
			if e != nil {
				_ = s.store.RecordBlocker(ctx, id, e.Error())
				return
			}
			if e = s.store.ClearExpiredStartDeadline(ctx, id, i.RuntimeGeneration, *activity.StartDeadlineAt); e != nil {
				return
			}
			GetHub().BroadcastInstanceStatus(i.UserID, i)
		}()
	}
	return nil
}
