package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/services"

	"github.com/gin-gonic/gin"
)

func TestValidateImportedUserAllowsEmptyPassword(t *testing.T) {
	if err := validateImportedUser("alice", "alice@example.com", "", "user", "local", ""); err != "" {
		t.Fatalf("expected empty import password to be allowed, got %q", err)
	}
}

func TestValidateImportedUserRejectsShortExplicitPassword(t *testing.T) {
	if err := validateImportedUser("alice", "alice@example.com", "user123", "user", "local", ""); err != "Password must be at least 8 characters" {
		t.Fatalf("expected short explicit password error, got %q", err)
	}
}

func TestNormalizeImportedAuthProviderDefaultsToLocal(t *testing.T) {
	if got := normalizeImportedAuthProvider(""); got != "local" {
		t.Fatalf("provider = %q, want local", got)
	}
}

func TestValidateImportedUserAllowsLDAPProviderWithExternalID(t *testing.T) {
	if err := validateImportedUser("alice", "alice@example.com", "", "user", "ldap", "uid=alice,ou=users,dc=example,dc=com"); err != "" {
		t.Fatalf("expected CSV LDAP creation to be allowed, got %q", err)
	}
}

func TestImportWarningCodesIgnoreLDAPPassword(t *testing.T) {
	if got := importWarningCodes("ldap", "directory-password"); len(got) != 1 || got[0] != "ldap_password_ignored" {
		t.Fatalf("warning codes = %#v, want ldap_password_ignored", got)
	}
	if got := importWarningCodes("local", "local-password"); len(got) != 0 {
		t.Fatalf("local warning codes = %#v, want none", got)
	}
}

func TestQuotaForImportRoleUsesAdminDefaultsOnlyForOrdinaryDefaults(t *testing.T) {
	adminQuota := quotaForImportRole("admin", models.DefaultQuotaForRole("user"), false)
	if !adminQuota.IsDefaultForRole("admin") {
		t.Fatalf("admin import quota = %#v, want admin defaults", adminQuota)
	}

	custom := models.DefaultQuotaForRole("user")
	custom.MaxInstances = 11
	if got := quotaForImportRole("admin", custom, false); got.MaxInstances != 11 || got.IsDefaultForRole("admin") {
		t.Fatalf("custom admin import quota changed = %#v", got)
	}
	if got := quotaForImportRole("admin", custom, true); !got.IsDefaultForRole("admin") {
		t.Fatalf("LDAP-synced admin import quota = %#v, want admin defaults", got)
	}
}

func TestValidateImportedUserRequiresLDAPExternalID(t *testing.T) {
	if err := validateImportedUser("alice", "alice@example.com", "", "user", "ldap", ""); err != "External ID is required for LDAP users" {
		t.Fatalf("expected missing LDAP external ID error, got %q", err)
	}
}

func TestValidateImportedUserIgnoresLDAPPasswordValidation(t *testing.T) {
	if err := validateImportedUser("alice", "alice@example.com", "short", "user", "ldap", "uid=alice,ou=users,dc=example,dc=com"); err != "" {
		t.Fatalf("expected LDAP password to be ignored, got %q", err)
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "  ", "directory error"); got != "directory error" {
		t.Fatalf("firstNonEmpty = %q", got)
	}
	if got := firstNonEmpty("", " "); got != "unknown LDAP import error" {
		t.Fatalf("firstNonEmpty empty = %q", got)
	}
}

func TestValidateImportedUserRejectsInvalidProvider(t *testing.T) {
	if err := validateImportedUser("alice", "alice@example.com", "", "user", "saml", ""); err != "Auth Provider must be local or ldap" {
		t.Fatalf("expected invalid provider error, got %q", err)
	}
}

func TestValidateImportedUserRejectsUnderscoreInLocalUsername(t *testing.T) {
	if err := validateImportedUser("ldap_fsmith", "fsmith@example.com", "", "user", "local", ""); err != "Username must be alphanumeric" {
		t.Fatalf("expected alphanumeric username error, got %q", err)
	}
}

