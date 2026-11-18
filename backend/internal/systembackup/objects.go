package systembackup

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
)

type ObjectBudget struct {
	MaxBytes int64
	MaxItems int
}
type ObjectMetadata struct {
	Bucket       string            `json:"bucket"`
	Key          string            `json:"key"`
	Version      string            `json:"version"`
	Size         int64             `json:"size"`
	ContentType  string            `json:"content_type"`
	UserMetadata map[string]string `json:"user_metadata,omitempty"`
	ModifiedAt   time.Time         `json:"modified_at"`
}

var isolatedObjectBucket = regexp.MustCompile(`^cm-restore-[a-f0-9]{32}$`)

type objectUploadSink struct {
	*io.PipeWriter
	done   chan error
	closed bool
}

func (s *objectUploadSink) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	s.PipeWriter.Close()
	return <-s.done
}

// RestoreObjects only writes to pre-provisioned, empty, run-specific buckets.
// It never creates buckets or touches the original bucket/key namespace.
func RestoreObjects(ctx context.Context, c *minio.Client, r io.Reader, b ObjectBudget, mapping map[string]string) error {
	if c == nil || len(mapping) == 0 {
		return errors.New("isolated object targets required")
	}
	targets := map[string]bool{}
	for source, target := range mapping {
		if source == target || source == "" || !isolatedObjectBucket.MatchString(target) || targets[target] {
			return errors.New("invalid isolated object target")
		}
		targets[target] = true
		exists, e := c.BucketExists(ctx, target)
		if e != nil || !exists {
			return errors.New("isolated object target unavailable")
		}
		for obj := range c.ListObjects(ctx, target, minio.ListObjectsOptions{Recursive: true, WithVersions: true}) {
			if obj.Err != nil {
				return errors.New("isolated object target unavailable")
			}
			return errors.New("isolated object target not empty")
		}
	}
	return ReadObjectSnapshot(ctx, r, b, func(m ObjectMetadata) (io.WriteCloser, error) {
		target, ok := mapping[m.Bucket]
		if !ok {
			return nil, errors.New("unmapped object bucket")
		}
		reader, writer := io.Pipe()
		sink := &objectUploadSink{PipeWriter: writer, done: make(chan error, 1)}
		go func() {
			opts := minio.PutObjectOptions{ContentType: m.ContentType, UserMetadata: m.UserMetadata}
			_, e := c.PutObject(ctx, target, m.Key, reader, m.Size, opts)
			reader.CloseWithError(e)
			if e != nil {
				e = errors.New("isolated object write failed")
			}
			sink.done <- e
		}()
		return sink, nil
	})
}

type ObjectRecord struct {
	Kind     string          `json:"kind"`
	Buckets  []string        `json:"buckets,omitempty"`
	Metadata *ObjectMetadata `json:"metadata,omitempty"`
	Data     []byte          `json:"data,omitempty"`
	Count    int64           `json:"count,omitempty"`
	SHA256   string          `json:"sha256,omitempty"`
}

// CaptureObjects requires versioned buckets and pins every read to the listed
// version. LIST alone does not prove a cross-provider checkpoint: the caller
// must hold the installation write gate for an acceptance capture.
func CaptureObjects(ctx context.Context, c *minio.Client, w io.Writer, buckets []string, b ObjectBudget) error {
	return captureObjects(ctx, c, w, buckets, b, false)
}

