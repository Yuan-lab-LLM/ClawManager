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

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func verifyRestoredWithReadOnlyAccounts(t *testing.T, ctx context.Context, store ImmutableObjectStore, artifact PublishedArtifact, wrapper AESKeyWrapper, pub ed25519.PublicKey, report DataRestoreReport, target DataRestoreTarget, verifierDSN string, budget CaptureBudget) {
	t.Helper()
	user := os.Getenv("TEST_SYSTEM_BACKUP_RESTORED_OBJECT_USER")
	if user == "" {
		return
	}
	objects, e := minio.New("127.0.0.1:"+os.Getenv("TEST_SYSTEM_BACKUP_MINIO_PORT"), &minio.Options{Creds: credentials.NewStaticV4(user, os.Getenv("TEST_SYSTEM_BACKUP_RESTORED_OBJECT_PASSWORD"), ""), Secure: false})
	if e != nil {
		t.Fatal(e)
	}
	for _, bucket := range target.BucketMapping {
		if e = objects.RemoveObject(ctx, bucket, "permission-probe", minio.RemoveObjectOptions{}); minio.ToErrorResponse(e).Code != "AccessDenied" {
			t.Fatal("restore verifier can delete objects", e)
		}
		if _, e = objects.PutObject(ctx, bucket, "permission-probe", bytes.NewReader(nil), 0, minio.PutObjectOptions{}); minio.ToErrorResponse(e).Code != "AccessDenied" {
			t.Fatal("restore verifier can write objects", e)
		}
	}
	redisUser, _ := RandomID("verify_")
	password, _ := RandomID("secret_")
	if _, e = target.Redis.command(ctx, []byte("ACL"), []byte("SETUSER"), []byte(redisUser), []byte("reset"), []byte("on"), []byte(">"+password), []byte("~cm:restore-history:"+report.RunID+":*"), []byte("+select"), []byte("+eval_ro"), []byte("+dump"), []byte("+type"), []byte("+pttl"), []byte("+dbsize")); e != nil {
		t.Fatal(e)
	}
	defer target.Redis.command(context.Background(), []byte("ACL"), []byte("DELUSER"), []byte(redisUser))
	redis, e := ConnectRedis(ctx, RedisOptions{Database: redisFixtureDatabase(t), Address: "127.0.0.1:" + os.Getenv("TEST_SYSTEM_BACKUP_REDIS_TARGET_PORT"), Username: redisUser, Password: password, MaxReplyBytes: 8 << 20})
	if e != nil {
		t.Fatal(e)
	}
	defer redis.Close()
	if _, e = redis.command(ctx, []byte("SET"), []byte("cm:restore-history:"+report.RunID+":forbidden"), []byte("forbidden")); e == nil {
		t.Fatal("restore verifier can write Redis")
	}
	intent := RestoreVerificationIntent{RunID: report.RunID, NormalizedAt: report.StartedAt, HistoryParts: report.HistoryParts}
	reader := RestoredDataReader{MySQL: target.Verifier, Database: target.Database, Redis: redis, Objects: objects, BucketMapping: target.BucketMapping, Directory: target.Directory}
	got, e := VerifyRestoredData(ctx, store, artifact, wrapper, intent, reader, budget)
	if e != nil || !got.IntegrityVerified || got.AcceptanceEligible || got.HistoryPartsVerified != 2 || got.FileCount != report.FileCount || got.RedisCount != report.VerifiedRedisCount || got.ObjectCount != report.VerifiedObjectCount {
		t.Fatal("independent read-only restore verification", e)
	}
	if exe := os.Getenv("TEST_SYSTEM_BACKUP_VERIFY_EXE"); exe != "" {
		dir := t.TempDir()
		write := func(name string, raw []byte) string {
			path := filepath.Join(dir, name)
			if e := os.WriteFile(path, raw, 0600); e != nil {
				t.Fatal(e)
			}
			return path
		}
		jsonFile := func(name string, v any) string {
			raw, e := json.Marshal(v)
			if e != nil {
				t.Fatal(e)
			}
			return write(name, raw)
		}
		endpoint := "127.0.0.1:" + os.Getenv("TEST_SYSTEM_BACKUP_MINIO_PORT")
		archiveCred := jsonFile("archive.json", map[string]string{"access_key": os.Getenv("TEST_SYSTEM_BACKUP_MINIO_USER"), "secret_key": os.Getenv("TEST_SYSTEM_BACKUP_MINIO_PASSWORD")})
		bootstrap := jsonFile("bootstrap.json", map[string]any{"endpoint": endpoint, "bucket": store.Bucket, "root": store.Root, "secure": false, "credentials_file": archiveCred, "verification_key_file": write("verify.pub", pub), "verification_key_version": "v1", "encryption_key_file": write("encryption.key", wrapper.Key), "encryption_reference": wrapper.Reference})
		config := jsonFile("verify-restored.json", map[string]any{"intent": intent, "verifier_dsn_file": write("verify.dsn", []byte(verifierDSN)), "redis": map[string]any{"address": "127.0.0.1:" + os.Getenv("TEST_SYSTEM_BACKUP_REDIS_TARGET_PORT"), "database": redisFixtureDatabase(t), "secure": false, "credentials_file": jsonFile("redis.json", map[string]string{"username": redisUser, "password": password})}, "objects": map[string]any{"endpoint": endpoint, "secure": false, "credentials_file": jsonFile("target-objects.json", map[string]string{"access_key": user, "secret_key": os.Getenv("TEST_SYSTEM_BACKUP_RESTORED_OBJECT_PASSWORD")})}, "bucket_mapping": target.BucketMapping, "directory": target.Directory})
		aid, _ := RandomID("sat_")
		binding := DataOperationBinding{Operation: "verify-restored", RunID: report.RunID, AttemptID: aid, TargetUID: "isolated-fixture-target", Origin: artifact.Manifest.Payload.OriginInstallationID, BackupID: artifact.Manifest.Payload.BackupID, CommitSHA256: artifact.CommitReference.SHA256}
		binding.IntentSHA256, _ = RequestHash(intent)
		bindingRaw, _ := json.Marshal(binding)
		termination := filepath.Join(dir, "termination.json")
		cmd := exec.CommandContext(ctx, exe, "--operation", "verify-restored", "--bootstrap", bootstrap, "--operation-config", config, "--origin", artifact.Manifest.Payload.OriginInstallationID, "--backup", artifact.Manifest.Payload.BackupID, "--max-bytes", "67108864", "--max-items", "10000", "--timeout", "2m", "--data-request", base64.RawURLEncoding.EncodeToString(bindingRaw), "--report-file", termination)
		raw, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("read-only verifier CLI failed: %s (%v)", raw, e)
		}
		var result DataOperationReceipt
		saved, e := os.ReadFile(termination)
		want, receiptErr := RestoredVerificationReceipt(binding, intent, got)
		if e != nil || receiptErr != nil || !bytes.Equal(saved, raw) || len(raw) > 4096 || json.Unmarshal(raw, &result) != nil || !result.IntegrityVerified || result.AcceptanceEligible || result.DataOperationBinding != binding || result.SQLSHA256 != want.SQLSHA256 {
			t.Fatal("invalid independent verifier CLI report")
		}
	}
	// A valid original archive must not conceal damaged restored history.
	history := filepath.Join(target.Directory, "encrypted-history", intent.HistoryParts[0].Name+".enc")
	original, e := os.ReadFile(history)
	if e != nil {
		t.Fatal(e)
	}
	changed := bytes.Clone(original)
	changed[len(changed)/2] ^= 1
	if e = os.WriteFile(history, changed, 0600); e != nil {
		t.Fatal(e)
	}
	defer os.WriteFile(history, original, 0600)
	if _, e = VerifyRestoredData(ctx, store, artifact, wrapper, intent, reader, budget); e == nil {
		t.Fatal("read-only verifier missed damaged restored encrypted history")
	}
	t.Log("independent restored-data verifier: SELECT-only SQL, read-only Redis ACL/object identity, all six data parts, encrypted-history tamper detected")
}
