package services

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"clawreef/internal/config"
	"clawreef/internal/models"
	"clawreef/internal/repository"
)

const enterpriseLDAPProvider = AuthProviderLDAP

type LDAPConfigPublic struct {
	Host              string   `json:"host"`
	Port              int      `json:"port"`
	UseTLS            bool     `json:"use_tls"`
	StartTLS          bool     `json:"start_tls"`
	SkipTLSVerify     bool     `json:"skip_tls_verify"`
	TLSCAFile         string   `json:"tls_ca_file"`
	TLSServerName     string   `json:"tls_server_name"`
	BindDN            string   `json:"bind_dn"`
	BaseDN            string   `json:"base_dn"`
	UserFilter        string   `json:"user_filter"`
	UsernameAttribute string   `json:"username_attribute"`
	EmailAttribute    string   `json:"email_attribute"`
	GroupBaseDN        string   `json:"group_base_dn"`
	GroupFilter        string   `json:"group_filter"`
	AdminGroupDNs      []string `json:"admin_group_dns"`
	DefaultRole        string   `json:"default_role"`
}

type LDAPConfigUpdate struct {
	LDAPConfigPublic
}

type EnterpriseAuthConfigResponse struct {
	Provider               string               `json:"provider"`
	Enabled                bool                 `json:"enabled"`
	AllowLocalFallback     bool                 `json:"allow_local_fallback"`
	SyncRole               bool                 `json:"sync_role"`
	LDAP                   LDAPConfigPublic     `json:"ldap"`
	BindPasswordConfigured bool                 `json:"bind_password_configured"`
	Version                int64                `json:"version"`
	UpdatedAt              *time.Time           `json:"updated_at,omitempty"`
	Status                   EnterpriseAuthStatus `json:"status"`
}

type EnterpriseAuthConfigUpdateRequest struct {
	ExpectedVersion    int64            `json:"expected_version"`
	Enabled            bool             `json:"enabled"`
	AllowLocalFallback bool             `json:"allow_local_fallback"`
	SyncRole           bool             `json:"sync_role"`
	LDAP               LDAPConfigUpdate `json:"ldap"`
	BindPassword       string           `json:"bind_password"`
	ClearBindPassword  bool             `json:"clear_bind_password"`
}

type enterpriseAuthSnapshot struct {
	config                   config.EnterpriseAuthConfig
	authenticator            *LDAPAuthenticator
	diagnostics              *LDAPAuthenticator
	version                  int64
	updatedAt                *time.Time
	bindPasswordConfigured   bool
	bindPasswordOverride     bool
}

type EnterpriseAuthManager struct {
	repo         repository.EnterpriseAuthSettingRepository
	baseConfig   config.EnterpriseAuthConfig
	cipher       cipher.AEAD
	pollInterval time.Duration
	state        atomic.Value
}

func NewEnterpriseAuthManager(repo repository.EnterpriseAuthSettingRepository, base config.EnterpriseAuthConfig, encryptionKeys ...string) (*EnterpriseAuthManager, error) {
	manager := &EnterpriseAuthManager{
		repo:         repo,
		baseConfig:   normalizeEnterpriseAuthConfig(base),
		cipher:       loadEnterpriseAuthCipher(encryptionKeys...),
		pollInterval: 5 * time.Second,
	}
	snapshot, err := manager.loadSnapshot()
	if err != nil {
		return nil, err
	}
	manager.state.Store(snapshot)
	return manager, nil
}

func (m *EnterpriseAuthManager) Start(ctx context.Context) {
	if m == nil || m.repo == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(m.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.refreshIfChanged(ctx)
			}
		}
	}()
}

func (m *EnterpriseAuthManager) AuthenticateByIdentity(ctx context.Context, externalID, password string) (*EnterpriseUser, error) {
	snapshot := m.current()
	if snapshot == nil || snapshot.authenticator == nil {
		return nil, ErrEnterpriseUnavailable
	}
	return snapshot.authenticator.AuthenticateByIdentity(ctx, externalID, password)
}

