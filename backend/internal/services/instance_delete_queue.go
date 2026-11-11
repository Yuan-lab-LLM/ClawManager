package services

import (
	"fmt"
	"time"
)

// Persist intent before returning to batch callers. Native teardown can take
// several container restarts; the pending-delete reconciler resumes it on restart.
func (s *instanceService) QueueDelete(instanceID int) error {
	if s.ownerTeamMutationGuard != nil {
		if err := s.ownerTeamMutationGuard(instanceID, "delete", 0); err != nil {
			return err
		}
	}
	release, err := s.acquireLifecycleOperation(instanceID)
	if err != nil {
		return err
	}
	defer release()
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return err
	}
	if instance == nil {
		return fmt.Errorf("instance not found")
	}
	if err := s.ValidateDelete(instanceID); err != nil {
		return err
	}
	if instance.Status != "deleting" {
		instance.Status = "deleting"
		instance.UpdatedAt = time.Now()
		if err := s.instanceRepo.Update(instance); err != nil {
			return fmt.Errorf("failed to persist deletion intent: %w", err)
		}
		GetHub().BroadcastInstanceStatus(instance.UserID, instance)
	}
	s.scheduleLegacyDeletion(instance.UserID, instance.ID)
	return nil
}

// Bound background teardown without introducing another persistent queue.
var instanceDeletionWorkers = make(chan struct{}, 4)
