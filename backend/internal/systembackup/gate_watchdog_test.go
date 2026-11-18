package systembackup

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"clawreef/internal/backupgate"
	batchv1 "k8s.io/api/batch/v1"
)

func TestGateWatchdogRequiresActualParticipantResume(t *testing.T) {
	ctx := context.Background()
	db := controlDB(t)
	g := Gate{db}
	if err := g.EnsureScope(ctx, "watchdog"); err != nil {
		t.Fatal(err)
	}
	if err := g.RegisterParticipant(ctx, "watchdog", "runtime-1"); err != nil {
		t.Fatal(err)
	}
	own, err := g.Begin(ctx, "watchdog", "backup", RegistryHash(), time.Second, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err = g.AckPaused(ctx, own, "runtime-1", strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	own, err = g.Hold(ctx, own)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE system_backup_gate SET hold_deadline=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE scope='watchdog'"); err != nil {
		t.Fatal(err)
	}
	if released, err := g.ForceReleaseExpired(ctx, "watchdog"); err != nil || released {
		t.Fatal("watchdog fabricated a participant resume", released, err)
	}
	if _, err = backupgate.HoldRequest(ctx, db, "watchdog"); !errors.Is(err, backupgate.ErrMaintenance) {
		t.Fatal("source reopened before ACK", err)
	}
	if err = g.AckResumed(ctx, own, "runtime-1", true); err != nil {
		t.Fatal(err)
	}
	if released, err := g.ForceReleaseExpired(ctx, "watchdog"); err != nil || !released {
		t.Fatal("confirmed source resume did not reopen admission", released, err)
	}
	release, err := backupgate.HoldRequest(ctx, db, "watchdog")
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestExpiredGateInvalidatesCaptureAndRestoresApplicationAdmission(t *testing.T) {
	ctx := context.Background()
	s := Store{controlDB(t), "watchdog-plan"}
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveConfig(ctx, 0, 1, testConfig(), permitConfig); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordHealth(ctx, 1, "writer_registry", "healthy", "none", strings.Repeat("a", 64), 300); err != nil {
		t.Fatal(err)
	}
	g := Gate{s.DB}
	if err := g.EnsureScope(ctx, "installation"); err != nil {
		t.Fatal(err)
	}
	task, _, err := s.Create(ctx, 1, "watchdog-plan-0001", CreateTaskRequest{Type: Backup, Purpose: "functional_test"}, permitTask)
	if err != nil {
		t.Fatal(err)
	}
	jobs := &fixtureCaptureJobs{s: s, jobs: map[string]*batchv1.Job{}}
	c := Controller{Store: s, Owner: "watchdog-controller", Capturer: &CaptureTaskRunner{Jobs: jobs, Resources: &fixtureCaptureResources{scope: "installation"}, Publisher: &fixtureCapturePublisher{}}}
	for i := 0; i < 8; i++ {
		if err = c.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		actual, e := s.Get(ctx, task.ID)
		if e != nil {
			t.Fatal(e)
		}
		if actual.Status == "capturing" {
			break
		}
	}
	if _, err = s.DB.Exec("UPDATE system_backup_gate SET hold_deadline=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE scope='installation'"); err != nil {
		t.Fatal(err)
	}
	if released, err := g.ForceReleaseExpired(ctx, "installation"); err != nil || !released {
		t.Fatal(released, err)
	}
	plan, err := s.GetCapturePlan(ctx, task.ID)
	if err != nil || plan.ForcedResumeAt == nil || plan.SourceResumedAt == nil {
		t.Fatal("lost forced-resume evidence", err)
	}
	release, err := backupgate.HoldRequest(ctx, s.DB, "installation")
	if err != nil {
		t.Fatal("expired controller gate kept the application frozen", err)
	}
	release()
	for i := 0; i < 4; i++ {
		if err = c.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	actual, err := s.Get(ctx, task.ID)
	if err != nil || actual.Status != "failed" || actual.Failure != "quiesce_resume_failed" || actual.Eligible {
		t.Fatal("expired capture was accepted", actual, err)
	}
}
