package services

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	_ "time/tzdata"

	"clawreef/internal/config"

	"github.com/golang-jwt/jwt/v5"
)

const (
	ieiTimestampLayout = "2006-01-02 15:04:05"
	ieiSessionIssuer   = "clawmanager-ieisystem"
	ieiSessionAudience = "clawmanager-ieisystem-portal"
	ieiSessionType     = "iei_sso_session"
)

var (
	ErrIEISystemDisabled     = errors.New("IEI system SSO is disabled")
	ErrInvalidIEISystemToken = errors.New("IEI system token is invalid or expired")
	ErrInvalidIEISession     = errors.New("IEI system session is invalid or expired")
)

// IEISession is the validated identity used throughout the IEI portal. Email
// is normalized for case-insensitive owner matching.
type IEISession struct {
	Email     string
	SessionID string
	Token     string
	ExpiresAt time.Time
}

type ieiSessionClaims struct {
	Email     string `json:"email"`
	SessionID string `json:"session_id"`
	TokenType string `json:"token_type"`
	jwt.RegisteredClaims
}

// IEISSOService implements the integration contract documented by the unified
// platform: standard Base64, AES-128-CBC, PKCS5/PKCS7 padding and plaintext
// "email+yyyy-MM-dd HH:mm:ss". A successful external token is exchanged for a
// separate short-lived signed session so the AES token is not reused.
type IEISSOService struct {
	cfg      config.IEISystemConfig
	key      []byte
	iv       []byte
	location *time.Location
	now      func() time.Time
}

func NewIEISSOService(cfg config.IEISystemConfig) (*IEISSOService, error) {
	service := &IEISSOService{cfg: cfg, now: time.Now}
	if !cfg.Enabled {
		return service, nil
	}
	if len([]byte(cfg.AESKey)) != aes.BlockSize {
		return nil, fmt.Errorf("IEISYSTEM_SSO_KEY must contain exactly 16 UTF-8 bytes")
	}
	if len([]byte(cfg.AESIV)) != aes.BlockSize {
		return nil, fmt.Errorf("IEISYSTEM_SSO_IV must contain exactly 16 UTF-8 bytes")
	}
	if len([]byte(cfg.SessionSecret)) < 32 {
		return nil, fmt.Errorf("IEISYSTEM_SESSION_SECRET must contain at least 32 UTF-8 bytes")
	}
	if cfg.TokenTTL <= 0 || cfg.TokenTTL > 24*time.Hour {
		return nil, fmt.Errorf("IEISYSTEM_SSO_TOKEN_TTL must be greater than zero and no more than 24h")
	}
	if cfg.SessionTTL <= 0 || cfg.SessionTTL > 24*time.Hour {
		return nil, fmt.Errorf("IEISYSTEM_SESSION_TTL must be greater than zero and no more than 24h")
	}
	location, err := time.LoadLocation(strings.TrimSpace(cfg.Timezone))
	if err != nil {
		return nil, fmt.Errorf("invalid IEISYSTEM_SSO_TIMEZONE: %w", err)
	}
	service.key = []byte(cfg.AESKey)
	service.iv = []byte(cfg.AESIV)
	service.location = location
	return service, nil
}

func (s *IEISSOService) Enabled() bool {
	return s != nil && s.cfg.Enabled
}

func (s *IEISSOService) ExchangeExternalToken(rawToken string) (*IEISession, error) {
	if !s.Enabled() {
		return nil, ErrIEISystemDisabled
	}
	email, err := s.validateExternalToken(rawToken)
	if err != nil {
		return nil, err
	}
	return s.issueSession(email)
}

// RenewSession extends a valid ClawManager-owned IEI portal session without
// consulting IEI again. The session ID is preserved so instance capabilities
// bound to the active browser session remain valid while they are rotated.
func (s *IEISSOService) RenewSession(rawToken string) (*IEISession, error) {
	current, err := s.ValidateSession(rawToken)
	if err != nil {
		return nil, err
	}
	return s.issueSessionWithID(current.Email, current.SessionID)
}

