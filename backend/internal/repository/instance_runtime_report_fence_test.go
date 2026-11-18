package repository

import (
	"context"
	"testing"
)

type reportedRuntimeStateWriter interface {
	UpdateReportedRuntimeStateForBinding(context.Context, int, string, int, *string, int64, string, int, string) error
}

func TestRuntimeReportedStateRecoveryFenceMySQL(t *testing.T) {
	r := idleRuntimeRepository(t)
	ctx := t.Context()
	writer, ok := interface{}(r).(reportedRuntimeStateWriter)
	if !ok {
		t.Fatal("reported runtime state fence capability missing")
	}
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := r.sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE TABLE instance_runtime_bindings(instance_id INT PRIMARY KEY,runtime_pod_id BIGINT,generation INT,gateway_id VARCHAR(128),gateway_port INT,workspace_path TEXT)")
	exec("INSERT INTO instance_runtime_bindings VALUES(7,11,2,'gw-7-2',20000,'/workspace')")
	exec("CREATE TABLE instance_lifecycle_manual_guards(instance_id INT PRIMARY KEY,token VARCHAR(64))")
	exec("CREATE TABLE runtime_management_operations(id VARCHAR(64) PRIMARY KEY,kind VARCHAR(24),status VARCHAR(24),request_json LONGTEXT)")
	exec("INSERT INTO runtime_management_operations VALUES('recovery','eviction','processing','{\"mode\":\"recovery\"}')")
	exec("INSERT INTO instance_lifecycle_manual_guards VALUES(7,'recovery')")
	state := func() string {
		t.Helper()
		var got string
		row, err := r.sess.SQL().QueryRow("SELECT status FROM instances WHERE id=7")
		if err != nil {
			t.Fatal(err)
		}
		if err = row.Scan(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	for _, status := range []string{"pending", "processing", "needs_attention"} {
		exec("UPDATE instances SET status='running' WHERE id=7")
		exec("UPDATE runtime_management_operations SET status=?", status)
		if err := writer.UpdateReportedRuntimeStateForBinding(ctx, 7, "stopped", 2, nil, 11, "gw-7-2", 20000, "/workspace"); err != nil {
			t.Fatal(err)
		}
		if got := state(); got != "running" {
			t.Fatalf("%s report changed intent to %s", status, got)
		}
	}
	// Manual stop still records business intent while the report fence is active.
	if err := r.UpdateRuntimeState(ctx, 7, "stopped", 2, nil); err != nil {
		t.Fatal(err)
	}
	if state() != "stopped" {
		t.Fatal("manual stop suppressed")
	}
	for _, change := range []string{
		"DELETE FROM instance_lifecycle_manual_guards WHERE instance_id=7",
		"INSERT INTO instance_lifecycle_manual_guards VALUES(7,'unselected-neighbor')",
		"UPDATE instance_lifecycle_manual_guards SET token='recovery' WHERE instance_id=7;",
	} {
		exec(change)
		exec("UPDATE instances SET status='running' WHERE id=7")
		if change == "UPDATE instance_lifecycle_manual_guards SET token='recovery' WHERE instance_id=7;" {
			exec("UPDATE runtime_management_operations SET status='completed'")
		}
		if err := writer.UpdateReportedRuntimeStateForBinding(ctx, 7, "stopped", 2, nil, 11, "gw-7-2", 20000, "/workspace"); err != nil {
			t.Fatal(err)
		}
		if state() != "stopped" {
			t.Fatal("normal/unselected/terminal report suppressed")
		}
	}
	exec("UPDATE runtime_management_operations SET status='processing',request_json='{\"mode\":\"force\"}'")
	exec("UPDATE instances SET status='running' WHERE id=7")
	if err := writer.UpdateReportedRuntimeStateForBinding(ctx, 7, "stopped", 2, nil, 11, "gw-7-2", 20000, "/workspace"); err != nil {
		t.Fatal(err)
	}
	if state() != "stopped" {
		t.Fatal("force mode incorrectly fenced")
	}
	// A report admitted before retirement cannot revive a retired same-generation
	// source after its guard disappears, nor describe a replacement binding.
	exec("DELETE FROM instance_lifecycle_manual_guards")
	for _, latest := range []string{"stopped", "deleting", "deleted"} {
		exec("UPDATE instances SET status=? WHERE id=7", latest)
		exec("DELETE FROM instance_runtime_bindings WHERE instance_id=7")
		if err := writer.UpdateReportedRuntimeStateForBinding(ctx, 7, "running", 2, nil, 11, "gw-7-2", 20000, "/workspace"); err != nil {
			t.Fatal(err)
		}
		if state() != latest {
			t.Fatalf("retired %s source resurrected", latest)
		}
		exec("INSERT INTO instance_runtime_bindings VALUES(7,12,2,'gw-7-2',20000,'/workspace')")
		if err := writer.UpdateReportedRuntimeStateForBinding(ctx, 7, "running", 2, nil, 11, "gw-7-2", 20000, "/workspace"); err != nil {
			t.Fatal(err)
		}
		if state() != latest {
			t.Fatalf("replacement binding revived %s source", latest)
		}
	}
	exec("UPDATE instances SET status='deleting' WHERE id=7")
	if err := writer.UpdateReportedRuntimeStateForBinding(ctx, 7, "running", 2, nil, 12, "gw-7-2", 20000, "/workspace"); err != nil {
		t.Fatal(err)
	}
	if state() != "deleting" {
		t.Fatal("matching report resurrected deleting intent")
	}
	exec("UPDATE instances SET status='running' WHERE id=7")
	for _, scope := range []struct {
		gateway   string
		port      int
		workspace string
	}{
		{"wrong", 20000, "/workspace"}, {"gw-7-2", 20003, "/workspace"}, {"gw-7-2", 20000, "/other"},
	} {
		if err := writer.UpdateReportedRuntimeStateForBinding(ctx, 7, "error", 2, nil, 12, scope.gateway, scope.port, scope.workspace); err != nil {
			t.Fatal(err)
		}
		if state() != "running" {
			t.Fatal("mismatched report scope accepted")
		}
	}
	if err := writer.UpdateReportedRuntimeStateForBinding(ctx, 7, "error", 2, nil, 12, "gw-7-2", 20000, "/workspace"); err != nil {
		t.Fatal(err)
	}
	if state() != "error" {
		t.Fatal("healthy matching binding report suppressed")
	}

}