func (m *EnterpriseAuthManager) ListUsers(ctx context.Context, options ...LDAPListOptions) ([]LDAPDirectoryUser, error) {
	snapshot := m.current()
	if snapshot == nil || snapshot.authenticator == nil {
		return nil, errors.New("ldap authentication is not enabled")
	}
	return snapshot.authenticator.ListUsers(ctx, options...)
}

func (m *EnterpriseAuthManager) Status(ctx context.Context) EnterpriseAuthStatus {
	snapshot := m.current()
	if snapshot == nil || snapshot.diagnostics == nil {
		return disabledEnterpriseStatus()
	}
	return snapshot.diagnostics.Status(ctx)
}

func (m *EnterpriseAuthManager) EnterpriseAuthPolicy() EnterpriseAuthPolicy {
	snapshot := m.current()
	if snapshot == nil {
		return EnterpriseAuthPolicy{AllowLocalFallback: true}
	}
	return EnterpriseAuthPolicy{
		AllowLocalFallback: snapshot.config.AllowLocalFallback,
		SyncRole:           snapshot.config.SyncRole,
	}
}

func (m *EnterpriseAuthManager) Config(ctx context.Context) EnterpriseAuthConfigResponse {
	snapshot := m.current()
	if snapshot == nil {
		return EnterpriseAuthConfigResponse{
			Provider: enterpriseLDAPProvider,
			Status:   disabledEnterpriseStatus(),
		}
	}
	return responseFromSnapshot(snapshot, m.Status(ctx))
}

func (m *EnterpriseAuthManager) TestConfig(ctx context.Context, req EnterpriseAuthConfigUpdateRequest) (EnterpriseAuthStatus, error) {
	snapshot := m.current()
	candidate, _, _, err := m.candidateConfig(req, snapshot)
	if err != nil {
		return disabledEnterpriseStatus(), err
	}
	if !candidate.Enabled {
		return NewLDAPDiagnostics(false, candidate.LDAP).Status(ctx), nil
	}
	authenticator, err := NewLDAPAuthenticator(candidate.LDAP)
	if err != nil {
		return disabledEnterpriseStatus(), err
	}
	status := authenticator.Status(ctx)
	if !status.Configured || status.Error != "" {
		return status, errors.New(firstNonEmpty(status.Error, "ldap test failed"))
	}
	return status, nil
}

func (m *EnterpriseAuthManager) UpdateConfig(ctx context.Context, req EnterpriseAuthConfigUpdateRequest, updatedBy *int) (EnterpriseAuthConfigResponse, error) {
	snapshot := m.current()
	candidate, passwordConfigured, bindPasswordOverride, err := m.candidateConfig(req, snapshot)
	if err != nil {
		return EnterpriseAuthConfigResponse{}, err
	}
	var authenticator *LDAPAuthenticator
	if candidate.Enabled {
		authenticator, err = NewLDAPAuthenticator(candidate.LDAP)
		if err != nil {
			return EnterpriseAuthConfigResponse{}, err
		}
		status := authenticator.Status(ctx)
		if !status.Configured || status.Error != "" {
			return EnterpriseAuthConfigResponse{}, errors.New(firstNonEmpty(status.Error, "ldap test failed"))
		}
	}

	setting, err := m.settingFromConfig(candidate, passwordConfigured, bindPasswordOverride, updatedBy)
	if err != nil {
		return EnterpriseAuthConfigResponse{}, err
	}
	if err := m.repo.Save(setting, req.ExpectedVersion); err != nil {
		return EnterpriseAuthConfigResponse{}, err
	}
	next, err := m.snapshotFromConfig(candidate, setting.Version, &setting.UpdatedAt, passwordConfigured, bindPasswordOverride, authenticator)
	if err != nil {
		return EnterpriseAuthConfigResponse{}, err
	}
	m.state.Store(next)
	return responseFromSnapshot(next, next.diagnostics.Status(ctx)), nil
}

func (m *EnterpriseAuthManager) current() *enterpriseAuthSnapshot {
	value := m.state.Load()
	if value == nil {
		return nil
	}
	snapshot, _ := value.(*enterpriseAuthSnapshot)
	return snapshot
}

