package systembackup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
)

const artifactChunkBytes = 4 << 20

type PartIndex struct {
	Name    string            `json:"name"`
	Objects []ObjectReference `json:"objects"`
}
type ArtifactIndex struct {
	Schema               string      `json:"schema"`
	BackupID             string      `json:"backup_id"`
	OriginInstallationID string      `json:"origin_installation_id"`
	ManifestSHA256       string      `json:"manifest_sha256"`
	Parts                []PartIndex `json:"parts"`
}
type SignedIndex struct {
	Payload   ArtifactIndex     `json:"payload"`
	Signature ArtifactSignature `json:"signature"`
}
type CommitPayload struct {
	Schema               string          `json:"schema"`
	BackupID             string          `json:"backup_id"`
	OriginInstallationID string          `json:"origin_installation_id"`
	Manifest             ObjectReference `json:"manifest"`
	Index                ObjectReference `json:"index"`
}
type SignedCommit struct {
	Payload   CommitPayload     `json:"payload"`
	Signature ArtifactSignature `json:"signature"`
}
type PublishedArtifact struct {
	Manifest        SignedManifest  `json:"manifest"`
	Index           SignedIndex     `json:"index"`
	Commit          SignedCommit    `json:"commit"`
	CommitReference ObjectReference `json:"commit_reference"`
}

func signObject(domain string, payload any, key ed25519.PrivateKey, version string) (ArtifactSignature, error) {
	return signPurpose(domain, "artifact", payload, key, version)
}
func verifyObject(domain string, payload any, s ArtifactSignature, key ed25519.PublicKey, version string) error {
	if s.Protected != nil {
		return verifyPurpose(domain, "artifact", payload, s, key, version)
	}
	if len(key) != ed25519.PublicKeySize || s.Algorithm != "Ed25519" || s.KeyVersion != version {
		return errors.New("signature_invalid")
	}
	raw, e := json.Marshal(payload)
	if e != nil {
		return e
	}
	if !ed25519.Verify(key, append([]byte(domain+"\n"), raw...), s.Value) {
		return errors.New("signature_invalid")
	}
	return nil
}
func artifactPrefix(origin, id string) string { return origin + "/" + id + "/" }

// PublishFunctionalArtifact exercises real immutable external publication but
// never writes _ACCEPTANCE or a recovery-catalog registration. Formal recovery
// eligibility additionally requires capture/verifier/cleanup/provider proofs.
func PublishFunctionalArtifact(ctx context.Context, store ImmutableObjectStore, stage string, manifest SignedManifest, key ed25519.PrivateKey, keyVersion string, maxBytes int64) (PublishedArtifact, error) {
	var out PublishedArtifact
	if manifest.Payload.Purpose != "functional_test" && manifest.Payload.Purpose != "team_data" {
		return out, errors.New("acceptance publication requires the independent finalizer")
	}
	if len(key) != ed25519.PrivateKeySize {
		return out, errors.New("signing_key_unavailable")
	}
	if e := VerifyManifest(manifest, key.Public().(ed25519.PublicKey), keyVersion); e != nil {
		return out, e
	}
	if e := store.ProbeRead(ctx); e != nil {
		return out, e
	}
	index, e := UploadCapturedParts(ctx, store, CaptureStageDirectories{Ordinary: stage, Secrets: stage}, manifest, key.Public().(ed25519.PublicKey), keyVersion, maxBytes)
	if e != nil {
		return out, e
	}
	prefix := artifactPrefix(manifest.Payload.OriginInstallationID, manifest.Payload.BackupID)
	manifestBytes, e := canonicalJSON(manifest)
	if e != nil {
		return out, e
	}
	manifestRef, e := store.PutImmutable(ctx, prefix+"manifest.json", manifestBytes)
	if e != nil {
		return out, e
	}
	if manifestRef.SHA256 != index.ManifestSHA256 {
		return out, errors.New("artifact_conflict")
	}
	signature, e := signObject("clawmanager:artifact-index:v1", index, key, keyVersion)
	if e != nil {
		return out, e
	}
	signedIndex := SignedIndex{index, signature}
	indexBytes, e := canonicalJSON(signedIndex)
	if e != nil {
		return out, e
	}
	indexRef, e := store.PutImmutable(ctx, prefix+"index.json", indexBytes)
	if e != nil {
		return out, e
	}
	commit := CommitPayload{Schema: "system-backup-commit.v1", BackupID: manifest.Payload.BackupID, OriginInstallationID: manifest.Payload.OriginInstallationID, Manifest: manifestRef, Index: indexRef}
	signature, e = signObject("clawmanager:artifact-commit:v1", commit, key, keyVersion)
	if e != nil {
		return out, e
	}
	signedCommit := SignedCommit{commit, signature}
	raw, e := canonicalJSON(signedCommit)
	if e != nil {
		return out, e
	}
	commitRef, e := store.PutImmutable(ctx, prefix+"_COMMITTED", raw)
	if e != nil {
		return out, e
	}
	return PublishedArtifact{manifest, signedIndex, signedCommit, commitRef}, nil
}

func strictDecode(raw []byte, out any) error {
	if e := uniqueJSONKeys(raw); e != nil {
		return e
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return errors.New("invalid artifact metadata")
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return errors.New("trailing artifact metadata")
	}
	return nil
}

