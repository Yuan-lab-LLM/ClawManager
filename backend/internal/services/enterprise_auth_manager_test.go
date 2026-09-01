package services

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"clawreef/internal/config"
	"clawreef/internal/models"
	"clawreef/internal/repository"
)

func TestEnterpriseAuthManagerDisabledUpdateSavesWithoutLDAPProbe(t *testing.T) {
	repo := &fakeEnterpriseAuthSettingRepo{}
	manager, err := NewEnterpriseAuthManager(repo, config.EnterpriseAuthConfig{})
	if err != nil {
		t.Fatalf("NewEnterpriseAuthManager returned error: %v", err)
	}

	response, err := manager.UpdateConfig(context.Background(), EnterpriseAuthConfigUpdateRequest{
		ExpectedVersion:    0,
		Enabled:            false,
		AllowLocalFallback:  true,
		SyncRole:            true,
		LDAP:                LDAPConfigUpdate{LDAPConfigPublic: LDAPConfigPublic{Host: "", BaseDN: ""}},
		ClearBindPassword:   true,
	}, nil)
	if err != nil {
		t.Fatalf("UpdateConfig returned error: %v", err)
	}

	if response.Enabled {
		t.Fatalf("expected LDAP to remain disabled")
	}
	if response.Version != 1 {
		t.Fatalf("version = %d, want 1", response.Version)
	}
	if !manager.EnterpriseAuthPolicy().SyncRole {
		t.Fatalf("expected runtime policy to use saved sync_role")
	}
}

func TestEnterpriseAuthManagerEncryptsAndRedactsBindPassword(t *testing.T) {
	t.Setenv("AUTH_CONFIG_ENCRYPTION_KEY", "01234567890123456789012345678901")
	repo := &fakeEnterpriseAuthSettingRepo{}
	manager, err := NewEnterpriseAuthManager(repo, config.EnterpriseAuthConfig{})
	if err != nil {
		t.Fatalf("NewEnterpriseAuthManager returned error: %v", err)
	}

	response, err := manager.UpdateConfig(context.Background(), EnterpriseAuthConfigUpdateRequest{
		ExpectedVersion:   0,
		Enabled:           false,
		AllowLocalFallback: true,
		LDAP: LDAPConfigUpdate{LDAPConfigPublic: LDAPConfigPublic{
			Host:          "ldap.example.com",
			BaseDN:        "dc=example,dc=com",
			UserFilter:    "(&(objectClass=person)(uid=%s))",
			GroupFilter:   "(member=%s)",
			TLSCAFile:     "/etc/ssl/certs/company-ldap.pem",
			TLSServerName: "ldap.internal.example.com",
			UseTLS:        true,
		}},
		BindPassword: "super-secret",
	}, nil)
	if err != nil {
		t.Fatalf("UpdateConfig returned error: %v", err)
	}
	if !response.BindPasswordConfigured {
		t.Fatalf("expected bind password to be marked configured")
	}
	if repo.setting == nil || repo.setting.LDAPBindPasswordCiphertext == nil {
		t.Fatalf("expected encrypted bind password to be stored")
	}
	if got, want := response.LDAP.TLSCAFile, "/etc/ssl/certs/company-ldap.pem"; got != want {
		t.Fatalf("response LDAP TLS CA file = %q, want %q", got, want)
	}
	if got, want := response.LDAP.TLSServerName, "ldap.internal.example.com"; got != want {
		t.Fatalf("response LDAP TLS server name = %q, want %q", got, want)
	}
	if got, want := repo.setting.LDAPTLSCAFile, "/etc/ssl/certs/company-ldap.pem"; got != want {
		t.Fatalf("stored LDAP TLS CA file = %q, want %q", got, want)
	}
	if strings.Contains(*repo.setting.LDAPBindPasswordCiphertext, "super-secret") {
		t.Fatalf("ciphertext leaked plaintext password")
	}
	payload, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if strings.Contains(string(payload), "super-secret") {
		t.Fatalf("response leaked plaintext password: %s", payload)
	}

	previousCiphertext := *repo.setting.LDAPBindPasswordCiphertext
	updated, err := manager.UpdateConfig(context.Background(), EnterpriseAuthConfigUpdateRequest{
		ExpectedVersion:    response.Version,
		Enabled:            false,
		AllowLocalFallback: true,
		SyncRole:           true,
		LDAP:               LDAPConfigUpdate{LDAPConfigPublic: LDAPConfigPublic{}},
	}, nil)
	if err != nil {
		t.Fatalf("UpdateConfig with existing encrypted password returned error: %v", err)
	}
	if !updated.BindPasswordConfigured || repo.setting.LDAPBindPasswordCiphertext == nil {
		t.Fatalf("expected existing encrypted bind password to remain configured")
	}
	if *repo.setting.LDAPBindPasswordCiphertext == previousCiphertext {
		t.Fatalf("expected existing encrypted bind password to be re-encrypted")
	}
}

