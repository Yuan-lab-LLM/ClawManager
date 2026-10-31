package systembackup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

type testCaptureGuard struct {
	state CaptureGuardSnapshot
	fail  bool
}

func TestAggregateCaptureRequiresBothSealsAndTheCurrentFence(t *testing.T) {
	ctx := context.Background()
	id, _ := RandomID("sbk_")
	a, _ := RandomID("sat_")
	b, _ := RandomID("sat_")
	own := GateOwnership{Scope: "installation", Owner: id, Generation: 1, Token: 2}
	now := time.Now().UTC()
	g := &testCaptureGuard{state: CaptureGuardSnapshot{Ownership: own}}
	ordinary := CaptureCandidate{Schema: "system-backup-capture-candidate.v1", CapturePhaseRequest: CapturePhaseRequest{Origin: "installation", BackupID: id, AttemptID: a, Phase: "capture", Gate: own, StageUID: "ordinary-pvc"}, RegistryHash: RegistryHash(), StartedAt: now, SealedAt: now}
	secret := ordinary
	secret.AttemptID = b
	secret.Phase = "secret_capture"
	secret.StageUID = "secret-pvc"
	w := AESKeyWrapper{Reference: SecretRef{"kubernetes", "kek", "v1", "encryption"}, Key: bytes.Repeat([]byte{7}, 32)}
	for _, name := range RequiredParts {
		part, err := CapturePart(ctx, t.TempDir(), "installation", id, name, 1<<20, w, func(_ context.Context, out io.Writer) error { _, err := out.Write([]byte("fixture")); return err })
		if err != nil {
			t.Fatal(err)
		}
		if name == "secret_bundle" {
			secret.Parts = append(secret.Parts, part)
		} else {
			ordinary.Parts = append(ordinary.Parts, part)
		}
	}
	if m, err := AggregateCapturedPhases(ctx, ordinary, secret, CaptureBudget{MaxBytes: 1 << 20, MaxItems: 100}, g); err != nil || len(m.Parts) != 6 {
		t.Fatal(m, err)
	}
	missing := secret
	missing.Parts = nil
	if _, err := AggregateCapturedPhases(ctx, ordinary, missing, CaptureBudget{MaxBytes: 1 << 20, MaxItems: 100}, g); err == nil {
		t.Fatal("partial phase accepted")
	}
	g.fail = true
	if _, err := AggregateCapturedPhases(ctx, ordinary, secret, CaptureBudget{MaxBytes: 1 << 20, MaxItems: 100}, g); !errors.Is(err, ErrFence) {
		t.Fatal("stale capture fence accepted", err)
	}
}

func TestSQLCaptureGuardRequiresHeldGenerationAndAbortMargin(t *testing.T) {
	ctx := context.Background()
	db := controlDB(t)
	g := Gate{db}
	if err := g.EnsureScope(ctx, "guard"); err != nil {
		t.Fatal(err)
	}
	id, _ := RandomID("sbk_")
	own, err := g.Begin(ctx, "guard", id, RegistryHash(), time.Second, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	guard := SQLCaptureGuard{Gate: g, Ownership: own, AbortMargin: 2 * time.Second}
	if _, err = guard.Snapshot(ctx); !errors.Is(err, ErrFence) {
		t.Fatal("acquiring gate accepted", err)
	}
	own, err = g.Hold(ctx, own)
	if err != nil {
		t.Fatal(err)
	}
	guard.Ownership = own
	if s, err := guard.Snapshot(ctx); err != nil || s.Now.IsZero() {
		t.Fatal(s, err)
	}
	guard.Ownership.Token--
	if _, err = guard.Snapshot(ctx); !errors.Is(err, ErrFence) {
		t.Fatal("stale token accepted", err)
	}
	guard.Ownership = own
	if _, err = db.Exec("UPDATE system_backup_gate SET hold_deadline=UTC_TIMESTAMP(6)+INTERVAL 1 SECOND WHERE scope='guard'"); err != nil {
		t.Fatal(err)
	}
	if _, err = guard.Snapshot(ctx); !errors.Is(err, ErrFence) {
		t.Fatal("capture ignored pre-resume abort margin", err)
	}
}

func (g *testCaptureGuard) Snapshot(context.Context) (CaptureGuardSnapshot, error) {
	if g.fail {
		return g.state, ErrFence
	}
	g.state.Now = time.Now().UTC()
	return g.state, nil
}

func TestSecretCaptureHasSeparateCapabilitiesAndCannotSealAfterFenceLoss(t *testing.T) {
	ctx := context.Background()
	id, _ := RandomID("sbk_")
	attempt, _ := RandomID("sat_")
	own := GateOwnership{Scope: "installation", Owner: id, Generation: 2, Token: 4}
	g := &testCaptureGuard{state: CaptureGuardSnapshot{Ownership: own}}
	secret := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": "token", "namespace": "source", "uid": "secret-uid"}, "data": map[string]any{"token": "c2VjcmV0"}}}
	client := fake.NewSimpleDynamicClient(runtime.NewScheme(), secret)
	source := CaptureSources{Namespace: "source", NamespaceUID: "source-uid", SecretClient: client, Secrets: []KubernetesSelection{{Version: "v1", Resource: "secrets", Name: "token", UID: "secret-uid"}}}
	r := CapturePhaseRequest{Origin: "installation", BackupID: id, AttemptID: attempt, Phase: "secret_capture", Gate: own, StageUID: "pvc-uid"}
	w := AESKeyWrapper{Reference: SecretRef{"kubernetes", "kek", "v1", "encryption"}, Key: bytes.Repeat([]byte{7}, 32)}
	dir := t.TempDir()
	c, err := CapturePhase(ctx, dir, r, source, CaptureBudget{MaxBytes: 1 << 20, MaxItems: 100}, w, g)
	if err != nil || len(c.Parts) != 1 || c.Parts[0].Name != "secret_bundle" || c.Gate != own {
		t.Fatal(c, err)
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "secret_bundle.enc")); err != nil || bytes.Contains(raw, []byte("c2VjcmV0")) {
		t.Fatal("secret capture staged plaintext", err)
	}
	if _, err = CapturePhase(ctx, dir, r, source, CaptureBudget{MaxBytes: 1 << 20, MaxItems: 100}, w, g); err == nil {
		t.Fatal("reused sealed staging")
	}
	bad := source
	bad.DeploymentClient = client
	if _, err = CapturePhase(ctx, t.TempDir(), r, bad, CaptureBudget{MaxBytes: 1 << 20, MaxItems: 100}, w, g); err == nil {
		t.Fatal("combined source and secret capabilities")
	}
	g.fail = true
	fenced := t.TempDir()
	if _, err = CapturePhase(ctx, fenced, r, source, CaptureBudget{MaxBytes: 1 << 20, MaxItems: 100}, w, g); !errors.Is(err, ErrFence) {
		t.Fatal("fenced capture sealed", err)
	}
	if _, err = os.Stat(filepath.Join(fenced, "capture-candidate.json")); !os.IsNotExist(err) {
		t.Fatal("candidate survived fence loss", err)
	}
}
