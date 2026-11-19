package systembackup

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"clawreef/internal/observability"
	"clawreef/internal/systembackup/team"
	"github.com/minio/minio-go/v7"
)

const observationKeySecret = "clawmanager-observability-key-v1"

// The historical key is used only in this verification call. It is never
// installed in Kubernetes or activated in the restored application.
func historyMasterKey(raw json.RawMessage) ([]byte, error) {
	var secret struct {
		Immutable bool              `json:"immutable"`
		Data      map[string]string `json:"data"`
	}
	if json.Unmarshal(raw, &secret) != nil || !secret.Immutable {
		return nil, errors.New("immutable history master key required")
	}
	encoded, err := base64.StdEncoding.DecodeString(secret.Data["master-key"])
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil || len(key) != 32 {
		return nil, errors.New("invalid history master key")
	}
	return key, nil
}

func historyBodyRefs(raw []byte) ([]observability.BodyRef, error) {
	deps, err := observability.ArchiveDependencies(raw)
	var refs []observability.BodyRef
	for _, dep := range deps {
		if dep.Ref.ByteLength > maxRowBytes {
			return nil, errors.New("history object exceeds bound")
		}
		refs = append(refs, dep.Ref)
	}
	return refs, err
}

type historyCiphertext struct {
	key string
	raw []byte
}

func (s historyCiphertext) PutObject(context.Context, string, []byte, string) error {
	return errors.New("read-only history verifier")
}
func (s historyCiphertext) GetObject(_ context.Context, key string) ([]byte, error) {
	if key != s.key {
		return nil, errors.New("unmapped history dependency")
	}
	return s.raw, nil
}

type historyObjectSink struct {
	bytes.Buffer
	finish func([]byte) error
}

func (s *historyObjectSink) Write(p []byte) (int, error) {
	if s.Len()+len(p) > maxRowBytes+64 {
		return 0, errors.New("encrypted history object exceeds bound")
	}
	return s.Buffer.Write(p)
}
func (s *historyObjectSink) Close() error { return s.finish(s.Bytes()) }

type historyDiscard struct{}

func (historyDiscard) Write(p []byte) (int, error) { return len(p), nil }
func (historyDiscard) Close() error                { return nil }

