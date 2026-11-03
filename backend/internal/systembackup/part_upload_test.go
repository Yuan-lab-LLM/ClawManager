package systembackup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type memoryPartUpload struct {
	objects map[string][]byte
	invalid bool
}

func (s *memoryPartUpload) PutImmutable(_ context.Context, key string, raw []byte) (ObjectReference, error) {
	s.objects[key] = bytes.Clone(raw)
	h := sha256.Sum256(raw)
	r := ObjectReference{Key: key, VersionID: "version-1", Bytes: int64(len(raw)), SHA256: hex.EncodeToString(h[:])}
	if s.invalid {
		r.VersionID = "null"
	}
	return r, nil
}

func TestUploadSealedPhasesHasNoSigningOrFinalWriteCapability(t *testing.T) {
	ctx := context.Background()
	dirs := CaptureStageDirectories{Ordinary: t.TempDir(), Secrets: t.TempDir()}
	id, _ := RandomID("sbk_")
	m := Manifest{Schema: "system-backup.v1", Contract: ContractVersion, BackupID: id, OriginInstallationID: "upload-source", Purpose: "acceptance", RegistryHash: RegistryHash(), CapturedAt: time.Now().UTC(), Consistency: "strong"}
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	wrapper := AESKeyWrapper{SecretRef{"kubernetes", "kek", "v1", "encryption"}, bytes.Repeat([]byte{7}, 32)}
	for _, name := range RequiredParts {
		dir := dirs.Ordinary
		if name == "secret_bundle" {
			dir = dirs.Secrets
		}
		p, err := CapturePart(ctx, dir, m.OriginInstallationID, id, name, 16<<20, wrapper, func(_ context.Context, w io.Writer) error {
			_, err := w.Write(bytes.Repeat([]byte("private task data"), 300000))
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		m.Parts = append(m.Parts, p)
	}
	signed, err := SignManifest(m, key, "v1")
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryPartUpload{objects: map[string][]byte{}}
	index, err := UploadCapturedParts(ctx, store, dirs, signed, pub, "v1", 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(index.Parts) != 6 || len(store.objects) != 12 {
		t.Fatal("missing chunks", len(index.Parts), len(store.objects))
	}
	for key, raw := range store.objects {
		if !strings.Contains(key, "/parts/") || bytes.Contains(raw, []byte("private task data")) {
			t.Fatal("uploader wrote final metadata or plaintext")
		}
	}
	changed := index
	changed.Parts = append([]PartIndex(nil), index.Parts...)
	changed.Parts[0].Objects = append([]ObjectReference(nil), index.Parts[0].Objects...)
	changed.Parts[0].Objects[0].Key = "other-source/parts/00000000.bin"
	if ValidateArtifactIndex(changed, m, index.ManifestSHA256) == nil {
		t.Fatal("index escaped frozen identity")
	}
	store.invalid = true
	if _, err = UploadCapturedParts(ctx, store, dirs, signed, pub, "v1", 64<<20); PublicError(err) != "evidence_incomplete" {
		t.Fatal("provider returned non-versioned chunk", err)
	}
	store.invalid = false
	store.objects = map[string][]byte{}
	file := filepath.Join(dirs.Secrets, "secret_bundle.enc")
	f, err := os.OpenFile(file, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteAt([]byte{42}, 64)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = UploadCapturedParts(ctx, store, dirs, signed, pub, "v1", 64<<20); PublicError(err) != "checksum_mismatch" || len(store.objects) != 0 {
		t.Fatal("corrupt sealed part dispatched writes", err, len(store.objects))
	}
}
