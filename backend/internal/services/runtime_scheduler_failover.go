package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

const runtimeFailoverMaxAttempts = 5

func (s *RuntimeScheduler) beginOwnedRuntimeFailover(ctx context.Context, store repository.RuntimeFailoverRepository, instance models.Instance, binding models.InstanceRuntimeBinding, pod models.RuntimePod, reason string) error {
	if strings.TrimSpace(stringValue(pod.PodUID)) == "" || strings.TrimSpace(stringValue(pod.AgentEndpoint)) == "" {
		return fmt.Errorf("failover requires the exact source runtime identity")
	}
	want := models.RuntimeFailoverIntent{InstanceID: instance.ID, UserID: instance.UserID, Generation: binding.Generation, RuntimeType: binding.RuntimeType, SourcePodID: pod.ID, SourcePodUID: *pod.PodUID, SourceEndpoint: *pod.AgentEndpoint, GatewayID: binding.GatewayID, GatewayPort: binding.GatewayPort, WorkspacePath: binding.WorkspacePath, Reason: reason}
	prior, err := store.GetRuntimeFailover(ctx, instance.ID)
	if err != nil {
		return err
	}
	if prior != nil && prior.Generation < instance.RuntimeGeneration {
		retired, retireErr := s.retireSupersededRuntimeFailoverLocked(ctx, store, *prior, instance, &binding)
		if retireErr != nil {
			return retireErr
		}
		if !retired {
			return fmt.Errorf("runtime failover predecessor was not retired")
		}
	}
	state, err := store.BeginRuntimeFailover(ctx, want)
	if err != nil {
		return err
	}
	if state == nil {
		return fmt.Errorf("runtime failover intent was not persisted")
	}
	return s.continueOwnedRuntimeFailoverLocked(ctx, store, *state, instance, &binding, &pod)
}

func (s *RuntimeScheduler) resumeOwnedRuntimeFailovers(ctx context.Context) error {
	store, ok := s.instanceRepo.(repository.RuntimeFailoverRepository)
	if !ok {
		return nil
	}
	states, err := store.ListRuntimeFailovers(ctx, runtimeSchedulerBatchLimit)
	if err != nil {
		return err
	}
	var errs []error
	for _, state := range states {
		if err = s.resumeOwnedRuntimeFailover(ctx, store, state); err != nil {
			errs = append(errs, fmt.Errorf("resume failover instance %d: %w", state.InstanceID, err))
		}
	}
	return errors.Join(errs...)
}

func (s *RuntimeScheduler) resumeOwnedRuntimeFailover(ctx context.Context, store repository.RuntimeFailoverRepository, state models.RuntimeFailoverIntent) error {
	release, err := acquireInstanceLifecycleOperation(ctx, s.instanceRepo, state.InstanceID)
	if err != nil {
		return errors.Join(schedulerGuardErrors(err)...)
	}
	defer release()
	// A queued snapshot can predate a manual retry that checkpointed stop while
	// this worker waited for the lifecycle lock. Never replay that stale phase.
	current, err := store.GetRuntimeFailover(ctx, state.InstanceID)
	if err != nil || current == nil {
		return err
	}
	state = *current
	instance, err := s.instanceRepo.GetByID(state.InstanceID)
	if err != nil {
		return err
	}
	binding, err := s.bindingRepo.GetByInstanceID(ctx, state.InstanceID)
	if err != nil {
		return err
	}
	if instance == nil {
		return nil
	}
	if state.Generation < instance.RuntimeGeneration {
		_, err := s.retireSupersededRuntimeFailoverLocked(ctx, store, state, *instance, binding)
		return err
	}
	pod, err := s.podRepo.GetByID(ctx, state.SourcePodID)
	if err != nil {
		return err
	}
	return s.continueOwnedRuntimeFailoverLocked(ctx, store, state, *instance, binding, pod)
}