func TestImportLDAPUsersSyncRoleCreatesUsersFromDirectoryRoles(t *testing.T) {
	userSvc := newFakeLDAPImportUserService()
	quotaSvc := &fakeLDAPImportQuotaService{}
	directory := &fakeLDAPImportDirectory{
		syncRole: true,
		users: []services.LDAPDirectoryUser{
			{ExternalID: "uid=alice,ou=People,dc=example,dc=com", Username: "alice", Email: "alice@example.com", Role: "admin"},
			{ExternalID: "uid=bob,ou=People,dc=example,dc=com", Username: "bob", Email: "bob@example.com", Role: "user"},
		},
	}
	handler := NewUserHandler(userSvc, quotaSvc, directory)

	result := performLDAPImport(t, handler, LDAPImportRequest{
		Role:         "user",
		MaxInstances: 10,
		MaxCPUCores:  40,
		MaxMemoryGB:  100,
		MaxStorageGB: 500,
		MaxGPUCount:  2,
	})

	if result.Data.CreatedCount != 2 || result.Data.UpdatedCount != 0 || result.Data.SkippedCount != 0 || result.Data.FailedCount != 0 {
		t.Fatalf("response counts = %#v", result.Data)
	}
	if got, want := userSvc.createdByExternalID["uid=alice,ou=People,dc=example,dc=com"].Role, "admin"; got != want {
		t.Fatalf("alice role = %q, want %q", got, want)
	}
	if got, want := userSvc.createdByExternalID["uid=bob,ou=People,dc=example,dc=com"].Role, "user"; got != want {
		t.Fatalf("bob role = %q, want %q", got, want)
	}
	if len(quotaSvc.updates) != 2 {
		t.Fatalf("quota updates = %d, want 2", len(quotaSvc.updates))
	}
	if !quotaSvc.updates[0].IsDefaultForRole("admin") || !quotaSvc.updates[1].IsDefaultForRole("user") {
		t.Fatalf("LDAP import quotas = %#v, want admin then user defaults", quotaSvc.updates)
	}
}

func TestImportLDAPUsersSyncRoleUpdatesExistingUserRole(t *testing.T) {
	userSvc := newFakeLDAPImportUserService()
	externalID := "uid=alice,ou=People,dc=example,dc=com"
	userSvc.existingByExternalID[externalID] = &models.User{
		ID:           7,
		Username:     "alice",
		Email:        "alice@example.com",
		Role:         "user",
		AuthProvider: services.AuthProviderLDAP,
		LoginAlias:   stringPtrValueForTest("ldap_alice"),
		ExternalID:   stringPtrValueForTest(externalID),
		IsActive:     true,
	}
	handler := NewUserHandler(userSvc, &fakeLDAPImportQuotaService{}, &fakeLDAPImportDirectory{
		syncRole: true,
		users: []services.LDAPDirectoryUser{
			{ExternalID: externalID, Username: "alice", Email: "alice@example.com", Role: "admin"},
		},
	})

	result := performLDAPImport(t, handler, LDAPImportRequest{
		Role:         "user",
		MaxInstances: 10,
		MaxCPUCores:  40,
		MaxMemoryGB:  100,
		MaxStorageGB: 500,
	})

	if result.Data.CreatedCount != 0 || result.Data.UpdatedCount != 1 || result.Data.SkippedCount != 0 || result.Data.FailedCount != 0 {
		t.Fatalf("response counts = %#v", result.Data)
	}
	if got, want := userSvc.existingByExternalID[externalID].Role, "admin"; got != want {
		t.Fatalf("existing role = %q, want %q", got, want)
	}
	if len(result.Data.UpdatedUsers) != 1 || result.Data.UpdatedUsers[0].Role != "admin" {
		t.Fatalf("updated users = %#v, want alice admin", result.Data.UpdatedUsers)
	}
}

func TestImportLDAPUsersWithoutSyncRoleUsesRequestRole(t *testing.T) {
	userSvc := newFakeLDAPImportUserService()
	handler := NewUserHandler(userSvc, &fakeLDAPImportQuotaService{}, &fakeLDAPImportDirectory{
		syncRole: false,
		users: []services.LDAPDirectoryUser{
			{ExternalID: "uid=alice,ou=People,dc=example,dc=com", Username: "alice", Email: "alice@example.com", Role: "user"},
		},
	})

	result := performLDAPImport(t, handler, LDAPImportRequest{
		Role:         "admin",
		MaxInstances: 10,
		MaxCPUCores:  40,
		MaxMemoryGB:  100,
		MaxStorageGB: 500,
	})

	if result.Data.CreatedCount != 1 || result.Data.UpdatedCount != 0 || result.Data.FailedCount != 0 {
		t.Fatalf("response counts = %#v", result.Data)
	}
	if got, want := userSvc.createdByExternalID["uid=alice,ou=People,dc=example,dc=com"].Role, "admin"; got != want {
		t.Fatalf("created role = %q, want %q", got, want)
	}
}

