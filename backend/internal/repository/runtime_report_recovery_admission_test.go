package repository

import (
	"clawreef/internal/models"
	"context"
	"testing"
)

func TestLegacyReportedGatewayCannotAdoptRetiredRecoveryGenerationMySQL(t *testing.T) {
	r := protectedReportRepository(t)
	ctx := context.Background()
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := r.sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("ALTER TABLE runtime_management_operations ADD items_json LONGTEXT")
	report := &models.InstanceRuntimeBinding{InstanceID: 7, RuntimePodID: 5, RuntimeType: "openclaw", GatewayID: "gw-7-2", GatewayPort: 20000, WorkspacePath: "/workspaces/openclaw/user-3/instance-7", State: "running", Generation: 2}
	reset := func() {
		exec("DELETE FROM instance_runtime_bindings")
		exec("UPDATE instances SET type='openclaw',runtime_security_profile='',status='creating',runtime_generation=3 WHERE id=7")
	}
	reset()
	ok, err := r.ReconcileReportedGateway(ctx, report, 3, 3)
	if err != nil || !ok {
		t.Fatalf("legitimate legacy adoption=%v err=%v", ok, err)
	}
	for _, phase := range []string{"planned", "stopping", "source_stopped", "released"} {
		reset()
		exec("DELETE FROM runtime_management_operations")
		exec(`INSERT INTO runtime_management_operations(kind,status,request_json,items_json) VALUES('eviction',?,'{"mode":"recovery"}',?)`, map[bool]string{true: "processing", false: "completed"}[phase == "planned"], `[{"instance_id":7,"source_generation":2,"source_pod_id":5,"source_gateway_id":"gw-7-2","recovery":{"phase":"`+phase+`"}}]`)
		ok, err = r.ReconcileReportedGateway(ctx, report, 3, 3)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatalf("%s old source adopted", phase)
		}
		var generation, count int
		var status string
		row, _ := r.sess.SQL().QueryRow("SELECT status,runtime_generation FROM instances WHERE id=7")
		if err = row.Scan(&status, &generation); err != nil {
			t.Fatal(err)
		}
		row, _ = r.sess.SQL().QueryRow("SELECT COUNT(*) FROM instance_runtime_bindings WHERE instance_id=7")
		if err = row.Scan(&count); err != nil {
			t.Fatal(err)
		}
		if status != "creating" || generation != 3 || count != 0 {
			t.Fatalf("retired source resurrected %s/%d bindings=%d", status, generation, count)
		}
	}
	// A current successor stays eligible; retirement applies to the old generation.
	report.Generation = 3
	report.GatewayID = "gw-7-3"
	ok, err = r.ReconcileReportedGateway(ctx, report, 3, 3)
	if err != nil || !ok {
		t.Fatalf("successor adoption=%v err=%v", ok, err)
	}
}
