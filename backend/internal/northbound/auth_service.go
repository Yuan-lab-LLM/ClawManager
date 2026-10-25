package northbound

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/config"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/utils"
)

type AuthService struct {
	repo            AuthRepository
	users           repository.UserRepository
	config          config.NorthboundConfig
	decryptor       *JWEDecryptor
	dummyHash       string
	accountLimiter  *FixedWindowLimiter
	runtimeSettings RuntimeSettingsProvider
}

type AuthRepository interface {
	CreateChallenge(*models.NorthboundAuthChallenge) error
	ClaimChallenge(context.Context, string, time.Time) (*models.NorthboundAuthChallenge, error)
	ConsumeChallenge(context.Context, string, time.Time) error
	CreateSession(*models.NorthboundSession) error
	GetSessionByID(string) (*models.NorthboundSession, error)
	GetActiveSessionByRefreshHash(string, time.Time) (*models.NorthboundSession, error)
	GetActiveSessionByPreviousRefreshHash(string) (*models.NorthboundSession, error)
	RotateSession(context.Context, string, string, string, time.Time, time.Time, time.Time) (bool, error)
	RevokeSession(context.Context, string, time.Time) error
}

type loginEnvelope struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	ChallengeID string `json:"challenge_id"`
	Nonce       string `json:"nonce"`
	ClientNonce string `json:"client_nonce"`
	IssuedAt    int64  `json:"issued_at"`
}

func NewAuthService(repo AuthRepository, users repository.UserRepository, cfg config.NorthboundConfig, decryptor *JWEDecryptor, providers ...RuntimeSettingsProvider) (*AuthService, error) {
	if repo == nil || users == nil || decryptor == nil {
		return nil, fmt.Errorf("northbound auth dependencies are required")
	}
	if err := requireStrongSecret("northbound JWT secret", cfg.JWTSecret); err != nil {
		return nil, err
	}
	if err := requireStrongSecret("northbound refresh token pepper", cfg.RefreshTokenPepper); err != nil {
		return nil, err
	}
	if cfg.ChallengeTTL <= 0 || cfg.AccessTokenTTL <= 0 || cfg.RefreshTokenTTL <= 0 {
		return nil, fmt.Errorf("northbound token TTL values must be positive")
	}
	dummyHash, err := utils.HashPassword("northbound-invalid-credential-placeholder")
	if err != nil {
		return nil, fmt.Errorf("initialize northbound credential verifier: %w", err)
	}
	service := &AuthService{
		repo: repo, users: users, config: cfg, decryptor: decryptor, dummyHash: dummyHash,
		accountLimiter: NewFixedWindowLimiter(5, time.Minute),
	}
	if len(providers) > 0 {
		service.runtimeSettings = providers[0]
	}
	return service, nil
}

func (s *AuthService) settings() *models.NorthboundAdminSettings {
	if s.runtimeSettings != nil {
		return s.runtimeSettings.Current()
	}
	return defaultRuntimeSettings(s.config)
}

