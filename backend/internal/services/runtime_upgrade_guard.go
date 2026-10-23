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
	if instance.Status != "creating" && instance.Status != "running" && instance.Status != "error" {
		return nil
	}
	if isRuntimeErrorInstance(instance) && !isRecoverableRuntimeSchedulingError(instance) {
		return nil
	}
	binding, err := s.bindingRepo.GetRunningByInstanceID(ctx, instance.ID)
	if err != nil {
		return []error{err}
	}
	if binding != nil {
		stale, err := s.cleanupStaleInstanceBinding(ctx, instance, binding)
		if err != nil {
			return []error{err}
		}
		if !stale {
			if err := s.syncInstanceStateFromBinding(ctx, instance, binding); err != nil {
				return []error{err}
			}
			return nil
		}
	}
	// This checks any non-running binding before attempting assignment and
	// retains the lifecycle lock across port reservation, HTTP create and cleanup.
	return s.reconcileCreatingInstanceLocked(ctx, instance)
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
