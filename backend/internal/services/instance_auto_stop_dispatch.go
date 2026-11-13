package services

import (
	"context"
	"fmt"
	"log"

	"clawreef/internal/backupgate"
)

type autoStopWork struct {
	id  int
	ctx context.Context
	run func()
}

// dispatchStop separates the due-scan clock from native RPC latency. Capacity
// and deduplication cover pending retries and newly eligible stops together.
func (s *InstanceAutoStopService) dispatchStop(ctx context.Context, id int, run func()) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	instance, err := s.lifecycle.instanceRepo.GetByID(id)
	if err != nil {
		return err
	}
	if instance == nil || instance.Status == "deleting" {
		return nil
	}
	kind, supported := v2RuntimeTypeForInstance(instance)
	if !supported {
		_ = s.store.RecordBlocker(ctx, id, fmt.Sprintf("unsupported automatic-stop runtime type %q", instance.Type))
		return nil
	}
	s.workMu.Lock()
	// ponytail: three native workers per type; larger pools can raise this
	// ceiling without coupling unrelated types or spawning unbounded work.
	if s.working[id] {
		s.workMu.Unlock()
		return nil
	}
	if s.working == nil {
		s.working = make(map[int]bool)
		s.byType = make(map[string]int)
		s.queued = make(map[string][]autoStopWork)
	}
	s.working[id] = true
	// One queued job per existing instance. Overflow stays FIFO instead of
	// being forgotten when the due-scan cursor advances.
	s.queued[kind] = append(s.queued[kind], autoStopWork{id: id, ctx: ctx, run: run})
	if s.byType[kind] >= 3 {
		s.workMu.Unlock()
		return nil
	}
	s.byType[kind]++
	s.workers.Add(1)
	s.workMu.Unlock()
	go func() {
		defer s.workers.Done()
		for {
			s.workMu.Lock()
			queue := s.queued[kind]
			if len(queue) == 0 {
				s.byType[kind]--
				s.workMu.Unlock()
				return
			}
			work := queue[0]
			queue[0] = autoStopWork{}
			s.queued[kind] = queue[1:]
			s.workMu.Unlock()
			// Queued selection does not claim execution authority. Acquire the
			// backup drain lease only when a finite native unit actually starts.
			if work.ctx.Err() == nil {
				release, err := backupgate.HoldWorkUnit(work.ctx, "instance.auto-stop-worker")
				if err == nil {
					work.run()
					release()
				} else if work.ctx.Err() == nil {
					log.Printf("automatic Lite stop: worker admission instance %d: %v", work.id, err)
				}
			}
			s.workMu.Lock()
			delete(s.working, work.id)
			s.workMu.Unlock()
		}
	}()
	return nil
}
