package ownerrecords

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestScanBudgetAndBackoff(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if scanHasBudget(ctx) {
		t.Fatal("must reserve time for checkpoint")
	}
	for n, want := range []time.Duration{10, 10, 20, 40, 80, 160, 160} {
		if got := scanRetryDelay(uint64(n)); got != want*time.Minute {
			t.Fatalf("retry %d: %v", n, got)
		}
	}
	if scanErrorClass(fmt.Errorf("private/path: %w", os.ErrNotExist)) != "missing" {
		t.Fatal("missing classification")
	}
	if strings.Contains(scanErrorClass(fmt.Errorf("private prompt")), "private") {
		t.Fatal("error leaks input")
	}
}

func TestBatchDisplayExpiresRunningAndScopesPending(t *testing.T) {
	now := time.Now()
	for _, tt := range []struct {
		status  string
		age     time.Duration
		pending bool
		want    string
	}{
		{"updating", 10 * time.Second, false, "updating"},
		{"updating", time.Minute, false, "updating"},
		{"updating", 7 * time.Minute, false, "updating"},
		{"updating", 9 * time.Minute, false, "deferred"},
		{"ready", time.Minute, false, "ready"},
		{"ready", time.Minute, true, "deferred"},
		{"deferred", time.Minute, false, "ready"},
		{"ready", 31 * time.Minute, false, "deferred"},
		{"updating", 31 * time.Minute, false, "deferred"},
	} {
		if got := batchDisplayState(tt.status, now.Add(-tt.age), now, tt.pending); got != tt.want {
			t.Fatalf("%+v got %s", tt, got)
		}
	}
}

func TestHistoryReadCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := historyContextReader{ctx, strings.NewReader("must not read")}
	if n, err := reader.Read(make([]byte, 32)); n != 0 || err != context.Canceled {
		t.Fatal(n, err)
	}
}

// Called in the existing localhost-only, disposable MySQL integration suite.
func verifyScanScheduling(t *testing.T, s *Store) {
	t.Helper()
	defer verifyScanSafety(t, s)
	ctx := context.Background()
	values := []string{}
	args := []any{}
	for n := 0; n < 1030; n++ {
		values = append(values, "(?,99,?,'openclaw','lite','/unused','test')")
		args = append(args, 10000+n, fmt.Sprintf("schedule-%d@example.test", n))
	}
	if _, err := s.Session.SQL().ExecContext(ctx, `INSERT INTO instances(id,user_id,owner,type,instance_mode,workspace_path,access_token) VALUES `+strings.Join(values, ","), args...); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Session.SQL().ExecContext(ctx, `INSERT INTO model_invocations(id,instance_id,session_id,status,created_at,prompt_tokens,completion_tokens,total_tokens) VALUES(99001,11029,'test','completed',UTC_TIMESTAMP(),0,0,0)`); err != nil {
		t.Fatal(err)
	}
	items, err := s.scheduledScans(ctx, "recent")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) > 20 {
		t.Fatal("unbounded scan")
	}
	var active *scanItem
	for idx := range items {
		if items[idx].identity.Instance == 11029 {
			active = &items[idx]
		}
	}
	if active == nil {
		t.Fatal("active high ID starved behind 1000 quiet instances")
	}
	if err = s.finishScan(ctx, "recent", *active, time.Now(), os.ErrNotExist); err != nil {
		t.Fatal(err)
	}
	// New Store simulates leader/app restart; no in-memory cursor is required.
	restarted := &Store{Session: s.Session}
	items, err = restarted.scheduledScans(ctx, "recent")
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range items {
		if i.identity.Instance == 11029 {
			t.Fatal("retry ignored after restart")
		}
	}
	if _, err = s.Session.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET expires_at=UTC_TIMESTAMP()-INTERVAL 1 SECOND WHERE record_type='recent_schedule' AND instance_id=11029`); err != nil {
		t.Fatal(err)
	}
	items, err = restarted.scheduledScans(ctx, "recent")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, i := range items {
		if i.identity.Instance == 11029 {
			found = true
			if i.failures != 1 {
				t.Fatal("failure count lost")
			}
			if err = s.finishScan(ctx, "recent", i, time.Now(), nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !found {
		t.Fatal("due retry omitted")
	}
	state := SnapshotState{}
	if err = s.batchState(ctx, time.Now(), "ready"); err != nil {
		t.Fatal(err)
	}
	if err = s.applyBatchView(ctx, active.identity.Owner, time.Now(), "ready", &state); err != nil {
		t.Fatal(err)
	}
	if state.State != "ready" || state.LastBatchAt == nil {
		t.Fatal("successful retry still pending", state)
	}
	if err = s.finishScan(ctx, "recent", *active, time.Now(), os.ErrNotExist); err != nil {
		t.Fatal(err)
	}
	if err = s.applyBatchView(ctx, "unrelated@example.test", time.Now(), "ready", &state); err != nil || state.State != "ready" {
		t.Fatal("foreign retry affects owner", state, err)
	}
	if err = s.applyBatchView(ctx, active.identity.Owner, time.Now(), "ready", &state); err != nil || state.State != "ready" {
		t.Fatal("first excerpt retry must not warn globally", state, err)
	}
}
