package systembackup

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	batchv1 "k8s.io/api/batch/v1"
	"os"
	"path/filepath"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
)

func TestCaptureBootstrapWiresCoreWithoutAssertingFormalReadiness(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, value []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, value, 0600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	capture := captureFixtureRequest()
	capture.ServiceAccount = "ordinary-capture"
	secret := capture
	secret.ServiceAccount = "secret-capture"
	secret.BootstrapSecret = "secret-key"
	secret.BootstrapUID = "secret-key-uid"
	secret.OperationSecret = "secret-operation"
	secret.OperationSecretUID = "secret-operation-uid"
	secret.SourceVolumes = nil
	upload := uploadFixtureRequest()
	upload.ServiceAccount = "upload-parts"
	verify := verifyFixtureRequest()
	verify.ServiceAccount = "snapshot-verify"
	verify.BootstrapUID = "verifier-bootstrap-uid"
	credential := write("credential.json", []byte(`{"access_key":"local-fixture","secret_key":"local-fixture-only"}`))
	provider := ArtifactClientBootstrap{Reference: SecretRef{"kubernetes", "archive", "v1", "artifact"}, Endpoint: "127.0.0.1:39000", Bucket: "owned-fixture", Root: "backup", CredentialsFile: credential}
	cfg := CaptureRunnerBootstrap{Capture: LocalCaptureResourceConfig{Capture: capture, Secrets: secret, MountDirectory: dir, HostRoot: "/owned/backup", NodeName: "owned-node", StorageClass: "manual"}, Upload: upload, Verifier: verify, ArtifactRead: provider, ArtifactFinalWrite: provider, SigningReference: SecretRef{"kubernetes", "signer", "v1", "signing"}, SigningKeyFile: write("sign.key", key)}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	client := fake.NewSimpleClientset()
	runner, closeRunner, err := LoadCaptureTaskRunner(context.Background(), write("runner.json", raw), client)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRunner()
	publisher, ok := runner.Publisher.(*CorePublisher)
	if !ok {
		t.Fatal("core publisher not wired")
	}
	if PublicError(publisher.ReadyForPurpose("acceptance")) != "not_ready" || publisher.ReadyForPurpose("functional_test") != nil {
		t.Fatal("unproven formal capability enabled")
	}
	if len(client.Actions()) != 0 {
		t.Fatal("loading offline configuration created resources or enrolled health")
	}
	cfg.Upload.ServiceAccount = capture.ServiceAccount
	raw, _ = json.Marshal(cfg)
	if _, _, err = LoadCaptureTaskRunner(context.Background(), write("shared-identity.json", raw), client); PublicError(err) != "isolation_invalid" {
		t.Fatal("collector/uploader identity reuse accepted", err)
	}
	provider.Endpoint = "10.130.14.23:9000"
	if _, _, err = artifactClientFromBootstrap(provider); PublicError(err) != "isolation_invalid" {
		t.Fatal("non-local plaintext provider accepted", err)
	}
}

func TestUnconfiguredFormalPublisherNeverStartsCapture(t *testing.T) {
	ctx := context.Background()
	s := Store{controlDB(t), "formal-not-ready"}
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveConfig(ctx, 0, 1, testConfig(), permitConfig); err != nil {
		t.Fatal(err)
	}
	task, _, err := s.Create(ctx, 1, "formal-not-ready-0001", CreateTaskRequest{Type: Backup, Purpose: "acceptance"}, permitTask)
	if err != nil {
		t.Fatal(err)
	}
	jobs := &fixtureCaptureJobs{s: s, jobs: map[string]*batchv1.Job{}}
	c := Controller{Store: s, Owner: "formal-not-ready-controller", Capturer: &CaptureTaskRunner{Jobs: jobs, Resources: &fixtureCaptureResources{}, Publisher: &CorePublisher{}}}
	if err = c.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	actual, err := s.Get(ctx, task.ID)
	if err != nil || actual.Status != "pending" || actual.PendingReason != "dependency_backoff" || jobs.created != 0 {
		t.Fatal("unsupported formal work started capture", actual, err)
	}
}
