package services

import (
	"context"
	"testing"

	"clawreef/internal/config"
)

func TestNewLDAPAuthenticatorValidatesRequiredConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.LDAPConfig
		want string
	}{
		{
			name: "host required",
			cfg: config.LDAPConfig{
				BaseDN:     "dc=example,dc=com",
				UserFilter: "(&(objectClass=person)(uid=%s))",
				GroupFilter: "(member=%s)",
			},
			want: "ldap host is required",
		},
		{
			name: "base dn required",
			cfg: config.LDAPConfig{
				Host:        "ldap.example.com",
				UserFilter:  "(&(objectClass=person)(uid=%s))",
				GroupFilter: "(member=%s)",
			},
			want: "ldap baseDN is required",
		},
		{
			name: "tls modes are exclusive",
			cfg: config.LDAPConfig{
				Host:        "ldap.example.com",
				BaseDN:      "dc=example,dc=com",
				UseTLS:      true,
				StartTLS:    true,
				UserFilter:  "(&(objectClass=person)(uid=%s))",
				GroupFilter: "(member=%s)",
			},
			want: "ldap useTLS and startTLS cannot both be enabled",
		},
		{
			name: "user filter placeholder required",
			cfg: config.LDAPConfig{
				Host:        "ldap.example.com",
				BaseDN:      "dc=example,dc=com",
				UserFilter:  "(uid=alice)",
				GroupFilter: "(member=%s)",
			},
			want: "ldap userFilter must contain %s placeholder",
		},
		{
			name: "group filter placeholder required",
			cfg: config.LDAPConfig{
				Host:        "ldap.example.com",
				BaseDN:      "dc=example,dc=com",
				UserFilter:  "(uid=%s)",
				GroupFilter: "(member=alice)",
			},
			want: "ldap groupFilter must contain %s placeholder",
		},
		{
			name: "ca file requires tls mode",
			cfg: config.LDAPConfig{
				Host:        "ldap.example.com",
				BaseDN:      "dc=example,dc=com",
				UserFilter:  "(uid=%s)",
				GroupFilter: "(member=%s)",
				TLSCAFile:   "/etc/ssl/certs/company-ldap.pem",
			},
			want: "ldap TLS CA file requires useTLS or startTLS",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewLDAPAuthenticator(tc.cfg); err == nil || err.Error() != tc.want {
				t.Fatalf("NewLDAPAuthenticator error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestNewLDAPAuthenticatorAcceptsValidConfig(t *testing.T) {
	authenticator, err := NewLDAPAuthenticator(config.LDAPConfig{
		Host:        "ldap.example.com",
		BaseDN:      "dc=example,dc=com",
		UserFilter:  "(&(objectClass=person)(uid=%s))",
		GroupFilter: "(member=%s)",
	})
	if err != nil {
		t.Fatalf("NewLDAPAuthenticator returned error: %v", err)
	}
	if authenticator == nil {
		t.Fatalf("expected authenticator")
	}
}

func TestUniqueLDAPAttributesTrimsAndDeduplicates(t *testing.T) {
	got := uniqueLDAPAttributes([]string{"uid", " mail ", "UID", "", "displayName"})
	want := []string{"uid", "mail", "displayName"}
	if len(got) != len(want) {
		t.Fatalf("attributes = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attributes = %#v, want %#v", got, want)
		}
	}
}

func TestLDAPDirectoryFilterUsesWildcardForConfiguredPlaceholder(t *testing.T) {
	got := ldapDirectoryFilter("(&(objectClass=person)(uid=%s))", "")
	if got != "(&(objectClass=person)(uid=*))" {
		t.Fatalf("directory filter = %q", got)
	}
}

func TestLDAPDirectoryFilterUsesEscapedContainsQuery(t *testing.T) {
	got := ldapDirectoryFilter("(&(objectClass=person)(uid=%s))", "ali*ce)")
	if got != "(&(objectClass=person)(uid=*ali\\2ace\\29*))" {
		t.Fatalf("directory filter = %q", got)
	}
}

func TestLDAPSearchLimit(t *testing.T) {
	if got := ldapSearchLimit(-1); got != 0 {
		t.Fatalf("negative limit = %d, want 0", got)
	}
	if got := ldapSearchLimit(25); got != 25 {
		t.Fatalf("limit = %d, want 25", got)
	}
}

func TestLDAPSimpleMemberAttribute(t *testing.T) {
	attribute, ok := ldapSimpleMemberAttribute("(member=%s)")
	if !ok || attribute != "member" {
		t.Fatalf("simple member attribute = %q, %v; want member, true", attribute, ok)
	}
	if _, ok := ldapSimpleMemberAttribute("(&(objectClass=group)(member=%s))"); ok {
		t.Fatalf("compound group filter must use per-user fallback")
	}
}

func TestLDAPRoleForDNUsePrefetchedAdminMembers(t *testing.T) {
	authenticator := &LDAPAuthenticator{cfg: config.LDAPConfig{DefaultRole: "user"}}
	role := authenticator.roleForDN("uid=alice,ou=People,dc=example,dc=com", map[string]struct{}{
		"uid=alice,ou=people,dc=example,dc=com": {},
	})
	if role != "admin" {
		t.Fatalf("role = %q, want admin", role)
	}
	if role := authenticator.roleForDN("uid=bob,ou=People,dc=example,dc=com", nil); role != "user" {
		t.Fatalf("default role = %q, want user", role)
	}
}

func TestLDAPDiagnosticsStatusDisabledSkipsChecks(t *testing.T) {
	status := NewLDAPDiagnostics(false, config.LDAPConfig{}).Status(context.Background())
	if status.Enabled {
		t.Fatalf("status enabled = true, want false")
	}
	if status.Configured {
		t.Fatalf("status configured = true, want false")
	}
	for name, value := range status.Checks {
		if value != "skipped" {
			t.Fatalf("check %s = %q, want skipped", name, value)
		}
	}
}

func TestLDAPDiagnosticsStatusReportsConfigError(t *testing.T) {
	status := NewLDAPDiagnostics(true, config.LDAPConfig{}).Status(context.Background())
	if !status.Enabled {
		t.Fatalf("status enabled = false, want true")
	}
	if status.Configured {
		t.Fatalf("status configured = true, want false")
	}
	if status.Error != "ldap host is required" {
		t.Fatalf("status error = %q, want ldap host is required", status.Error)
	}
}

func TestLDAPDiagnosticsStatusIncludesDetails(t *testing.T) {
	status := NewLDAPDiagnostics(true, config.LDAPConfig{
		Host:          "ldap.example.com",
		Port:          636,
		UseTLS:        true,
		SkipTLSVerify: true,
		TLSCAFile:     "/etc/ssl/certs/company-ldap.pem",
		TLSServerName: "ldap.internal.example.com",
		BaseDN:        "dc=example,dc=com",
		GroupBaseDN:   "ou=Groups,dc=example,dc=com",
		UserFilter:    "(uid=%s)",
		GroupFilter:   "(member=%s)",
	}).Status(context.Background())

	if got, want := status.Details["tls_mode"], "ldaps"; got != want {
		t.Fatalf("tls mode = %q, want %q", got, want)
	}
	if got, want := status.Details["tls_verify"], "disabled"; got != want {
		t.Fatalf("tls verify = %q, want %q", got, want)
	}
	if got, want := status.Details["tls_server_name"], "ldap.internal.example.com"; got != want {
		t.Fatalf("tls server name = %q, want %q", got, want)
	}
	if got, want := status.Details["group_role_strategy"], "prefetch_admin_group_members"; got != want {
		t.Fatalf("group role strategy = %q, want %q", got, want)
	}
}
