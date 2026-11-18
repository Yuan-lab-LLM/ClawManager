package ownerrecords

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSyncResilienceMySQLSlowDiagnosticsAndFinalReserve(t *testing.T) {
	s := resilienceStore(t)
	ctx, r := newSyncRun(context.Background(), time.Now())
	resilienceExec(t, s, `CREATE TRIGGER slow_diagnostic BEFORE INSERT ON northbound_owner_records FOR EACH ROW BEGIN IF NEW.record_type='sync_batch' THEN DO SLEEP(0.25); END IF; END`)
	r.Current = "usage"
	s.checkpointSyncRun(ctx, r, false)
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='sync_batch' AND status='updating'`); n != 1 {
		t.Fatal("a normal 250ms checkpoint was discarded", n)
	}
	// Exhaust intermediate allowance without sleeping for the whole batch.
	// The final result must retain its own one-second reserved allowance.
	r.diagnosticSpent = 4 * time.Second
	r.Current = "history"
	s.checkpointSyncRun(ctx, r, false)
	r.stage("usage", time.Now(), nil)
	s.saveSyncRun(ctx, r)
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='sync_batch' AND status='success' AND interval_end IS NOT NULL`); n != 1 {
		t.Fatal("intermediate checkpoints starved final diagnostic persistence", n)
	}
}

func TestSyncResilienceMySQLEmptyProbeAndSQLCheckpointStaySeparate(t *testing.T) {
	s := resilienceStore(t)
	s.HistoryRoot = t.TempDir()
	ws := filepath.Join(s.HistoryRoot, "openclaw/user-99/instance-1")
	if err := os.MkdirAll(filepath.Join(ws, "home"), 0700); err != nil {
		t.Fatal(err)
	}
	resilienceExec(t, s, `ALTER TABLE instances ADD runtime_generation INT NOT NULL DEFAULT 1`)
	resilienceExec(t, s, `INSERT INTO instances(id,user_id,owner,type,instance_mode,workspace_path,access_token) VALUES(1,99,'empty-worker@example.test','openclaw','lite',?,'fixture')`, ws)
	if _, err := s.ScanHistories(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='history_schedule' AND status='empty' AND version=0`); n != 1 {
		t.Fatal("accessible empty history was incorrectly treated as a read failure", n)
	}
	resilienceExec(t, s, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,instance_id,status) VALUES('history_scan',?,99,'empty-worker@example.test',1,'ready')`, Key("history_scan", "1"))
	resilienceExec(t, s, `UPDATE northbound_owner_records SET expires_at=UTC_TIMESTAMP()-INTERVAL 1 SECOND WHERE record_type='history_schedule'`)
	if _, err := s.ScanHistories(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='history_schedule' AND status='retry' AND JSON_UNQUOTE(JSON_EXTRACT(payload,'$.reason'))='missing'`); n != 1 {
		t.Fatal("previous successful history disappeared but was silently declared empty", n)
	}
}

func TestSyncResilienceMySQLHeldReaderStopsNativeBacklogBurst(t *testing.T) {
	s := resilienceStore(t)
	s.HistoryRoot = t.TempDir()
	resilienceExec(t, s, `ALTER TABLE instances ADD runtime_generation INT NOT NULL DEFAULT 1`)
	for n := 1; n <= 30; n++ {
		resilienceExec(t, s, `INSERT INTO instances(id,user_id,owner,type,instance_mode,workspace_path,access_token) VALUES(?,99,'held-reader@example.test','openclaw','lite','/unused','fixture')`, n)
	}
	nativeReaderSlots <- struct{}{}
	defer func() { <-nativeReaderSlots }()
	for _, lane := range []string{"history", "recent"} {
		ctx, run := newSyncRun(context.Background(), time.Now())
		var err error
		if lane == "history" {
			_, err = s.ScanHistories(ctx, 0)
		} else {
			_, err = s.scanRecentQuestions(ctx, 0, map[uint64]string{})
		}
		if !errors.Is(err, errReaderBusy) {
			t.Fatal("held reader did not apply backpressure", lane, err)
		}
		counts := run.counts[lane]
		if counts == nil || counts.Processed != 1 {
			t.Fatal("one blocked reader caused the entire candidate batch to be attempted", lane, counts)
		}
		if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type=?`, lane+"_schedule"); n != 1 {
			t.Fatal("untouched backlog was unnecessarily rewritten", lane, n)
		}
	}
}

