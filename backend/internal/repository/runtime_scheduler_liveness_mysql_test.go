package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

func schedulingMySQL(t *testing.T) *instanceRepository {
	t.Helper()
	sess := upgradeMySQL(t)
	for _, q := range []string{
		`ALTER TABLE instances ADD user_id INT NOT NULL DEFAULT 1, ADD runtime_type VARCHAR(32) NOT NULL DEFAULT 'gateway', ADD workspace_path VARCHAR(1024) DEFAULT '/workspace', ADD description TEXT NULL`,
		`CREATE TABLE instance_lifecycle_manual_guards (instance_id INT PRIMARY KEY,token VARCHAR(255))`,
		`CREATE TABLE instance_desired_state (instance_id INT PRIMARY KEY,desired_power_state VARCHAR(32))`,
	} {
		if _, err := sess.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{"066_add_runtime_upgrade_fence.sql", "107_add_runtime_failover_intents.sql"} {
		raw, err := os.ReadFile("../db/migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		for _, q := range strings.Split(string(raw), ";") {
			if strings.TrimSpace(q) != "" {
				if _, err := sess.SQL().Exec(q); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	return &instanceRepository{sess: sess}
}

func TestSchedulerPageMySQLBoundedSnapshotIncludesLaterIDs(t *testing.T) {
	r := schedulingMySQL(t)
	ctx := context.Background()
	var values []string
	for id := 1; id <= 1001; id++ {
		values = append(values, fmt.Sprintf("(%d,'hermes','lite','running',1,NOW(6))", id))
	}
	if _, err := r.sess.SQL().Exec(`INSERT INTO instances(id,type,instance_mode,status,runtime_generation,updated_at) VALUES ` + strings.Join(values, ",")); err != nil {
		t.Fatal(err)
	}
	rows, upper, err := r.GetV2SchedulerPage(ctx, false, 0, 0, 1000)
	if err != nil || len(rows) != 1000 || rows[0].ID != 1 || rows[999].ID != 1000 || upper != 1001 {
		t.Fatalf("first page=%d upper=%d err=%v", len(rows), upper, err)
	}
	if _, err := r.sess.SQL().Exec(`INSERT INTO instances(id,type,instance_mode,status,runtime_generation,updated_at) VALUES (1002,'hermes','lite','creating',1,NOW(6))`); err != nil {
		t.Fatal(err)
	}
	rows, end, err := r.GetV2SchedulerPage(ctx, false, 1000, upper, 1000)
	if err != nil || len(rows) != 1 || rows[0].ID != 1001 || end != upper {
		t.Fatalf("later IDs starved or snapshot grew: rows=%+v upper=%d err=%v", rows, end, err)
	}
	rows, upper, err = r.GetV2SchedulerPage(ctx, true, 0, 0, 1000)
	if err != nil || len(rows) != 1 || rows[0].ID != 1002 || upper != 1002 {
		t.Fatalf("creating scan did not use separate snapshot: %+v %d %v", rows, upper, err)
	}
}

func TestRuntimeFailoverMySQLSourceProofAndAtomicContinuation(t *testing.T) {
	r := schedulingMySQL(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := r.sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO instances(id,type,instance_mode,status,runtime_generation) VALUES (17,'hermes','lite','running',3)`)
	exec(`INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,agent_endpoint,state,capacity,used_slots) VALUES (9,'hermes','runtime','source','source-uid','hermes-runtime','repo/image','https://source:19090','unhealthy',1,1)`)
	exec(`INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES(17,9,'hermes','gw-17-3',20000,'/workspace','running',3)`)
	applyIdleStopFixtureSchema(t, r.sess)
	state := models.RuntimeFailoverIntent{InstanceID: 17, UserID: 1, Generation: 3, RuntimeType: "hermes", SourcePodID: 9, SourcePodUID: "source-uid", SourceEndpoint: "https://source:19090", GatewayID: "gw-17-3", GatewayPort: 20000, WorkspacePath: "/workspace", Reason: "confirmed stale pod"}
	got, err := r.BeginRuntimeFailover(ctx, state)
	if err != nil || got == nil {
		t.Fatalf("persist intent: %+v %v", got, err)
	}
	if advanced, err := r.AdvanceRuntimeFailover(ctx, state); err != nil || advanced {
		t.Fatalf("unconfirmed stop advanced=%v err=%v", advanced, err)
	}
	state.SourceStopped = true
	state.Attempts = 5
	if err := r.SaveRuntimeFailover(ctx, state); err != nil {
		t.Fatal(err)
	}
	// Establish a repeatable-read snapshot, then commit desired stop on another
	// connection without changing instances.status. Stop priority needs current
	// locking reads even after the journal was read in this transaction.
	if err := r.sess.TxContext(ctx, func(tx db.Session) error {
		var prior models.RuntimeFailoverIntent
		if err := tx.Collection("runtime_failover_intents").Find(db.Cond{"instance_id": 17}).One(&prior); err != nil {
			return err
		}
		exec(`INSERT INTO instance_desired_state VALUES(17,'stopped')`)
		valid, err := runtimeFailoverCurrent(ctx, tx, state)
		if valid {
			t.Errorf("repeatable-read snapshot hid concurrently committed desired stop: %v", err)
		}
		// MariaDB may return a serialization conflict for a current read of a
		// newly inserted row. That also retains source ownership safely.
		return nil
	}, nil); err != nil {
		t.Fatal(err)
	}
	exec(`DELETE FROM instance_desired_state`)
	if err := r.sess.TxContext(ctx, func(tx db.Session) error {
		var prior models.RuntimeFailoverIntent
		if err := tx.Collection("runtime_failover_intents").Find(db.Cond{"instance_id": 17}).One(&prior); err != nil {
			return err
		}
		exec(`INSERT INTO instance_model_activity(instance_id,generation,monitoring_since,stop_operation_id) VALUES(17,3,NOW(6),'pending-idle-stop')`)
		valid, err := runtimeFailoverCurrent(ctx, tx, state)
		if valid {
			t.Errorf("repeatable-read snapshot hid concurrently committed idle stop: %v", err)
		}
		return nil
	}, nil); err != nil {
		t.Fatal(err)
	}
	exec(`DELETE FROM instance_model_activity WHERE instance_id=17`)
	for _, mutation := range []string{
		`UPDATE instances SET user_id=2 WHERE id=17`,
		`UPDATE instances SET status='stopped' WHERE id=17`,
		`UPDATE runtime_pods SET pod_uid='SOURCE-UID' WHERE id=9`,
		`UPDATE instance_runtime_bindings SET gateway_port=20001 WHERE instance_id=17`,
		`UPDATE instance_runtime_bindings SET gateway_id='GW-17-3' WHERE instance_id=17`,
		`INSERT INTO instance_desired_state VALUES(17,'stopped')`,
	} {
		exec(mutation)
		if advanced, err := r.AdvanceRuntimeFailover(ctx, state); err != nil || advanced {
			t.Fatalf("changed authority advanced: %s %v %v", mutation, advanced, err)
		}
		exec(`UPDATE instances SET user_id=1,status='running' WHERE id=17`)
		exec(`UPDATE runtime_pods SET pod_uid='source-uid' WHERE id=9`)
		exec(`UPDATE instance_runtime_bindings SET gateway_id='gw-17-3',gateway_port=20000 WHERE instance_id=17`)
		exec(`DELETE FROM instance_desired_state`)
	}
	// Interrupt the transaction after generation advancement. SQL must roll it all
	// back while the independent confirmed-stop checkpoint remains durable.
	exec(`CREATE TRIGGER fail_binding_delete BEFORE DELETE ON instance_runtime_bindings FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected delete failure'`)
	if advanced, err := r.AdvanceRuntimeFailover(ctx, state); err == nil || advanced {
		t.Fatal("injected transaction failure was ignored")
	}
	journal, err := (&instanceRepository{sess: r.sess}).GetRuntimeFailover(ctx, 17)
	if err != nil || journal == nil || !journal.SourceStopped || journal.Attempts != 5 {
		t.Fatalf("restart lost final stop checkpoint: %+v %v", journal, err)
	}
	var generation int
	row, err := r.sess.SQL().QueryRowContext(ctx, `SELECT runtime_generation FROM instances WHERE id=17`)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&generation); err != nil || generation != 3 {
		t.Fatalf("failed transaction advanced generation=%d %v", generation, err)
	}
	exec(`DROP TRIGGER fail_binding_delete`)
	advanced, err := r.AdvanceRuntimeFailover(ctx, *journal)
	if err != nil || !advanced {
		t.Fatalf("confirmed checkpoint failed to continue: %v %v", advanced, err)
	}
	var bindings, slots, intents int
	row, err = r.sess.SQL().QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM instance_runtime_bindings),(SELECT used_slots FROM runtime_pods WHERE id=9),(SELECT COUNT(*) FROM runtime_failover_intents)`)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&bindings, &slots, &intents); err != nil || bindings != 0 || slots != 0 || intents != 0 {
		t.Fatalf("atomic release incomplete: %d %d %d %v", bindings, slots, intents, err)
	}
	// A separately completed Stop/Start may supersede a retained old journal.
	// Generation advancement alone cannot retire it; the exact current binding
	// and unchanged owner are checked again in the retirement transaction.
	state.UpdatedAt = time.Now().UTC()
	if _, err := r.sess.Collection("runtime_failover_intents").Insert(state); err != nil {
		t.Fatal(err)
	}
	successor := state
	successor.Generation = 4
	successor.GatewayID = "gw-17-4"
	if retired, err := r.RetireRuntimeFailover(ctx, state, successor); err != nil || retired {
		t.Fatalf("bare new generation retired source: %v %v", retired, err)
	}
	exec(`INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES(17,9,'hermes','gw-17-4',20000,'/workspace','error',4)`)
	exec(`UPDATE instances SET user_id=2 WHERE id=17`)
	if retired, err := r.RetireRuntimeFailover(ctx, state, successor); err != nil || retired {
		t.Fatalf("changed owner retired source: %v %v", retired, err)
	}
	exec(`UPDATE instances SET user_id=1 WHERE id=17`)
	if retired, err := r.RetireRuntimeFailover(ctx, state, successor); err != nil || !retired {
		t.Fatalf("exact successor could not retire source: %v %v", retired, err)
	}
	if current, err := r.BeginRuntimeFailover(ctx, successor); err != nil || current.Generation != 4 {
		t.Fatalf("new failed generation still blocked by old source: %+v %v", current, err)
	}
}
