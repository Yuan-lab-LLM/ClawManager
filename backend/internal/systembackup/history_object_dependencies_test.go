package systembackup

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"testing"

	"clawreef/internal/observability"
	"github.com/minio/minio-go/v7"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
)

type historyTestObjects struct {
	store  ImmutableObjectStore
	prefix string
}

func (s historyTestObjects) PutObject(ctx context.Context, key string, raw []byte, ct string) error {
	_, err := s.store.Client.PutObject(ctx, s.store.Bucket, s.prefix+key, bytes.NewReader(raw), int64(len(raw)), minio.PutObjectOptions{ContentType: ct})
	return err
}
func (s historyTestObjects) GetObject(ctx context.Context, key string) ([]byte, error) {
	o, err := s.store.Client.GetObject(ctx, s.store.Bucket, s.prefix+key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer o.Close()
	return io.ReadAll(o)
}

func TestHistoryObjectDependencyRequiresCapturedKeyAndHydratesRestoredBytes(t *testing.T) {
	ctx := context.Background()
	store := testObjectStore(t)
	objects := historyTestObjects{store, "native/base/path/"}
	key := bytes.Repeat([]byte{7}, 32)
	enc, err := observability.NewEncryptedObjects(objects, key)
	if err != nil {
		t.Fatal(err)
	}
	body, err := enc.Put(ctx, "user:12", []byte("full original body"))
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"body_ref": body})
	ref, err := enc.Put(ctx, "user:12", payload)
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err = CaptureObjects(ctx, store.Client, &archive, []string{store.Bucket}, ObjectBudget{1 << 20, 100}); err != nil {
		t.Fatal(err)
	}
	rawKey, _ := json.Marshal(map[string]any{"immutable": true, "data": map[string]string{"master-key": base64.StdEncoding.EncodeToString([]byte(base64.StdEncoding.EncodeToString(key)))}})
	snapshot := func(fn func(ObjectMetadata) (io.WriteCloser, error)) error {
		return ReadObjectSnapshot(ctx, bytes.NewReader(archive.Bytes()), ObjectBudget{1 << 20, 100}, fn)
	}
	if _, err = verifyHistoryObjectDependencies(ctx, []observability.BodyRef{ref}, newHistoryInventory(), nil, snapshot, nil, ObjectBudget{1 << 20, 100}); err == nil {
		t.Fatal("missing encryption key accepted")
	}
	if n, err := verifyHistoryObjectDependencies(ctx, []observability.BodyRef{ref}, newHistoryInventory(), rawKey, snapshot, nil, ObjectBudget{1 << 20, 100}); err != nil || len(n) != 2 {
		t.Fatalf("nested bodies not hydrated: %d %v", len(n), err)
	}
	target, _ := RandomID("cm-restore-")
	if err = store.Client.MakeBucket(ctx, target, minio.MakeBucketOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for o := range store.Client.ListObjects(ctx, target, minio.ListObjectsOptions{Recursive: true}) {
			store.Client.RemoveObject(ctx, target, o.Key, minio.RemoveObjectOptions{})
		}
		store.Client.RemoveBucket(ctx, target)
	})
	mapping := map[string]string{store.Bucket: target}
	if err = RestoreObjects(ctx, store.Client, bytes.NewReader(archive.Bytes()), ObjectBudget{1 << 20, 100}, mapping); err != nil {
		t.Fatal(err)
	}
	reader := &RestoredDataReader{Objects: store.Client, BucketMapping: mapping}
	if n, err := verifyHistoryObjectDependencies(ctx, []observability.BodyRef{ref}, newHistoryInventory(), rawKey, snapshot, reader, ObjectBudget{1 << 20, 100}); err != nil || len(n) != 2 {
		t.Fatalf("actual restored hydration: %d %v", len(n), err)
	}
	if _, err = store.Client.PutObject(ctx, target, objects.prefix+body.ObjectKey, bytes.NewReader([]byte("corrupt")), 7, minio.PutObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err = verifyHistoryObjectDependencies(ctx, []observability.BodyRef{ref}, newHistoryInventory(), rawKey, snapshot, reader, ObjectBudget{1 << 20, 100}); err == nil {
		t.Fatal("corrupt restored nested body accepted using healthy archive")
	}
	// Native Team capture retains this archive and nested body, while an AI
	// body for the same owner in the same bucket must not enter the Team copy.
	unrelated, err := enc.Put(ctx, "user:12", []byte("unrelated AI body"))
	if err != nil {
		t.Fatal(err)
	}
	db := fixtureDatabase(t)
	for _, q := range []string{`CREATE TABLE history_archive_records(kind VARCHAR(32),object_ref JSON)`, `CREATE TABLE team_redis_archive_receipts(object_ref JSON)`, `CREATE TABLE team_redis_dlq_receipts(object_ref JSON)`} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	refRaw, _ := json.Marshal(ref)
	if _, err = db.Exec(`INSERT INTO history_archive_records VALUES('team_tasks',?)`, string(refRaw)); err != nil {
		t.Fatal(err)
	}
	secret := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": observationKeySecret, "namespace": "team-fixture", "uid": "immutable-key-uid"}, "immutable": true, "data": map[string]any{"master-key": base64.StdEncoding.EncodeToString([]byte(base64.StdEncoding.EncodeToString(key)))}}}
	source := CaptureSources{MySQL: db, Objects: store.Client, ObjectBuckets: []string{store.Bucket}, Namespace: "team-fixture", SecretClient: fake.NewSimpleDynamicClient(runtime.NewScheme(), secret), TeamDataOnly: true}
	if err = freezeTeamHistoryKey(ctx, &source); err != nil || len(source.Secrets) != 1 {
		t.Fatal("native Team source failed to freeze required key", err)
	}
	var teamObjects bytes.Buffer
	if err = captureTeamDataHistoryObjects(ctx, &teamObjects, source, ObjectBudget{1 << 20, 100}); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	if err = ReadObjectSnapshot(ctx, bytes.NewReader(teamObjects.Bytes()), ObjectBudget{1 << 20, 100}, func(m ObjectMetadata) (io.WriteCloser, error) { seen[m.Key] = true; return historyDiscard{}, nil }); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || !seen[objects.prefix+ref.ObjectKey] || !seen[objects.prefix+body.ObjectKey] || seen[objects.prefix+unrelated.ObjectKey] {
		t.Fatalf("Team object confidentiality/dependency scope differs: %v", seen)
	}
}
