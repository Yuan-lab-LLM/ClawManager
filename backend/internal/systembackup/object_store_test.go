package systembackup

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func testObjectStore(t *testing.T) ImmutableObjectStore {
	t.Helper()
	port := os.Getenv("TEST_SYSTEM_BACKUP_MINIO_PORT")
	if port == "" {
		t.Skip("requires disposable localhost MinIO")
	}
	client, e := minio.New("127.0.0.1:"+port, &minio.Options{Creds: credentials.NewStaticV4(os.Getenv("TEST_SYSTEM_BACKUP_MINIO_USER"), os.Getenv("TEST_SYSTEM_BACKUP_MINIO_PASSWORD"), ""), Secure: false})
	if e != nil {
		t.Fatal(e)
	}
	bucket, e := RandomID("cmbktest-")
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	if e = client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		for item := range client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true, WithVersions: true}) {
			if item.Err != nil {
				t.Error(item.Err)
				continue
			}
			if e := client.RemoveObject(ctx, bucket, item.Key, minio.RemoveObjectOptions{VersionID: item.VersionID}); e != nil {
				t.Error(e)
			}
		}
		if e := client.RemoveBucket(ctx, bucket); e != nil {
			t.Error(e)
		}
	})
	if e = client.SetBucketVersioning(ctx, bucket, minio.BucketVersioningConfiguration{Status: "Enabled"}); e != nil {
		t.Fatal(e)
	}
	return ImmutableObjectStore{Client: client, Bucket: bucket, Root: "test-archive"}
}

func TestImmutableStoreConditionalCreateAndExactVersion(t *testing.T) {
	store := testObjectStore(t)
	ctx := context.Background()
	if e := store.ProbeRead(ctx); e != nil {
		t.Fatal(e)
	}
	source := bytes.Repeat([]byte("encrypted-part\x00\xff"), 100)
	ref, e := store.PutImmutable(ctx, "installation/backup/part", source)
	if e != nil {
		t.Fatal(e)
	}
	replay, e := store.PutImmutable(ctx, "installation/backup/part", source)
	if e != nil || ref != replay {
		t.Fatal("idempotent publish changed version", ref, replay, e)
	}
	if _, e = store.PutImmutable(ctx, "installation/backup/part", []byte("different-content")); e == nil {
		t.Fatal("overwrote immutable part")
	}
	raw, e := store.Read(ctx, ref)
	if e != nil || !bytes.Equal(raw, source) {
		t.Fatal("original object changed", e)
	}
	damaged := ref
	damaged.SHA256 = strings.Repeat("0", 64)
	if _, e = store.Read(ctx, damaged); e == nil {
		t.Fatal("unchecked bytes returned")
	}
	var wg sync.WaitGroup
	success := make(chan ObjectReference, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if r, e := store.PutImmutable(ctx, "race/only-one", bytes.Repeat([]byte{byte(i + 1)}, 100)); e == nil {
				success <- r
			}
		}(i)
	}
	wg.Wait()
	close(success)
	if len(success) != 1 {
		t.Fatalf("conditional create admitted %d writers", len(success))
	}
}
