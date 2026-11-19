package historymaintenance

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	_ "github.com/go-sql-driver/mysql"
	"os"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) (*sql.DB, *sql.DB) {
	t.Helper()
	port := os.Getenv("TEST_DATABASE_MYSQL_PORT")
	if port == "" {
		t.Skip("disposable marker MySQL required")
	}
	if port != "33317" {
		t.Fatal("dedicated fixture port33317 required")
	}
	root, err := sql.Open("mysql", "root@tcp(127.0.0.1:"+port+")/mysql?parseTime=true&multiStatements=true")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	var marker int
	if err = root.QueryRow("SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='cm_database_test_marker'").Scan(&marker); err != nil || marker != 1 {
		t.Fatalf("marker: %d %v", marker, err)
	}
	random := make([]byte, 16)
	rand.Read(random)
	suffix := hex.EncodeToString(random)
	create := func(prefix string) *sql.DB {
		name := prefix + suffix
		if _, err = root.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
			t.Fatal(err)
		}
		db, err := sql.Open("mysql", "root@tcp(127.0.0.1:"+port+")/"+name+"?parseTime=true&multiStatements=true")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close(); root.Exec("DROP DATABASE `" + name + "`") })
		return db
	}
	db := create("cm_history_test_")
	restore := create("cm_history_restore_")

	migration, err := os.ReadFile("../db/migrations/114_add_history_maintenance.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{
		`CREATE TABLE observation_invocation_pending(id BIGINT PRIMARY KEY)`,
		`CREATE TABLE observation_audit_pending(id BIGINT PRIMARY KEY)`,
		`CREATE TABLE observation_cost_pending(id BIGINT PRIMARY KEY)`,
		`CREATE TABLE observation_chat_pending(id BIGINT PRIMARY KEY)`,
		`CREATE TABLE observation_risk_pending(id BIGINT PRIMARY KEY)`,
		`CREATE TABLE model_invocations(id BIGINT PRIMARY KEY,user_id BIGINT,instance_id BIGINT,trace_id VARCHAR(100),session_id VARCHAR(100),status VARCHAR(30),completed_at DATETIME(6),created_at DATETIME(6),request_payload LONGTEXT)`,
		`CREATE TABLE audit_events(id BIGINT PRIMARY KEY,user_id BIGINT,instance_id BIGINT,invocation_id BIGINT,trace_id VARCHAR(100),session_id VARCHAR(100),created_at DATETIME(6),details LONGTEXT)`,
		`CREATE TABLE chat_messages LIKE audit_events`, `CREATE TABLE risk_hits LIKE audit_events`,
		`CREATE TABLE teams(id BIGINT PRIMARY KEY,user_id BIGINT,status VARCHAR(30))`,
		`CREATE TABLE team_tasks(id BIGINT PRIMARY KEY,team_id BIGINT,status VARCHAR(30),finished_at DATETIME(6),created_at DATETIME(6))`,
		`CREATE TABLE team_events(id BIGINT PRIMARY KEY,team_id BIGINT,task_id BIGINT,created_at DATETIME(6))`,
		`CREATE TABLE team_work_items(id BIGINT PRIMARY KEY,root_task_id BIGINT,status VARCHAR(30),finished_at DATETIME(6))`,
		`CREATE TABLE team_workflow_phases(id BIGINT PRIMARY KEY,root_task_id BIGINT,status VARCHAR(30),completed_at DATETIME(6))`,
		`CREATE TABLE instance_model_activity_requests(request_id VARCHAR(100) PRIMARY KEY,finished_at DATETIME(6),lease_expires_at DATETIME(6),completion_reason VARCHAR(32))`,
		`CREATE TABLE team_outbox_receipt_closures(outbox_id BIGINT PRIMARY KEY,team_id BIGINT,task_id BIGINT,source_window_sha256 CHAR(64),closed_before DATETIME(6),closed_at DATETIME(6),redis_reclaimed_at DATETIME(6))`,
		`CREATE TABLE team_event_outbox(id BIGINT PRIMARY KEY,team_id BIGINT,source_event_id VARCHAR(255),destination VARCHAR(255),message_id VARCHAR(255),payload_json LONGTEXT,status VARCHAR(30),delivered_at DATETIME(6),created_at DATETIME(6))`,
		`CREATE TABLE history_outbox_cancellations(team_id BIGINT,destination VARCHAR(255),message_id VARCHAR(255),source_event_id VARCHAR(255),payload_sha256 CHAR(64),cancelled_at DATETIME(6),reason VARCHAR(255),closure_sha256 CHAR(64),PRIMARY KEY(team_id,destination,message_id))`,
	} {
		if _, err = db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	return db, restore
}
func exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}
func count(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func TestMySQLArchiveRestoreGateAndStableReader(t *testing.T) {
	db, restore := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	old := now.AddDate(0, 0, -100)
	exec(t, db, "INSERT INTO model_invocations VALUES(1,12,3,'trace','session','completed',?,?,?), (2,12,3,'active','session','pending',NULL,?,?)", old, old, `{"secret":"kept"}`, old, `pending`)
	store := &memoryArchive{objects: map[string][]byte{}}
	svc := New(db, store, nil)
	if _, err := svc.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "model_invocations") != 2 {
		t.Fatal("source removed without restore proof")
	}
	r, err := svc.Get(ctx, "model_invocations", 1, 12, false)
	if err != nil || !strings.Contains(string(r.Payload), "secret") {
		t.Fatalf("reader %s %v", r.Payload, err)
	}
	if _, err = svc.Get(ctx, "model_invocations", 1, 13, false); err == nil {
		t.Fatal("owner boundary bypassed")
	}
	if _, err = svc.Get(ctx, "model_invocations", 1, 0, false); err == nil {
		t.Fatal("zero owner implicit admin")
	}
	if err = svc.VerifyRestore(ctx, "model_invocations", 1, restore, "test-isolated-restore"); err != nil {
		t.Fatal(err)
	}
	exec(t, db, "UPDATE history_archive_records SET summary_verified_at=UTC_TIMESTAMP(6),summary_sha256=payload_sha256")
	month, _ := MonthlyTable("model_invocations", old)
	if _, complete, e := svc.VerifyMonth(ctx, month, 0); e != nil || !complete {
		t.Fatalf("monthcutover %v %v", complete, e)
	}
	// Cursor completed its previous pass; reset cycle before retention recheck.
	exec(t, db, "UPDATE history_maintenance_cursors SET after_id=0")
	if _, err = svc.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "model_invocations") != 1 {
		t.Fatal("verified completed source not removed")
	}
	r, err = svc.Get(ctx, "model_invocations", 1, 12, false)
	if err != nil || r.Status != "archived" {
		t.Fatalf("historical record unavailable: %+v %v", r, err)
	}
	rows, err := svc.List(ctx, Query{Kind: "model_invocations", OwnerID: 12, Since: old.Add(-time.Hour), Until: old.Add(time.Hour), Limit: 200})
	if err != nil || len(rows) != 1 {
		t.Fatalf("bounded cross-month query %d %v", len(rows), err)
	}
	store.corrupt = true
	if _, err = svc.Get(ctx, "model_invocations", 1, 12, false); err == nil {
		t.Fatal("corrupt object returned")
	}
}
func TestMySQLBoundedMaintenanceAndOutboxReplaySafety(t *testing.T) {
	db, _ := fixture(t)
	ctx := context.Background()
	old := time.Now().UTC().AddDate(0, 0, -8)
	for i := 0; i < 510; i++ {
		exec(t, db, "INSERT INTO instance_model_activity_requests VALUES(?,?,?,'completed')", i, old, old)
	}
	exec(t, db, "INSERT INTO instance_model_activity_requests VALUES('live',NULL,?,'')", old)
	exec(t, db, "INSERT INTO instance_model_activity_requests VALUES('ambiguous',?,?, 'lease_expired')", old, old)
	exec(t, db, "INSERT INTO teams VALUES(1,12,'deleted')")
	exec(t, db, "INSERT INTO team_event_outbox VALUES(1,1,'e','dest','m','{}','delivered',?,?), (2,1,'e2','dest','m2','{}','pending',NULL,?)", old, old, old)
	svc := New(db, &memoryArchive{objects: map[string][]byte{}}, nil)
	result, err := svc.Tick(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Processed > 500 {
		t.Fatal("batch exceeds500")
	}
	if count(t, db, "team_event_outbox") != 2 {
		t.Fatal("outbox removed without closure")
	}
	exec(t, db, "INSERT INTO history_replay_closures VALUES(1,?,TRUE,TRUE,TRUE,?,?)", time.Now().UTC(), strings.Repeat("a", 64), time.Now().UTC())
	if _, err = svc.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "instance_model_activity_requests") != 2 {
		t.Fatal("active or ambiguous requests removed")
	}
	if count(t, db, "team_event_outbox") != 1 || count(t, db, "history_outbox_receipts") != 1 {
		t.Fatal("closed delivered outbox not retained as idempotency receipt")
	}
}
func TestMySQLLeaseAndBackupPause(t *testing.T) {
	db, _ := fixture(t)
	svc := New(db, &memoryArchive{objects: map[string][]byte{}}, func(context.Context) (func(), error) { return nil, ErrBackupPaused })
	if _, err := svc.Tick(context.Background()); err != ErrBackupPaused {
		t.Fatalf("gate ignored %v", err)
	}
	svc.Gate = nil
	exec(t, db, "INSERT INTO history_maintenance_leases(name,holder,lease_until) VALUES('history','other',?)", time.Now().UTC().Add(time.Hour))
	result, err := svc.Tick(context.Background())
	if err != nil || !result.LeaseBusy {
		t.Fatalf("live lease stolen %+v %v", result, err)
	}
}

