package systembackup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path"
	"strings"

	"github.com/minio/minio-go/v7"
)

const MaxImmutableObjectBytes int64 = 8 << 20

type ObjectReference struct {
	Key       string `json:"key"`
	VersionID string `json:"version_id"`
	Bytes     int64  `json:"bytes"`
	SHA256    string `json:"sha256"`
}

// ImmutableObjectStore never creates buckets, silently falls back to local
// storage, or overwrites an existing key. Bucket and root come from a resolved
// server-side reference, never an API-supplied URL.
type ImmutableObjectStore struct {
	Client *minio.Client
	Bucket string
	Root   string
}

func (s ImmutableObjectStore) key(key string) (string, error) {
	if s.Client == nil || s.Bucket == "" || s.Root == "" || path.Clean(s.Root) != s.Root || strings.HasPrefix(s.Root, "/") || strings.ContainsAny(s.Root, "\\:\x00") || key == "" || path.Clean(key) != key || strings.HasPrefix(key, "/") || strings.HasPrefix(key, "..") || strings.ContainsAny(key, "\\:\x00") {
		return "", errors.New("invalid immutable object identity")
	}
	if !validObjectSegments(key) || !validObjectSegments(s.Root) {
		return "", errors.New("invalid immutable object key")
	}
	return s.Root + "/" + key, nil
}

// ProbeRead checks the real bucket versioning API. This is not a proof of
// external failure-domain separation or conditional-create support; those
// require separately recorded write/read capability probes.
func (s ImmutableObjectStore) ProbeRead(ctx context.Context) error {
	if _, e := s.key("probe"); e != nil {
		return e
	}
	v, e := s.Client.GetBucketVersioning(ctx, s.Bucket)
	if e != nil {
		return errors.New("artifact_unavailable")
	}
	if v.Status != "Enabled" {
		return errors.New("immutable object versioning is required")
	}
	return nil
}

func (s ImmutableObjectStore) PutImmutable(ctx context.Context, key string, content []byte) (ObjectReference, error) {
	var out ObjectReference
	full, e := s.key(key)
	if e != nil {
		return out, e
	}
	if len(content) == 0 || int64(len(content)) > MaxImmutableObjectBytes {
		return out, errors.New("capacity_exceeded")
	}
	hash := sha256.Sum256(content)
	out = ObjectReference{Key: key, Bytes: int64(len(content)), SHA256: hex.EncodeToString(hash[:])}
	options := minio.PutObjectOptions{ContentType: "application/octet-stream", DisableMultipart: true, UserMetadata: map[string]string{"backup-sha256": out.SHA256}}
	options.SetMatchETagExcept("*")
	info, e := s.Client.PutObject(ctx, s.Bucket, full, bytes.NewReader(content), int64(len(content)), options)
	if e != nil {
		// A timed-out write may already have committed. Read and compare the
		// immutable identity instead of overwriting or guessing the outcome.
		stat, readErr := s.Client.StatObject(ctx, s.Bucket, full, minio.StatObjectOptions{})
		if readErr != nil {
			return out, errors.New("external_result_unknown")
		}
		if stat.Size != out.Bytes || stat.VersionID == "" || stat.VersionID == "null" {
			return out, errors.New("artifact_conflict")
		}
		out.VersionID = stat.VersionID
	} else {
		out.VersionID = info.VersionID
	}
	if out.VersionID == "" || out.VersionID == "null" {
		return out, errors.New("immutable version unavailable")
	}
	actual, e := s.Read(ctx, out)
	if e != nil {
		return out, e
	}
	if !bytes.Equal(actual, content) {
		return out, errors.New("artifact_conflict")
	}
	return out, nil
}

func (s ImmutableObjectStore) Read(ctx context.Context, ref ObjectReference) ([]byte, error) {
	full, e := s.key(ref.Key)
	if e != nil {
		return nil, e
	}
	if ref.VersionID == "" || ref.VersionID == "null" || ref.Bytes <= 0 || ref.Bytes > MaxImmutableObjectBytes || !validHash(ref.SHA256) {
		return nil, errors.New("invalid object reference")
	}
	object, e := s.Client.GetObject(ctx, s.Bucket, full, minio.GetObjectOptions{VersionID: ref.VersionID})
	if e != nil {
		return nil, errors.New("artifact_unavailable")
	}
	defer object.Close()
	stat, e := object.Stat()
	if e != nil {
		return nil, errors.New("artifact_unavailable")
	}
	if stat.Size != ref.Bytes || stat.VersionID != ref.VersionID {
		return nil, errors.New("checksum_mismatch")
	}
	raw, e := io.ReadAll(io.LimitReader(object, ref.Bytes+1))
	if e != nil {
		return nil, errors.New("artifact_unavailable")
	}
	h := sha256.Sum256(raw)
	if int64(len(raw)) != ref.Bytes || hex.EncodeToString(h[:]) != ref.SHA256 {
		return nil, errors.New("checksum_mismatch")
	}
	return raw, nil
}
