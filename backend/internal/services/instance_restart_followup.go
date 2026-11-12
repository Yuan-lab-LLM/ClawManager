package services

import (
	"context"
	"errors"
	"fmt"
)

var ErrInstanceRestartPending = errors.New("restart accepted; original runtime shutdown is awaiting confirmation")

// Called under the existing instance lifecycle and upgrade guards, including
// recovery by the ordinary stop coordinator. Never starts a second executor.
func (s *instanceService) resumeRestartFollowup(ctx context.Context, id int) error {
	op, err := s.autoStopRepo.RestartFollowup(ctx, id)
	if err != nil || op == nil {
		return err
	}
	i, err := s.instanceRepo.GetByID(id)
	if err != nil {
		return err
	}
	if i == nil {
		return fmt.Errorf("restart instance disappeared")
	}
	if i.RuntimeGeneration == op.Generation+1 {
		// Start committed but its response/progress write was lost. It is now
		// owned by the existing scheduler/start deadline, never replay Start.
		return s.autoStopRepo.FinishRestartFollowup(ctx, *op, nil)
	}
	if i.RuntimeGeneration != op.Generation || i.Status != "stopped" {
		err = fmt.Errorf("restart source generation or stopped state changed")
		_ = s.autoStopRepo.FinishRestartFollowup(ctx, *op, err)
		return err
	}
	// A crash can occur after Start recorded its deadline but before it advanced
	// the generation. The confirmed source is gone and this same persisted
	// follow-up still owns the start. Re-enter without waiting for a false timeout.
	a, err := s.autoStopRepo.GetActivity(ctx, id)
	if err != nil {
		return err
	}
	if a != nil && a.StopOperationID == "" && a.StartDeadlineAt != nil {
		if err = s.autoStopRepo.ClearStartDeadline(ctx, id); err != nil {
			return err
		}
	}
	err = s.startInstance(id)
	if err != nil {
		current, readErr := s.instanceRepo.GetByID(id)
		if readErr == nil && current != nil && current.RuntimeGeneration == op.Generation+1 {
			return s.autoStopRepo.FinishRestartFollowup(ctx, *op, nil)
		}
	}
	if saveErr := s.autoStopRepo.FinishRestartFollowup(ctx, *op, err); saveErr != nil {
		return saveErr
	}
	return err
}
