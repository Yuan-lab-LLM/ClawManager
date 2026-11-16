package ownerrecords

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanPhaseBudgetAndAppend(t *testing.T) {
	if nativeScanBudget != 90*time.Second || nativeReadBudget != 30*time.Second {
		t.Fatal("scan and read allowances lost coordinated finite bounds", nativeScanBudget, nativeReadBudget)
	}
	for _, parse := range []func(string, []byte) ([]NativeQuestion, error){ParseNativeLog, parseRecentNativeLog} {
		if _, err := parse("openclaw", []byte("{\"type\":\"session\",\"id\":\"s\"}\n{\"type\":")); !errors.Is(err, errIncompleteTail) {
			t.Fatal("unfinished append", err)
		}
		if _, err := parse("openclaw", []byte("broken\n{}\n")); err == nil || errors.Is(err, errIncompleteTail) {
			t.Fatal("middle corruption hidden", err)
		}
		if _, err := parse("openclaw", []byte("{\"type\":\"session\",\"id\":\"s\"}")); err != nil {
			t.Fatal("complete unterminated JSON", err)
		}
	}
}

func verifyScanSafety(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	ws := filepath.Join(root, "deepseek-harness", "user-99", "instance-12001")
	if err := os.MkdirAll(filepath.Join(ws, "home"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Session.SQL().ExecContext(ctx, `INSERT INTO instances(id,user_id,owner,type,instance_mode,workspace_path,access_token,status) VALUES(12001,99,'empty@example.test','deepseek-harness','lite',?,'generation1','running')`, ws); err != nil {
		t.Fatal(err)
	}
	scanner := &Store{Session: s.Session, HistoryRoot: root}
	item := scanItem{identity: Identity{Instance: 12001, Scope: 99, Owner: "empty@example.test", Runtime: "deepseek-harness", Mode: "lite"}, workspace: ws, generation: "test-generation"}
	row, _ := s.Session.SQL().QueryRowContext(ctx, `SELECT SHA2(CONCAT(workspace_path,'|',access_token),256) FROM instances WHERE id=12001`)
	if err := row.Scan(&item.generation); err != nil {
		t.Fatal(err)
	}
	if ok, err := scanner.noHistoryYet(ctx, item); err != nil || !ok {
		t.Fatal("new source absence", ok, err)
	}
	if err := scanner.finishScan(ctx, "history", item, time.Now(), os.ErrNotExist); err != nil {
		t.Fatal(err)
	}
	assertState := func(want string) {
		t.Helper()
		r, e := s.Session.SQL().QueryRowContext(ctx, `SELECT status FROM northbound_owner_records WHERE record_type='history_schedule' AND instance_id=12001`)
		if e != nil {
			t.Fatal(e)
		}
		var state string
		if e = r.Scan(&state); e != nil || state != want {
			t.Fatal(state, want, e)
		}
	}
	assertState("empty")
	if _, err := s.Session.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET occurred_at=UTC_TIMESTAMP()-INTERVAL 1 MINUTE WHERE record_type='history_schedule' AND instance_id=12001`); err != nil {
		t.Fatal(err)
	}
	// New activity wakes empty sources at the next regular invocation of the worker.
	if _, err := s.Session.SQL().ExecContext(ctx, `INSERT INTO model_invocations(id,instance_id,session_id,status,created_at,prompt_tokens,completion_tokens,total_tokens) VALUES(99002,12001,'new','completed',UTC_TIMESTAMP()-INTERVAL 2 SECOND,0,0,0)`); err != nil {
		t.Fatal(err)
	}
	contains := func(want bool) {
		t.Helper()
		items, e := scanner.scheduledScans(ctx, "history")
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, v := range items {
			if v.identity.Instance == 12001 {
				found = true
			}
		}
		if found != want {
			t.Fatal("eligibility", found, want)
		}
	}
	contains(true)
	if _, err := s.Session.SQL().ExecContext(ctx, `UPDATE instances SET status='stopped' WHERE id=12001`); err != nil {
		t.Fatal(err)
	}
	contains(false)
	if _, err := s.Session.SQL().ExecContext(ctx, `UPDATE instances SET status='running' WHERE id=12001`); err != nil {
		t.Fatal(err)
	}
	contains(true)
	// Missing workspace is not an unused source. No file operations in production.
	moved := item
	moved.workspace = filepath.Join(root, "absent")
	if ok, err := scanner.noHistoryYet(ctx, moved); err != nil || ok {
		t.Fatal("workspace failure swallowed", ok, err)
	}
	if _, err := s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,instance_id,status) VALUES('history_scan','safety-evidence',99,'empty@example.test',12001,'ready')`); err != nil {
		t.Fatal(err)
	}
	if ok, err := scanner.noHistoryYet(ctx, item); err != nil || ok {
		t.Fatal("previous history loss swallowed", ok, err)
	}
	if err := scanner.finishScan(ctx, "history", item, time.Now(), os.ErrPermission); err != nil {
		t.Fatal(err)
	}
	assertState("retry")
	if _, err := s.Session.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET version=3,interval_start=UTC_TIMESTAMP()-INTERVAL 40 MINUTE WHERE record_type='history_schedule' AND instance_id=12001`); err != nil {
		t.Fatal(err)
	}
	state := SnapshotState{}
	if err := s.applyBatchView(ctx, item.identity.Owner, time.Now(), "ready", &state); err != nil || state.State != "deferred" {
		t.Fatal("persistent failure hidden", state, err)
	}
	if _, err := s.Session.SQL().ExecContext(ctx, `UPDATE instances SET access_token='generation2' WHERE id=12001`); err != nil {
		t.Fatal(err)
	}
	if err := s.applyBatchView(ctx, item.identity.Owner, time.Now(), "ready", &state); err != nil || state.State != "ready" {
		t.Fatal("old generation warning", state, err)
	}
	contains(true)
	// Recent-only persistent failures stay local and preserve existing excerpts.
	row, _ = s.Session.SQL().QueryRowContext(ctx, `SELECT SHA2(CONCAT(workspace_path,'|',access_token),256) FROM instances WHERE id=12001`)
	if err := row.Scan(&item.generation); err != nil {
		t.Fatal(err)
	}
	if err := scanner.finishScan(ctx, "recent", item, time.Now(), os.ErrPermission); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Session.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET version=3,interval_start=UTC_TIMESTAMP()-INTERVAL 40 MINUTE WHERE record_type='recent_schedule' AND instance_id=12001`); err != nil {
		t.Fatal(err)
	}
	qs, err := s.recentQuestions(ctx, item.identity.Owner, nil, time.Now())
	if err != nil || qs[12001].Warning == "" {
		t.Fatal("missing local warning", qs, err)
	}
	if err = s.applyBatchView(ctx, item.identity.Owner, time.Now(), "ready", &state); err != nil || state.State != "ready" {
		t.Fatal("excerpt affects numbers", state, err)
	}
	if _, err = s.Session.SQL().ExecContext(ctx, `UPDATE instances SET owner='new-owner@example.test' WHERE id=12001`); err != nil {
		t.Fatal(err)
	}
	qs, err = s.recentQuestions(ctx, item.identity.Owner, nil, time.Now())
	if err != nil || len(qs) != 0 {
		t.Fatal("ownership leak", qs, err)
	}
	if err = markUsageDay(ctx, s.Session, "dirty-safety@example.test", time.Now().Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err = s.applyBatchView(ctx, "dirty-safety@example.test", time.Now(), "ready", &state); err != nil || state.State != "ready" {
		t.Fatal("old event is not old pending work", state, err)
	}
	if _, err = s.Session.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET interval_start=UTC_TIMESTAMP()-INTERVAL 40 MINUTE WHERE record_type='usage_dirty' AND owner_normalized='dirty-safety@example.test'`); err != nil {
		t.Fatal(err)
	}
	if err = s.applyBatchView(ctx, "dirty-safety@example.test", time.Now(), "ready", &state); err != nil || state.State != "deferred" {
		t.Fatal("stalled pending work hidden", state, err)
	}
}
