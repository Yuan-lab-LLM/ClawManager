package systembackup

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"time"
)

var RequiredParts = []string{"mysql", "redis", "object_storage", "workspace", "deployment_resources", "secret_bundle"}

type WrappedKey struct {
	Reference  SecretRef `json:"reference"`
	Nonce      []byte    `json:"nonce"`
	Ciphertext []byte    `json:"ciphertext"`
}
type DataKeyWrapper interface {
	Wrap(context.Context, []byte, []byte) (WrappedKey, error)
	Unwrap(context.Context, WrappedKey, []byte) ([]byte, error)
}

// AESKeyWrapper is the offline/bootstrap provider. The KEK is resolved from a
// versioned external secret by the runner, never serialized into the artifact.
// KMS providers implement the same interface without exposing their KEK.
type AESKeyWrapper struct {
	Reference SecretRef
	Key       []byte
}

func (w AESKeyWrapper) Wrap(ctx context.Context, key, identity []byte) (WrappedKey, error) {
	if e := ctx.Err(); e != nil {
		return WrappedKey{}, e
	}
	if e := w.Reference.Validate("encryption"); e != nil {
		return WrappedKey{}, e
	}
	if len(w.Key) != 32 || len(key) != 32 {
		return WrappedKey{}, errors.New("invalid wrapping key")
	}
	block, e := aes.NewCipher(w.Key)
	if e != nil {
		return WrappedKey{}, e
	}
	a, e := cipher.NewGCM(block)
	if e != nil {
		return WrappedKey{}, e
	}
	nonce := make([]byte, a.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return WrappedKey{}, e
	}
	return WrappedKey{w.Reference, nonce, a.Seal(nil, nonce, key, append([]byte("cm-backup-dek.v1\x00"), identity...))}, nil
}
func (w AESKeyWrapper) Unwrap(ctx context.Context, k WrappedKey, identity []byte) ([]byte, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if k.Reference != w.Reference || len(w.Key) != 32 {
		return nil, errors.New("encryption_key_unavailable")
	}
	block, e := aes.NewCipher(w.Key)
	if e != nil {
		return nil, e
	}
	a, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	if len(k.Nonce) != a.NonceSize() {
		return nil, errors.New("invalid wrapped key")
	}
	key, e := a.Open(nil, k.Nonce, k.Ciphertext, append([]byte("cm-backup-dek.v1\x00"), identity...))
	if e != nil || len(key) != 32 {
		clear(key)
		return nil, errors.New("wrapped key authentication failed")
	}
	return key, nil
}

type ArtifactPart struct {
	Name             string     `json:"name"`
	CiphertextBytes  int64      `json:"ciphertext_bytes"`
	CiphertextSHA256 string     `json:"ciphertext_sha256"`
	PlaintextBytes   int64      `json:"plaintext_bytes"`
	PlaintextSHA256  string     `json:"plaintext_sha256"`
	Key              WrappedKey `json:"key"`
}
type Manifest struct {
	Schema               string         `json:"schema"`
	Contract             string         `json:"contract"`
	BackupID             string         `json:"backup_id"`
	OriginInstallationID string         `json:"origin_installation_id"`
	Purpose              string         `json:"purpose"`
	RegistryHash         string         `json:"registry_hash"`
	CapturedAt           time.Time      `json:"captured_at"`
	Consistency          string         `json:"consistency"`
	Parts                []ArtifactPart `json:"parts"`
	ExcludedSQLTables    []string       `json:"excluded_sql_tables,omitempty"`
}
type ArtifactSignature struct {
	Algorithm  string           `json:"algorithm"`
	KeyVersion string           `json:"key_version"`
	Value      []byte           `json:"value"`
	Protected  *SignatureHeader `json:"protected,omitempty"`
}
type SignedManifest struct {
	Payload   Manifest          `json:"payload"`
	Signature ArtifactSignature `json:"signature"`
}

func partIdentity(origin, id, name string) []byte {
	return []byte("system-backup.v1\x00" + origin + "\x00" + id + "\x00" + name)
}
func validPartIdentity(origin, id, name string) bool {
	return refPattern.MatchString(origin) && publicIDPattern.MatchString(id) && len(id) > 4 && id[:4] == "sbk_" && slices.Contains(RequiredParts, name)
}
func validHash(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == sha256.Size && hex.EncodeToString(b) == s
}

