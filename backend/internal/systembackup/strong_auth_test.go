package systembackup

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func signProof(t *testing.T, key ed25519.PrivateKey, claims StrongAuthClaims) string {
	t.Helper()
	h, _ := json.Marshal(map[string]string{"alg": "EdDSA", "typ": "sb-mfa+jwt", "kid": "key-1"})
	p, _ := json.Marshal(claims)
	b := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(p)
	return b + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, []byte(b)))
}

func TestStrongAuthTransactionReplayAndRollback(t *testing.T) {
	ctx := context.Background()
	s := Store{DB: controlDB(t), InstallationID: "mfa-test"}
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	v := &StrongAuthVerifier{Audience: "mfa-test", MaxAgeSeconds: 300, NonceKey: make([]byte, 32), Issuers: []StrongAuthIssuer{{Issuer: "trusted-mfa", KeyVersion: "key-1", PublicKey: pub, Methods: []string{"totp"}}}}
	input := ConfigChange{Expected: 0, Config: testConfig(), ChangeReason: "initial setup"}
	input.Config.Profile = "cluster" // A sensitive change, never password-only authorization.
	hash, _ := RequestHash(input)
	var now time.Time
	if err := s.DB.QueryRow("SELECT UTC_TIMESTAMP(6)").Scan(&now); err != nil {
		t.Fatal(err)
	}
	claims := StrongAuthClaims{Issuer: "trusted-mfa", Audience: "mfa-test", Subject: "1", Session: "session-1", AuthTime: now.Unix(), Expires: now.Add(time.Minute).Unix(), Method: "totp", Nonce: "nonce-00000000000000000001", Endpoint: "PUT /config", RequestHash: hash, IdempotencyKey: "mfa-config-0001"}
	proof := signProof(t, key, claims)
	bindings := RoleBindings{1: "security_admin"}
	first, replay, err := s.SaveConfigAuthorized(ctx, 1, "mfa-config-0001", input, "session-1", proof, bindings, v)
	if err != nil || replay || first.Version != 1 {
		t.Fatal(first, replay, err)
	}
	// Response loss retries succeed without another proof, even after its expiry.
	if got, replay, err := s.SaveConfigAuthorized(ctx, 1, "mfa-config-0001", input, "session-1", "", bindings, v); err != nil || !replay || got.Version != 1 {
		t.Fatal(got, replay, err)
	}
	input.Expected = 1
	input.Config.Profile = "single-node"
	if _, _, err = s.SaveConfigAuthorized(ctx, 1, "mfa-config-0002", input, "session-1", proof, bindings, v); PublicError(err) != "strong_auth_replayed" {
		t.Fatal("consumed nonce was reused", err)
	}
	// A failed final ledger insert must roll back both configuration and nonce.
	if _, err = s.DB.Exec("ALTER TABLE system_backup_operations ADD CONSTRAINT fixture_mfa_failure CHECK(idempotency_key<>'mfa-blocked-0003')"); err != nil {
		t.Fatal(err)
	}
	claims.Nonce = "nonce-00000000000000000003"
	claims.IdempotencyKey = "mfa-blocked-0003"
	claims.RequestHash, _ = RequestHash(input)
	proof = signProof(t, key, claims)
	if _, _, err = s.SaveConfigAuthorized(ctx, 1, claims.IdempotencyKey, input, "session-1", proof, bindings, v); err == nil {
		t.Fatal("expected transaction failure")
	}
	var count int
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM system_backup_strong_auth_nonces").Scan(&count); err != nil || count != 1 {
		t.Fatal("nonce escaped rollback", count, err)
	}
	if got, err := s.Config(ctx); err != nil || got.Version != 1 {
		t.Fatal(got, err)
	}
	// Scope checks run before consuming a fresh proof. It remains usable for
	// the original request after a wrong session or role has been rejected.
	claims.IdempotencyKey = "mfa-config-0004"
	proof = signProof(t, key, claims)
	for _, tc := range []struct{ session, role, code string }{{"other-session", "security_admin", "strong_auth_invalid"}, {"session-1", "config_admin", "permission_denied"}} {
		if _, _, err = s.SaveConfigAuthorized(ctx, 1, claims.IdempotencyKey, input, tc.session, proof, RoleBindings{1: tc.role}, v); PublicError(err) != tc.code {
			t.Fatal(tc, err)
		}
	}
	if _, _, err = s.SaveConfigAuthorized(ctx, 1, claims.IdempotencyKey, input, "session-1", proof, bindings, v); err != nil {
		t.Fatal(err)
	}
	input.Expected = 2
	input.Config.RetentionSeconds = 86400
	input.ChangeReason = ""
	if _, _, err = s.SaveConfigAuthorized(ctx, 1, "mfa-config-0005", input, "session-1", "", bindings, v); PublicError(err) != "validation_failed" {
		t.Fatal("missing destructive change reason", err)
	}
}

func TestStrongAuthVerifierRejectsUntrustedAndMismatchedProofs(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	v := &StrongAuthVerifier{Audience: "installation", MaxAgeSeconds: 300, NonceKey: make([]byte, 32), Issuers: []StrongAuthIssuer{{Issuer: "trusted", KeyVersion: "key-1", PublicKey: pub, Methods: []string{"totp"}}}}
	now := time.Now().UTC()
	c := StrongAuthClaims{Issuer: "trusted", Audience: "installation", Subject: "1", Session: "session", AuthTime: now.Unix(), Expires: now.Add(time.Minute).Unix(), Method: "totp", Nonce: "nonce-00000000000000000000", Endpoint: "PUT /config", RequestHash: "hash", IdempotencyKey: "key"}
	if _, err := v.Verify(signProof(t, key, c), now); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*StrongAuthClaims){
		func(c *StrongAuthClaims) { c.Method = "password" },
		func(c *StrongAuthClaims) { c.Audience = "other" },
		func(c *StrongAuthClaims) { c.AuthTime = now.Add(-10 * time.Minute).Unix() },
		func(c *StrongAuthClaims) { c.Expires = now.Add(-time.Minute).Unix() },
		func(c *StrongAuthClaims) { c.Issuer = "untrusted" },
	} {
		bad := c
		change(&bad)
		if _, err := v.Verify(signProof(t, key, bad), now); err == nil {
			t.Fatal("accepted invalid proof", bad)
		}
	}
	// Duplicate JSON fields must not select a different principal at another verifier.
	h := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"EdDSA","alg":"none","typ":"sb-mfa+jwt","kid":"key-1"}`))
	p := base64.RawURLEncoding.EncodeToString([]byte(`{}`))
	raw := h + "." + p
	if _, err := v.Verify(raw+"."+base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, []byte(raw))), now); err == nil {
		t.Fatal("accepted duplicate header")
	}
}
