package repository

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/upper/db/v4/adapter/mysql"
)

// Opt-in integration test. Requires an EMPTY, DISPOSABLE database whose name
// begins with clawmanager_test_. Never runs against the application database.
func TestRuntimeTaskMySQLRevocationLifecycle(t *testing.T) {
	dsn := os.Getenv("CLAWMANAGER_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("CLAWMANAGER_TEST_MYSQL_DSN not configured; requires disposable MySQL")
	}
	cfg, err := drivermysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid test DSN")
	}
	if !strings.HasPrefix(cfg.DBName, "clawmanager_test_") {
		t.Fatal("refusing non-test database")
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
	if err = row.Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("refusing non-empty test database")
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	// All tables below were created by this test after the empty-schema check.
	t.Cleanup(func() {
		for _, table := range []string{"instance_workspace_write_intents", "gateway_runtime_upgrade_stops", "instance_commands", "instance_desired_state", "instance_runtime_bindings", "runtime_pods", "gateway_runtime_upgrades", "gateway_runtime_maintenance", "gateway_runtime_protocols", "gateway_runtime_task_models", "gateway_runtime_tasks", "llm_model_user_grants", "llm_models", "instances", "users"} {
			_, _ = sess.SQL().Exec("DROP TABLE IF EXISTS " + table)
		}
	})
	exec("CREATE TABLE users (id INT PRIMARY KEY,is_active BOOLEAN NOT NULL) ENGINE=InnoDB")
	exec("CREATE TABLE instances (id INT PRIMARY KEY,user_id INT NOT NULL,runtime_generation INT NOT NULL) ENGINE=InnoDB")
	exec("INSERT INTO users VALUES (1,TRUE),(2,TRUE)")
	exec("INSERT INTO instances VALUES (10,1,5)")
	writeSchema, schemaErr := os.ReadFile("../db/migrations/068_add_workspace_write_intents.sql")
	if schemaErr != nil {
		t.Fatal(schemaErr)
	}
	for _, statement := range strings.Split(string(writeSchema), ";") {
		if strings.TrimSpace(statement) != "" {
			exec(statement)
		}
	}
	commandSchema, schemaErr := os.ReadFile("../db/migrations/007_add_instance_agent_control_plane.sql")
	if schemaErr != nil {
		t.Fatal(schemaErr)
	}
	for _, statement := range strings.Split(string(commandSchema), ";") {
		if strings.Contains(statement, "CREATE TABLE IF NOT EXISTS instance_commands (") || strings.Contains(statement, "CREATE TABLE IF NOT EXISTS instance_desired_state (") {
			exec(statement)
		}
	}
	modelRepo := NewLLMModelRepository(sess)
	exec(`ALTER TABLE llm_models ADD COLUMN visibility_scope VARCHAR(24) NOT NULL DEFAULT 'all',
 ADD COLUMN access_policy_version INT NOT NULL DEFAULT 1, ADD COLUMN supports_tools BOOLEAN NOT NULL DEFAULT FALSE,
 ADD COLUMN supports_vision BOOLEAN NOT NULL DEFAULT FALSE, ADD COLUMN tools_support_override BOOLEAN NULL,
 ADD COLUMN vision_support_override BOOLEAN NULL,
 ADD COLUMN model_kind VARCHAR(32) NOT NULL DEFAULT 'chat',
 ADD COLUMN image_generation_enabled BOOLEAN NOT NULL DEFAULT FALSE,
 ADD COLUMN image_edit_enabled BOOLEAN NOT NULL DEFAULT FALSE,
 ADD COLUMN capability_source VARCHAR(100) NOT NULL DEFAULT '', ADD COLUMN reasoning_control_mode VARCHAR(32) NOT NULL DEFAULT ''`)
	exec(`CREATE TABLE llm_model_user_grants (model_id INT NOT NULL,user_id INT NOT NULL,PRIMARY KEY(model_id,user_id),
 FOREIGN KEY(model_id) REFERENCES llm_models(id) ON DELETE CASCADE) ENGINE=InnoDB`)
	raw, err := os.ReadFile("../db/migrations/063_add_gateway_runtime_tasks.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(statement) != "" {
			exec(statement)
		}
	}
	model := &models.LLMModel{DisplayName: "deployment-one", ProviderType: "local", ProtocolType: "openai-compatible", ModelGroup: "general", BaseURL: "https://test.invalid/v1", ProviderModelName: "same", VisibilityScope: "all", IsActive: true, Currency: "USD"}
	if err = modelRepo.Save(model); err != nil {
		t.Fatal(err)
	}
	tasks := NewGatewayTaskRepository(sess)
	identity := models.GatewayTaskIdentity{InstanceID: 10, UserID: 1, Generation: 5, SessionID: "ses_test", TaskID: "msg_one"}
	if err = tasks.Bind(t.Context(), identity, model.ID); err != nil {
		t.Fatal(err)
	}
	model.VisibilityScope = "selected_users"
	model.AllowedUserIDs = []int{2}
	model.UpdateAccessPolicy = true
	if err = modelRepo.Save(model); err != nil {
		t.Fatal(err)
	}
	state, err := tasks.Check(t.Context(), identity)
	if err != nil || !state.Revoked || state.RuntimeAcknowledged {
		t.Fatalf("revoke: %+v %v", state, err)
	}
	model.VisibilityScope = "all"
	model.AllowedUserIDs = nil
	if err = modelRepo.Save(model); err != nil {
		t.Fatal(err)
	}
	if err = tasks.Bind(t.Context(), identity, model.ID); !errors.Is(err, models.ErrModelUnavailable) {
		t.Fatalf("old task resumed: %v", err)
	}
	if err = tasks.Finish(t.Context(), identity, true); err != nil {
		t.Fatal(err)
	}
	state, err = tasks.Check(t.Context(), identity)
	if err != nil || !state.RuntimeAcknowledged || !state.Finished {
		t.Fatalf("receipt: %+v %v", state, err)
	}
	identity.TaskID = "msg_new"
	if err = tasks.Bind(t.Context(), identity, model.ID); err != nil {
		t.Fatal(err)
	}
	if err = modelRepo.Delete(model.ID); err != nil {
		t.Fatal(err)
	}
	state, err = tasks.Check(t.Context(), identity)
	if err != nil || !state.Revoked {
		t.Fatalf("deletion: %+v %v", state, err)
	}
	identity.TaskID = "msg_stale"
	if _, err = tasks.Check(t.Context(), identity); err != nil {
		t.Fatal(err)
	}
	exec("UPDATE instances SET runtime_generation=6 WHERE id=10")
	state, err = tasks.Check(t.Context(), identity)
	if err != nil || !state.Revoked {
		t.Fatalf("stale generation: %+v %v", state, err)
	}
	identity.Generation = 6
	identity.TaskID = "msg_one"
	state, err = tasks.Check(t.Context(), identity)
	if err != nil || !state.Revoked {
		t.Fatalf("revoked task resumed after restart: %+v %v", state, err)
	}
	identity.TaskID = "msg_fresh_after_restart"
	state, err = tasks.Check(t.Context(), identity)
	if err != nil || state.Revoked {
		t.Fatalf("new user turn rejected: %+v %v", state, err)
	}
	raw, err = os.ReadFile("../db/migrations/064_add_runtime_protocol.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(statement) != "" {
			exec(statement)
		}
	}
	protocols := NewRuntimeProtocolRepository(sess)
	protocol, err := protocols.Get(t.Context(), 10)
	if err != nil || protocol != nil {
		t.Fatalf("legacy instance auto-enrolled: %+v %v", protocol, err)
	}
	report := models.RuntimeCapabilityReport{Protocol: 1, Generation: 6, RuntimeVersion: "2.0.27", AdapterVersion: "cm.1", Capabilities: []string{"task_identity", "turn_cancel", "cancel_receipt", "live_catalog"}}
	if err := protocols.Negotiate(t.Context(), 10, 2, report); !errors.Is(err, models.ErrModelUnavailable) {
		t.Fatalf("wrong owner enrolled: %v", err)
	}
	if err := protocols.Negotiate(t.Context(), 10, 1, report); err != nil {
		t.Fatal(err)
	}
	if err := protocols.Negotiate(t.Context(), 10, 1, report); err != nil {
		t.Fatal("repeat negotiation", err)
	}
	exec("UPDATE instances SET runtime_generation=7 WHERE id=10")
	if err := protocols.Negotiate(t.Context(), 10, 1, report); !errors.Is(err, models.ErrModelUnavailable) {
		t.Fatalf("stale process enrolled: %v", err)
	}
	protocol, err = protocols.Get(t.Context(), 10)
	if err != nil || protocol.Protocol != 1 {
		t.Fatal("enrollment lost on restart", err)
	}
	report.Generation = 7
	report.Protocol = 0
	if protocols.Negotiate(t.Context(), 10, 1, report) == nil {
		t.Fatal("runtime downgraded itself")
	}
	raw, err = os.ReadFile("../db/migrations/065_add_runtime_maintenance.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(statement) != "" {
			exec(statement)
		}
	}
	raw, err = os.ReadFile("../db/migrations/066_add_runtime_upgrade_fence.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(statement) != "" {
			exec(statement)
		}
	}
	maintenance := NewRuntimeMaintenanceRepository(sess)
	if _, err = maintenance.Begin(t.Context(), 10, 1, 7, 1, time.Minute); !errors.Is(err, models.ErrRuntimeMaintenanceConflict) {
		t.Fatalf("stale/non-drain runtime accepted: %v", err)
	}
	report.Protocol = 1
	report.Capabilities = append(report.Capabilities, "maintenance_drain")
	if err = protocols.Negotiate(t.Context(), 10, 1, report); err != nil {
		t.Fatal(err)
	}
	drain, err := maintenance.Begin(t.Context(), 10, 1, 7, 1, time.Minute)
	if err != nil || drain.Drained(time.Now()) {
		t.Fatalf("drain before receipt: %+v %v", drain, err)
	}
	if _, err = maintenance.Begin(t.Context(), 10, 1, 7, 1, time.Minute); !errors.Is(err, models.ErrRuntimeMaintenanceConflict) {
		t.Fatal("overlapping drain accepted", err)
	}
	status := models.RuntimeMaintenanceReport{Generation: 7, Operation: drain.Operation, GateClosed: true, Jobs: 1}
	drain, err = maintenance.Poll(t.Context(), 10, 1, status)
	if err != nil || drain.Drained(time.Now()) {
		t.Fatalf("busy runtime drained: %+v %v", drain, err)
	}
	status.Jobs = 0
	drain, err = maintenance.Poll(t.Context(), 10, 1, status)
	if err != nil || !drain.Drained(time.Now()) {
		t.Fatalf("idle receipt rejected: %+v %v", drain, err)
	}
	if err = maintenance.Cancel(t.Context(), 10, strings.Repeat("0", 32)); !errors.Is(err, models.ErrRuntimeMaintenanceConflict) {
		t.Fatal("wrong cancel accepted", err)
	}
	if err = maintenance.Cancel(t.Context(), 10, drain.Operation); err != nil {
		t.Fatal(err)
	}
	drain, err = maintenance.Poll(t.Context(), 10, 1, status)
	if err != nil || drain.Draining(time.Now()) {
		t.Fatal("late receipt revived cancellation", err)
	}
	drain, err = maintenance.Begin(t.Context(), 10, 1, 7, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if drain.Operation == status.Operation {
		t.Fatal("operation identity reused")
	}
	drain, err = maintenance.Poll(t.Context(), 10, 1, status)
	if err != nil || drain.ReportedAt != nil {
		t.Fatal("late receipt acknowledged next operation", err)
	}
	exec("UPDATE gateway_runtime_maintenance SET deadline=DATE_SUB(UTC_TIMESTAMP(6), INTERVAL 1 SECOND) WHERE instance_id=10")
	status.Operation = drain.Operation
	drain, err = maintenance.Poll(t.Context(), 10, 1, status)
	if err != nil || drain.Draining(time.Now()) || drain.Drained(time.Now()) {
		t.Fatal("expired drain accepted", err)
	}
	status.Generation = 6
	if _, err = maintenance.Poll(t.Context(), 10, 1, status); !errors.Is(err, models.ErrModelUnavailable) {
		t.Fatal("stale generation report accepted", err)
	}
	exec("ALTER TABLE instances ADD COLUMN type VARCHAR(32) NOT NULL DEFAULT 'opencode', ADD COLUMN status VARCHAR(32) NOT NULL DEFAULT 'running'")
	upgrade := NewRuntimeUpgradeRepository(sess)
	status.Generation = 7
	drain, err = maintenance.Begin(t.Context(), 10, 1, 7, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	status.Operation = drain.Operation
	status.Jobs = 1
	if _, err = maintenance.Poll(t.Context(), 10, 1, status); err != nil {
		t.Fatal(err)
	}
	if err = upgrade.Reserve(t.Context(), 10, 1, 7, status.Operation); !errors.Is(err, models.ErrRuntimeUpgradeConflict) {
		t.Fatal("busy runtime reserved", err)
	}
	status.Jobs = 0
	if _, err = maintenance.Poll(t.Context(), 10, 1, status); err != nil {
		t.Fatal(err)
	}
	instanceRepo := &instanceRepository{sess}
	release, err := instanceRepo.AcquireLifecycleLock(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	err = upgrade.Reserve(t.Context(), 10, 1, 7, status.Operation)
	release()
	if !errors.Is(err, models.ErrInstanceOperationInProgress) {
		t.Fatal("reservation bypassed running operation", err)
	}
	commands := NewInstanceCommandRepository(sess)
	writes := NewWorkspaceWriteRepository(sess)
	writeIntent := models.WorkspaceWriteIntent{InstanceID: 10, OwnerID: 1, Generation: 7, Operation: strings.Repeat("e", 32), Action: "upload"}
	wrongWrite := writeIntent
	wrongWrite.OwnerID = 2
	if err = writes.Begin(t.Context(), wrongWrite); !errors.Is(err, models.ErrRuntimeUpgradeConflict) {
		t.Fatal("wrong write owner accepted", err)
	}
	wrongWrite = writeIntent
	wrongWrite.Generation = 6
	if err = writes.Begin(t.Context(), wrongWrite); !errors.Is(err, models.ErrRuntimeUpgradeConflict) {
		t.Fatal("stale write accepted", err)
	}
	if err = writes.Begin(t.Context(), writeIntent); err != nil {
		t.Fatal(err)
	}
	if err = writes.Begin(t.Context(), writeIntent); err == nil {
		t.Fatal("duplicate intent authorized redispatch")
	}
	if err = upgrade.Reserve(t.Context(), 10, 1, 7, status.Operation); !errors.Is(err, models.ErrRuntimeUpgradeWritersActive) {
		t.Fatal("active remote write permitted reservation", err)
	}
	// A restarted manager and an arbitrarily old intent are still not proof of exit.
	writes = NewWorkspaceWriteRepository(sess)
	exec("UPDATE instance_workspace_write_intents SET created_at=DATE_SUB(UTC_TIMESTAMP(6), INTERVAL 30 DAY)")
	if err = writes.Complete(t.Context(), wrongWrite); !errors.Is(err, models.ErrRuntimeUpgradeConflict) {
		t.Fatal("wrong receipt removed intent", err)
	}
	if err = upgrade.Reserve(t.Context(), 10, 1, 7, status.Operation); !errors.Is(err, models.ErrRuntimeUpgradeWritersActive) {
		t.Fatal("intent expired silently", err)
	}
	if err = writes.Complete(t.Context(), writeIntent); err != nil {
		t.Fatal(err)
	}
	desiredStates := NewInstanceDesiredStateRepository(sess)
	desired := &models.InstanceDesiredState{InstanceID: 10, DesiredPowerState: "running"}
	if err = desiredStates.Create(desired); err != nil {
		t.Fatal("ordinary desired state create", err)
	}
	action := "reload"
	desired.DesiredRuntimeAction = &action
	if err = desiredStates.Update(desired); err != nil {
		t.Fatal("ordinary desired state update", err)
	}
	queued := &models.InstanceCommand{InstanceID: 10, CommandType: "install_skill", Status: "pending", IdempotencyKey: "upgrade-writer", TimeoutSeconds: 300}
	if err = commands.Create(queued); err != nil || queued.ID <= 0 {
		t.Fatal("queue fixture", err)
	}
	for _, commandState := range []string{"pending", "dispatched", "running", "unknown"} {
		queued.Status = commandState
		if err = commands.Update(queued); err != nil {
			t.Fatal(err)
		}
		if err = upgrade.Reserve(t.Context(), 10, 1, 7, status.Operation); !errors.Is(err, models.ErrRuntimeUpgradeWritersActive) {
			t.Fatal("active command permitted reservation", commandState, err)
		}
	}
	queued.Status = "succeeded"
	if err = commands.Update(queued); err != nil {
		t.Fatal(err)
	}
	staleCommand := *queued
	staleCommand.Status = "dispatched"
	if err = commands.Update(&staleCommand); err == nil {
		t.Fatal("finished command resurrected")
	}
	if err = upgrade.Reserve(t.Context(), 10, 1, 7, status.Operation); err != nil {
		t.Fatal(err)
	}
	if err = upgrade.Reserve(t.Context(), 10, 1, 7, status.Operation); err != nil {
		t.Fatal("retry not idempotent", err)
	}
	if err = desiredStates.Update(desired); !errors.Is(err, models.ErrRuntimeUpgradeInProgress) {
		t.Fatal("desired state changed under fence", err)
	}
	if err = writes.Begin(t.Context(), writeIntent); !errors.Is(err, models.ErrRuntimeUpgradeInProgress) {
		t.Fatal("write began under fence", err)
	}
	if err = commands.Create(&models.InstanceCommand{InstanceID: 10, CommandType: "install_skill", Status: "pending", IdempotencyKey: "blocked"}); !errors.Is(err, models.ErrRuntimeUpgradeInProgress) {
		t.Fatal("command created under fence", err)
	}
	if err = commands.Update(&staleCommand); !errors.Is(err, models.ErrRuntimeUpgradeInProgress) {
		t.Fatal("command dispatched under fence", err)
	}
	if _, err = (&instanceRepository{sess}).AcquireLifecycleLock(t.Context(), 10); !errors.Is(err, models.ErrRuntimeUpgradeInProgress) {
		t.Fatal("new repository bypassed persistent fence", err)
	}
	for name, call := range map[string]func() error{
		"state":     func() error { return instanceRepo.UpdateRuntimeState(t.Context(), 10, "creating", 8, nil) },
		"workspace": func() error { return instanceRepo.SetWorkspacePath(t.Context(), 10, "/not-written") },
		"snapshot":  func() error { return instanceRepo.Update(&models.Instance{ID: 10}) },
		"delete":    func() error { return instanceRepo.Delete(10) },
		"desired-create": func() error {
			return NewInstanceDesiredStateRepository(sess).Create(&models.InstanceDesiredState{InstanceID: 10})
		},
		"desired-update": func() error {
			return NewInstanceDesiredStateRepository(sess).Update(&models.InstanceDesiredState{ID: 1, InstanceID: 10})
		},
		"binding-update": func() error {
			return NewInstanceRuntimeBindingRepository(sess).UpdateState(t.Context(), 10, 7, "error", nil)
		},
		"binding-delete": func() error {
			return NewInstanceRuntimeBindingRepository(sess).DeleteByInstanceIDAndReleaseSlot(t.Context(), 10, 1)
		},
	} {
		if err := call(); !errors.Is(err, models.ErrRuntimeUpgradeInProgress) {
			t.Fatalf("%s bypassed fence: %v", name, err)
		}
	}
	if err = maintenance.Cancel(t.Context(), 10, status.Operation); !errors.Is(err, models.ErrRuntimeUpgradeInProgress) {
		t.Fatal("drain cancel bypassed reservation", err)
	}
	if err = upgrade.CancelReservation(t.Context(), 10, 1, 7, status.Operation); err != nil {
		t.Fatal(err)
	}
	if err = upgrade.Reserve(t.Context(), 10, 1, 7, status.Operation); !errors.Is(err, models.ErrRuntimeUpgradeConflict) {
		t.Fatal("cancelled operation revived", err)
	}
	release, err = instanceRepo.AcquireLifecycleLock(t.Context(), 10)
	if err != nil {
		t.Fatal("cancel did not release fence", err)
	}
	release()
	drain, err = maintenance.Begin(t.Context(), 10, 1, 7, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	status.Operation = drain.Operation
	if _, err = maintenance.Poll(t.Context(), 10, 1, status); err != nil {
		t.Fatal(err)
	}
	if err = upgrade.Reserve(t.Context(), 10, 1, 7, status.Operation); err != nil {
		t.Fatal(err)
	}
	exec("UPDATE gateway_runtime_maintenance SET deadline=DATE_SUB(UTC_TIMESTAMP(6), INTERVAL 1 DAY),reported_at=NULL WHERE instance_id=10")
	if err = upgrade.MarkStopIntent(t.Context(), 10, 1, 7, status.Operation); !errors.Is(err, models.ErrRuntimeUpgradeConflict) {
		t.Fatal("stale drain permitted stop intent", err)
	}
	drain, err = maintenance.Poll(t.Context(), 10, 1, status)
	if err != nil || !drain.UpgradeFenced || !drain.Drained(time.Now()) {
		t.Fatalf("expired lease reopened held gate: %+v %v", drain, err)
	}
	if err = upgrade.MarkStopIntent(t.Context(), 10, 1, 7, status.Operation); err != nil {
		t.Fatal(err)
	}
	if err = upgrade.CancelReservation(t.Context(), 10, 1, 7, status.Operation); !errors.Is(err, models.ErrRuntimeUpgradeConflict) {
		t.Fatal("stop intent unlocked by cancel", err)
	}
	if err = upgrade.MarkRecoveryRequired(t.Context(), 10, 1, 7, status.Operation); err != nil {
		t.Fatal(err)
	}
	if _, err = maintenance.Begin(t.Context(), 10, 1, 7, 1, time.Minute); !errors.Is(err, models.ErrRuntimeUpgradeInProgress) {
		t.Fatal("new drain bypassed recovery fence", err)
	}
	if err = upgrade.MarkRecoveryRequired(t.Context(), 10, 2, 7, status.Operation); !errors.Is(err, models.ErrRuntimeUpgradeConflict) {
		t.Fatal("wrong owner accepted", err)
	}
	if err = upgrade.MarkRecoveryRequired(t.Context(), 10, 1, 8, status.Operation); !errors.Is(err, models.ErrRuntimeUpgradeConflict) {
		t.Fatal("wrong generation accepted", err)
	}
	if err = upgrade.CancelReservation(t.Context(), 10, 1, 7, strings.Repeat("a", 32)); !errors.Is(err, models.ErrRuntimeUpgradeConflict) {
		t.Fatal("wrong operation accepted", err)
	}
	if _, err = sess.SQL().Exec("DELETE FROM instances WHERE id=10"); err == nil {
		t.Fatal("active fence cascade-deleted")
	}
	exec("INSERT INTO instances(id,user_id,runtime_generation) VALUES (11,1,1)")
	exec("INSERT INTO gateway_runtime_upgrades VALUES (11,1,1,?,'cancelled',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", strings.Repeat("b", 32))
	if err = instanceRepo.Delete(11); err != nil {
		t.Fatal("terminal fence prevented ordinary deletion", err)
	}

	// Isolated stop-coordination fixture, using the production stop migration.
	raw, err = os.ReadFile("../db/migrations/067_add_runtime_upgrade_stop.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(statement) != "" {
			exec(statement)
		}
	}
	exec("CREATE TABLE runtime_pods(id BIGINT PRIMARY KEY,pod_uid VARCHAR(128),agent_endpoint VARCHAR(1024)) ENGINE=InnoDB")
	exec("CREATE TABLE instance_runtime_bindings(instance_id INT PRIMARY KEY,runtime_pod_id BIGINT,generation INT,gateway_id VARCHAR(100),gateway_pid INT,state VARCHAR(32)) ENGINE=InnoDB")
	exec("INSERT INTO instances(id,user_id,runtime_generation) VALUES(12,1,1)")
	exec("INSERT INTO runtime_pods VALUES(9,'pod-9','http://agent:8080')")
	exec("INSERT INTO instance_runtime_bindings VALUES(12,9,1,'gw-12-1',1234,'running')")
	report.Generation = 1
	if err = protocols.Negotiate(t.Context(), 12, 1, report); err != nil {
		t.Fatal(err)
	}
	drain, err = maintenance.Begin(t.Context(), 12, 1, 1, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	status = models.RuntimeMaintenanceReport{Generation: 1, Operation: drain.Operation, GateClosed: true}
	if _, err = maintenance.Poll(t.Context(), 12, 1, status); err != nil {
		t.Fatal(err)
	}
	if err = upgrade.Reserve(t.Context(), 12, 1, 1, drain.Operation); err != nil {
		t.Fatal(err)
	}
	stops := NewRuntimeUpgradeStopRepository(sess)
	target := models.RuntimeUpgradeStop{InstanceID: 12, OwnerID: 1, Generation: 1, Operation: drain.Operation, RuntimePodID: 9, PodUID: "pod-9", AgentEndpoint: "http://agent:8080", AgentEpoch: strings.Repeat("c", 32), GatewayID: "gw-12-1", PID: 1234}
	wrong := target
	wrong.PID++
	if err = stops.PrepareStop(t.Context(), wrong); !errors.Is(err, models.ErrRuntimeUpgradeConflict) {
		t.Fatal("wrong process pinned", err)
	}
	current, err := stops.Get(t.Context(), 12)
	if err != nil || current.State != "reserved" {
		t.Fatal("failed prepare changed fence", current, err)
	}
	exec("UPDATE gateway_runtime_maintenance SET reported_at=NULL WHERE instance_id=12")
	if err = stops.PrepareStop(t.Context(), target); !errors.Is(err, models.ErrRuntimeUpgradeConflict) {
		t.Fatal("stale evidence accepted", err)
	}
	if _, err = maintenance.Poll(t.Context(), 12, 1, status); err != nil {
		t.Fatal(err)
	}
	// Fail AFTER target INSERT, proving intent + target roll back together.
	exec("CREATE TRIGGER reject_stop_intent BEFORE UPDATE ON gateway_runtime_upgrades FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected stop-intent write failure'")
	if err = stops.PrepareStop(t.Context(), target); err == nil {
		t.Fatal("injected transaction failure ignored")
	}
	exec("DROP TRIGGER reject_stop_intent")
	partial, readErr := stops.GetStop(t.Context(), 12)
	if readErr != nil || partial != nil {
		t.Fatal("partial stop target committed", partial, readErr)
	}
	current, err = stops.Get(t.Context(), 12)
	if err != nil || current.State != "reserved" {
		t.Fatal("partial stop intent committed", current, err)
	}
	if err = stops.PrepareStop(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	// Simulate coordinator recreation and expired drain after the process stops.
	stops = NewRuntimeUpgradeStopRepository(sess)
	exec("UPDATE gateway_runtime_maintenance SET reported_at=NULL WHERE instance_id=12")
	if err = stops.PrepareStop(t.Context(), target); err != nil {
		t.Fatal("retry demanded live stopped process report", err)
	}
	if err = stops.CancelReservation(t.Context(), 12, 1, 1, target.Operation); !errors.Is(err, models.ErrRuntimeUpgradeConflict) {
		t.Fatal("stop cancelled", err)
	}
	wrong = target
	wrong.AgentEpoch = strings.Repeat("d", 32)
	if err = stops.PrepareStop(t.Context(), wrong); !errors.Is(err, models.ErrRuntimeUpgradeConflict) {
		t.Fatal("agent epoch rebound", err)
	}
	confirmed := time.Now().UTC().Truncate(time.Microsecond)
	// Simulate a stale/unpatched writer bypassing the guarded queue after the
	// fence. Confirmation must refuse; terminal acknowledgements may settle it.
	exec("INSERT INTO instance_commands(instance_id,command_type,status,idempotency_key) VALUES(12,'install_skill','dispatched','late-writer')")
	late, err := commands.GetByInstanceIdempotencyKey(12, "late-writer")
	if err != nil || late == nil {
		t.Fatal(err)
	}
	probe := target
	probe.ConfirmationScope = "process_group"
	probe.ConfirmedAt = &confirmed
	if err = stops.RecordStopConfirmation(t.Context(), probe); !errors.Is(err, models.ErrRuntimeUpgradeWritersActive) {
		t.Fatal("confirmed with active external command", err)
	}
	late.Status = "running"
	if err = commands.Update(late); !errors.Is(err, models.ErrRuntimeUpgradeInProgress) {
		t.Fatal("late start bypassed fence", err)
	}
	forged := *late
	forged.InstanceID = 10
	forged.Status = "succeeded"
	if err = commands.Update(&forged); err == nil {
		t.Fatal("command owner instance replaced")
	}
	late.Status = "succeeded"
	if err = commands.Update(late); err != nil {
		t.Fatal("fence prevented terminal acknowledgement", err)
	}
	// Simulate a legacy writer appearing after reservation. Even an old-generation
	// write blocks both stop retry and confirmation until its exact receipt settles.
	lateWrite := models.WorkspaceWriteIntent{InstanceID: 12, OwnerID: 1, Generation: 0, Operation: strings.Repeat("f", 32), Action: "rename"}
	exec("INSERT INTO instance_workspace_write_intents(operation_id,instance_id,owner_user_id,generation,action,created_at) VALUES(?,?,?,?,?,UTC_TIMESTAMP(6))", lateWrite.Operation, lateWrite.InstanceID, lateWrite.OwnerID, lateWrite.Generation, lateWrite.Action)
	if err = stops.PrepareStop(t.Context(), target); !errors.Is(err, models.ErrRuntimeUpgradeWritersActive) {
		t.Fatal("stop retried with outstanding write", err)
	}
	if err = stops.RecordStopConfirmation(t.Context(), probe); !errors.Is(err, models.ErrRuntimeUpgradeWritersActive) {
		t.Fatal("stop confirmed with outstanding write", err)
	}
	if err = writes.Complete(t.Context(), lateWrite); err != nil {
		t.Fatal("exact late receipt rejected", err)
	}
	wrong = target
	wrong.PID++
	wrong.ConfirmationScope = "process_group"
	wrong.ConfirmedAt = &confirmed
	if err = stops.RecordStopConfirmation(t.Context(), wrong); !errors.Is(err, models.ErrRuntimeUpgradeConflict) {
		t.Fatal("wrong receipt accepted", err)
	}
	target.ConfirmationScope = "process_group"
	target.ConfirmedAt = &confirmed
	if err = stops.RecordStopConfirmation(t.Context(), target); err != nil {
		t.Fatal(err)
	}
	if err = stops.RecordStopConfirmation(t.Context(), target); err != nil {
		t.Fatal("receipt not idempotent", err)
	}
	newObservation := confirmed.Add(time.Second)
	refreshed := target
	refreshed.ConfirmedAt = &newObservation
	if err = stops.RecordStopConfirmation(t.Context(), refreshed); err != nil {
		t.Fatal("fresh same-agent confirmation rejected", err)
	}
	persisted, err := stops.GetStop(t.Context(), 12)
	if err != nil || persisted == nil || !persisted.SameTarget(target) || persisted.ConfirmedAt == nil || !persisted.ConfirmedAt.Equal(confirmed) {
		t.Fatal("lost stop identity/receipt", persisted, err)
	}
	current, err = stops.Get(t.Context(), 12)
	if err != nil || current.State != "process_group_confirmed" || !current.Active() {
		t.Fatal("receipt released fence", current, err)
	}
	if _, err = instanceRepo.AcquireLifecycleLock(t.Context(), 12); !errors.Is(err, models.ErrRuntimeUpgradeInProgress) {
		t.Fatal("receipt allowed ordinary start", err)
	}
	exec("UPDATE runtime_pods SET pod_uid='replacement' WHERE id=9")
	if err = stops.RecordStopConfirmation(t.Context(), target); !errors.Is(err, models.ErrRuntimeUpgradeConflict) {
		t.Fatal("historical receipt accepted for replacement", err)
	}
	exec("UPDATE runtime_pods SET pod_uid='pod-9' WHERE id=9")
	if err = stops.MarkRecoveryRequired(t.Context(), 12, 1, 1, target.Operation); err != nil {
		t.Fatal(err)
	}
	if err = stops.RecordStopConfirmation(t.Context(), target); !errors.Is(err, models.ErrRuntimeUpgradeConflict) {
		t.Fatal("late receipt cleared recovery", err)
	}
}
