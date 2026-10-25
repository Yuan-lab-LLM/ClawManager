package ownerrecords

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHistoryFingerprintRepairInputs(t *testing.T) {
	root := t.TempDir()
	i := Identity{Scope: 1, Instance: 2, Runtime: "hermes", Owner: "fixture@example.com"}
	workspace := filepath.Join(root, "hermes", "user-1", "instance-2")
	path := filepath.Join(workspace, "home", ".hermes", "state.db")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("db"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := historyFingerprint(context.Background(), root, workspace, i)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path+"-wal", []byte("wal"), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := historyFingerprint(context.Background(), root, workspace, i)
	if err != nil || second == first {
		t.Fatal("WAL creation must invalidate hint", err)
	}
	if err = os.WriteFile(path+"-wal", []byte("longer wal"), 0600); err != nil {
		t.Fatal(err)
	}
	third, err := historyFingerprint(context.Background(), root, workspace, i)
	if err != nil || third == second {
		t.Fatal("WAL changes must invalidate hint", err)
	}
	if _, err = historyFingerprint(context.Background(), root, workspace, Identity{Scope: 1, Instance: 3, Runtime: "hermes"}); err == nil {
		t.Fatal("foreign workspace accepted")
	}
}

func verifyPerformance(t *testing.T, base *Store) {
	t.Helper()
	ctx := context.Background()
	s := &Store{Session: base.Session}
	now := time.Date(2036, 2, 18, 12, 0, 0, 0, time.UTC)
	// Insert a bounded, isolated 500-owner workload. No real users or workspaces.
	placeholders := []string{}
	args := []any{}
	for n := 0; n < 500; n++ {
		owner := fmt.Sprintf("load-%03d@example.com", n)
		placeholders = append(placeholders, "('invocation',?,0,?,'applied',?,?)")
		args = append(args, Key("load", owner), owner, now.Add(-time.Hour), n+1)
	}
	if _, err := s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,occurred_at,total_tokens) VALUES `+strings.Join(placeholders, ","), args...); err != nil {
		t.Fatal(err)
	}
	// A database trigger detects even no-op UPDATE attempts, not only changed values.
	for _, q := range []string{`CREATE TABLE performance_audit(n BIGINT NOT NULL)`, `INSERT INTO performance_audit VALUES(0)`,
		`CREATE TRIGGER perf_insert AFTER INSERT ON northbound_owner_records FOR EACH ROW UPDATE performance_audit SET n=n+1`,
		`CREATE TRIGGER perf_update AFTER UPDATE ON northbound_owner_records FOR EACH ROW UPDATE performance_audit SET n=n+1`,
		`CREATE TRIGGER perf_delete AFTER DELETE ON northbound_owner_records FOR EACH ROW UPDATE performance_audit SET n=n+1`} {
		if _, err := s.Session.SQL().ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	audit := func() uint64 {
		row, err := s.Session.SQL().QueryRowContext(ctx, `SELECT n FROM performance_audit`)
		if err != nil {
			t.Fatal(err)
		}
		var n uint64
		if err = row.Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	var wg sync.WaitGroup
	durations := make([]time.Duration, 500)
	errs := make(chan error, 500)
	slots := make(chan struct{}, 20)
	begin := time.Now()
	for n := 0; n < 500; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			at := time.Now()
			g, err := s.Growth(ctx, fmt.Sprintf("load-%03d@example.com", n), now)
			durations[n] = time.Since(at)
			if err == nil && (g.Ranking.Rank == nil || *g.Ranking.Rank != 500-n) {
				err = fmt.Errorf("wrong self rank for %d", n)
			}
			errs <- err
		}(n)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := audit(); n != 0 {
		t.Fatalf("read-only polls attempted %d writes", n)
	}
	if s.perf.rankBuilds != 1 {
		t.Fatal("rank was not coalesced", s.perf.rankBuilds)
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	t.Logf("500 owners, 20 concurrent Growth reads: elapsed=%s p95=%s statistics write attempts=0 global rank builds=1", time.Since(begin), durations[474])
	if _, err := s.TokenRanking(ctx, "load-001@example.com", now.Add(29*time.Minute)); err != nil || s.perf.rankBuilds != 1 {
		t.Fatal("rank refreshed before expiry", err)
	}
	if _, err := s.TokenRanking(ctx, "load-001@example.com", now.Add(30*time.Minute)); err != nil || s.perf.rankBuilds != 2 {
		t.Fatal("time expiry did not refresh ranks", err)
	}

	owner := "dirty-check@example.com"
	i := Identity{Scope: 111, Owner: owner, Instance: 91919, Runtime: "hermes", Mode: "lite"}
	if err := s.VisitGrowth(ctx, owner, now); err != nil {
		t.Fatal(err)
	}
	fact := Fact{Type: "invocation", Source: "perf-test", SourceID: "once", At: now.Add(time.Second), Tokens: 5000}
	if _, err := s.Apply(ctx, i, fact); err != nil {
		t.Fatal(err)
	}
	before := audit()
	if added, err := s.Apply(ctx, i, fact); err != nil || added {
		t.Fatal("duplicate fact", err)
	}
	if audit() != before {
		t.Fatal("duplicate immutable fact attempted a write")
	}
	// Clear this owner's work by a fresh Store to prove persistence over takeover.
	restart := &Store{Session: s.Session}
	if err := restart.processDirty(ctx, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	before = audit()
	if err := s.ReconcileGrowth(ctx, owner, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if audit() != before {
		t.Fatal("unchanged task reconciliation wrote rows")
	}
	// Later evidence must remain pending if an older worker tries to ACK its version.
	if _, err := s.Apply(ctx, i, Fact{Type: "question", Source: "perf-test", SourceID: "q1", SessionID: "s", QuestionID: "q1", At: now}); err != nil {
		t.Fatal(err)
	}
	days, err := s.pendingDays(ctx, owner, 10, now.Add(time.Minute))
	if err != nil || len(days) == 0 {
		t.Fatal("no durable dirty date", err)
	}
	d := days[0]
	if _, err = s.Apply(ctx, i, Fact{Type: "question", Source: "perf-test", SourceID: "q2", SessionID: "s", QuestionID: "q2", At: now}); err != nil {
		t.Fatal(err)
	}
	result, err := s.Session.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET status='done' WHERE record_type='usage_dirty' AND record_key=? AND version=?`, d.key, d.version)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := result.RowsAffected(); n != 0 {
		t.Fatal("concurrent evidence lost")
	}
	// Moving a session's first occurrence repairs both dates but not its lifetime count.
	sf := Fact{Type: "session", Source: "perf-test", SourceID: "session", SessionID: "session", At: now}
	if _, err = s.Apply(ctx, i, sf); err != nil {
		t.Fatal(err)
	}
	sf.At = now.AddDate(0, 0, -1)
	if _, err = s.Apply(ctx, i, sf); err != nil {
		t.Fatal(err)
	}
	if err = restart.processDirty(ctx, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	row, err := s.Session.SQL().QueryRowContext(ctx, `SELECT SUM(session_count),SUM(question_count),SUM(total_tokens) FROM northbound_owner_records WHERE record_type='daily_usage' AND owner_normalized=?`, owner)
	if err != nil {
		t.Fatal(err)
	}
	var sessions, questions, tokens uint64
	if err = row.Scan(&sessions, &questions, &tokens); err != nil || sessions != 1 || questions != 2 || tokens != 5000 {
		t.Fatal("changed-day projection drift", sessions, questions, tokens, err)
	}
	closed := now.AddDate(0, 1, 15)
	for n := 0; n < 12; n++ {
		if err = restart.processDueReports(ctx, closed); err != nil {
			t.Fatal(err)
		}
	}
	r, err := restart.ReadReport(ctx, owner, "month", now.Format("2006-01-02"), closed)
	if err != nil || r.Revision == 0 || r.Counts["tokens"] != 5000 {
		t.Fatal("closed snapshot missing", r, err)
	}
	before = audit()
	if _, err = restart.ReadReport(ctx, owner, "month", now.Format("2006-01-02"), closed); err != nil {
		t.Fatal(err)
	}
	if audit() != before {
		t.Fatal("closed report read attempted a write")
	}
	if _, err = s.Apply(ctx, i, Fact{Type: "invocation", Source: "perf-test", SourceID: "late", At: now.Add(time.Second), Tokens: 100}); err != nil {
		t.Fatal(err)
	}
	if err = restart.processDirty(ctx, closed); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 3; n++ {
		if err = restart.processDueReports(ctx, closed); err != nil {
			t.Fatal(err)
		}
	}
	r, err = restart.ReadReport(ctx, owner, "month", now.Format("2006-01-02"), closed)
	if err != nil || r.Revision < 2 || r.Counts["tokens"] != 5100 {
		t.Fatal("late fact did not revise closed report", r, err)
	}
}
