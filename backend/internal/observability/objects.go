// Package observability owns encrypted content references and bounded durable delivery.
package observability

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

type ObjectStore interface {
	PutObject(context.Context, string, []byte, string) error
	GetObject(context.Context, string) ([]byte, error)
}
type BodyRef struct {
	ObjectKey         string `json:"object_key"`
	Owner             string `json:"owner"`
	SHA256            string `json:"sha256"`
	ByteLength        int64  `json:"byte_length"`
	EncryptionVersion int    `json:"encryption_version"`
}
type EncryptedObjects struct {
	store  ObjectStore
	master []byte
}

func NewEncryptedObjects(store ObjectStore, masterKey []byte) (*EncryptedObjects, error) {
	if store == nil || len(masterKey) != 32 {
		return nil, errors.New("encrypted objects require storage and a stable 32-byte master key")
	}
	return &EncryptedObjects{store: store, master: append([]byte(nil), masterKey...)}, nil
}
func (s *EncryptedObjects) derive(label, owner string) []byte {
	h := hmac.New(sha256.New, s.master)
	h.Write([]byte(label + "\x00" + owner))
	return h.Sum(nil)
}
func (s *EncryptedObjects) ref(owner string, body []byte) BodyRef {
	digest := sha256.Sum256(body)
	scope := s.derive("scope-v1", owner)
	return BodyRef{ObjectKey: fmt.Sprintf("observability/v1/%x/%x", scope, digest), Owner: owner, SHA256: hex.EncodeToString(digest[:]), ByteLength: int64(len(body)), EncryptionVersion: 1}
}
func (s *EncryptedObjects) seal(owner string, body, aad []byte) ([]byte, error) {
	block, e := aes.NewCipher(s.derive("aes-gcm-v1", owner))
	if e != nil {
		return nil, e
	}
	g, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	nonce := make([]byte, g.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, e
	}
	return g.Seal(nonce, nonce, body, aad), nil
}
func (s *EncryptedObjects) open(owner string, body, aad []byte) ([]byte, error) {
	block, e := aes.NewCipher(s.derive("aes-gcm-v1", owner))
	if e != nil {
		return nil, e
	}
	g, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	if len(body) < g.NonceSize()+g.Overhead() {
		return nil, errors.New("invalid encrypted object")
	}
	return g.Open(nil, body[:g.NonceSize()], body[g.NonceSize():], aad)
}
func (s *EncryptedObjects) Put(ctx context.Context, owner string, body []byte) (BodyRef, error) {
	if owner == "" {
		return BodyRef{}, errors.New("body owner required")
	}
	ref := s.ref(owner, body)
	if prior, e := s.Get(ctx, owner, ref); e == nil && bytes.Equal(prior, body) {
		return ref, nil
	}
	encrypted, e := s.seal(owner, body, []byte(ref.ObjectKey))
	if e != nil {
		return BodyRef{}, e
	}
	if e = s.store.PutObject(ctx, ref.ObjectKey, encrypted, "application/octet-stream"); e != nil {
		return BodyRef{}, e
	}
	verified, e := s.Get(ctx, owner, ref)
	if e != nil {
		return BodyRef{}, fmt.Errorf("verify encrypted object: %w", e)
	}
	if !bytes.Equal(verified, body) {
		return BodyRef{}, errors.New("object verification mismatch")
	}
	return ref, nil
}
func (s *EncryptedObjects) Get(ctx context.Context, owner string, ref BodyRef) ([]byte, error) {
	if owner == "" || owner != ref.Owner || ref.EncryptionVersion != 1 || ref.ByteLength < 0 {
		return nil, errors.New("invalid body reference or owner")
	}
	scope := s.derive("scope-v1", owner)
	expected := fmt.Sprintf("observability/v1/%x/%s", scope, ref.SHA256)
	digest, e := hex.DecodeString(ref.SHA256)
	if e != nil || len(digest) != 32 || ref.ObjectKey != expected {
		return nil, errors.New("invalid body object identity")
	}
	encrypted, e := s.store.GetObject(ctx, ref.ObjectKey)
	if e != nil {
		return nil, e
	}
	plain, e := s.open(owner, encrypted, []byte(ref.ObjectKey))
	if e != nil {
		return nil, e
	}
	hash := sha256.Sum256(plain)
	if int64(len(plain)) != ref.ByteLength || !hmac.Equal(hash[:], digest) {
		return nil, errors.New("body length or digest mismatch")
	}
	return plain, nil
}

// Reference computes metadata without uploading. SQL callers register this
// intent before Put so failed uploads remain discoverable by orphan maintenance.
func (s *EncryptedObjects) Reference(owner string, body []byte) BodyRef { return s.ref(owner, body) }
func (s *EncryptedObjects) Delete(ctx context.Context, owner string, ref BodyRef) error {
	if owner == "" || owner != ref.Owner || ref.EncryptionVersion != 1 {
		return errors.New("invalid deletion owner")
	}
	scope := s.derive("scope-v1", owner)
	if ref.ObjectKey != fmt.Sprintf("observability/v1/%x/%s", scope, ref.SHA256) {
		return errors.New("invalid deletion identity")
	}
	deleter, ok := s.store.(interface {
		DeleteObservabilityObject(context.Context, string) error
	})
	if !ok {
		return errors.New("object store has no safe observability deletion capability")
	}
	return deleter.DeleteObservabilityObject(ctx, ref.ObjectKey)
}
