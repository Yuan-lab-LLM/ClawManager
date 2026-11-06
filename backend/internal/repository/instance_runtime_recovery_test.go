package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestRuntimeRecoveryMySQLPersistsBudgetAndRejectsStoppedOrStaleGeneration(t *testing.T) {
	r := runtimeSecurityMySQL(t)
	raw, err := os.ReadFile("../db/migrations/103_add_instance_runtime_recovery.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.sess.SQL().Exec(string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err = r.sess.SQL().Exec(`CREATE TABLE instance_desired_state (instance_id INT PRIMARY KEY,desired_power_state VARCHAR(32))`); err != nil {
		t.Fatal(err)
	}
	if _, err = r.sess.SQL().Exec(`CREATE TABLE instance_runtime_bindings (instance_id INT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	snapshot, err := r.GetByIDContext(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	next := now.Add(120 * time.Second)
	state := models.InstanceRuntimeRecovery{InstanceID: 7, Attempts: 2, NextRetryAt: &next, Reason: "process exited", Generation: 2, UpdatedAt: now}
	ok, err := r.SaveRuntimeRecovery(ctx, *snapshot, state)
	if err != nil || !ok {
		t.Fatalf("save: %v %v", ok, err)
	}
	restarted := &instanceRepository{sess: r.sess}
	got, err := restarted.GetRuntimeRecovery(ctx, 7)
	if err != nil || got.Attempts != 2 || !got.NextRetryAt.Equal(next) {
		t.Fatalf("restart lost budget: %+v %v", got, err)
	}
	if _, err = r.sess.SQL().Exec(`UPDATE instances SET updated_at=? WHERE id=7`, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	ok, err = r.SaveRuntimeRecovery(ctx, *snapshot, state)
	if err != nil || !ok {
		t.Fatalf("heartbeat invalidated independent recovery clock: %v %v", ok, err)
	}
	for _, mutation := range []string{`UPDATE instances SET status='stopped' WHERE id=7`, `UPDATE instances SET runtime_generation=3 WHERE id=7`, `INSERT INTO instance_desired_state VALUES (7,'stopped')`} {
		if _, err = r.sess.SQL().Exec(`UPDATE instances SET status='creating',runtime_generation=2 WHERE id=7`); err != nil {
			t.Fatal(err)
		}
		if _, err = r.sess.SQL().Exec(mutation); err != nil {
			t.Fatal(err)
		}
		state.Attempts = 3
		ok, err = r.SaveRuntimeRecovery(ctx, *snapshot, state)
		if err != nil || ok {
			t.Fatalf("stale/user stop accepted: %s %v %v", mutation, ok, err)
		}
		ok, err = r.AdvanceRuntimeRecovery(ctx, *snapshot)
		if err != nil || ok {
			t.Fatalf("stale/user stop restarted: %s %v %v", mutation, ok, err)
		}
	}
	if _, err = r.sess.SQL().Exec(`DELETE FROM instance_desired_state`); err != nil {
		t.Fatal(err)
	}
	advanced, err := r.AdvanceRuntimeRecovery(ctx, *snapshot)
	if err != nil || !advanced {
		t.Fatalf("confirmed unbound recovery did not advance: %v %v", advanced, err)
	}
	got, err = restarted.GetRuntimeRecovery(ctx, 7)
	if err != nil || got.Attempts != 2 {
		t.Fatal("automatic generation advance reset durable budget")
	}
	if err = r.ResetRuntimeRecovery(ctx, 7); err != nil {
		t.Fatal(err)
	}
	got, err = restarted.GetRuntimeRecovery(ctx, 7)
	if err != nil || got != nil {
		t.Fatal("explicit restart reset did not clear budget")
	}

}

func TestRuntimeRecoveryBindingMySQLExactSourceAssignment(t *testing.T) {
	r := runtimeSecurityMySQL(t)
	for _, ddl := range []string{`CREATE TABLE runtime_pods (id BIGINT PRIMARY KEY,used_slots INT,updated_at DATETIME(6))`, `CREATE TABLE instance_runtime_bindings (instance_id INT PRIMARY KEY,runtime_pod_id BIGINT,generation INT,gateway_id VARCHAR(255),gateway_port INT,state VARCHAR(32))`, `INSERT INTO runtime_pods VALUES (9,1,NOW(6))`} {
		if _, err := r.sess.SQL().Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	bindings := &instanceRuntimeBindingRepository{sess: r.sess}
	expected := models.InstanceRuntimeBinding{InstanceID: 7, RuntimePodID: 9, Generation: 2, GatewayID: "gw-7-2", GatewayPort: 19000, State: "error"}
	for _, scenario := range []string{"gateway", "port", "generation", "running", "exact"} {
		t.Run(scenario, func(t *testing.T) {
			if _, err := r.sess.SQL().Exec(`DELETE FROM instance_runtime_bindings`); err != nil {
				t.Fatal(err)
			}
			gateway, port, generation, state := expected.GatewayID, expected.GatewayPort, expected.Generation, expected.State
			switch scenario {
			case "gateway":
				gateway = "GW-7-2"
			case "port":
				port++
			case "generation":
				generation++
			case "running":
				state = "running"
			}
			if _, err := r.sess.SQL().Exec(`INSERT INTO instance_runtime_bindings VALUES (7,9,?,?,?,?)`, generation, gateway, port, state); err != nil {
				t.Fatal(err)
			}
			deleted, err := bindings.DeleteConfirmedRecoveryBinding(context.Background(), expected)
			if err != nil || deleted != (scenario == "exact") {
				t.Fatalf("source identity was not fenced: %v %v", deleted, err)
			}
		})
	}
}
