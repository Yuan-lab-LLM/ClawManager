package services

import (
	"errors"
	"testing"

	"clawreef/internal/models"
)

func TestWorkspaceMutationRejectsActiveOrphanReservationMySQL(t *testing.T) {
	sess := runtimeManagementMySQLDB(t)
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("ALTER TABLE team_members ADD instance_id INT,ADD team_id INT,ADD status VARCHAR(32)")
	exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation) VALUES(101,1,'openclaw','gateway','lite','stopped',1),(102,1,'openclaw','gateway','lite','stopped',1)")
	exec("INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES('orphan','orphan_recovery','processing','{}','[]',NOW(),NOW())")
	exec("INSERT INTO instance_lifecycle_manual_guards(instance_id,token,created_at) VALUES(101,'orphan',NOW())")
	service := &RuntimeUpgradeService{sess: sess, workspaceRoot: t.TempDir()}
	writes := 0
	mutate := func() error { writes++; return nil }
	if err := service.WithInstanceMutation(t.Context(), 101, mutate); !errors.Is(err, models.ErrInstanceOperationInProgress) || writes != 0 {
		t.Fatalf("instance mutation crossed orphan fence: err=%v writes=%d", err, writes)
	}
	if err := service.WithWorkspaceMutation(t.Context(), WorkspaceFileScope{InstanceID: 101}, mutate); !errors.Is(err, models.ErrInstanceOperationInProgress) || writes != 0 {
		t.Fatalf("workspace mutation crossed orphan fence: err=%v writes=%d", err, writes)
	}
	if err := service.WithInstanceMutation(t.Context(), 102, mutate); err != nil || writes != 1 {
		t.Fatalf("healthy neighbor blocked: %v writes=%d", err, writes)
	}
	exec("UPDATE runtime_management_operations SET status='completed'")
	if err := service.WithInstanceMutation(t.Context(), 101, mutate); err != nil || writes != 2 {
		t.Fatalf("completed reservation blocked mutation: %v writes=%d", err, writes)
	}
	exec("DROP TABLE runtime_management_operations")
	if err := service.WithInstanceMutation(t.Context(), 101, mutate); err == nil || writes != 2 {
		t.Fatalf("missing inventory failed open: %v writes=%d", err, writes)
	}
}
