package systembackup

import (
	"clawreef/internal/backupgate"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

type fixtureCaptureResources struct {
	scope   string
	cleaned bool
}

func (r *fixtureCaptureResources) Prepare(ctx context.Context, s Store, l Leadership, t Task, c BackupConfig) (CapturePlanRequest, error) {
	g, err := s.captureGate(ctx, r.scope)
	if err != nil {
		return CapturePlanRequest{}, err
	}
	b := captureFixtureRequest()
	b.Origin = s.InstallationID
	b.BackupID = t.ID
	b.Gate = GateOwnership{Scope: r.scope, Owner: t.ID, Generation: g.Generation + 1, Token: g.Token + 1}
	b.DeadlineAt = t.DeadlineAt
	b.Stage.UID = "ordinary-" + t.ID[4:]
	b.MaxBytes = 1 << 20
	secret := b
	secret.Phase = "secret_capture"
	secret.SourceVolumes = nil
	secret.Stage = PVCIdentity{Name: "secret-stage", UID: "secret-" + t.ID[4:]}
	return CapturePlanRequest{Ordinary: b, Secrets: secret, WriterRegistryHash: strings.Repeat("a", 64)}, nil
}
func (r *fixtureCaptureResources) Cleanup(context.Context, Store, Leadership, Task, *CapturePlan) (bool, error) {
	r.cleaned = true
	return true, nil
}

type fixtureCapturePublisher struct{ seen int }

func (p *fixtureCapturePublisher) Tick(ctx context.Context, s Store, l Leadership, t Task, c CapturePlan, r CaptureResourceDriver) error {
	if c.Manifest == nil || c.Seal == nil || c.SourceResumedAt == nil || c.ForcedResumeAt != nil {
		return fmt.Errorf("publication crossed an incomplete capture barrier")
	}
	p.seen++
	return nil
}

type fixtureCaptureJobs struct {
	s       Store
	jobs    map[string]*batchv1.Job
	created int
}

func (j *fixtureCaptureJobs) Ensure(ctx context.Context, a Attempt, uid string, create bool) (*batchv1.Job, error) {
	if x := j.jobs[a.ID]; x != nil {
		if uid != "" && string(x.UID) != uid {
			return nil, ErrFence
		}
		return x, nil
	}
	if !create {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: "batch", Resource: "jobs"}, a.ID)
	}
	x, _, err := captureJob(a)
	if err != nil {
		return nil, err
	}
	j.created++
	x.UID = types.UID(fmt.Sprintf("job-%d", j.created))
	j.jobs[a.ID] = x
	return x, nil
}
func (j *fixtureCaptureJobs) Existing(ctx context.Context, a Attempt) (*batchv1.Job, error) {
	return j.Ensure(ctx, a, "", false)
}
func (j *fixtureCaptureJobs) Stop(ctx context.Context, a Attempt, uid string) (bool, error) {
	if x := j.jobs[a.ID]; x != nil {
		if string(x.UID) != uid {
			return false, ErrFence
		}
		delete(j.jobs, a.ID)
		return false, nil
	}
	return true, nil
}
func (j *fixtureCaptureJobs) Observe(ctx context.Context, a Attempt, uid string) (CaptureJobResult, error) {
	_, r, err := captureJob(a)
	if err != nil {
		return CaptureJobResult{}, err
	}
	g, err := j.s.captureGate(ctx, r.Gate.Scope)
	if err != nil {
		return CaptureJobResult{}, err
	}
	if g.Phase != "held" {
		return CaptureJobResult{State: "ready", JobUID: uid}, nil
	}
	var now time.Time
	if err = j.s.DB.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now); err != nil {
		return CaptureJobResult{}, err
	}
	c := &CaptureCandidate{Schema: "system-backup-capture-candidate.v1", CapturePhaseRequest: CapturePhaseRequest{Origin: r.Origin, BackupID: r.BackupID, AttemptID: a.ID, Phase: r.Phase, Gate: r.Gate, StageUID: r.Stage.UID}, RegistryHash: RegistryHash(), StartedAt: now, SealedAt: now}
	for _, name := range RequiredParts {
		if (name == "secret_bundle") != (a.Phase == "secret_capture") {
			continue
		}
		c.Parts = append(c.Parts, ArtifactPart{Name: name, CiphertextBytes: 100, PlaintextBytes: 50, CiphertextSHA256: strings.Repeat("b", 64), PlaintextSHA256: strings.Repeat("c", 64), Key: WrappedKey{Reference: SecretRef{"kubernetes", "fixture-kek", "v1", "encryption"}, Ciphertext: []byte("fixture")}})
	}
	return CaptureJobResult{State: "sealed", JobUID: uid, Candidate: c}, nil
}

