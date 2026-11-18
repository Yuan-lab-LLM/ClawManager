package systembackup

import (
	"bytes"
	"context"
	"github.com/minio/minio-go/v7"
	"io"
	"testing"
)

func TestTeamDataObjectsCaptureUnversionedSourceWithoutChangingBucket(t *testing.T) {
	store := testObjectStore(t)
	c := store.Client
	ctx := context.Background()
	source, _ := RandomID("teamdata-")
	target, _ := RandomID("cm-restore-")
	for _, bucket := range []string{source, target} {
		if e := c.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() {
			for o := range c.ListObjects(context.Background(), bucket, minio.ListObjectsOptions{Recursive: true}) {
				_ = c.RemoveObject(context.Background(), bucket, o.Key, minio.RemoveObjectOptions{})
			}
			_ = c.RemoveBucket(context.Background(), bucket)
		})
	}
	payload := []byte("partial artifact\x00\xff")
	if _, e := c.PutObject(ctx, source, "team/partial.bin", bytes.NewReader(payload), int64(len(payload)), minio.PutObjectOptions{ContentType: "application/octet-stream"}); e != nil {
		t.Fatal(e)
	}
	var archive bytes.Buffer
	b := ObjectBudget{1 << 20, 100}
	if e := CaptureTeamDataObjects(ctx, c, &archive, []string{source}, b); e != nil {
		t.Fatal("online Team capture rejected ordinary source bucket", e)
	}
	if e := RestoreObjects(ctx, c, bytes.NewReader(archive.Bytes()), b, map[string]string{source: target}); e != nil {
		t.Fatal(e)
	}
	if count, e := VerifyObjects(ctx, c, bytes.NewReader(archive.Bytes()), b, map[string]string{source: target}); e != nil || count != 1 {
		t.Fatal("unversioned artifact not preserved", count, e)
	}
	v, e := c.GetBucketVersioning(ctx, source)
	if e != nil || v.Status != "" {
		t.Fatal("source bucket changed", e, v.Status)
	}
	if e := CaptureObjects(ctx, c, io.Discard, []string{source}, b); e == nil {
		t.Fatal("full-system version requirement was widened")
	}
}

func TestRealObjectCaptureRestorePreservesPayloadMetadataAndVersions(t *testing.T) {
	store := testObjectStore(t)
	ctx := context.Background()
	client := store.Client
	sources := map[string][]byte{"team/private/.hidden": {0, 1, 255, 0}, "team/report.txt": bytes.Repeat([]byte("产物\n"), 200000), "empty": {}}
	for key, data := range sources {
		if _, e := client.PutObject(ctx, store.Bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: "application/octet-stream", UserMetadata: map[string]string{"owner": "owner-x"}}); e != nil {
			t.Fatal(e)
		}
	}
	var archive bytes.Buffer
	budget := ObjectBudget{32 << 20, 100}
	if e := CaptureObjects(ctx, client, &archive, []string{store.Bucket}, budget); e != nil {
		t.Fatal(e)
	}
	if e := ReadObjectSnapshot(ctx, bytes.NewReader(archive.Bytes()), budget, nil); e != nil {
		t.Fatal(e)
	}
	target, _ := RandomID("cm-restore-")
	if e := client.MakeBucket(ctx, target, minio.MakeBucketOptions{}); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		for o := range client.ListObjects(ctx, target, minio.ListObjectsOptions{Recursive: true}) {
			if o.Err != nil {
				t.Error(o.Err)
				continue
			}
			if e := client.RemoveObject(ctx, target, o.Key, minio.RemoveObjectOptions{}); e != nil {
				t.Error(e)
			}
		}
		if e := client.RemoveBucket(ctx, target); e != nil {
			t.Error(e)
		}
	})
	if e := RestoreObjects(ctx, client, bytes.NewReader(archive.Bytes()), budget, map[string]string{store.Bucket: store.Bucket}); e == nil {
		t.Fatal("source overwrite accepted")
	}
	if e := RestoreObjects(ctx, client, bytes.NewReader(archive.Bytes()), budget, map[string]string{store.Bucket: target}); e != nil {
		t.Fatal(e)
	}
	for key, want := range sources {
		o, e := client.GetObject(ctx, target, key, minio.GetObjectOptions{})
		if e != nil {
			t.Fatal(e)
		}
		got, e := io.ReadAll(o)
		o.Close()
		if e != nil || !bytes.Equal(want, got) {
			t.Fatal("object data lost", key, e)
		}
		stat, e := client.StatObject(ctx, target, key, minio.StatObjectOptions{})
		if e != nil || stat.UserMetadata["Owner"] != "owner-x" || stat.ContentType != "application/octet-stream" {
			t.Fatal("object metadata lost", stat.UserMetadata, e)
		}
	}
	if count, e := VerifyObjects(ctx, client, bytes.NewReader(archive.Bytes()), budget, map[string]string{store.Bucket: target}); e != nil || count != len(sources) {
		t.Fatal("object readback failed", count, e)
	}
	if _, e := client.PutObject(ctx, target, "unexpected-object", bytes.NewReader([]byte("extra")), 5, minio.PutObjectOptions{}); e != nil {
		t.Fatal(e)
	}
	if _, e := VerifyObjects(ctx, client, bytes.NewReader(archive.Bytes()), budget, map[string]string{store.Bucket: target}); e == nil {
		t.Fatal("unexpected object accepted")
	}
	if e := RestoreObjects(ctx, client, bytes.NewReader(archive.Bytes()), budget, map[string]string{store.Bucket: target}); e == nil {
		t.Fatal("populated target overwritten")
	}
	if e := ReadObjectSnapshot(ctx, bytes.NewReader(archive.Bytes()[:archive.Len()-20]), budget, nil); e == nil {
		t.Fatal("truncated object archive accepted")
	}
}
