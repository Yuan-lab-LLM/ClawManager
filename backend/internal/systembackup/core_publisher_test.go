package systembackup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

type coreMemoryStore struct {
	data         map[string][]byte
	puts         map[string]int
	loseManifest bool
}

func (m *coreMemoryStore) PutImmutable(_ context.Context, key string, raw []byte) (ObjectReference, error) {
	m.puts[key]++
	m.data[key] = bytes.Clone(raw)
	h := sha256.Sum256(raw)
	ref := ObjectReference{Key: key, VersionID: "object-v1", Bytes: int64(len(raw)), SHA256: hex.EncodeToString(h[:])}
	if m.loseManifest && strings.HasSuffix(key, "/manifest.json") {
		m.loseManifest = false
		return ObjectReference{}, errors.New("lost write response")
	}
	return ref, nil
}
func (m *coreMemoryStore) Read(_ context.Context, ref ObjectReference) ([]byte, error) {
	raw, ok := m.data[ref.Key]
	h := sha256.Sum256(raw)
	if !ok || ref.VersionID != "object-v1" || ref.Bytes != int64(len(raw)) || ref.SHA256 != hex.EncodeToString(h[:]) {
		return nil, errors.New("artifact_unavailable")
	}
	return bytes.Clone(raw), nil
}
func (m *coreMemoryStore) ReadCurrent(ctx context.Context, key string) ([]byte, ObjectReference, error) {
	raw, ok := m.data[key]
	if !ok {
		return nil, ObjectReference{}, errors.New("artifact_unavailable")
	}
	h := sha256.Sum256(raw)
	return bytes.Clone(raw), ObjectReference{Key: key, VersionID: "object-v1", Bytes: int64(len(raw)), SHA256: hex.EncodeToString(h[:])}, nil
}

type fixtureManagedStages struct{ fixtureCaptureResources }

func (r *fixtureManagedStages) Prepare(ctx context.Context, s Store, l Leadership, t Task, c BackupConfig) (CapturePlanRequest, error) {
	p, err := r.fixtureCaptureResources.Prepare(ctx, s, l, t, c)
	if err != nil {
		return p, err
	}
	for _, job := range []*CaptureJobRequest{&p.Ordinary, &p.Secrets} {
		v, err := s.PrepareStageVolume(ctx, l, t, StageVolumeRequest{Phase: job.Phase, Namespace: job.Namespace, NamespaceUID: job.NamespaceUID, NodeName: "fixture-node", HostRoot: "/fixture/stage", StorageClass: "manual", CapacityBytes: 8 << 20})
		if err != nil {
			return p, err
		}
		for action, uid := range map[string]string{"pvc_uid": "pvc-" + v.ID, "pv_uid": "pv-" + v.ID} {
			if err = s.updateStageVolume(ctx, l, t, v, action, uid); err != nil {
				return p, err
			}
		}
		if err = s.updateStageVolume(ctx, l, t, v, "ready", ""); err != nil {
			return p, err
		}
		job.Stage = PVCIdentity{Name: v.Name(), UID: "pvc-" + v.ID}
	}
	return p, nil
}
func (r *fixtureManagedStages) Cleanup(ctx context.Context, s Store, l Leadership, t Task, _ *CapturePlan) (bool, error) {
	for _, phase := range []string{"capture", "secret_capture"} {
		v, err := s.StageVolume(ctx, t.ID, phase)
		if err != nil {
			return false, err
		}
		if err = s.updateStageVolume(ctx, l, t, v, "delete_requested", ""); err != nil {
			return false, err
		}
		if err = s.updateStageVolume(ctx, l, t, v, "deleted", ""); err != nil {
			return false, err
		}
	}
	r.cleaned = true
	return true, nil
}

type fixtureCoreResources struct{ cleaned bool }

