package systembackup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type realCaptureFixture struct {
	Request         CaptureJobRequest `json:"request"`
	AttemptID       string            `json:"attempt_id"`
	ControlDSNFile  string            `json:"control_dsn_file"`
	KEKFile         string            `json:"kek_file"`
	StageDirectory  string            `json:"stage_directory"`
	Owner           string            `json:"owner"`
	KubeServer      string            `json:"kube_server"`
	KubeCA          string            `json:"kube_ca"`
	ControllerToken string            `json:"controller_token"`
	SourceUID       string            `json:"source_secret_uid"`
	SecretHash      string            `json:"secret_sha256"`
	GatePassword    string            `json:"gate_password"`
}

// This test runs only against an explicitly owned, disposable test fixture.
// Credential contents never become test output or repository artifacts.
func TestRealSecretCaptureJobBarrier(t *testing.T) {
	path := os.Getenv("TEST_SYSTEM_BACKUP_CAPTURE_FIXTURE_FILE")
	if path == "" {
		t.Skip("requires owned Kubernetes capture fixture")
	}
	var cfg realCaptureFixture
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("fixture unavailable")
	}
	defer clear(raw)
	if strictDecode(raw, &cfg) != nil || cfg.Owner != "cm-backup-first-20261024-0543" || cfg.Request.Namespace != cfg.Owner || cfg.KubeServer != "https://10.130.14.23:6443" || !regexp.MustCompile(`^[a-f0-9]{48}$`).MatchString(cfg.GatePassword) {
		t.Fatal("fixture identity invalid")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client, err := kubernetes.NewForConfig(&rest.Config{Host: cfg.KubeServer, BearerToken: cfg.ControllerToken, Timeout: 10 * time.Second, TLSClientConfig: rest.TLSClientConfig{CAData: []byte(cfg.KubeCA), ServerName: "10.130.14.23"}})
	if err != nil {
		t.Fatal("fixture client unavailable")
	}
	namespace, err := client.CoreV1().Namespaces().Get(ctx, cfg.Request.Namespace, metav1.GetOptions{})
	if err != nil || namespace.Labels["backup-test-owner"] != cfg.Owner || string(namespace.UID) != cfg.Request.NamespaceUID {
		t.Fatal("unowned capture fixture")
	}
	if _, err = client.CoreV1().Secrets(cfg.Request.Namespace).Get(ctx, "fixture-source-secret", metav1.GetOptions{}); !apierrors.IsForbidden(err) {
		t.Fatal("controller unexpectedly has direct source Secret access")
	}
	if _, err = client.CoreV1().Secrets(cfg.Request.Namespace).List(ctx, metav1.ListOptions{}); !apierrors.IsForbidden(err) {
		t.Fatal("controller unexpectedly has broad Secret access")
	}
	db, err := OpenControlDatabase(ctx, cfg.ControlDSNFile)
	if err != nil {
		t.Fatal("fixture database unavailable")
	}
	defer db.Close()
	ddl, err := os.ReadFile(filepath.Join(filepath.Dir(path), "control-ddl.sql"))
	if err != nil {
		t.Fatal("fixture migrations unavailable")
	}
	var initialized int
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='system_backup_capture_jobs'").Scan(&initialized); err != nil {
		t.Fatal("fixture schema status unavailable")
	}
	if initialized == 0 {
		for _, statement := range strings.Split(string(ddl), ";") {
			if strings.TrimSpace(statement) != "" {
				if _, err = db.ExecContext(ctx, statement); err != nil {
					t.Fatal("fixture migration failed", err)
				}
			}
		}
	}
	if _, err = db.ExecContext(ctx, "CREATE USER IF NOT EXISTS 'gate_reader'@'%' IDENTIFIED BY '"+cfg.GatePassword+"'"); err != nil {
		t.Fatal("fixture gate reader creation failed")
	}
	for _, table := range []string{"system_backup_gate", "system_backup_participants"} {
		if _, err = db.ExecContext(ctx, "GRANT SELECT ON capture_fixture."+table+" TO 'gate_reader'@'%'"); err != nil {
			t.Fatal("fixture gate read grant failed")
		}
	}
	g := Gate{db}
	if err = g.EnsureScope(ctx, cfg.Request.Gate.Scope); err != nil {
		t.Fatal(err)
	}
	request, _ := json.Marshal(cfg.Request)
	a := Attempt{ID: cfg.AttemptID, TaskID: cfg.Request.BackupID, Phase: "secret_capture", Status: "running", Request: request}
	j := CaptureJobs{Client: client}
	job, err := j.Ensure(ctx, a, "", true)
	if err != nil {
		t.Fatal("capture Job creation failed", err)
	}
	bound := string(job.UID)
	defer func() {
		if t.Failed() && os.Getenv("TEST_SYSTEM_BACKUP_KEEP_FAILED_CAPTURE_JOB") == "1" {
			t.Logf("retained owned failed capture Job=%s name=%s for diagnosis", bound, job.Name)
			return
		}
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		for cleanup.Err() == nil {
			done, err := j.Stop(cleanup, a, bound)
			if err != nil {
				t.Error("capture Job cleanup failed", err)
				return
			}
			if done {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Error("capture Job cleanup timed out")
	}()
	for {
		r, err := j.Observe(ctx, a, bound)
		if err != nil {
			t.Fatal("capture readiness observation failed", err)
		}
		if r.State == "ready" {
			break
		}
		if r.State == "failed" || ctx.Err() != nil {
			t.Fatal("capture Job did not prepare")
		}
		time.Sleep(250 * time.Millisecond)
	}
	entries, err := os.ReadDir(cfg.StageDirectory)
	if err != nil || len(entries) != 0 {
		t.Fatal("source was captured before the gate was held", err)
	}
	own, err := g.Begin(ctx, cfg.Request.Gate.Scope, cfg.Request.BackupID, RegistryHash(), 5*time.Second, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if e := g.BeginRelease(context.Background(), own); e != nil {
			t.Error(e)
			return
		}
		if e := g.FinishRelease(context.Background(), own); e != nil {
			t.Error(e)
		}
	}()
	own, err = g.Hold(ctx, own)
	if err != nil || own != cfg.Request.Gate {
		t.Fatal("capture fence mismatch", own, err)
	}
	var result CaptureJobResult
	for {
		result, err = j.Observe(ctx, a, bound)
		if err != nil {
			t.Fatal("capture result observation failed", err)
		}
		if result.State == "sealed" {
			break
		}
		if result.State == "failed" || ctx.Err() != nil {
			t.Fatal("capture Job did not seal")
		}
		time.Sleep(250 * time.Millisecond)
	}
	if result.Candidate == nil || len(result.Candidate.Parts) != 1 || result.Candidate.Parts[0].Name != "secret_bundle" {
		t.Fatal("invalid sealed Secret part")
	}
	key, err := os.ReadFile(cfg.KEKFile)
	if err != nil {
		t.Fatal("fixture KEK unavailable")
	}
	defer clear(key)
	part := result.Candidate.Parts[0]
	wrapper := AESKeyWrapper{Reference: part.Key.Reference, Key: key}
	f, err := os.Open(filepath.Join(cfg.StageDirectory, "secret_bundle.enc"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var plain bytes.Buffer
	defer func() { clear(plain.Bytes()) }()
	if err = OpenPart(ctx, f, &plain, Manifest{OriginInstallationID: cfg.Request.Origin, BackupID: cfg.Request.BackupID}, part, wrapper, 1<<20); err != nil {
		t.Fatal("sealed part verification failed", PublicError(err))
	}
	count := 0
	err = ReadKubernetesHistory(ctx, bytes.NewReader(plain.Bytes()), "secret_bundle", ObjectBudget{MaxBytes: 1 << 20, MaxItems: 1000}, func(selection KubernetesSelection, raw json.RawMessage) error {
		var secret struct {
			Data map[string]string `json:"data"`
		}
		if json.Unmarshal(raw, &secret) != nil || selection.UID != cfg.SourceUID {
			return context.Canceled
		}
		value, e := base64.StdEncoding.DecodeString(secret.Data["token"])
		if e != nil {
			return e
		}
		defer clear(value)
		h := sha256.Sum256(value)
		if hex.EncodeToString(h[:]) != cfg.SecretHash {
			return context.Canceled
		}
		count++
		return nil
	})
	if err != nil || count != 1 {
		t.Fatal("Secret data or identity did not survive capture", count, PublicError(err))
	}
	t.Logf("verified real capture Job=%s namespace=%s stage=%s generation=%d parts=%d", bound, cfg.Request.NamespaceUID, cfg.Request.Stage.UID, own.Generation, count)
}
