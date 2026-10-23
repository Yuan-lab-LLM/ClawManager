package services

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImageArtifactAuthenticatedEncryption(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	id := []byte("artifact-a")
	plain := []byte("private image bytes")
	sealed, err := encryptImageArtifact(key, id, plain)
	if err != nil || bytes.Contains(sealed, plain) {
		t.Fatal("not encrypted", err)
	}
	got, err := decryptImageArtifact(key, id, sealed)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatal("round trip", err)
	}
	if _, err := decryptImageArtifact(key, []byte("artifact-b"), sealed); err == nil {
		t.Fatal("cross-object substitution accepted")
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := decryptImageArtifact(key, id, sealed); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
	if _, err := decryptImageArtifact(key, id, []byte{1}); err == nil {
		t.Fatal("truncated ciphertext accepted")
	}
}

func TestImageObjectStoreLimitsAndNarrowDeletion(t *testing.T) {
	s := &objectStorageService{localPath: t.TempDir(), basePath: "skills"}
	key := "gateway-images/img_" + strings.Repeat("a", 32)
	if err := s.PutObject(t.Context(), key, []byte("12345"), "application/octet-stream"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetObjectLimited(t.Context(), key, 4); err == nil {
		t.Fatal("unbounded read")
	}
	if got, err := s.GetObjectLimited(t.Context(), key, 5); err != nil || string(got) != "12345" {
		t.Fatal(got, err)
	}
	for _, bad := range []string{"", "../", "gateway-images/../skill", "skills/a", "gateway-images/img_bad", key + "/extra"} {
		if err := s.DeleteObject(t.Context(), bad); err == nil {
			t.Fatal("broad delete accepted", bad)
		}
		if _, err := s.GetObjectLimited(t.Context(), bad, 5); err == nil {
			t.Fatal("broad read accepted", bad)
		}
	}
	if err := s.DeleteObject(t.Context(), key); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteObject(t.Context(), key); err != nil {
		t.Fatal("delete not idempotent", err)
	}
	if _, err := os.Stat(filepath.Join(s.localPath, "skills", "gateway-images")); err != nil {
		t.Fatal("parent directory unexpectedly deleted", err)
	}
}
