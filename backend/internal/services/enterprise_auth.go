package services

import (
	"context"
	"errors"
)

const (
	AuthProviderLocal = "local"
	AuthProviderLDAP  = "ldap"
)

var (
	ErrEnterpriseInvalidCredentials = errors.New("enterprise invalid credentials")
	ErrEnterpriseUserNotFound       = errors.New("enterprise user not found")
	ErrEnterpriseUnavailable        = errors.New("enterprise authentication unavailable")
)

type EnterpriseAuthenticator interface {
	AuthenticateByIdentity(ctx context.Context, externalID, password string) (*EnterpriseUser, error)
}

type EnterpriseAuthDiagnostics interface {
	Status(ctx context.Context) EnterpriseAuthStatus
}

type EnterpriseAuthPolicy struct {
	AllowLocalFallback bool `json:"allow_local_fallback"`
	SyncRole           bool `json:"sync_role"`
}

type EnterpriseAuthPolicyProvider interface {
	EnterpriseAuthPolicy() EnterpriseAuthPolicy
}

type LDAPListOptions struct {
	Query string
	Limit int
}

// LDAPDirectory provides read-only access to the configured enterprise directory.
type LDAPDirectory interface {
	ListUsers(ctx context.Context, options ...LDAPListOptions) ([]LDAPDirectoryUser, error)
}

type LDAPDirectoryUser struct {
	ExternalID string `json:"external_id"`
	Username   string `json:"username"`
	Email      string `json:"email"`
	Role       string `json:"role,omitempty"`
	Error      string `json:"error,omitempty"`
}

type EnterpriseAuthStatus struct {
	Enabled    bool              `json:"enabled"`
	Provider   string            `json:"provider"`
	Configured bool              `json:"configured"`
	Checks     map[string]string `json:"checks"`
	Details    map[string]string `json:"details,omitempty"`
	Warnings   []string          `json:"warnings,omitempty"`
	Error      string            `json:"error,omitempty"`
}

type EnterpriseUser struct {
	Provider   string
	ExternalID string
	Username   string
	Email      string
	Role       string
}
