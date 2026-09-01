package services

import (
	"errors"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

func TestLDAPLoginAliasesAllowSameUIDAcrossOUs(t *testing.T) {
	repo := newFakeUserRepo()
	service := NewUserService(repo, &fakeQuotaRepo{})

	local, err := service.CreateUserWithProvider("fsmith", "local@example.com", "local-password", "user", AuthProviderLocal)
	if err != nil { t.Fatalf("create local user: %v", err) }
	employees, err := service.CreateUserWithProviderAndExternalID("fsmith", "employees@example.com", "", "user", AuthProviderLDAP, "uid=fsmith,ou=employees,dc=foobar,dc=com")
	if err != nil { t.Fatalf("create employees LDAP user: %v", err) }
	contractors, err := service.CreateUserWithProviderAndExternalID("fsmith", "contractors@example.com", "", "user", AuthProviderLDAP, "uid=fsmith,ou=contractors,dc=foobar,dc=com")
	if err != nil { t.Fatalf("create contractors LDAP user: %v", err) }

	if local.LoginAlias != nil { t.Fatalf("local user must not receive an LDAP alias") }
	if got, want := *employees.LoginAlias, "ldap_fsmith"; got != want { t.Fatalf("employees alias = %q, want %q", got, want) }
	if got, want := *contractors.LoginAlias, "ldap_fsmith_contractors"; got != want { t.Fatalf("contractors alias = %q, want %q", got, want) }
}

func TestLDAPLoginAliasAddsSequenceOnOUCollision(t *testing.T) {
	repo := newFakeUserRepo()
	service := NewUserService(repo, &fakeQuotaRepo{})
	first, err := service.CreateUserWithProviderAndExternalID("fsmith", "one@example.com", "", "user", AuthProviderLDAP, "uid=fsmith,ou=employees,dc=example,dc=com")
	if err != nil { t.Fatalf("create first LDAP user: %v", err) }
	second, err := service.CreateUserWithProviderAndExternalID("fsmith", "two@example.com", "", "user", AuthProviderLDAP, "uid=fsmith,ou=employees,dc=other,dc=com")
	if err != nil { t.Fatalf("create second LDAP user: %v", err) }
	if got, want := *first.LoginAlias, "ldap_fsmith"; got != want { t.Fatalf("first alias = %q, want %q", got, want) }
	if got, want := *second.LoginAlias, "ldap_fsmith_employees"; got != want { t.Fatalf("second alias = %q, want %q", got, want) }
}

func TestLDAPLoginAliasRetriesAfterConcurrentUniqueConflict(t *testing.T) {
	repo := &aliasRaceRepo{fakeUserRepo: newFakeUserRepo()}
	service := NewUserService(repo, &fakeQuotaRepo{})

	user, err := service.CreateUserWithProviderAndExternalID("fsmith", "two@example.com", "", "user", AuthProviderLDAP, "uid=fsmith,ou=contractors,dc=example,dc=com")
	if err != nil {
		t.Fatalf("create LDAP user after alias race: %v", err)
	}
	if got, want := *user.LoginAlias, "ldap_fsmith_contractors"; got != want {
		t.Fatalf("alias after retry = %q, want %q", got, want)
	}
	if got := len(repo.users); got != 2 {
		t.Fatalf("user count after simulated race = %d, want 2", got)
	}
}

func TestLocalUsernameCannotUseLDAPPrefix(t *testing.T) {
	service := NewUserService(newFakeUserRepo(), &fakeQuotaRepo{})
	if _, err := service.CreateUserWithProvider("ldap_fsmith", "local@example.com", "password", "user", AuthProviderLocal); err == nil || err.Error() != "local usernames cannot start with ldap_" {
		t.Fatalf("reserved local username error = %v", err)
	}
}

func TestLocalUsernameUniqueConflictReturnsExistingUsernameError(t *testing.T) {
	service := NewUserService(&localUsernameRaceRepo{fakeUserRepo: newFakeUserRepo()}, &fakeQuotaRepo{})
	if _, err := service.CreateUserWithProvider("alice", "alice@example.com", "password", "user", AuthProviderLocal); err == nil || err.Error() != "username already exists" {
		t.Fatalf("local username conflict error = %v", err)
	}
}

func TestEnsureLDAPLoginAliasAddsOUForLegacyDuplicateUIDs(t *testing.T) {
	repo := newFakeUserRepo()
	service := NewUserService(repo, &fakeQuotaRepo{})
	firstDN := "uid=fsmith,ou=employees,dc=example,dc=com"
	secondDN := "uid=fsmith,ou=contractors,dc=example,dc=com"
	for index, externalID := range []string{firstDN, secondDN} {
		if err := repo.Create(&models.User{
			Username: "fsmith", Email: string(rune('a'+index)) + "@example.com", PasswordHash: "external:ldap",
			Role: "user", AuthProvider: AuthProviderLDAP, ExternalID: &externalID, IsActive: true,
		}); err != nil {
			t.Fatalf("create legacy LDAP user: %v", err)
		}
	}
	first, err := service.EnsureLDAPLoginAlias(firstDN)
	if err != nil { t.Fatalf("ensure first alias: %v", err) }
	second, err := service.EnsureLDAPLoginAlias(secondDN)
	if err != nil { t.Fatalf("ensure second alias: %v", err) }
	if got, want := *first.LoginAlias, "ldap_fsmith_employees"; got != want { t.Fatalf("first alias = %q, want %q", got, want) }
	if got, want := *second.LoginAlias, "ldap_fsmith_contractors"; got != want { t.Fatalf("second alias = %q, want %q", got, want) }
}

type aliasRaceRepo struct {
	*fakeUserRepo
	claimed bool
}

func (r *aliasRaceRepo) Create(user *models.User) error {
	if !r.claimed {
		r.claimed = true
		winner := *user
		winner.Email = "winner@example.com"
		winner.ExternalID = stringPtr("uid=fsmith,ou=employees,dc=example,dc=com")
		if err := r.fakeUserRepo.Create(&winner); err != nil {
			return err
		}
		return errors.Join(repository.ErrUserLoginAliasConflict, errors.New("duplicate entry for uk_users_provider_login_alias"))
	}
	return r.fakeUserRepo.Create(user)
}

type localUsernameRaceRepo struct {
	*fakeUserRepo
}

func (r *localUsernameRaceRepo) Create(user *models.User) error {
	return errors.Join(repository.ErrUserUsernameConflict, errors.New("duplicate entry for uk_users_local_username"))
}