func (m *EnterpriseAuthManager) refreshIfChanged(ctx context.Context) {
	current := m.current()
	currentVersion := int64(0)
	if current != nil {
		currentVersion = current.version
	}
	version, err := m.repo.GetVersion(enterpriseLDAPProvider)
	if err != nil {
		log.Printf("enterprise auth settings version check failed: %v", err)
		return
	}
	if version == currentVersion {
		return
	}
	snapshot, err := m.loadSnapshot()
	if err != nil {
		log.Printf("enterprise auth settings reload failed: %v", err)
		return
	}
	_ = ctx
	m.state.Store(snapshot)
}

func (m *EnterpriseAuthManager) loadSnapshot() (*enterpriseAuthSnapshot, error) {
	if m.repo == nil {
		return m.snapshotFromConfig(m.baseConfig, 0, nil, strings.TrimSpace(m.baseConfig.LDAP.BindPassword) != "", false, nil)
	}
	setting, err := m.repo.Get(enterpriseLDAPProvider)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if setting == nil {
		return m.snapshotFromConfig(m.baseConfig, 0, nil, strings.TrimSpace(m.baseConfig.LDAP.BindPassword) != "", false, nil)
	}
	cfg, passwordConfigured, bindPasswordOverride, err := m.configFromSetting(setting)
	if err != nil {
		return nil, err
	}
	return m.snapshotFromConfig(cfg, setting.Version, &setting.UpdatedAt, passwordConfigured, bindPasswordOverride, nil)
}

func (m *EnterpriseAuthManager) snapshotFromConfig(cfg config.EnterpriseAuthConfig, version int64, updatedAt *time.Time, passwordConfigured, bindPasswordOverride bool, authenticator *LDAPAuthenticator) (*enterpriseAuthSnapshot, error) {
	cfg = normalizeEnterpriseAuthConfig(cfg)
	if cfg.Enabled && authenticator == nil {
		var err error
		authenticator, err = NewLDAPAuthenticator(cfg.LDAP)
		if err != nil {
			return nil, err
		}
	}
	return &enterpriseAuthSnapshot{
		config:                 cfg,
		authenticator:          authenticator,
		diagnostics:            NewLDAPDiagnostics(cfg.Enabled, cfg.LDAP),
		version:                version,
		updatedAt:              updatedAt,
		bindPasswordConfigured: passwordConfigured,
		bindPasswordOverride:   bindPasswordOverride,
	}, nil
}

func (m *EnterpriseAuthManager) candidateConfig(req EnterpriseAuthConfigUpdateRequest, snapshot *enterpriseAuthSnapshot) (config.EnterpriseAuthConfig, bool, bool, error) {
	current := m.baseConfig
	passwordConfigured := strings.TrimSpace(current.LDAP.BindPassword) != ""
	bindPasswordOverride := false
	if snapshot != nil {
		current = snapshot.config
		passwordConfigured = snapshot.bindPasswordConfigured
		bindPasswordOverride = snapshot.bindPasswordOverride
	}
	next := config.EnterpriseAuthConfig{
		Enabled:            req.Enabled,
		AllowLocalFallback: req.AllowLocalFallback,
		SyncRole:           req.SyncRole,
		LDAP: config.LDAPConfig{
			Host:              req.LDAP.Host,
			Port:              req.LDAP.Port,
			UseTLS:            req.LDAP.UseTLS,
			StartTLS:          req.LDAP.StartTLS,
			SkipTLSVerify:     req.LDAP.SkipTLSVerify,
			TLSCAFile:         req.LDAP.TLSCAFile,
			TLSServerName:     req.LDAP.TLSServerName,
			BindDN:            req.LDAP.BindDN,
			BaseDN:            req.LDAP.BaseDN,
			UserFilter:        req.LDAP.UserFilter,
			UsernameAttribute: req.LDAP.UsernameAttribute,
			EmailAttribute:    req.LDAP.EmailAttribute,
			GroupBaseDN:        req.LDAP.GroupBaseDN,
			GroupFilter:        req.LDAP.GroupFilter,
			AdminGroupDNs:      cleanStringList(req.LDAP.AdminGroupDNs),
			DefaultRole:        req.LDAP.DefaultRole,
		},
	}
	if req.ClearBindPassword {
		next.LDAP.BindPassword = ""
		passwordConfigured = false
		bindPasswordOverride = true
	} else if req.BindPassword != "" {
		next.LDAP.BindPassword = req.BindPassword
		passwordConfigured = strings.TrimSpace(req.BindPassword) != ""
		bindPasswordOverride = true
	} else {
		next.LDAP.BindPassword = current.LDAP.BindPassword
	}
	return normalizeEnterpriseAuthConfig(next), passwordConfigured, bindPasswordOverride, nil
}

