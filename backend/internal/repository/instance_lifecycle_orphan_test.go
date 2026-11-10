package repository

import (
	"errors"
	"testing"

	"clawreef/internal/models"
)

func TestLifecycleLockRejectsActiveOrphanRecoveryMySQL(t *testing.T) {
	r := runtimeSecurityMySQL(t)
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, err := r.sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE TABLE instance_lifecycle_manual_guards(instance_id INT PRIMARY KEY,token VARCHAR(64))")
	exec("CREATE TABLE runtime_management_operations(id VARCHAR(64) PRIMARY KEY,kind VARCHAR(24),status VARCHAR(24))")
	exec("INSERT INTO runtime_management_operations VALUES('orphan','orphan_recovery','pending')")
	exec("INSERT INTO instance_lifecycle_manual_guards VALUES(7,'orphan')")
	for _, status := range []string{"pending", "processing", "starting", "needs_attention"} {
		exec("UPDATE runtime_management_operations SET status=?", status)
		release, err := r.AcquireLifecycleLock(t.Context(), 7)
		if release != nil {
			release()
		}
		if !errors.Is(err, models.ErrInstanceOperationInProgress) {
			t.Fatalf("%s orphan guard admitted lifecycle mutation: %v", status, err)
		}
		// Rejection must release the advisory connection, including on this path.
		unlock, err := r.acquireLifecycleLock(t.Context(), 7)
		if err != nil {
			t.Fatalf("rejected guard leaked advisory lock: %v", err)
		}
		unlock()
	}
	for _, status := range []string{"completed", "cancelled", "failed", "partial_failure"} {
		exec("UPDATE runtime_management_operations SET status=?", status)
		release, err := r.AcquireLifecycleLock(t.Context(), 7)
		if err != nil {
			t.Fatalf("terminal %s blocked native lifecycle: %v", status, err)
		}
		release()
	}
	exec("UPDATE runtime_management_operations SET kind='eviction',status='processing'")
	release, err := r.AcquireLifecycleLock(t.Context(), 7)
	if err != nil {
		t.Fatalf("bound recovery's own token was blocked: %v", err)
	}
	release()
	exec("DELETE FROM instance_lifecycle_manual_guards")
	release, err = r.AcquireLifecycleLock(t.Context(), 7)
	if err != nil {
		t.Fatal(err)
	}
	release()
	exec("DROP TABLE runtime_management_operations")
	release, err = r.AcquireLifecycleLock(t.Context(), 7)
	if release != nil {
		release()
	}
	if err == nil {
		t.Fatal("missing reservation inventory failed open")
	}
	unlock, err := r.acquireLifecycleLock(t.Context(), 7)
	if err != nil {
		t.Fatal("inventory failure leaked advisory lock")
	}
	unlock()
}
