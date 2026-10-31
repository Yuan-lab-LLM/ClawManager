package systembackup

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// This is a component acceptance test, not a successful six-part backup. Its
// only source is the immutable test Secret in the explicitly owned namespace.
func TestRealManagedSecretCaptureCreatesAndRemovesItsStage(t *testing.T) {
	fixturePath := os.Getenv("TEST_SYSTEM_BACKUP_CAPTURE_FIXTURE_FILE")
	if fixturePath == "" {
		t.Skip("requires owned Kubernetes capture fixture")
	}
	const owner = "cm-backup-first-20261024-0543"
	if filepath.Dir(fixturePath) != "/home/hxc/"+owner {
		t.Fatal("fixture root is not owned by this run")
	}
	ownerBytes, err := os.ReadFile(filepath.Join(filepath.Dir(fixturePath), ".owner"))
	if err != nil || strings.TrimSpace(string(ownerBytes)) != owner {
		t.Fatal("fixture directory ownership mismatch")
	}
	var cfg realCaptureFixture
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal("fixture unavailable")
	}
	defer clear(raw)
	if strictDecode(raw, &cfg) != nil || cfg.Owner != owner || cfg.Request.Namespace != owner || cfg.KubeServer != "https://10.130.14.23:6443" {
		t.Fatal("fixture identity mismatch")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	client, err := kubernetes.NewForConfig(&rest.Config{Host: cfg.KubeServer, BearerToken: cfg.ControllerToken, Timeout: 10 * time.Second, TLSClientConfig: rest.TLSClientConfig{CAData: []byte(cfg.KubeCA), ServerName: "10.130.14.23"}})
	if err != nil {
		t.Fatal("fixture client unavailable")
	}
	ns, err := client.CoreV1().Namespaces().Get(ctx, owner, metav1.GetOptions{})
	if err != nil || string(ns.UID) != cfg.Request.NamespaceUID || ns.Labels["backup-test-owner"] != owner {
		t.Fatal("fixture namespace ownership mismatch")
	}
	database, err := OpenControlDatabase(ctx, cfg.ControlDSNFile)
	if err != nil {
		t.Fatal("fixture control database unavailable")
	}
	defer database.Close()
	for _, file := range []string{"087_add_backup_stage_resources.sql", "089_add_backup_gate_admission.sql"} {
		ddl, e := os.ReadFile(filepath.Join(filepath.Dir(fixturePath), file))
		if e != nil {
			t.Fatal("stage migration unavailable")
		}
		for _, statement := range strings.Split(string(ddl), ";") {
			if strings.TrimSpace(statement) != "" {
				if _, e = database.ExecContext(ctx, statement); e != nil {
					t.Fatal("stage migration failed", e)
				}
			}
		}
	}
	s := Store{DB: database, InstallationID: "managed-stage-fixture"}
	if err = s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	c, err := s.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if c.Version == 0 {
		if _, err = s.SaveConfig(ctx, 0, 1, testConfig(), permitConfig); err != nil {
			t.Fatal(err)
		}
	}
	l, err := s.AcquireLeadership(ctx, "managed-stage-test", 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := RandomID("managed-secret-")
	x, _, err := s.Create(ctx, 1, key, CreateTaskRequest{Type: Backup, Purpose: "functional_test"}, permitTask)
	if err != nil {
		t.Fatal(err)
	}
	x, err = s.ClaimLedTask(ctx, l, x.ID, x.Version, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	x, err = s.Advance(ctx, x, l.Owner, "preparing", "none")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(filepath.Dir(fixturePath), "managed-stage-root")
	if err = os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	request := StageVolumeRequest{Phase: "secret_capture", Namespace: owner, NamespaceUID: cfg.Request.NamespaceUID, NodeName: "k8s-master", HostRoot: root, StorageClass: "manual", CapacityBytes: 64 << 20}
	v, err := s.PrepareStageVolume(ctx, l, x, request)
	if err != nil {
		t.Fatal(err)
	}
	volumes := LocalStageVolumes{Client: client, MountDirectory: root}
	for {
		var ready bool
		v, ready, err = volumes.Ensure(ctx, s, l, x, v)
		if err != nil {
			t.Fatal("automatic stage provisioning", err)
		}
		if ready {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("stage binding deadline")
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Logf("provisioned owned stage resource=%s pvc=%s pv=%s", v.ID, v.PVCUID, v.PVUID)
	r := cfg.Request
	r.Origin = s.InstallationID
	r.BackupID = x.ID
	r.Stage = PVCIdentity{Name: v.Name(), UID: v.PVCUID}
	r.NodeName = request.NodeName
	r.DeadlineAt = x.DeadlineAt
	r.DeadlineSeconds = int64(time.Until(x.DeadlineAt) / time.Second)
	r.Gate = GateOwnership{Scope: "managed-" + x.ID, Owner: x.ID, Generation: 1, Token: 1}
	g := Gate{DB: database}
	if err = g.EnsureScope(ctx, r.Gate.Scope); err != nil {
		t.Fatal(err)
	}
	a, err := s.PrepareAttempt(ctx, l, x, "secret_capture", r)
	if err != nil {
		t.Fatal(err)
	}
	jobs := CaptureJobs{Client: client}
	job, err := jobs.Ensure(ctx, a, "", true)
	if err != nil {
		t.Fatal("managed capture Job creation", err)
	}
	uid := string(job.UID)
	if err = s.BindCaptureJob(ctx, l, x, a, job); err != nil {
		t.Fatal(err)
	}
	held := false
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), time.Minute)
		defer stop()
		for cleanup.Err() == nil {
			done, e := jobs.Stop(cleanup, a, uid)
			if e != nil {
				t.Error("managed capture stop", e)
				return
			}
			if done {
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
		if held {
			own := r.Gate
			if e := g.BeginRelease(cleanup, own); e != nil {
				t.Error(e)
				return
			}
			if e := g.FinishRelease(cleanup, own); e != nil {
				t.Error(e)
				return
			}
		}
		for cleanup.Err() == nil {
			v, e := s.StageVolume(cleanup, x.ID, "secret_capture")
			if e != nil {
				t.Error(e)
				return
			}
			done, e := volumes.Cleanup(cleanup, s, l, x, v)
			if e != nil {
				t.Error("automatic stage cleanup", e)
				return
			}
			if done {
				if _, e = os.Stat(filepath.Join(root, v.ID)); !errors.Is(e, os.ErrNotExist) {
					t.Error("stage directory still exists")
				}
				if _, e = s.Advance(cleanup, x, l.Owner, "failed", "evidence_incomplete"); e != nil {
					t.Error(e)
				}
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
		t.Error("managed stage cleanup deadline")
	}()
	for {
		observed, e := jobs.Observe(ctx, a, uid)
		if e != nil {
			t.Fatal(e)
		}
		if observed.State == "ready" {
			break
		}
		if observed.State == "failed" || ctx.Err() != nil {
			t.Fatal("collector did not become ready")
		}
		time.Sleep(250 * time.Millisecond)
	}
	data := filepath.Join(root, v.ID, "data")
	entries, err := os.ReadDir(data)
	if err != nil || len(entries) != 0 {
		t.Fatal("source read before held gate", err)
	}
	own, err := g.Begin(ctx, r.Gate.Scope, x.ID, RegistryHash(), 5*time.Second, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	// Release uses the held token; BeginRelease accepts this owner/generation.
	held = true
	own, err = g.Hold(ctx, own)
	if err != nil || own != r.Gate {
		t.Fatal("gate ownership mismatch", err)
	}
	var observed CaptureJobResult
	for {
		observed, err = jobs.Observe(ctx, a, uid)
		if err != nil {
			t.Fatal(err)
		}
		if observed.State == "sealed" {
			break
		}
		if observed.State == "failed" || ctx.Err() != nil {
			t.Fatal("collector did not seal")
		}
		time.Sleep(250 * time.Millisecond)
	}
	if err = s.FinishAttempt(ctx, l, x, a, "sealed", observed); err != nil {
		t.Fatal(err)
	}
	if observed.Candidate == nil || len(observed.Candidate.Parts) != 1 {
		t.Fatal("invalid Secret capture result")
	}
	part := observed.Candidate.Parts[0]
	kek, err := os.ReadFile(cfg.KEKFile)
	if err != nil {
		t.Fatal("fixture key unavailable")
	}
	defer clear(kek)
	f, err := os.Open(filepath.Join(data, "secret_bundle.enc"))
	if err != nil {
		t.Fatal(err)
	}
	var plaintext bytes.Buffer
	err = OpenPart(ctx, f, &plaintext, Manifest{OriginInstallationID: s.InstallationID, BackupID: x.ID}, part, AESKeyWrapper{Reference: part.Key.Reference, Key: kek}, 1<<20)
	f.Close()
	defer clear(plaintext.Bytes())
	if err != nil {
		t.Fatal("managed stage decrypt/readback", PublicError(err))
	}
	if err = ValidateSnapshotPart(ctx, "secret_bundle", bytes.NewReader(plaintext.Bytes()), CaptureBudget{MaxBytes: 1 << 20, MaxItems: 1000}); err != nil {
		t.Fatal("managed snapshot invalid", PublicError(err))
	}
	if err = g.BeginRelease(ctx, own); err != nil {
		t.Fatal(err)
	}
	if err = g.FinishRelease(ctx, own); err != nil {
		t.Fatal(err)
	}
	held = false
	// The deferred cleanup ends this intentionally partial fixture as failed;
	// it never grants an accepted six-part backup or recovery eligibility.
	t.Logf("verified managed capture Job=%s source-resumed=true parts=1", uid)
}