func TestCaptureControllerAdoptsBothJobsAndPublishesOnlyAfterSourceResume(t *testing.T) {
	ctx := context.Background()
	s := Store{controlDB(t), "capture-controller"}
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
	task, _, err := s.Create(ctx, 1, "capture-controller-0001", CreateTaskRequest{Type: Backup, Purpose: "functional_test"}, permitTask)
	if err != nil {
		t.Fatal(err)
	}
	jobs := &fixtureCaptureJobs{s: s, jobs: map[string]*batchv1.Job{}}
	resources := &fixtureCaptureResources{scope: "installation"}
	publisher := &fixtureCapturePublisher{}
	runner := &CaptureTaskRunner{Jobs: jobs, Resources: resources, Publisher: publisher}
	c := Controller{Store: s, Owner: "leader-a", PollInterval: time.Second, Capturer: runner}
	changed := false
	for i := 0; i < 15 && publisher.seen == 0; i++ {
		if err = c.Tick(ctx); err != nil {
			t.Fatal("capture controller tick", i, err)
		}
		actual, e := s.Get(ctx, task.ID)
		if e != nil {
			t.Fatal(e)
		}
		if actual.Status == "capturing" && !changed {
			if _, e = s.DB.Exec("UPDATE system_backup_controller_leases SET expires_at=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND"); e != nil {
				t.Fatal(e)
			}
			if _, e = s.DB.Exec("UPDATE system_backup_tasks SET lease_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE public_id=?", task.ID); e != nil {
				t.Fatal(e)
			}
			c.Owner = "leader-b"
			changed = true
		}
	}
	if publisher.seen != 1 || !changed || jobs.created != 2 {
		t.Fatal("did not adopt existing capture Jobs", publisher.seen, changed, jobs.created)
	}
	actual, err := s.Get(ctx, task.ID)
	if err != nil || actual.Status != "validating" || actual.Registered || actual.Eligible {
		t.Fatal("capture itself granted recovery eligibility", actual, err)
	}
	if resources.cleaned {
		t.Fatal("staging deleted before the publisher consumed it")
	}
	for _, phase := range []string{"capture", "secret_capture"} {
		a, err := s.GetAttempt(ctx, task.ID, phase)
		if err != nil || a.Status != "sealed" {
			t.Fatal(a, err)
		}
		var result CaptureJobResult
		if json.Unmarshal(a.Result, &result) != nil || result.JobUID == "" {
			t.Fatal("missing original capture Job identity")
		}
	}
}

func TestCanceledCaptureReopensAdmissionBeforeTheDrainDeadline(t *testing.T) {
	ctx := context.Background()
	s := Store{controlDB(t), "cancel-admission"}
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
	release, err := backupgate.HoldRequest(ctx, s.DB, "installation")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	task, _, err := s.Create(ctx, 1, "cancel-admission-0001", CreateTaskRequest{Type: Backup, Purpose: "functional_test"}, permitTask)
	if err != nil {
		t.Fatal(err)
	}
	jobs := &fixtureCaptureJobs{s: s, jobs: map[string]*batchv1.Job{}}
	c := Controller{Store: s, Owner: "cancel-controller", Capturer: &CaptureTaskRunner{Jobs: jobs, Resources: &fixtureCaptureResources{scope: "installation"}, Publisher: &fixtureCapturePublisher{}}}
	for i := 0; i < 5; i++ {
		if err = c.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	actual, err := s.Get(ctx, task.ID)
	if err != nil || actual.Status != "acquiring_gate" {
		t.Fatal(actual, err)
	}
	if r, e := backupgate.HoldRequest(ctx, s.DB, "installation"); e == nil {
		r()
		t.Fatal("drain did not close admission")
	}
	if _, err = s.Cancel(ctx, task.ID, actual.Version, 1); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if err = c.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	actual, err = s.Get(ctx, task.ID)
	if err != nil || actual.Status != "canceled" {
		t.Fatal(actual, err)
	}
	r, err := backupgate.HoldRequest(ctx, s.DB, "installation")
	if err != nil {
		t.Fatal("cancellation left the source blocked", err)
	}
	r()
}