func (m *EnterpriseAuthManager) configFromSetting(setting *models.EnterpriseAuthSetting) (config.EnterpriseAuthConfig, bool, bool, error) {
	cfg := config.EnterpriseAuthConfig{
		Enabled:            setting.Enabled,
		AllowLocalFallback: setting.AllowLocalFallback,
		SyncRole:           setting.SyncRole,
		LDAP: config.LDAPConfig{
			Host:              setting.LDAPHost,
			Port:              setting.LDAPPort,
			UseTLS:            setting.LDAPUseTLS,
			StartTLS:          setting.LDAPStartTLS,
			SkipTLSVerify:     setting.LDAPSkipTLSVerify,
			TLSCAFile:         setting.LDAPTLSCAFile,
			TLSServerName:     setting.LDAPTLSServerName,
			BindDN:            setting.LDAPBindDN,
			BaseDN:            setting.LDAPBaseDN,
			UserFilter:        setting.LDAPUserFilter,
			UsernameAttribute: setting.LDAPUsernameAttribute,
			EmailAttribute:    setting.LDAPEmailAttribute,
			GroupBaseDN:        setting.LDAPGroupBaseDN,
			GroupFilter:        setting.LDAPGroupFilter,
			AdminGroupDNs:      splitStoredList(derefEnterpriseString(setting.LDAPAdminGroupDNs)),
			DefaultRole:        setting.LDAPDefaultRole,
		},
	}
	if setting.LDAPBindPasswordCiphertext != nil {
		if strings.TrimSpace(*setting.LDAPBindPasswordCiphertext) == "" {
			return normalizeEnterpriseAuthConfig(cfg), false, true, nil
		}
		password, err := m.decrypt(*setting.LDAPBindPasswordCiphertext)
		if err != nil {
			return cfg, true, true, err
		}
		cfg.LDAP.BindPassword = password
		return normalizeEnterpriseAuthConfig(cfg), true, true, nil
	}
	cfg.LDAP.BindPassword = m.baseConfig.LDAP.BindPassword
	return normalizeEnterpriseAuthConfig(cfg), strings.TrimSpace(cfg.LDAP.BindPassword) != "", false, nil
}

func (m *EnterpriseAuthManager) settingFromConfig(cfg config.EnterpriseAuthConfig, passwordConfigured, bindPasswordOverride bool, updatedBy *int) (*models.EnterpriseAuthSetting, error) {
	var ciphertext *string
	if bindPasswordOverride && passwordConfigured && strings.TrimSpace(cfg.LDAP.BindPassword) != "" {
		encrypted, err := m.encrypt(cfg.LDAP.BindPassword)
		if err != nil {
			return nil, err
		}
		ciphertext = &encrypted
	} else if bindPasswordOverride {
		// A non-nil empty value is an explicit clear marker. A nil value means
		// that the password remains managed by LDAP_BIND_PASSWORD.
		cleared := ""
		ciphertext = &cleared
	}
	return &models.EnterpriseAuthSetting{
		Provider:                   enterpriseLDAPProvider,
		Enabled:                    cfg.Enabled,
		AllowLocalFallback:         cfg.AllowLocalFallback,
		SyncRole:                   cfg.SyncRole,
		LDAPHost:                   strings.TrimSpace(cfg.LDAP.Host),
		LDAPPort:                   cfg.LDAP.Port,
		LDAPUseTLS:                 cfg.LDAP.UseTLS,
		LDAPStartTLS:               cfg.LDAP.StartTLS,
		LDAPSkipTLSVerify:          cfg.LDAP.SkipTLSVerify,
		LDAPTLSCAFile:              strings.TrimSpace(cfg.LDAP.TLSCAFile),
		LDAPTLSServerName:          strings.TrimSpace(cfg.LDAP.TLSServerName),
		LDAPBindDN:                 strings.TrimSpace(cfg.LDAP.BindDN),
		LDAPBindPasswordCiphertext: ciphertext,
		LDAPBaseDN:                 strings.TrimSpace(cfg.LDAP.BaseDN),
		LDAPUserFilter:             strings.TrimSpace(cfg.LDAP.UserFilter),
		LDAPUsernameAttribute:      strings.TrimSpace(cfg.LDAP.UsernameAttribute),
		LDAPEmailAttribute:         strings.TrimSpace(cfg.LDAP.EmailAttribute),
		LDAPGroupBaseDN:            strings.TrimSpace(cfg.LDAP.GroupBaseDN),
		LDAPGroupFilter:            strings.TrimSpace(cfg.LDAP.GroupFilter),
		LDAPAdminGroupDNs:          stringPtrOrNil(strings.Join(cleanStringList(cfg.LDAP.AdminGroupDNs), "\n")),
		LDAPDefaultRole:            normalizeLDAPRoleLocal(cfg.LDAP.DefaultRole),
		UpdatedBy:                  updatedBy,
	}, nil
}

