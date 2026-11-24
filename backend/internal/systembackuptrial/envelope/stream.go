// Package staging provides bounded encrypted local staging for domain adapters.
// It is NOT the published artifact/envelope format. The shared finalizer owns
// that format, KEK wrapping, signatures, catalog and acceptance certificates.
package envelope

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
)

const chunkSize = 1 << 20
const magic = "CMSTG01\n"

var ErrIntegrity = errors.New("encrypted staging authentication failed")
var ErrBinding = errors.New("invalid staging key or capture binding")

// Binding is frozen before capture. The DEK is supplied by a task-scoped key
// provider, never embedded in this object or persisted by this package.
type Binding struct {
	BackupID           string `json:"backup_id"`
	Attempt            string `json:"attempt"`
	Path               string `json:"path"`
	PartIndex          uint32 `json:"part_index"`
	CaptureContextHash string `json:"capture_context_hash"`
	EnvelopeID         string `json:"envelope_id"`
}

func setup(key []byte, b Binding) (cipher.AEAD, []byte, error) {
	if len(key) != 32 || b.BackupID == "" || b.Attempt == "" || b.Path == "" || len(b.CaptureContextHash) != 64 || b.EnvelopeID == "" {
		return nil, nil, ErrBinding
	}
	if _, e := hex.DecodeString(b.CaptureContextHash); e != nil {
		return nil, nil, ErrBinding
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, nil, ErrBinding
	}
	aead, e := cipher.NewGCM(block)
	if e != nil {
		return nil, nil, ErrBinding
	}
	data, e := json.Marshal(b)
	if e != nil {
		return nil, nil, ErrBinding
	}
	hash := sha256.Sum256(append([]byte(magic), data...))
	return aead, hash[:], nil
}
func aad(binding []byte, index uint64, final bool) []byte {
	b := make([]byte, len(binding)+9)
	copy(b, binding)
	binary.BigEndian.PutUint64(b[len(binding):], index)
	if final {
		b[len(b)-1] = 1
	}
	return b
}

// Seal writes only ciphertext to dst. A failed producer leaves an unsealed
// object that Open rejects. Never resume a partially written stream: retry into
// a new owner/attempt staging object with a fresh DEK.
func Seal(ctx context.Context, dst io.Writer, key []byte, b Binding, produce func(io.Writer) error) error {
	a, hash, e := setup(key, b)
	if e != nil {
		return e
	}
	if produce == nil {
		return ErrBinding
	}
	if e = writeAll(dst, []byte(magic)); e != nil {
		return e
	}
	w := &encryptWriter{ctx: ctx, dst: dst, aead: a, binding: hash, buf: make([]byte, 0, chunkSize)}
	defer clear(w.buf[:cap(w.buf)])
	if e = produce(w); e != nil {
		return e
	}
	if w.err != nil {
		return w.err
	}
	if len(w.buf) > 0 {
		if e = w.flush(false); e != nil {
			return e
		}
	}
	return w.flush(true)
}

type encryptWriter struct {
	ctx          context.Context
	dst          io.Writer
	aead         cipher.AEAD
	binding, buf []byte
	index        uint64
	err          error
}

func (w *encryptWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n := 0
	for len(p) > 0 {
		take := min(chunkSize-len(w.buf), len(p))
		w.buf = append(w.buf, p[:take]...)
		p = p[take:]
		n += take
		if len(w.buf) == chunkSize {
			if e := w.flush(false); e != nil {
				w.err = e
				return n, e
			}
		}
	}
	return n, nil
}
func (w *encryptWriter) flush(final bool) error {
	if e := w.ctx.Err(); e != nil {
		return e
	}
	if w.index == ^uint64(0) {
		return ErrIntegrity
	}
	nonce := make([]byte, w.aead.NonceSize())
	if _, e := io.ReadFull(rand.Reader, nonce); e != nil {
		return e
	}
	ct := w.aead.Seal(nil, nonce, w.buf, aad(w.binding, w.index, final))
	header := make([]byte, 5)
	if final {
		header[0] = 1
	}
	binary.BigEndian.PutUint32(header[1:], uint32(len(ct)))
	for _, p := range [][]byte{header, nonce, ct} {
		if e := writeAll(w.dst, p); e != nil {
			return e
		}
	}
	clear(w.buf)
	w.buf = w.buf[:0]
	w.index++
	return nil
}
func writeAll(w io.Writer, p []byte) error {
	n, e := w.Write(p)
	if e == nil && n != len(p) {
		e = io.ErrShortWrite
	}
	return e
}

// Open authenticates each chunk and a mandatory final frame, checks trailing
// bytes, and enforces a decompressed-agnostic plaintext limit. dst must be an
// inert transaction/quarantine sink: it may receive a valid prefix before a
// corrupt final frame is detected and must not publish until Open succeeds.
func Open(ctx context.Context, src io.Reader, dst io.Writer, key []byte, b Binding, maxBytes int64) error {
	a, hash, e := setup(key, b)
	if e != nil {
		return e
	}
	if maxBytes <= 0 {
		return ErrBinding
	}
	header := make([]byte, len(magic))
	if _, e = io.ReadFull(src, header); e != nil || string(header) != magic {
		return ErrIntegrity
	}
	var total int64
	for index := uint64(0); ; index++ {
		if e = ctx.Err(); e != nil {
			return e
		}
		if index == ^uint64(0) {
			return ErrIntegrity
		}
		h := make([]byte, 5)
		if _, e = io.ReadFull(src, h); e != nil {
			return ErrIntegrity
		}
		n := int(binary.BigEndian.Uint32(h[1:]))
		final := h[0] == 1
		if h[0] > 1 || n < a.Overhead() || n > chunkSize+a.Overhead() || (final && n != a.Overhead()) || (!final && n == a.Overhead()) {
			return ErrIntegrity
		}
		nonce := make([]byte, a.NonceSize())
		if _, e = io.ReadFull(src, nonce); e != nil {
			return ErrIntegrity
		}
		ct := make([]byte, n)
		if _, e = io.ReadFull(src, ct); e != nil {
			return ErrIntegrity
		}
		plain, e := a.Open(nil, nonce, ct, aad(hash, index, final))
		if e != nil {
			return ErrIntegrity
		}
		total += int64(len(plain))
		if total > maxBytes {
			clear(plain)
			return ErrIntegrity
		}
		if final {
			var trailing [1]byte
			_, e = io.ReadFull(src, trailing[:])
			if e != io.EOF {
				return ErrIntegrity
			}
			return nil
		}
		e = writeAll(dst, plain)
		clear(plain)
		if e != nil {
			return e
		}
	}
}
