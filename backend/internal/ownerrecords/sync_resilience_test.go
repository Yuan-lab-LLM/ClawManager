package ownerrecords

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestCancelledActivityFlushRetainsUncommittedWindows(t *testing.T) {
	s := &Store{}
	s.perf.low.windows = map[string]activityWindow{"pending": {owner: "fixture@example.test", start: time.Now(), mask: 3}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.flushActivity(ctx); err == nil {
		t.Fatal("cancelled flush reported success")
	}
	if w, ok := s.perf.low.windows["pending"]; !ok || w.mask != 3 {
		t.Fatal("uncommitted activity was discarded on timeout")
	}
}

func TestSyncPhaseTimeoutDoesNotCancelSiblings(t *testing.T) {
	ctx, run := newSyncRun(context.Background(), time.Now())
	visited := []string{}
	checkpoints := []string{}
	phases := []syncPhase{{name: "usage", budget: time.Millisecond, run: func(c context.Context) error { <-c.Done(); return c.Err() }}}
	for _, name := range []string{"recent", "history", "projection", "publish", "ranking", "reports"} {
		phases = append(phases, syncPhase{name: name, run: func(c context.Context) error {
			if c.Err() != nil {
				t.Fatal("inherited expired sibling context")
			}
			visited = append(visited, name)
			return nil
		}})
	}
	runSyncPhases(ctx, run, phases, func() { checkpoints = append(checkpoints, run.Current) })
	if len(visited) != 6 || len(checkpoints) != 7 || len(run.Stages) != 7 {
		t.Fatal("a failed phase blocked siblings", visited, checkpoints)
	}
	if run.Stages[0].State != "failed" {
		t.Fatal("actual timeout hidden")
	}
}

func TestSyncPhaseYieldAndShutdownAreDistinct(t *testing.T) {
	ctx, run := newSyncRun(context.Background(), time.Now())
	runSyncPhases(ctx, run, []syncPhase{{name: "projection", run: func(context.Context) error { return errSyncYield }}}, nil)
	if run.Stages[0].State != "partial" || run.Stages[0].Reason != "budget_yield" {
		t.Fatal("normal bounded work labeled failure")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	runSyncPhases(cancelled, run, []syncPhase{{name: "must-not-run", run: func(context.Context) error { t.Fatal("ran after shutdown"); return errors.New("unreachable") }}}, nil)
	if len(run.Stages) != 1 {
		t.Fatal("shutdown fabricated a completed stage")
	}
}

func TestCancelledObservationDrainRetainsUnprocessedItems(t *testing.T) {
	s := &Store{}
	s.QueueBinding(42)
	s.QueueOpenClaw(42, OpenClawEvent{Kind: "submitted"}, time.Now())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.drainObservations(ctx)
	if !s.perf.low.bindings[42] || len(s.perf.low.events) != 1 {
		t.Fatal("unprocessed observations were discarded on timeout")
	}
}

func TestSyncReadBudgetHasTenSecondHeadroom(t *testing.T) {
	if nativeScanBudget < 90*time.Second {
		t.Fatal("scan phase lacks room for both selection groups and a bounded reader")
	}
	if scanSelectionGroupBudget != 10*time.Second || nativeReadBudget != 30*time.Second {
		t.Fatal("database selection and filesystem read must retain separate finite deadlines")
	}
	if syncProjectionBudget < time.Minute || syncReportBudget < 30*time.Second {
		t.Fatal("projection/report budgets lack measured production headroom")
	}
}

func TestBoundedScanQuotaBorrowsUnusedCapacity(t *testing.T) {
	quietLimit := 0
	selection := collectScanGroupsBounded(context.Background(), 12, 3, true, func(_ context.Context, active bool, limit int) ([]scanItem, error) {
		if active {
			return []scanItem{{}}, nil
		}
		quietLimit = limit
		return make([]scanItem, limit), nil
	})
	if selection.err != nil || quietLimit != 14 || len(selection.items) != 15 {
		t.Fatal("idle quota was wasted or unbounded", quietLimit, len(selection.items), selection.err)
	}
	if syncProjectionBudget < 30*time.Second || syncReportBudget < 20*time.Second || nativeReadBudget < 10*time.Second {
		t.Fatal("measured background headroom regressed")
	}
}

func TestInProgressDiagnosticDoesNotClaimFinishTime(t *testing.T) {
	_, run := newSyncRun(context.Background(), time.Now())
	raw, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if v, ok := fields["finished_at"]; ok && v != nil && v != "" {
		t.Fatal("an unfinished batch has a fabricated finish timestamp", v)
	}
}
