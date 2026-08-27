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
	Authenticate(ctx context.Context, username, password string) (*EnterpriseUser, error)
}

type EnterpriseAuthDiagnostics interface {
	Status(ctx context.Context) EnterpriseAuthStatus
}

type EnterpriseAuthStatus struct {
	Enabled    bool              `json:"enabled"`
	Provider   string            `json:"provider"`
	Configured bool              `json:"configured"`
	Checks     map[string]string `json:"checks"`
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
