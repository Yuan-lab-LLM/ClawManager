package repository

import (
	"clawreef/internal/models"
	"context"
	"testing"
)

func TestStoppedSlotBindingMySQLRequiresExactStoppedOwner(t *testing.T) {
	r := runtimeSecurityMySQL(t)
	for _, ddl := range []string{`CREATE TABLE runtime_pods(id BIGINT PRIMARY KEY,used_slots INT,updated_at DATETIME(6))`, `CREATE TABLE instance_runtime_bindings(instance_id INT PRIMARY KEY,runtime_pod_id BIGINT,generation INT,gateway_id VARCHAR(255),gateway_port INT,state VARCHAR(32))`, `INSERT INTO runtime_pods VALUES(9,1,NOW(6))`} {
		if _, err := r.sess.SQL().Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	b := &instanceRuntimeBindingRepository{sess: r.sess}
	expected := models.Instance{ID: 7, UserID: 3, RuntimeGeneration: 2, Status: "stopped"}
	binding := models.InstanceRuntimeBinding{InstanceID: 7, RuntimePodID: 9, Generation: 2, GatewayID: "gw-7-2", GatewayPort: 20000, State: "running"}
	for _, scenario := range []string{"running-instance", "changed-user", "changed-generation", "changed-gateway-case", "changed-port", "exact-stale-running-binding"} {
		t.Run(scenario, func(t *testing.T) {
			for _, sql := range []string{`DELETE FROM instance_runtime_bindings`, `UPDATE instances SET status='stopped',runtime_generation=2,user_id=3 WHERE id=7`, `UPDATE runtime_pods SET used_slots=1 WHERE id=9`, `INSERT INTO instance_runtime_bindings VALUES(7,9,2,'gw-7-2',20000,'running')`} {
				if _, err := r.sess.SQL().Exec(sql); err != nil {
					t.Fatal(err)
				}
			}
			mutations := map[string]string{"running-instance": `UPDATE instances SET status='running' WHERE id=7`, "changed-user": `UPDATE instances SET user_id=4 WHERE id=7`, "changed-generation": `UPDATE instances SET runtime_generation=3 WHERE id=7`, "changed-gateway-case": `UPDATE instance_runtime_bindings SET gateway_id='GW-7-2' WHERE instance_id=7`, "changed-port": `UPDATE instance_runtime_bindings SET gateway_port=20001 WHERE instance_id=7`}
			if sql := mutations[scenario]; sql != "" {
				if _, err := r.sess.SQL().Exec(sql); err != nil {
					t.Fatal(err)
				}
			}
			deleted, err := b.DeleteConfirmedStoppedSlotBinding(context.Background(), expected, binding)
			if err != nil || deleted != (scenario == "exact-stale-running-binding") {
				t.Fatalf("stopped owner release predicate failed: deleted=%v err=%v", deleted, err)
			}
			var remaining, used int
			row, err := r.sess.SQL().QueryRow(`SELECT COUNT(*) FROM instance_runtime_bindings`)
			if err != nil {
				t.Fatal(err)
			}
			if err = row.Scan(&remaining); err != nil {
				t.Fatal(err)
			}
			row, err = r.sess.SQL().QueryRow(`SELECT used_slots FROM runtime_pods WHERE id=9`)
			if err != nil {
				t.Fatal(err)
			}
			if err = row.Scan(&used); err != nil {
				t.Fatal(err)
			}
			expectedCount := 1
			if deleted {
				expectedCount = 0
			}
			if remaining != expectedCount || used != expectedCount {
				t.Fatalf("slot capacity diverged from exact binding release: remaining=%d used=%d", remaining, used)
			}

		})
	}
}
