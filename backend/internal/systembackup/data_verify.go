package systembackup

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"strings"

	"github.com/minio/minio-go/v7"
)

// VerifyFiles rereads restored bytes against the authenticated archive. It
// never writes or repairs differences. Archive links are inspected, not followed.
func VerifyFiles(ctx context.Context, r io.Reader, target string, b FileBudget) (int, error) {
	if e := b.validate(); e != nil {
		return 0, e
	}
	root, e := os.OpenRoot(target)
	if e != nil {
		return 0, e
	}
	defer root.Close()
	tr := tar.NewReader(contextReader{ctx, r})
	seen := map[string]bool{}
	parents := map[string]bool{".": true, "roots": true}
	var total int64
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return 0, e
		}
		if !safeArchiveName(h.Name) || seen[h.Name] || len(seen) >= b.MaxEntries || h.Size < 0 || h.Size > b.MaxBytes-total {
			return 0, errors.New("invalid file verification inventory")
		}
		seen[h.Name] = true
		total += h.Size
		for p := path.Dir(h.Name); p != "."; p = path.Dir(p) {
			parents[p] = true
		}
		info, e := root.Lstat(h.Name)
		if e != nil {
			return 0, errors.New("restored file missing")
		}
		actual, e := tar.FileInfoHeader(info, "")
		if e != nil {
			return 0, e
		}
		if actual.Typeflag != h.Typeflag || actual.Uid != h.Uid || actual.Gid != h.Gid {
			return 0, errors.New("restored file identity mismatch")
		}
		if h.Typeflag != tar.TypeSymlink && (actual.Mode != h.Mode || !actual.ModTime.Equal(h.ModTime)) {
			return 0, errors.New("restored file metadata mismatch")
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if h.Size != 0 {
				return 0, errors.New("invalid directory payload")
			}
		case tar.TypeSymlink:
			link, e := root.Readlink(h.Name)
			if e != nil || link != h.Linkname || h.Size != 0 {
				return 0, errors.New("restored link mismatch")
			}
		case tar.TypeReg:
			if actual.Size != h.Size {
				return 0, errors.New("restored file size mismatch")
			}
			expected := sha256.New()
			if _, e = io.Copy(expected, tr); e != nil {
				return 0, e
			}
			f, e := root.Open(h.Name)
			if e != nil {
				return 0, e
			}
			opened, e := f.Stat()
			if e != nil || !os.SameFile(info, opened) {
				f.Close()
				return 0, errors.New("restored file changed during verification")
			}
			digest := sha256.New()
			n, readErr := io.Copy(digest, contextReader{ctx, io.LimitReader(f, h.Size+1)})
			after, statErr := f.Stat()
			closeErr := f.Close()
			if readErr != nil || statErr != nil || closeErr != nil || n != h.Size || !after.ModTime().Equal(info.ModTime()) || !bytes.Equal(expected.Sum(nil), digest.Sum(nil)) {
				return 0, errors.New("restored file checksum mismatch")
			}
		default:
			return 0, errors.New("unsupported restored file type")
		}
	}
	var extra [1]byte
	if n, e := io.ReadFull(contextReader{ctx, r}, extra[:]); n != 0 || e != io.EOF {
		return 0, errors.New("trailing file archive payload")
	}
	if e = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !seen[name] && !(parents[name] && entry.IsDir()) {
			return errors.New("unexpected restored file")
		}
		return ctx.Err()
	}); e != nil {
		return 0, e
	}
	return len(seen), nil
}

type comparingObjectSink struct {
	object *minio.Object
	closed bool
}

func (s *comparingObjectSink) Write(p []byte) (int, error) {
	b := make([]byte, len(p))
	n, e := io.ReadFull(s.object, b)
	if e != nil || !bytes.Equal(p, b) {
		return 0, errors.New("restored object checksum mismatch")
	}
	return n, nil
}
func (s *comparingObjectSink) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	var b [1]byte
	n, e := s.object.Read(b[:])
	closeErr := s.object.Close()
	if n != 0 || e != io.EOF {
		return errors.New("restored object has extra data")
	}
	return closeErr
}

