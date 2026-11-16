package services

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/upper/db/v4/adapter/mysql"
)

func TestNativeRecoveryMySQLRetainsDesiredPowerAndAtomicStopCheckpoint(t *testing.T) {
	dsn := os.Getenv("RECOVERY_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("RECOVERY_TEST_MYSQL_DSN requires a dedicated empty fixture")
	}
	cfg, err := drivermysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(cfg.DBName, "clawmanager_test_recovery_") {
		t.Fatal("refusing non-recovery fixture")
	}
	sess, err := mysql.Open(mysql.ConnectionURL{Host: cfg.Addr, User: cfg.User, Password: cfg.Passwd, Database: cfg.DBName, Options: map[string]string{"parseTime": "true"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	row, err := sess.SQL().QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=?", cfg.DBName)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = row.Scan(&count); err != nil || count != 0 {
		t.Fatal("requires empty dedicated recovery DB")
	}
	tables := []string{"instance_runtime_recovery", "instance_runtime_bindings", "runtime_pods", "instance_desired_state", "instance_model_activity", "gateway_runtime_upgrades", "instances"}
	t.Cleanup(func() {
		for _, table := range tables {
			if _, err := sess.SQL().Exec("DROP TABLE IF EXISTS " + table); err != nil {
				t.Error(err)
			}
		}
	})
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE TABLE instances(id INT PRIMARY KEY,status VARCHAR(32),instance_mode VARCHAR(32),runtime_generation INT,runtime_error_code VARCHAR(64),runtime_error_message TEXT,updated_at DATETIME(6))")
	exec("CREATE TABLE instance_desired_state(instance_id INT PRIMARY KEY,desired_power_state VARCHAR(32))")
	exec("CREATE TABLE gateway_runtime_upgrades(instance_id INT PRIMARY KEY,state VARCHAR(32))")
	exec("CREATE TABLE instance_model_activity(instance_id INT PRIMARY KEY,stop_operation_id VARCHAR(64),stopped_reason VARCHAR(32),start_deadline_at DATETIME(6))")
	exec("CREATE TABLE runtime_pods(id BIGINT PRIMARY KEY,used_slots INT,updated_at DATETIME(6))")
	exec("CREATE TABLE instance_runtime_bindings(instance_id INT PRIMARY KEY,runtime_pod_id BIGINT,generation INT,gateway_id VARCHAR(128),gateway_port INT,state VARCHAR(32))")
	for _, file := range []string{"103_add_instance_runtime_recovery.sql", "106_add_runtime_recovery_phase.sql"} {
		raw, err := os.ReadFile("../db/migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range strings.Split(string(raw), ";") {
			if strings.TrimSpace(statement) != "" {
				exec(statement)
			}
		}
	}
	exec("INSERT INTO instances VALUES(7,'error','lite',2,NULL,'gateway_stop_failed',NOW(6))")
	exec("INSERT INTO runtime_pods VALUES(9,1,NOW(6))")
	exec("INSERT INTO instance_runtime_bindings VALUES(7,9,2,'gw-7-2',20000,'error')")
	instances := repository.NewInstanceRepository(sess).(repository.InstanceRuntimeRecoveryRepository)
	bindings := repository.NewInstanceRuntimeBindingRepository(sess).(repository.ConfirmedRecoveryBindingReleaser)
	snapshot := models.Instance{ID: 7, Status: "error", RuntimeGeneration: 2}
	now := time.Now().UTC()
	state := models.InstanceRuntimeRecovery{InstanceID: 7, Generation: 2, Attempts: 1, Reason: runtimeRecoveryNativeResetReason, SourcePodID: 9, SourceGatewayPort: 20000, UpdatedAt: now}
	ctx := context.Background()
	exec("INSERT INTO instance_desired_state VALUES(7,'stopped')")
	if saved, err := instances.SaveRuntimeRecovery(ctx, snapshot, state); err != nil || saved {
		t.Fatalf("desired stop armed recovery: %v %v", saved, err)
	}
	exec("DELETE FROM instance_desired_state")
	if saved, err := instances.SaveRuntimeRecovery(ctx, snapshot, state); err != nil || !saved {
		t.Fatalf("legacy implicit running lost intent: %v %v", saved, err)
	}
	binding := models.InstanceRuntimeBinding{InstanceID: 7, RuntimePodID: 9, Generation: 2, GatewayID: "gw-7-2", GatewayPort: 20000, State: "error"}
	changed := binding
	changed.GatewayPort++
	if released, err := bindings.DeleteConfirmedRecoveryBinding(ctx, changed); err != nil || released {
		t.Fatal("changed source released")
	}
	if released, err := bindings.DeleteConfirmedRecoveryBinding(ctx, binding); err != nil || !released {
		t.Fatalf("confirmed exact source did not checkpoint: %v %v", released, err)
	}
	got, err := instances.GetRuntimeRecovery(ctx, 7)
	if err != nil || got.Phase != "source_stopped" || got.Attempts != 1 {
		t.Fatalf("lost stop checkpoint: %+v %v", got, err)
	}
	exec("INSERT INTO instance_desired_state VALUES(7,'stopped')")
	if advanced, err := instances.AdvanceRuntimeRecovery(ctx, snapshot); err != nil || advanced {
		t.Fatal("desired stop advanced generation")
	}
	exec("DELETE FROM instance_desired_state")
	exec("INSERT INTO instance_model_activity VALUES(7,'manual-stop','manual',NULL)")
	if advanced, err := instances.AdvanceRuntimeRecovery(ctx, snapshot); err == nil || advanced {
		t.Fatal("active manual stop advanced generation")
	}
	exec("DELETE FROM instance_model_activity")
	exec("INSERT INTO gateway_runtime_upgrades VALUES(7,'reserved')")
	if advanced, err := instances.AdvanceRuntimeRecovery(ctx, snapshot); err == nil || advanced {
		t.Fatal("upgrade fence advanced generation")
	}
	exec("DELETE FROM gateway_runtime_upgrades")
	if advanced, err := instances.AdvanceRuntimeRecovery(ctx, snapshot); err != nil || !advanced {
		t.Fatalf("checkpoint could not advance: %v %v", advanced, err)
	}
	if advanced, err := instances.AdvanceRuntimeRecovery(ctx, snapshot); err != nil || advanced {
		t.Fatal("stale generation advanced twice")
	}
	got, err = instances.GetRuntimeRecovery(ctx, 7)
	if err != nil || got.Phase != "placing" || got.Generation != 3 || got.Attempts != 1 {
		t.Fatalf("replay lost budget/generation: %+v %v", got, err)
	}
}
