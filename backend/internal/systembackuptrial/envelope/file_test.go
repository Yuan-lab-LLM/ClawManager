package envelope

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
)

func TestSealedFileCompressionRoundTripAndNoOverwrite(t *testing.T) {
	root, e := os.OpenRoot(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	key := make([]byte, 32)
	input := bytes.Repeat([]byte("synthetic-source-private-data"), 10000)
	produce := func(w io.Writer) error {
		return Compress(w, func(z io.Writer) error { _, e := z.Write(input); return e })
	}
	r, e := SealFile(context.Background(), root, "part.enc", key, binding(), produce)
	if e != nil {
		t.Fatal(e)
	}
	stored, _ := root.ReadFile(r.Name)
	if bytes.Contains(stored, []byte("synthetic-source-private-data")) {
		t.Fatal("plaintext persisted")
	}
	f, e := OpenFile(context.Background(), root, r)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	var got bytes.Buffer
	if e = ConsumeCompressed(context.Background(), f, key, binding(), 1<<20, 1<<20, func(r io.Reader) error { _, e := io.Copy(&got, r); return e }); e != nil || !bytes.Equal(got.Bytes(), input) {
		t.Fatalf("round trip: %v", e)
	}
	if _, e = SealFile(context.Background(), root, "part.enc", key, binding(), produce); e == nil {
		t.Fatal("overwrote sealed artifact")
	}
	after, _ := root.ReadFile(r.Name)
	if !bytes.Equal(stored, after) {
		t.Fatal("existing object changed")
	}
	if _, e = root.Stat(".part.enc.partial"); !os.IsNotExist(e) {
		t.Fatal("owned partial not cleaned")
	}
	r.SHA256 = string(bytes.Repeat([]byte{'a'}, 64))
	if f, e = OpenFile(context.Background(), root, r); e == nil {
		f.Close()
		t.Fatal("wrong digest accepted")
	}
}
func TestPartialFailureCannotPublishOrRemoveForeignFile(t *testing.T) {
	root, e := os.OpenRoot(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	_, e = SealFile(context.Background(), root, "failed.enc", make([]byte, 32), binding(), func(w io.Writer) error { w.Write(bytes.Repeat([]byte("x"), 2<<20)); return errors.New("failed") })
	if e == nil {
		t.Fatal("failed producer published")
	}
	if _, e = root.Stat("failed.enc"); !os.IsNotExist(e) {
		t.Fatal("partial published")
	}
	root.WriteFile(".foreign.enc.partial", []byte("owned by previous attempt"), 0600)
	_, e = SealFile(context.Background(), root, "foreign.enc", make([]byte, 32), binding(), func(io.Writer) error { return nil })
	if e == nil {
		t.Fatal("adopted foreign partial")
	}
	b, _ := root.ReadFile(".foreign.enc.partial")
	if string(b) != "owned by previous attempt" {
		t.Fatal("foreign partial removed")
	}
}
func TestCompressedStreamRejectsUnconsumedAndOversizedContent(t *testing.T) {
	key := make([]byte, 32)
	var sealed bytes.Buffer
	if e := Seal(context.Background(), &sealed, key, binding(), func(w io.Writer) error {
		return Compress(w, func(z io.Writer) error { _, e := z.Write(bytes.Repeat([]byte{'x'}, 2<<20)); return e })
	}); e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"unconsumed", "oversized", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			data := sealed.Bytes()
			limit := int64(3 << 20)
			consumer := func(r io.Reader) error { _, e := io.Copy(io.Discard, r); return e }
			switch mode {
			case "unconsumed":
				consumer = func(io.Reader) error { return nil }
			case "oversized":
				limit = 1 << 20
			case "truncated":
				data = data[:len(data)-1]
			}
			if e := ConsumeCompressed(context.Background(), bytes.NewReader(data), key, binding(), 3<<20, limit, consumer); e == nil {
				t.Fatal("invalid compressed stream accepted")
			}
		})
	}
}