func (m Manifest) Validate() error {
	if m.Schema != "system-backup.v1" || m.Contract != ContractVersion || !refPattern.MatchString(m.OriginInstallationID) || !validHash(m.RegistryHash) || m.CapturedAt.IsZero() || (m.Purpose != "acceptance" && m.Purpose != "functional_test" && m.Purpose != "team_data") || (m.Consistency != "strong" && m.Consistency != "best_effort") {
		return errors.New("invalid manifest identity")
	}
	if m.Purpose == "acceptance" && m.Consistency != "strong" {
		return errors.New("acceptance requires strong consistency")
	}
	if m.Purpose == "team_data" && m.Consistency != "best_effort" {
		return errors.New("online Team data must declare its capture consistency")
	}
	if m.Purpose == "team_data" && !slices.Equal(m.ExcludedSQLTables, teamDataExcludedTables()) {
		return errors.New("Team data scope declaration missing")
	}
	if len(m.Parts) != len(RequiredParts) {
		return errors.New("required part missing")
	}
	seen := map[string]bool{}
	for _, p := range m.Parts {
		if !validPartIdentity(m.OriginInstallationID, m.BackupID, p.Name) || seen[p.Name] || p.CiphertextBytes <= 0 || p.PlaintextBytes < 0 || !validHash(p.CiphertextSHA256) || !validHash(p.PlaintextSHA256) || p.Key.Reference.Validate("encryption") != nil || len(p.Key.Ciphertext) == 0 {
			return errors.New("invalid manifest part")
		}
		seen[p.Name] = true
	}
	return nil
}

func manifestSigningBytes(m Manifest) ([]byte, error) {
	if e := m.Validate(); e != nil {
		return nil, e
	}
	raw, e := json.Marshal(m)
	return append([]byte("clawmanager:manifest:system-backup.v1\n"), raw...), e
}
func SignManifest(m Manifest, key ed25519.PrivateKey, keyVersion string) (SignedManifest, error) {
	if len(key) != ed25519.PrivateKeySize || !refPattern.MatchString(keyVersion) {
		return SignedManifest{}, errors.New("signing_key_unavailable")
	}
	if e := m.Validate(); e != nil {
		return SignedManifest{}, e
	}
	s, e := signPurpose("clawmanager:manifest:system-backup.v1", "artifact", m, key, keyVersion)
	return SignedManifest{m, s}, e
}
func VerifyManifest(s SignedManifest, key ed25519.PublicKey, keyVersion string) error {
	if e := s.Payload.Validate(); e != nil {
		return e
	}
	if s.Signature.Protected != nil {
		return verifyPurpose("clawmanager:manifest:system-backup.v1", "artifact", s.Payload, s.Signature, key, keyVersion)
	}
	if s.Payload.Purpose != "functional_test" {
		return errors.New("signature_invalid")
	}
	if len(key) != ed25519.PublicKeySize || s.Signature.Algorithm != "Ed25519" || s.Signature.KeyVersion != keyVersion {
		return errors.New("signature_invalid")
	}
	raw, e := manifestSigningBytes(s.Payload)
	if e != nil {
		return e
	}
	if !ed25519.Verify(key, raw, s.Signature.Value) {
		return errors.New("signature_invalid")
	}
	return nil
}

type limitedHashWriter struct {
	writer io.Writer
	count  int64
	max    int64
}

func (w *limitedHashWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.max-w.count {
		return 0, errors.New("capacity_exceeded")
	}
	n, e := w.writer.Write(p)
	w.count += int64(n)
	return n, e
}

