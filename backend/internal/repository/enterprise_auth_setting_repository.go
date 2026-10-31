package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

var ErrEnterpriseAuthVersionConflict = errors.New("enterprise auth settings version conflict")

// EnterpriseAuthSettingRepository stores the single provider configuration row.
type EnterpriseAuthSettingRepository interface {
	Get(provider string) (*models.EnterpriseAuthSetting, error)
	GetVersion(provider string) (int64, error)
	Save(setting *models.EnterpriseAuthSetting, expectedVersion int64) error
}

type enterpriseAuthSettingRepository struct {
	sess db.Session
}

func NewEnterpriseAuthSettingRepository(sess db.Session) EnterpriseAuthSettingRepository {
	repo := &enterpriseAuthSettingRepository{sess: sess}
	return repo
}

func (r *enterpriseAuthSettingRepository) ensureColumn(column, alterSQL string) {
	var exists int
	row, err := r.sess.SQL().QueryRow(`
SELECT COUNT(*)
FROM information_schema.columns
WHERE table_schema = DATABASE()
  AND table_name = 'enterprise_auth_settings'
  AND column_name = ?
`, column)
	if err != nil {
		panic(fmt.Errorf("failed to check enterprise_auth_settings.%s: %w", column, err))
	}
	if err := row.Scan(&exists); err != nil {
		panic(fmt.Errorf("failed to scan enterprise_auth_settings.%s existence: %w", column, err))
	}
	if exists > 0 {
		return
	}
	if _, err := r.sess.SQL().Exec(alterSQL); err != nil {
		panic(fmt.Errorf("failed to add enterprise_auth_settings.%s: %w", column, err))
	}
}

func (r *enterpriseAuthSettingRepository) Get(provider string) (*models.EnterpriseAuthSetting, error) {
	var setting models.EnterpriseAuthSetting
	err := r.sess.Collection("enterprise_auth_settings").Find(db.Cond{"provider": provider}).One(&setting)
	if err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get enterprise auth settings: %w", err)
	}
	return &setting, nil
}

func (r *enterpriseAuthSettingRepository) GetVersion(provider string) (int64, error) {
	var version int64
	row, err := r.sess.SQL().QueryRow("SELECT version FROM enterprise_auth_settings WHERE provider = ?", provider)
	if err != nil {
		return 0, fmt.Errorf("failed to query enterprise auth settings version: %w", err)
	}
	if err := row.Scan(&version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to scan enterprise auth settings version: %w", err)
	}
	return version, nil
}

func (r *enterpriseAuthSettingRepository) Save(setting *models.EnterpriseAuthSetting, expectedVersion int64) error {
	now := time.Now()
	existing, err := r.Get(setting.Provider)
	if err != nil {
		return err
	}
	if existing == nil {
		if expectedVersion != 0 {
			return ErrEnterpriseAuthVersionConflict
		}
		setting.Version = 1
		setting.CreatedAt = now
		setting.UpdatedAt = now
		res, err := r.sess.Collection("enterprise_auth_settings").Insert(setting)
		if err != nil {
			return fmt.Errorf("failed to create enterprise auth settings: %w", err)
		}
		if id, ok := res.ID().(int64); ok {
			setting.ID = int(id)
		}
		return nil
	}
	if existing.Version != expectedVersion {
		return ErrEnterpriseAuthVersionConflict
	}

	setting.ID = existing.ID
	setting.Version = existing.Version + 1
	setting.CreatedAt = existing.CreatedAt
	setting.UpdatedAt = now
	result, err := r.sess.SQL().Exec(`
UPDATE enterprise_auth_settings
SET enabled = ?,
    allow_local_fallback = ?,
    sync_role = ?,
    ldap_host = ?,
    ldap_port = ?,
    ldap_use_tls = ?,
    ldap_start_tls = ?,
    ldap_skip_tls_verify = ?,
    ldap_tls_ca_file = ?,
    ldap_tls_server_name = ?,
    ldap_bind_dn = ?,
    ldap_bind_password_ciphertext = ?,
    ldap_base_dn = ?,
    ldap_user_filter = ?,
    ldap_username_attribute = ?,
    ldap_email_attribute = ?,
    ldap_group_base_dn = ?,
    ldap_group_filter = ?,
    ldap_admin_group_dns = ?,
    ldap_default_role = ?,
    version = ?,
    updated_by = ?,
    updated_at = ?
WHERE provider = ? AND version = ?
`,
		setting.Enabled,
		setting.AllowLocalFallback,
		setting.SyncRole,
		setting.LDAPHost,
		setting.LDAPPort,
		setting.LDAPUseTLS,
		setting.LDAPStartTLS,
		setting.LDAPSkipTLSVerify,
		setting.LDAPTLSCAFile,
		setting.LDAPTLSServerName,
		setting.LDAPBindDN,
		setting.LDAPBindPasswordCiphertext,
		setting.LDAPBaseDN,
		setting.LDAPUserFilter,
		setting.LDAPUsernameAttribute,
		setting.LDAPEmailAttribute,
		setting.LDAPGroupBaseDN,
		setting.LDAPGroupFilter,
		setting.LDAPAdminGroupDNs,
		setting.LDAPDefaultRole,
		setting.Version,
		setting.UpdatedBy,
		setting.UpdatedAt,
		setting.Provider,
		expectedVersion,
	)
	if err != nil {
		return fmt.Errorf("failed to update enterprise auth settings: %w", err)
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return ErrEnterpriseAuthVersionConflict
	}
	return nil
}
