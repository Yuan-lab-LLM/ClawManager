package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/config"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/utils"
)

// AuthService defines the interface for authentication operations
type AuthService interface {
	Register(username, email, password string) (*models.User, error)
	Login(username, password string) (*TokenPair, error)
	RefreshToken(refreshToken string) (*TokenPair, error)
	GetCurrentUser(userID int) (*models.User, error)
	ChangePassword(userID int, currentPassword, newPassword string) error
}

// TokenPair holds access and refresh tokens
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// authService implements AuthService
type authService struct {
	userRepo                repository.UserRepository
	quotaRepo               repository.QuotaRepository
	jwtConfig               config.JWTConfig
	enterpriseAuthenticator EnterpriseAuthenticator
	enterprisePolicy        enterpriseAuthPolicy
}

type enterpriseAuthPolicy struct {
	AllowLocalFallback bool
	SyncRole           bool
}

type AuthServiceOption func(*authService)

func WithEnterpriseAuthPolicy(cfg config.EnterpriseAuthConfig) AuthServiceOption {
	return func(s *authService) {
		s.enterprisePolicy.AllowLocalFallback = cfg.AllowLocalFallback
		s.enterprisePolicy.SyncRole = cfg.SyncRole
	}
}

func WithQuotaRepository(quotaRepo repository.QuotaRepository) AuthServiceOption {
	return func(s *authService) {
		s.quotaRepo = quotaRepo
	}
}

// NewAuthService creates a new auth service
func NewAuthService(userRepo repository.UserRepository, jwtConfig config.JWTConfig, enterpriseAuthenticator EnterpriseAuthenticator, options ...AuthServiceOption) AuthService {
	s := &authService{
		userRepo:                userRepo,
		jwtConfig:               jwtConfig,
		enterpriseAuthenticator: enterpriseAuthenticator,
		enterprisePolicy: enterpriseAuthPolicy{
			AllowLocalFallback: true,
		},
	}
	for _, option := range options {
		if option != nil {
			option(s)
		}
	}
	return s
}

// Register registers a new user
func (s *authService) Register(username, email, password string) (*models.User, error) {
	if isReservedLocalUsername(username) {
		return nil, errors.New("local usernames cannot start with ldap_")
	}
	// Check if username already exists
	existingUser, err := s.userRepo.GetByAuthProviderUsername(AuthProviderLocal, username)
	if err != nil {
		return nil, fmt.Errorf("failed to check username: %w", err)
	}
	if existingUser != nil {
		return nil, errors.New("username already exists")
	}

	// Check if email already exists
	existingUser, err = s.userRepo.GetByEmail(email)
	if err != nil {
		return nil, fmt.Errorf("failed to check email: %w", err)
	}
	if existingUser != nil {
		return nil, errors.New("email already exists")
	}

	// Hash password
	passwordHash, err := utils.HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	// Create user
	user := &models.User{
		Username:     username,
		Email:        email,
		PasswordHash: passwordHash,
		Role:         "user",
		AuthProvider: AuthProviderLocal,
		IsActive:     true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := s.userRepo.Create(user); err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return user, nil
}

// Login authenticates a user and returns tokens
func (s *authService) Login(username, password string) (*TokenPair, error) {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(username)), "ldap_") {
		return s.loginLDAPAlias(strings.TrimSpace(username), password)
	}
	// An unqualified username is always a local login. LDAP is never selected
	// implicitly, even when a directory contains the same uid.
	return s.loginLocal(username, password)
}

func (s *authService) loginLocal(username, password string) (*TokenPair, error) {
	// Get user by username
	user, err := s.userRepo.GetByAuthProviderUsername(AuthProviderLocal, username)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return nil, errors.New("invalid username or password")
	}

	// Check if user is active
	if !user.IsActive {
		return nil, errors.New("account is disabled")
	}
	if normalizeAuthProvider(user.AuthProvider) == AuthProviderLDAP {
		return nil, errors.New("invalid username or password")
	}

	// Verify password
	if !utils.VerifyPassword(password, user.PasswordHash) {
		return nil, errors.New("invalid username or password")
	}

	// Update last login
	now := time.Now()
	user.LastLogin = &now
	if strings.TrimSpace(user.AuthProvider) == "" {
		user.AuthProvider = AuthProviderLocal
	}
	if err := s.userRepo.Update(user); err != nil {
		return nil, fmt.Errorf("failed to update last login: %w", err)
	}

	// Generate tokens
	tokenPair, err := s.generateTokens(user.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate tokens: %w", err)
	}

	return tokenPair, nil
}