// CapturePart streams directly through encryption into an exclusively-created
// staging file. Neither plaintext nor the unwrapped DEK is persisted. stage is
// a task-owned empty directory from resource inventory, not an API path.
func CapturePart(ctx context.Context, stage, origin, id, name string, maxBytes int64, wrapper DataKeyWrapper, capture func(context.Context, io.Writer) error) (ArtifactPart, error) {
	var out ArtifactPart
	if !validPartIdentity(origin, id, name) || maxBytes < 1<<20 || wrapper == nil || capture == nil {
		return out, errors.New("invalid capture request")
	}
	root, e := os.OpenRoot(stage)
	if e != nil {
		return out, e
	}
	defer root.Close()
	f, e := root.OpenFile(name+".enc", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return out, e
	}
	sealed := false
	defer func() {
		f.Close()
		if !sealed {
			_ = root.Remove(name + ".enc")
		}
	}()
	key := make([]byte, 32)
	if _, e = rand.Read(key); e != nil {
		return out, e
	}
	defer clear(key)
	identity := partIdentity(origin, id, name)
	wrapped, e := wrapper.Wrap(ctx, key, identity)
	if e != nil {
		return out, e
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	pipeR, pipeW := io.Pipe()
	defer pipeR.Close()
	plainHash := sha256.New()
	plain := &limitedHashWriter{writer: io.MultiWriter(pipeW, plainHash), max: maxBytes}
	done := make(chan error, 1)
	go func() { err := capture(ctx, plain); _ = pipeW.CloseWithError(err); done <- err }()
	cipherHash := sha256.New()
	encrypted := &limitedHashWriter{writer: io.MultiWriter(f, cipherHash), max: maxBytes}
	e = Encrypt(ctx, encrypted, pipeR, key, identity)
	if e != nil {
		cancel()
		_ = pipeR.CloseWithError(e)
	}
	captureError := <-done
	if e != nil {
		return out, e
	}
	if captureError != nil {
		return out, captureError
	}
	if e = f.Sync(); e != nil {
		return out, e
	}
	if e = f.Close(); e != nil {
		return out, e
	}
	out = ArtifactPart{name, encrypted.count, hex.EncodeToString(cipherHash.Sum(nil)), plain.count, hex.EncodeToString(plainHash.Sum(nil)), wrapped}
	sealed = true
	return out, nil
}

// OpenPart authenticates bytes while streaming to the isolated restore target.
// Callers must not expose that target until the whole part, EOF and hashes pass.
func OpenPart(ctx context.Context, r io.Reader, w io.Writer, m Manifest, p ArtifactPart, wrapper DataKeyWrapper, maxBytes int64) error {
	if !validPartIdentity(m.OriginInstallationID, m.BackupID, p.Name) || p.CiphertextBytes > maxBytes || p.PlaintextBytes > maxBytes || maxBytes <= 0 || p.CiphertextBytes <= 0 || p.PlaintextBytes < 0 || wrapper == nil {
		return errors.New("invalid artifact part")
	}
	identity := partIdentity(m.OriginInstallationID, m.BackupID, p.Name)
	key, e := wrapper.Unwrap(ctx, p.Key, identity)
	if e != nil {
		return e
	}
	defer clear(key)
	ciphertextHash := sha256.New()
	plainHash := sha256.New()
	limited := &io.LimitedReader{R: r, N: p.CiphertextBytes + 1}
	counted := &limitedHashWriter{writer: io.MultiWriter(w, plainHash), max: p.PlaintextBytes}
	if e = Decrypt(ctx, counted, io.TeeReader(limited, ciphertextHash), key, identity); e != nil {
		return e
	}
	if p.CiphertextBytes+1-limited.N != p.CiphertextBytes || counted.count != p.PlaintextBytes || hex.EncodeToString(ciphertextHash.Sum(nil)) != p.CiphertextSHA256 || hex.EncodeToString(plainHash.Sum(nil)) != p.PlaintextSHA256 {
		return errors.New("checksum_mismatch")
	}
	return nil
}

func DecodeManifest(r io.Reader, max int64) (SignedManifest, error) {
	var out SignedManifest
	if max < 1 || max > 16<<20 {
		return out, errors.New("invalid manifest budget")
	}
	raw, e := io.ReadAll(io.LimitReader(r, max+1))
	if e != nil {
		return out, e
	}
	if int64(len(raw)) > max {
		return out, errors.New("manifest exceeds budget")
	}
	if e = strictDecode(raw, &out); e != nil {
		return out, errors.New("invalid manifest")
	}
	return out, out.Payload.Validate()
}

func VerifyArtifactDirectory(ctx context.Context, dir string, s SignedManifest, key ed25519.PublicKey, keyVersion string, wrapper DataKeyWrapper, maxBytes int64) error {
	if e := VerifyManifest(s, key, keyVersion); e != nil {
		return e
	}
	root, e := os.OpenRoot(dir)
	if e != nil {
		return e
	}
	defer root.Close()
	var total int64
	for _, p := range s.Payload.Parts {
		if p.CiphertextBytes > maxBytes-total {
			return errors.New("capacity_exceeded")
		}
		total += p.CiphertextBytes
		f, e := root.Open(p.Name + ".enc")
		if e != nil {
			return errors.New("artifact_unavailable")
		}
		e = OpenPart(ctx, f, io.Discard, s.Payload, p, wrapper, maxBytes)
		closeErr := f.Close()
		if e != nil {
			return fmt.Errorf("part %s: %w", p.Name, e)
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
