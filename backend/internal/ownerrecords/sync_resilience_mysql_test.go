package ownerrecords

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func resilienceStore(t *testing.T) *Store {
	t.Helper()
	session := scanSelectionTestDB(t)
	migration, e := os.ReadFile("../db/migrations/067_add_northbound_owner_records.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{string(migration),
		`CREATE TABLE instances(id BIGINT PRIMARY KEY,user_id BIGINT,owner VARCHAR(128),type VARCHAR(32),instance_mode VARCHAR(16),status VARCHAR(32) DEFAULT 'running',workspace_path TEXT,access_token VARCHAR(255))`,
		`CREATE TABLE model_invocations(id BIGINT PRIMARY KEY,instance_id BIGINT,session_id VARCHAR(255),status VARCHAR(32),created_at DATETIME(6),prompt_tokens BIGINT,completion_tokens BIGINT,total_tokens BIGINT,KEY idx_model_invocations_created_at(created_at),KEY idx_model_invocations_instance_session(instance_id,session_id,created_at))`,
	} {
		if _, e = session.SQL().Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	store := &Store{Session: session}
	addProductionOwnerColumn(t, store)
	return store
}

func TestSyncResilienceMySQLReaderFenceSurvivesStoreRestart(t *testing.T) {
	s := resilienceStore(t)
	ctx := context.Background()
	s.HistoryRoot = "/synthetic-native-root"
	guard, e := s.acquireReaderGuard(ctx)
	if e != nil {
		t.Fatal(e)
	}
	restarted := &Store{Session: s.Session, HistoryRoot: s.HistoryRoot}
	if _, e = restarted.acquireReaderGuard(ctx); !errors.Is(e, errReaderBusy) {
		t.Fatal("restart ignored an unreaped reader", e)
	}
	guard.release() // fixture proves exit; no timer-based unlock
	next, e := restarted.acquireReaderGuard(ctx)
	if e != nil {
		t.Fatal(e)
	}
	next.release()
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='native_reader_guard' AND status='idle'`); n != 1 {
		t.Fatal("clean reap did not release fence", n)
	}
}

func TestSyncResilienceMySQLRealReaderReleasesFence(t *testing.T) {
	s := resilienceStore(t)
	root := t.TempDir()
	s.HistoryRoot = root
	i := Identity{Scope: 99, Instance: 1, Owner: "reader@example.test", Runtime: "openclaw", Mode: "lite"}
	ws := filepath.Join(root, "openclaw/user-99/instance-1")
	if e := os.MkdirAll(filepath.Join(ws, "home"), 0700); e != nil {
		t.Fatal(e)
	}
	r, e := s.isolatedHistory(context.Background(), historyReadRequest{Operation: "empty", Root: root, Workspace: ws, Identity: i})
	if e != nil || !r.Empty {
		t.Fatal("normal reader failed", r, e)
	}
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='native_reader_guard' AND status='idle'`); n != 1 {
		t.Fatal("real child was reaped but durable fence was not released", n)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if !DrainHistoryReaders(ctx) {
		t.Fatal("clean reader did not drain")
	}
}

func TestSyncResilienceMySQLCoverageDoesNotJumpAcrossGap(t *testing.T) {
	s := resilienceStore(t)
	ctx := context.Background()
	today := sourceLocalDay(time.Now())
	yesterday := today.AddDate(0, 0, -1)
	if _, e := s.prepareAnalyticsEpoch(ctx, yesterday); e != nil {
		t.Fatal(e)
	}
	for _, v := range []struct{ day, end time.Time }{{yesterday, yesterday.Add(6 * time.Hour)}, {today, today.Add(time.Hour)}} {
		resilienceExec(t, s, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,interval_end) VALUES('source_day_cursor',?,0,'ready',?)`, Key("source-day-cursor", v.day.Format("2006-01-02")), v.end.UTC())
	}
	covered, e := s.sourceAsOf(ctx, today.Add(2*time.Hour))
	if e != nil || !covered.Equal(yesterday.Add(6*time.Hour)) {
		t.Fatal("coverage jumped across yesterday's gap", covered, e)
	}
}

func TestSyncResilienceMySQLSourceBatchAvoidsRepeatedDayReads(t *testing.T) {
	s := resilienceStore(t)
	ctx := withSourceReadBatch(context.Background())
	day := sourceLocalDay(time.Now())
	resilienceExec(t, s, `INSERT INTO instances(id,user_id,owner,type,instance_mode) VALUES(1,99,'bucket@example.test','openclaw','lite'),(2,99,'late-owner@example.test','openclaw','lite')`)
	resilienceExec(t, s, `INSERT INTO model_invocations VALUES(1,1,'s','completed',?,0,10,10)`, day.Add(time.Minute).UTC())
	for n := 0; n < 4; n++ {
		at := day.Add(time.Duration(n) * summaryInterval)
		if _, e := s.aggregateSourceWindow(ctx, at, at.Add(summaryInterval)); e != nil {
			t.Fatal(e)
		}
	}
	batch := ctx.Value(sourceReadBatchKey{}).(*sourceReadBatch)
	if batch.loads != 1 {
		t.Fatal("each bucket reread the legacy day", batch.loads)
	}
	// Another worker can publish into the same day between slices. Its revision
	// must invalidate the cache, including newly created owner keys to zero.
	resilienceExec(t, s, `INSERT INTO model_invocations VALUES(2,2,'late','completed',?,0,33,33)`, day.Add(2*time.Minute).UTC())
	if _, e := s.aggregateSourceWindow(context.Background(), day, day.Add(summaryInterval)); e != nil {
		t.Fatal(e)
	}
	resilienceExec(t, s, `UPDATE model_invocations SET status='pending' WHERE id=2`)
	if _, e := s.aggregateSourceWindow(ctx, day, day.Add(summaryInterval)); e != nil {
		t.Fatal(e)
	}
	if batch.loads != 2 {
		t.Fatal("concurrent source change did not invalidate cache", batch.loads)
	}
	if n := resilienceInt(t, s, `SELECT SUM(total_tokens) FROM northbound_owner_records WHERE record_type='source_usage'`); n != 10 {
		t.Fatal("stale cache retained corrected data", n)
	}
}

func TestSyncResilienceMySQLSourceBatchRollbackDoesNotAdvanceCache(t *testing.T) {
	s := resilienceStore(t)
	ctx := withSourceReadBatch(context.Background())
	day := sourceLocalDay(time.Now())
	resilienceExec(t, s, `INSERT INTO instances(id,user_id,owner,type,instance_mode) VALUES(1,99,'rollback@example.test','openclaw','lite')`)
	resilienceExec(t, s, `INSERT INTO model_invocations VALUES(1,1,'s','completed',?,0,10,10)`, day.Add(time.Minute).UTC())
	resilienceExec(t, s, `CREATE TRIGGER fail_source_batch BEFORE INSERT ON northbound_owner_records FOR EACH ROW BEGIN IF NEW.record_type='source_usage' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='synthetic source rollback'; END IF; END`)
	if _, e := s.aggregateSourceWindow(ctx, day, day.Add(summaryInterval)); e == nil {
		t.Fatal("failure was not injected")
	}
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type IN ('source_usage','source_day_cursor')`); n != 0 {
		t.Fatal("failed source transaction partially committed", n)
	}
	resilienceExec(t, s, `DROP TRIGGER fail_source_batch`)
	if _, e := s.aggregateSourceWindow(ctx, day, day.Add(summaryInterval)); e != nil {
		t.Fatal(e)
	}
	resilienceExec(t, s, `UPDATE model_invocations SET status='pending' WHERE id=1`)
	if _, e := s.aggregateSourceWindow(ctx, day, day.Add(summaryInterval)); e != nil {
		t.Fatal(e)
	}
	if n := resilienceInt(t, s, `SELECT SUM(total_tokens) FROM northbound_owner_records WHERE record_type='source_usage'`); n != 0 {
		t.Fatal("rollback cache lost the subsequent correction", n)
	}
}

func TestSyncResilienceMySQLGenerationRejectsOldResults(t *testing.T) {
	s := resilienceStore(t)
	ctx := context.Background()
	now := time.Now()
	day := sourceLocalDay(now)
	if _, e := s.prepareAnalyticsEpoch(ctx, day); e != nil {
		t.Fatal(e)
	}
	i := Identity{Scope: 99, Owner: "generation@example.test", Instance: 1, Runtime: "openclaw", Mode: "lite"}
	resilienceExec(t, s, `INSERT INTO instances(id,user_id,owner,type,instance_mode,status,workspace_path,access_token) VALUES(1,99,?,'openclaw','lite','running','/synthetic/home','old-generation')`, i.Owner)
	row, e := s.Session.SQL().QueryRow(`SELECT SHA2(CONCAT(workspace_path,'|',access_token),256) FROM instances WHERE id=1`)
	if e != nil {
		t.Fatal(e)
	}
	var generation string
	if e = row.Scan(&generation); e != nil {
		t.Fatal(e)
	}
	ctx = context.WithValue(ctx, dshGenerationKey{}, generation)
	resilienceExec(t, s, `UPDATE instances SET access_token='new-generation' WHERE id=1`)
	f := Fact{Type: "question", Source: "native_openclaw", SourceID: "q", SessionID: "s", QuestionID: "q", At: now, SubmittedAt: now.Add(-time.Second)}
	if applied, e := s.Apply(ctx, i, f); applied || !errors.Is(e, errScanChanged) {
		t.Fatal("old read entered new generation", applied, e)
	}
	item := scanItem{identity: i, workspace: "/synthetic/home", generation: generation}
	if e = s.finishScan(ctx, "history", item, time.Now(), nil); e != nil {
		t.Fatal(e)
	}
	if e = s.saveRecentQuestions(ctx, i, item.workspace, now, []NativeQuestion{{ID: "q", Session: "s", Text: "synthetic excerpt", At: float64(now.Unix())}}, nil); e != nil {
		t.Fatal(e)
	}
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type IN ('history_schedule','question_v4','recent_question')`); n != 0 {
		t.Fatal("old metadata or excerpt acknowledged", n)
	}
}

func TestSyncResilienceMySQLUnchangedTokenCannotBypassRuntimeGeneration(t *testing.T) {
	s := resilienceStore(t)
	ctx := context.Background()
	now := time.Now()
	if _, e := s.prepareAnalyticsEpoch(ctx, sourceLocalDay(now)); e != nil {
		t.Fatal(e)
	}
	resilienceExec(t, s, `ALTER TABLE instances ADD runtime_generation INT NOT NULL DEFAULT 1`)
	i := Identity{Scope: 99, Instance: 1, Owner: "runtime-epoch@example.test", Runtime: "openclaw", Mode: "lite"}
	resilienceExec(t, s, `INSERT INTO instances(id,user_id,owner,type,instance_mode,workspace_path,access_token) VALUES(1,99,?,'openclaw','lite','/same-path','same-token')`, i.Owner)
	row, e := s.Session.SQL().QueryRow(`SELECT SHA2(CONCAT(workspace_path,'|',access_token),256) FROM instances WHERE id=1`)
	if e != nil {
		t.Fatal(e)
	}
	item := scanItem{identity: i, workspace: "/same-path"}
	if e = row.Scan(&item.generation); e != nil {
		t.Fatal(e)
	}
	ctx, e = s.captureScanRuntime(ctx, &item)
	if e != nil {
		t.Fatal(e)
	}
	resilienceExec(t, s, `UPDATE instances SET runtime_generation=2 WHERE id=1`)
	f := Fact{Type: "question", Source: "native_openclaw", SourceID: "q", SessionID: "s", QuestionID: "q", At: now, SubmittedAt: now.Add(-time.Second)}
	if applied, e := s.Apply(ctx, i, f); applied || !errors.Is(e, errScanChanged) {
		t.Fatal("unchanged token bypassed generation fence", applied, e)
	}
	if e = s.saveRecentQuestions(ctx, i, item.workspace, now, []NativeQuestion{{ID: "q", Session: "s", Text: "old result", At: float64(now.Unix())}}, nil); e != nil {
		t.Fatal(e)
	}
	if e = s.finishScan(ctx, "history", item, now, nil); e != nil {
		t.Fatal(e)
	}
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type IN ('question_v4','recent_question','history_schedule')`); n != 0 {
		t.Fatal("stale generation acknowledged", n)
	}
}

