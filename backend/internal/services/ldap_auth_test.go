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
