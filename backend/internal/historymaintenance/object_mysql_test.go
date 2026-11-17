package historymaintenance

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"clawreef/internal/observability"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type fixtureObjectStore struct {
	client *minio.Client
	bucket string
}

func (s fixtureObjectStore) PutObject(ctx context.Context, key string, raw []byte, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(raw), int64(len(raw)), minio.PutObjectOptions{ContentType: contentType})
	return err
}
func (s fixtureObjectStore) GetObject(ctx context.Context, key string) ([]byte, error) {
	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer object.Close()
	return io.ReadAll(io.LimitReader(object, MaxPageBytes+128))
}

type fixtureEncryptedArchive struct {
	objects *observability.EncryptedObjects
}

func (s fixtureEncryptedArchive) Put(ctx context.Context, owner, key string, raw []byte) ([]byte, error) {
	ref, err := s.objects.Put(ctx, owner, raw)
	if err != nil {
		return nil, err
	}
	return json.Marshal(ref)
}
func (s fixtureEncryptedArchive) Read(ctx context.Context, owner string, encoded []byte) ([]byte, error) {
	var ref observability.BodyRef
	if err := json.Unmarshal(encoded, &ref); err != nil {
		return nil, err
	}
	return s.objects.Get(ctx, owner, ref)
}
func minioFixture(t *testing.T) (*minio.Client, string, string) {
	t.Helper()
	path := os.Getenv("CM_HISTORY_MINIO_CREDENTIALS")
	if path == "" {
		t.Skip("dedicated MinIO fixture credential required")
	}
	if !strings.HasPrefix(path, "/root/cm-database-completion-20261118/fixtures/") {
		t.Fatal("isolated fixture credential path required")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("private fixture credential required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var key struct {
		Access string `json:"access_key"`
		Secret string `json:"secret_key"`
	}
	if err = json.Unmarshal(raw, &key); err != nil {
		t.Fatal(err)
	}
	clear(raw)
	client, err := minio.New("127.0.0.1:33319", &minio.Options{Creds: credentials.NewStaticV4(key.Access, key.Secret, ""), Secure: false})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	marker := "cm-history-fixture-marker"
	exists, err := client.BucketExists(ctx, marker)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		if err = client.MakeBucket(ctx, marker, minio.MakeBucketOptions{}); err != nil {
			t.Fatal(err)
		}
		data := []byte("cm_database_test_marker")
		if _, err = client.PutObject(ctx, marker, "marker", bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	object, err := client.GetObject(ctx, marker, "marker", minio.GetObjectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(io.LimitReader(object, 100))
	object.Close()
	if err != nil || string(data) != "cm_database_test_marker" {
		t.Fatal("object fixture marker required", err)
	}
	makeBucket := func(label string) string {
		random := make([]byte, 8)
		rand.Read(random)
		name := "cm-history-test-" + label + "-" + hex.EncodeToString(random)
		if err := client.MakeBucket(ctx, name, minio.MakeBucketOptions{}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			for object := range client.ListObjects(ctx, name, minio.ListObjectsOptions{Recursive: true}) {
				if object.Err != nil {
					t.Error(object.Err)
					continue
				}
				if err := client.RemoveObject(ctx, name, object.Key, minio.RemoveObjectOptions{}); err != nil {
					t.Error(err)
				}
			}
			if err := client.RemoveBucket(ctx, name); err != nil {
				t.Error(err)
			}
		})
		return name
	}
	return client, makeBucket("source"), makeBucket("restore")
}
func TestActualObjectArchiveCorruptionAndIsolatedRestore(t *testing.T) {
	db, target := fixture(t)
	client, sourceBucket, restoredBucket := minioFixture(t)
	key := bytes.Repeat([]byte{27}, 32)
	source := fixtureObjectStore{client, sourceBucket}
	restored := fixtureObjectStore{client, restoredBucket}
	encrypted, err := observability.NewEncryptedObjects(source, key)
	if err != nil {
		t.Fatal(err)
	}
	isolated, err := observability.NewEncryptedObjects(restored, key)
	if err != nil {
		t.Fatal(err)
	}
	svc := New(db, fixtureEncryptedArchive{encrypted}, nil)
	old := time.Now().UTC().AddDate(0, 0, -100)
	ctx := context.Background()
	exec(t, db, "INSERT INTO model_invocations VALUES(1,12,3,'object-real','session','completed',?,?,?)", old, old, "private-request-body-actual-MinIO")
	if _, err = svc.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	record, err := svc.metadata(ctx, "model_invocations", 1, 12, false)
	if err != nil {
		t.Fatal(err)
	}
	var ref observability.BodyRef
	if err = json.Unmarshal(record.Reference, &ref); err != nil {
		t.Fatal(err)
	}
	ciphertext, err := source.GetObject(ctx, ref.ObjectKey)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, []byte("private-request-body")) {
		t.Fatal("archive plaintext exposed in object store")
	}
	if err = restored.PutObject(ctx, ref.ObjectKey, ciphertext, "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	verified, err := isolated.Get(ctx, "user:12", ref)
	if err != nil || !bytes.Contains(verified, []byte("private-request-body")) {
		t.Fatal("isolated encrypted object restore failed", err)
	}
	if _, err = isolated.Get(ctx, "user:13", ref); err == nil {
		t.Fatal("restored object crossed owner boundary")
	}
	corrupted := append([]byte(nil), ciphertext...)
	corrupted[len(corrupted)-1] ^= 1
	if err = source.PutObject(ctx, ref.ObjectKey, corrupted, "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ProvePending(ctx, target, 500); err == nil {
		t.Fatal("corrupt archive granted source deletion proof")
	}
	if count(t, db, "model_invocations") != 1 {
		t.Fatal("corrupt source deleted")
	}
	if err = source.PutObject(ctx, ref.ObjectKey, ciphertext, "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	if n, err := svc.ProvePending(ctx, target, 500); err != nil || n != 1 {
		t.Fatalf("recovered restore %d %v", n, err)
	}
	if _, err = svc.CutoverPending(ctx, 500); err != nil {
		t.Fatal(err)
	}
	exec(t, db, "UPDATE history_archive_records SET summary_verified_at=UTC_TIMESTAMP(6),summary_sha256=payload_sha256")
	exec(t, db, "UPDATE history_maintenance_cursors SET after_id=0")
	if _, err = svc.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "model_invocations") != 0 {
		t.Fatal("fully proved source never removed")
	}
	if _, err = isolated.Get(ctx, "user:12", ref); err != nil {
		t.Fatal("independent object restore depended on online source", err)
	}
}
