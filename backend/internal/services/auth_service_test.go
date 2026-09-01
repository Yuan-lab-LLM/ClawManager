package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"clawreef/internal/config"
	"clawreef/internal/models"
	"clawreef/internal/utils"
)

func TestLoginEnterpriseRejectsUnprovisionedLDAPUser(t *testing.T) {
	userRepo := newFakeUserRepo()
	auth := NewAuthService(userRepo, testJWTConfig(), fakeEnterpriseAuth{
		user: &EnterpriseUser{
			Provider:   AuthProviderLDAP,
			ExternalID: "uid=alice,ou=People,dc=example,dc=com",
			Username:   "alice",
			Email:      "alice@example.com",
			Role:       "admin",
		},
	})

	if _, err := auth.Login("alice", "secret"); err == nil || err.Error() != "invalid username or password" {
		t.Fatalf("Login error = %v, want invalid username or password", err)
	}

	user, err := userRepo.GetByUsername("alice")
	if err != nil {
		t.Fatalf("GetByUsername returned error: %v", err)
	}
	if user != nil {
		t.Fatalf("expected LDAP login not to auto-create user, got %#v", user)
	}
}

func TestLoginEnterpriseAllowsProvisionedLDAPUser(t *testing.T) {
	userRepo := newFakeUserRepo()
	externalID := "uid=alice,ou=People,dc=example,dc=com"
	if err := userRepo.Create(&models.User{
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: "external:ldap",
		Role:         "user",
		AuthProvider: AuthProviderLDAP,
		LoginAlias:   stringPtr("ldap_alice"),
		ExternalID:   &externalID,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	auth := NewAuthService(userRepo, testJWTConfig(), fakeEnterpriseAuth{
		user: &EnterpriseUser{
			Provider:   AuthProviderLDAP,
			ExternalID: "uid=alice,ou=People,dc=example,dc=com",
			Username:   "alice",
			Email:      "alice@example.com",
			Role:       "admin",
		},
	})

	tokenPair, err := auth.Login("ldap_alice", "secret")
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if tokenPair.AccessToken == "" || tokenPair.RefreshToken == "" {
		t.Fatalf("expected token pair to be issued")
	}

	user, err := userRepo.GetByUsername("alice")
	if err != nil {
		t.Fatalf("GetByUsername returned error: %v", err)
	}
	if user == nil {
		t.Fatalf("expected provisioned LDAP user to remain")
	}
	if got, want := user.AuthProvider, AuthProviderLDAP; got != want {
		t.Fatalf("auth provider = %q, want %q", got, want)
	}
	if got, want := user.Role, "user"; got != want {
		t.Fatalf("role = %q, want %q", got, want)
	}
	if user.ExternalID == nil || *user.ExternalID != "uid=alice,ou=People,dc=example,dc=com" {
		t.Fatalf("external id = %#v, want %q", user.ExternalID, "uid=alice,ou=People,dc=example,dc=com")
	}
	if user.LastLogin == nil {
		t.Fatalf("expected last login to be updated")
	}
}

func TestLoginEnterpriseRejectsDisabledProvisionedLDAPUser(t *testing.T) {
	userRepo := newFakeUserRepo()
	if err := userRepo.Create(&models.User{
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: "external:ldap",
		Role:         "user",
		AuthProvider: AuthProviderLDAP,
		LoginAlias:   stringPtr("ldap_alice"),
		IsActive:     false,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	auth := NewAuthService(userRepo, testJWTConfig(), fakeEnterpriseAuth{
		user: &EnterpriseUser{
			Provider:   AuthProviderLDAP,
			ExternalID: "uid=alice,ou=People,dc=example,dc=com",
			Username:   "alice",
			Email:      "alice@example.com",
			Role:       "user",
		},
	})

	if _, err := auth.Login("ldap_alice", "secret"); err == nil || err.Error() != "invalid username or password" {
		t.Fatalf("Login error = %v, want account is disabled", err)
	}
}

func TestLoginEnterpriseRejectsExternalIdentityConflict(t *testing.T) {
	userRepo := newFakeUserRepo()
	conflictingDN := "uid=alice,ou=People,dc=example,dc=com"
	if err := userRepo.Create(&models.User{
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: "external:ldap",
		Role:         "user",
		AuthProvider: AuthProviderLDAP,
		LoginAlias:   stringPtr("ldap_alice"),
		ExternalID:   &conflictingDN,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if err := userRepo.Create(&models.User{
		Username:     "mallory",
		Email:        "mallory@example.com",
		PasswordHash: "external:ldap",
		Role:         "user",
		AuthProvider: AuthProviderLDAP,
		LoginAlias:   stringPtr("ldap_alice"),
		ExternalID:   stringPtr("uid=alice,ou=People,dc=example,dc=com"),
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	auth := NewAuthService(userRepo, testJWTConfig(), fakeEnterpriseAuth{
		user: &EnterpriseUser{
			Provider:   AuthProviderLDAP,
			ExternalID: conflictingDN,
			Username:   "mallory",
			Email:      "mallory@example.com",
			Role:       "user",
		},
	})
	if _, err := auth.Login("mallory", "secret"); err == nil || err.Error() != "invalid username or password" {
		t.Fatalf("Login error = %v, want invalid username or password", err)
	}
}

func TestLoginEnterpriseSyncsRoleWhenEnabled(t *testing.T) {
	userRepo := newFakeUserRepo()
	quotaRepo := newQuotaRoleTestRepo()
	if err := userRepo.Create(&models.User{
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: "external:ldap",
		Role:         "user",
		AuthProvider: AuthProviderLDAP,
		LoginAlias:   stringPtr("ldap_alice"),
		ExternalID:   stringPtr("uid=alice,ou=People,dc=example,dc=com"),
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	auth := NewAuthService(
		userRepo,
		testJWTConfig(),
		fakeEnterpriseAuth{
			user: &EnterpriseUser{
				Provider:   AuthProviderLDAP,
				ExternalID: "uid=alice,ou=People,dc=example,dc=com",
				Username:   "alice",
				Email:      "alice@example.com",
				Role:       "admin",
			},
		},
		WithEnterpriseAuthPolicy(config.EnterpriseAuthConfig{AllowLocalFallback: true, SyncRole: true}),
		WithQuotaRepository(quotaRepo),
	)
	if _, err := auth.Login("ldap_alice", "secret"); err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	user, err := userRepo.GetByUsername("alice")
	if err != nil {
		t.Fatalf("GetByUsername returned error: %v", err)
	}
	if got, want := user.Role, "admin"; got != want {
		t.Fatalf("role = %q, want %q", got, want)
	}
	quota, err := quotaRepo.GetByUserID(user.ID)
	if err != nil {
		t.Fatalf("GetByUserID returned error: %v", err)
	}
	if !quota.IsDefaultForRole("admin") {
		t.Fatalf("quota after LDAP login = %#v, want admin defaults", quota)
	}
}

func TestLoginFallsBackToLocalUser(t *testing.T) {
	userRepo := newFakeUserRepo()
	passwordHash, err := utils.HashPassword("local-password")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if err := userRepo.Create(&models.User{
		Username:     "admin",
		Email:        "admin@example.com",
		PasswordHash: passwordHash,
		Role:         "admin",
		AuthProvider: AuthProviderLocal,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	auth := NewAuthService(userRepo, testJWTConfig(), fakeEnterpriseAuth{err: ErrEnterpriseUnavailable})
	tokenPair, err := auth.Login("admin", "local-password")
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if tokenPair.AccessToken == "" {
		t.Fatalf("expected local fallback token")
	}
}

func TestLoginWithoutPrefixIgnoresEnterpriseFallbackPolicy(t *testing.T) {
	userRepo := newFakeUserRepo()
	passwordHash, err := utils.HashPassword("local-password")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if err := userRepo.Create(&models.User{
		Username:     "admin",
		Email:        "admin@example.com",
		PasswordHash: passwordHash,
		Role:         "admin",
		AuthProvider: AuthProviderLocal,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	auth := NewAuthService(
		userRepo,
		testJWTConfig(),
		fakeEnterpriseAuth{err: ErrEnterpriseUnavailable},
		WithEnterpriseAuthPolicy(config.EnterpriseAuthConfig{AllowLocalFallback: false}),
	)
	if _, err := auth.Login("admin", "local-password"); err != nil {
		t.Fatalf("unqualified login should use local authentication: %v", err)
	}
}

func TestLoginFallsBackToLocalUserWhenLDAPUserNotFound(t *testing.T) {
	userRepo := newFakeUserRepo()
	passwordHash, err := utils.HashPassword("local-password")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if err := userRepo.Create(&models.User{
		Username:     "admin",
		Email:        "admin@example.com",
		PasswordHash: passwordHash,
		Role:         "admin",
		AuthProvider: AuthProviderLocal,
		ExternalID:   nil,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	auth := NewAuthService(userRepo, testJWTConfig(), fakeEnterpriseAuth{err: ErrEnterpriseUserNotFound})
	tokenPair, err := auth.Login("admin", "local-password")
	if err != nil {
		t.Fatalf("Login returned error: %v", err)
	}
	if tokenPair.AccessToken == "" {
		t.Fatalf("expected local fallback token")
	}
}

func TestLoginWithoutPrefixUsesLocalAuthentication(t *testing.T) {
	userRepo := newFakeUserRepo()
	passwordHash, err := utils.HashPassword("local-password")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if err := userRepo.Create(&models.User{
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: passwordHash,
		Role:         "user",
		AuthProvider: AuthProviderLocal,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	auth := NewAuthService(userRepo, testJWTConfig(), fakeEnterpriseAuth{err: ErrEnterpriseInvalidCredentials})
	if _, err := auth.Login("alice", "local-password"); err != nil {
		t.Fatalf("unqualified login should use local authentication: %v", err)
	}
}

func TestChangePasswordRejectsLDAPUser(t *testing.T) {
	userRepo := newFakeUserRepo()
	if err := userRepo.Create(&models.User{
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: "external:ldap",
		Role:         "user",
		AuthProvider: AuthProviderLDAP,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	auth := NewAuthService(userRepo, testJWTConfig(), nil)
	if err := auth.ChangePassword(1, "old", "new-password"); err == nil {
		t.Fatalf("expected LDAP password change to be rejected")
	}
}

func TestDeleteUserHardDeletesLDAPUser(t *testing.T) {
	userRepo := newFakeUserRepo()
	quotaRepo := &fakeQuotaRepo{}
	if err := userRepo.Create(&models.User{
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: "external:ldap",
		Role:         "user",
		AuthProvider: AuthProviderLDAP,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	users := NewUserService(userRepo, quotaRepo)
	if err := users.DeleteUser(1); err != nil {
		t.Fatalf("DeleteUser returned error: %v", err)
	}
	user, err := userRepo.GetByID(1)
	if err != nil {
		t.Fatalf("GetByID returned error: %v", err)
	}
	if user != nil {
		t.Fatalf("expected LDAP user to be deleted, got %#v", user)
	}
	if quotaRepo.deletedFor != 1 {
		t.Fatalf("quota deleted for user %d, want 1", quotaRepo.deletedFor)
	}
}

func TestUpdateUserCanDisableAndRestoreLDAPUser(t *testing.T) {
	userRepo := newFakeUserRepo()
	quotaRepo := &fakeQuotaRepo{}
	if err := userRepo.Create(&models.User{
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: "external:ldap",
		Role:         "user",
		AuthProvider: AuthProviderLDAP,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	users := NewUserService(userRepo, quotaRepo)
	if err := users.UpdateUser(&models.User{ID: 1, IsActive: false}); err != nil {
		t.Fatalf("UpdateUser disable returned error: %v", err)
	}
	user, err := userRepo.GetByID(1)
	if err != nil {
		t.Fatalf("GetByID returned error: %v", err)
	}
	if user == nil || user.IsActive {
		t.Fatalf("expected LDAP user to be disabled, got %#v", user)
	}
	if quotaRepo.deletedFor != 0 {
		t.Fatalf("quota deleted for user %d, want 0", quotaRepo.deletedFor)
	}

	if err := users.UpdateUser(&models.User{ID: 1, IsActive: true}); err != nil {
		t.Fatalf("UpdateUser restore returned error: %v", err)
	}
	user, err = userRepo.GetByID(1)
	if err != nil {
		t.Fatalf("GetByID returned error: %v", err)
	}
	if user == nil || !user.IsActive {
		t.Fatalf("expected LDAP user to be restored, got %#v", user)
	}
}

func TestDeleteUserHardDeletesLocalUser(t *testing.T) {
	userRepo := newFakeUserRepo()
	quotaRepo := &fakeQuotaRepo{}
	passwordHash, err := utils.HashPassword("local-password")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	if err := userRepo.Create(&models.User{
		Username:     "bob",
		Email:        "bob@example.com",
		PasswordHash: passwordHash,
		Role:         "user",
		AuthProvider: AuthProviderLocal,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	users := NewUserService(userRepo, quotaRepo)
	if err := users.DeleteUser(1); err != nil {
		t.Fatalf("DeleteUser returned error: %v", err)
	}
	user, err := userRepo.GetByID(1)
	if err != nil {
		t.Fatalf("GetByID returned error: %v", err)
	}
	if user != nil {
		t.Fatalf("expected local user to be deleted, got %#v", user)
	}
	if quotaRepo.deletedFor != 1 {
		t.Fatalf("quota deleted for user %d, want 1", quotaRepo.deletedFor)
	}
}

func testJWTConfig() config.JWTConfig {
	return config.JWTConfig{
		Secret:        "test-secret",
		AccessExpiry:  60,
		RefreshExpiry: 168,
	}
}

type fakeEnterpriseAuth struct {
	user *EnterpriseUser
	err  error
}

func (a fakeEnterpriseAuth) Authenticate(context.Context, string, string) (*EnterpriseUser, error) {
	if a.err != nil {
		return nil, a.err
	}
	return a.user, nil
}

type fakeUserRepo struct {
	nextID int
	users  map[int]*models.User
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{
		nextID: 1,
		users:  make(map[int]*models.User),
	}
}

func (r *fakeUserRepo) Create(user *models.User) error {
	clone := *user
	clone.ID = r.nextID
	r.nextID++
	r.users[clone.ID] = &clone
	user.ID = clone.ID
	return nil
}

func (r *fakeUserRepo) GetByID(id int) (*models.User, error) {
	user := r.users[id]
	if user == nil {
		return nil, nil
	}
	clone := *user
	return &clone, nil
}

func (r *fakeUserRepo) GetByUsername(username string) (*models.User, error) {
	for _, user := range r.users {
		if user.Username == username {
			clone := *user
			return &clone, nil
		}
	}
	return nil, nil
}

func (a fakeEnterpriseAuth) AuthenticateByIdentity(_ context.Context, externalID, _ string) (*EnterpriseUser, error) {
	if a.err != nil {
		return nil, a.err
	}
	if a.user == nil || a.user.ExternalID != externalID {
		return nil, ErrEnterpriseUserNotFound
	}
	return a.user, nil
}

func (r *fakeUserRepo) GetByAuthProviderUsername(authProvider, username string) (*models.User, error) {
	for _, user := range r.users {
		provider := user.AuthProvider
		if provider == "" { provider = AuthProviderLocal }
		if provider == authProvider && user.Username == username { clone := *user; return &clone, nil }
	}
	return nil, nil
}

func (r *fakeUserRepo) CountByAuthProviderUsername(authProvider, username string) (int, error) {
	count := 0
	for _, user := range r.users {
		provider := user.AuthProvider
		if provider == "" { provider = AuthProviderLocal }
		if provider == authProvider && user.Username == username { count++ }
	}
	return count, nil
}

func (r *fakeUserRepo) GetByLoginAlias(authProvider, loginAlias string) (*models.User, error) {
	for _, user := range r.users {
		if user.AuthProvider == authProvider && user.LoginAlias != nil && *user.LoginAlias == loginAlias { clone := *user; return &clone, nil }
	}
	return nil, nil
}

func (r *fakeUserRepo) GetByEmail(email string) (*models.User, error) {
	for _, user := range r.users {
		if user.Email == email {
			clone := *user
			return &clone, nil
		}
	}
	return nil, nil
}

func (r *fakeUserRepo) GetByExternalIdentity(authProvider, externalID string) (*models.User, error) {
	for _, user := range r.users {
		if user.AuthProvider == authProvider && user.ExternalID != nil && *user.ExternalID == externalID {
			clone := *user
			return &clone, nil
		}
	}
	return nil, nil
}

func (r *fakeUserRepo) Update(user *models.User) error {
	if _, ok := r.users[user.ID]; !ok {
		return errors.New("user not found")
	}
	clone := *user
	r.users[user.ID] = &clone
	return nil
}

func (r *fakeUserRepo) Delete(id int) error {
	delete(r.users, id)
	return nil
}

func (r *fakeUserRepo) List(offset, limit int) ([]models.User, error) {
	result := make([]models.User, 0, len(r.users))
	for _, user := range r.users {
		result = append(result, *user)
	}
	return result, nil
}

func (r *fakeUserRepo) Count() (int, error) {
	return len(r.users), nil
}

type fakeQuotaRepo struct {
	createdFor int
	deletedFor int
}

func (r *fakeQuotaRepo) Create(*models.UserQuota) error {
	return nil
}

func (r *fakeQuotaRepo) GetByUserID(int) (*models.UserQuota, error) {
	return nil, nil
}

func (r *fakeQuotaRepo) Update(*models.UserQuota) error {
	return nil
}

func (r *fakeQuotaRepo) DeleteByUserID(userID int) error {
	r.deletedFor = userID
	return nil
}

func (r *fakeQuotaRepo) CreateDefaultQuota(userID int) (*models.UserQuota, error) {
	r.createdFor = userID
	return &models.UserQuota{UserID: userID}, nil
}

