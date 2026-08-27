package handlers

import "testing"

func TestValidateImportedUserAllowsEmptyPassword(t *testing.T) {
	if err := validateImportedUser("alice", "alice@example.com", "", "user", "local"); err != "" {
		t.Fatalf("expected empty import password to be allowed, got %q", err)
	}
}

func TestValidateImportedUserRejectsShortExplicitPassword(t *testing.T) {
	if err := validateImportedUser("alice", "alice@example.com", "user123", "user", "local"); err != "Password must be at least 8 characters" {
		t.Fatalf("expected short explicit password error, got %q", err)
	}
}

func TestNormalizeImportedAuthProviderDefaultsToLocal(t *testing.T) {
	if got := normalizeImportedAuthProvider(""); got != "local" {
		t.Fatalf("provider = %q, want local", got)
	}
}

func TestValidateImportedUserAcceptsLDAPProvider(t *testing.T) {
	if err := validateImportedUser("alice", "alice@example.com", "", "user", "ldap"); err != "" {
		t.Fatalf("expected LDAP import to be valid, got %q", err)
	}
}

func TestValidateImportedUserRejectsInvalidProvider(t *testing.T) {
	if err := validateImportedUser("alice", "alice@example.com", "", "user", "saml"); err != "Auth Provider must be local or ldap" {
		t.Fatalf("expected invalid provider error, got %q", err)
	}
}
