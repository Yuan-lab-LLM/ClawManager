package services

import (
	"errors"
	"testing"
)

func TestDeletingInstanceRecoveryPreservesUpgradeFencesMySQL(t *testing.T) {
	sess := runtimeManagementMySQLDB(t)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("ALTER TABLE team_members ADD instance_id INT,ADD team_id INT,ADD status VARCHAR(32)")
	exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation) VALUES(101,1,'openclaw','gateway','lite','deleting',1)")
	exec("INSERT INTO runtime_rollouts(id,runtime_type,target_image_ref,status) VALUES(1,'openclaw','source','error')")
	exec("INSERT INTO runtime_upgrade_items(rollout_id,instance_id,state,recovery_status,created_at,updated_at) VALUES(1,101,'prepared','needs_attention',NOW(),NOW())")
	u := &RuntimeUpgradeService{sess: sess, workspaceRoot: t.TempDir()}
	calls := 0
	stop := func() error { calls++; return nil }
	if err := u.WithInstanceMutation(t.Context(), 101, stop); !errors.Is(err, ErrWorkspaceUpgradeBusy) || calls != 0 {
		t.Fatalf("ordinary mutation crossed failed-upgrade fence: %v calls=%d", err, calls)
	}
	if err := u.withDeletingInstanceRecovery(t.Context(), 101, stop); err != nil || calls != 1 {
		t.Fatalf("deleting source cannot reach physical exit proof: %v calls=%d", err, calls)
	}
	if err := u.WithInstanceMutation(t.Context(), 101, stop); !errors.Is(err, ErrWorkspaceUpgradeBusy) || calls != 1 {
		t.Fatal("fault recovery removed the failed-upgrade fence", err)
	}
	exec("UPDATE runtime_rollouts SET status='running' WHERE id=1")
	if err := u.withDeletingInstanceRecovery(t.Context(), 101, stop); !errors.Is(err, ErrWorkspaceUpgradeBusy) || calls != 1 {
		t.Fatal("fault recovery crossed active upgrade", err)
	}
	exec("UPDATE runtime_rollouts SET status='error' WHERE id=1")
	exec("UPDATE instances SET status='stopped' WHERE id=101")
	if err := u.withDeletingInstanceRecovery(t.Context(), 101, stop); !errors.Is(err, ErrWorkspaceUpgradeBusy) || calls != 1 {
		t.Fatal("missing delete intent authorized teardown", err)
	}
	exec("UPDATE instances SET status='deleting' WHERE id=101")
	exec("INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES('orphan','orphan_recovery','processing','{}','[]',NOW(),NOW())")
	exec("INSERT INTO instance_lifecycle_manual_guards(instance_id,token,created_at) VALUES(101,'orphan',NOW())")
	if err := u.withDeletingInstanceRecovery(t.Context(), 101, stop); err == nil || calls != 1 {
		t.Fatal("fault recovery crossed independent orphan reservation")
	}
	exec("DELETE FROM instance_lifecycle_manual_guards WHERE instance_id=101")
	exec("DROP TABLE runtime_upgrade_items")
	if err := u.withDeletingInstanceRecovery(t.Context(), 101, stop); err == nil || calls != 1 {
		t.Fatal("failed upgrade inventory read admitted teardown")
	}
}