// Verify bytes from the signed object part, or from its exact isolated target
// mapping. BasePath is derived from signed object names; ambiguous copies fail.
// Nested body references are followed with bounded passes and total bytes.
func verifyHistoryObjectDependencies(ctx context.Context, refs []observability.BodyRef, h *historyInventory, secret json.RawMessage, snapshot func(func(ObjectMetadata) (io.WriteCloser, error)) error, target *RestoredDataReader, b ObjectBudget) (map[string]observability.BodyRef, error) {
	if len(refs) == 0 && len(h.live) == 0 {
		return nil, nil
	}
	key, err := historyMasterKey(secret)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	pending := map[string]observability.BodyRef{}
	verified := map[string]observability.BodyRef{}
	expand := map[string]bool{}
	locations := map[string]string{}
	var total int64
	var edgeCount int
	add := func(ref observability.BodyRef, required bool) error {
		if old, ok := pending[ref.ObjectKey]; ok && old != ref {
			return errors.New("conflicting history dependency identity")
		}
		pending[ref.ObjectKey] = ref
		_, archiveRoot := h.archiveKinds[ref.ObjectKey]
		if required && !expand[ref.ObjectKey] && (archiveRoot || !h.bodyKeys[ref.ObjectKey]) {
			expand[ref.ObjectKey] = true
			// The object may already have been authenticated as an upload intent
			// before its parent identified it as an archive dependency.
			delete(verified, ref.ObjectKey)
		}
		if len(pending) > b.MaxItems {
			return errors.New("history dependency inventory exceeds bound")
		}
		return nil
	}
	for _, ref := range refs {
		if err = add(ref, true); err != nil {
			return nil, err
		}
	}
	for pass := 0; pass < 8; pass++ {
		progress := false
		err = snapshot(func(m ObjectMetadata) (io.WriteCloser, error) {
			pos := strings.LastIndex(m.Key, "observability/v1/")
			if pos < 0 || (pos > 0 && m.Key[pos-1] != '/') {
				return historyDiscard{}, nil
			}
			objectKey := m.Key[pos:]
			// Registration precedes upload. A present orphan can earn proof, while
			// a failed upload has no ciphertext and cannot earn one.
			if intent, ok := h.live[objectKey]; ok {
				if err := add(intent, false); err != nil {
					return nil, err
				}
			}
			ref, ok := pending[objectKey]
			if !ok {
				return historyDiscard{}, nil
			}
			location := m.Bucket + "/" + m.Key
			if prior, ok := locations[objectKey]; ok && prior != location {
				return nil, errors.New("ambiguous history object binding")
			}
			locations[objectKey] = location
			if _, ok := verified[objectKey]; ok {
				return historyDiscard{}, nil
			}
			return &historyObjectSink{finish: func(ciphertext []byte) error {
				if target != nil {
					bucket, ok := target.BucketMapping[m.Bucket]
					if !ok || !isolatedObjectBucket.MatchString(bucket) {
						return errors.New("history target mapping missing")
					}
					o, e := target.Objects.GetObject(ctx, bucket, m.Key, minio.GetObjectOptions{})
					if e != nil {
						return e
					}
					ciphertext, e = io.ReadAll(io.LimitReader(o, maxRowBytes+65))
					o.Close()
					if e != nil {
						return e
					}
					if len(ciphertext) > maxRowBytes+64 {
						return errors.New("history target object exceeds bound")
					}
				}
				encrypted, e := observability.NewEncryptedObjects(historyCiphertext{objectKey, ciphertext}, key)
				if e != nil {
					return e
				}
				plain, e := encrypted.Get(ctx, ref.Owner, ref)
				if e != nil {
					return e
				}
				total += int64(len(plain))
				if total > b.MaxBytes {
					return errors.New("history hydration exceeds bound")
				}
				progress = true
				if !expand[objectKey] {
					verified[objectKey] = ref
					return nil
				}
				nested, e := observability.ArchiveDependencies(plain)
				if e != nil {
					return e
				}
				kind, archive := h.archiveKinds[objectKey]
				if h.edges != nil && (archive || len(nested) != 0) {
					h.edges[objectKey] = nil
				}
				for _, dep := range nested {
					child := dep.Ref
					if child.Owner != ref.Owner {
						return errors.New("history nested body owner differs")
					}
					if h.expired(dep, kind) {
						continue
					}
					edgeCount++
					if edgeCount > b.MaxItems || child.ByteLength > maxRowBytes {
						return errors.New("nested history dependency inventory exceeds bound")
					}
					if h.edges != nil {
						h.edges[objectKey] = append(h.edges[objectKey], child.ObjectKey)
					}
					if e = add(child, true); e != nil {
						return e
					}
				}
				verified[objectKey] = ref
				return nil
			}}, nil
		})
		if err != nil {
			return nil, err
		}
		if len(verified) == len(pending) {
			return verified, nil
		}
		if !progress {
			return nil, errors.New("history object dependency missing from captured buckets")
		}
	}
	return nil, errors.New("history dependency nesting exceeds bound")
}

func verifyPublishedHistoryDependencies(ctx context.Context, store ImmutableObjectStore, artifact PublishedArtifact, wrapper DataKeyWrapper, b CaptureBudget, target *RestoredDataReader, edges map[string][]string) (map[string]observability.BodyRef, error) {
	consume := func(name string, fn func(io.Reader) error) error {
		return ConsumePublishedPart(ctx, store, artifact, name, wrapper, b.MaxBytes, fn)
	}
	var refs []observability.BodyRef
	h := newHistoryInventory()
	h.edges = edges
	if err := consume("mysql", func(r io.Reader) error {
		_, err := ReadSQLSnapshot(r, nil, func(t SQLTable, row team.Row) error {
			return h.record(t, row, b.MaxItems)
		})
		return err
	}); err != nil {
		return nil, err
	}
	for _, dep := range h.sources {
		if !h.expired(dep, dep.Field.Table) {
			refs = append(refs, dep.Ref)
		}
	}
	if len(refs) == 0 && len(h.live) == 0 {
		return nil, nil
	}
	var secret json.RawMessage
	if err := consume("secret_bundle", func(r io.Reader) error {
		return ReadKubernetesHistory(ctx, r, "secret_bundle", ObjectBudget{b.MaxBytes, b.MaxItems}, func(s KubernetesSelection, raw json.RawMessage) error {
			if s.Name == observationKeySecret {
				if secret != nil {
					return errors.New("duplicate history master key")
				}
				secret = append([]byte(nil), raw...)
			}
			return nil
		})
	}); err != nil {
		return nil, err
	}
	return verifyHistoryObjectDependencies(ctx, refs, h, secret, func(fn func(ObjectMetadata) (io.WriteCloser, error)) error {
		return consume("object_storage", func(r io.Reader) error { return ReadObjectSnapshot(ctx, r, ObjectBudget{b.MaxBytes, b.MaxItems}, fn) })
	}, target, ObjectBudget{b.MaxBytes, b.MaxItems})
}
