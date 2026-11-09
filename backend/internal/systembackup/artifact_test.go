package systembackup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEncryptedSixPartArtifactIsIndependentlyRecoverable(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	id, _ := RandomID("sbk_")
	kek := make([]byte, 32)
	rand.Read(kek)
	wrapper := AESKeyWrapper{SecretRef{"kubernetes", "backup-kek", "v7", "encryption"}, kek}
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	manifest := Manifest{Schema: "system-backup.v1", Contract: ContractVersion, BackupID: id, OriginInstallationID: "installation-a", Purpose: "functional_test", RegistryHash: RegistryHash(), CapturedAt: time.Now().UTC(), Consistency: "best_effort"}
	source := bytes.Repeat([]byte("private task partial result\x00\xff\n"), 10000)
	for _, name := range RequiredParts {
		part, e := CapturePart(ctx, dir, manifest.OriginInstallationID, id, name, 4<<20, wrapper, func(_ context.Context, w io.Writer) error { _, e := w.Write(source); return e })
		if e != nil {
			t.Fatal(e)
		}
		manifest.Parts = append(manifest.Parts, part)
		disk, e := os.ReadFile(filepath.Join(dir, name+".enc"))
		if e != nil {
			t.Fatal(e)
		}
		if bytes.Contains(disk, []byte("private task partial result")) {
			t.Fatal("plaintext staging")
		}
	}
	signed, e := SignManifest(manifest, key, "sign-v3")
	if e != nil {
		t.Fatal(e)
	}
	legacyBytes, e := manifestSigningBytes(manifest)
	if e != nil {
		t.Fatal(e)
	}
	legacy := SignedManifest{Payload: manifest, Signature: ArtifactSignature{Algorithm: "Ed25519", KeyVersion: "sign-v3", Value: ed25519.Sign(key, legacyBytes)}}
	if e = VerifyManifest(legacy, pub, "sign-v3"); e != nil {
		t.Fatal("legacy functional artifact became unreadable", e)
	}
	raw, e := json.Marshal(signed)
	if e != nil {
		t.Fatal(e)
	}
	decoded, e := DecodeManifest(bytes.NewReader(raw), 1<<20)
	if e != nil {
		t.Fatal(e)
	}
	if e = VerifyArtifactDirectory(ctx, dir, decoded, pub, "sign-v3", wrapper, 32<<20); e != nil {
		t.Fatal(e)
	}
	for _, p := range decoded.Payload.Parts {
		f, e := os.Open(filepath.Join(dir, p.Name+".enc"))
		if e != nil {
			t.Fatal(e)
		}
		var restored bytes.Buffer
		e = OpenPart(ctx, f, &restored, decoded.Payload, p, wrapper, 4<<20)
		f.Close()
		if e != nil || !bytes.Equal(restored.Bytes(), source) {
			t.Fatal("part lost data", p.Name, e)
		}
	}
	decoded.Payload.OriginInstallationID = "another-installation"
	if e = VerifyManifest(decoded, pub, "sign-v3"); e == nil {
		t.Fatal("manifest identity substitution")
	}
	decoded = signed
	decoded.Payload.Parts = decoded.Payload.Parts[:5]
	if e = VerifyManifest(decoded, pub, "sign-v3"); e == nil {
		t.Fatal("missing required part")
	}
	f, e := os.OpenFile(filepath.Join(dir, "workspace.enc"), os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	f.WriteAt([]byte{42}, 100)
	f.Close()
	if e = VerifyArtifactDirectory(ctx, dir, signed, pub, "sign-v3", wrapper, 32<<20); e == nil {
		t.Fatal("ciphertext corruption undetected")
	}
}

func TestCaptureFailureDoesNotPublishOrOverwritePart(t *testing.T) {
	dir := t.TempDir()
	id, _ := RandomID("sbk_")
	wrapper := AESKeyWrapper{SecretRef{"kubernetes", "kek", "v1", "encryption"}, make([]byte, 32)}
	_, e := CapturePart(context.Background(), dir, "source", id, "mysql", 1<<20, wrapper, func(_ context.Context, w io.Writer) error {
		w.Write([]byte("partial"))
		return errors.New("source stopped")
	})
	if e == nil {
		t.Fatal("source failure ignored")
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 0 {
		t.Fatal("unsealed partial retained without owner")
	}
	if e = os.WriteFile(filepath.Join(dir, "mysql.enc"), []byte("existing"), 0600); e != nil {
		t.Fatal(e)
	}
	_, e = CapturePart(context.Background(), dir, "source", id, "mysql", 1<<20, wrapper, func(_ context.Context, w io.Writer) error { return nil })
	if e == nil {
		t.Fatal("overwrote old ciphertext")
	}
	got, _ := os.ReadFile(filepath.Join(dir, "mysql.enc"))
	if string(got) != "existing" {
		t.Fatal("existing part changed")
	}
}
