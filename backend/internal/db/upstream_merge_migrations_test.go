package db

import (
	"strings"
	"testing"
)

func TestMigration049BackfillsLDAPLoginAliasesWithJoin(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/049_add_ldap_login_alias.sql")
	if err != nil {
		t.Fatalf("read migration 049: %v", err)
	}

	sql := strings.ReplaceAll(string(raw), "\r\n", "\n")
	for _, required := range []string{
		"ALTER TABLE users ADD COLUMN login_alias",
		"UPDATE users AS u\nJOIN",
		"AS unique_ldap_usernames ON unique_ldap_usernames.username = u.username",
		"HAVING COUNT(*) = 1",
		"local_username_key",
		"uk_users_local_username",
		"uk_users_provider_login_alias",
		"uk_users_provider_external_id",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 049 must contain %s", required)
		}
	}
	if strings.Contains(sql, "SELECT COUNT(*) FROM users AS same_uid") {
		t.Fatalf("migration 049 must not use a same-table correlated subquery in the update")
	}
}

func TestMigration050IncludesLDAPTLSCertificateSettings(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/050_add_enterprise_auth_settings.sql")
	if err != nil {
		t.Fatalf("read migration 050: %v", err)
	}

	sql := string(raw)
	for _, required := range []string{
		"ldap_tls_ca_file VARCHAR(1000) NOT NULL DEFAULT ''",
		"ldap_tls_server_name VARCHAR(255) NOT NULL DEFAULT ''",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 050 must contain %s", required)
		}
	}
}

func TestMigration059AddsOptionalInstanceAliasIdempotently(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/063_add_instance_alias.sql")
	if err != nil {
		t.Fatalf("read migration 059: %v", err)
	}
	sql := string(raw)
	for _, required := range []string{
		"information_schema.COLUMNS",
		"COLUMN_NAME = 'alias'",
		"ADD COLUMN alias VARCHAR(128)",
		"utf8mb4_unicode_ci NULL",
		"PREPARE instance_alias_column_stmt",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration 059 must contain %s", required)
		}
	}
}