func (m *EnterpriseAuthManager) encrypt(value string) (string, error) {
	if m.cipher == nil {
		return "", errors.New("AUTH_CONFIG_ENCRYPTION_KEY is required to save LDAP bind password")
	}
	nonce := make([]byte, m.cipher.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate enterprise auth nonce: %w", err)
	}
	sealed := m.cipher.Seal(nil, nonce, []byte(value), []byte(enterpriseLDAPProvider))
	payload := append(nonce, sealed...)
	return base64.StdEncoding.EncodeToString(payload), nil
}

func (m *EnterpriseAuthManager) decrypt(value string) (string, error) {
	if m.cipher == nil {
		return "", errors.New("AUTH_CONFIG_ENCRYPTION_KEY is required to load LDAP bind password")
	}
	payload, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", fmt.Errorf("failed to decode LDAP bind password: %w", err)
	}
	nonceSize := m.cipher.NonceSize()
	if len(payload) <= nonceSize {
		return "", errors.New("LDAP bind password ciphertext is invalid")
	}
	plain, err := m.cipher.Open(nil, payload[:nonceSize], payload[nonceSize:], []byte(enterpriseLDAPProvider))
	if err != nil {
		return "", fmt.Errorf("failed to decrypt LDAP bind password: %w", err)
	}
	return string(plain), nil
}

func loadEnterpriseAuthCipher(encryptionKeys ...string) cipher.AEAD {
	key := ""
	for _, candidate := range encryptionKeys {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			key = trimmed
			break
		}
	}
	if key == "" {
		key = strings.TrimSpace(os.Getenv("AUTH_CONFIG_ENCRYPTION_KEY"))
	}
	if key == "" {
		return nil
	}
	if secret, ok := strings.CutPrefix(key, "sha256:"); ok {
		sum := sha256.Sum256([]byte(secret))
		block, _ := aes.NewCipher(sum[:])
		aead, _ := cipher.NewGCM(block)
		return aead
	}
	if decoded, err := base64.StdEncoding.DecodeString(key); err == nil && len(decoded) == 32 {
		block, _ := aes.NewCipher(decoded)
		aead, _ := cipher.NewGCM(block)
		return aead
	}
	if decoded, err := hex.DecodeString(key); err == nil && len(decoded) == 32 {
		block, _ := aes.NewCipher(decoded)
		aead, _ := cipher.NewGCM(block)
		return aead
	}
	if len([]byte(key)) == 32 {
		block, _ := aes.NewCipher([]byte(key))
		aead, _ := cipher.NewGCM(block)
		return aead
	}
	log.Printf("AUTH_CONFIG_ENCRYPTION_KEY must be 32 raw bytes, 32 bytes hex encoded, or 32 bytes base64 encoded")
	return nil
}

