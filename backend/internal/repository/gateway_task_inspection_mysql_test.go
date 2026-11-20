package repository

import (
	"errors"
	"os"
	"strings"
	"testing"

	"clawreef/internal/models"
	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/upper/db/v4/adapter/mysql"
)

// Requires an empty disposable schema, never an application database. This
// focused test also runs in the runtime-task-mysql CI job (not silently skipped).
func TestRuntimeTaskInspectionMySQL(t *testing.T) {
	dsn := os.Getenv("CLAWMANAGER_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("CLAWMANAGER_TEST_MYSQL_DSN not configured")
	}
	cfg, err := drivermysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(cfg.DBName, "clawmanager_test_") {
		t.Fatal("requires a disposable clawmanager_test_ database")
	}
	sess, err := mysql.Open(mysql.ConnectionURL{Host: cfg.Addr, User: cfg.User, Password: cfg.Passwd, Database: cfg.DBName, Options: map[string]string{"parseTime": "true", "charset": "utf8mb4"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	row, err := sess.SQL().QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=?", cfg.DBName)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("refusing non-empty database")
	}
	// Exact tables below are created only after the empty-schema check.
	t.Cleanup(func() {
		for _, table := range []string{"gateway_runtime_task_models", "gateway_runtime_tasks", "instances", "users"} {
			_, _ = sess.SQL().Exec("DROP TABLE IF EXISTS " + table)
		}
	})
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE TABLE users (id INT PRIMARY KEY,is_active BOOLEAN NOT NULL) ENGINE=InnoDB")
	exec("CREATE TABLE instances (id INT PRIMARY KEY,user_id INT NOT NULL,runtime_generation INT NOT NULL,instance_mode VARCHAR(16) NOT NULL DEFAULT 'pro',status VARCHAR(32) NOT NULL DEFAULT 'running') ENGINE=InnoDB")
	exec("INSERT INTO users VALUES (1,TRUE),(2,TRUE)")
	exec("INSERT INTO instances(id,user_id,runtime_generation) VALUES (10,1,5),(11,2,5)")
	raw, err := os.ReadFile("../db/migrations/063_add_gateway_runtime_tasks.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(statement) != "" {
			exec(statement)
		}
	}
	r := &gatewayTaskRepository{sess: sess}
	scope := models.GatewayTaskIdentity{InstanceID: 10, UserID: 1, Generation: 5}
	identity := func(generation int, id string) models.GatewayTaskIdentity {
		task := scope
		task.Generation = generation
		task.SessionID = "session"
		task.TaskID = id
		return task
	}
	seed := func(task models.GatewayTaskIdentity, state models.GatewayTaskState) {
		t.Helper()
		key, err := GatewayTaskKey(task)
		if err != nil {
			t.Fatal(err)
		}
		exec("INSERT INTO gateway_runtime_tasks(task_key,instance_id,owner_user_id,generation,session_id,task_id,revoked,finished,runtime_acknowledged) VALUES(?,?,?,?,?,?,?,?,?)", key, task.InstanceID, task.UserID, task.Generation, task.SessionID, task.TaskID, state.Revoked, state.Finished, state.RuntimeAcknowledged)
	}
	old := identity(4, "old")
	cancelled := identity(4, "cancelled")
	pending := identity(4, "pending")
	current := identity(5, "current")
	missing := identity(4, "missing")
	seed(old, models.GatewayTaskState{Finished: true})
	seed(cancelled, models.GatewayTaskState{Revoked: true, Finished: true, RuntimeAcknowledged: true})
	seed(pending, models.GatewayTaskState{Revoked: true})
	seed(current, models.GatewayTaskState{Revoked: true})
	foreign := missing
	foreign.UserID = 2
	seed(foreign, models.GatewayTaskState{Finished: true})
	foreign.InstanceID = 11
	foreign.TaskID = "other-instance"
	seed(foreign, models.GatewayTaskState{Finished: true})
	requested := []models.GatewayTaskIdentity{pending, old, missing, cancelled, foreign}
	snapshots, err := r.Inspect(t.Context(), scope, requested)
	if err != nil || len(snapshots) != len(requested) {
		t.Fatalf("inspect: %v %v", snapshots, err)
	}
	if snapshots[0].State == nil || !snapshots[0].State.Revoked || snapshots[0].State.Finished || snapshots[0].State.RuntimeAcknowledged {
		t.Fatal("pending task was inferred completed")
	}
	if snapshots[1].State == nil || !snapshots[1].State.Finished || snapshots[1].State.RuntimeAcknowledged {
		t.Fatal("normal finish became cancellation")
	}
	if snapshots[2].State != nil || snapshots[4].State != nil {
		t.Fatal("leaked another owner's or instance's task")
	}
	if snapshots[3].State == nil || !snapshots[3].State.RuntimeAcknowledged {
		t.Fatal("lost historical cancellation state")
	}
	row, err = sess.SQL().QueryRow("SELECT COUNT(*) FROM gateway_runtime_tasks")
	if err != nil {
		t.Fatal(err)
	}
	if err := row.Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 6 {
		t.Fatal("inspection created task rows")
	}
	if err := r.Finish(t.Context(), missing, true); !errors.Is(err, models.ErrModelUnavailable) {
		t.Fatalf("unknown task accepted: %v", err)
	}
	deleted := missing
	deleted.InstanceID = 99
	if err := r.Finish(t.Context(), deleted, true); !errors.Is(err, models.ErrModelUnavailable) {
		t.Fatalf("deleted instance accepted: %v", err)
	}
	// Even a settled old-generation receipt is not replayed by this API.
	if err := r.Finish(t.Context(), pending, true); !errors.Is(err, models.ErrModelUnavailable) {
		t.Fatalf("old generation accepted: %v", err)
	}
	if err := r.Finish(t.Context(), current, false); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := r.Finish(t.Context(), current, true); err != nil {
			t.Fatalf("idempotent cancellation: %v", err)
		}
	}
	snapshots, err = r.Inspect(t.Context(), scope, []models.GatewayTaskIdentity{current})
	if err != nil || !snapshots[0].State.RuntimeAcknowledged {
		t.Fatal("current cancellation not recorded", err)
	}
	// Simulate the restart between successful authentication/inspection and write.
	race := identity(5, "race")
	seed(race, models.GatewayTaskState{Revoked: true})
	if _, err := r.Inspect(t.Context(), scope, []models.GatewayTaskIdentity{race}); err != nil {
		t.Fatal(err)
	}
	exec("UPDATE instances SET runtime_generation=6 WHERE id=10")
	if err := r.Finish(t.Context(), race, true); !errors.Is(err, models.ErrModelUnavailable) {
		t.Fatal("stale write passed generation fence", err)
	}
	if _, err := r.Inspect(t.Context(), scope, []models.GatewayTaskIdentity{race}); !errors.Is(err, models.ErrModelUnavailable) {
		t.Fatal("stale reader treated scope mismatch as missing", err)
	}
	scope.Generation = 6
	newTask := race
	newTask.Generation = 6
	seed(newTask, models.GatewayTaskState{})
	snapshots, err = r.Inspect(t.Context(), scope, []models.GatewayTaskIdentity{race, newTask})
	if err != nil {
		t.Fatal(err)
	}
	if snapshots[0].State.Finished || snapshots[0].State.RuntimeAcknowledged || snapshots[1].State.Finished || snapshots[1].State.RuntimeAcknowledged {
		t.Fatal("old receipt affected either execution generation")
	}
	// Ownership transfer and inactive users are fenced at the write as well.
	exec("UPDATE instances SET user_id=2 WHERE id=10")
	if err := r.Finish(t.Context(), newTask, true); !errors.Is(err, models.ErrModelUnavailable) {
		t.Fatal("previous owner finished a task", err)
	}
	if _, err := r.Inspect(t.Context(), scope, []models.GatewayTaskIdentity{race}); !errors.Is(err, models.ErrModelUnavailable) {
		t.Fatal("previous owner inspected task", err)
	}
	scope.UserID = 2
	snapshots, err = r.Inspect(t.Context(), scope, []models.GatewayTaskIdentity{race})
	if err != nil || snapshots[0].State != nil {
		t.Fatal("new owner saw old owner's history", err)
	}
	exec("UPDATE instances SET user_id=1 WHERE id=10")
	scope.UserID = 1
	exec("UPDATE users SET is_active=FALSE WHERE id=1")
	if err := r.Finish(t.Context(), newTask, true); !errors.Is(err, models.ErrModelUnavailable) {
		t.Fatal("disabled user finished task", err)
	}
	if _, err := r.Inspect(t.Context(), scope, []models.GatewayTaskIdentity{newTask}); !errors.Is(err, models.ErrModelUnavailable) {
		t.Fatal("disabled user inspected task", err)
	}
	exec("UPDATE users SET is_active=TRUE WHERE id=1")
	snapshots, err = r.Inspect(t.Context(), scope, []models.GatewayTaskIdentity{newTask})
	if err != nil || snapshots[0].State.Finished {
		t.Fatal("failed fenced write changed state", err)
	}
}
