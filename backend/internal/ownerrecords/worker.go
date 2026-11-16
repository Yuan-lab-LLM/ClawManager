package ownerrecords

import (
	"context"
	"errors"
	"log"
	"time"
)

var errSyncYield = errors.New("bounded work remains for the next pass")

type syncPhase struct {
	name   string
	run    func(context.Context) error
	budget time.Duration
}

// Each phase owns a fresh deadline. A failed source/reader cannot cancel healthy
// sibling work. Only actual shutdown stops subsequent phases.
func runSyncPhases(ctx context.Context, r *syncRun, phases []syncPhase, checkpoint func()) {
	for _, phase := range phases {
		if ctx.Err() != nil {
			return
		}
		r.Current = phase.name
		if checkpoint != nil {
			checkpoint()
		}
		budget := phase.budget
		if budget == 0 {
			budget = syncPhaseBudget
		}
		work, cancel := context.WithTimeout(ctx, budget)
		started := time.Now()
		err := phase.run(work)
		if work.Err() != nil {
			err = errors.Join(err, work.Err())
		}
		cancel()
		r.stage(phase.name, started, err)
		if err != nil && ctx.Err() == nil {
			log.Printf("owner phase=%s deferred reason=%s", phase.name, syncReason(err))
		}
	}
	r.Current = ""
}

type syncWorkerState struct {
	bootstrapped                bool
	historyCursor, recentCursor uint64
	recentFingerprints          map[uint64]string
}

// No catch-up loop and no dependency on draining the whole projection queue at
// startup. Per-unit cursors survive restart; the normal ten-minute cadence stays.
func (s *Store) Run(ctx context.Context) {
	state := &syncWorkerState{recentFingerprints: map[uint64]string{}}
	for ctx.Err() == nil {
		if s.Available() {
			s.runSyncBatch(ctx, time.Now(), state)
		}
		if !waitSummaryBatch(ctx) {
			return
		}
	}
}

func (s *Store) runSyncBatch(parent context.Context, now time.Time, state *syncWorkerState) *syncRun {
	ctx, diagnostic := newSyncRun(parent, now)
	checkpoint := func() { s.checkpointSyncRun(parent, diagnostic, false) }
	var usageErr, projectionErr error
	phases := []syncPhase{}
	if !state.bootstrapped {
		phases = append(phases, syncPhase{name: "bootstrap", run: func(work context.Context) error {
			for n := 0; n < 32; n++ {
				if !scanHasBudget(work) {
					return errSyncYield
				}
				ready, err := s.bootstrapSource(work, now)
				if err != nil {
					return err
				}
				if ready {
					state.bootstrapped = true
					return nil
				}
			}
			return errSyncYield
		}})
	}
	native := withDSHBatch(ctx)
	phases = append(phases,
		syncPhase{name: "usage", budget: syncSourceBudget, run: func(work context.Context) error {
			usageErr = s.batchState(work, now, "updating")
			if usageErr == nil {
				usageErr = s.refreshSource(withSourceReadBatch(work), now)
			}
			return usageErr
		}},
		syncPhase{name: "recent", budget: nativeScanBudget, run: func(work context.Context) error {
			var err error
			state.recentCursor, err = s.scanRecentQuestions(withDSHCache(work, native), state.recentCursor, state.recentFingerprints)
			return err
		}},
		syncPhase{name: "history", budget: nativeScanBudget, run: func(work context.Context) error {
			var err error
			state.historyCursor, err = s.ScanHistories(withDSHCache(work, native), state.historyCursor)
			return err
		}},
		syncPhase{name: "repair", budget: syncRepairBudget, run: func(work context.Context) error { return s.repairSource(withSourceReadBatch(work), now) }},
		syncPhase{name: "projection", budget: syncProjectionBudget, run: func(work context.Context) error { projectionErr = s.processDirty(work, now); return projectionErr }},
		syncPhase{name: "publish", run: func(work context.Context) error {
			row, err := s.Session.SQL().QueryRowContext(work, `SELECT EXISTS(SELECT 1 FROM northbound_owner_records WHERE record_type='usage_dirty' AND status='pending' AND business_date<=?)`, sourceLocalDay(now).Format("2006-01-02"))
			if err != nil {
				return err
			}
			var pending bool
			if err = row.Scan(&pending); err != nil {
				return err
			}
			covered, err := s.sourceAsOf(work, now)
			if err != nil {
				return err
			}
			ready := state.bootstrapped && usageErr == nil && projectionErr == nil && !pending && !covered.Before(now.Truncate(summaryInterval))
			status := "deferred"
			if ready {
				_, err = s.Session.SQL().ExecContext(work, `UPDATE northbound_owner_records SET status='ready' WHERE record_type='analytics_epoch' AND record_key=? AND status<>'ready'`, Key("analytics-epoch", "v4"))
				if err != nil {
					return err
				}
				status = "ready"
			}
			if err = s.batchState(work, now, status); err != nil {
				return err
			}
			if !ready {
				return errSyncYield
			}
			return nil
		}},
		syncPhase{name: "ranking", run: func(work context.Context) error {
			covered, err := s.sourceAsOf(work, now)
			if err != nil {
				return err
			}
			return s.publishRanking(work, covered)
		}},
		syncPhase{name: "reports", budget: syncReportBudget, run: func(work context.Context) error { return s.processReports(work, now) }},
	)
	runSyncPhases(ctx, diagnostic, phases, checkpoint)
	s.saveSyncRun(parent, diagnostic)
	return diagnostic
}

func withDSHCache(ctx, native context.Context) context.Context {
	return context.WithValue(ctx, dshBatchKey{}, native.Value(dshBatchKey{}))
}
