package services

import (
	"context"
	"log"
	"sync"
	"time"

	"clawreef/internal/backupgate"
)

// A full queue falls back to the periodic scan. Requests carry Pod IDs only;
// every execution reloads identity, reservations and native exit proof.
type runtimeSlotRechecker struct {
	mu      sync.Mutex
	queue   chan int64
	pending map[int64]bool
}

func (r *runtimeSlotRechecker) requests() chan int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.queue == nil {
		r.queue = make(chan int64, 64)
		r.pending = make(map[int64]bool)
	}
	return r.queue
}

// RequestSlotAudit never waits for Runtime cleanup or a lifecycle lock.
func (s *RuntimeScheduler) RequestSlotAudit(podID int64) {
	if s == nil || podID < 1 {
		return
	}
	queue := s.slotRecheck.requests()
	s.slotRecheck.mu.Lock()
	defer s.slotRecheck.mu.Unlock()
	if s.slotRecheck.pending[podID] {
		return
	}
	select {
	case queue <- podID:
		s.slotRecheck.pending[podID] = true
	default:
	}
}

func (s *RuntimeScheduler) runSlotAuditRequests(ctx context.Context) {
	queue := s.slotRecheck.requests()
	for {
		select {
		case <-ctx.Done():
			return
		case podID := <-queue:
			s.slotRecheck.mu.Lock()
			delete(s.slotRecheck.pending, podID)
			s.slotRecheck.mu.Unlock()
			s.recheckRuntimePodSlots(ctx, podID)
		}
	}
}

func (s *RuntimeScheduler) recheckRuntimePodSlots(ctx context.Context, podID int64) {
	// Share exclusion with periodic audits, never the long scheduler reconcile.
	s.slotAudit.mu.Lock()
	defer s.slotAudit.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if ctx.Err() != nil {
		return
	}
	release, err := backupgate.HoldWorkUnit(ctx, "runtime.slot-audit")
	if err != nil {
		return
	}
	defer release()
	if s.leader != nil && !s.leader.IsLeader(ctx) {
		return
	}
	c, ok := s.agentClient.(*slotRuntimeAgentClient)
	if !ok || s.podRepo == nil || s.bindingRepo == nil || s.instanceRepo == nil {
		return
	}
	pod, err := s.podRepo.GetByID(ctx, podID)
	if err != nil {
		log.Printf("runtime Pod %d slot recheck unavailable: %v", podID, err)
		return
	}
	if pod == nil || !s.runtimePodEligibleForSlotAudit(*pod, time.Now()) {
		return
	}
	if err := s.auditRuntimePodSlots(ctx, c, *pod); err != nil {
		log.Printf("runtime Pod %d slot recheck failed: %v", podID, err)
	}
}
