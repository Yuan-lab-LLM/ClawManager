package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
	"github.com/upper/db/v4/adapter/mysql"
)

func applyIdleStopFixtureSchema(t *testing.T, session db.Session) {
	t.Helper()
	for _, name := range []string{"096_add_instance_auto_stop.sql", "097_add_instance_auto_stop_scan.sql", "098_add_instance_auto_stop_policy_status.sql", "104_add_instance_auto_stop_seconds.sql", "109_add_model_activity_leases.sql"} {
		raw, err := os.ReadFile("../db/migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range strings.Split(string(raw), ";") {
			if strings.TrimSpace(statement) != "" {
				if _, err := session.SQL().Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

func idleStopMySQL(t *testing.T) db.Session {
	t.Helper()
	port := os.Getenv("TEST_IDLE_MYSQL_PORT")
	if port == "" {
		t.Skip("TEST_IDLE_MYSQL_PORT enables disposable localhost MySQL tests")
	}
	for _, c := range port {
		if c < '0' || c > '9' {
			t.Fatal("localhost port required")
		}
	}
	u := mysql.ConnectionURL{Host: "127.0.0.1:" + port, User: "root", Database: "mysql", Options: map[string]string{"parseTime": "true"}}
	root, err := mysql.Open(u)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("cm_idle_test_%d", time.Now().UnixNano())
	if _, err = root.SQL().Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	u.Database = name
	s, err := mysql.Open(u)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(); root.SQL().Exec("DROP DATABASE " + name); root.Close() })
	for _, query := range []string{
		`CREATE TABLE instances (id INT PRIMARY KEY,user_id INT NOT NULL,type VARCHAR(32),instance_mode VARCHAR(16),status VARCHAR(32),runtime_generation INT,started_at DATETIME(6),created_at DATETIME(6),updated_at DATETIME(6),stopped_at DATETIME(6) NULL,agent_bootstrap_token VARCHAR(255) NULL)`,
		`CREATE TABLE instance_agents (instance_id INT,status VARCHAR(32),session_token VARCHAR(255),session_expires_at DATETIME(6),updated_at DATETIME(6))`,
		`CREATE TABLE instance_desired_state (instance_id INT PRIMARY KEY,desired_power_state VARCHAR(32),created_at DATETIME(6),updated_at DATETIME(6))`,
		`CREATE TABLE instance_workspace_write_intents (instance_id INT)`,
		`CREATE TABLE instance_commands (instance_id INT,status VARCHAR(32))`,
		`CREATE TABLE gateway_runtime_tasks (instance_id INT,generation INT,finished BOOLEAN)`,
		`CREATE TABLE instance_runtime_bindings (instance_id INT PRIMARY KEY,runtime_pod_id BIGINT,gateway_id VARCHAR(128),generation INT,state VARCHAR(32))`,
		`INSERT INTO instances (id,user_id,type,instance_mode,status,runtime_generation,started_at,created_at,updated_at) VALUES (1,7,'openclaw','lite','running',2,UTC_TIMESTAMP()-INTERVAL 2 DAY,UTC_TIMESTAMP()-INTERVAL 2 DAY,UTC_TIMESTAMP()),(2,7,'opencode','lite','running',1,UTC_TIMESTAMP(),UTC_TIMESTAMP(),UTC_TIMESTAMP()),(3,7,'openclaw','pro','running',1,UTC_TIMESTAMP(),UTC_TIMESTAMP(),UTC_TIMESTAMP())`,
		`INSERT INTO instance_runtime_bindings VALUES (1,11,'gw-1-2',2,'running'),(2,12,'gw-2-1',1,'running')`,
	} {
		if _, err = s.SQL().Exec(query); err != nil {
			t.Fatal(err)
		}
	}

	for _, q := range []string{
		`ALTER TABLE instances ADD provisioning_operation_id VARCHAR(64) NULL, ADD workspace_path VARCHAR(1024) NULL`,
		`CREATE TABLE teams (id INT PRIMARY KEY,status VARCHAR(32),resource_scope VARCHAR(32))`,
		`CREATE TABLE team_members (id INT PRIMARY KEY,team_id INT,instance_id INT NULL,member_key VARCHAR(64),role VARCHAR(32),status VARCHAR(32))`,
		`CREATE TABLE team_tasks (id INT PRIMARY KEY,team_id INT,created_by INT NULL,status VARCHAR(32),finished_at DATETIME(6) NULL,payload_json LONGTEXT NULL)`,
		`CREATE TABLE team_work_items (team_id INT,status VARCHAR(32),superseded_by VARCHAR(255) NULL,root_task_id INT NULL)`,
		`CREATE TABLE team_event_outbox (team_id INT,status VARCHAR(32))`,
		`CREATE TABLE team_workspace_write_intents (team_id INT)`,
	} {
		if _, e := s.SQL().Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	rawTeam, e := os.ReadFile("../db/migrations/101_add_team_runtime_lifecycle.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, q := range strings.Split(string(rawTeam), ";") {
		if strings.TrimSpace(q) != "" {
			if _, e = s.SQL().Exec(q); e != nil {
				t.Fatal(e)
			}
		}
	}
	raw, err := os.ReadFile("../db/migrations/096_add_instance_auto_stop.sql")
	extra, extraErr := os.ReadFile("../db/migrations/097_add_instance_auto_stop_scan.sql")
	if extraErr != nil {
		t.Fatal(extraErr)
	}
	raw = append(raw, extra...)
	policyStatus, policyStatusErr := os.ReadFile("../db/migrations/098_add_instance_auto_stop_policy_status.sql")
	if policyStatusErr != nil {
		t.Fatal(policyStatusErr)
	}
	raw = append(raw, policyStatus...)
	seconds, secondsErr := os.ReadFile("../db/migrations/104_add_instance_auto_stop_seconds.sql")
	if secondsErr != nil {
		t.Fatal(secondsErr)
	}
	raw = append(raw, seconds...)
	leases, leaseErr := os.ReadFile("../db/migrations/109_add_model_activity_leases.sql")
	if leaseErr != nil {
		t.Fatal(leaseErr)
	}
	raw = append(raw, leases...)

	if err != nil {
		t.Fatal(err)
	}
	for _, query := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(query) != "" {
			if _, err = s.SQL().Exec(query); err != nil {
				t.Fatal(err)
			}
		}
	}

	return s
}

func TestAutoStopMySQLAdmissionAndFence(t *testing.T) {
	s := idleStopMySQL(t)
	r := NewInstanceAutoStopRepository(s)
	ctx := context.Background()
	if err := r.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r.BeginModelActivity(ctx, 8, 1, 2, "gw-1-2"); err == nil {
		t.Fatal("wrong owner accepted")
	}
	if _, err := r.BeginModelActivity(ctx, 7, 1, 1, "gw-1-1"); err == nil {
		t.Fatal("stale generation accepted")
	}
	key, err := r.BeginModelActivity(ctx, 7, 1, 2, "gw-1-2")
	if err != nil {
		t.Fatal(err)
	}
	a, err := r.GetActivity(ctx, 1)
	if err != nil || a.ActiveRequests != 1 || a.LastGatewayActivityAt == nil {
		t.Fatalf("activity=%+v err=%v", a, err)
	}
	if err = r.FinishModelActivity(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err = r.FinishModelActivity(ctx, key); err != nil {
		t.Fatal(err)
	}
	a, err = r.GetActivity(ctx, 1)
	if err != nil || a.ActiveRequests != 0 {
		t.Fatalf("activity=%+v err=%v", a, err)
	}
	if _, err = s.SQL().Exec(`UPDATE instance_model_activity SET monitoring_since=UTC_TIMESTAMP()-INTERVAL 2 DAY,last_gateway_activity_at=UTC_TIMESTAMP()-INTERVAL 2 DAY WHERE instance_id=1`); err != nil {
		t.Fatal(err)
	}
	a, _ = r.GetActivity(ctx, 1)
	settings, err := r.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	op := models.InstanceIdleStopOperation{InstanceID: 1, UserID: 7, Generation: 2, RuntimePodID: 11, GatewayID: "gw-1-2", PodUID: "pod-uid", Endpoint: "https://10.0.0.1", WorkspacePath: "/workspace", StorageIdentity: "pvc:test"}

	claimed, err := r.ClaimStop(ctx, settings.Version, a.ActivityVersion, op)
	if err != nil || claimed == nil {
		t.Fatalf("claim=%+v err=%v", claimed, err)
	}
	if _, err = r.BeginModelActivity(ctx, 7, 1, 2, "gw-1-2"); err == nil {
		t.Fatal("model request entered stopping instance")
	}
	if err = r.CompleteStop(ctx, *claimed); err == nil {
		t.Fatal("completed before binding release")
	}
	if err = r.ConfirmTeardown(ctx, *claimed); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SQL().Exec("DELETE FROM instance_runtime_bindings WHERE instance_id=1"); err != nil {
		t.Fatal(err)
	}
	if err = r.CompleteStop(ctx, *claimed); err != nil {
		t.Fatal(err)
	}
	a, _ = r.GetActivity(ctx, 1)
	if a.StoppedReason != "idle_timeout" || a.StopOperationID != "" {
		t.Fatalf("completed=%+v", a)
	}
	if key, err = r.BeginModelActivity(ctx, 7, 3, 1, ""); err != nil || key != "" {
		t.Fatalf("Pro modified: %q %v", key, err)
	}
}

func TestAutoStopMySQLConcurrentAdmissionWinsOverStaleCandidate(t *testing.T) {
	s := idleStopMySQL(t)
	r := NewInstanceAutoStopRepository(s)
	ctx := context.Background()
	if err := r.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	s.SQL().Exec(`UPDATE instance_model_activity SET monitoring_since=UTC_TIMESTAMP()-INTERVAL 2 DAY,last_gateway_activity_at=UTC_TIMESTAMP()-INTERVAL 2 DAY WHERE instance_id=1`)
	a, _ := r.GetActivity(ctx, 1)
	cfg, _ := r.Settings(ctx)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key, e := r.BeginModelActivity(ctx, 7, 1, 2, "gw-1-2")
			if e == nil {
				e = r.FinishModelActivity(ctx, key)
			}
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	op := models.InstanceIdleStopOperation{InstanceID: 1, UserID: 7, Generation: 2, RuntimePodID: 11, GatewayID: "gw-1-2"}
	if _, e := r.ClaimStop(ctx, cfg.Version, a.ActivityVersion, op); e == nil {
		t.Fatal("stale candidate stopped after model use")
	}
	state, e := r.GetActivity(ctx, 1)
	if e != nil || state.ActiveRequests != 0 || state.ActivityVersion <= a.ActivityVersion {
		t.Fatalf("state=%+v err=%v", state, e)
	}
}

func TestAutoStopMySQLPolicyClaimsAndTeardownRecovery(t *testing.T) {
	s := idleStopMySQL(t)
	r := NewInstanceAutoStopRepository(s)
	ctx := t.Context()
	if err := r.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	cfg, err := r.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cfg.IdleTimeoutDays = 2
	saved, err := r.SaveSettings(ctx, *cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.SaveSettings(ctx, *cfg); err == nil {
		t.Fatal("stale policy overwrote newer version")
	}
	due := time.Now().UTC().Add(-48 * time.Hour)
	if _, err = s.SQL().Exec("UPDATE instance_auto_stop_settings SET next_check_at=?", due); err != nil {
		t.Fatal(err)
	}
	first, err := r.ClaimCheck(ctx, time.Now().UTC())
	if err != nil || first == nil {
		t.Fatalf("claim=%v %v", first, err)
	}
	second, err := r.ClaimCheck(ctx, time.Now().UTC())
	if err != nil || second != nil {
		t.Fatalf("duplicate daily claim=%v %v", second, err)
	}
	if err = r.SetDisabled(ctx, 1, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SQL().Exec("UPDATE instance_model_activity SET monitoring_since=UTC_TIMESTAMP()-INTERVAL 3 DAY"); err != nil {
		t.Fatal(err)
	}
	candidates, err := r.Candidates(ctx, time.Now().UTC(), 2, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range candidates {
		if a.InstanceID == 1 {
			t.Fatal("keep-running selected")
		}
	}
	if err = r.SetDisabled(ctx, 1, false); err != nil {
		t.Fatal(err)
	}
	a, _ := r.GetActivity(ctx, 1)
	op, err := r.ClaimStop(ctx, saved.Version, a.ActivityVersion, models.InstanceIdleStopOperation{InstanceID: 1, UserID: 7, Generation: 2, RuntimePodID: 11, GatewayID: "gw-1-2", PodUID: "pod", Endpoint: "https://runtime"})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.ConfirmTeardown(ctx, *op); err != nil {
		t.Fatal(err)
	}
	op.ErrorMessage = "binding release interrupted"
	if err = r.FailStop(ctx, *op); err != nil {
		t.Fatal(err)
	}
	pending, err := r.PendingStops(ctx)
	if err != nil || len(pending) != 1 || pending[0].Phase != "teardown_confirmed" {
		t.Fatalf("lost teardown proof: %+v %v", pending, err)
	}
	if err = r.BeginManualStart(ctx, 1, time.Now().Add(time.Minute)); err == nil {
		t.Fatal("start entered pending stop")
	}
}

func TestAutoStopMySQLStaleHeartbeatCannotReviveStoppedInstance(t *testing.T) {
	s := idleStopMySQL(t)
	r := NewInstanceAutoStopRepository(s)
	ctx := t.Context()
	if err := r.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SQL().Exec("UPDATE instances SET status='stopped' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if err := r.RecordManualStop(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := checkAutoStopRuntimeState(ctx, s, 1, "running", 2); err == nil {
		t.Fatal("stale heartbeat revived manual stopped instance")
	}
	if err := checkAutoStopRuntimeState(ctx, s, 1, "creating", 3); err == nil {
		t.Fatal("background retry bypassed manual start")
	}
	if err := r.BeginManualStart(ctx, 1, time.Now().Add(120*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := checkAutoStopRuntimeState(ctx, s, 1, "creating", 3); err != nil {
		t.Fatal(err)
	}
	if err := checkAutoStopRuntimeState(ctx, s, 1, "running", 2); err == nil {
		t.Fatal("same generation heartbeat bypassed start")
	}
}

func TestAutoStopMySQLScanCrashResumesCursorAndFinishesSchedule(t *testing.T) {
	s := idleStopMySQL(t)
	r := NewInstanceAutoStopRepository(s)
	ctx := t.Context()
	cfg, err := r.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	due := time.Now().UTC().Add(-time.Hour)
	if _, err = s.SQL().Exec("UPDATE instance_auto_stop_settings SET next_check_at=?", due); err != nil {
		t.Fatal(err)
	}
	first, err := r.ClaimCheck(ctx, time.Now().UTC())
	if err != nil || first == nil {
		t.Fatalf("first claim %v %v", first, err)
	}
	if err = r.CheckpointScan(ctx, *first, 17); err != nil {
		t.Fatal(err)
	}
	// Simulate process loss. Persisted due time must not move until completion.
	persisted, err := r.Settings(ctx)
	if err != nil || persisted.NextCheckAt.After(time.Now()) {
		t.Fatalf("unfinished scan advanced schedule: %+v %v", persisted, err)
	}
	if _, err = s.SQL().Exec("UPDATE instance_auto_stop_settings SET scan_lease_until=UTC_TIMESTAMP()-INTERVAL 1 SECOND"); err != nil {
		t.Fatal(err)
	}
	replacement := NewInstanceAutoStopRepository(s)
	second, err := replacement.ClaimCheck(ctx, time.Now().UTC())
	if err != nil || second == nil || second.ScanCursor != 17 || second.ScanID == first.ScanID {
		t.Fatalf("scan did not resume: %+v %v", second, err)
	}
	if err = r.CheckpointScan(ctx, *first, 99); err == nil {
		t.Fatal("stale controller retained scan authority")
	}
	if err = replacement.CompleteScan(ctx, *second); err != nil {
		t.Fatal(err)
	}
	persisted, err = r.Settings(ctx)
	if err != nil || !persisted.NextCheckAt.After(time.Now()) || persisted.ScanID != "" || persisted.ScanCursor != 0 {
		t.Fatalf("finished scan not scheduled: %+v %v", persisted, err)
	}
	if persisted.Version != cfg.Version {
		t.Fatal("scanner changed administrator config version")
	}
}

// Source-only regression; not executed locally per user instruction.
func TestAutoStopRetryTouchRotatesPendingOrderWithoutDroppingProof(t *testing.T) {
	s := idleStopMySQL(t)
	r := NewInstanceAutoStopRepository(s)
	ctx := t.Context()
	for _, id := range []string{"blocked", "later"} {
		if _, err := s.SQL().Exec(`INSERT INTO instance_idle_stop_operations(id,instance_id,user_id,generation,runtime_pod_id,gateway_id,pod_uid,endpoint,workspace_path,storage_identity,phase,created_at,updated_at) VALUES(?,1,7,2,11,'gw-1-2','pod','','','','teardown_confirmed',UTC_TIMESTAMP()-INTERVAL 2 DAY,UTC_TIMESTAMP()-INTERVAL 1 DAY)`, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.TouchStopRetry(ctx, "blocked"); err != nil {
		t.Fatal(err)
	}
	pending, err := r.PendingStops(ctx)
	if err != nil || len(pending) != 2 || pending[0].ID != "later" || pending[1].Phase != "teardown_confirmed" {
		t.Fatalf("blocked source starves later work or loses proof: %+v %v", pending, err)
	}
}