func TestImportLDAPUsersOnlyImportsSelectedExternalIDs(t *testing.T) {
	userSvc := newFakeLDAPImportUserService()
	directory := &fakeLDAPImportDirectory{
		users: []services.LDAPDirectoryUser{
			{ExternalID: "uid=alice,ou=People,dc=example,dc=com", Username: "alice", Email: "alice@example.com", Role: "user"},
			{ExternalID: "uid=bob,ou=People,dc=example,dc=com", Username: "bob", Email: "bob@example.com", Role: "user"},
		},
	}
	handler := NewUserHandler(userSvc, &fakeLDAPImportQuotaService{}, directory)

	result := performLDAPImport(t, handler, LDAPImportRequest{
		Role:         "user",
		MaxInstances: 10,
		MaxCPUCores:  40,
		MaxMemoryGB:  100,
		MaxStorageGB: 500,
		ExternalIDs:  []string{"uid=bob,ou=People,dc=example,dc=com"},
	})

	if result.Data.CreatedCount != 1 || result.Data.FailedCount != 0 {
		t.Fatalf("response counts = %#v", result.Data)
	}
	if _, ok := userSvc.createdByExternalID["uid=alice,ou=People,dc=example,dc=com"]; ok {
		t.Fatalf("alice should not have been imported")
	}
	if _, ok := userSvc.createdByExternalID["uid=bob,ou=People,dc=example,dc=com"]; !ok {
		t.Fatalf("bob should have been imported")
	}
	if len(directory.options) != 1 || directory.options[0].Query != "" || directory.options[0].Limit != 0 {
		t.Fatalf("directory options = %#v, want unlimited lookup for selected IDs", directory.options)
	}
}

func TestLDAPListOptionsFromRequest(t *testing.T) {
	got := ldapListOptionsFromRequest(" alice ", "25")
	if got.Query != "alice" || got.Limit != 25 {
		t.Fatalf("options = %#v, want query alice limit 25", got)
	}
}

type ldapImportTestResponse struct {
	Data struct {
		CreatedCount int               `json:"created_count"`
		UpdatedCount int               `json:"updated_count"`
		SkippedCount int               `json:"skipped_count"`
		FailedCount  int               `json:"failed_count"`
		UpdatedUsers []updatedLDAPUser `json:"updated_users"`
	} `json:"data"`
}

func performLDAPImport(t *testing.T, handler *UserHandler, req LDAPImportRequest) ldapImportTestResponse {
	t.Helper()
	gin.SetMode(gin.TestMode)
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/users/import/ldap", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.ImportLDAPUsers(c)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var result ldapImportTestResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return result
}

type fakeLDAPImportDirectory struct {
	users    []services.LDAPDirectoryUser
	syncRole bool
	options  []services.LDAPListOptions
}

func (d *fakeLDAPImportDirectory) ListUsers(_ context.Context, options ...services.LDAPListOptions) ([]services.LDAPDirectoryUser, error) {
	if len(options) > 0 {
		d.options = append(d.options, options[0])
	}
	return d.users, nil
}

func (d *fakeLDAPImportDirectory) EnterpriseAuthPolicy() services.EnterpriseAuthPolicy {
	return services.EnterpriseAuthPolicy{SyncRole: d.syncRole}
}

type fakeLDAPImportUserService struct {
	nextID              int
	existingByExternalID map[string]*models.User
	createdByExternalID  map[string]*models.User
}

func newFakeLDAPImportUserService() *fakeLDAPImportUserService {
	return &fakeLDAPImportUserService{
		nextID:               1,
		existingByExternalID: make(map[string]*models.User),
		createdByExternalID:  make(map[string]*models.User),
	}
}

func (s *fakeLDAPImportUserService) CreateUser(username, email, password, role string) (*models.User, error) {
	return s.CreateUserWithProvider(username, email, password, role, services.AuthProviderLocal)
}

