package models

import "time"

// EnterpriseAuthSetting stores the admin-managed enterprise auth override.
type EnterpriseAuthSetting struct {
	ID                         int       `db:"id,primarykey,autoincrement" json:"id"`
	Provider                   string    `db:"provider" json:"provider"`
	Enabled                    bool      `db:"enabled" json:"enabled"`
	AllowLocalFallback         bool      `db:"allow_local_fallback" json:"allow_local_fallback"`
	SyncRole                   bool      `db:"sync_role" json:"sync_role"`
	LDAPHost                   string    `db:"ldap_host" json:"ldap_host"`
	LDAPPort                   int       `db:"ldap_port" json:"ldap_port"`
	LDAPUseTLS                 bool      `db:"ldap_use_tls" json:"ldap_use_tls"`
	LDAPStartTLS               bool      `db:"ldap_start_tls" json:"ldap_start_tls"`
	LDAPSkipTLSVerify          bool      `db:"ldap_skip_tls_verify" json:"ldap_skip_tls_verify"`
	LDAPTLSCAFile              string    `db:"ldap_tls_ca_file" json:"ldap_tls_ca_file"`
	LDAPTLSServerName          string    `db:"ldap_tls_server_name" json:"ldap_tls_server_name"`
	LDAPBindDN                 string    `db:"ldap_bind_dn" json:"ldap_bind_dn"`
	// Nil inherits LDAP_BIND_PASSWORD; an empty value explicitly clears it.
	LDAPBindPasswordCiphertext *string   `db:"ldap_bind_password_ciphertext" json:"-"`
	LDAPBaseDN                 string    `db:"ldap_base_dn" json:"ldap_base_dn"`
	LDAPUserFilter             string    `db:"ldap_user_filter" json:"ldap_user_filter"`
	LDAPUsernameAttribute      string    `db:"ldap_username_attribute" json:"ldap_username_attribute"`
	LDAPEmailAttribute         string    `db:"ldap_email_attribute" json:"ldap_email_attribute"`
	LDAPGroupBaseDN            string    `db:"ldap_group_base_dn" json:"ldap_group_base_dn"`
	LDAPGroupFilter            string    `db:"ldap_group_filter" json:"ldap_group_filter"`
	LDAPAdminGroupDNs          *string   `db:"ldap_admin_group_dns" json:"ldap_admin_group_dns"`
	LDAPDefaultRole            string    `db:"ldap_default_role" json:"ldap_default_role"`
	Version                    int64     `db:"version" json:"version"`
	UpdatedBy                  *int      `db:"updated_by" json:"updated_by,omitempty"`
	CreatedAt                  time.Time `db:"created_at" json:"created_at"`
	UpdatedAt                  time.Time `db:"updated_at" json:"updated_at"`
}

func (s EnterpriseAuthSetting) TableName() string {
	return "enterprise_auth_settings"
}
