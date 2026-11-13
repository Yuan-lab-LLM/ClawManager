package services

import (
	"context"
	"fmt"
	"time"

	"clawreef/internal/models"
)

// Persist intent before returning to batch callers. Native teardown can take
// several container restarts; the pending-delete reconciler resumes it on restart.
func (s *instanceService) QueueDelete(instanceID int) error {
	instance, err := s.prepareDeletion(instanceID, 0)
	if err != nil {
		return err
	}
	s.scheduleLegacyDeletion(instance.UserID, instance.ID)
	return nil
}

func (s *instanceService) prepareDeletion(instanceID, teamID int) (*models.Instance, error) {
	if s.ownerTeamMutationGuard != nil {
		if err := s.ownerTeamMutationGuard(instanceID, "delete", teamID); err != nil {
			return nil, err
		}
	}
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return nil, err
	}
	if instance == nil {
		return nil, fmt.Errorf("instance not found")
	}
	if err := s.ValidateDelete(instanceID); err != nil {
		return nil, err
	}
	if instance.Status != "deleting" {
		prepared := *instance
		instance = &prepared
		instance.Status = "deleting"
		instance.UpdatedAt = time.Now()
		var err error
		if marker, ok := s.instanceRepo.(interface {
			MarkDeleting(context.Context, int, int, int) error
		}); ok {
			err = marker.MarkDeleting(context.Background(), instance.ID, instance.UserID, instance.RuntimeGeneration)
		} else {
			err = s.instanceRepo.Update(instance)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to persist deletion intent: %w", err)
		}
		GetHub().BroadcastInstanceStatus(instance.UserID, instance)
	}
	return instance, nil
}

// Bound background teardown without introducing another persistent queue.
var instanceDeletionWorkers = make(chan struct{}, 4)

// The database intent reaches stop owners on other API replicas too. Canceling
// their request releases the lifecycle lock; only the delete worker may then
// supersede graceful stop with its explicit force protocol and real stop proof.
func (s *instanceService) cancelStopWhenDeleting(parent context.Context, id int) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				readCtx, done := context.WithTimeout(ctx, 2*time.Second)
				var instance *models.Instance
				var err error
				if reader, ok := s.instanceRepo.(interface {
					GetByIDContext(context.Context, int) (*models.Instance, error)
				}); ok {
					instance, err = reader.GetByIDContext(readCtx, id)
				} else {
					instance, err = s.instanceRepo.GetByID(id)
				}
				done()
				if err == nil && (instance == nil || instance.Status == "deleting") {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, cancel
}