func (s *RuntimeScheduler) continueOwnedRuntimeFailoverLocked(ctx context.Context, store repository.RuntimeFailoverRepository, state models.RuntimeFailoverIntent, instance models.Instance, binding *models.InstanceRuntimeBinding, pod *models.RuntimePod) error {
	valid := instance.RuntimeType == RuntimeBackendGateway && instance.InstanceMode == InstanceModeLite && instance.ID == state.InstanceID && instance.UserID == state.UserID && instance.RuntimeGeneration == state.Generation && instance.Type == state.RuntimeType && stringValue(instance.WorkspacePath) == state.WorkspacePath && (instance.Status == "running" || instance.Status == "creating" || instance.Status == "error") && binding != nil && binding.InstanceID == state.InstanceID && binding.RuntimePodID == state.SourcePodID && binding.Generation == state.Generation && binding.GatewayID == state.GatewayID && binding.GatewayPort == state.GatewayPort && binding.RuntimeType == state.RuntimeType && binding.WorkspacePath == state.WorkspacePath && pod != nil && stringValue(pod.PodUID) == state.SourcePodUID && stringValue(pod.AgentEndpoint) == state.SourceEndpoint && pod.RuntimeType == state.RuntimeType
	if !valid {
		// Keep the journal for diagnosis, but changed authority cannot retry stop or
		// occupy the front of the automatic queue forever.
		state.Attempts = runtimeFailoverMaxAttempts + 1
		state.NextRetryAt = nil
		return errors.Join(fmt.Errorf("runtime failover source or desired ownership changed"), store.SaveRuntimeFailover(ctx, state))
	}
	if !state.SourceStopped {
		if s.agentClient == nil {
			return fmt.Errorf("runtime failover requires a configured source-stop client")
		}
		if state.Attempts >= runtimeFailoverMaxAttempts || state.NextRetryAt != nil && state.NextRetryAt.After(time.Now().UTC()) {
			return nil
		}
		delay := time.Now().UTC().Add(30 * time.Second)
		state.NextRetryAt = &delay
		// Persist retry timing before the external stop. A successful final stop is
		// checkpointed before advancing and is never gated by the failure budget.
		if err := store.SaveRuntimeFailover(ctx, state); err != nil {
			return err
		}
		if client, ok := s.agentClient.(*slotRuntimeAgentClient); ok {
			if _, err := client.containerStopPod(ctx, state.SourceEndpoint, state.SourcePodUID); err != nil {
				state.Attempts++
				return errors.Join(err, store.SaveRuntimeFailover(ctx, state))
			}
		}
		if err := s.agentClient.DeleteGateway(ctx, state.SourceEndpoint, state.GatewayID); err != nil {
			state.Attempts++
			return errors.Join(fmt.Errorf("failover source stop is unconfirmed: %w", err), store.SaveRuntimeFailover(ctx, state))
		}
		state.SourceStopped = true
		state.NextRetryAt = nil
		if err := store.SaveRuntimeFailover(ctx, state); err != nil {
			return err
		}
	}
	_, err := store.AdvanceRuntimeFailover(ctx, state)
	return err
}

// RetryOwnedRuntimeFailoverLocked is called only by an explicit administrator
// retry while the instance lifecycle lock is held. It retains source identity.
func (s *RuntimeScheduler) RetryOwnedRuntimeFailoverLocked(ctx context.Context, instance models.Instance) (bool, error) {
	store, ok := s.instanceRepo.(repository.RuntimeFailoverRepository)
	if !ok {
		return false, nil
	}
	state, err := store.GetRuntimeFailover(ctx, instance.ID)
	if err != nil {
		return true, err
	}
	if state == nil {
		return false, nil
	}
	binding, err := s.bindingRepo.GetByInstanceID(ctx, instance.ID)
	if err != nil {
		return true, err
	}
	if state.Generation < instance.RuntimeGeneration {
		retired, retireErr := s.retireSupersededRuntimeFailoverLocked(ctx, store, *state, instance, binding)
		return !retired, retireErr
	}
	if state.Generation != instance.RuntimeGeneration || state.UserID != instance.UserID {
		return true, fmt.Errorf("runtime failover intent belongs to another owner or generation")
	}
	pod, err := s.podRepo.GetByID(ctx, state.SourcePodID)
	if err != nil {
		return true, err
	}
	state.Attempts = 0
	state.NextRetryAt = nil
	if err = store.SaveRuntimeFailover(ctx, *state); err != nil {
		return true, err
	}
	return true, s.continueOwnedRuntimeFailoverLocked(ctx, store, *state, instance, binding, pod)
}
