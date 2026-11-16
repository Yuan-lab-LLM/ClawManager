package services

import (
	"context"
	"errors"
	"log"
	"time"

	"clawreef/internal/backupgate"
)

// Explicit maintenance and upgrade operations can wait on native migration for
// minutes. They retain their durable ownership and full context without holding
// up instance placement, observation or idle detection in the ordinary loop.
func (s *RuntimeScheduler) runRuntimeOperations(ctx context.Context) {
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()
	for {
		s.reconcileRuntimeOperationsIfLeader(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *RuntimeScheduler) reconcileRuntimeOperationsIfLeader(ctx context.Context) {
	release, err := backupgate.HoldWorkUnit(ctx, "runtime.operations")
	if err != nil {
		return
	}
	defer release()
	if s.leader != nil && !s.leader.IsLeader(ctx) {
		return
	}
	if err := s.reconcileRuntimeOperations(ctx); err != nil {
		log.Printf("runtime operations reconcile failed: %v", err)
	}
}

func (s *RuntimeScheduler) reconcileRuntimeOperations(ctx context.Context) error {
	var errs []error
	if s.management != nil {
		if err := s.management.Process(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if err := s.reconcileRollouts(ctx); err != nil {
		errs = append(errs, err)
	}
	// Finish only previously authorized, receipted slot releases. This creates
	// no new recovery/migration intent and never runs in ordinary placement.
	if s.management != nil {
		if err := s.management.reconcileHistoricalFaultSlots(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