func TestSyncResilienceMySQLOpenClawColdAcknowledgementIsGenerationFenced(t *testing.T) {
	s := resilienceStore(t)
	ctx := context.Background()
	resilienceExec(t, s, `ALTER TABLE instances ADD runtime_generation INT NOT NULL DEFAULT 1`)
	i := Identity{Scope: 99, Instance: 17, Owner: "cold-epoch@example.test", Runtime: "openclaw", Mode: "lite"}
	resilienceExec(t, s, `INSERT INTO instances(id,user_id,owner,type,instance_mode,workspace_path,access_token) VALUES(17,99,?,'openclaw','lite','/same-path','same-token')`, i.Owner)
	item := scanItem{identity: i, workspace: "/same-path"}
	row, err := s.Session.SQL().QueryRow(`SELECT SHA2(CONCAT(workspace_path,'|',access_token),256) FROM instances WHERE id=17`)
	if err != nil || row.Scan(&item.generation) != nil {
		t.Fatal("fixture identity unavailable", err)
	}
	work, err := s.captureScanRuntime(ctx, &item)
	if err != nil {
		t.Fatal(err)
	}
	ack := openClawColdAcknowledgement{Latest: NativeQuestion{Session: "s", ID: "original"}, AgentKey: strings.Repeat("a", 64), Session: "s", Generation: "g", SHA256: strings.Repeat("b", 64)}
	p := &openClawColdProgress{confirmed: map[string]openClawColdAcknowledgement{"cold": ack}}
	if err = s.saveOpenClawColdProgress(work, item, p); err != nil {
		t.Fatal(err)
	}
	resilienceExec(t, s, `UPDATE instances SET runtime_generation=2 WHERE id=17`)
	ack.Latest.ID = "stale-overwrite"
	p.confirmed["cold"] = ack
	if err = s.saveOpenClawColdProgress(work, item, p); err != nil {
		t.Fatal(err)
	}
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='history_files' AND JSON_UNQUOTE(JSON_EXTRACT(payload,'$.cold.latest.id'))='original'`); n != 1 {
		t.Fatal("old runtime overwrote a cold acknowledgement", n)
	}
	next := item
	work, err = s.captureScanRuntime(ctx, &next)
	if err != nil {
		t.Fatal(err)
	}
	_, fresh, err := s.openClawColdProgress(work, next)
	if err != nil || len(fresh.previous) != 0 {
		t.Fatal("new runtime reused old cold acknowledgement", err)
	}
	ack.Latest.ID = "new-runtime"
	fresh.confirmed["cold"] = ack
	if err = s.saveOpenClawColdProgress(work, next, fresh); err != nil {
		t.Fatal(err)
	}
	_, replay, err := s.openClawColdProgress(work, next)
	if err != nil || replay.previous["cold"].Latest.ID != "new-runtime" {
		t.Fatal("same-generation acknowledgement did not resume", err)
	}
}

func TestSyncResilienceMySQLActivityLimitRetainsRemainder(t *testing.T) {
	s := resilienceStore(t)
	s.perf.low.windows = map[string]activityWindow{}
	for n := 0; n < 501; n++ {
		s.perf.low.windows[fmt.Sprint(n)] = activityWindow{owner: "uninitialized@example.test", start: time.Now(), mask: 1}
	}
	if e := s.flushActivity(context.Background()); e != nil {
		t.Fatal(e)
	}
	if len(s.perf.low.windows) != 1 || s.perf.low.windowsInFlight != 0 {
		t.Fatal("work limit discarded pending windows", len(s.perf.low.windows))
	}
	if e := s.flushActivity(context.Background()); e != nil {
		t.Fatal(e)
	}
	if len(s.perf.low.windows) != 0 {
		t.Fatal("second pass did not drain remaining window")
	}
}

func TestSyncResilienceMySQLNativePrefixResumesWithoutDuplicate(t *testing.T) {
	s := resilienceStore(t)
	ctx := context.Background()
	now := time.Now()
	epoch, e := s.prepareAnalyticsEpoch(ctx, sourceLocalDay(now))
	if e != nil {
		t.Fatal(e)
	}
	i := Identity{Scope: 99, Instance: 1, Owner: "prefix@example.test", Runtime: "deepseek-harness", Mode: "lite"}
	facts := make([]NativeQuestion, 300)
	for n := range facts {
		facts[n] = NativeQuestion{ID: fmt.Sprint(n), Session: "synthetic", At: float64(now.Unix()), Submitted: float64(now.Add(-time.Second).Unix())}
	}
	limited, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	_ = s.applyHistoryFacts(limited, i, facts, &epoch)
	cancel()
	before := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='question_v4'`)
	if before == 0 || before >= 300 {
		t.Fatal("fixture did not interrupt an actual partial prefix", before)
	}
	if e = s.applyHistoryFacts(ctx, i, facts, &epoch); e != nil {
		t.Fatal(e)
	}
	if e = s.applyHistoryFacts(ctx, i, facts, &epoch); e != nil {
		t.Fatal(e)
	}
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='question_v4'`); n != 300 {
		t.Fatal("lost or duplicated native facts after resumed prefix", n)
	}
}

func TestSyncResilienceMySQLDeadlineOwnerBackoffSurvivesPhase(t *testing.T) {
	s := resilienceStore(t)
	ctx := context.Background()
	now := time.Now()
	day := sourceLocalDay(now)
	if _, e := s.prepareAnalyticsEpoch(ctx, day); e != nil {
		t.Fatal(e)
	}
	for n, owner := range []string{"slow@example.test", "healthy@example.test"} {
		resilienceExec(t, s, `INSERT INTO instances(id,user_id,owner,type,instance_mode) VALUES(?,99,?,'openclaw','lite')`, n+1, owner)
		resilienceExec(t, s, `INSERT INTO model_invocations VALUES(?,?,'session','completed',?,0,10,10)`, n+1, n+1, day.Add(time.Minute).UTC())
	}
	if _, e := s.aggregateSourceDay(ctx, day, now.Truncate(summaryInterval)); e != nil {
		t.Fatal(e)
	}
	resilienceExec(t, s, `UPDATE northbound_owner_records SET interval_start=? WHERE record_type='usage_dirty' AND owner_normalized='slow@example.test'`, day.UTC())
	resilienceExec(t, s, `CREATE TRIGGER slow_one_owner BEFORE INSERT ON northbound_owner_records FOR EACH ROW BEGIN IF NEW.record_type='daily_usage' AND NEW.owner_normalized='slow@example.test' THEN DO SLEEP(2); END IF; END`)
	limited, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	if e := s.processDirty(limited, now); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("slow transaction was not actually interrupted", e)
	}
	cancel()
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='usage_dirty' AND owner_normalized='slow@example.test' AND JSON_EXTRACT(payload,'$.retry_at')>UNIX_TIMESTAMP()`); n != 1 {
		t.Fatal("phase deadline left slow owner at queue head", n)
	}
	if e := s.processDirty(ctx, now); e != nil {
		t.Fatal(e)
	}
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='usage_dirty' AND owner_normalized='healthy@example.test' AND status='done'`); n != 1 {
		t.Fatal("healthy sibling remained blocked", n)
	}
}

func TestSyncResilienceMySQLThreeBatchesAndRestart(t *testing.T) {
	s := resilienceStore(t)
	ctx := context.Background()
	now := sourceLocalDay(time.Now()).Add(12 * time.Hour)
	if _, e := s.prepareAnalyticsEpoch(ctx, now.Add(-time.Hour)); e != nil {
		t.Fatal(e)
	}
	resilienceExec(t, s, `INSERT INTO instances(id,user_id,owner,type,instance_mode,status,workspace_path,access_token) VALUES(1,99,'rounds@example.test','openclaw','lite','running','/unused','fixture')`)
	resilienceExec(t, s, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,interval_start,interval_end) VALUES('source_bootstrap',?,0,'done',?,?)`, Key("source-bootstrap", "v4"), now.Add(-24*time.Hour).UTC(), now.UTC())
	for _, day := range []time.Time{sourceLocalDay(now).AddDate(0, 0, -1), sourceLocalDay(now)} {
		resilienceExec(t, s, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,interval_end) VALUES('source_day_cursor',?,0,'ready',?)`, Key("source-day-cursor", day.Format("2006-01-02")), now.UTC())
	}
	var expected int64
	for n := 0; n < 3; n++ {
		at := now.Add(time.Duration(n) * summaryInterval)
		resilienceExec(t, s, `INSERT INTO model_invocations VALUES(?,1,'same','completed',?,0,?,?)`, n+1, at.Add(-time.Minute).UTC(), 10+n, 10+n)
		expected += int64(10 + n)
		// A fresh worker at each step proves restart does not block on the entire
		// historical projection queue or reset the source watermark.
		state := &syncWorkerState{recentFingerprints: map[uint64]string{}}
		run := s.runSyncBatch(ctx, at, state)
		for _, phase := range run.Stages {
			if phase.State == "failed" {
				t.Fatalf("round %d phase failed: %+v", n, phase)
			}
		}
		if n := resilienceInt(t, s, `SELECT SUM(total_tokens) FROM northbound_owner_records WHERE record_type='source_usage'`); n != expected {
			t.Fatal("round drift", n, expected)
		}
		if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='usage_dirty' AND status='pending'`); n != 0 {
			t.Fatal("healthy projection stopped", n)
		}
	}
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='sync_batch' AND interval_end IS NOT NULL`); n != 3 {
		t.Fatal("batch history missing", n)
	}
}

func resilienceExec(t *testing.T, s *Store, q string, args ...any) {
	t.Helper()
	if _, e := s.Session.SQL().Exec(q, args...); e != nil {
		t.Fatal(e)
	}
}
func resilienceInt(t *testing.T, s *Store, q string, args ...any) int64 {
	t.Helper()
	row, e := s.Session.SQL().QueryRow(q, args...)
	if e != nil {
		t.Fatal(e)
	}
	var n int64
	if e = row.Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}

func TestSyncResilienceMySQLWindowReplacementAndReplay(t *testing.T) {
	s := resilienceStore(t)
	ctx := context.Background()
	day := sourceLocalDay(time.Now())
	resilienceExec(t, s, `INSERT INTO instances(id,user_id,owner,type,instance_mode) VALUES(1,99,'window@example.test','openclaw','lite')`)
	for n := 1; n <= 2; n++ {
		resilienceExec(t, s, `INSERT INTO model_invocations VALUES(?,1,'same-session','completed',?,0,?,?)`, n, day.Add(time.Duration(n)*10*time.Minute-time.Minute).UTC(), n*10, n*10)
	}
	if _, e := s.aggregateSourceDay(ctx, day, day.Add(30*time.Minute)); e != nil {
		t.Fatal(e)
	}
	resilienceExec(t, s, `UPDATE model_invocations SET total_tokens=7,completion_tokens=7 WHERE id=1`)
	for n := 0; n < 2; n++ {
		if _, e := s.aggregateSourceWindow(ctx, day, day.Add(summaryInterval)); e != nil {
			t.Fatal(e)
		}
	}
	if n := resilienceInt(t, s, `SELECT SUM(total_tokens) FROM northbound_owner_records WHERE record_type='source_usage'`); n != 27 {
		t.Fatal("window replacement zeroed other buckets or replay added twice", n)
	}
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='source_session'`); n != 1 {
		t.Fatal("cross-window session duplication", n)
	}
	covered, e := s.sourceAsOf(ctx, day.Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	// Without an analytics epoch, legacy reads retain their original convention.
	if !covered.Equal(day.Add(time.Hour).Truncate(summaryInterval)) {
		t.Fatal(covered)
	}
	resilienceExec(t, s, `UPDATE model_invocations SET status='pending' WHERE id=1`)
	if _, e := s.aggregateSourceWindow(ctx, day, day.Add(summaryInterval)); e != nil {
		t.Fatal(e)
	}
	if n := resilienceInt(t, s, `SELECT SUM(total_tokens) FROM northbound_owner_records WHERE record_type='source_usage'`); n != 20 {
		t.Fatal("removed terminal row left stale total", n)
	}
}

