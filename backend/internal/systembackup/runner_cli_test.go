package systembackup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func runRestoreCLI(t *testing.T, ctx context.Context, exe string, store ImmutableObjectStore, artifact PublishedArtifact, wrapper AESKeyWrapper, pub ed25519.PublicKey, run string, target DataRestoreTarget, verifierDSN string) DataRestoreReport {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, raw []byte) string {
		p := filepath.Join(dir, name)
		if e := os.WriteFile(p, raw, 0600); e != nil {
			t.Fatal(e)
		}
		return p
	}
	writeJSON := func(name string, value any) string {
		raw, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		return write(name, raw)
	}
	credentials := writeJSON("objects.json", map[string]string{"access_key": os.Getenv("TEST_SYSTEM_BACKUP_MINIO_USER"), "secret_key": os.Getenv("TEST_SYSTEM_BACKUP_MINIO_PASSWORD")})
	bootstrap := writeJSON("bootstrap.json", map[string]any{"endpoint": "127.0.0.1:" + os.Getenv("TEST_SYSTEM_BACKUP_MINIO_PORT"), "bucket": store.Bucket, "root": store.Root, "secure": false, "credentials_file": credentials, "verification_key_file": write("verify.pub", pub), "verification_key_version": "v1", "encryption_key_file": write("encryption.key", wrapper.Key), "encryption_reference": wrapper.Reference})
	var controlName string
	if e := target.Control.DB.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&controlName); e != nil {
		t.Fatal(e)
	}
	dsn := func(name string) string {
		c := mysql.Config{User: "root", Net: "tcp", Addr: "127.0.0.1:" + os.Getenv("TEST_SYSTEM_BACKUP_MYSQL_PORT"), DBName: name, ParseTime: true, AllowNativePasswords: true}
		return c.FormatDSN()
	}
	config := writeJSON("restore.json", map[string]any{"run_id": run, "mysql_dsn_file": write("target.dsn", []byte(dsn(target.Database))), "verifier_dsn_file": write("verify.dsn", []byte(verifierDSN)), "control_dsn_file": write("control.dsn", []byte(dsn(controlName))), "control_installation_id": target.Control.InstallationID, "redis": map[string]any{"address": "127.0.0.1:" + os.Getenv("TEST_SYSTEM_BACKUP_REDIS_TARGET_PORT"), "database": redisFixtureDatabase(t), "secure": false, "credentials_file": writeJSON("redis.json", map[string]string{"username": "", "password": os.Getenv("TEST_SYSTEM_BACKUP_REDIS_PASSWORD")})}, "objects": map[string]any{"endpoint": "127.0.0.1:" + os.Getenv("TEST_SYSTEM_BACKUP_MINIO_PORT"), "secure": false, "credentials_file": credentials}, "bucket_mapping": target.BucketMapping, "directory": target.Directory})
	aid, _ := RandomID("sat_")
	binding := DataOperationBinding{Operation: "restore", RunID: run, AttemptID: aid, TargetUID: "isolated-fixture-target", Origin: artifact.Manifest.Payload.OriginInstallationID, BackupID: artifact.Manifest.Payload.BackupID, CommitSHA256: artifact.CommitReference.SHA256}
	bindingRaw, _ := json.Marshal(binding)
	termination := filepath.Join(dir, "termination.json")
	cmd := exec.CommandContext(ctx, exe, "--operation", "restore", "--bootstrap", bootstrap, "--operation-config", config, "--origin", artifact.Manifest.Payload.OriginInstallationID, "--backup", artifact.Manifest.Payload.BackupID, "--max-bytes", "67108864", "--max-items", "10000", "--timeout", "2m", "--data-request", base64.RawURLEncoding.EncodeToString(bindingRaw), "--report-file", termination)
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("independent restore executable failed: %s (%v)", out, e)
	}
	var receipt DataOperationReceipt
	saved, e := os.ReadFile(termination)
	if e != nil || len(saved) > 4096 || !bytes.Equal(saved, out) || json.Unmarshal(out, &receipt) != nil || receipt.DataOperationBinding != binding || !receipt.IntegrityVerified || receipt.AcceptanceEligible {
		t.Fatal("restore CLI bounded receipt invalid", e)
	}
	raw, e := os.ReadFile(filepath.Join(target.Directory, "restore-report.json"))
	var report DataRestoreReport
	if e != nil || json.Unmarshal(raw, &report) != nil {
		t.Fatal("restore full report invalid", e)
	}
	want, e := RestoreReceipt(binding, report)
	if e != nil || want.SQLSHA256 != receipt.SQLSHA256 || !want.NormalizedAt.Equal(receipt.NormalizedAt) {
		t.Fatal("bounded receipt disagrees with full readback", e)
	}
	return report
}