func (r *fixtureCoreResources) PrepareUpload(ctx context.Context, s Store, l Leadership, t Task, p CapturePlan, core CorePublication) (UploadJobRequest, error) {
	if err := s.captureConfigMutation(ctx, l, t, "", true); err != nil {
		return UploadJobRequest{}, err
	}
	if err := s.captureConfigMutation(ctx, l, t, "upload-config-uid", false); err != nil {
		return UploadJobRequest{}, err
	}
	request := uploadFixtureRequest()
	request.Origin = s.InstallationID
	request.BackupID = t.ID
	request.Namespace = p.Request.Ordinary.Namespace
	request.NamespaceUID = p.Request.Ordinary.NamespaceUID
	request.NodeName = p.Request.Ordinary.NodeName
	request.OrdinaryStage = p.Request.Ordinary.Stage
	request.SecretStage = p.Request.Secrets.Stage
	request.DeadlineAt = t.DeadlineAt
	request.DeadlineSeconds = 600
	request.MaxBytes = p.Request.Ordinary.MaxBytes + p.Request.Secrets.MaxBytes
	request.BootstrapSecret = "upload-bootstrap"
	request.BootstrapUID = "upload-bootstrap-uid"
	request.OperationSecret = "upload-config"
	request.OperationSecretUID = "upload-config-uid"
	return request, nil
}
func (r *fixtureCoreResources) Cleanup(ctx context.Context, s Store, l Leadership, t Task, p CorePublication) (bool, error) {
	r.cleaned = true
	return true, s.MarkCoreConfigDeleted(ctx, l, t, p.UploadConfigUID)
}

type fixtureUploadDriver struct {
	s       Store
	store   *coreMemoryStore
	jobs    map[string]*batchv1.Job
	created int
}

func (j *fixtureUploadDriver) Ensure(ctx context.Context, a Attempt, uid string, create bool) (*batchv1.Job, error) {
	if job := j.jobs[a.ID]; job != nil {
		if uid != "" && string(job.UID) != uid {
			return nil, ErrFence
		}
		return job, nil
	}
	if !create {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "jobs"}, a.ID)
	}
	job, _, err := uploadJob(a)
	if err != nil {
		return nil, err
	}
	j.created++
	job.UID = types.UID(fmt.Sprintf("upload-%d", j.created))
	j.jobs[a.ID] = job
	return job, nil
}
func (j *fixtureUploadDriver) Existing(ctx context.Context, a Attempt) (*batchv1.Job, error) {
	return j.Ensure(ctx, a, "", false)
}
func (j *fixtureUploadDriver) Observe(ctx context.Context, a Attempt, uid string) (UploadJobResult, error) {
	p, err := j.s.CorePublication(ctx, a.TaskID)
	if err != nil {
		return UploadJobResult{}, err
	}
	index := ArtifactIndex{Schema: "system-backup-index.v1", BackupID: a.TaskID, OriginInstallationID: j.s.InstallationID, ManifestSHA256: p.ManifestHash}
	for _, part := range p.Manifest.Payload.Parts {
		index.Parts = append(index.Parts, PartIndex{Name: part.Name, Objects: []ObjectReference{{Key: artifactPrefix(j.s.InstallationID, a.TaskID) + "parts/" + part.Name + "/00000000.bin", VersionID: "part-v1", Bytes: part.CiphertextBytes, SHA256: part.CiphertextSHA256}}})
	}
	_, request, err := uploadJob(a)
	if err != nil {
		return UploadJobResult{}, err
	}
	report, err := SealUploadedIndex(ctx, j.store, PartUploadRequest{Origin: j.s.InstallationID, BackupID: a.TaskID, AttemptID: a.ID, OrdinaryStageUID: request.OrdinaryStage.UID, SecretStageUID: request.SecretStage.UID}, index)
	return UploadJobResult{State: "succeeded", JobUID: uid, Report: &report}, err
}
func (j *fixtureUploadDriver) Stop(_ context.Context, a Attempt, uid string) (bool, error) {
	if job := j.jobs[a.ID]; job != nil {
		if string(job.UID) != uid {
			return false, ErrFence
		}
		delete(j.jobs, a.ID)
		return false, nil
	}
	return true, nil
}

