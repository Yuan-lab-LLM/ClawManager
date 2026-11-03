package services

import (
	"context"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/utils"
)

type enterpriseAuthFunc func(context.Context, string, string) (*EnterpriseUser, error)

func (f enterpriseAuthFunc) AuthenticateByIdentity(ctx context.Context, dn, password string) (*EnterpriseUser, error) {
	return f(ctx, dn, password)
}

func TestUsernameLoginUsesStoredDNAndIgnoresLegacyAlias(t *testing.T) {
	for _, alias := range []*string{nil, stringPtr("ldap_alice")} {
		repo := newFakeUserRepo()
		_ = repo.Create(&models.User{Username: "alice", AuthProvider: AuthProviderLDAP, LoginAlias: alias, IsActive: true, ExternalID: stringPtr("uid=alice,ou=People")})
		calls := 0
		auth := NewAuthService(repo, testJWTConfig(), enterpriseAuthFunc(func(_ context.Context, dn, password string) (*EnterpriseUser, error) {
			calls++
			if dn != "uid=alice,ou=People" || password != "secret" {
				t.Fatalf("unexpected credentials: dn=%s", dn)
			}
			return &EnterpriseUser{Provider: AuthProviderLDAP, ExternalID: dn}, nil
		}))
		if _, err := auth.Login("ALICE", "secret"); err != nil {
			t.Fatal(err)
		}
		if _, err := auth.Login("ldap_alice", "secret"); err == nil {
			t.Fatal("legacy alias must not resolve")
		}
		if calls != 1 {
			t.Fatalf("LDAP calls = %d, want 1", calls)
		}
	}
}

func TestLDAPLoginFailuresDoNotUseLocalPassword(t *testing.T) {
	hash, err := utils.HashPassword("local-password")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"wrong password", "unavailable", "disabled", "missing DN", "empty DN", "missing authenticator", "nil identity"} {
		t.Run(scenario, func(t *testing.T) {
			repo := newFakeUserRepo()
			user := &models.User{Username: "alice", PasswordHash: hash, AuthProvider: AuthProviderLDAP, IsActive: true, ExternalID: stringPtr("uid=alice,ou=People")}
			if scenario == "disabled" {
				user.IsActive = false
			}
			if scenario == "missing DN" {
				user.ExternalID = nil
			}
			if scenario == "empty DN" {
				user.ExternalID = stringPtr("   ")
			}
			_ = repo.Create(user)
			calls := 0
			var authenticator EnterpriseAuthenticator = enterpriseAuthFunc(func(_ context.Context, dn, password string) (*EnterpriseUser, error) {
				calls++
				if scenario == "nil identity" {
					return nil, nil
				}
				if scenario == "unavailable" {
					return nil, ErrEnterpriseUnavailable
				}
				return nil, ErrEnterpriseInvalidCredentials
			})
			if scenario == "missing authenticator" {
				authenticator = nil
			}
			if _, err := NewAuthService(repo, testJWTConfig(), authenticator).Login("alice", "local-password"); err == nil {
				t.Fatal("LDAP failure must not fall back to the local password")
			}
			if (scenario == "disabled" || scenario == "missing DN" || scenario == "empty DN") && calls != 0 {
				t.Fatal("invalid provisioned account must not contact LDAP")
			}
			if repo.users[user.ID].LastLogin != nil {
				t.Fatal("failed login updated last login")
			}
		})
	}
}

func TestUsernamePrefixDoesNotSelectProvider(t *testing.T) {
	for _, provider := range []string{AuthProviderLocal, AuthProviderLDAP} {
		t.Run(provider, func(t *testing.T) {
			repo := newFakeUserRepo()
			hash, _ := utils.HashPassword("secret")
			_ = repo.Create(&models.User{Username: "ldap_alice", PasswordHash: hash, AuthProvider: provider, IsActive: true, ExternalID: stringPtr("uid=ldap_alice,ou=People")})
			calls := 0
			auth := NewAuthService(repo, testJWTConfig(), enterpriseAuthFunc(func(_ context.Context, dn, password string) (*EnterpriseUser, error) {
				calls++
				return &EnterpriseUser{Provider: AuthProviderLDAP, ExternalID: dn}, nil
			}))
			if _, err := auth.Login("ldap_alice", "secret"); err != nil {
				t.Fatal(err)
			}
			if (provider == AuthProviderLocal && calls != 0) || (provider == AuthProviderLDAP && calls != 1) {
				t.Fatalf("unexpected LDAP calls: %d", calls)
			}
		})
	}
}