func (s *IEISSOService) validateExternalToken(rawToken string) (string, error) {
	// URL query parsing converts an unescaped '+' to a space. Accepting that
	// representation keeps compatibility with the platform's example URL while
	// still requiring a valid AES ciphertext and timestamp.
	encoded := strings.ReplaceAll(strings.TrimSpace(rawToken), " ", "+")
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return "", ErrInvalidIEISystemToken
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", ErrInvalidIEISystemToken
	}
	plaintext := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, s.iv).CryptBlocks(plaintext, ciphertext)
	plaintext, err = removePKCS7Padding(plaintext, aes.BlockSize)
	if err != nil {
		return "", ErrInvalidIEISystemToken
	}

	value := string(plaintext)
	separator := strings.LastIndex(value, "+")
	if separator <= 0 || separator == len(value)-1 {
		return "", ErrInvalidIEISystemToken
	}
	email, ok := normalizeIEIEmail(value[:separator])
	if !ok {
		return "", ErrInvalidIEISystemToken
	}
	issuedAt, err := time.ParseInLocation(ieiTimestampLayout, value[separator+1:], s.location)
	if err != nil {
		return "", ErrInvalidIEISystemToken
	}
	now := s.now().In(s.location)
	if issuedAt.After(now.Add(5*time.Second)) || now.Sub(issuedAt) > s.cfg.TokenTTL {
		return "", ErrInvalidIEISystemToken
	}
	return email, nil
}

func normalizeIEIEmail(value string) (string, bool) {
	email := strings.TrimSpace(value)
	parsed, err := mail.ParseAddress(email)
	if err != nil || !strings.EqualFold(parsed.Address, email) {
		return "", false
	}
	return strings.ToLower(parsed.Address), true
}

func removePKCS7Padding(value []byte, blockSize int) ([]byte, error) {
	if len(value) == 0 || len(value)%blockSize != 0 {
		return nil, errors.New("invalid padded value")
	}
	padding := int(value[len(value)-1])
	if padding == 0 || padding > blockSize || padding > len(value) {
		return nil, errors.New("invalid padding")
	}
	for _, current := range value[len(value)-padding:] {
		if int(current) != padding {
			return nil, errors.New("invalid padding")
		}
	}
	return value[:len(value)-padding], nil
}

func (s *IEISSOService) issueSession(email string) (*IEISession, error) {
	randomID := make([]byte, 24)
	if _, err := rand.Read(randomID); err != nil {
		return nil, fmt.Errorf("generate IEI session id: %w", err)
	}
	return s.issueSessionWithID(email, hex.EncodeToString(randomID))
}

func (s *IEISSOService) issueSessionWithID(email, sessionID string) (*IEISession, error) {
	now := s.now()
	expiresAt := now.Add(s.cfg.SessionTTL)
	claims := ieiSessionClaims{
		Email:     email,
		SessionID: sessionID,
		TokenType: ieiSessionType,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    ieiSessionIssuer,
			Subject:   email,
			Audience:  jwt.ClaimStrings{ieiSessionAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-5 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.SessionSecret))
	if err != nil {
		return nil, fmt.Errorf("sign IEI session: %w", err)
	}
	return &IEISession{Email: email, SessionID: sessionID, Token: token, ExpiresAt: expiresAt}, nil
}

func (s *IEISSOService) ValidateSession(rawToken string) (*IEISession, error) {
	if !s.Enabled() {
		return nil, ErrIEISystemDisabled
	}
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, ErrInvalidIEISession
	}
	claims := &ieiSessionClaims{}
	parsed, err := jwt.ParseWithClaims(
		rawToken,
		claims,
		func(token *jwt.Token) (interface{}, error) {
			if token.Method != jwt.SigningMethodHS256 {
				return nil, ErrInvalidIEISession
			}
			return []byte(s.cfg.SessionSecret), nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(ieiSessionIssuer),
		jwt.WithAudience(ieiSessionAudience),
		jwt.WithTimeFunc(s.now),
	)
	if err != nil || !parsed.Valid || claims.TokenType != ieiSessionType || claims.SessionID == "" {
		return nil, ErrInvalidIEISession
	}
	email, ok := normalizeIEIEmail(claims.Email)
	if !ok || !strings.EqualFold(claims.Subject, email) || claims.ExpiresAt == nil {
		return nil, ErrInvalidIEISession
	}
	return &IEISession{
		Email:     email,
		SessionID: claims.SessionID,
		Token:     rawToken,
		ExpiresAt: claims.ExpiresAt.Time,
	}, nil
}