type fixtureSnapshotDriver struct {
	wrongReceipt bool
	s            Store
	jobs         map[string]*batchv1.Job
	created      int
}

func (j *fixtureSnapshotDriver) EnsureBound(ctx context.Context, a Attempt, uid string, create bool) (*batchv1.Job, error) {
	if job := j.jobs[a.ID]; job != nil {
		if uid != "" && string(job.UID) != uid {
			return nil, ErrFence
		}
		return job, nil
	}
	if !create {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "jobs"}, a.ID)
	}
	job, _, err := verifyJob(a)
	if err != nil {
		return nil, err
	}
	j.created++
	job.UID = types.UID(fmt.Sprintf("verify-%d", j.created))
	j.jobs[a.ID] = job
	return job, nil
}
func (j *fixtureSnapshotDriver) Existing(ctx context.Context, a Attempt) (*batchv1.Job, error) {
	return j.EnsureBound(ctx, a, "", false)
}
func (j *fixtureSnapshotDriver) ObserveBound(ctx context.Context, a Attempt, uid string) (VerifyJobResult, error) {
	refs := map[string]ObjectReference{}
	for _, kind := range []string{"manifest_put", "index_put", "marker_put"} {
		action, err := j.s.GetExternalAction(ctx, a.TaskID, kind)
		if err != nil {
			return VerifyJobResult{}, err
		}
		refs[kind] = action.Reference
	}
	if j.wrongReceipt {
		refs["manifest_put"] = ObjectReference{SHA256: strings.Repeat("e", 64)}
	}
	return VerifyJobResult{State: "succeeded", JobUID: uid, Report: &IntegrityReport{BackupID: a.TaskID, Origin: j.s.InstallationID, IntegrityVerified: true, SnapshotValidated: true, PartsVerified: 6, ManifestSHA256: refs["manifest_put"].SHA256, IndexSHA256: refs["index_put"].SHA256, CommitSHA256: refs["marker_put"].SHA256}}, nil
}
func (j *fixtureSnapshotDriver) StopBound(_ context.Context, a Attempt, uid string) (bool, error) {
	if job := j.jobs[a.ID]; job != nil {
		if string(job.UID) != uid {
			return false, ErrFence
		}
		delete(j.jobs, a.ID)
		return false, nil
	}
	return true, nil
}

func TestCorePublicationControllerAdoptsLostWriteAndCleansBeforeFunctionalSuccess(t *testing.T) {
	runCorePublicationFixture(t, false)
}

func TestCorePublicationRejectsWrongVerificationReceiptAndCleansBeforeFailure(t *testing.T) {
	runCorePublicationFixture(t, true)
}

