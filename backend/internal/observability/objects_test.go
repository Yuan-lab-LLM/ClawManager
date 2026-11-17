package observability

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

type memoryObjects struct {
	data    map[string][]byte
	fail    bool
	corrupt bool
}

func (m *memoryObjects) PutObject(_ context.Context, k string, b []byte, _ string) error {
	if m.fail {
		return errors.New("object outage")
	}
	m.data[k] = append([]byte(nil), b...)
	return nil
}
func (m *memoryObjects) GetObject(_ context.Context, k string) ([]byte, error) {
	if m.fail {
		return nil, errors.New("object outage")
	}
	b, ok := m.data[k]
	if !ok {
		return nil, errors.New("missing")
	}
	b = append([]byte(nil), b...)
	if m.corrupt && len(b) > 0 {
		b[len(b)-1] ^= 1
	}
	return b, nil
}
func TestEncryptedObjectsOwnerDedupVerifiedAndRestart(t *testing.T) {
	ctx := context.Background()
	mem := &memoryObjects{data: map[string][]byte{}}
	key := bytes.Repeat([]byte{7}, 32)
	s, e := NewEncryptedObjects(mem, key)
	if e != nil {
		t.Fatal(e)
	}
	raw := []byte("private request and response")
	a, e := s.Put(ctx, "user:1", raw)
	if e != nil {
		t.Fatal(e)
	}
	same, e := s.Put(ctx, "user:1", raw)
	if e != nil || same != a {
		t.Fatalf("same owner did not deduplicate: %v", e)
	}
	b, e := s.Put(ctx, "user:2", raw)
	if e != nil || b.ObjectKey == a.ObjectKey {
		t.Fatalf("owners share identity: %v", e)
	}
	for _, v := range mem.data {
		if bytes.Contains(v, raw) {
			t.Fatal("plaintext object")
		}
	}
	restarted, _ := NewEncryptedObjects(mem, key)
	got, e := restarted.Get(ctx, "user:1", a)
	if e != nil || !bytes.Equal(got, raw) {
		t.Fatalf("restart: %v", e)
	}
	if _, e = s.Get(ctx, "user:2", a); e == nil {
		t.Fatal("cross-owner read")
	}
	mem.corrupt = true
	if _, e = s.Get(ctx, "user:1", a); e == nil {
		t.Fatal("corruption accepted")
	}
	if _, e = s.Put(ctx, "user:1", []byte("new body")); e == nil {
		t.Fatal("unverified upload accepted")
	}
	mem.fail = true
	if _, e = s.Put(ctx, "user:1", raw); e == nil {
		t.Fatal("outage accepted")
	}
}
