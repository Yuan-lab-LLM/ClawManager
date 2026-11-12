package ownerrecords

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

// Returning only twenty instances does not bound the invocation rows examined.
// Use the time range for global activity and the existing covering instance
// index for a correlated empty-history wakeup, never an unrestricted table scan.
func TestScanSelectionAccessRegression(t *testing.T) {
	for _, active := range []bool{true, false} {
		query := scheduledScanQuery(active)
		if strings.Contains(query, "FROM model_invocations m WHERE") {
			t.Fatal("activity selection still leaves its invocation access path unconstrained")
		}
		if strings.Count(query, "FORCE INDEX(idx_model_invocations_created_at)") != 1 || strings.Count(query, "FORCE INDEX(idx_model_invocations_instance_session)") != 1 {
			t.Fatal("activity/wakeup predicates must use their bounded existing indexes")
		}
		if !strings.Contains(query, "MAX_EXECUTION_TIME(10000)") {
			t.Fatal("selection SQL must share the ten-second context allowance")
		}
		if !strings.Contains(query, "northbound_owner_records q FORCE INDEX(idx_owner_record_binding)") {
			t.Fatal("populated schedules can regress to scanning all rows of a record type")
		}
	}
}

func TestScanSelectionAllowsMeasuredDatabaseLatency(t *testing.T) {
	selection := collectScanGroups(context.Background(), func(ctx context.Context, _ bool, _ int) ([]scanItem, error) {
		select {
		case <-time.After(600 * time.Millisecond):
			return []scanItem{{}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	if selection.err != nil || len(selection.items) != 2 {
		t.Fatal("healthy selection still rejected by the old 500ms inner deadline", selection.err)
	}
}

func TestScanSelectionRetainsSiblingAndPhaseBudget(t *testing.T) {
	for _, failingActive := range []bool{false, true} {
		t.Run(map[bool]string{true: "active-fails", false: "quiet-fails"}[failingActive], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), nativeScanBudget)
			defer cancel()
			queries := 0
			selection := collectScanGroups(ctx, func(query context.Context, active bool, limit int) ([]scanItem, error) {
				queries++
				deadline, ok := query.Deadline()
				if !ok || time.Until(deadline) > scanSelectionGroupBudget || limit != map[bool]int{true: 16, false: 4}[active] {
					t.Fatal("group lost its bounded deadline/limit")
				}
				if active == failingActive {
					<-query.Done()
					// Incomplete rows from a failed group must never be used.
					return []scanItem{{identity: Identity{Instance: 666}}}, query.Err()
				}
				return []scanItem{{identity: Identity{Instance: 7}}}, nil
			})
			if queries != 2 || len(selection.items) != 1 || selection.items[0].identity.Instance != 7 || ctx.Err() != nil {
				t.Fatal("sibling/phase context lost", queries, selection, ctx.Err())
			}
			var incomplete *scanSelectionError
			if !errors.As(selection.err, &incomplete) || !incomplete.partial || !errors.Is(selection.err, context.DeadlineExceeded) {
				t.Fatal("partial selection missing", selection.err)
			}
		})
	}
}

func TestScanSelectionFailureDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name                    string
		activeFails, quietFails bool
		want                    string
	}{
		{"both-fail", true, true, "failed"}, {"active-fails", true, false, "partial"}, {"quiet-fails", false, true, "partial"}, {"empty-success", false, false, "success"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, run := newSyncRun(context.Background(), time.Now())
			selection := collectScanGroups(ctx, func(_ context.Context, active bool, _ int) ([]scanItem, error) {
				if active && test.activeFails || !active && test.quietFails {
					return nil, &mysqlDriver.MySQLError{Number: 3024, Message: "private/path password=secret"}
				}
				return nil, nil
			})
			recordSyncSelection(ctx, "history", selection.groups)
			recordSyncSelected(ctx, "history", len(selection.items))
			run.stage("history", time.Now(), selection.err)
			stage := run.Stages[0]
			if stage.State != test.want || !stage.SelectedKnown || len(stage.Selection) != 2 || len(run.Events) != 0 {
				t.Fatal("incorrect group/zero-count diagnosis", stage)
			}
			body, _ := json.Marshal(stage)
			if strings.Contains(string(body), "private") || strings.Contains(string(body), "secret") || !strings.Contains(string(body), `"selected":0`) {
				t.Fatal("private error leaked or zero disguised", string(body))
			}
		})
	}
}

func TestScanSelectionCanceledParentAndOversizeGroup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	selection := collectScanGroups(ctx, func(context.Context, bool, int) ([]scanItem, error) {
		t.Fatal("canceled parent must not issue SQL")
		return nil, nil
	})
	if !errors.Is(selection.err, context.Canceled) || len(selection.items) != 0 {
		t.Fatal(selection)
	}
	selection = collectScanGroups(context.Background(), func(_ context.Context, _ bool, limit int) ([]scanItem, error) {
		return make([]scanItem, limit+1), nil
	})
	if selection.err == nil || len(selection.items) != 0 {
		t.Fatal("oversized query result accepted", selection)
	}
}
