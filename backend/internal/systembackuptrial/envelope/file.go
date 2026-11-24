package envelope

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"regexp"
)

var ErrFile = errors.New("staging file identity, ownership or seal is invalid")
var fileName = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,127}$`)

// Receipt describes a sealed local ciphertext object. The controller must save
// it in its durable resource journal. This is not an artifact registration.
type Receipt struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// SealFile never overwrites an existing object. Only the partial file created
// by this invocation may be removed on failure. The caller owns root exclusively
// and has journaled its intent before calling this function.
func SealFile(ctx context.Context, root *os.Root, name string, key []byte, b Binding, produce func(io.Writer) error) (receipt Receipt, err error) {
	return WriteUnpublishedFile(ctx, root, name, func(dst io.Writer) error { return Seal(ctx, dst, key, b, produce) })
}

// WriteUnpublishedFile durably creates ONLY a local candidate. It grants no
// artifact publication/acceptance. The producer must emit ciphertext or public
// metadata, never source plaintext. Original SealFile uses the same safe path.
func WriteUnpublishedFile(ctx context.Context, root *os.Root, name string, produce func(io.Writer) error) (receipt Receipt, err error) {
	if root == nil || ctx == nil || produce == nil || !fileName.MatchString(name) {
		return receipt, ErrFile
	}
	if ctx.Err() != nil {
		return receipt, ctx.Err()
	}
	partial := "." + name + ".partial"
	f, e := root.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return receipt, ErrFile
	}
	identity, e := f.Stat()
	if e != nil {
		f.Close()
		return receipt, ErrFile
	}
	closed := false
	defer func() {
		if !closed {
			f.Close()
		}
		if observed, e := root.Lstat(partial); e == nil && os.SameFile(identity, observed) {
			if root.Remove(partial) != nil && err == nil {
				err = ErrFile
			}
		}
	}()
	h := sha256.New()
	counter := &countWriter{w: io.MultiWriter(f, h)}
	if e = produce(counter); e != nil {
		return receipt, e
	}
	if f.Sync() != nil {
		return receipt, ErrFile
	}
	if e = f.Close(); e != nil {
		return receipt, ErrFile
	}
	closed = true
	if ctx.Err() != nil {
		return receipt, ctx.Err()
	}
	observed, e := root.Lstat(partial)
	if e != nil || !os.SameFile(identity, observed) || !observed.Mode().IsRegular() {
		return receipt, ErrFile
	}
	// Linking is atomic create-if-absent; Rename would overwrite on Unix.
	if e = root.Link(partial, name); e != nil {
		return receipt, ErrFile
	}
	if root.Remove(partial) != nil || root.Chmod(name, 0400) != nil || syncDirectory(root) != nil {
		return receipt, ErrFile
	}
	receipt = Receipt{Name: name, Size: counter.n, SHA256: hex.EncodeToString(h.Sum(nil))}
	return receipt, nil
}

type countWriter struct {
	w io.Writer
	n int64
}

func (w *countWriter) Write(p []byte) (int, error) {
	n, e := w.w.Write(p)
	w.n += int64(n)
	return n, e
}

// OpenFile verifies the whole ciphertext BEFORE returning the same pinned file
// handle for decryption. It never follows a staging leaf symlink. AEAD still has
// to be checked by Open, including its mandatory final frame.
func OpenFile(ctx context.Context, root *os.Root, r Receipt) (*os.File, error) {
	return OpenFileGuarded(ctx, root, r, func(ctx context.Context) error { return ctx.Err() })
}

// Guarded verification allows a publisher to maintain its CURRENT task claim
// while hashing large immutable C copies. It never renews a DEK/provider grant.
func OpenFileGuarded(ctx context.Context, root *os.Root, r Receipt, check func(context.Context) error) (*os.File, error) {
	if ctx == nil || ctx.Err() != nil || check == nil || check(ctx) != nil || ctx.Err() != nil {
		return nil, ErrFile
	}
	if root == nil || !fileName.MatchString(r.Name) || r.Size <= 0 || len(r.SHA256) != 64 {
		return nil, ErrFile
	}
	if _, e := hex.DecodeString(r.SHA256); e != nil {
		return nil, ErrFile
	}
	before, e := root.Lstat(r.Name)
	if e != nil || !before.Mode().IsRegular() || before.Size() != r.Size {
		return nil, ErrFile
	}
	f, e := root.Open(r.Name)
	if e != nil {
		return nil, ErrFile
	}
	good := false
	defer func() {
		if !good {
			f.Close()
		}
	}()
	actual, e := f.Stat()
	if e != nil || !os.SameFile(before, actual) {
		return nil, ErrFile
	}
	h := sha256.New()
	buf := make([]byte, 128<<10)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if e = check(ctx); e != nil {
			return nil, e
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		n, readErr := f.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > r.Size {
				return nil, ErrFile
			}
			h.Write(buf[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, ErrFile
		}
	}
	if ctx.Err() != nil || total != r.Size || hex.EncodeToString(h.Sum(nil)) != r.SHA256 || check(ctx) != nil || ctx.Err() != nil {
		return nil, ErrFile
	}
	if _, e = f.Seek(0, io.SeekStart); e != nil {
		return nil, ErrFile
	}
	good = true
	return f, nil
}