func TestMySQLAutomaticProofAndCutoverRecoversAfterCorruption(t *testing.T) {
	db, target := fixture(t)
	old := time.Now().UTC().AddDate(0, 0, -100)
	exec(t, db, "INSERT INTO model_invocations VALUES(1,12,3,'automatic','session','completed',?,?,?)", old, old, `secret`)
	store := &memoryArchive{objects: map[string][]byte{}}
	svc := New(db, store, nil)
	if _, err := svc.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	store.corrupt = true
	if _, err := svc.ProvePending(context.Background(), target, 500); err == nil {
		t.Fatal("corrupt proof succeeded")
	}
	var at sql.NullTime
	if err := db.QueryRow("SELECT restore_verified_at FROM history_archive_records").Scan(&at); err != nil || at.Valid {
		t.Fatal("failure granted restore proof", err)
	}
	store.corrupt = false
	n, err := svc.ProvePending(context.Background(), target, 500)
	if err != nil || n != 1 {
		t.Fatalf("automatic proof %d %v", n, err)
	}
	if _, err = svc.CutoverPending(context.Background(), 500); err != nil {
		t.Fatal(err)
	}
	exec(t, db, "UPDATE history_archive_records SET summary_verified_at=UTC_TIMESTAMP(6),summary_sha256=payload_sha256")
	exec(t, db, "UPDATE history_maintenance_cursors SET after_id=0")
	if _, err = svc.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "model_invocations") != 0 {
		t.Fatal("proved source never reclaimed")
	}
	if count(t, target, "history_restore_rows") != 0 {
		t.Fatal("temporary restore payload not reclaimed")
	}
}

