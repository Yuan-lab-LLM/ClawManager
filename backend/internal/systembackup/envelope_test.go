package systembackup

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"testing"
)

func TestEnvelopeRoundTripAndAuthentication(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	plain := bytes.Repeat([]byte("partial-result:原始产物\x00"), 100000)
	var encrypted bytes.Buffer
	if err := Encrypt(context.Background(), &encrypted, bytes.NewReader(plain), key, []byte("backup-1/workspace")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Decrypt(context.Background(), &out, bytes.NewReader(encrypted.Bytes()), key, []byte("backup-1/workspace")); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), plain) {
		t.Fatal("data changed")
	}
	for _, tc := range []struct {
		name string
		data []byte
		aad  []byte
	}{
		{"wrong-context", encrypted.Bytes(), []byte("backup-2/workspace")},
		{"truncated", encrypted.Bytes()[:encrypted.Len()-1], []byte("backup-1/workspace")},
		{"trailing", append(append([]byte{}, encrypted.Bytes()...), 1), []byte("backup-1/workspace")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if Decrypt(context.Background(), io.Discard, bytes.NewReader(tc.data), key, tc.aad) == nil {
				t.Fatal("accepted unauthenticated stream")
			}
		})
	}
	corrupt := append([]byte{}, encrypted.Bytes()...)
	corrupt[len(corrupt)/2] ^= 1
	if Decrypt(context.Background(), io.Discard, bytes.NewReader(corrupt), key, []byte("backup-1/workspace")) == nil {
		t.Fatal("accepted corrupted frame")
	}
}

func TestEnvelopeEmptyAndCancellation(t *testing.T) {
	var buf bytes.Buffer
	key := make([]byte, 32)
	if err := Encrypt(context.Background(), &buf, bytes.NewReader(nil), key, nil); err != nil {
		t.Fatal(err)
	}
	if err := Decrypt(context.Background(), io.Discard, &buf, key, nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Encrypt(ctx, io.Discard, bytes.NewReader([]byte("data")), key, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if Encrypt(context.Background(), io.Discard, bytes.NewReader(nil), []byte("short"), nil) == nil {
		t.Fatal("invalid key accepted")
	}
}
