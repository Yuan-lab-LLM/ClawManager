package ownerrecords

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/upper/db/v4/adapter/mysql"
)

// Uses only a unique disposable database on an explicitly configured localhost.
func TestOwnerMigrationMySQL(t *testing.T) {
	host := os.Getenv("NB_KEYS_TEST_MYSQL")
	if host == "" {
		t.Skip("local disposable MySQL not configured")
	}
	if !strings.HasPrefix(host, "127.0.0.1:") {
		t.Fatal("localhost required")
	}
	root, err := mysql.Open(mysql.ConnectionURL{Host: host, User: "root", Password: os.Getenv("NB_KEYS_TEST_PASSWORD"), Database: "mysql"})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	name := fmt.Sprintf("owner_records_test_%d", time.Now().UnixNano())
	if _, err = root.SQL().Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	defer root.SQL().Exec("DROP DATABASE " + name)
	db, err := mysql.Open(mysql.ConnectionURL{Host: host, User: "root", Password: os.Getenv("NB_KEYS_TEST_PASSWORD"), Database: name, Options: map[string]string{"parseTime": "true"}})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	migration, err := os.ReadFile("../db/migrations/067_add_northbound_owner_records.sql")
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2; n++ {
		if _, err = db.SQL().Exec(string(migration)); err != nil {
			t.Fatal(err)
		}
	}
	insert := `INSERT INTO northbound_owner_records (record_type,record_key,scope_id,owner,owner_normalized,status,total_tokens) VALUES (?,?,?,?,?,?,?)`
	args := []any{"invocation", Key("invocation", "local", "1"), 1, "Test@example.com", "test@example.com", "applied", uint64(5000000000)}
	if _, err = db.SQL().Exec(insert, args...); err != nil {
		t.Fatal(err)
	}
	// Owner reassignment must not create a second copy of one immutable source.
	args[2] = 2
	args[3] = "other@example.com"
	args[4] = "other@example.com"
	if _, err = db.SQL().Exec(insert, args...); err == nil {
		t.Fatal("duplicate source accepted")
	}
	var tokens uint64
	row, err := db.SQL().QueryRow(`SELECT total_tokens FROM northbound_owner_records WHERE record_type='invocation'`)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&tokens); err != nil || tokens != 5000000000 {
		t.Fatal("64-bit token storage", tokens, err)
	}
	var fks int
	row, err = db.SQL().QueryRow(`SELECT COUNT(*) FROM information_schema.REFERENTIAL_CONSTRAINTS WHERE CONSTRAINT_SCHEMA=?`, name)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&fks); err != nil || fks != 0 {
		t.Fatal("unexpected cascading relation", fks, err)
	}
	store := &Store{Session: db}
	identity := Identity{Scope: 10, Owner: "Owner@example.com", Instance: 123, Runtime: "openclaw", Mode: "lite"}
	fact := Fact{Type: "invocation", Source: "model_invocations", SourceID: "991", At: time.Now().UTC(), Prompt: 10, Completion: 20, Tokens: 30}
	var wg sync.WaitGroup
	errors := make(chan error, 8)
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := store.Apply(context.Background(), identity, fact); errors <- err }()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	row, err = db.SQL().QueryRow(`SELECT total_tokens,invocation_count FROM northbound_owner_records WHERE record_type='profile' AND scope_id=10`)
	if err != nil {
		t.Fatal(err)
	}
	var calls uint64
	if err = row.Scan(&tokens, &calls); err != nil || tokens != 30 || calls != 1 {
		t.Fatal("concurrent replay inflated totals", tokens, calls, err)
	}
	// Same words are not used as the identity: two question IDs count twice.
	for _, id := range []string{"q1", "q1", "q2"} {
		_, err = store.Apply(context.Background(), identity, Fact{Type: "question", Source: "test-protocol", SourceID: id, SessionID: "s1", QuestionID: id, At: fact.At})
		if err != nil {
			t.Fatal(err)
		}
	}
	row, err = db.SQL().QueryRow(`SELECT question_count FROM northbound_owner_records WHERE record_type='profile' AND scope_id=10`)
	if err != nil {
		t.Fatal(err)
	}
	var questions uint64
	if err = row.Scan(&questions); err != nil || questions != 2 {
		t.Fatal("question dedupe", questions, err)
	}
	// Overflow must roll back the fact, allowing a later safe retry.
	_, err = db.SQL().Exec(`UPDATE northbound_owner_records SET total_tokens=18446744073709551615 WHERE record_type='profile' AND scope_id=10`)
	if err != nil {
		t.Fatal(err)
	}
	fact.SourceID = "rollback-check"
	if _, err = store.Apply(context.Background(), identity, fact); err == nil {
		t.Fatal("overflow accepted")
	}
	row, err = db.SQL().QueryRow(`SELECT COUNT(*) FROM northbound_owner_records WHERE source_key='rollback-check'`)
	if err != nil {
		t.Fatal(err)
	}
	var facts int
	if err = row.Scan(&facts); err != nil || facts != 0 {
		t.Fatal("failed aggregate left dedupe fact", facts, err)
	}
	for _, statement := range []string{
		`CREATE TABLE instances (id BIGINT PRIMARY KEY,user_id BIGINT,owner VARCHAR(128),type VARCHAR(32),instance_mode VARCHAR(16))`,
		`INSERT INTO instances VALUES (800,20,'real-owner@example.com','openclaw','lite')`,
		`CREATE TABLE model_invocations (id BIGINT PRIMARY KEY,instance_id BIGINT,session_id VARCHAR(255),status VARCHAR(32),created_at DATETIME(6),prompt_tokens BIGINT,completion_tokens BIGINT,total_tokens BIGINT)`,
		`INSERT INTO model_invocations VALUES (801,800,'stable-session','completed',UTC_TIMESTAMP(6),3,7,10)`,
	} {
		if _, err = db.SQL().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	submitted := OpenClawEvent{Kind: "submitted", Session: "stable-session", RunID: "run-1"}
	completed := submitted
	completed.Kind = "completed"
	if err = store.ObserveOpenClaw(context.Background(), 800, completed, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = store.ObserveOpenClaw(context.Background(), 800, submitted, time.Now()); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2; n++ {
		if err = store.ObserveOpenClaw(context.Background(), 800, completed, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	// Model completion arrives after the old instance has been removed. The
	// request-time binding snapshot must retain the original owner attribution.
	if _, err = db.SQL().Exec(`DELETE FROM instances WHERE id=800`); err != nil {
		t.Fatal(err)
	}
	if n, err := store.Backfill(context.Background(), 100); err != nil || n != 1 {
		t.Fatal("late completion after reset", n, err)
	}
	if n, err := store.Backfill(context.Background(), 100); err != nil || n != 0 {
		t.Fatal("backfill replay", n, err)
	}
	scope := uint64(20)
	dashboard, err := store.Dashboard(context.Background(), "real-owner@example.com", &scope, "7d", time.Now())
	if err != nil || dashboard.Summary.TotalTokens != 10 || dashboard.Summary.Sessions != 1 {
		t.Fatal("dashboard totals", dashboard, err)
	}
	// A reset replacement belongs to the same owner but has a new instance ID.
	// Even a reused chat session name must represent a new instance-local session.
	for _, statement := range []string{
		`INSERT INTO instances VALUES (802,20,'real-owner@example.com','openclaw','lite')`,
		`INSERT INTO model_invocations VALUES (803,802,'stable-session','completed',UTC_TIMESTAMP(6),4,6,10)`,
		`INSERT INTO model_invocations VALUES (804,802,'sess_trc_generated','completed',UTC_TIMESTAMP(6),1,2,3)`,
	} {
		if _, err = db.SQL().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := store.Backfill(context.Background(), 100); err != nil || n != 2 {
		t.Fatal("reset replacement", n, err)
	}
	dashboard, err = store.Dashboard(context.Background(), "real-owner@example.com", &scope, "7d", time.Now())
	if err != nil || dashboard.Summary.TotalTokens != 23 || dashboard.Summary.Sessions != 2 {
		t.Fatal("reset totals or generated-session overcount", dashboard, err)
	}
	// Multiple caller accounts must remain isolated even for the same owner string.
	other := Identity{Scope: 21, Owner: "real-owner@example.com", Instance: 900, Runtime: "hermes", Mode: "lite"}
	if _, err = store.Apply(context.Background(), other, Fact{Type: "invocation", Source: "fixture", SourceID: "other-account", At: time.Now(), Tokens: 7}); err != nil {
		t.Fatal(err)
	}
	portal, err := store.Dashboard(context.Background(), "real-owner@example.com", nil, "7d", time.Now())
	if err != nil || portal.Summary.TotalTokens != 30 {
		t.Fatal("verified portal owner aggregation", portal, err)
	}
	wrongScope := uint64(21)
	dashboard, err = store.Dashboard(context.Background(), "real-owner@example.com", &wrongScope, "7d", time.Now())
	if err != nil || dashboard.Summary.TotalTokens != 7 {
		t.Fatal("scope leak", dashboard, err)
	}
	// Native replay and the live observer use different session aliases, but
	// the same human submission must still count exactly once.
	liveIdentity := Identity{Scope: 20, Owner: "real-owner@example.com", Instance: 800, Runtime: "openclaw", Mode: "lite"}
	for _, source := range []string{"native_openclaw", "openclaw_chat", "native_openclaw"} {
		if _, err = store.Apply(context.Background(), liveIdentity, Fact{Type: "question", Source: source, SourceID: source + "-replay", SessionID: "native-uuid", QuestionID: "run-1", At: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	dashboard, err = store.Dashboard(context.Background(), "real-owner@example.com", &scope, "7d", time.Now())
	if err != nil || dashboard.Summary.Questions == nil || *dashboard.Summary.Questions != 1 {
		t.Fatal("native/live replay inflated questions", dashboard, err)
	}
	// Exercise the actual history worker SQL, filesystem reader and dashboard
	// together, including repeated scans and missing histories.
	rootPath := t.TempDir()
	workspace := filepath.Join(rootPath, "openclaw", "user-22", "instance-901")
	directory := filepath.Join(workspace, "home", ".openclaw", "agents", "main", "sessions")
	if err = os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	fixture := "{\"type\":\"session\",\"id\":\"native-session\"}\n{\"type\":\"message\",\"id\":\"human\",\"message\":{\"role\":\"user\",\"content\":\"fixture\",\"__openclaw\":{\"senderIsOwner\":true}}}\n{\"type\":\"message\",\"id\":\"reply\",\"parentId\":\"human\",\"timestamp\":\"2026-09-16T01:00:00Z\",\"message\":{\"role\":\"assistant\",\"content\":\"fixture reply\",\"stopReason\":\"stop\"}}\n"
	if err = os.WriteFile(filepath.Join(directory, "session.jsonl"), []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SQL().Exec(`ALTER TABLE instances ADD workspace_path TEXT, ADD access_token VARCHAR(255)`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.SQL().Exec(`INSERT INTO instances (id,user_id,owner,type,instance_mode,workspace_path) VALUES (901,22,'native@example.com','openclaw','lite',?)`, workspace); err != nil {
		t.Fatal(err)
	}
	store.HistoryRoot = rootPath
	// Real instances also has status: exercise name collisions in INSERT SELECT.
	if _, err = db.SQL().Exec(`ALTER TABLE instances ADD status VARCHAR(32) NOT NULL DEFAULT 'running'`); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2; n++ {
		if _, err = store.ScanHistories(context.Background(), 0); err != nil {
			t.Fatal(err)
		}
	}
	nativeScope := uint64(22)
	dashboard, err = store.Dashboard(context.Background(), "native@example.com", &nativeScope, "7d", time.Now())
	if err != nil || dashboard.Summary.Questions == nil || *dashboard.Summary.Questions != 1 || dashboard.Coverage.ReadyHistories != 1 {
		t.Fatal("native history integration", dashboard, err)
	}
	verifyRecentQuestionCache(t, store, workspace, filepath.Join(directory, "session.jsonl"))
	verifyGrowthAndReports(t, store)
	verifyGrowthV2(t, store)
	verifyRankingAndSixTasks(t, store)
	verifyImmediateVisits(t, store)
	verifyPerformance(t, store)
	verifyBufferedActivity(t, store)
	verifyGrowthV4(t, store)
	verifySourceRollup(t, store)
}

func verifyRecentQuestionCache(t *testing.T, s *Store, workspace, path string) {
	t.Helper()
	ctx := context.Background()
	i := Identity{Scope: 22, Owner: "native@example.com", Instance: 901, Runtime: "openclaw", Mode: "lite"}
	raw := `{"type":"session","id":"recent-session"}
{"type":"message","id":"human","timestamp":"2026-09-16T00:00:00Z","message":{"role":"user","content":"recent fixture","__openclaw":{"senderIsOwner":true}}}`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 2; n++ {
		if err := s.cacheRecentQuestion(ctx, i, workspace); err != nil {
			t.Fatal(err)
		}
	}
	read := func(owner string, scope *uint64) map[uint64]RecentQuestion {
		q, e := s.recentQuestions(ctx, owner, scope, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		return q
	}
	if q := read(i.Owner, &i.Scope); len(q) != 1 || q[901].Text != "recent fixture" {
		t.Fatal("cache missing")
	}
	initial := read(i.Owner, &i.Scope)[901]
	if err := s.cacheRecentQuestion(ctx, i, workspace); err != nil {
		t.Fatal(err)
	}
	if repeated := read(i.Owner, &i.Scope)[901]; repeated != initial {
		t.Fatal("unchanged question was rewritten")
	}
	if err := os.WriteFile(path, []byte("not valid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.cacheRecentQuestion(ctx, i, workspace); err == nil {
		t.Fatal("invalid history must report failure")
	}
	if retained := read(i.Owner, &i.Scope)[901]; retained != initial {
		t.Fatal("failed read erased last successful question")
	}
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	public, err := s.Dashboard(ctx, i.Owner, &i.Scope, "7d", time.Now())
	if err != nil || len(public.RecentQuestions) != 0 {
		t.Fatal("usage-only API exposed conversation content", err)
	}
	portal, err := s.Dashboard(ctx, i.Owner, nil, "7d", time.Now())
	if err != nil || len(portal.RecentQuestions) != 1 {
		t.Fatal("verified portal excerpt missing", err)
	}
	wrong := uint64(999)
	if len(read(i.Owner, &wrong)) != 0 || len(read("other@example.com", nil)) != 0 {
		t.Fatal("cache identity leak")
	}
	// Current ownership, not the immutable statistics binding, authorizes text.
	if _, err := s.Session.SQL().Exec(`UPDATE instances SET owner='other@example.com' WHERE id=901`); err != nil {
		t.Fatal(err)
	}
	if len(read(i.Owner, nil)) != 0 || len(read("other@example.com", nil)) != 0 {
		t.Fatal("stale ownership exposed")
	}
	if _, err := s.Session.SQL().Exec(`UPDATE instances SET owner='native@example.com' WHERE id=901`); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"type":"session","id":"recent-session"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.cacheRecentQuestion(ctx, i, workspace); err != nil {
		t.Fatal(err)
	}
	if len(read(i.Owner, nil)) != 0 {
		t.Fatal("deleted question retained")
	}
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.cacheRecentQuestion(ctx, i, workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Session.SQL().Exec(`UPDATE northbound_owner_records SET expires_at=UTC_TIMESTAMP()-INTERVAL 1 SECOND WHERE record_type='recent_question'`); err != nil {
		t.Fatal(err)
	}
	if len(read(i.Owner, nil)) != 1 {
		t.Fatal("last successful question disappeared after cache expiry")
	}
	if _, err := s.Session.SQL().Exec(`UPDATE instances SET access_token='new-runtime-generation' WHERE id=901`); err != nil {
		t.Fatal(err)
	}
	if len(read(i.Owner, nil)) != 0 {
		t.Fatal("previous runtime generation excerpt exposed")
	}
	if err := s.cacheRecentQuestion(ctx, i, workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Session.SQL().Exec(`DELETE FROM instances WHERE id=901`); err != nil {
		t.Fatal(err)
	}
	if len(read(i.Owner, nil)) != 0 {
		t.Fatal("deleted instance exposed")
	}
}
