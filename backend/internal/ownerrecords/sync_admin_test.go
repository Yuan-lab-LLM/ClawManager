package ownerrecords

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSyncDiagnosticsBounds(t *testing.T) {
	ctx, run := newSyncRun(context.Background(), time.Now())
	for n := 0; n < 100; n++ {
		recordSyncItem(ctx, "history", Identity{Instance: 1}, "retry", "permission", time.Now(), true)
	}
	if len(run.Events) != 50 || run.Omitted != 50 {
		t.Fatal(len(run.Events), run.Omitted)
	}
	run.stage("usage", time.Now(), errors.New("password=private /secret/chat"))
	if strings.Contains(run.Stages[0].Reason, "private") {
		t.Fatal("unsafe error")
	}
	if _, err := validateSyncQuery(SyncQuery{Kind: "xp_ledger; DELETE", Limit: 20}); err == nil {
		t.Fatal("unsafe type")
	}
	if _, err := validateSyncQuery(SyncQuery{Kind: "sync_batch", Limit: 101}); err == nil {
		t.Fatal("unbounded query")
	}
	if syncReason(context.DeadlineExceeded) != "budget" {
		t.Fatal("budget classification")
	}
	if syncRetryable("owner_snapshot", "retry") || syncRetryable("history_schedule", "ready") {
		t.Fatal("unsafe retry")
	}
	if !syncRetryable("history_schedule", "restricted") {
		t.Fatal("retry unsupported")
	}
}

func verifySyncAdmin(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	count := func(typ string) int {
		row, e := s.Session.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type=?`, typ)
		if e != nil {
			t.Fatal(e)
		}
		var n int
		if e = row.Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	xp, questions, visits := count("xp_event"), count("question_v4"), count("login_day")
	work, run := newSyncRun(ctx, now)
	recordSyncSelected(work, "history", 2)
	recordSyncItem(work, "history", Identity{Scope: 99, Owner: "sync@example.test", Instance: 18881}, "retry", "permission", now, true)
	run.stage("history", now, nil)
	s.saveSyncRun(ctx, run)
	page, e := s.SyncRecords(ctx, SyncQuery{Kind: "sync_batch", Limit: 20, Days: 1}, 0)
	if e != nil || len(page.Records) == 0 {
		t.Fatal("batch missing", e, page)
	}
	if page.Records[0].Status != "partial" {
		t.Fatal("partial falsely successful", page)
	}
	detail, e := s.SyncRecords(ctx, SyncQuery{Kind: "sync_batch", Limit: 1}, page.Records[0].ID)
	if e != nil || !strings.Contains(string(detail.Records[0].Details), "permission") && len(detail.Records[0].Details) == 0 {
		t.Fatal("details", e, detail)
	}
	if _, e = s.Session.SQL().ExecContext(ctx, `INSERT INTO instances(id,user_id,owner,type,instance_mode,workspace_path,access_token,status) VALUES(18881,99,'sync@example.test','openclaw','lite','/workspaces/sync','secret-fixture','running')`); e != nil {
		t.Fatal(e)
	}
	_, e = s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,instance_id,status,version,source_key,occurred_at,expires_at,payload) VALUES('history_schedule',?,99,'sync@example.test',18881,'retry',3,SHA2('/workspaces/sync|secret-fixture',256),?,?,JSON_OBJECT('reason','permission'))`, Key("sync-test"), now, now.Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	// Production-style AUTO_INCREMENT gaps: active schedules retain their old ID
	// while unrelated UPSERTs advance the high watermark by millions.
	if _, e = s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(id,record_type,record_key,scope_id,status) VALUES(50000000,'sync_test_sentinel',?,0,'ready')`, Key("sparse-id")); e != nil {
		t.Fatal(e)
	}
	page, e = s.SyncRecords(ctx, SyncQuery{Kind: "history_schedule", Instance: 18881, Limit: 20}, 0)
	if e != nil || len(page.Records) != 1 {
		t.Fatal("schedule", e, page)
	}
	r := page.Records[0]
	if e = s.RetrySync(ctx, r.ID, r.Updated, 1, now); e != nil {
		t.Fatal("retry", e)
	}
	if e = s.RetrySync(ctx, r.ID, r.Updated, 1, now); !errors.Is(e, ErrSyncConflict) {
		t.Fatal("duplicate allowed", e)
	}
	if count("sync_action") != 1 {
		t.Fatal("missing transactional audit")
	}
	if _, e = s.Session.SQL().ExecContext(ctx, `UPDATE instances SET access_token='new-generation' WHERE id=18881`); e != nil {
		t.Fatal(e)
	}
	page, _ = s.SyncRecords(ctx, SyncQuery{Kind: "history_schedule", Instance: 18881, Limit: 20}, 0)
	if e = s.RetrySync(ctx, r.ID, page.Records[0].Updated, 1, now.Add(11*time.Minute)); !errors.Is(e, ErrSyncConflict) {
		t.Fatal("stale instance accepted", e)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	before := count("sync_batch")
	s.saveSyncRun(canceled, run)
	if count("sync_batch") != before {
		t.Fatal("canceled diagnostics wrote")
	}
	if count("xp_event") != xp || count("question_v4") != questions || count("login_day") != visits {
		t.Fatal("business data mutated")
	}
	old := now.AddDate(0, 0, -40)
	_, e = s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,occurred_at) VALUES('sync_event',?,0,'retry',?),('login_day',?,0,'confirmed',?)`, Key("old-diagnostic"), old, Key("old-protected-visit"), old)
	if e != nil {
		t.Fatal(e)
	}
	_, next := newSyncRun(ctx, now.Add(time.Second))
	next.stage("usage", now, nil)
	s.saveSyncRun(ctx, next)
	if count("login_day") != visits+1 {
		t.Fatal("retention deleted business row")
	}
}
