package systembackup

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
)

type disappearedCaptureCreate struct {
	*fixtureCaptureJobs
	lost bool
}

func (j *disappearedCaptureCreate) Ensure(ctx context.Context, a Attempt, uid string, create bool) (*batchv1.Job, error) {
	job, err := j.fixtureCaptureJobs.Ensure(ctx, a, uid, create)
	if err == nil && create && !j.lost {
		j.lost = true
		delete(j.jobs, a.ID)
		return nil, errors.New("lost response followed by missing original execution")
	}
	return job, err
}
func TestUnknownCaptureCreateNeverStartsAnotherSourceRead(t *testing.T) {
	ctx := context.Background()
	s := Store{controlDB(t), "unknown-capture-create"}
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveConfig(ctx, 0, 1, testConfig(), permitConfig); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordHealth(ctx, 1, "writer_registry", "healthy", "none", strings.Repeat("a", 64), 300); err != nil {
		t.Fatal(err)
	}
	if err := (Gate{s.DB}).EnsureScope(ctx, "installation"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Create(ctx, 1, "unknown-capture-create-0001", CreateTaskRequest{Type: Backup, Purpose: "functional_test"}, permitTask); err != nil {
		t.Fatal(err)
	}
	jobs := &disappearedCaptureCreate{fixtureCaptureJobs: &fixtureCaptureJobs{s: s, jobs: map[string]*batchv1.Job{}}}
	c := Controller{Store: s, Owner: "unknown-capture-controller", Capturer: &CaptureTaskRunner{Jobs: jobs, Resources: &fixtureCaptureResources{scope: "installation"}, Publisher: &fixtureCapturePublisher{}}}
	if err := c.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	_ = c.Tick(ctx)
	if !jobs.lost {
		t.Fatal("fixture did not lose create response")
	}
	_ = c.Tick(ctx)
	if jobs.created != 1 {
		t.Fatal("unknown capture creation repeated a live source read", jobs.created)
	}
}

func TestLegacyUnboundCaptureAttemptKeepsUnknownSendAfterMigration(t *testing.T) {
	ctx := context.Background()
	s := Store{controlDB(t), "legacy-capture-send"}
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveConfig(ctx, 0, 1, testConfig(), permitConfig); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordHealth(ctx, 1, "writer_registry", "healthy", "none", strings.Repeat("a", 64), 300); err != nil {
		t.Fatal(err)
	}
	if err := (Gate{s.DB}).EnsureScope(ctx, "installation"); err != nil {
		t.Fatal(err)
	}
	task, _, err := s.Create(ctx, 1, "legacy-capture-send-0001", CreateTaskRequest{Type: Backup, Purpose: "functional_test"}, permitTask)
	if err != nil {
		t.Fatal(err)
	}
	l, err := s.AcquireLeadership(ctx, "legacy-capture-controller", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	task, err = s.ClaimLedTask(ctx, l, task.ID, task.Version, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	task, err = s.Advance(ctx, task, l.Owner, "preparing", "none")
	if err != nil {
		t.Fatal(err)
	}
	r := &fixtureCaptureResources{scope: "installation"}
	request, err := r.Prepare(ctx, s, l, task, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PrepareCapturePlan(ctx, l, task, request); err != nil {
		t.Fatal(err)
	}
	a, err := s.PrepareAttempt(ctx, l, task, "capture", request.Ordinary)
	if err != nil {
		t.Fatal(err)
	}
	// Reapply the actual upgrade DDL after a legacy attempt exists. No Job
	// binding can tell whether its former Create reached the cluster.
	ddl, err := os.ReadFile("../db/migrations/091_add_backup_job_send_intents.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(ddl), ";") {
		if strings.TrimSpace(statement) != "" {
			if _, err = s.DB.Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	jobs := &fixtureCaptureJobs{s: s, jobs: map[string]*batchv1.Job{}}
	_, err = s.ensureBackupExecution(ctx, l, task, a, func(step context.Context) (*batchv1.Job, error) { return jobs.Ensure(step, a, "", true) }, func(step context.Context) (*batchv1.Job, error) { return jobs.Existing(step, a) }, func(step context.Context, job *batchv1.Job) error { return s.BindCaptureJob(step, l, task, a, job) })
	if err == nil || jobs.created != 0 {
		t.Fatal("upgrade restarted an unknown legacy source read", jobs.created, err)
	}
}

func TestUnknownCaptureDoesNotStopOtherTasksFromCanceling(t *testing.T) {
	ctx := context.Background()
	s := Store{controlDB(t), "unknown-capture-poll"}
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveConfig(ctx, 0, 1, testConfig(), permitConfig); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordHealth(ctx, 1, "writer_registry", "healthy", "none", strings.Repeat("a", 64), 300); err != nil {
		t.Fatal(err)
	}
	if err := (Gate{s.DB}).EnsureScope(ctx, "installation"); err != nil {
		t.Fatal(err)
	}
	source, _, err := s.Create(ctx, 1, "unknown-poll-backup-0001", CreateTaskRequest{Type: Backup, Purpose: "functional_test"}, permitTask)
	if err != nil {
		t.Fatal(err)
	}
	jobs := &disappearedCaptureCreate{fixtureCaptureJobs: &fixtureCaptureJobs{s: s, jobs: map[string]*batchv1.Job{}}}
	c := Controller{Store: s, Owner: "unknown-poll-controller", Capturer: &CaptureTaskRunner{Jobs: jobs, Resources: &fixtureCaptureResources{scope: "installation"}, Publisher: &fixtureCapturePublisher{}}}
	if err = c.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	other, _, err := s.Create(ctx, 1, "unknown-poll-preflight-0001", CreateTaskRequest{Type: Preflight, Purpose: "functional_test", SourceID: source.ID}, permitTask)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Cancel(ctx, other.ID, other.Version, 1); err != nil {
		t.Fatal(err)
	}
	if err = c.Tick(ctx); err != nil {
		t.Fatal("unknown job shut down controller polling", err)
	}
	actual, err := s.Get(ctx, other.ID)
	if err != nil || actual.Status != "canceled" {
		t.Fatal("one unknown Job starved another task's cancellation", actual, err)
	}
	backup, err := s.Get(ctx, source.ID)
	if err != nil || backup.AttentionReason != "external_result_unknown" || backup.Terminal() {
		t.Fatal("unknown execution was hidden or finalized", backup, err)
	}
}
