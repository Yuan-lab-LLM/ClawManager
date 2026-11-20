package services

import (
	"errors"
	"fmt"
)

var ErrInstanceNotStopped = errors.New("instance is not stopped")

// StartStopped is an explicit owner start, never a restart or reset. Keep the
// same Team, cross-replica lifecycle and Runtime upgrade guards as Start, then
// recheck state under those guards before entering the existing sandbox path.
func (s *instanceService) StartStopped(id int) error {
	if s.ownerTeamMutationGuard != nil {
		if err := s.ownerTeamMutationGuard(id, "start", 0); err != nil {
			return err
		}
	}
	release, err := s.acquireLifecycleOperation(id)
	if err != nil {
		return err
	}
	defer release()
	return s.withRuntimeUpgradeMutation(id, func() error {
		instance, err := s.instanceRepo.GetByID(id)
		if err != nil {
			return err
		}
		if instance == nil {
			return fmt.Errorf("instance not found")
		}
		if instance.Status == "running" || instance.Status == "creating" {
			return nil
		}
		if instance.Status != "stopped" {
			return ErrInstanceNotStopped
		}
		return s.startInstance(id)
	})
}
