package repository

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

func protectedReportRepository(t *testing.T) *instanceRuntimeBindingRepository {
	t.Helper()
	instances := runtimeSecurityMySQL(t)
	for _, q := range []string{
		`CREATE TABLE instance_desired_state(instance_id INT PRIMARY KEY,desired_power_state VARCHAR(32))`,
		`CREATE TABLE runtime_management_admission_lock(id INT PRIMARY KEY,revision BIGINT NOT NULL DEFAULT 0)`,
		`INSERT INTO runtime_management_admission_lock(id) VALUES (1)`,
		`CREATE TABLE runtime_management_operations(kind VARCHAR(24),status VARCHAR(24),request_json LONGTEXT)`,
		`CREATE TABLE runtime_pods(id BIGINT PRIMARY KEY,namespace VARCHAR(128),deployment_name VARCHAR(255),pod_uid VARCHAR(128),runtime_type VARCHAR(40),capacity INT,used_slots INT,updated_at DATETIME(6))`,
		`INSERT INTO runtime_pods VALUES(5,'runtime','pool','pod-current','hermes',8,0,NOW(6))`,
		`CREATE TABLE instance_runtime_bindings (id BIGINT PRIMARY KEY AUTO_INCREMENT,instance_id BIGINT NOT NULL UNIQUE,runtime_pod_id BIGINT,runtime_type VARCHAR(40),gateway_id VARCHAR(80),gateway_port INT,gateway_pid INT NULL,workspace_path TEXT,state VARCHAR(20),generation INT,last_health_at DATETIME(6) NULL,error_message TEXT NULL,created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6))`,
		`UPDATE instances SET status='running',runtime_generation=2,type='hermes',instance_mode='lite',runtime_type='gateway',runtime_security_profile='isolated-network-v1',workspace_path='/workspaces/hermes/user-3/instance-7' WHERE id=7`,
	} {
		if _, err := instances.sess.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	migration, err := os.ReadFile("../db/migrations/080_add_runtime_pod_deletion_intents.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range strings.Split(string(migration), ";") {
		if strings.TrimSpace(q) != "" {
			if _, err := instances.sess.SQL().Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	}
	return &instanceRuntimeBindingRepository{sess: instances.sess}
}

func TestUnboundProtectedReportedGatewayPersistence(t *testing.T) {
	repo := protectedReportRepository(t)
	for _, scenario := range []string{"valid", "creating", "stopped", "deleting", "error", "generation", "owner", "profile", "pod-uid", "runtime", "existing-binding", "port-conflict", "capacity", "upgrade", "desired-stop", "workspace", "stop-operation"} {
		t.Run(scenario, func(t *testing.T) {
			for _, q := range []string{`DELETE FROM instance_desired_state`, `DELETE FROM instance_model_activity`, `DELETE FROM instance_runtime_bindings`, `DELETE FROM gateway_runtime_upgrades`, `UPDATE instances SET user_id=3,status='running',runtime_generation=2,type='hermes',instance_mode='lite',runtime_type='gateway',runtime_security_profile='isolated-network-v1',workspace_path='/workspaces/hermes/user-3/instance-7' WHERE id=7`, `UPDATE runtime_pods SET pod_uid='pod-current',runtime_type='hermes',capacity=8,used_slots=0 WHERE id=5`} {
				if _, err := repo.sess.SQL().Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			ctx := context.Background()
			report := &models.InstanceRuntimeBinding{InstanceID: 7, RuntimePodID: 5, RuntimeType: "hermes", GatewayID: "gw-7-2", GatewayPort: 20001, WorkspacePath: "/workspaces/hermes/user-3/instance-7", State: "running", Generation: 2}
			query := ""
			switch scenario {
			case "valid":
				query = `UPDATE instances SET runtime_error_code='INVALID_CONFIGURATION',runtime_error_message='stale failure' WHERE id=7`
			case "creating", "stopped", "deleting", "error":
				query = "UPDATE instances SET status='" + scenario + "' WHERE id=7"
			case "generation":
				query = `UPDATE instances SET runtime_generation=3 WHERE id=7`
			case "owner":
				query = `UPDATE instances SET user_id=4 WHERE id=7`
			case "profile":
				query = `UPDATE instances SET runtime_security_profile='future-profile' WHERE id=7`
			case "pod-uid":
				query = `UPDATE runtime_pods SET pod_uid='replaced' WHERE id=5`
			case "runtime":
				query = `UPDATE runtime_pods SET runtime_type='openclaw' WHERE id=5`
			case "existing-binding":
				query = `INSERT INTO instance_runtime_bindings (instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES (7,6,'hermes','gw-7-2',20001,'/other','running',2)`
			case "port-conflict":
				query = `INSERT INTO instance_runtime_bindings (instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES (8,5,'hermes','gw-8-1',20002,'/other','running',1)`
			case "desired-stop":
				query = `INSERT INTO instance_desired_state VALUES(7,'stopped')`
			case "workspace":
				query = `UPDATE instances SET workspace_path='/other' WHERE id=7`
			case "stop-operation":
				query = `INSERT INTO instance_model_activity(instance_id,generation,monitoring_since,stop_operation_id,updated_at) VALUES(7,2,NOW(6),'manual-stop',NOW(6))`
			case "capacity":
				query = `UPDATE runtime_pods SET capacity=0 WHERE id=5`
			case "upgrade":
				query = `INSERT INTO gateway_runtime_upgrades(instance_id,operation_id,owner_user_id,generation,state,created_at,updated_at) VALUES(7,'0123456789abcdef0123456789abcdef',3,2,'reserved',NOW(6),NOW(6))`
			}
			if query != "" {
				if _, err := repo.sess.SQL().Exec(query); err != nil {
					t.Fatal(err)
				}
			}
			ok, err := repo.ReconcileUnboundProtectedReportedGateway(ctx, report, 3, "pod-current", 3)
			valid := scenario == "valid" || scenario == "creating"
			if ok != valid || (err != nil && scenario != "upgrade" && scenario != "stop-operation") {
				t.Fatalf("reconciled=%v err=%v", ok, err)
			}
			binding, e := repo.GetByInstanceID(ctx, 7)
			if e != nil {
				t.Fatal(e)
			}
			if valid {
				instance, err := (&instanceRepository{repo.sess}).GetByIDContext(ctx, 7)
				if err != nil || instance.RuntimeErrorCode != nil || instance.RuntimeErrorMessage != nil {
					t.Fatalf("healthy adoption retained stale failure metadata: code=%v message=%v err=%v", instance.RuntimeErrorCode, instance.RuntimeErrorMessage, err)
				}
				if binding == nil || binding.GatewayID != "gw-7-2" || binding.Generation != 2 {
					t.Fatalf("binding=%+v", binding)
				}
				var used int
				row, e := repo.sess.SQL().QueryRow(`SELECT used_slots FROM runtime_pods WHERE id=5`)
				if e != nil {
					t.Fatal(e)
				}
				if e = row.Scan(&used); e != nil || used != 1 {
					t.Fatalf("used=%d err=%v", used, e)
				}
			} else if scenario != "existing-binding" && binding != nil {
				t.Fatalf("rejected report adopted: %+v", binding)
			}
		})
	}
}

func TestProtectedMissingReportMarkerPreservesStateAndFencesFreshReports(t *testing.T) {
	repo := protectedReportRepository(t)
	for _, scenario := range []string{"valid", "fresh-health", "fresh-update", "new-generation", "new-pod", "new-gateway"} {
		t.Run(scenario, func(t *testing.T) {
			if _, err := repo.sess.SQL().Exec(`DELETE FROM instance_runtime_bindings`); err != nil {
				t.Fatal(err)
			}
			old := time.Now().UTC().Add(-time.Hour)
			_, err := repo.sess.SQL().Exec(`INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation,last_health_at,created_at,updated_at)VALUES(7,5,'hermes','gw-7-2',20001,'/workspaces/hermes/user-3/instance-7','running',2,?,?,?)`, old, old, old)
			if err != nil {
				t.Fatal(err)
			}
			report, err := repo.GetByInstanceID(context.Background(), 7)
			if err != nil {
				t.Fatal(err)
			}
			query := ""
			switch scenario {
			case "fresh-health":
				query = `UPDATE instance_runtime_bindings SET last_health_at=NOW(6) WHERE instance_id=7`
			case "fresh-update":
				query = `UPDATE instance_runtime_bindings SET updated_at=NOW(6) WHERE instance_id=7`
			case "new-generation":
				query = `UPDATE instance_runtime_bindings SET generation=3 WHERE instance_id=7`
			case "new-pod":
				query = `UPDATE instance_runtime_bindings SET runtime_pod_id=6 WHERE instance_id=7`
			case "new-gateway":
				query = `UPDATE instance_runtime_bindings SET gateway_id='gw-7-other' WHERE instance_id=7`
			}
			if query != "" {
				if _, err = repo.sess.SQL().Exec(query); err != nil {
					t.Fatal(err)
				}
			}
			marked, err := repo.MarkProtectedGatewayReportMissing(context.Background(), *report, time.Now().UTC().Add(-time.Minute), "missing; ownership retained")
			if err != nil || marked != (scenario == "valid") {
				t.Fatalf("marked=%v err=%v", marked, err)
			}
			current, err := repo.GetByInstanceID(context.Background(), 7)
			if err != nil {
				t.Fatal(err)
			}
			if current == nil || current.State != "running" || (current.ErrorMessage != nil) != (scenario == "valid") {
				t.Fatalf("current=%+v", current)
			}
		})
	}
}

func TestProtectedReportAdoptionCannotUndoConcurrentStop(t *testing.T) {
	for _, intent := range []string{"status", "desired-only"} {
		t.Run(intent, func(t *testing.T) {
			repo := protectedReportRepository(t)
			ctx := context.Background()
			report := &models.InstanceRuntimeBinding{InstanceID: 7, RuntimePodID: 5, RuntimeType: "hermes", GatewayID: "gw-7-2", GatewayPort: 20001, WorkspacePath: "/workspaces/hermes/user-3/instance-7", State: "running", Generation: 2}
			type result struct {
				adopted bool
				err     error
			}
			done := make(chan result, 1)
			err := repo.sess.TxContext(ctx, func(tx db.Session) error {
				var id int
				row, e := tx.SQL().QueryRowContext(ctx, `SELECT id FROM instances WHERE id=7 FOR UPDATE`)
				if e != nil {
					return e
				}
				if e = row.Scan(&id); e != nil {
					return e
				}
				go func() {
					ok, e := repo.ReconcileUnboundProtectedReportedGateway(ctx, report, 3, "pod-current", 3)
					done <- result{ok, e}
				}()
				if intent == "desired-only" {
					_, e = tx.SQL().ExecContext(ctx, `INSERT INTO instance_desired_state VALUES(7,'stopped')`)
				} else {
					_, e = tx.SQL().ExecContext(ctx, `UPDATE instances SET status='stopped' WHERE id=7`)
				}
				return e
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			r := <-done
			if r.err != nil || r.adopted {
				t.Fatalf("adoption after stop=%v err=%v", r.adopted, r.err)
			}
			binding, err := repo.GetByInstanceID(ctx, 7)
			if err != nil || binding != nil {
				t.Fatalf("stopped binding reconstructed: %+v %v", binding, err)
			}
		})
	}
}
