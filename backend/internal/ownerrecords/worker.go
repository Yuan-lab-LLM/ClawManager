package ownerrecords

import (
	"context"
	"log"
	"time"
)

// Low-priority bounded work; no catch-up loop after a slow pass.
func (s *Store) Run(ctx context.Context) {
	var historyCursor uint64
	var recentCursor uint64
	recentFingerprints := make(map[uint64]string)
	bootstrapped := false
	for {
		if !s.Available() {
			if !waitSummaryBatch(ctx) {
				return
			}
			continue
		}
		if !bootstrapped {
			pass, cancel := context.WithTimeout(ctx, 5*time.Second)
			ready, err := s.bootstrapSource(pass, time.Now())
			cancel()
			if ready && err == nil {
				projection, stop := context.WithTimeout(ctx, 5*time.Second)
				err = s.processDirty(projection, time.Now())
				if err == nil {
					pending, e := s.pendingDays(projection, "", 1, time.Now())
					err = e
					ready = len(pending) == 0
				}
				if ready && err == nil {
					_, err = s.Session.SQL().ExecContext(projection, `UPDATE northbound_owner_records SET status='ready' WHERE record_type='analytics_epoch' AND record_key=? AND status<>'ready'`, Key("analytics-epoch", "v4"))
				}
				stop()
			}
			if err != nil && ctx.Err() == nil {
				log.Printf("owner initialization deferred: %T", err)
			}
			bootstrapped = ready && err == nil
			if !bootstrapped {
				delay := time.Second
				if err != nil {
					delay = time.Minute
				}
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				continue
			}
		}
		batchAt := time.Now()
		pass, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := s.batchState(pass, batchAt, "updating")
		if err == nil {
			err = s.refreshSource(pass, batchAt)
		}
		cancel()
		if err != nil && ctx.Err() == nil {
			log.Printf("owner usage batch deferred: %T", err)
			if !waitSummaryBatch(ctx) {
				return
			}
			continue
		}
		recent, stopRecent := context.WithTimeout(ctx, 3*time.Second)
		recentCursor, err = s.scanRecentQuestions(recent, recentCursor, recentFingerprints)
		stopRecent()
		if err != nil && ctx.Err() == nil {
			log.Printf("owner recent question batch deferred: %T", err)
		}
		history, cancel := context.WithTimeout(ctx, 3*time.Second)
		next, err := s.ScanHistories(history, historyCursor)
		cancel()
		historyCursor = next
		if err != nil && ctx.Err() == nil {
			log.Printf("owner history batch deferred: %T", err)
		}
		historyOK := err == nil
		projection, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = s.processDirty(projection, time.Now())
		cancel()
		if err != nil && ctx.Err() == nil {
			log.Printf("owner projection batch deferred: %T", err)
		}
		status, stop := context.WithTimeout(ctx, 3*time.Second)
		if err == nil && historyOK {
			pending, e := s.pendingDays(status, "", 1, batchAt)
			if e == nil && len(pending) == 0 {
				_ = s.batchState(status, batchAt, "ready")
			}
		}
		if e := s.publishRanking(status, batchAt); e != nil && ctx.Err() == nil {
			log.Printf("owner ranking deferred: %T", e)
		}
		stop()
		if !waitSummaryBatch(ctx) {
			return
		}
	}
}
