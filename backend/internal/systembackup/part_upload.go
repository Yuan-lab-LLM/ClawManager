package systembackup

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
)

// PartUploadStore deliberately exposes no manifest, marker, acceptance or
// catalog operation. Provider credentials must additionally restrict PUT to
// this backup's parts prefix; the uploader never needs a private signing key.
type PartUploadStore interface {
	PutImmutable(context.Context, string, []byte) (ObjectReference, error)
}

type CaptureStageDirectories struct {
	Ordinary string
	Secrets  string
}

// UploadCapturedParts reads the two sealed directories without modifying them.
// The signed manifest is frozen by the finalizer, not supplied by a collector.
// The returned index is unsigned and cannot make a backup recoverable by itself.
func UploadCapturedParts(ctx context.Context, store PartUploadStore, dirs CaptureStageDirectories, manifest SignedManifest, publicKey ed25519.PublicKey, keyVersion string, maxBytes int64) (ArtifactIndex, error) {
	var zero ArtifactIndex
	if store == nil || dirs.Ordinary == "" || dirs.Secrets == "" || dirs.Ordinary == dirs.Secrets && manifest.Payload.Purpose != "functional_test" && manifest.Payload.Purpose != "team_data" || maxBytes < 1<<20 || maxBytes > 900<<30 {
		return zero, errors.New("validation_failed")
	}
	if err := VerifyManifest(manifest, publicKey, keyVersion); err != nil {
		return zero, err
	}
	raw, err := canonicalJSON(manifest)
	if err != nil {
		return zero, err
	}
	hash := sha256.Sum256(raw)
	m := manifest.Payload
	index := ArtifactIndex{Schema: "system-backup-index.v1", BackupID: m.BackupID, OriginInstallationID: m.OriginInstallationID, ManifestSHA256: hex.EncodeToString(hash[:]), Parts: []PartIndex{}}
	ordinary, err := os.OpenRoot(dirs.Ordinary)
	if err != nil {
		return zero, errors.New("artifact_unavailable")
	}
	defer ordinary.Close()
	secrets, err := os.OpenRoot(dirs.Secrets)
	if err != nil {
		return zero, errors.New("artifact_unavailable")
	}
	defer secrets.Close()
	var total int64
	// Validate every encrypted file before dispatching any external write.
	for _, part := range m.Parts {
		if part.CiphertextBytes > maxBytes-total {
			return zero, errors.New("capacity_exceeded")
		}
		total += part.CiphertextBytes
		root := ordinary
		if part.Name == "secret_bundle" {
			root = secrets
		}
		f, err := openSealedPart(root, part)
		if err != nil {
			return zero, err
		}
		err = checkSealedPart(ctx, f, part)
		closeErr := f.Close()
		if err != nil {
			return zero, err
		}
		if closeErr != nil {
			return zero, closeErr
		}
	}
	for _, part := range m.Parts {
		root := ordinary
		if part.Name == "secret_bundle" {
			root = secrets
		}
		f, err := openSealedPart(root, part)
		if err != nil {
			return zero, err
		}
		p, err := uploadSealedPart(ctx, store, f, m, part)
		closeErr := f.Close()
		if err != nil {
			return zero, err
		}
		if closeErr != nil {
			return zero, closeErr
		}
		index.Parts = append(index.Parts, p)
	}
	return index, ValidateArtifactIndex(index, m, index.ManifestSHA256)
}

func openSealedPart(root *os.Root, part ArtifactPart) (*os.File, error) {
	name := part.Name + ".enc"
	info, err := root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() != part.CiphertextBytes {
		return nil, errors.New("checksum_mismatch")
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, errors.New("artifact_unavailable")
	}
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		f.Close()
		return nil, errors.New("checksum_mismatch")
	}
	return f, nil
}

func checkSealedPart(ctx context.Context, f *os.File, part ArtifactPart) error {
	h := sha256.New()
	n, err := io.Copy(h, contextReader{ctx, io.LimitReader(f, part.CiphertextBytes+1)})
	if err != nil {
		return err
	}
	if n != part.CiphertextBytes || hex.EncodeToString(h.Sum(nil)) != part.CiphertextSHA256 {
		return errors.New("checksum_mismatch")
	}
	return nil
}

func uploadSealedPart(ctx context.Context, store PartUploadStore, f *os.File, m Manifest, part ArtifactPart) (PartIndex, error) {
	p := PartIndex{Name: part.Name, Objects: []ObjectReference{}}
	h := sha256.New()
	var size int64
	buffer := make([]byte, artifactChunkBytes)
	for sequence := 0; ; sequence++ {
		if err := ctx.Err(); err != nil {
			return p, err
		}
		n, err := io.ReadFull(f, buffer)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return p, err
		}
		if n > 0 {
			size += int64(n)
			if size > part.CiphertextBytes {
				return p, errors.New("checksum_mismatch")
			}
			h.Write(buffer[:n])
			key := fmt.Sprintf("%sparts/%s/%08d.bin", artifactPrefix(m.OriginInstallationID, m.BackupID), part.Name, sequence)
			ref, putErr := store.PutImmutable(ctx, key, buffer[:n])
			if putErr != nil {
				return p, putErr
			}
			digest := sha256.Sum256(buffer[:n])
			if ref.Key != key || ref.VersionID == "" || ref.VersionID == "null" || ref.Bytes != int64(n) || ref.SHA256 != hex.EncodeToString(digest[:]) {
				return p, errors.New("evidence_incomplete")
			}
			p.Objects = append(p.Objects, ref)
		}
		if err != nil {
			break
		}
	}
	if size != part.CiphertextBytes || hex.EncodeToString(h.Sum(nil)) != part.CiphertextSHA256 {
		return p, errors.New("checksum_mismatch")
	}
	return p, nil
}

// ValidateArtifactIndex is shared by the finalizer and disaster-side reader.
// It binds every immutable version to the signed inventory and exact chunk key.
func ValidateArtifactIndex(index ArtifactIndex, manifest Manifest, manifestHash string) error {
	if manifest.Validate() != nil || !validHash(manifestHash) || index.Schema != "system-backup-index.v1" || index.BackupID != manifest.BackupID || index.OriginInstallationID != manifest.OriginInstallationID || index.ManifestSHA256 != manifestHash || len(index.Parts) != len(manifest.Parts) {
		return errors.New("artifact_conflict")
	}
	seen := map[string]bool{}
	for _, part := range index.Parts {
		if seen[part.Name] || len(part.Objects) == 0 {
			return errors.New("artifact_conflict")
		}
		seen[part.Name] = true
		expected := int64(-1)
		for _, p := range manifest.Parts {
			if p.Name == part.Name {
				expected = p.CiphertextBytes
			}
		}
		var size int64
		for i, o := range part.Objects {
			if o.Key != fmt.Sprintf("%sparts/%s/%08d.bin", artifactPrefix(manifest.OriginInstallationID, manifest.BackupID), part.Name, i) || o.Bytes <= 0 || o.Bytes > artifactChunkBytes || o.Bytes > expected-size || o.VersionID == "" || o.VersionID == "null" || !validHash(o.SHA256) {
				return errors.New("artifact_conflict")
			}
			size += o.Bytes
		}
		if size != expected {
			return errors.New("artifact_conflict")
		}
	}
	return nil
}
