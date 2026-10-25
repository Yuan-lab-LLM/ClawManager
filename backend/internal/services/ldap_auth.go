package services

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"clawreef/internal/config"
	"github.com/go-ldap/ldap/v3"
)

type LDAPAuthenticator struct {
	enabled bool
	cfg     config.LDAPConfig
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

func (a *LDAPAuthenticator) AuthenticateByIdentity(ctx context.Context, externalID, password string) (*EnterpriseUser, error) {
	if a == nil || !a.enabled {
		return nil, ErrEnterpriseUnavailable
	}
	if strings.TrimSpace(externalID) == "" || password == "" {
		return nil, ErrEnterpriseInvalidCredentials
	}
	conn, err := a.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if err := a.bindService(conn); err != nil {
		return nil, err
	}
	entry, err := a.searchUserByDN(conn, externalID)
	if err != nil {
		return nil, err
	}
	role := a.roleForUser(conn, entry.DN)
	if err := conn.Bind(entry.DN, password); err != nil {
		return nil, ErrEnterpriseInvalidCredentials
	}
	return &EnterpriseUser{
		Provider:   AuthProviderLDAP,
		ExternalID: entry.DN,
		Username:   entry.Username,
		Email:      entry.Email,
		Role:       role,
	}, nil
}

// ListUsers returns every directory entry matching the configured user filter.
// The configured %s placeholder is replaced with a wildcard for the directory-wide query.
func (a *LDAPAuthenticator) ListUsers(ctx context.Context, options ...LDAPListOptions) ([]LDAPDirectoryUser, error) {
	if a == nil || !a.enabled {
		return nil, errors.New("ldap authentication is not enabled")
	}
	if err := a.validateConfig(); err != nil {
		return nil, err
	}
	conn, err := a.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if err := a.bindService(conn); err != nil {
		return nil, err
	}

	option := LDAPListOptions{}
	if len(options) > 0 {
		option = options[0]
	}
	filter := ldapDirectoryFilter(a.cfg.UserFilter, option.Query)
	attributes := uniqueLDAPAttributes([]string{a.cfg.UsernameAttribute, a.cfg.EmailAttribute})
	request := ldap.NewSearchRequest(a.cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, ldapSearchLimit(option.Limit), 10, false, filter, attributes, nil)
	entries, err := conn.SearchWithPaging(request, 500)
	if err != nil {
		return nil, fmt.Errorf("%w: ldap user search failed: %v", ErrEnterpriseUnavailable, err)
	}
	adminMembers, useAdminMembers := a.adminMemberSet(conn)
	users := make([]LDAPDirectoryUser, 0, len(entries.Entries))
	for _, entry := range entries.Entries {
		username := strings.TrimSpace(entry.GetAttributeValue(a.cfg.UsernameAttribute))
		email := strings.TrimSpace(entry.GetAttributeValue(a.cfg.EmailAttribute))
		role := ""
		if useAdminMembers {
			role = a.roleForDN(entry.DN, adminMembers)
		} else {
			role = a.roleForUser(conn, entry.DN)
		}
		item := LDAPDirectoryUser{ExternalID: entry.DN, Username: username, Email: email, Role: role}
		if username == "" {
			item.Error = "ldap entry has no username attribute"
		}
		users = append(users, item)
	}
	return users, nil
}

func ldapDirectoryFilter(template, query string) string {
	value := "*"
	if trimmed := strings.TrimSpace(query); trimmed != "" {
		value = "*" + ldap.EscapeFilter(trimmed) + "*"
	}
	return strings.Replace(template, "%s", value, 1)
}

func ldapSearchLimit(limit int) int {
	if limit < 0 {
		return 0
	}
	return limit
}

func (a *LDAPAuthenticator) bindService(conn *ldap.Conn) error {
	if strings.TrimSpace(a.cfg.BindDN) == "" && strings.TrimSpace(a.cfg.BindPassword) == "" {
		return nil
	}
	if err := conn.Bind(a.cfg.BindDN, a.cfg.BindPassword); err != nil {
		return fmt.Errorf("%w: ldap service bind failed: %v", ErrEnterpriseUnavailable, err)
	}
	return nil
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
		Details: a.statusDetails(),
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
	if strings.TrimSpace(a.cfg.TLSCAFile) != "" && !a.cfg.UseTLS && !a.cfg.StartTLS {
		status.Warnings = append(status.Warnings, "ldap_tls_ca_file_unused")
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
		if err := a.bindService(conn); err != nil {
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
	tlsConfig, err := a.tlsConfig()
	if err != nil {
		return nil, fmt.Errorf("%w: ldap tls config failed: %v", ErrEnterpriseUnavailable, err)
	}
	conn, err := ldap.DialURL(address, ldap.DialWithDialer(dialer), ldap.DialWithTLSConfig(tlsConfig))
	if err != nil {
		return nil, fmt.Errorf("%w: ldap dial failed: %v", ErrEnterpriseUnavailable, err)
	}

	if a.cfg.StartTLS {
		if err := conn.StartTLS(tlsConfig); err != nil {
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
	if strings.TrimSpace(a.cfg.TLSCAFile) != "" && !a.cfg.UseTLS && !a.cfg.StartTLS {
		return errors.New("ldap TLS CA file requires useTLS or startTLS")
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

func (a *LDAPAuthenticator) tlsConfig() (*tls.Config, error) {
	serverName := strings.TrimSpace(a.cfg.TLSServerName)
	if serverName == "" {
		serverName = strings.TrimSpace(a.cfg.Host)
	}
	cfg := &tls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: a.cfg.SkipTLSVerify, //nolint:gosec // Operator-controlled compatibility option.
	}
	if caFile := strings.TrimSpace(a.cfg.TLSCAFile); caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read LDAP TLS CA file: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}
		if pool == nil {
			pool = x509.NewCertPool()
		}
		if ok := pool.AppendCertsFromPEM(pem); !ok {
			return nil, errors.New("LDAP TLS CA file has no PEM certificates")
		}
		cfg.RootCAs = pool
	}
	return cfg, nil
}

func (a *LDAPAuthenticator) searchUserByDN(conn *ldap.Conn, userDN string) (*ldapUserEntry, error) {
	attributes := uniqueLDAPAttributes([]string{a.cfg.UsernameAttribute, a.cfg.EmailAttribute})
	req := ldap.NewSearchRequest(userDN, ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, 10, false, "(objectClass=*)", attributes, nil)
	result, err := conn.Search(req)
	if err != nil || len(result.Entries) != 1 {
		return nil, ErrEnterpriseInvalidCredentials
	}
	entry := result.Entries[0]
	username := strings.TrimSpace(entry.GetAttributeValue(a.cfg.UsernameAttribute))
	return &ldapUserEntry{
		DN:       entry.DN,
		Username: username,
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
	groupBaseDN := strings.TrimSpace(a.cfg.GroupBaseDN)
	if groupBaseDN == "" {
		groupBaseDN = a.cfg.BaseDN
	}
	req := ldap.NewSearchRequest(
		groupBaseDN,
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

func (a *LDAPAuthenticator) roleForUser(conn *ldap.Conn, userDN string) string {
	if a.isAdmin(conn, userDN) {
		return "admin"
	}
	return a.defaultRole()
}

func (a *LDAPAuthenticator) roleForDN(userDN string, adminMembers map[string]struct{}) string {
	if _, ok := adminMembers[strings.ToLower(strings.TrimSpace(userDN))]; ok {
		return "admin"
	}
	return a.defaultRole()
}

func (a *LDAPAuthenticator) defaultRole() string {
	role := a.cfg.DefaultRole
	if role == "" {
		role = "user"
	}
	if strings.EqualFold(strings.TrimSpace(role), "admin") {
		return "admin"
	}
	return "user"
}

func (a *LDAPAuthenticator) adminMemberSet(conn *ldap.Conn) (map[string]struct{}, bool) {
	adminGroups := cleanLDAPDNList(a.cfg.AdminGroupDNs)
	if len(adminGroups) == 0 {
		return nil, true
	}
	memberAttribute, ok := ldapSimpleMemberAttribute(a.cfg.GroupFilter)
	if !ok {
		return nil, false
	}
	members := make(map[string]struct{})
	for _, groupDN := range adminGroups {
		req := ldap.NewSearchRequest(groupDN, ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, 10, false, "(objectClass=*)", []string{memberAttribute}, nil)
		result, err := conn.Search(req)
		if err != nil || len(result.Entries) != 1 {
			return nil, false
		}
		for _, member := range result.Entries[0].GetAttributeValues(memberAttribute) {
			if normalized := strings.ToLower(strings.TrimSpace(member)); normalized != "" {
				members[normalized] = struct{}{}
			}
		}
	}
	if len(members) == 0 {
		return nil, false
	}
	return members, true
}

func ldapSimpleMemberAttribute(filter string) (string, bool) {
	filter = strings.TrimSpace(filter)
	if !strings.HasPrefix(filter, "(") || !strings.HasSuffix(filter, ")") {
		return "", false
	}
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(filter, "("), ")"))
	attribute, value, ok := strings.Cut(inner, "=")
	if !ok || strings.TrimSpace(value) != "%s" {
		return "", false
	}
	attribute = strings.TrimSpace(attribute)
	if attribute == "" || strings.ContainsAny(attribute, " ()&|!") {
		return "", false
	}
	return attribute, true
}

func cleanLDAPDNList(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func (a *LDAPAuthenticator) statusDetails() map[string]string {
	if a == nil {
		return nil
	}
	details := map[string]string{
		"address":             fmt.Sprintf("%s:%d", strings.TrimSpace(a.cfg.Host), a.cfg.Port),
		"tls_mode":            ldapTLSMode(a.cfg),
		"tls_verify":          ldapTLSVerify(a.cfg),
		"tls_server_name":     firstNonEmpty(strings.TrimSpace(a.cfg.TLSServerName), strings.TrimSpace(a.cfg.Host)),
		"bind_mode":           ldapBindMode(a.cfg),
		"user_base_dn":        strings.TrimSpace(a.cfg.BaseDN),
		"group_base_dn":       firstNonEmpty(strings.TrimSpace(a.cfg.GroupBaseDN), strings.TrimSpace(a.cfg.BaseDN)),
		"group_role_strategy": ldapGroupRoleStrategy(a.cfg),
	}
	if strings.TrimSpace(a.cfg.TLSCAFile) != "" {
		details["tls_ca_file"] = strings.TrimSpace(a.cfg.TLSCAFile)
	}
	return details
}

func ldapTLSMode(cfg config.LDAPConfig) string {
	if cfg.UseTLS {
		return "ldaps"
	}
	if cfg.StartTLS {
		return "starttls"
	}
	return "plain"
}

func ldapTLSVerify(cfg config.LDAPConfig) string {
	if cfg.SkipTLSVerify {
		return "disabled"
	}
	return "enabled"
}

func ldapBindMode(cfg config.LDAPConfig) string {
	if strings.TrimSpace(cfg.BindDN) != "" || strings.TrimSpace(cfg.BindPassword) != "" {
		return "service"
	}
	return "anonymous"
}

func ldapGroupRoleStrategy(cfg config.LDAPConfig) string {
	if _, ok := ldapSimpleMemberAttribute(cfg.GroupFilter); ok {
		return "prefetch_admin_group_members"
	}
	return "per_user_group_search"
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