func TestSyncResilienceMySQLStageRemainderDoesNotFailUnreadInstances(t *testing.T) {
	s := resilienceStore(t)
	s.HistoryRoot = t.TempDir()
	resilienceExec(t, s, `INSERT INTO instances(id,user_id,owner,type,instance_mode,status,workspace_path,access_token) VALUES(1,99,'not-admitted@example.test','openclaw','lite','running','/unused','fixture')`)
	for _, lane := range []string{"history", "recent"} {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		var err error
		if lane == "history" {
			_, err = s.ScanHistories(ctx, 0)
		} else {
			_, err = s.scanRecentQuestions(ctx, 0, map[uint64]string{})
		}
		cancel()
		if !errors.Is(err, errSyncYield) {
			t.Fatal("short remainder should leave work queued", lane, err)
		}
	}
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type IN ('history_schedule','recent_schedule','native_reader_guard')`); n != 0 {
		t.Fatal("unread instances were marked as failed or acquired a reader", n)
	}
}

func TestSyncResilienceMySQLLargeRepairQueueResumesInFixedSlices(t *testing.T) {
	s := resilienceStore(t)
	ctx := context.Background()
	now := sourceLocalDay(time.Now()).Add(12 * time.Hour)
	resilienceExec(t, s, `CREATE TABLE queue_digits(n INT PRIMARY KEY)`)
	resilienceExec(t, s, `INSERT INTO queue_digits VALUES(0),(1),(2),(3),(4),(5),(6),(7),(8),(9)`)
	numbers := `SELECT a.n+10*b.n+100*c.n+1000*d.n n FROM queue_digits a CROSS JOIN queue_digits b CROSS JOIN queue_digits c CROSS JOIN queue_digits d`
	resilienceExec(t, s, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,interval_start,interval_end,version) SELECT 'source_window_dirty',SHA2(CONCAT('synthetic-pending-',n),256),0,'pending',TIMESTAMPADD(SECOND,-600*(n+1),?),TIMESTAMPADD(SECOND,-600*n,?),1 FROM (`+numbers+`) v WHERE n<1030`, now.UTC(), now.UTC())
	for pass := int64(1); pass <= 2; pass++ {
		restarted := &Store{Session: s.Session}
		work, cancel := context.WithTimeout(ctx, syncRepairBudget)
		err := restarted.repairSource(withSourceReadBatch(work), now)
		cancel()
		if !errors.Is(err, errSyncYield) {
			t.Fatal("large queue must yield explicitly, not silently appear fully drained", pass, err)
		}
		if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='source_window_dirty' AND status='done'`); n != pass*24 {
			t.Fatal("repair flooded the backlog or replayed already committed work", pass, n)
		}
	}
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='source_window_dirty' AND status='pending'`); n != 982 {
		t.Fatal("bounded recovery lost remaining work", n)
	}
}

func TestSyncResilienceMySQLSlowGuardRelease(t *testing.T) {
	s := resilienceStore(t)
	s.HistoryRoot = t.TempDir()
	g, err := s.acquireReaderGuard(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	resilienceExec(t, s, `CREATE TRIGGER slow_guard_release BEFORE UPDATE ON northbound_owner_records FOR EACH ROW BEGIN IF NEW.record_type='native_reader_guard' AND NEW.status='idle' THEN DO SLEEP(0.75); END IF; END`)
	defer func() { resilienceExec(t, s, `DROP TRIGGER slow_guard_release`); g.release() }()
	g.release()
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='native_reader_guard' AND status='idle'`); n != 1 {
		t.Fatal("a normal 750ms metadata write left the filesystem reader fenced", n)
	}
}

func TestSyncResilienceMySQLTimeoutRetryCappedAndOldBacklogSelectable(t *testing.T) {
	s := resilienceStore(t)
	ctx := context.Background()
	resilienceExec(t, s, `INSERT INTO instances(id,user_id,owner,type,instance_mode,status,workspace_path,access_token) VALUES(1,99,'retry-window@example.test','openclaw','lite','running','/unused','fixture')`)
	items, err := s.scheduledScans(ctx, "history")
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	item := items[0]
	item.failures = 5
	if err = s.finishScan(ctx, "history", item, time.Now(), context.DeadlineExceeded); err != nil {
		t.Fatal(err)
	}
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='history_schedule' AND expires_at<=UTC_TIMESTAMP()+INTERVAL 31 MINUTE`); n != 1 {
		t.Fatal("recovered storage could remain unvisited for 160 minutes", n)
	}
	// Adopt old persisted backoffs without mass rewriting schedules at startup.
	resilienceExec(t, s, `UPDATE northbound_owner_records SET occurred_at=UTC_TIMESTAMP()-INTERVAL 35 MINUTE,expires_at=UTC_TIMESTAMP()+INTERVAL 120 MINUTE WHERE record_type='history_schedule'`)
	restarted := &Store{Session: s.Session}
	items, err = restarted.scheduledScans(ctx, "history")
	if err != nil || len(items) != 1 {
		t.Fatal("old timeout backoff was not admitted after the new cap", len(items), err)
	}
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='history_schedule' AND expires_at>UTC_TIMESTAMP()+INTERVAL 119 MINUTE`); n != 1 {
		t.Fatal("selection rewrote the persisted backlog instead of adopting it", n)
	}
}