func TestMySQLDedicatedRestoreCredentialCannotReadBusinessSource(t *testing.T) {
	db, target := fixture(t)
	var targetName string
	if err := target.QueryRow("SELECT DATABASE()").Scan(&targetName); err != nil {
		t.Fatal(err)
	}
	username := "cmhr_" + targetName[len(targetName)-16:]
	password := "historyfixturepassword"
	exec(t, db, "CREATE USER '"+username+"'@'%' IDENTIFIED BY '"+password+"'")
	t.Cleanup(func() { db.Exec("DROP USER '" + username + "'@'%'") })
	exec(t, db, "GRANT SELECT,INSERT,UPDATE,DELETE,CREATE ON `"+targetName+"`.* TO '"+username+"'@'%'")
	path := t.TempDir() + "/restore.dsn"
	if err := os.WriteFile(path, []byte(username+":"+password+"@tcp(127.0.0.1:33317)/"+targetName+"?parseTime=true"), 0600); err != nil {
		t.Fatal(err)
	}
	svc := New(db, &memoryArchive{objects: map[string][]byte{}}, nil)
	isolated, err := svc.OpenRestoreTarget(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer isolated.Close()
	old := time.Now().UTC().AddDate(0, 0, -100)
	exec(t, db, "INSERT INTO model_invocations VALUES(1,12,3,'leastpriv','session','completed',?,?,?)", old, old, `secret`)
	if _, err = svc.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n, err := svc.ProvePending(context.Background(), isolated, 500); err != nil || n != 1 {
		t.Fatalf("leastpriv restore %d %v", n, err)
	}
	exec(t, db, "GRANT SELECT ON `"+databaseName(t, db)+"`.* TO '"+username+"'@'%'")
	if err = svc.ValidateRestoreTarget(context.Background(), isolated); err == nil {
		t.Fatal("source-readable restore credential accepted")
	}
}
func databaseName(t *testing.T, db *sql.DB) string {
	t.Helper()
	var name string
	if err := db.QueryRow("SELECT DATABASE()").Scan(&name); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestMySQLPointerOnlyBodyRestoreCannotGrantProof(t *testing.T) {
	db, target := fixture(t)
	exec(t, db, "ALTER TABLE model_invocations ADD COLUMN request_body_ref JSON NULL")
	old := time.Now().UTC().AddDate(0, 0, -100)
	exec(t, db, `INSERT INTO model_invocations VALUES(1,12,3,'ref-only','session','completed',?,?,NULL,?)`, old, old, `{"object_key":"unrestored","owner":"user:12","sha256":"unavailable"}`)
	svc := New(db, &memoryArchive{objects: map[string][]byte{}}, nil)
	if _, err := svc.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ProvePending(context.Background(), target, 500); err == nil {
		t.Fatal("body reference restored without proving body bytes")
	}
	var verified sql.NullTime
	if err := db.QueryRow("SELECT restore_verified_at FROM history_archive_records").Scan(&verified); err != nil || verified.Valid {
		t.Fatal("pointer-only restore created proof", err)
	}
}

func TestMySQLArchivedIdentityCannotChangeOwners(t *testing.T) {
	db, _ := fixture(t)
	old := time.Now().UTC().AddDate(0, 0, -100)
	exec(t, db, "INSERT INTO model_invocations VALUES(1,12,3,'owner','session','completed',?,?,?)", old, old, `original`)
	svc := New(db, &memoryArchive{objects: map[string][]byte{}}, nil)
	ctx := context.Background()
	if _, _, err := svc.Backfill(ctx, "model_invocations", old.Add(-time.Hour), old.Add(time.Hour), 0, 500); err != nil {
		t.Fatal(err)
	}
	exec(t, db, "UPDATE model_invocations SET user_id=13,request_payload='replacement'")
	if _, _, err := svc.Backfill(ctx, "model_invocations", old.Add(-time.Hour), old.Add(time.Hour), 0, 500); err == nil {
		t.Fatal("stable history ID reassigned across owners")
	}
	r, err := svc.Get(ctx, "model_invocations", 1, 12, false)
	if err != nil || !strings.Contains(string(r.Payload), "original") {
		t.Fatal("original owner archive overwritten", err)
	}
}
