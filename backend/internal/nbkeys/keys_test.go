package nbkeys

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCredentialAndEffectiveScopes(t *testing.T) {
	key, raw, err := New(12, "integration", []string{"pro-instances:read", "pro-instances:restart"}, time.Now().Add(time.Hour), 1)
	if err != nil {
		t.Fatal(err)
	}
	uid, id, err := Parse(raw)
	if err != nil || uid != 12 || id != key.ID || !key.Verify(raw) {
		t.Fatal("credential did not round trip")
	}
	if key.Verify(raw+"x") || key.Verify(strings.Replace(raw, ".12.", ".13.", 1)) {
		t.Fatal("accepted modified key")
	}
	got := key.EffectiveScopes([]string{"pro-instances:read"}, time.Now())
	if len(got) != 1 || got[0] != "pro-instances:read" {
		t.Fatal(got)
	}
	key.Enabled = false
	if len(key.EffectiveScopes([]string{"pro-instances:read"}, time.Now())) != 0 {
		t.Fatal("disabled key allowed")
	}
	key.Enabled = true
	key.RevokedAt = ptr(time.Now())
	if len(key.EffectiveScopes([]string{"pro-instances:read"}, time.Now())) != 0 {
		t.Fatal("revoked key allowed")
	}
	safe, _ := json.Marshal(key.Public(nil, time.Now()))
	if strings.Contains(string(safe), key.SecretHash) || strings.Contains(string(safe), raw) {
		t.Fatal("secret leaked")
	}
}

func ptr(t time.Time) *time.Time { return &t }

func TestInvalidFormats(t *testing.T) {
	for _, raw := range []string{"", "nbk1.0.a.b", "nbk1.-1.a.b", strings.Repeat("x", 513)} {
		if _, _, err := Parse(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}
