package services

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"clawreef/internal/config"
	"github.com/go-ldap/ldap/v3"
)

type LDAPAuthenticator struct {
	enabled bool
	cfg config.LDAPConfig
}

func NewLDAPAuthenticator(cfg config.LDAPConfig) (*LDAPAuthenticator, error) {
	authenticator := &LDAPAuthenticator{
		enabled: true,
		cfg:     cfg,
	}
	if err := authenticator.validateConfig(); err != nil {
		return nil, err
	}
	return authenticator, nil
}

func NewLDAPDiagnostics(enabled bool, cfg config.LDAPConfig) *LDAPAuthenticator {
	return &LDAPAuthenticator{
		enabled: enabled,
		cfg:     cfg,
	}
}

func (a *LDAPAuthenticator) Authenticate(ctx context.Context, username, password string) (*EnterpriseUser, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, ErrEnterpriseInvalidCredentials
	}

	conn, err := a.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if strings.TrimSpace(a.cfg.BindDN) != "" || strings.TrimSpace(a.cfg.BindPassword) != "" {
		if err := conn.Bind(a.cfg.BindDN, a.cfg.BindPassword); err != nil {
			return nil, fmt.Errorf("%w: ldap service bind failed: %v", ErrEnterpriseUnavailable, err)
		}
	}

	ldapUser, err := a.searchUser(conn, username)
	if err != nil {
		return nil, err
	}

	role := a.cfg.DefaultRole
	if role == "" {
		role = "user"
	}
	if a.isAdmin(conn, ldapUser.DN) {
		role = "admin"
	}

	if err := conn.Bind(ldapUser.DN, password); err != nil {
		return nil, ErrEnterpriseInvalidCredentials
	}

	return &EnterpriseUser{
		Provider:   AuthProviderLDAP,
		ExternalID: ldapUser.DN,
		Username:   ldapUser.Username,
		Email:      ldapUser.Email,
		Role:       role,
	}, nil
}

func (a *LDAPAuthenticator) Status(ctx context.Context) EnterpriseAuthStatus {
	status := EnterpriseAuthStatus{
		Enabled:    a != nil && a.enabled,
		Provider:   AuthProviderLDAP,
		Configured: false,
		Checks: map[string]string{
			"dial":         "skipped",
			"service_bind": "skipped",
			"user_search":  "skipped",
			"group_search": "skipped",
		},
	}
	if a == nil {
		status.Error = "ldap diagnostics unavailable"
		return status
	}
	if !a.enabled {
		return status
	}
	if err := a.validateConfig(); err != nil {
		status.Error = err.Error()
		return status
	}
	status.Configured = true
	if a.cfg.SkipTLSVerify {
		status.Warnings = append(status.Warnings, "ldap_skip_tls_verify_enabled")
	}

	conn, err := a.dial(ctx)
	if err != nil {
		status.Checks["dial"] = "failed"
		status.Error = err.Error()
		return status
	}
	defer conn.Close()
	status.Checks["dial"] = "ok"

	if strings.TrimSpace(a.cfg.BindDN) != "" || strings.TrimSpace(a.cfg.BindPassword) != "" {
		if err := conn.Bind(a.cfg.BindDN, a.cfg.BindPassword); err != nil {
			status.Checks["service_bind"] = "failed"
			status.Error = "ldap service bind failed"
			return status
		}
		status.Checks["service_bind"] = "ok"
	} else {
		status.Checks["service_bind"] = "anonymous"
	}

	if err := a.probeSearch(conn, a.cfg.BaseDN, a.cfg.UserFilter); err != nil {
		status.Checks["user_search"] = "failed"
		status.Error = "ldap user search failed"
		return status
	}
	status.Checks["user_search"] = "ok"

	groupBaseDN := strings.TrimSpace(a.cfg.GroupBaseDN)
	if groupBaseDN == "" {
		groupBaseDN = a.cfg.BaseDN
	}
	if err := a.probeSearch(conn, groupBaseDN, a.cfg.GroupFilter); err != nil {
		status.Checks["group_search"] = "failed"
		status.Error = "ldap group search failed"
		return status
	}
	status.Checks["group_search"] = "ok"
	return status
}

type ldapUserEntry struct {
	DN       string
	Username string
	Email    string
}