func (s *fakeLDAPImportUserService) CreateUserWithProvider(username, email, password, role, authProvider string) (*models.User, error) {
	return s.CreateUserWithProviderAndExternalID(username, email, password, role, authProvider, "")
}

func (s *fakeLDAPImportUserService) CreateUserWithProviderAndExternalID(username, email, _ string, role, authProvider, externalID string) (*models.User, error) {
	if _, ok := s.existingByExternalID[externalID]; ok {
		return nil, errors.New("user already exists")
	}
	user := &models.User{
		ID:           s.nextID,
		Username:     username,
		Email:        email,
		Role:         role,
		AuthProvider: authProvider,
		LoginAlias:   stringPtrValueForTest("ldap_" + username),
		ExternalID:   stringPtrValueForTest(externalID),
		IsActive:     true,
	}
	s.nextID++
	s.createdByExternalID[externalID] = user
	return user, nil
}

func (s *fakeLDAPImportUserService) GetUserByID(id int) (*models.User, error) {
	for _, user := range s.existingByExternalID {
		if user.ID == id {
			return user, nil
		}
	}
	for _, user := range s.createdByExternalID {
		if user.ID == id {
			return user, nil
		}
	}
	return nil, errors.New("user not found")
}

func (s *fakeLDAPImportUserService) GetUserByUsername(username string) (*models.User, error) {
	return nil, errors.New("user not found")
}

func (s *fakeLDAPImportUserService) GetUserByLoginAlias(authProvider, loginAlias string) (*models.User, error) {
	return nil, errors.New("user not found")
}

func (s *fakeLDAPImportUserService) GetUserByExternalIdentity(_ string, externalID string) (*models.User, error) {
	if user, ok := s.existingByExternalID[externalID]; ok {
		return user, nil
	}
	if user, ok := s.createdByExternalID[externalID]; ok {
		return user, nil
	}
	return nil, errors.New("user not found")
}

func (s *fakeLDAPImportUserService) EnsureLDAPLoginAlias(externalID string) (*models.User, error) {
	user, ok := s.existingByExternalID[externalID]
	if !ok {
		return nil, errors.New("user not found")
	}
	if user.LoginAlias == nil {
		user.LoginAlias = stringPtrValueForTest("ldap_" + user.Username)
	}
	return user, nil
}

func (s *fakeLDAPImportUserService) GetUserByEmail(email string) (*models.User, error) {
	for _, user := range s.existingByExternalID {
		if user.Email == email {
			return user, nil
		}
	}
	for _, user := range s.createdByExternalID {
		if user.Email == email {
			return user, nil
		}
	}
	return nil, errors.New("user not found")
}

func (s *fakeLDAPImportUserService) ListUsers(offset, limit int) ([]models.User, error) {
	return nil, nil
}

func (s *fakeLDAPImportUserService) CountUsers() (int, error) {
	return 0, nil
}

func (s *fakeLDAPImportUserService) UpdateUser(user *models.User) error {
	return nil
}

func (s *fakeLDAPImportUserService) DeleteUser(id int) error {
	return nil
}

func (s *fakeLDAPImportUserService) UpdateUserRole(id int, role string) error {
	user, err := s.GetUserByID(id)
	if err != nil {
		return err
	}
	user.Role = role
	return nil
}

func (s *fakeLDAPImportUserService) CreateDefaultQuota(userID int) error {
	return nil
}

type fakeLDAPImportQuotaService struct {
	updates []models.UserQuota
}

func (s *fakeLDAPImportQuotaService) GetUserQuota(userID int) (*models.UserQuota, error) {
	return nil, nil
}

func (s *fakeLDAPImportQuotaService) UpdateUserQuota(userID int, quota *models.UserQuota) error {
	updated := *quota
	updated.UserID = userID
	s.updates = append(s.updates, updated)
	return nil
}

func (s *fakeLDAPImportQuotaService) CreateDefaultQuota(userID int) (*models.UserQuota, error) {
	return &models.UserQuota{UserID: userID}, nil
}

func (s *fakeLDAPImportQuotaService) CheckUserQuota(userID int, requiredCPU float64, requiredMemory, requiredStorage int) error {
	return nil
}

func stringPtrValueForTest(value string) *string {
	return &value
}