func (s ImmutableObjectStore) readCurrent(ctx context.Context, key string) ([]byte, ObjectReference, error) {
	var ref ObjectReference
	full, e := s.key(key)
	if e != nil {
		return nil, ref, e
	}
	stat, e := s.Client.StatObject(ctx, s.Bucket, full, minio.StatObjectOptions{})
	if e != nil {
		return nil, ref, errors.New("artifact_unavailable")
	}
	if stat.Size <= 0 || stat.Size > MaxImmutableObjectBytes || stat.VersionID == "" || stat.VersionID == "null" {
		return nil, ref, errors.New("invalid artifact metadata version")
	}
	obj, e := s.Client.GetObject(ctx, s.Bucket, full, minio.GetObjectOptions{VersionID: stat.VersionID})
	if e != nil {
		return nil, ref, errors.New("artifact_unavailable")
	}
	defer obj.Close()
	raw, e := io.ReadAll(io.LimitReader(obj, stat.Size+1))
	if e != nil || int64(len(raw)) != stat.Size {
		return nil, ref, errors.New("artifact_unavailable")
	}
	h := sha256.Sum256(raw)
	return raw, ObjectReference{key, stat.VersionID, stat.Size, hex.EncodeToString(h[:])}, nil
}

// OpenPublishedArtifact needs no source database. Its only entry identity is
// origin + public backup ID; a signed commit pins exact immutable versions.
// This is exact discovery, not a claim that S3 LIST supplied a full snapshot.
func OpenPublishedArtifact(ctx context.Context, store ImmutableObjectStore, origin, id string, key ed25519.PublicKey, keyVersion string) (PublishedArtifact, error) {
	var out PublishedArtifact
	if !validPartIdentity(origin, id, "mysql") {
		return out, errors.New("invalid artifact identity")
	}
	prefix := artifactPrefix(origin, id)
	raw, ref, e := store.readCurrent(ctx, prefix+"_COMMITTED")
	if e != nil {
		return out, e
	}
	if e = strictDecode(raw, &out.Commit); e != nil {
		return out, e
	}
	if e = verifyObject("clawmanager:artifact-commit:v1", out.Commit.Payload, out.Commit.Signature, key, keyVersion); e != nil {
		return out, e
	}
	c := out.Commit.Payload
	if c.Schema != "system-backup-commit.v1" || c.BackupID != id || c.OriginInstallationID != origin || c.Manifest.Key != prefix+"manifest.json" || c.Index.Key != prefix+"index.json" {
		return out, errors.New("artifact_conflict")
	}
	out.CommitReference = ref
	raw, e = store.Read(ctx, c.Manifest)
	if e != nil {
		return out, e
	}
	if e = strictDecode(raw, &out.Manifest); e != nil {
		return out, e
	}
	if e = VerifyManifest(out.Manifest, key, keyVersion); e != nil {
		return out, e
	}
	if out.Manifest.Payload.BackupID != id || out.Manifest.Payload.OriginInstallationID != origin {
		return out, errors.New("artifact_conflict")
	}
	raw, e = store.Read(ctx, c.Index)
	if e != nil {
		return out, e
	}
	if e = strictDecode(raw, &out.Index); e != nil {
		return out, e
	}
	if e = verifyObject("clawmanager:artifact-index:v1", out.Index.Payload, out.Index.Signature, key, keyVersion); e != nil {
		return out, e
	}
	if err := ValidateArtifactIndex(out.Index.Payload, out.Manifest.Payload, c.Manifest.SHA256); err != nil {
		return out, err
	}

	return out, nil
}

type remotePartReader struct {
	ctx     context.Context
	store   ImmutableObjectStore
	objects []ObjectReference
	offset  int
	current *bytes.Reader
}

func (r *remotePartReader) Read(p []byte) (int, error) {
	for {
		if e := r.ctx.Err(); e != nil {
			return 0, e
		}
		if r.current != nil {
			n, e := r.current.Read(p)
			if n > 0 || e != io.EOF {
				return n, e
			}
			r.current = nil
		}
		if r.offset >= len(r.objects) {
			return 0, io.EOF
		}
		raw, e := r.store.Read(r.ctx, r.objects[r.offset])
		if e != nil {
			return 0, e
		}
		r.offset++
		r.current = bytes.NewReader(raw)
	}
}
func OpenPublishedPart(ctx context.Context, store ImmutableObjectStore, artifact PublishedArtifact, name string, wrapper DataKeyWrapper, w io.Writer, maxBytes int64) error {
	for _, part := range artifact.Manifest.Payload.Parts {
		if part.Name != name {
			continue
		}
		for _, index := range artifact.Index.Payload.Parts {
			if index.Name == name {
				return OpenPart(ctx, &remotePartReader{ctx: ctx, store: store, objects: index.Objects}, w, artifact.Manifest.Payload, part, wrapper, maxBytes)
			}
		}
	}
	return errors.New("required part unavailable")
}

func VerifyPublishedArtifact(ctx context.Context, store ImmutableObjectStore, artifact PublishedArtifact, wrapper DataKeyWrapper, maxBytes int64) error {
	var total int64
	for _, p := range artifact.Manifest.Payload.Parts {
		if p.CiphertextBytes > maxBytes-total {
			return errors.New("capacity_exceeded")
		}
		total += p.CiphertextBytes
		if e := OpenPublishedPart(ctx, store, artifact, p.Name, wrapper, io.Discard, maxBytes); e != nil {
			return e
		}
	}
	return nil
}

func reservedArtifactSegment(s string) bool { return s == "_COMMITTED" || s == "_ACCEPTANCE" }
func validObjectSegments(key string) bool {
	for _, segment := range strings.Split(key, "/") {
		if !refPattern.MatchString(segment) && !reservedArtifactSegment(segment) {
			return false
		}
	}
	return true
}