func TestSyncResilienceMySQLPersistentDiagnostics(t *testing.T) {
	s := resilienceStore(t)
	ctx, r := newSyncRun(context.Background(), time.Now())
	r.Current = "usage"
	s.checkpointSyncRun(ctx, r, false)
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='sync_batch' AND status='updating' AND interval_end IS NULL`); n != 1 {
		t.Fatal("no ongoing record", n)
	}
	r.stage("usage", time.Now(), nil)
	r.Current = "history"
	s.checkpointSyncRun(ctx, r, false)
	r.stage("history", time.Now(), errSyncYield)
	s.saveSyncRun(ctx, r)
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='sync_batch'`); n != 1 {
		t.Fatal("checkpoints created duplicate batches", n)
	}
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='sync_batch' AND status='partial' AND interval_end IS NOT NULL`); n != 1 {
		t.Fatal("finish was not recorded", n)
	}
}

func TestSyncResilienceMySQLBadOwnerDoesNotBlockHealthy(t *testing.T) {
	s := resilienceStore(t)
	ctx := context.Background()
	day := sourceLocalDay(time.Now())
	now := time.Now()
	if _, e := s.prepareAnalyticsEpoch(ctx, day); e != nil {
		t.Fatal(e)
	}
	for n, owner := range []string{"bad@example.test", "good@example.test"} {
		resilienceExec(t, s, `INSERT INTO instances(id,user_id,owner,type,instance_mode) VALUES(?,99,?,'openclaw','lite')`, n+1, owner)
		resilienceExec(t, s, `INSERT INTO model_invocations VALUES(?,?,'session','completed',?,0,10,10)`, n+1, n+1, day.Add(time.Minute).UTC())
	}
	if _, e := s.aggregateSourceDay(ctx, day, now.Truncate(summaryInterval)); e != nil {
		t.Fatal(e)
	}
	resilienceExec(t, s, `CREATE TRIGGER fail_one_owner BEFORE INSERT ON northbound_owner_records FOR EACH ROW BEGIN IF NEW.record_type='daily_usage' AND NEW.owner_normalized='bad@example.test' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='synthetic per-owner failure'; END IF; END`)
	if e := s.processDirty(ctx, now); e == nil {
		t.Fatal("real owner error suppressed")
	}
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='usage_dirty' AND owner_normalized='good@example.test' AND status='done'`); n != 1 {
		t.Fatal("healthy owner did not progress", n)
	}
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='usage_dirty' AND owner_normalized='bad@example.test' AND status='pending' AND JSON_EXTRACT(payload,'$.retry_at')>UNIX_TIMESTAMP()`); n != 1 {
		t.Fatal("failed owner not retained with backoff", n)
	}
	restart := &Store{Session: s.Session}
	if e := restart.processDirty(ctx, now); e != nil {
		t.Fatal("backoff not persisted across restart", e)
	}
}

func TestSyncResilienceMySQLProductionScaleSource(t *testing.T) {
	s := resilienceStore(t)
	ctx := context.Background()
	day := sourceLocalDay(time.Now())
	now := day.Add(12 * time.Hour)
	if _, e := s.prepareAnalyticsEpoch(ctx, day); e != nil {
		t.Fatal(e)
	}
	resilienceExec(t, s, `CREATE TABLE digits(n INT PRIMARY KEY)`)
	resilienceExec(t, s, `INSERT INTO digits VALUES(0),(1),(2),(3),(4),(5),(6),(7),(8),(9)`)
	numbers := `SELECT a.n+10*b.n+100*c.n+1000*d.n n FROM digits a CROSS JOIN digits b CROSS JOIN digits c CROSS JOIN digits d`
	resilienceExec(t, s, `INSERT INTO instances(id,user_id,owner,type,instance_mode) SELECT n+1,99,CONCAT('scale-',n%1000,'@example.test'),'openclaw','lite' FROM (`+numbers+`) v WHERE n<3200`)
	resilienceExec(t, s, `INSERT INTO model_invocations SELECT n+1,1+n%3200,CONCAT('session-',n%3200),'completed',TIMESTAMPADD(SECOND,(n*7)%43200,?),1,9,10 FROM (`+numbers+`) v WHERE n<10000`, day.UTC())
	// Existing coverage is adopted; only the last four missing buckets are work.
	if _, e := s.aggregateSourceDay(ctx, day, now.Add(-40*time.Minute)); e != nil {
		t.Fatal(e)
	}
	resilienceExec(t, s, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,interval_end) VALUES('source_day_cursor',?,0,'ready',?)`, Key("source-day-cursor", day.AddDate(0, 0, -1).Format("2006-01-02")), day.UTC())
	before := time.Now()
	work, cancel := context.WithTimeout(ctx, syncSourceBudget)
	e := s.refreshSource(withSourceReadBatch(work), now)
	elapsed := time.Since(before)
	cancel()
	if e != nil {
		t.Fatal("bounded source pass did not finish", e)
	}
	if n := resilienceInt(t, s, `SELECT SUM(total_tokens) FROM northbound_owner_records WHERE record_type='source_usage'`); n != 100000 {
		t.Fatal("wrong total", n)
	}
	covered, e := s.sourceAsOf(ctx, now)
	if e != nil || !covered.Equal(now) {
		t.Fatal("coverage not advanced", covered, e)
	}
	if e = s.refreshSource(ctx, now); e != nil {
		t.Fatal("idle replay", e)
	}
	if n := resilienceInt(t, s, `SELECT SUM(total_tokens) FROM northbound_owner_records WHERE record_type='source_usage'`); n != 100000 {
		t.Fatal("replay drift", n)
	}
	t.Logf("source_window scale=3200_instances/1000_owners/10000_calls elapsed=%s", elapsed)
	if e = s.discoverSourceWindows(ctx, now); e != nil {
		t.Fatal(e)
	}
	resilienceExec(t, s, `INSERT INTO model_invocations VALUES(10001,1,'late','completed',?,0,50,50)`, day.Add(-time.Minute).UTC())
	if e = s.discoverSourceWindows(ctx, now); e != nil {
		t.Fatal(e)
	}
	if n := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='source_window_dirty' AND status='pending'`); n != 1 {
		t.Fatal("late cross-day insertion was missed", n)
	}
	if e = s.repairSource(ctx, now); e != nil {
		t.Fatal(e)
	}
	if n := resilienceInt(t, s, `SELECT SUM(total_tokens) FROM northbound_owner_records WHERE record_type='source_usage'`); n != 100050 {
		t.Fatal("late call not repaired", n)
	}
	started := time.Now()
	initialPending := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='usage_dirty' AND status='pending'`)
	work, cancel = context.WithTimeout(ctx, syncProjectionBudget)
	projectionCtx, diagnostic := newSyncRun(work, now)
	projectionErr := s.processDirty(projectionCtx, now)
	cancel()
	if projectionErr != nil && !errors.Is(projectionErr, context.DeadlineExceeded) && !errors.Is(projectionErr, errSyncYield) {
		t.Fatal(projectionErr)
	}
	remaining := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='usage_dirty' AND status='pending'`)
	processed := 0
	if count := diagnostic.counts["projection"]; count != nil {
		processed = count.Processed
	}
	if processed == 0 || remaining >= initialPending {
		t.Fatal("no healthy owner advanced in the full production-scale phase")
	}
	t.Logf("projection_scale owners=1000 processed=%d remaining_dates=%d elapsed=%s", processed, remaining, time.Since(started))
	// Even after discarding every process-local cache, a fresh worker must pick
	// up the durable remainder rather than restart all owners or re-add totals.
	restarted := &Store{Session: s.Session}
	work, cancel = context.WithTimeout(ctx, 3*time.Second)
	e = restarted.processDirty(work, now)
	cancel()
	if e != nil && !errors.Is(e, context.DeadlineExceeded) && !errors.Is(e, errSyncYield) {
		t.Fatal("restart failed to resume scale backlog", e)
	}
	remainingAfterRestart := resilienceInt(t, s, `SELECT COUNT(*) FROM northbound_owner_records WHERE record_type='usage_dirty' AND status='pending'`)
	if remainingAfterRestart >= remaining {
		t.Fatal("restart did not reduce persisted pending work", remainingAfterRestart, remaining)
	}
	if n := resilienceInt(t, s, `SELECT SUM(total_tokens) FROM northbound_owner_records WHERE record_type='source_usage'`); n != 100050 {
		t.Fatal("projection restart changed source accounting", n)
	}
	t.Logf("projection_restart remaining_before=%d remaining_after=%d", remaining, remainingAfterRestart)
}
