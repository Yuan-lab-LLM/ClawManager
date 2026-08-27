package services

import (
	"context"
	"errors"
	"fmt"
	"log"
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
	// Check if username already exists
	existingUser, err := s.userRepo.GetByUsername(username)
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
	if s.enterpriseAuthenticator != nil {
		tokenPair, err := s.loginEnterprise(username, password)
		if err == nil {
			return tokenPair, nil
		}
		switch {
		case errors.Is(err, ErrEnterpriseInvalidCredentials):
			log.Printf("Enterprise authentication rejected credentials for username=%q", username)
			return nil, errors.New("invalid username or password")
		case errors.Is(err, ErrEnterpriseUserNotFound):
			log.Printf("Enterprise authentication did not find username=%q", username)
		case errors.Is(err, ErrEnterpriseUnavailable):
			log.Printf("Enterprise authentication unavailable for username=%q: %v", username, err)
		default:
			return nil, err
		}
		if !s.enterprisePolicy.AllowLocalFallback {
			return nil, errors.New("invalid username or password")
		}
		log.Printf("Enterprise authentication did not complete for username=%q; trying local authentication", username)
	}

	// Get user by username
	user, err := s.userRepo.GetByUsername(username)
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

func (s *authService) loginEnterprise(username, password string) (*TokenPair, error) {
	enterpriseUser, err := s.enterpriseAuthenticator.Authenticate(context.Background(), username, password)
	if err != nil {
		return nil, err
	}

	user, err := s.resolveProvisionedEnterpriseUser(enterpriseUser)
	if err != nil {
		return nil, err
	}
	if !user.IsActive {
		return nil, errors.New("account is disabled")
	}

	now := time.Now()
	user.LastLogin = &now
	if err := s.userRepo.Update(user); err != nil {
		return nil, fmt.Errorf("failed to update last login: %w", err)
	}

	tokenPair, err := s.generateTokens(user.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate tokens: %w", err)
	}
	return tokenPair, nil
}

func (s *authService) resolveProvisionedEnterpriseUser(external *EnterpriseUser) (*models.User, error) {
	if external == nil {
		return nil, errors.New("enterprise user is empty")
	}
	username := strings.TrimSpace(external.Username)
	if username == "" {
		return nil, errors.New("enterprise user username is empty")
	}
	provider := normalizeAuthProvider(external.Provider)
	if provider != AuthProviderLDAP {
		return nil, errors.New("invalid username or password")
	}

	user, err := s.userRepo.GetByUsername(username)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	externalID := strings.TrimSpace(external.ExternalID)
	if externalID != "" {
		externalUser, err := s.userRepo.GetByExternalIdentity(provider, externalID)
		if err != nil {
			return nil, fmt.Errorf("failed to get enterprise user: %w", err)
		}
		if externalUser != nil && (user == nil || externalUser.ID != user.ID) {
			return nil, errors.New("invalid username or password")
		}
	}
	if user == nil || normalizeAuthProvider(user.AuthProvider) != provider {
		return nil, errors.New("invalid username or password")
	}

	if externalID != "" {
		if user.ExternalID != nil && strings.TrimSpace(*user.ExternalID) != "" && strings.TrimSpace(*user.ExternalID) != externalID {
			return nil, errors.New("invalid username or password")
		}
		user.ExternalID = stringPtr(externalID)
		user.UpdatedAt = time.Now()
	}
	if s.enterprisePolicy.SyncRole && (external.Role == "admin" || external.Role == "user") && user.Role != external.Role {
		user.Role = external.Role
		user.UpdatedAt = time.Now()
	}
	if externalID != "" || s.enterprisePolicy.SyncRole {
		if err := s.userRepo.Update(user); err != nil {
			return nil, fmt.Errorf("failed to update enterprise user: %w", err)
		}
	}
	return user, nil
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
