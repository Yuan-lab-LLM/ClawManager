package repository

import (
	"strings"
	"testing"
	"time"
)

func TestAppendTimeFilterAddsSinceAndUntilClauses(t *testing.T) {
	since := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	query := "SELECT 1 FROM model_invocations WHERE instance_id = ?"
	args := []interface{}{9}

	query, args = appendTimeFilter(query, args, SessionUsageFilter{
		Since: &since,
		Until: &until,
	}, "created_at")

	if !strings.Contains(query, "created_at >= ?") || !strings.Contains(query, "created_at < ?") {
		t.Fatalf("expected created_at bounds in query, got %q", query)
	}
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d (%v)", len(args), args)
	}
	if args[1] != since || args[2] != until {
		t.Fatalf("unexpected bound args: %+v", args[1:])
	}
}

func TestAppendTimeFilterEmptyFilterLeavesQueryUnchanged(t *testing.T) {
	query := "SELECT 1 FROM cost_records WHERE instance_id = ?"
	args := []interface{}{9}

	updatedQuery, updatedArgs := appendTimeFilter(query, args, SessionUsageFilter{}, "recorded_at")
	if updatedQuery != query || len(updatedArgs) != 1 {
		t.Fatalf("expected unchanged query/args, got query=%q args=%v", updatedQuery, updatedArgs)
	}
}