func (a *LDAPAuthenticator) dial(ctx context.Context) (*ldap.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scheme := "ldap"
	if a.cfg.UseTLS {
		scheme = "ldaps"
	}
	address := fmt.Sprintf("%s://%s:%d", scheme, a.cfg.Host, a.cfg.Port)
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := ldap.DialURL(address, ldap.DialWithDialer(dialer), ldap.DialWithTLSConfig(a.tlsConfig()))
	if err != nil {
		return nil, fmt.Errorf("%w: ldap dial failed: %v", ErrEnterpriseUnavailable, err)
	}

	if a.cfg.StartTLS {
		if err := conn.StartTLS(a.tlsConfig()); err != nil {
			conn.Close()
			return nil, fmt.Errorf("%w: ldap starttls failed: %v", ErrEnterpriseUnavailable, err)
		}
	}
	return conn, nil
}

func (a *LDAPAuthenticator) validateConfig() error {
	if strings.TrimSpace(a.cfg.Host) == "" {
		return errors.New("ldap host is required")
	}
	if strings.TrimSpace(a.cfg.BaseDN) == "" {
		return errors.New("ldap baseDN is required")
	}
	if a.cfg.UseTLS && a.cfg.StartTLS {
		return errors.New("ldap useTLS and startTLS cannot both be enabled")
	}
	if !strings.Contains(a.cfg.UserFilter, "%s") {
		return errors.New("ldap userFilter must contain %s placeholder")
	}
	if !strings.Contains(a.cfg.GroupFilter, "%s") {
		return errors.New("ldap groupFilter must contain %s placeholder")
	}
	return nil
}

func (a *LDAPAuthenticator) probeSearch(conn *ldap.Conn, baseDN, filterTemplate string) error {
	filter := fmt.Sprintf(filterTemplate, ldap.EscapeFilter("__clawmanager_healthcheck__"))
	req := ldap.NewSearchRequest(
		baseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0,
		5,
		false,
		filter,
		[]string{"dn"},
		nil,
	)
	_, err := conn.Search(req)
	return err
}

func (a *LDAPAuthenticator) tlsConfig() *tls.Config {
	return &tls.Config{
		ServerName:         a.cfg.Host,
		InsecureSkipVerify: a.cfg.SkipTLSVerify, //nolint:gosec // Operator-controlled compatibility option.
	}
}

func (a *LDAPAuthenticator) searchUser(conn *ldap.Conn, username string) (*ldapUserEntry, error) {
	filter := fmt.Sprintf(a.cfg.UserFilter, ldap.EscapeFilter(username))
	attributes := uniqueLDAPAttributes([]string{
		a.cfg.UsernameAttribute,
		a.cfg.EmailAttribute,
	})
	req := ldap.NewSearchRequest(
		a.cfg.BaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		2,
		10,
		false,
		filter,
		attributes,
		nil,
	)
	result, err := conn.Search(req)
	if err != nil {
		return nil, fmt.Errorf("%w: ldap user search failed: %v", ErrEnterpriseUnavailable, err)
	}
	if len(result.Entries) == 0 {
		return nil, ErrEnterpriseUserNotFound
	}
	if len(result.Entries) > 1 {
		return nil, fmt.Errorf("%w: multiple ldap users matched", ErrEnterpriseInvalidCredentials)
	}

	entry := result.Entries[0]
	resolvedUsername := strings.TrimSpace(entry.GetAttributeValue(a.cfg.UsernameAttribute))
	if resolvedUsername == "" {
		resolvedUsername = username
	}

	return &ldapUserEntry{
		DN:       entry.DN,
		Username: resolvedUsername,
		Email:    strings.TrimSpace(entry.GetAttributeValue(a.cfg.EmailAttribute)),
	}, nil
}

func (a *LDAPAuthenticator) isAdmin(conn *ldap.Conn, userDN string) bool {
	adminGroups := make(map[string]struct{}, len(a.cfg.AdminGroupDNs))
	for _, groupDN := range a.cfg.AdminGroupDNs {
		if normalized := strings.ToLower(strings.TrimSpace(groupDN)); normalized != "" {
			adminGroups[normalized] = struct{}{}
		}
	}
	if len(adminGroups) == 0 {
		return false
	}

	filter := fmt.Sprintf(a.cfg.GroupFilter, ldap.EscapeFilter(userDN))
	req := ldap.NewSearchRequest(
		a.cfg.GroupBaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0,
		10,
		false,
		filter,
		[]string{"dn"},
		nil,
	)
	result, err := conn.Search(req)
	if err != nil {
		return false
	}
	for _, entry := range result.Entries {
		if _, ok := adminGroups[strings.ToLower(strings.TrimSpace(entry.DN))]; ok {
			return true
		}
	}
	return false
}

func uniqueLDAPAttributes(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}
