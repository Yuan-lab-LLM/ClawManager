package services

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"clawreef/internal/config"
	"clawreef/internal/observability"
)

func TestObservabilityUsesEncryptedRealObjectStorageAndOutageRecovery(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "objects")
	store, err := NewObjectStorageService(config.ObjectStorageConfig{LocalFallback: directory})
	if err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{6}, 32)
	encrypted, err := observability.NewEncryptedObjects(store, key)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	plain := bytes.Repeat([]byte("private detail "), 1000)
	ref, err := encrypted.Put(ctx, "user:1", plain)
	if err != nil {
		t.Fatal(err)
	}
	disk, err := os.ReadFile(filepath.Join(directory, filepath.FromSlash(ref.ObjectKey)))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(disk, []byte("private detail")) {
		t.Fatal("plaintext object persisted")
	}
	if err = os.Rename(directory, directory+"-offline"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(directory, []byte("storage outage"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = encrypted.Put(ctx, "user:1", []byte("new body during outage")); err == nil {
		t.Fatal("object outage falsely accepted")
	}
	if err = os.Remove(directory); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(directory+"-offline", directory); err != nil {
		t.Fatal(err)
	}
	restarted, _ := observability.NewEncryptedObjects(store, key)
	got, err := restarted.Get(ctx, "user:1", ref)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("real object recovery: %v", err)
	}
	if err = restarted.Delete(ctx, "user:2", ref); err == nil {
		t.Fatal("cross-owner deletion")
	}
	if err = restarted.Delete(ctx, "user:1", ref); err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.Get(ctx, "user:1", ref); err == nil {
		t.Fatal("object retained after explicit scoped delete")
	}
}