func runCaptureCLI(t *testing.T, ctx context.Context, exe string, store ImmutableObjectStore, source CaptureSources, stage, origin, id string, wrapper AESKeyWrapper, pub ed25519.PublicKey, signer ed25519.PrivateKey) {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, raw []byte) string {
		p := filepath.Join(dir, name)
		if e := os.WriteFile(p, raw, 0600); e != nil {
			t.Fatal(e)
		}
		return p
	}
	writeJSON := func(name string, value any) string {
		raw, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		return write(name, raw)
	}
	credentials := writeJSON("objects.json", map[string]string{"access_key": os.Getenv("TEST_SYSTEM_BACKUP_MINIO_USER"), "secret_key": os.Getenv("TEST_SYSTEM_BACKUP_MINIO_PASSWORD")})
	endpoint := "127.0.0.1:" + os.Getenv("TEST_SYSTEM_BACKUP_MINIO_PORT")
	bootstrap := writeJSON("bootstrap.json", map[string]any{"endpoint": endpoint, "bucket": store.Bucket, "root": store.Root, "secure": false, "credentials_file": credentials, "verification_key_file": write("verify.pub", pub), "verification_key_version": "v1", "signing_key_file": write("sign.key", signer), "encryption_key_file": write("encryption.key", wrapper.Key), "encryption_reference": wrapper.Reference})
	var database string
	if e := source.MySQL.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&database); e != nil {
		t.Fatal(e)
	}
	dsn := mysql.Config{User: "root", Net: "tcp", Addr: "127.0.0.1:" + os.Getenv("TEST_SYSTEM_BACKUP_MYSQL_PORT"), DBName: database, ParseTime: true, AllowNativePasswords: true}
	raw, e := os.ReadFile(os.Getenv("TEST_SYSTEM_BACKUP_KUBE_AUTH_FILE"))
	if e != nil {
		t.Fatal(e)
	}
	var auth struct {
		CA            string `json:"ca"`
		ResourceToken string `json:"resource_token"`
		SecretToken   string `json:"secret_token"`
	}
	if e = json.Unmarshal(raw, &auth); e != nil {
		t.Fatal("Kubernetes fixture credential invalid")
	}
	clear(raw)
	ca := write("kube-ca.pem", []byte(auth.CA))
	kube := func(name, token string) map[string]string {
		return map[string]string{"server": "https://127.0.0.1:36443", "server_name": "10.130.14.23", "ca_file": ca, "token_file": write(name, []byte(token))}
	}
	roots := []map[string]string{}
	for _, r := range source.Workspace {
		roots = append(roots, map[string]string{"id": r.ID, "path": r.Path})
	}
	config := writeJSON("capture.json", map[string]any{"mysql_dsn_file": write("source.dsn", []byte(dsn.FormatDSN())), "redis": map[string]any{"address": "127.0.0.1:" + os.Getenv("TEST_SYSTEM_BACKUP_REDIS_PORT"), "database": redisFixtureDatabase(t), "secure": false, "credentials_file": writeJSON("redis.json", map[string]string{"username": "", "password": os.Getenv("TEST_SYSTEM_BACKUP_REDIS_PASSWORD")})}, "objects": map[string]any{"endpoint": endpoint, "secure": false, "credentials_file": credentials}, "buckets": source.ObjectBuckets, "workspace": roots, "deployment_client": kube("resource.token", auth.ResourceToken), "secret_client": kube("secret.token", auth.SecretToken), "namespace": source.Namespace, "namespace_uid": source.NamespaceUID, "deployments": source.Deployments, "secrets": source.Secrets, "stage_directory": stage})
	cmd := exec.CommandContext(ctx, exe, "--operation", "capture", "--bootstrap", bootstrap, "--operation-config", config, "--origin", origin, "--backup", id, "--max-bytes", "67108864", "--max-items", "10000", "--timeout", "2m")
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("independent capture executable failed: %s (%v)", out, e)
	}
	var report struct {
		Status   string `json:"status"`
		Eligible bool   `json:"acceptance_eligible"`
	}
	if json.Unmarshal(out, &report) != nil || report.Status != "published" || report.Eligible {
		t.Fatal("capture CLI report invalid")
	}
	entries, e := os.ReadDir(stage)
	if e != nil || len(entries) != 0 {
		t.Fatal("successful capture staging was not cleaned", e)
	}
}