// VerifyObjects uses GET/LIST only, and rejects missing, extra and duplicate
// versions in the newly provisioned target buckets.
func VerifyObjects(ctx context.Context, c *minio.Client, r io.Reader, b ObjectBudget, mapping map[string]string) (int, error) {
	if c == nil || len(mapping) == 0 {
		return 0, errors.New("invalid object verification target")
	}
	seen := map[string]bool{}
	targets := map[string]bool{}
	for source, target := range mapping {
		if source == target || !isolatedObjectBucket.MatchString(target) || targets[target] {
			return 0, errors.New("invalid object verification target")
		}
		targets[target] = true
	}
	e := ReadObjectSnapshot(ctx, r, b, func(m ObjectMetadata) (io.WriteCloser, error) {
		target, ok := mapping[m.Bucket]
		if !ok {
			return nil, errors.New("unmapped object bucket")
		}
		identity := target + "\x00" + m.Key
		if seen[identity] {
			return nil, errors.New("duplicate restored object")
		}
		seen[identity] = true
		info, e := c.StatObject(ctx, target, m.Key, minio.StatObjectOptions{})
		if e != nil {
			return nil, e
		}
		if info.Size != m.Size || info.ContentType != m.ContentType || !maps.Equal(info.UserMetadata, m.UserMetadata) {
			return nil, errors.New("restored object metadata mismatch")
		}
		object, e := c.GetObject(ctx, target, m.Key, minio.GetObjectOptions{VersionID: info.VersionID})
		if e != nil {
			return nil, e
		}
		return &comparingObjectSink{object: object}, nil
	})
	if e != nil {
		return 0, e
	}
	observed := map[string]bool{}
	for target := range targets {
		for info := range c.ListObjects(ctx, target, minio.ListObjectsOptions{Recursive: true, WithVersions: true}) {
			if info.Err != nil {
				return 0, info.Err
			}
			key := target + "\x00" + info.Key
			if !seen[key] || observed[key] || info.IsDeleteMarker {
				return 0, errors.New("unexpected restored object version")
			}
			observed[key] = true
		}
	}
	if len(observed) != len(seen) {
		return 0, errors.New("restored object inventory mismatch")
	}
	return len(seen), nil
}

// VerifyRedisHistory preserves the deliberate no-expiry history policy while
// checking DUMP bytes (including stream consumer groups and pending entries).
func VerifyRedisHistory(ctx context.Context, c *RedisClient, r io.Reader, b RedisBudget, runID string) (int, error) {
	if c == nil || !publicIDPattern.MatchString(runID) || !strings.HasPrefix(runID, "sdr_") {
		return 0, errors.New("invalid Redis verification identity")
	}
	count := 0
	e := ReadRedisSnapshot(r, b, func(record RedisRecord) error {
		key := "cm:restore-history:" + runID + ":" + hex.EncodeToString(record.Key)
		v, e := c.command(ctx, []byte("EVAL_RO"), []byte("return {redis.call('DUMP',KEYS[1]),redis.call('TYPE',KEYS[1]).ok,redis.call('PTTL',KEYS[1])}"), []byte("1"), []byte(key))
		if e != nil {
			return e
		}
		values, ok := v.([]any)
		if !ok || len(values) != 3 {
			return errors.New("invalid Redis verification response")
		}
		dump, ok := values[0].([]byte)
		kind, kok := values[1].([]byte)
		ttl, tok := values[2].(int64)
		if !ok || !kok || !tok || ttl != -1 || string(kind) != record.Type {
			return errors.New("restored Redis history mismatch")
		}
		equal, e := redisDumpsEqual(record.Type, record.Dump, dump)
		if e != nil {
			return e
		}
		if !equal {
			return errors.New("restored Redis history mismatch")
		}
		count++
		return nil
	})
	if e != nil {
		return 0, e
	}
	v, e := c.command(ctx, []byte("DBSIZE"))
	if e != nil {
		return 0, e
	}
	size, ok := v.(int64)
	if !ok || size != int64(count) {
		return 0, errors.New("unexpected restored Redis keys")
	}
	return count, nil
}
