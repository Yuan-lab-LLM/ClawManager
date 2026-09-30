package services

import (
	"errors"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

func TestUsernamesAreUniqueAcrossProviders(t *testing.T) {
	for _, existingProvider := range []string{AuthProviderLocal, AuthProviderLDAP} {
		for _, newProvider := range []string{AuthProviderLocal, AuthProviderLDAP} {
			for _, username := range []string{"alice", "ALICE"} {
				t.Run(existingProvider+"/"+newProvider+"/"+username, func(t *testing.T) {
					repo := newFakeUserRepo()
					_ = repo.Create(&models.User{Username: "alice", Email: "same@example.com", AuthProvider: existingProvider, ExternalID: stringPtr("uid=alice,ou=one"), IsActive: false})
					quotas := &fakeQuotaRepo{}
					service := NewUserService(repo, quotas)
					_, err := service.CreateUserWithProviderAndExternalID(username, "same@example.com", "password", "user", newProvider, "uid=alice,ou=two")
					if err == nil || err.Error() != "username already exists" {
						t.Fatalf("expected username conflict before email conflict, got %v", err)
					}
					if len(repo.users) != 1 || quotas.createdFor != 0 {
						t.Fatal("conflicting import must not create an account or quota")
					}
				})
			}
		}
	}
}

func TestLDAPImportDoesNotAllocateAlias(t *testing.T) {
	repo := newFakeUserRepo()
	service := NewUserService(repo, &fakeQuotaRepo{})
	user, err := service.CreateUserWithProviderAndExternalID("alice", "alice@example.com", "", "user", AuthProviderLDAP, "uid=alice,ou=People")
	if err != nil {
		t.Fatal(err)
	}
	if user.Username != "alice" || user.LoginAlias != nil || user.ExternalID == nil || *user.ExternalID != "uid=alice,ou=People" {
		t.Fatalf("unexpected imported account: %#v", user)
	}
}

func TestUsernameConflictAfterConcurrentInsert(t *testing.T) {
	for _, provider := range []string{AuthProviderLocal, AuthProviderLDAP} {
		t.Run(provider, func(t *testing.T) {
			repo := &usernameRaceRepo{fakeUserRepo: newFakeUserRepo()}
			quotas := &fakeQuotaRepo{}
			_, err := NewUserService(repo, quotas).CreateUserWithProviderAndExternalID("alice", "alice@example.com", "password", "user", provider, "uid=alice,ou=People")
			if err == nil || err.Error() != "username already exists" || repo.attempts != 1 || quotas.createdFor != 0 {
				t.Fatalf("race result: err=%v attempts=%d quota=%d", err, repo.attempts, quotas.createdFor)
			}
		})
	}
}

func TestRegistrationRejectsLDAPUsername(t *testing.T) {
	repo := newFakeUserRepo()
	_ = repo.Create(&models.User{Username: "alice", Email: "same@example.com", AuthProvider: AuthProviderLDAP})
	_, err := NewAuthService(repo, testJWTConfig(), nil).Register("ALICE", "same@example.com", "password")
	if err == nil || err.Error() != "username already exists" {
		t.Fatalf("registration error: %v", err)
	}
}

func TestRegistrationMapsConcurrentUsernameConflict(t *testing.T) {
	repo := &usernameRaceRepo{fakeUserRepo: newFakeUserRepo()}
	_, err := NewAuthService(repo, testJWTConfig(), nil).Register("alice", "alice@example.com", "password")
	if err == nil || err.Error() != "username already exists" {
		t.Fatalf("registration race: %v", err)
	}
}

func TestUserServiceNoLongerReservesLDAPPrefix(t *testing.T) {
	// API format validation is unchanged; the service has no provider-prefix rule.
	_, err := NewUserService(newFakeUserRepo(), &fakeQuotaRepo{}).CreateUser("ldap_alice", "alice@example.com", "password", "user")
	if err != nil {
		t.Fatal(err)
	}
}

type usernameRaceRepo struct {
	*fakeUserRepo
	attempts int
}

func (r *usernameRaceRepo) Create(user *models.User) error {
	r.attempts++
	return errors.Join(repository.ErrUserUsernameConflict, errors.New("duplicate entry for uk_users_username"))
}
