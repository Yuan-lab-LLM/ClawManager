package workbench

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLookupPreviewCapabilityUsesExistingScopeAndExpiry(t *testing.T) {
	p, _ := NewPreviews("https://p-{token}.preview.test", &previewMemoryStore{values: map[string]PreviewLease{}}, time.Hour)
	lease, err := p.Create(context.Background(), PreviewScope{InstanceID: 42, UserID: 7, Generation: 3, GatewayID: "gw-42-3"}, "http://localhost:3000/a?q=one")
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.LookupCapability(context.Background(), lease.ID)
	if err != nil || got.Scope != lease.Scope || got.Target.URL() != lease.Target.URL() {
		t.Fatalf("wrong lease: %+v %v", got, err)
	}
	for _, id := range []string{"", lease.ID + "a", strings.Repeat("z", 48), strings.Repeat("a", 48)} {
		if _, err := p.LookupCapability(context.Background(), id); err == nil {
			t.Fatal("invalid or unissued capability accepted")
		}
	}
	p.now = func() time.Time { return lease.ExpiresAt }
	if _, err := p.LookupCapability(context.Background(), lease.ID); err == nil {
		t.Fatal("expired capability accepted")
	}
}
