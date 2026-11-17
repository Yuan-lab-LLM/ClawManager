package historymaintenance

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestArchiveRequiresReadbackAndIndependentRestoreProof(t *testing.T) {
	store := &memoryArchive{objects: map[string][]byte{}}
	svc := New(nil, store, nil)
	rec := Record{Kind: "model_invocations", ID: 7, OwnerID: 12, CreatedAt: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), Payload: []byte(`{"id":7,"status":"completed"}`)}
	store.corrupt = true
	if _, err := svc.verifyArchive(context.Background(), rec); err == nil {
		t.Fatal("corrupt readback accepted")
	}
	store.corrupt = false
	sealed, err := svc.verifyArchive(context.Background(), rec)
	if err != nil {
		t.Fatal(err)
	}
	if sealed.Removable() {
		t.Fatal("readback alone permits source removal")
	}
	sealed.RestoreVerifiedAt = time.Now()
	sealed.RestoreSHA256 = sealed.SHA256
	sealed.RestoreReceipt = "isolated-restore:test"
	sealed.SummaryVerifiedAt = time.Now()
	sealed.SummarySHA256 = sealed.SHA256
	if !sealed.Removable() {
		t.Fatal("verified independent restore should permit removal")
	}
	sealed.RestoreSHA256 = "wrong"
	if sealed.Removable() {
		t.Fatal("mismatching restore digest accepted")
	}
}
func TestMonthUsesUTCAndOnlySupportedKinds(t *testing.T) {
	ts := time.Date(2026, 12, 1, 1, 0, 0, 0, time.FixedZone("CST", 8*3600))
	name, err := MonthlyTable("audit_events", ts)
	if err != nil || name != "audit_events_202611" {
		t.Fatalf("%s %v", name, err)
	}
	if _, err := MonthlyTable("gateway_key_budget_ledger", ts); err == nil {
		t.Fatal("budget ledger admitted")
	}
}

type memoryArchive struct {
	objects map[string][]byte
	corrupt bool
}

func (s *memoryArchive) Put(_ context.Context, owner, key string, raw []byte) ([]byte, error) {
	s.objects[owner+key] = append([]byte(nil), raw...)
	return json.Marshal(owner + key)
}
func (s *memoryArchive) Read(_ context.Context, owner string, ref []byte) ([]byte, error) {
	var key string
	if err := json.Unmarshal(ref, &key); err != nil {
		return nil, err
	}
	raw := append([]byte(nil), s.objects[key]...)
	if s.corrupt {
		raw = append(raw, '!')
	}
	return raw, nil
}