func (s *AuthService) CreateChallenge(ctx context.Context, sourceIP string) (map[string]any, error) {
	challengeID, err := randomToken("nbc_", 18)
	if err != nil {
		return nil, err
	}
	nonce, err := randomToken("", 32)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	settings := s.settings()
	ip := strings.TrimSpace(sourceIP)
	item := &models.NorthboundAuthChallenge{
		ChallengeID: challengeID,
		NonceHash:   sha256Hex(nonce),
		KeyID:       s.decryptor.KeyID(),
		Status:      "issued",
		ExpiresAt:   now.Add(time.Duration(settings.ChallengeTTLSeconds) * time.Second),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if ip != "" {
		item.SourceIP = &ip
	}
	if err := s.repo.CreateChallenge(item); err != nil {
		return nil, err
	}
	return map[string]any{
		"challenge_id": challengeID,
		"nonce":        nonce,
		"expires_at":   item.ExpiresAt,
		"encryption": map[string]any{
			"kid":        s.decryptor.KeyID(),
			"alg":        "RSA-OAEP-256",
			"enc":        "A256GCM",
			"public_jwk": s.decryptor.PublicJWK(),
		},
	}, nil
}

func (s *AuthService) Login(ctx context.Context, challengeID, compactJWE, sourceIP, userAgent string) (*TokenResponse, error) {
	now := time.Now().UTC()
	settings := s.settings()
	challenge, err := s.repo.ClaimChallenge(ctx, strings.TrimSpace(challengeID), now)
	if err != nil {
		return nil, err
	}
	if challenge == nil {
		rejected := apiError(401, "INVALID_CREDENTIALS", "Invalid username or password", nil)
		rejected.AuditEvent = "northbound.auth.challenge_rejected"
		return nil, rejected
	}
	defer func() { _ = s.repo.ConsumeChallenge(context.Background(), challenge.ChallengeID, time.Now().UTC()) }()

	plaintext, err := s.decryptor.DecryptCompact(compactJWE)
	if err != nil {
		rejected := apiError(401, "INVALID_CREDENTIALS", "Invalid username or password", err)
		rejected.AuditEvent = "northbound.auth.jwe_rejected"
		return nil, rejected
	}
	defer clear(plaintext)
	var envelope loginEnvelope
	if err := json.Unmarshal(plaintext, &envelope); err != nil {
		return nil, apiError(401, "INVALID_CREDENTIALS", "Invalid username or password", err)
	}
	if envelope.ChallengeID != challenge.ChallengeID || strings.TrimSpace(envelope.ClientNonce) == "" || envelope.IssuedAt == 0 {
		return nil, apiError(401, "INVALID_CREDENTIALS", "Invalid username or password", nil)
	}
	accountKey := hmacHex(s.config.RefreshTokenPepper, strings.ToLower(strings.TrimSpace(envelope.Username)))
	if !s.accountLimiter.AllowLimit(accountKey, settings.AccountLoginRatePerMinute) {
		return nil, apiError(429, "RATE_LIMITED", "Too many login attempts", nil)
	}
	expectedNonce := challenge.NonceHash
	actualNonce := sha256Hex(envelope.Nonce)
	if subtle.ConstantTimeCompare([]byte(expectedNonce), []byte(actualNonce)) != 1 {
		return nil, apiError(401, "INVALID_CREDENTIALS", "Invalid username or password", nil)
	}
	issuedAt := time.Unix(envelope.IssuedAt, 0).UTC()
	if issuedAt.After(now.Add(30*time.Second)) || now.Sub(issuedAt) > time.Duration(settings.ChallengeTTLSeconds)*time.Second {
		return nil, apiError(401, "INVALID_CREDENTIALS", "Invalid username or password", nil)
	}
	user, err := s.users.GetByUsername(strings.TrimSpace(envelope.Username))
	if err != nil {
		return nil, err
	}
	passwordHash := s.dummyHash
	if user != nil {
		passwordHash = user.PasswordHash
	}
	passwordValid := utils.VerifyPassword(envelope.Password, passwordHash)
	if user == nil || !user.IsActive || !passwordValid {
		return nil, apiError(401, "INVALID_CREDENTIALS", "Invalid username or password", nil)
	}
	scopes := []string{
		ScopeLiteCreate,
		ScopeLiteRead,
		ScopeProCreate,
		ScopeProRead,
		ScopeLiteRestart,
		ScopeLiteReset,
		ScopeProRestart,
		ScopeProReset,
		ScopeShareLinkManage,
		ScopeShareLinkReset,
	}
	if s.runtimeSettings != nil {
		policy, policyErr := s.runtimeSettings.CallerPolicy(user.ID)
		if policyErr != nil {
			return nil, policyErr
		}
		if policy == nil && settings.RequireExplicitCallers {
			return nil, apiError(403, "CALLER_DISABLED", "This account is not allowed to use the northbound API", nil)
		}
		if policy != nil {
			if !policy.Enabled {
				return nil, apiError(403, "CALLER_DISABLED", "This account is not allowed to use the northbound API", nil)
			}
			scopes = policy.Scopes
		}
	}
	return s.createSession(user.ID, sourceIP, userAgent, scopes)
}

func (s *AuthService) createSession(userID int, sourceIP, userAgent string, scopes []string) (*TokenResponse, error) {
	now := time.Now().UTC()
	settings := s.settings()
	sessionID, err := randomToken("nbs_", 18)
	if err != nil {
		return nil, err
	}
	refresh, err := randomToken("nbr_", 32)
	if err != nil {
		return nil, err
	}
	scopesJSON, _ := json.Marshal(scopes)
	principal := Principal{UserID: userID, SessionID: sessionID, Scopes: scopes}
	accessTTL := time.Duration(settings.AccessTokenTTLSeconds) * time.Second
	refreshTTL := time.Duration(settings.RefreshTokenTTLSeconds) * time.Second
	access, accessExpiry, err := issueAccessToken(s.config.JWTSecret, principal, accessTTL)
	if err != nil {
		return nil, err
	}
	ip := strings.TrimSpace(sourceIP)
	ua := strings.TrimSpace(userAgent)
	item := &models.NorthboundSession{
		SessionID:           sessionID,
		UserID:              userID,
		RefreshTokenHash:    hmacHex(s.config.RefreshTokenPepper, refresh),
		RefreshTokenHistory: "[]",
		ScopesJSON:          string(scopesJSON),
		Status:              "active",
		AccessExpiresAt:     accessExpiry,
		RefreshExpiresAt:    now.Add(refreshTTL),
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if ip != "" {
		item.LastIP = &ip
	}
	if ua != "" {
		item.UserAgent = &ua
	}
	if err := s.repo.CreateSession(item); err != nil {
		return nil, err
	}
	return &TokenResponse{
		UserID:           userID,
		AccessToken:      access,
		RefreshToken:     refresh,
		TokenType:        "Bearer",
		ExpiresIn:        int64(accessTTL.Seconds()),
		RefreshExpiresIn: int64(refreshTTL.Seconds()),
		Scopes:           scopes,
		SessionID:        sessionID,
	}, nil
}

func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	now := time.Now().UTC()
	settings := s.settings()
	oldHash := hmacHex(s.config.RefreshTokenPepper, strings.TrimSpace(refreshToken))
	session, err := s.repo.GetActiveSessionByRefreshHash(oldHash, now)
	if err != nil {
		return nil, err
	}
	if session == nil {
		replayed, replayErr := s.repo.GetActiveSessionByPreviousRefreshHash(oldHash)
		if replayErr != nil {
			return nil, replayErr
		}
		if replayed != nil {
			_ = s.repo.RevokeSession(ctx, replayed.SessionID, now)
			rejected := apiError(401, "AUTH_INVALID", "Invalid or expired refresh token", nil)
			rejected.AuditEvent = "northbound.auth.refresh_replay"
			return nil, rejected
		}
		return nil, apiError(401, "AUTH_INVALID", "Invalid or expired refresh token", nil)
	}
	user, err := s.users.GetByID(session.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil || !user.IsActive {
		_ = s.repo.RevokeSession(ctx, session.SessionID, now)
		return nil, apiError(401, "AUTH_INVALID", "Invalid or expired refresh token", nil)
	}
	if s.runtimeSettings != nil {
		policy, policyErr := s.runtimeSettings.CallerPolicy(user.ID)
		if policyErr != nil {
			return nil, policyErr
		}
		if (policy == nil && settings.RequireExplicitCallers) || (policy != nil && !policy.Enabled) {
			_ = s.repo.RevokeSession(ctx, session.SessionID, now)
			return nil, apiError(403, "CALLER_DISABLED", "This account is not allowed to use the northbound API", nil)
		}
	}
	if user.UpdatedAt.After(session.CreatedAt) {
		_ = s.repo.RevokeSession(ctx, session.SessionID, now)
		return nil, apiError(401, "AUTH_INVALID", "Invalid or expired refresh token", nil)
	}
	var scopes []string
	if err := json.Unmarshal([]byte(session.ScopesJSON), &scopes); err != nil {
		return nil, err
	}
	newRefresh, err := randomToken("nbr_", 32)
	if err != nil {
		return nil, err
	}
	principal := Principal{UserID: session.UserID, SessionID: session.SessionID, Scopes: scopes}
	accessTTL := time.Duration(settings.AccessTokenTTLSeconds) * time.Second
	refreshTTL := time.Duration(settings.RefreshTokenTTLSeconds) * time.Second
	access, accessExpiry, err := issueAccessToken(s.config.JWTSecret, principal, accessTTL)
	if err != nil {
		return nil, err
	}
	refreshExpiry := now.Add(refreshTTL)
	rotated, err := s.repo.RotateSession(ctx, session.SessionID, oldHash, hmacHex(s.config.RefreshTokenPepper, newRefresh), accessExpiry, refreshExpiry, now)
	if err != nil {
		return nil, err
	}
	if !rotated {
		return nil, apiError(401, "AUTH_INVALID", "Invalid or expired refresh token", nil)
	}
	return &TokenResponse{
		UserID:           session.UserID,
		AccessToken:      access,
		RefreshToken:     newRefresh,
		TokenType:        "Bearer",
		ExpiresIn:        int64(accessTTL.Seconds()),
		RefreshExpiresIn: int64(refreshTTL.Seconds()),
		Scopes:           scopes,
		SessionID:        session.SessionID,
	}, nil
}

func (s *AuthService) AuthenticateAccess(encoded string) (*Principal, error) {
	claims, err := parseAccessToken(s.config.JWTSecret, encoded)
	if err != nil {
		return nil, apiError(401, "AUTH_INVALID", "Invalid or expired access token", err)
	}
	userID, err := strconv.Atoi(claims.Subject)
	if err != nil || userID <= 0 {
		return nil, apiError(401, "AUTH_INVALID", "Invalid or expired access token", err)
	}
	session, err := s.repo.GetSessionByID(claims.SessionID)
	if err != nil {
		return nil, err
	}
	if session == nil || session.Status != "active" || session.RefreshExpiresAt.Before(time.Now().UTC()) || session.UserID != userID {
		return nil, apiError(401, "AUTH_INVALID", "Invalid or expired access token", nil)
	}
	user, err := s.users.GetByID(userID)
	if err != nil {
		return nil, err
	}
	if user == nil || !user.IsActive || user.UpdatedAt.After(session.CreatedAt) {
		_ = s.repo.RevokeSession(context.Background(), claims.SessionID, time.Now().UTC())
		return nil, apiError(401, "AUTH_INVALID", "Invalid or expired access token", nil)
	}
	return &Principal{UserID: userID, SessionID: claims.SessionID, Scopes: claims.Scopes}, nil
}

func (s *AuthService) Logout(ctx context.Context, sessionID string) error {
	return s.repo.RevokeSession(ctx, sessionID, time.Now().UTC())
}

func (s *AuthService) CurrentUser(userID int) (*models.User, error) {
	return s.users.GetByID(userID)
}