func (s *authService) loginLDAPAlias(loginName, password string) (*TokenPair, error) {
	loginAlias := strings.ToLower(strings.TrimSpace(loginName))
	if len(loginAlias) == len("ldap_") || s.enterpriseAuthenticator == nil {
		return nil, errors.New("invalid username or password")
	}
	user, err := s.userRepo.GetByLoginAlias(AuthProviderLDAP, loginAlias)
	if err != nil || user == nil || user.ExternalID == nil || strings.TrimSpace(*user.ExternalID) == "" || !user.IsActive {
		return nil, errors.New("invalid username or password")
	}
	externalID := strings.TrimSpace(*user.ExternalID)
	enterpriseUser, err := s.enterpriseAuthenticator.AuthenticateByIdentity(context.Background(), externalID, password)
	if err != nil {
		return nil, errors.New("invalid username or password")
	}
	if enterpriseUser == nil || !strings.EqualFold(strings.TrimSpace(enterpriseUser.ExternalID), externalID) {
		return nil, errors.New("invalid username or password")
	}

	now := time.Now()
	user.LastLogin = &now
	if s.currentEnterprisePolicy().SyncRole && (enterpriseUser.Role == "admin" || enterpriseUser.Role == "user") {
		user.Role = enterpriseUser.Role
		if err := SyncDefaultQuotaForRole(s.quotaRepo, user.ID, user.Role); err != nil {
			return nil, err
		}
	}
	if err := s.userRepo.Update(user); err != nil {
		return nil, fmt.Errorf("failed to update enterprise user: %w", err)
	}
	return s.generateTokens(user.ID)
}

func (s *authService) currentEnterprisePolicy() enterpriseAuthPolicy {
	if provider, ok := s.enterpriseAuthenticator.(EnterpriseAuthPolicyProvider); ok && provider != nil {
		policy := provider.EnterpriseAuthPolicy()
		return enterpriseAuthPolicy{
			AllowLocalFallback: policy.AllowLocalFallback,
			SyncRole:           policy.SyncRole,
		}
	}
	return s.enterprisePolicy
}

// RefreshToken refreshes the access token using a refresh token
func (s *authService) RefreshToken(refreshToken string) (*TokenPair, error) {
	// Validate refresh token
	claims, err := utils.ValidateToken(refreshToken, s.jwtConfig.Secret)
	if err != nil {
		return nil, errors.New("invalid refresh token")
	}

	// Check token type
	if claims.TokenType != "refresh" {
		return nil, errors.New("invalid token type")
	}

	// Get user
	user, err := s.userRepo.GetByID(claims.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return nil, errors.New("user not found")
	}

	// Check if user is active
	if !user.IsActive {
		return nil, errors.New("account is disabled")
	}

	// Generate new tokens
	tokenPair, err := s.generateTokens(user.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate tokens: %w", err)
	}

	return tokenPair, nil
}

// GetCurrentUser gets the current user by ID
func (s *authService) GetCurrentUser(userID int) (*models.User, error) {
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return nil, errors.New("user not found")
	}
	return user, nil
}

// ChangePassword updates the current user's password
func (s *authService) ChangePassword(userID int, currentPassword, newPassword string) error {
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return errors.New("user not found")
	}
	if normalizeAuthProvider(user.AuthProvider) == AuthProviderLDAP {
		return errors.New("enterprise users must change password in the enterprise identity platform")
	}

	if !utils.VerifyPassword(currentPassword, user.PasswordHash) {
		return errors.New("current password is incorrect")
	}

	passwordHash, err := utils.HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	user.PasswordHash = passwordHash
	user.UpdatedAt = time.Now()

	if err := s.userRepo.Update(user); err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	return nil
}

func normalizeAuthProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case AuthProviderLDAP:
		return AuthProviderLDAP
	default:
		return AuthProviderLocal
	}
}

func isReservedLocalUsername(username string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(username)), "ldap_")
}

func enterprisePasswordMarker(provider string) string {
	return fmt.Sprintf("external:%s", normalizeAuthProvider(provider))
}

// generateTokens generates access and refresh tokens
func (s *authService) generateTokens(userID int) (*TokenPair, error) {
	// Generate access token
	accessToken, err := utils.GenerateToken(utils.TokenClaims{
		UserID:    userID,
		TokenType: "access",
	}, s.jwtConfig.Secret, time.Duration(s.jwtConfig.AccessExpiry)*time.Minute)
	if err != nil {
		return nil, err
	}

	// Generate refresh token
	refreshToken, err := utils.GenerateToken(utils.TokenClaims{
		UserID:    userID,
		TokenType: "refresh",
	}, s.jwtConfig.Secret, time.Duration(s.jwtConfig.RefreshExpiry)*time.Hour)
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    s.jwtConfig.AccessExpiry * 60,
	}, nil
}

func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