// An online data copy may read a non-versioned source using If-Match and a
// second metadata observation. The saved bytes have their own SHA-256 seal;
// etag: identities are explicitly observations, not immutable provider versions.
func CaptureTeamDataObjects(ctx context.Context, c *minio.Client, w io.Writer, buckets []string, b ObjectBudget) error {
	return captureObjects(ctx, c, w, buckets, b, true)
}
func captureObjects(ctx context.Context, c *minio.Client, w io.Writer, buckets []string, b ObjectBudget, teamData bool) error {
	if c == nil || b.MaxBytes <= 0 || b.MaxItems <= 0 || len(buckets) == 0 {
		return errors.New("invalid object capture configuration")
	}
	buckets = append([]string(nil), buckets...)
	sort.Strings(buckets)
	for i, bucket := range buckets {
		if bucket == "" || (i > 0 && bucket == buckets[i-1]) {
			return errors.New("invalid object bucket inventory")
		}
		v, e := c.GetBucketVersioning(ctx, bucket)
		if e != nil || (!teamData && v.Status != "Enabled") {
			return errors.New("source object versioning required")
		}
	}
	var total, count int64
	whole := sha256.New()
	emit := func(record ObjectRecord, seal bool) error {
		raw, e := json.Marshal(record)
		if e != nil {
			return e
		}
		raw = append(raw, '\n')
		total += int64(len(raw))
		if total > b.MaxBytes {
			return errors.New("capacity_exceeded")
		}
		if !seal {
			whole.Write(raw)
		}
		return writeFull(w, raw)
	}
	if e := emit(ObjectRecord{Kind: "object_bytes_v1", Buckets: buckets}, false); e != nil {
		return e
	}
	buffer := make([]byte, 512<<10)
	for _, bucket := range buckets {
		var previous string
		for item := range c.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true}) {
			if item.Err != nil {
				return errors.New("object inventory unavailable")
			}
			if e := ctx.Err(); e != nil {
				return e
			}
			count++
			if count > int64(b.MaxItems) || item.Key == "" || (previous != "" && item.Key <= previous) {
				return errors.New("invalid object inventory or capacity")
			}
			previous = item.Key
			// ListObjectsV2 omits version IDs on some providers. Stat fixes the selected
			// immutable version before any data is read; gate prevents inventory drift.
			stat, e := c.StatObject(ctx, bucket, item.Key, minio.StatObjectOptions{})
			if e != nil || stat.Size < 0 {
				return errors.New("source object version unavailable")
			}
			version := stat.VersionID
			options := minio.GetObjectOptions{VersionID: version}
			observed := version == "" || version == "null"
			if observed {
				if !teamData || stat.ETag == "" {
					return errors.New("source object version unavailable")
				}
				version = "etag:" + stat.ETag
				options.VersionID = ""
				if e = options.SetMatchETag(stat.ETag); e != nil {
					return errors.New("source object identity unavailable")
				}
			}
			meta := ObjectMetadata{bucket, item.Key, version, stat.Size, stat.ContentType, stat.UserMetadata, stat.LastModified}
			if e = emit(ObjectRecord{Kind: "object", Metadata: &meta}, false); e != nil {
				return e
			}
			object, e := c.GetObject(ctx, bucket, item.Key, options)
			if e != nil {
				return errors.New("source object unavailable")
			}
			hash := sha256.New()
			var size int64
			for {
				n, re := object.Read(buffer)
				if n > 0 {
					size += int64(n)
					if size > stat.Size {
						object.Close()
						return errors.New("source object changed")
					}
					hash.Write(buffer[:n])
					if e = emit(ObjectRecord{Kind: "chunk", Data: buffer[:n]}, false); e != nil {
						object.Close()
						return e
					}
				}
				if re != nil {
					object.Close()
					if re != io.EOF {
						return errors.New("source object unavailable")
					}
					break
				}
			}
			if size != stat.Size {
				return errors.New("source object truncated")
			}
			if observed {
				after, e := c.StatObject(ctx, bucket, item.Key, minio.StatObjectOptions{})
				if e != nil || after.ETag != stat.ETag || after.Size != stat.Size || !after.LastModified.Equal(stat.LastModified) || after.ContentType != stat.ContentType || !reflect.DeepEqual(after.UserMetadata, stat.UserMetadata) {
					return errors.New("source object changed")
				}
			}
			if e = emit(ObjectRecord{Kind: "end_object", Count: size, SHA256: hex.EncodeToString(hash.Sum(nil))}, false); e != nil {
				return e
			}
		}
	}
	return emit(ObjectRecord{Kind: "end", Count: count, SHA256: hex.EncodeToString(whole.Sum(nil))}, true)
}

