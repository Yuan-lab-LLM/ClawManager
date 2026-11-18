package services

import (
	"context"
	"errors"
	"fmt"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

func schedulerGuardErrors(err error) []error {
	if errors.Is(err, models.ErrRuntimeUpgradeInProgress) || errors.Is(err, models.ErrInstanceOperationInProgress) {
		return nil
	}
	return []error{err}
}

func currentLifecycleSnapshot(repo repository.InstanceRepository, fallback models.Instance) (*models.Instance, error) {
	// SQL-backed repositories must re-read under the distributed lock. Offline
	// adapters without that contract keep their existing snapshot semantics.
	if _, ok := repo.(instanceLifecycleLocker); ok {
		return repo.GetByID(fallback.ID)
	}
	return &fallback, nil
}

func (s *RuntimeScheduler) reconcileDesiredInstance(ctx context.Context, snapshot models.Instance) []error {
	if !isSchedulerManagedV2Instance(snapshot) {
		return nil
	}
	release, err := acquireInstanceLifecycleOperation(ctx, s.instanceRepo, snapshot.ID)
	if err != nil {
		return schedulerGuardErrors(err)
	}
	defer release()
	current, err := currentLifecycleSnapshot(s.instanceRepo, snapshot)
	if err != nil {
		return []error{err}
	}
	if current == nil || !isSchedulerManagedV2Instance(*current) {
		return nil
	}
	instance := *current
	if instance.Status == "creating" {
		return s.reconcileCreatingInstanceLocked(ctx, instance)
	}
	if instance.Status != "running" {
		return nil
	}
	binding, err := s.bindingRepo.GetByInstanceID(ctx, instance.ID)
	if err != nil {
		return []error{err}
	}
	if binding != nil {
		runtimeType, _ := schedulerRuntimeType(instance)
		if binding.InstanceID != instance.ID || binding.RuntimeType != runtimeType || binding.Generation != instance.RuntimeGeneration {
			return []error{fmt.Errorf("instance %d Runtime binding identity or generation differs; explicit inspection is required", instance.ID)}
		}
		if err := s.syncInstanceStateFromBinding(ctx, instance, binding); err != nil {
			return []error{err}
		}
		return nil
	}
	// A running row is an observation, not permission to create another writer.
	// Start/Recovery must explicitly establish a new creating intent.
	var errs []error
	s.markInstanceError(ctx, instance, &runtimeFailure{code: "RUNTIME_BINDING_MISSING", cause: fmt.Errorf("instance %d has no Runtime binding; explicit start or recovery is required", instance.ID)}, &errs)
	return errs
}

func (s *RuntimeScheduler) failoverBinding(ctx context.Context, podID int64, snapshot models.InstanceRuntimeBinding, reason string) error {
	release, err := acquireInstanceLifecycleOperation(ctx, s.instanceRepo, snapshot.InstanceID)
	if err != nil {
		if len(schedulerGuardErrors(err)) == 0 {
			return nil
		}
		return err
	}
	defer release()
	instance, err := s.instanceRepo.GetByID(snapshot.InstanceID)
	if err != nil {
		return err
	}
	if instance == nil || instance.Status == "stopped" || instance.Status == "deleting" {
		return nil
	}
	current, err := s.bindingRepo.GetByInstanceID(ctx, snapshot.InstanceID)
	if err != nil {
		return err
	}
	if current == nil || current.RuntimePodID != podID || current.Generation != snapshot.Generation || current.GatewayID != snapshot.GatewayID {
		return nil
	}
	if current.Generation != instance.RuntimeGeneration {
		return nil
	}
	if requiresConfirmedGatewayStop(s.agentClient) {
		pod, err := s.podRepo.GetByID(ctx, podID)
		if err != nil {
			return err
		}
		if pod == nil || stringValue(pod.PodUID) == "" || stringValue(pod.AgentEndpoint) == "" || current.GatewayID == "" {
			return fmt.Errorf("failover requires the exact source runtime identity")
		}
		if store, ok := s.instanceRepo.(repository.RuntimeFailoverRepository); ok {
			return s.beginOwnedRuntimeFailover(ctx, store, *instance, *current, *pod, reason)
		}
		// Heartbeat loss and Pod disappearance do not retire the durable writer
		// assignment. Keep SQL ownership until the normal stop path proves it.
		if err := s.agentClient.DeleteGateway(ctx, *pod.AgentEndpoint, current.GatewayID); err != nil {
			return fmt.Errorf("failover source stop is unconfirmed: %w", err)
		}
	}
	if err := s.instanceRepo.UpdateRuntimeState(ctx, instance.ID, "creating", instance.RuntimeGeneration+1, &reason); err != nil {
		return err
	}
	if err := s.bindingRepo.DeleteByInstanceID(ctx, instance.ID); err != nil {
		return err
	}
	if err := s.podRepo.ReleaseSlot(ctx, podID); err != nil {
		return fmt.Errorf("release failed-over slot: %w", err)
	}
	return nil
}
