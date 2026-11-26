package envelope

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

func binding() Binding {
	return Binding{BackupID: "fixture", Attempt: "1", Path: "mysql/users-instances", CaptureContextHash: string(bytes.Repeat([]byte{'a'}, 64)), EnvelopeID: "test-only-key-version"}
}
func TestEncryptedStagingRoundTripAndTamper(t *testing.T) {
	ctx := context.Background()
	key := bytes.Repeat([]byte{7}, 32)
	payload := bytes.Repeat([]byte("synthetic-password-and-session-data"), 100000)
	var encrypted bytes.Buffer
	if e := Seal(ctx, &encrypted, key, binding(), func(w io.Writer) error { _, e := w.Write(payload); return e }); e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(encrypted.Bytes(), []byte("synthetic-password")) {
		t.Fatal("plaintext leaked")
	}
	var decrypted bytes.Buffer
	if e := Open(ctx, bytes.NewReader(encrypted.Bytes()), &decrypted, key, binding(), int64(len(payload))); e != nil || !bytes.Equal(decrypted.Bytes(), payload) {
		t.Fatalf("round trip: %v", e)
	}
	for _, kind := range []string{"wrong-key", "wrong-context", "truncate", "bit-flip", "trailing", "capacity", "reorder"} {
		t.Run(kind, func(t *testing.T) {
			data := append([]byte(nil), encrypted.Bytes()...)
			k := append([]byte(nil), key...)
			b := binding()
			limit := int64(len(payload))
			switch kind {
			case "wrong-key":
				k[0] ^= 1
			case "wrong-context":
				b.Attempt = "2"
			case "truncate":
				data = data[:len(data)-1]
			case "bit-flip":
				data[len(magic)+25] ^= 1
			case "trailing":
				data = append(data, 0)
			case "capacity":
				limit--
			case "reorder":
				size := 5 + 12 + chunkSize + 16
				a := append([]byte(nil), data[len(magic):len(magic)+size]...)
				copy(data[len(magic):], data[len(magic)+size:len(magic)+size*2])
				copy(data[len(magic)+size:], a)
			}
			if e := Open(ctx, bytes.NewReader(data), io.Discard, k, b, limit); e == nil {
				t.Fatal("invalid ciphertext accepted")
			}
		})
	}
}
func TestFailedProducerCannotSealAndCancellation(t *testing.T) {
	key := make([]byte, 32)
	var out bytes.Buffer
	e := Seal(context.Background(), &out, key, binding(), func(w io.Writer) error { w.Write([]byte("private")); return errors.New("producer failed") })
	if e == nil {
		t.Fatal("producer failure ignored")
	}
	if Open(context.Background(), bytes.NewReader(out.Bytes()), io.Discard, key, binding(), 100) == nil {
		t.Fatal("partial staging opened")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if Seal(ctx, io.Discard, key, binding(), func(io.Writer) error { return nil }) == nil {
		t.Fatal("cancellation ignored")
	}
}
