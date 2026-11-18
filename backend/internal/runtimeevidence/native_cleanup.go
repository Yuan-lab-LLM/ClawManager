// Package runtimeevidence verifies pinned historical evidence. Verification is
// not permission to release a writer, finish a task or activate a generation.
package runtimeevidence

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"regexp"
	"strings"
)

var ErrUnverified = errors.New("runtime cleanup evidence could not be independently verified")
var claimPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var ownerPattern = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)

// Binding must come from authoritative server placement/operation state, never
// directly from a manager report or an HTTP request body.
type Binding struct {
	InstanceID, OwnerID, Generation    int
	Operation, PodUID, PVCUID, ClaimID string
	CanonicalBaseURL                   string
}

func (b Binding) Valid() bool {
	operation, err := hex.DecodeString(b.Operation)
	if b.InstanceID <= 0 || b.OwnerID <= 0 || b.Generation < 0 || int64(b.Generation) > 2147483647 || err != nil || len(operation) != 16 || hex.EncodeToString(operation) != b.Operation || !claimPattern.MatchString(b.ClaimID) || !validIdentity(b.PodUID) || !validIdentity(b.PVCUID) {
		return false
	}
	u, err := url.Parse(b.CanonicalBaseURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.HasSuffix(b.CanonicalBaseURL, "/") || u.String() != b.CanonicalBaseURL || u.Host != strings.ToLower(u.Host) {
		return false
	}
	if u.Scheme == "https" && u.Port() == "443" || u.Scheme == "http" && u.Port() == "80" {
		return false
	}
	return ascii(b.CanonicalBaseURL)
}
func validIdentity(v string) bool { return len(v) <= 128 && ascii(v) }
func ascii(v string) bool {
	if v == "" {
		return false
	}
	for _, c := range []byte(v) {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}

type ReceiptBundle struct {
	Binding                Binding
	OwnerJSON, ReceiptJSON []byte
}

// EvidenceSource must read the pinned, protected receipt storage, not repeat a
// manager's boolean observation. No production storage adapter is implied.
type EvidenceSource interface {
	ReadPinned(context.Context, Binding) (ReceiptBundle, error)
}

// OriginalKeySource resolves the original scoped key, including its historical
// version. It must not fall back to a new key or an unrelated owner's key.
type OriginalKeySource interface {
	ResolveOriginal(context.Context, Binding) ([]byte, error)
}
type CleanupVerifier struct {
	Evidence EvidenceSource
	Keys     OriginalKeySource
}
type CleanupResult struct {
	NativeCleanupVerified bool   `json:"native_cleanup_verified"`
	WriterReleaseVerified bool   `json:"writer_release_verified"`
	LifecycleAuthorized   bool   `json:"lifecycle_authorized"`
	ReceiptHash           string `json:"receipt_hash"`
}
type ownerRecord struct {
	Version    int    `json:"version"`
	Generation int    `json:"generation"`
	Owner      string `json:"owner"`
}
type cleanupRecord struct {
	Version                int    `json:"version"`
	Namespace              string `json:"namespace"`
	Generation             int    `json:"generation"`
	ClaimID                string `json:"claim_id"`
	NativeCleanupConfirmed bool   `json:"native_cleanup_confirmed"`
}
type cleanupEnvelope struct {
	Record cleanupRecord `json:"record"`
	MAC    string        `json:"mac"`
}

func wire(v any) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}
func digest(v []byte) string { h := sha256.Sum256(v); return hex.EncodeToString(h[:]) }
func strict(raw []byte, value any) bool {
	if len(raw) == 0 || len(raw) > 2048 {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(value) == nil && d.Decode(new(any)) == io.EOF && bytes.Equal(raw, wire(value))
}

func (v CleanupVerifier) Verify(ctx context.Context, binding Binding) (*CleanupResult, error) {
	if !binding.Valid() || v.Evidence == nil || v.Keys == nil || ctx.Err() != nil {
		return nil, ErrUnverified
	}
	bundle, err := v.Evidence.ReadPinned(ctx, binding)
	if err != nil || bundle.Binding != binding || ctx.Err() != nil {
		return nil, ErrUnverified
	}
	key, err := v.Keys.ResolveOriginal(ctx, binding)
	if err != nil || len(key) > 4096 || !ascii(string(key)) || ctx.Err() != nil {
		return nil, ErrUnverified
	}
	var owner ownerRecord
	var receipt cleanupEnvelope
	if !strict(bundle.OwnerJSON, &owner) || owner.Version != 1 || owner.Generation != binding.Generation || !ownerPattern.MatchString(owner.Owner) || digest(bundle.OwnerJSON) != binding.ClaimID || !strict(bundle.ReceiptJSON, &receipt) {
		return nil, ErrUnverified
	}
	namespace := digest(wire([]any{binding.CanonicalBaseURL, string(key), binding.Generation}))
	expected := cleanupRecord{1, namespace, binding.Generation, binding.ClaimID, true}
	if receipt.Record != expected || !claimPattern.MatchString(receipt.MAC) {
		return nil, ErrUnverified
	}
	m := hmac.New(sha256.New, key)
	_, _ = m.Write(wire([]any{"native-cleanup-v1", namespace, binding.Generation, binding.ClaimID, true}))
	actual, _ := hex.DecodeString(receipt.MAC)
	if !hmac.Equal(actual, m.Sum(nil)) || ctx.Err() != nil {
		return nil, ErrUnverified
	}
	// The native receipt attests cleanup only. It says nothing about an old
	// Pod, workspace writer, volume attachment, release or current ownership.
	return &CleanupResult{NativeCleanupVerified: true, ReceiptHash: digest(bundle.ReceiptJSON)}, nil
}