func runCorePublicationFixture(t *testing.T, wrongReceipt bool) {
	ctx := context.Background()
	s := Store{controlDB(t), "core-controller"}
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	config := testConfig()
	config.EncryptionKey = &SecretRef{"kubernetes", "fixture-kek", "v1", "encryption"}
	if _, err := s.SaveConfig(ctx, 0, 1, config, permitConfig); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordHealth(ctx, 1, "writer_registry", "healthy", "none", strings.Repeat("a", 64), 300); err != nil {
		t.Fatal(err)
	}
	if err := (Gate{s.DB}).EnsureScope(ctx, "installation"); err != nil {
		t.Fatal(err)
	}
	task, _, err := s.Create(ctx, 1, "core-controller-0001", CreateTaskRequest{Type: Backup, Purpose: "functional_test"}, permitTask)
	if err != nil {
		t.Fatal(err)
	}
	_, private, _ := ed25519.GenerateKey(rand.Reader)
	provider := &coreMemoryStore{data: map[string][]byte{}, puts: map[string]int{}, loseManifest: true}
	captureJobs := &fixtureCaptureJobs{s: s, jobs: map[string]*batchv1.Job{}}
	uploads := &fixtureUploadDriver{s: s, store: provider, jobs: map[string]*batchv1.Job{}}
	verifier := &fixtureSnapshotDriver{wrongReceipt: wrongReceipt, s: s, jobs: map[string]*batchv1.Job{}}
	resources := &fixtureCoreResources{}
	verifyTemplate := verifyFixtureRequest()
	verifyTemplate.BootstrapUID = "verifier-bootstrap-uid"
	verifyTemplate.BootstrapSecret = "independent-verifier"
	publisher := &CorePublisher{CaptureJobs: captureJobs, Uploads: uploads, Verifier: verifier, VerifyTemplate: verifyTemplate, Resources: resources, ResolveReader: func(context.Context, SecretRef) (CoreArtifactReader, error) { return provider, nil }, ResolveSigner: func(context.Context, SecretRef) (ed25519.PrivateKey, error) { return bytes.Clone(private), nil }, FinalWrites: FinalWriteDriver{Resolve: func(context.Context, SecretRef) (FinalWriteProvider, error) { return provider, nil }}}
	stages := &fixtureManagedStages{fixtureCaptureResources{scope: "installation"}}
	c := Controller{Store: s, Owner: "core-leader-a", PollInterval: time.Second, Capturer: &CaptureTaskRunner{Jobs: captureJobs, Resources: stages, Publisher: publisher}}
	changed := false
	for i := 0; i < 80; i++ {
		if err = c.Tick(ctx); err != nil {
			t.Fatal("core tick", i, err)
		}
		actual, e := s.Get(ctx, task.ID)
		if e != nil {
			t.Fatal(e)
		}
		if actual.Status == "publishing" && actual.CommitStarted && !changed {
			if _, e = s.DB.Exec("UPDATE system_backup_controller_leases SET expires_at=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND"); e != nil {
				t.Fatal(e)
			}
			if _, e = s.DB.Exec("UPDATE system_backup_tasks SET lease_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE public_id=?", task.ID); e != nil {
				t.Fatal(e)
			}
			c.Owner = "core-leader-b"
			changed = true
		}
		if actual.Terminal() {
			expected := "succeeded"
			if wrongReceipt {
				expected = "failed"
				if actual.Failure != "checksum_mismatch" {
					t.Fatal("lost integrity failure category", actual.Failure)
				}
			}
			if actual.Status != expected || actual.Eligible || actual.Registered {
				t.Fatal("core incorrectly granted acceptance", actual)
			}
			break
		}
		if i == 79 {
			t.Fatal("core publication did not converge", actual.Status)
		}
	}
	if !changed || captureJobs.created != 2 || uploads.created != 1 || verifier.created != 1 || !stages.cleaned || !resources.cleaned {
		t.Fatal("missing adoption/cleanup evidence", changed, captureJobs.created, uploads.created, verifier.created)
	}
	if len(captureJobs.jobs)+len(uploads.jobs)+len(verifier.jobs) != 0 {
		t.Fatal("success left owned Jobs")
	}
	core, err := s.CorePublication(ctx, task.ID)
	if err != nil || !wrongReceipt && core.Cleanup == nil || core.UploadConfigDeletedAt == nil {
		t.Fatal("missing durable cleanup", err)
	}
	for _, key := range []string{"manifest.json", "index.json", "_COMMITTED"} {
		if provider.puts[artifactPrefix(s.InstallationID, task.ID)+key] != 1 {
			t.Fatal("lost response repeated final write", key)
		}
	}
	if _, ok := provider.data[artifactPrefix(s.InstallationID, task.ID)+"_ACCEPTANCE"]; ok {
		t.Fatal("functional pipeline issued acceptance")
	}
	var audits int
	expectedAudits := 1
	if wrongReceipt {
		expectedAudits = 0
	}
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM system_backup_events WHERE task_id=? AND event_type='audit.functional_completed'", task.ID).Scan(&audits); err != nil || audits != expectedAudits {
		t.Fatal("completion audit missing/duplicated", audits, err)
	}
}