func TestEnterpriseAuthManagerEncryptsWithConfiguredKey(t *testing.T) {
	t.Setenv("AUTH_CONFIG_ENCRYPTION_KEY", "")
	repo := &fakeEnterpriseAuthSettingRepo{}
	manager, err := NewEnterpriseAuthManager(repo, config.EnterpriseAuthConfig{}, "01234567890123456789012345678901")
	if err != nil {
		t.Fatalf("NewEnterpriseAuthManager returned error: %v", err)
	}

	_, err = manager.UpdateConfig(context.Background(), EnterpriseAuthConfigUpdateRequest{
		ExpectedVersion:   0,
		Enabled:           false,
		AllowLocalFallback: true,
		LDAP:              LDAPConfigUpdate{LDAPConfigPublic: LDAPConfigPublic{Host: "ldap.example.com"}},
		BindPassword:      "super-secret",
	}, nil)
	if err != nil {
		t.Fatalf("UpdateConfig returned error: %v", err)
	}
	if repo.setting == nil || repo.setting.LDAPBindPasswordCiphertext == nil {
		t.Fatalf("expected encrypted bind password to be stored")
	}
}

func TestEnterpriseAuthManagerEncryptsWithDerivedConfiguredKey(t *testing.T) {
	t.Setenv("AUTH_CONFIG_ENCRYPTION_KEY", "")
	repo := &fakeEnterpriseAuthSettingRepo{}
	manager, err := NewEnterpriseAuthManager(repo, config.EnterpriseAuthConfig{}, "sha256:stable-jwt-secret")
	if err != nil {
		t.Fatalf("NewEnterpriseAuthManager returned error: %v", err)
	}

	_, err = manager.UpdateConfig(context.Background(), EnterpriseAuthConfigUpdateRequest{
		ExpectedVersion:   0,
		Enabled:           false,
		AllowLocalFallback: true,
		LDAP:              LDAPConfigUpdate{LDAPConfigPublic: LDAPConfigPublic{Host: "ldap.example.com"}},
		BindPassword:      "super-secret",
	}, nil)
	if err != nil {
		t.Fatalf("UpdateConfig returned error: %v", err)
	}
	if repo.setting == nil || repo.setting.LDAPBindPasswordCiphertext == nil {
		t.Fatalf("expected encrypted bind password to be stored")
	}
}

func TestEnterpriseAuthManagerPreservesEnvironmentBindPasswordWithoutEncryptionKey(t *testing.T) {
	t.Setenv("AUTH_CONFIG_ENCRYPTION_KEY", "")
	repo := &fakeEnterpriseAuthSettingRepo{}
	base := config.EnterpriseAuthConfig{
		LDAP: config.LDAPConfig{BindPassword: "environment-secret"},
	}
	manager, err := NewEnterpriseAuthManager(repo, base)
	if err != nil {
		t.Fatalf("NewEnterpriseAuthManager returned error: %v", err)
	}

	response, err := manager.UpdateConfig(context.Background(), EnterpriseAuthConfigUpdateRequest{
		ExpectedVersion:    0,
		Enabled:            false,
		AllowLocalFallback: true,
		SyncRole:           true,
		LDAP:               LDAPConfigUpdate{LDAPConfigPublic: LDAPConfigPublic{}},
	}, nil)
	if err != nil {
		t.Fatalf("UpdateConfig returned error: %v", err)
	}
	if !response.BindPasswordConfigured {
		t.Fatalf("expected environment bind password to remain configured")
	}
	if repo.setting == nil || repo.setting.LDAPBindPasswordCiphertext != nil {
		t.Fatalf("expected environment bind password to remain out of the database")
	}

	reloaded, err := NewEnterpriseAuthManager(repo, base)
	if err != nil {
		t.Fatalf("NewEnterpriseAuthManager reload returned error: %v", err)
	}
	if got := reloaded.current().config.LDAP.BindPassword; got != "environment-secret" {
		t.Fatalf("reloaded bind password = %q, want environment-managed password", got)
	}
}

