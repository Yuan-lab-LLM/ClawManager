package systembackup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"clawreef/internal/historymaintenance"
	"clawreef/internal/observability"
	"github.com/minio/minio-go/v7"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

func TestTeamHistoryNativeBundleRestoresMonthlyObjectsAndImmutableKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	db := fullSchemaDB(t)
	sourceObjects := testObjectStore(t)
	backup := testObjectStore(t)
	master := bytes.Repeat([]byte{9}, 32)
	objects := historyTestObjects{sourceObjects, "app/native/base/"}
	encrypted, err := observability.NewEncryptedObjects(objects, master)
	if err != nil {
		t.Fatal(err)
	}
	body, err := encrypted.Put(ctx, "user:12", []byte("full Team body after isolated restore"))
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"id": 17, "team_id": 12, "content_body_ref": body})
	archiveRef, err := encrypted.Put(ctx, "user:12", payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = encrypted.Put(ctx, "user:12", []byte("unrelated AI payload must stay outside Team copy")); err != nil {
		t.Fatal(err)
	}
	if err = historymaintenance.New(db, nil, nil).PrepareMonths(ctx, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	refJSON, _ := json.Marshal(archiveRef)
	if _, err = db.Exec(`INSERT INTO history_archive_records(kind,original_id,owner_id,team_id,created_at,month_table,object_ref,payload_sha256,payload_bytes,metadata_json,archived_at,readback_verified_at) VALUES('team_events',17,12,12,'2026-11-02','team_events_202611',?,?,?,'{}',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`, string(refJSON), archiveRef.SHA256, len(payload)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO team_events_202611(original_id,owner_id,created_at,trace_id,instance_id,object_ref,payload_sha256,payload_bytes) VALUES(17,12,'2026-11-02','',0,?,?,?)`, string(refJSON), archiveRef.SHA256, len(payload)); err != nil {
		t.Fatal(err)
	}
	secret := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": observationKeySecret, "namespace": "team-history", "uid": "history-key-uid"}, "immutable": true, "data": map[string]any{"master-key": base64.StdEncoding.EncodeToString([]byte(base64.StdEncoding.EncodeToString(master)))}}}
	kube := fake.NewSimpleDynamicClient(runtime.NewScheme(), secret)
	redis := redisFixture(t, "TEST_SYSTEM_BACKUP_REDIS_PORT")
	source := CaptureSources{MySQL: db, Redis: redis, Objects: sourceObjects.Client, ObjectBuckets: []string{sourceObjects.Bucket}, Workspace: []FileRoot{{ID: "team_shared", Path: t.TempDir()}}, Namespace: "team-history", NamespaceUID: "namespace-uid", DeploymentClient: kube, SecretClient: kube, TeamDataOnly: true}
	if err = freezeTeamHistoryKey(ctx, &source); err != nil {
		t.Fatal(err)
	}
	_, signer, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wrapper := AESKeyWrapper{Reference: SecretRef{Provider: "kubernetes", Name: "test-history-wrapper", Version: "v1", Purpose: "encryption"}, Key: bytes.Repeat([]byte{4}, 32)}
	id, _ := RandomID("sbk_")
	stage := t.TempDir()
	budget := CaptureBudget{64 << 20, 10000}
	manifest, err := CaptureFunctionalBundle(ctx, stage, "team-history-fixture", id, source, budget, wrapper, signer, "v1")
	if err != nil {
		t.Fatal("native capture", err)
	}
	artifact, err := PublishFunctionalArtifact(ctx, backup, stage, manifest, signer, "v1", budget.MaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidatePublishedSnapshot(ctx, backup, artifact, wrapper, budget); err != nil {
		t.Fatal("published hydration", err)
	}
	target, name := isolatedSchemaDB(t, db)
	reader := selectOnlyVerifier(t, db, name)
	bucket, _ := RandomID("cm-restore-")
	if err = sourceObjects.Client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for o := range sourceObjects.Client.ListObjects(context.Background(), bucket, minio.ListObjectsOptions{Recursive: true}) {
			sourceObjects.Client.RemoveObject(context.Background(), bucket, o.Key, minio.RemoveObjectOptions{})
		}
		sourceObjects.Client.RemoveBucket(context.Background(), bucket)
	})
	run, _ := RandomID("sdr_")
	targetRedis := redisFixture(t, "TEST_SYSTEM_BACKUP_REDIS_TARGET_PORT")
	t.Cleanup(func() {
		cursor := []byte("0")
		for {
			reply, e := targetRedis.command(context.Background(), []byte("SCAN"), cursor, []byte("MATCH"), []byte("cm:restore-history:"+run+":*"), []byte("COUNT"), []byte("100"))
			if e != nil {
				t.Error(e)
				return
			}
			parts := reply.([]any)
			cursor = parts[0].([]byte)
			for _, key := range parts[1].([]any) {
				if _, e = targetRedis.command(context.Background(), []byte("DEL"), key.([]byte)); e != nil {
					t.Error(e)
				}
			}
			if string(cursor) == "0" {
				return
			}
		}
	})
	dir := t.TempDir()
	mapping := map[string]string{sourceObjects.Bucket: bucket}
	restore := DataRestoreTarget{MySQL: target, Verifier: reader, Database: name, Redis: targetRedis, Objects: sourceObjects.Client, BucketMapping: mapping, Directory: dir, Control: Store{DB: controlDB(t), InstallationID: "history-restore-control"}}
	report, err := RestorePublishedData(ctx, backup, artifact, wrapper, run, restore, budget)
	if err != nil {
		t.Fatal("native isolated restore", err)
	}
	verifyTarget := RestoredDataReader{MySQL: reader, Database: name, Redis: targetRedis, Objects: sourceObjects.Client, BucketMapping: mapping, Directory: dir}
	intent := RestoreVerificationIntent{RunID: run, NormalizedAt: report.StartedAt, HistoryParts: report.HistoryParts}
	verified, err := VerifyRestoredData(ctx, backup, artifact, wrapper, intent, verifyTarget, budget)
	if err != nil {
		t.Fatal("native read-only verification", err)
	}
	if !verified.IntegrityVerified || verified.HistoryBodiesVerified != 2 || verified.ObjectCount != 2 || verified.SQL.Tables["team_events_202611"] != 1 {
		t.Fatalf("incomplete historical recovery: %+v", verified)
	}
	if _, err = sourceObjects.Client.PutObject(ctx, bucket, objects.prefix+body.ObjectKey, bytes.NewReader([]byte("corrupt")), 7, minio.PutObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err = VerifyRestoredData(ctx, backup, artifact, wrapper, intent, verifyTarget, budget); err == nil {
		t.Fatal("corrupt actual restored body accepted")
	}
	t.Log("native six-part Team capture/publish/isolated SQL+object restore passed, two hydrated bodies, unrelated AI object omitted, corruption rejected; key remained historical encrypted evidence")
}
