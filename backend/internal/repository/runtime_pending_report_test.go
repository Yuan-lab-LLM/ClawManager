package repository

import (
	"clawreef/internal/models"
	"context"
	"github.com/upper/db/v4"
	"os"
	"strings"
	"testing"
)

func TestPendingReportedBindingExactPredicate(t *testing.T) {
	report := &models.InstanceRuntimeBinding{InstanceID: 7, RuntimePodID: 5, RuntimeType: "hermes", GatewayID: "gw-7-2", GatewayPort: 20001, WorkspacePath: "/workspaces/hermes/user-3/instance-7", State: "running", Generation: 2}
	for _, scenario := range []string{"valid", "deleted", "pod", "generation", "port", "workspace", "runtime", "state", "id"} {
		t.Run(scenario, func(t *testing.T) {
			existing := *report
			existing.GatewayID = "pending-7-2"
			existing.State = "creating"
			p := &existing
			switch scenario {
			case "deleted":
				p = nil
			case "pod":
				existing.RuntimePodID++
			case "generation":
				existing.Generation++
			case "port":
				existing.GatewayPort++
			case "workspace":
				existing.WorkspacePath += "/other"
			case "runtime":
				existing.RuntimeType = "openclaw"
			case "state":
				existing.State = "stopped"
			case "id":
				existing.GatewayID = "pending-7-1"
			}
			if exactPendingReportedBinding(p, report) != (scenario == "valid") {
				t.Fatal("incorrect pending predicate")
			}
		})
	}
}
func TestPendingReportedGatewayPersistence(t *testing.T) {
	instances := runtimeSecurityMySQL(t)
	sess := instances.sess
	for _, q := range []string{"CREATE TABLE runtime_management_admission_lock(id INT PRIMARY KEY,revision BIGINT NOT NULL DEFAULT 0)", "INSERT INTO runtime_management_admission_lock(id) VALUES (1)", "CREATE TABLE runtime_pods(id BIGINT PRIMARY KEY,namespace VARCHAR(128),deployment_name VARCHAR(255))", "INSERT INTO runtime_pods VALUES(5,'runtime','pool')"} {
		if _, err := sess.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	fenceSQL, err := os.ReadFile("../db/migrations/080_add_runtime_pod_deletion_intents.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range strings.Split(string(fenceSQL), ";") {
		if strings.TrimSpace(q) != "" {
			if _, err := sess.SQL().Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	}
	repo := &instanceRuntimeBindingRepository{sess: sess}
	ctx := context.Background()
	_, err = sess.SQL().Exec(`CREATE TABLE instance_runtime_bindings (id BIGINT PRIMARY KEY AUTO_INCREMENT,instance_id BIGINT NOT NULL UNIQUE,runtime_pod_id BIGINT,runtime_type VARCHAR(40),gateway_id VARCHAR(80),gateway_port INT,gateway_pid INT NULL,workspace_path TEXT,state VARCHAR(20),generation INT,last_health_at DATETIME(6) NULL,error_message TEXT NULL,created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6))`)
	if err != nil {
		t.Fatal(err)
	}
	reset := func() {
		t.Helper()
		if _, e := sess.SQL().Exec(`DELETE FROM instance_runtime_bindings`); e != nil {
			t.Fatal(e)
		}
		if _, e := sess.SQL().Exec(`UPDATE instances SET status='creating',runtime_generation=2,type='hermes',instance_mode='lite',runtime_type='gateway',runtime_security_profile='isolated-network-v1' WHERE id=7`); e != nil {
			t.Fatal(e)
		}
		if _, e := sess.SQL().Exec(`INSERT INTO instance_runtime_bindings (instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES (7,5,'hermes','pending-7-2',20001,'/workspaces/hermes/user-3/instance-7','creating',2)`); e != nil {
			t.Fatal(e)
		}
	}
	report := &models.InstanceRuntimeBinding{InstanceID: 7, RuntimePodID: 5, RuntimeType: "hermes", GatewayID: "gw-7-2", GatewayPort: 20001, WorkspacePath: "/workspaces/hermes/user-3/instance-7", State: "running", Generation: 2}
	for _, scenario := range []string{"valid", "wrong-pod", "stopped", "port-conflict", "deleted-before-lock"} {
		t.Run(scenario, func(t *testing.T) {
			reset()
			switch scenario {
			case "wrong-pod":
				_, err = sess.SQL().Exec(`UPDATE instance_runtime_bindings SET runtime_pod_id=6 WHERE instance_id=7`)
			case "stopped":
				_, err = sess.SQL().Exec(`UPDATE instances SET status='stopped' WHERE id=7`)
			case "port-conflict":
				_, err = sess.SQL().Exec(`INSERT INTO instance_runtime_bindings (instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES (8,5,'hermes','gw-8-1',20002,'/other','running',1)`)
			}
			if err != nil {
				t.Fatal(err)
			}
			type result struct {
				ok  bool
				err error
			}
			done := make(chan result, 1)
			if scenario == "deleted-before-lock" {
				err = sess.TxContext(ctx, func(tx db.Session) error {
					var id int
					row, e := tx.SQL().QueryRowContext(ctx, `SELECT id FROM instances WHERE id=7 FOR UPDATE`)
					if e != nil {
						return e
					}
					if e = row.Scan(&id); e != nil {
						return e
					}
					go func() { ok, e := repo.ReconcilePendingReportedGateway(ctx, report, 3, 3); done <- result{ok, e} }()
					_, e = tx.SQL().ExecContext(ctx, `DELETE FROM instance_runtime_bindings WHERE instance_id=7`)
					return e
				}, nil)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				ok, e := repo.ReconcilePendingReportedGateway(ctx, report, 3, 3)
				done <- result{ok, e}
			}
			r := <-done
			if r.err != nil || r.ok != (scenario == "valid") {
				t.Fatalf("result=%+v", r)
			}
			binding, e := repo.GetByInstanceID(ctx, 7)
			if e != nil {
				t.Fatal(e)
			}
			if scenario == "deleted-before-lock" && binding != nil {
				t.Fatal("deleted pending binding resurrected")
			}
			if scenario == "port-conflict" && (binding == nil || binding.GatewayID != "pending-7-2" || binding.State != "creating") {
				t.Fatal("conflict released pending slot")
			}
			if scenario == "valid" && (binding == nil || binding.GatewayID != "gw-7-2") {
				t.Fatal("exact binding not recovered")
			}
		})
	}
}
