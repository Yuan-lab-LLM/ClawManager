package envelope

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
)

// Compress is intended to run inside Seal's producer. Plaintext goes directly
// through gzip into the encrypted writer; there is no plaintext temporary file.
func Compress(dst io.Writer, produce func(io.Writer) error) error {
	if produce == nil {
		return ErrBinding
	}
	z := gzip.NewWriter(dst)
	if e := produce(z); e != nil {
		z.Close()
		return e
	}
	return z.Close()
}

// ConsumeCompressed decrypts into a bounded pipe and decompresses into consume.
// It waits for BOTH the consumer and the final GCM authentication. Consumers
// must remain inert until this function succeeds. Early return by consume is
// rejected; a truncated or unused suffix cannot masquerade as a restored part.
func ConsumeCompressed(ctx context.Context, src io.Reader, key []byte, b Binding, maxCompressed, maxPlain int64, consume func(io.Reader) error) error {
	if maxCompressed <= 0 || maxPlain <= 0 || maxPlain == int64(^uint64(0)>>1) || consume == nil {
		return ErrBinding
	}
	r, w := io.Pipe()
	done := make(chan error, 1)
	go func() { e := Open(ctx, src, w, key, b, maxCompressed); w.CloseWithError(e); done <- e }()
	z, e := gzip.NewReader(r)
	if e != nil {
		r.CloseWithError(ErrIntegrity)
		<-done
		return ErrIntegrity
	}
	limited := &io.LimitedReader{R: z, N: maxPlain + 1}
	e = consume(limited)
	if e == nil {
		var extra [1]byte
		n, tailErr := limited.Read(extra[:])
		if n != 0 || tailErr != io.EOF || limited.N <= 0 {
			e = ErrIntegrity
		}
	}
	closeErr := z.Close()
	r.CloseWithError(io.ErrClosedPipe)
	cryptoErr := <-done
	if e != nil || closeErr != nil || cryptoErr != nil {
		return errors.Join(e, ErrIntegrity)
	}
	return nil
}