func responseFromSnapshot(snapshot *enterpriseAuthSnapshot, status EnterpriseAuthStatus) EnterpriseAuthConfigResponse {
	return EnterpriseAuthConfigResponse{
		Provider:               enterpriseLDAPProvider,
		Enabled:                snapshot.config.Enabled,
		AllowLocalFallback:     snapshot.config.AllowLocalFallback,
		SyncRole:               snapshot.config.SyncRole,
		LDAP:                   publicLDAPConfig(snapshot.config.LDAP),
		BindPasswordConfigured: snapshot.bindPasswordConfigured,
		Version:                snapshot.version,
		UpdatedAt:              snapshot.updatedAt,
		Status:                 status,
	}
}

func publicLDAPConfig(cfg config.LDAPConfig) LDAPConfigPublic {
	return LDAPConfigPublic{
		Host:              cfg.Host,
		Port:              cfg.Port,
		UseTLS:            cfg.UseTLS,
		StartTLS:          cfg.StartTLS,
		SkipTLSVerify:     cfg.SkipTLSVerify,
		TLSCAFile:         cfg.TLSCAFile,
		TLSServerName:     cfg.TLSServerName,
		BindDN:            cfg.BindDN,
		BaseDN:            cfg.BaseDN,
		UserFilter:        cfg.UserFilter,
		UsernameAttribute: cfg.UsernameAttribute,
		EmailAttribute:    cfg.EmailAttribute,
		GroupBaseDN:        cfg.GroupBaseDN,
		GroupFilter:        cfg.GroupFilter,
		AdminGroupDNs:      cleanStringList(cfg.AdminGroupDNs),
		DefaultRole:        cfg.DefaultRole,
	}
}

func normalizeEnterpriseAuthConfig(cfg config.EnterpriseAuthConfig) config.EnterpriseAuthConfig {
	ldap := &cfg.LDAP
	ldap.Host = strings.TrimSpace(ldap.Host)
	ldap.TLSCAFile = strings.TrimSpace(ldap.TLSCAFile)
	ldap.TLSServerName = strings.TrimSpace(ldap.TLSServerName)
	ldap.BindDN = strings.TrimSpace(ldap.BindDN)
	ldap.BaseDN = strings.TrimSpace(ldap.BaseDN)
	ldap.UserFilter = strings.TrimSpace(ldap.UserFilter)
	ldap.UsernameAttribute = strings.TrimSpace(ldap.UsernameAttribute)
	ldap.EmailAttribute = strings.TrimSpace(ldap.EmailAttribute)
	ldap.GroupBaseDN = strings.TrimSpace(ldap.GroupBaseDN)
	ldap.GroupFilter = strings.TrimSpace(ldap.GroupFilter)
	if ldap.Port <= 0 {
		if ldap.UseTLS {
			ldap.Port = 636
		} else {
			ldap.Port = 389
		}
	}
	if ldap.UserFilter == "" {
		ldap.UserFilter = "(&(objectClass=person)(uid=%s))"
	}
	if ldap.UsernameAttribute == "" {
		ldap.UsernameAttribute = "uid"
	}
	if ldap.EmailAttribute == "" {
		ldap.EmailAttribute = "mail"
	}
	if ldap.GroupFilter == "" {
		ldap.GroupFilter = "(member=%s)"
	}
	if ldap.GroupBaseDN == "" {
		ldap.GroupBaseDN = ldap.BaseDN
	}
	ldap.AdminGroupDNs = cleanStringList(ldap.AdminGroupDNs)
	ldap.DefaultRole = normalizeLDAPRoleLocal(ldap.DefaultRole)
	return cfg
}

func normalizeLDAPRoleLocal(role string) string {
	if strings.EqualFold(strings.TrimSpace(role), "admin") {
		return "admin"
	}
	return "user"
}

func disabledEnterpriseStatus() EnterpriseAuthStatus {
	return EnterpriseAuthStatus{
		Enabled:    false,
		Provider:   enterpriseLDAPProvider,
		Configured: false,
		Checks: map[string]string{
			"dial":         "skipped",
			"service_bind": "skipped",
			"user_search":  "skipped",
			"group_search": "skipped",
		},
	}
}

func cleanStringList(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func splitStoredList(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ';'
	})
	return cleanStringList(fields)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func derefEnterpriseString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringPtrOrNil(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}
