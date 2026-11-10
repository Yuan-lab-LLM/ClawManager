package services

import (
	"context"
	"errors"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

func TestReplacementResetExecutionRespectsOrphanGuardMySQL(t *testing.T) {
	sess := runtimeManagementMySQLDB(t)
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("ALTER TABLE team_members ADD instance_id INT,ADD team_id INT,ADD status VARCHAR(32)")
	exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation) VALUES(101,1,'openclaw','gateway','lite','stopped',1),(102,1,'openclaw','gateway','lite','running',1)")
	exec("INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES('orphan','orphan_recovery','processing','{}','[]',NOW(),NOW())")
	exec("INSERT INTO instance_lifecycle_manual_guards(instance_id,token,created_at) VALUES(101,'orphan',NOW())")
	repo := repository.NewInstanceRepository(sess)
	upgrade := &RuntimeUpgradeService{sess: sess, workspaceRoot: t.TempDir()}
	entered := 0
	service := &instanceService{instanceRepo: repo, runtimeUpgradeGuard: upgrade, ownerTeamMutationGuard: func(int, string, int) error { entered++; return errors.New("unguarded reset body reached") }}
	if err := upgrade.ValidateInstanceDeletion(t.Context(), 101); !errors.Is(err, models.ErrInstanceOperationInProgress) {
		t.Fatalf("reset deletion validator ignored orphan guard: %v", err)
	}
	assertBlocked := func() {
		t.Helper()
		if _, err := service.CreateResetReplacement(101, "queued-reset"); !errors.Is(err, models.ErrInstanceOperationInProgress) {
			t.Fatalf("queued reset create bypassed guard: %v", err)
		}
		if pending, warning, err := service.FinalizeResetReplacement(101, 102); !errors.Is(err, models.ErrInstanceOperationInProgress) || pending || warning != "" {
			t.Fatalf("replacement waiting cutover bypassed guard: %v/%q/%v", pending, warning, err)
		}
		if err := service.DiscardResetReplacement(101); !errors.Is(err, models.ErrInstanceOperationInProgress) {
			t.Fatalf("reset cleanup bypassed guard: %v", err)
		}
		if entered != 0 {
			t.Fatal("reset body ran before admission guard")
		}
	}
	assertBlocked()
	exec("UPDATE runtime_management_operations SET status='completed'")
	if err := upgrade.ValidateInstanceDeletion(t.Context(), 101); err != nil {
		t.Fatalf("completed recovery permanently blocked reset: %v", err)
	}
	// An already-admitted reset and an orphan submission share this exact lock.
	locker := repo.(interface {
		AcquireLifecycleLock(context.Context, int) (func(), error)
	})
	release, err := locker.AcquireLifecycleLock(t.Context(), 101)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	assertBlocked()
	release()
}