// ReadObjectSnapshot streams bytes only to an isolated sink. The caller must
// destroy the target if any final seal, metadata or object digest fails.
func ReadObjectSnapshot(ctx context.Context, r io.Reader, b ObjectBudget, onObject func(ObjectMetadata) (io.WriteCloser, error)) error {
	if b.MaxBytes <= 0 || b.MaxItems <= 0 {
		return errors.New("invalid object budget")
	}
	scan := bufio.NewScanner(io.LimitReader(r, b.MaxBytes+1))
	scan.Buffer(make([]byte, 64<<10), 2<<20)
	var sink io.WriteCloser
	defer func() {
		if sink != nil {
			sink.Close()
		}
	}()
	whole, part := sha256.New(), sha256.New()
	var total, count, size int64
	var current *ObjectMetadata
	started, ended := false, false
	allowed := map[string]bool{}
	previous := ""
	for scan.Scan() {
		if e := ctx.Err(); e != nil {
			return e
		}
		raw := scan.Bytes()
		total += int64(len(raw)) + 1
		if total > b.MaxBytes || ended {
			return errors.New("object archive exceeds bounds")
		}
		var record ObjectRecord
		if e := strictDecode(raw, &record); e != nil {
			return errors.New("invalid object record")
		}
		if !started {
			if record.Kind != "object_bytes_v1" || len(record.Buckets) == 0 {
				return errors.New("invalid object archive")
			}
			for _, bucket := range record.Buckets {
				if bucket == "" || allowed[bucket] {
					return errors.New("invalid object bucket")
				}
				allowed[bucket] = true
			}
			started = true
		} else {
			switch record.Kind {
			case "object":
				m := record.Metadata
				if current != nil || m == nil || !allowed[m.Bucket] || m.Key == "" || strings.ContainsRune(m.Key, 0) || m.Version == "" || m.Version == "null" || m.Size < 0 || m.Size > b.MaxBytes {
					return errors.New("invalid object metadata")
				}
				identity := m.Bucket + "\x00" + m.Key
				if identity <= previous {
					return errors.New("duplicate object identity")
				}
				previous = identity
				count++
				if count > int64(b.MaxItems) {
					return errors.New("capacity_exceeded")
				}
				current = m
				size = 0
				part.Reset()
				if onObject != nil {
					var e error
					sink, e = onObject(*m)
					if e != nil {
						return e
					}
					if sink == nil {
						return errors.New("object sink unavailable")
					}
				}
			case "chunk":
				if current == nil || len(record.Data) == 0 || len(record.Data) > 512<<10 {
					return errors.New("invalid object chunk")
				}
				size += int64(len(record.Data))
				if size > current.Size {
					return errors.New("object size mismatch")
				}
				part.Write(record.Data)
				if sink != nil {
					if e := writeFull(sink, record.Data); e != nil {
						return e
					}
				}
			case "end_object":
				if current == nil || size != current.Size || record.Count != size || record.SHA256 != hex.EncodeToString(part.Sum(nil)) {
					return errors.New("object checksum mismatch")
				}
				if sink != nil {
					if e := sink.Close(); e != nil {
						return e
					}
					sink = nil
				}
				current = nil
			case "end":
				if current != nil || record.Count != count || record.SHA256 != hex.EncodeToString(whole.Sum(nil)) {
					return errors.New("object archive checksum mismatch")
				}
				ended = true
			default:
				return errors.New("unknown object record")
			}
		}
		if record.Kind != "end" {
			var canonical bytes.Buffer
			if e := json.NewEncoder(&canonical).Encode(record); e != nil {
				return e
			}
			whole.Write(canonical.Bytes())
		}
	}
	if e := scan.Err(); e != nil {
		return e
	}
	if !ended {
		return errors.New("incomplete object archive")
	}
	return nil
}
