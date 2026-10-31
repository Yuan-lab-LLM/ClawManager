package systembackup

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestControllerLeaderFenceAndSafeUnstartedRecovery(t *testing.T) {
	ctx := context.Background()
	s := Store{controlDB(t), "controller-test"}
	if e := s.Bootstrap(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SaveConfig(ctx, 0, 1, testConfig(), permitConfig); e != nil {
		t.Fatal(e)
	}
	task, _, e := s.Create(ctx, 1, "controller-0001", CreateTaskRequest{Type: Backup, Purpose: "functional_test"}, permitTask)
	if e != nil {
		t.Fatal(e)
	}
	first, e := s.AcquireLeadership(ctx, "leader-a", 30*time.Second)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.AcquireLeadership(ctx, "leader-b", 30*time.Second); !errors.Is(e, ErrFence) {
		t.Fatal("two leaders", e)
	}
	claimed, e := s.ClaimLedTask(ctx, first, task.ID, task.Version, 30*time.Second)
	if e != nil || claimed.StartedAt != nil {
		t.Fatal("claim falsely started execution", claimed, e)
	}
	if _, e = s.DB.Exec("UPDATE system_backup_controller_leases SET expires_at=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE scope=?", s.InstallationID); e != nil {
		t.Fatal(e)
	}
	second, e := s.AcquireLeadership(ctx, "leader-b", 30*time.Second)
	if e != nil || second.Fence <= first.Fence {
		t.Fatal("leader takeover lost fence", e)
	}
	if e = s.SetPendingReason(ctx, first, claimed, "dependency_backoff"); !errors.Is(e, ErrFence) {
		t.Fatal("stale leader mutated queue", e)
	}
	if _, e = s.ClaimLedTask(ctx, second, claimed.ID, claimed.Version, 30*time.Second); !errors.Is(e, ErrFence) {
		t.Fatal("unexpired task lease stolen", e)
	}
	if _, e = s.DB.Exec("UPDATE system_backup_tasks SET lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE public_id=?", claimed.ID); e != nil {
		t.Fatal(e)
	}
	next, e := s.ClaimLedTask(ctx, second, claimed.ID, claimed.Version, 30*time.Second)
	if e != nil {
		t.Fatal(e)
	}
	if !next.DeadlineAt.Equal(task.DeadlineAt) {
		t.Fatal("takeover extended deadline")
	}
	canceled, e := s.Cancel(ctx, next.ID, next.Version, 1)
	if e != nil {
		t.Fatal(e)
	}
	settled, e := s.ReconcileUnstarted(ctx, second, canceled)
	if e != nil || settled.Status != "canceled" || settled.StartedAt != nil {
		t.Fatal("safe pending cancel did not settle", settled, e)
	}
	expired, _, e := s.Create(ctx, 1, "controller-0002", CreateTaskRequest{Type: Backup, Purpose: "functional_test"}, permitTask)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("UPDATE system_backup_tasks SET deadline_at=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE public_id=?", expired.ID); e != nil {
		t.Fatal(e)
	}
	controller := Controller{Store: s, Owner: "leader-b", PollInterval: time.Second}
	if e = controller.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	expired, e = s.Get(ctx, expired.ID)
	if e != nil || expired.Status != "failed" || expired.Failure != "deadline_exceeded" {
		t.Fatal("deadline did not settle", expired, e)
	}
	health, e := s.Readiness(ctx)
	if e != nil || health.CanCreate {
		t.Fatal("leader heartbeat fabricated capture readiness", e)
	}
}
