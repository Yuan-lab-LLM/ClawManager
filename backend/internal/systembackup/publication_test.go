package systembackup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

func TestPublishedArtifactRecoveryWithoutSourceOrStaging(t *testing.T) {
	store := testObjectStore(t)
	ctx := context.Background()
	stage := t.TempDir()
	id, _ := RandomID("sbk_")
	kek := make([]byte, 32)
	rand.Read(kek)
	wrapper := AESKeyWrapper{SecretRef{"kubernetes", "archive-kek", "v1", "encryption"}, kek}
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	m := Manifest{Schema: "system-backup.v1", Contract: ContractVersion, BackupID: id, OriginInstallationID: "source-lost", Purpose: "functional_test", RegistryHash: RegistryHash(), CapturedAt: time.Now().UTC(), Consistency: "best_effort"}
	originals := map[string][]byte{}
	for i, name := range RequiredParts {
		original := bytes.Repeat([]byte{byte(i), 0xff, 0, 42}, 100+i)
		if name == "workspace" {
			original = bytes.Repeat([]byte("large-private-file\x00\xff"), 300000)
		}
		originals[name] = original
		p, e := CapturePart(ctx, stage, m.OriginInstallationID, id, name, 16<<20, wrapper, func(_ context.Context, w io.Writer) error { _, e := w.Write(original); return e })
		if e != nil {
			t.Fatal(e)
		}
		m.Parts = append(m.Parts, p)
	}
	signed, e := SignManifest(m, key, "v1")
	if e != nil {
		t.Fatal(e)
	}
	published, e := PublishFunctionalArtifact(ctx, store, stage, signed, key, "v1", 64<<20)
	if e != nil {
		t.Fatal(e)
	}
	replay, e := PublishFunctionalArtifact(ctx, store, stage, signed, key, "v1", 64<<20)
	if e != nil || replay.CommitReference != published.CommitReference {
		t.Fatal("publication retry changed immutable commit", e)
	}
	for _, name := range RequiredParts {
		if e = os.Remove(filepath.Join(stage, name+".enc")); e != nil {
			t.Fatal(e)
		}
	}
	t.Run("offline verifier executable", func(t *testing.T) {
		exe := os.Getenv("TEST_SYSTEM_BACKUP_VERIFY_EXE")
		if exe == "" {
			t.Skip("offline verifier executable not configured")
		}
		dir := t.TempDir()
		write := func(name string, raw []byte) string {
			p := filepath.Join(dir, name)
			if e := os.WriteFile(p, raw, 0600); e != nil {
				t.Fatal(e)
			}
			return p
		}
		credentialJSON, _ := json.Marshal(map[string]string{"access_key": os.Getenv("TEST_SYSTEM_BACKUP_MINIO_USER"), "secret_key": os.Getenv("TEST_SYSTEM_BACKUP_MINIO_PASSWORD")})
		cfg := map[string]any{"endpoint": "127.0.0.1:" + os.Getenv("TEST_SYSTEM_BACKUP_MINIO_PORT"), "bucket": store.Bucket, "root": store.Root, "secure": false, "credentials_file": write("credential.json", credentialJSON), "verification_key_file": write("verify.pub", pub), "verification_key_version": "v1", "encryption_key_file": write("wrap.key", kek), "encryption_reference": wrapper.Reference}
		raw, _ := json.Marshal(cfg)
		path := write("bootstrap.json", raw)
		cmd := exec.CommandContext(ctx, exe, "--bootstrap", path, "--origin", m.OriginInstallationID, "--backup", id, "--max-bytes", "67108864")
		output, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("offline verify failed: %v: %s", e, output)
		}
		var report struct {
			Integrity bool `json:"integrity_verified"`
			Eligible  bool `json:"acceptance_eligible"`
			Parts     int  `json:"parts_verified"`
		}
		if e = json.Unmarshal(output, &report); e != nil || !report.Integrity || report.Eligible || report.Parts != 6 {
			t.Fatal("offline verifier report invalid", e)
		}
	})
	recovered, e := OpenPublishedArtifact(ctx, store, m.OriginInstallationID, id, pub, "v1")
	if e != nil {
		t.Fatal(e)
	}
	if e = VerifyPublishedArtifact(ctx, store, recovered, wrapper, 64<<20); e != nil {
		t.Fatal(e)
	}
	for name, original := range originals {
		var got bytes.Buffer
		if e = OpenPublishedPart(ctx, store, recovered, name, wrapper, &got, 16<<20); e != nil || !bytes.Equal(got.Bytes(), original) {
			t.Fatal("data-only recovery lost part", name, e)
		}
	}
	if _, e = OpenPublishedArtifact(ctx, store, "other-source", id, pub, "v1"); e == nil {
		t.Fatal("origin substitution accepted")
	}
	wrongPub, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, e = OpenPublishedArtifact(ctx, store, m.OriginInstallationID, id, wrongPub, "v1"); e == nil {
		t.Fatal("wrong trust root accepted")
	}
	// An attacker can overwrite the current object; signed version pins retain
	// the original bytes. Removing that exact version must fail closed.
	chunk := recovered.Index.Payload.Parts[0].Objects[0]
	full, e := store.key(chunk.Key)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = store.Client.PutObject(ctx, store.Bucket, full, bytes.NewReader([]byte("tampered")), 8, minio.PutObjectOptions{}); e != nil {
		t.Fatal(e)
	}
	if e = VerifyPublishedArtifact(ctx, store, recovered, wrapper, 64<<20); e != nil {
		t.Fatal("latest-version substitution affected pinned data", e)
	}
	if e = store.Client.RemoveObject(ctx, store.Bucket, full, minio.RemoveObjectOptions{VersionID: chunk.VersionID}); e != nil {
		t.Fatal(e)
	}
	if e = VerifyPublishedArtifact(ctx, store, recovered, wrapper, 64<<20); e == nil {
		t.Fatal("missing pinned chunk accepted")
	}
	if published.Manifest.Payload.Purpose != "functional_test" {
		t.Fatal("functional capture acquired recovery eligibility")
	}
	acceptance := signed
	acceptance.Payload.Purpose = "acceptance"
	acceptance.Payload.Consistency = "strong"
	acceptance, e = SignManifest(acceptance.Payload, key, "v1")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = PublishFunctionalArtifact(ctx, store, stage, acceptance, key, "v1", 64<<20); e == nil {
		t.Fatal("bypassed independent acceptance finalizer")
	}
}