func TestEnterpriseAuthManagerClearBindPasswordOverridesEnvironment(t *testing.T) {
	t.Setenv("AUTH_CONFIG_ENCRYPTION_KEY", "")
	repo := &fakeEnterpriseAuthSettingRepo{}
	base := config.EnterpriseAuthConfig{
		LDAP: config.LDAPConfig{BindPassword: "environment-secret"},
	}
	manager, err := NewEnterpriseAuthManager(repo, base)
	if err != nil {
		t.Fatalf("NewEnterpriseAuthManager returned error: %v", err)
	}

	response, err := manager.UpdateConfig(context.Background(), EnterpriseAuthConfigUpdateRequest{
		ExpectedVersion:    0,
		Enabled:            false,
		AllowLocalFallback: true,
		LDAP:               LDAPConfigUpdate{LDAPConfigPublic: LDAPConfigPublic{}},
		ClearBindPassword:  true,
	}, nil)
	if err != nil {
		t.Fatalf("UpdateConfig returned error: %v", err)
	}
	if response.BindPasswordConfigured {
		t.Fatalf("expected bind password to be cleared")
	}
	if repo.setting == nil || repo.setting.LDAPBindPasswordCiphertext == nil || *repo.setting.LDAPBindPasswordCiphertext != "" {
		t.Fatalf("expected an explicit empty bind password override")
	}

	reloaded, err := NewEnterpriseAuthManager(repo, base)
	if err != nil {
		t.Fatalf("NewEnterpriseAuthManager reload returned error: %v", err)
	}
	if got := reloaded.current().config.LDAP.BindPassword; got != "" {
		t.Fatalf("reloaded bind password = %q, want cleared password", got)
	}
}

func TestEnterpriseAuthManagerRejectsPasswordWithoutEncryptionKey(t *testing.T) {
	repo := &fakeEnterpriseAuthSettingRepo{}
	manager, err := NewEnterpriseAuthManager(repo, config.EnterpriseAuthConfig{})
	if err != nil {
		t.Fatalf("NewEnterpriseAuthManager returned error: %v", err)
	}

	_, err = manager.UpdateConfig(context.Background(), EnterpriseAuthConfigUpdateRequest{
		ExpectedVersion:   0,
		Enabled:           false,
		AllowLocalFallback: true,
		LDAP:              LDAPConfigUpdate{LDAPConfigPublic: LDAPConfigPublic{Host: "ldap.example.com"}},
		BindPassword:      "super-secret",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "AUTH_CONFIG_ENCRYPTION_KEY") {
		t.Fatalf("UpdateConfig error = %v, want encryption key error", err)
	}
}

func TestSplitStoredListPreservesLDAPDNCommas(t *testing.T) {
	groups := splitStoredList("cn=admins,ou=Groups,dc=example,dc=com\ncn=ops,ou=Groups,dc=example,dc=com; cn=devs,ou=Groups,dc=example,dc=com")

	want := []string{
		"cn=admins,ou=Groups,dc=example,dc=com",
		"cn=ops,ou=Groups,dc=example,dc=com",
		"cn=devs,ou=Groups,dc=example,dc=com",
	}
	if len(groups) != len(want) {
		t.Fatalf("group count = %d, want %d: %#v", len(groups), len(want), groups)
	}
	for i := range want {
		if groups[i] != want[i] {
			t.Fatalf("groups[%d] = %q, want %q", i, groups[i], want[i])
		}
	}
}

type fakeEnterpriseAuthSettingRepo struct {
	setting *models.EnterpriseAuthSetting
}

func (r *fakeEnterpriseAuthSettingRepo) Get(provider string) (*models.EnterpriseAuthSetting, error) {
	if r.setting == nil || r.setting.Provider != provider {
		return nil, nil
	}
	clone := *r.setting
	return &clone, nil
}

func (r *fakeEnterpriseAuthSettingRepo) GetVersion(provider string) (int64, error) {
	if r.setting == nil || r.setting.Provider != provider {
		return 0, nil
	}
	return r.setting.Version, nil
}

func (r *fakeEnterpriseAuthSettingRepo) Save(setting *models.EnterpriseAuthSetting, expectedVersion int64) error {
	if r.setting == nil {
		if expectedVersion != 0 {
			return repository.ErrEnterpriseAuthVersionConflict
		}
		clone := *setting
		clone.ID = 1
		clone.Version = 1
		r.setting = &clone
		*setting = clone
		return nil
	}
	if r.setting.Version != expectedVersion {
		return repository.ErrEnterpriseAuthVersionConflict
	}
	clone := *setting
	clone.ID = r.setting.ID
	clone.Version = r.setting.Version + 1
	r.setting = &clone
	*setting = clone
	return nil
}

var _ repository.EnterpriseAuthSettingRepository = (*fakeEnterpriseAuthSettingRepo)(nil)
