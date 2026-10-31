// Package systembackup contains the isolated backup control/data primitives.
// It does not start business workers or mutate source workloads on import.
package systembackup

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const FrameSize = 1 << 20

var envelopeMagic = []byte("CMBKENC1")

// Encrypt writes authenticated bounded frames with an authenticated EOF. key
// must be a fresh 256-bit part DEK; callers persist only its wrapped form. Each
// attempt must use a new DEK; failed attempts must never append to old streams.
func Encrypt(ctx context.Context, dst io.Writer, src io.Reader, key, identity []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return err
	}
	header := make([]byte, len(envelopeMagic)+32)
	copy(header, envelopeMagic)
	if _, err = rand.Read(header[len(envelopeMagic):]); err != nil {
		return err
	}
	if err = writeFull(dst, header); err != nil {
		return err
	}
	buf := make([]byte, FrameSize)
	defer clear(buf)
	var seq uint64
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		n, readErr := io.ReadFull(src, buf)
		if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
			return readErr
		}
		if n > 0 {
			if err = writeFrame(dst, gcm, header, identity, seq, buf[:n]); err != nil {
				return err
			}
			seq++
		}
		if readErr != nil {
			return writeFrame(dst, gcm, header, identity, seq, nil)
		}
	}
}

// Decrypt only releases authenticated frames. The consumer must keep its
// destination isolated until this returns nil: EOF authentication detects
// truncation even when all preceding frames authenticated successfully.
func Decrypt(ctx context.Context, dst io.Writer, src io.Reader, key, identity []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return err
	}
	header := make([]byte, len(envelopeMagic)+32)
	if _, err = io.ReadFull(src, header); err != nil {
		return fmt.Errorf("envelope header: %w", err)
	}
	if !bytes.Equal(header[:len(envelopeMagic)], envelopeMagic) {
		return errors.New("unsupported encrypted envelope")
	}
	for seq := uint64(0); ; seq++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		var size [4]byte
		if _, err = io.ReadFull(src, size[:]); err != nil {
			return fmt.Errorf("missing authenticated end: %w", err)
		}
		n := binary.BigEndian.Uint32(size[:])
		if n > FrameSize {
			return errors.New("encrypted frame exceeds limit")
		}
		sealed := make([]byte, int(n)+gcm.Overhead())
		if _, err = io.ReadFull(src, sealed); err != nil {
			return fmt.Errorf("incomplete encrypted frame: %w", err)
		}
		plain, openErr := gcm.Open(sealed[:0], frameNonce(header, seq), sealed, frameAAD(header, identity, seq, n))
		if openErr != nil {
			return errors.New("encrypted frame authentication failed")
		}
		if n == 0 {
			var extra [1]byte
			_, err = io.ReadFull(src, extra[:])
			if !errors.Is(err, io.EOF) {
				return errors.New("data after authenticated end")
			}
			return nil
		}
		err = writeFull(dst, plain)
		clear(plain)
		if err != nil {
			return err
		}
	}
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("part key must be 256 bits")
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

func frameNonce(header []byte, seq uint64) []byte {
	n := make([]byte, 12)
	copy(n, header[len(envelopeMagic):len(envelopeMagic)+4])
	binary.BigEndian.PutUint64(n[4:], seq)
	return n
}

func frameAAD(header, identity []byte, seq uint64, size uint32) []byte {
	h := sha256.New()
	h.Write(header)
	h.Write(identity)
	var metadata [12]byte
	binary.BigEndian.PutUint64(metadata[:8], seq)
	binary.BigEndian.PutUint32(metadata[8:], size)
	h.Write(metadata[:])
	return h.Sum(nil)
}

func writeFrame(dst io.Writer, gcm cipher.AEAD, header, identity []byte, seq uint64, plain []byte) error {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(plain)))
	sealed := gcm.Seal(nil, frameNonce(header, seq), plain, frameAAD(header, identity, seq, uint32(len(plain))))
	if err := writeFull(dst, size[:]); err != nil {
		return err
	}
	return writeFull(dst, sealed)
}

func writeFull(w io.Writer, p []byte) error {
	n, err := w.Write(p)
	if err == nil && n != len(p) {
		return io.ErrShortWrite
	}
	return err
}
