package services

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// AccessToken represents a temporary access token for instance
type AccessToken struct {
	Token        string    `json:"token"`
	InstanceID   int       `json:"instance_id"`
	UserID       int       `json:"user_id"`
	InstanceType string    `json:"instance_type"`
	TargetPort   int32     `json:"target_port"`
	AccessMode   string    `json:"access_mode"`
	RoutePrefix  string    `json:"route_prefix"`
	TokenType    string    `json:"token_type"`
	AccessURL    string    `json:"access_url"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
}

const (
	AccessModeDesktop          = "desktop"
	AccessModeControlUI        = "control-ui"
	InstanceAccessTokenType    = "instance_access"
	DefaultDesktopTargetPort   = int32(3001)
	DefaultControlUITargetPort = int32(18789)
)

var (
	ErrInvalidAccessMode     = errors.New("invalid access mode")
	ErrUnsupportedAccessMode = errors.New("access mode is not supported for instance type")
	ErrAccessScopeMismatch   = errors.New("access token scope does not match route")
)

type InstanceAccessScope struct {
	InstanceID  int
	AccessMode  string
	TargetPort  int32
	RoutePrefix string
	AccessURL   string
	CookieName  string
	CookiePath  string
}

// InstanceAccessService manages instance access tokens
type InstanceAccessService struct {
	tokens   map[string]*AccessToken
	mu       sync.RWMutex
	secret   string
	stopChan chan struct{}
}

type instanceAccessClaims struct {
	InstanceID   int    `json:"instance_id"`
	UserID       int    `json:"user_id"`
	InstanceType string `json:"instance_type"`
	TargetPort   int32  `json:"target_port"`
	AccessMode   string `json:"access_mode,omitempty"`
	RoutePrefix  string `json:"route_prefix,omitempty"`
	AccessURL    string `json:"access_url"`
	TokenType    string `json:"token_type"`
	jwt.RegisteredClaims
}

func ResolveInstanceAccessScope(instanceID int, instanceType string, mode string, desktopTargetPort int32) (InstanceAccessScope, error) {
	switch mode {
	case "":
		mode = AccessModeDesktop
	case AccessModeDesktop, AccessModeControlUI:
	default:
		return InstanceAccessScope{}, ErrInvalidAccessMode
	}

	switch mode {
	case AccessModeDesktop:
		if desktopTargetPort == 0 {
			desktopTargetPort = DefaultDesktopTargetPort
		}
		routePrefix := fmt.Sprintf("/api/v1/instances/%d/proxy", instanceID)
		return InstanceAccessScope{
			InstanceID:  instanceID,
			AccessMode:  AccessModeDesktop,
			TargetPort:  desktopTargetPort,
			RoutePrefix: routePrefix,
			AccessURL:   routePrefix + "/",
			CookieName:  fmt.Sprintf("instance_access_%d", instanceID),
			CookiePath:  routePrefix,
		}, nil
	case AccessModeControlUI:
		if !strings.EqualFold(instanceType, "openclaw") {
			return InstanceAccessScope{}, ErrUnsupportedAccessMode
		}
		routePrefix := fmt.Sprintf("/api/v1/instances/%d/control-ui", instanceID)
		return InstanceAccessScope{
			InstanceID:  instanceID,
			AccessMode:  AccessModeControlUI,
			TargetPort:  DefaultControlUITargetPort,
			RoutePrefix: routePrefix,
			AccessURL:   routePrefix + "/",
			CookieName:  fmt.Sprintf("instance_control_ui_access_%d", instanceID),
			CookiePath:  routePrefix,
		}, nil
	default:
		return InstanceAccessScope{}, ErrInvalidAccessMode
	}
}

// NewInstanceAccessService creates a new instance access service
func NewInstanceAccessService() *InstanceAccessService {
	service := &InstanceAccessService{
		tokens:   make(map[string]*AccessToken),
		secret:   getInstanceAccessTokenSecret(),
		stopChan: make(chan struct{}),
	}

	// Start cleanup goroutine
	go service.cleanupExpiredTokens()

	return service
}

// GenerateToken generates a new access token for an instance
func (s *InstanceAccessService) GenerateToken(userID, instanceID int, instanceType string, accessURL string, targetPort int32, duration time.Duration) (*AccessToken, error) {
	if targetPort == 0 {
		targetPort = DefaultDesktopTargetPort
	}
	routePrefix := strings.TrimRight(accessURL, "/")
	if routePrefix == "" {
		routePrefix = fmt.Sprintf("/api/v1/instances/%d/proxy", instanceID)
		accessURL = routePrefix + "/"
	}
	scope := InstanceAccessScope{
		InstanceID:  instanceID,
		AccessMode:  AccessModeDesktop,
		TargetPort:  targetPort,
		RoutePrefix: routePrefix,
		AccessURL:   accessURL,
		CookieName:  fmt.Sprintf("instance_access_%d", instanceID),
		CookiePath:  routePrefix,
	}
	return s.GenerateTokenForScope(userID, instanceID, instanceType, scope, duration)
}

func (s *InstanceAccessService) GenerateTokenForScope(userID, instanceID int, instanceType string, scope InstanceAccessScope, duration time.Duration) (*AccessToken, error) {
	now := time.Now()
	expiresAt := now.Add(duration)
	if scope.InstanceID != 0 && scope.InstanceID != instanceID {
		return nil, fmt.Errorf("access scope does not match instance")
	}
	if scope.AccessMode == "" {
		scope.AccessMode = AccessModeDesktop
	}
	if scope.AccessMode != AccessModeDesktop && scope.AccessMode != AccessModeControlUI {
		return nil, ErrInvalidAccessMode
	}
	if scope.AccessMode == AccessModeControlUI && !strings.EqualFold(instanceType, "openclaw") {
		return nil, ErrUnsupportedAccessMode
	}
	if scope.TargetPort == 0 {
		return nil, fmt.Errorf("access scope target port is required")
	}
	if scope.RoutePrefix == "" {
		return nil, fmt.Errorf("access scope route prefix is required")
	}
	if scope.AccessURL == "" {
		scope.AccessURL = strings.TrimRight(scope.RoutePrefix, "/") + "/"
	}

	claims := instanceAccessClaims{
		InstanceID:   instanceID,
		UserID:       userID,
		InstanceType: instanceType,
		TargetPort:   scope.TargetPort,
		AccessMode:   scope.AccessMode,
		RoutePrefix:  scope.RoutePrefix,
		AccessURL:    scope.AccessURL,
		TokenType:    InstanceAccessTokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(s.secret))
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	accessToken := &AccessToken{
		Token:        tokenString,
		InstanceID:   instanceID,
		UserID:       userID,
		InstanceType: instanceType,
		TargetPort:   scope.TargetPort,
		AccessMode:   scope.AccessMode,
		RoutePrefix:  scope.RoutePrefix,
		TokenType:    InstanceAccessTokenType,
		AccessURL:    scope.AccessURL,
		ExpiresAt:    expiresAt,
		CreatedAt:    now,
	}

	return accessToken, nil
}

// ValidateToken validates an access token
func (s *InstanceAccessService) ValidateToken(token string) (*AccessToken, error) {
	accessToken, err := s.validateSignedToken(token)
	if err == nil {
		return accessToken, nil
	}

	legacyToken, legacyErr := s.validateLegacyToken(token)
	if legacyErr == nil {
		return legacyToken, nil
	}

	return nil, err
}

func (s *InstanceAccessService) ValidateTokenForScope(token string, scope InstanceAccessScope) (*AccessToken, error) {
	accessToken, err := s.ValidateToken(token)
	if err != nil {
		return nil, err
	}
	return validateAccessTokenScope(accessToken, scope)
}

func validateAccessTokenScope(accessToken *AccessToken, scope InstanceAccessScope) (*AccessToken, error) {
	if accessToken == nil {
		return nil, fmt.Errorf("invalid token")
	}
	if accessToken.TokenType != InstanceAccessTokenType {
		return nil, fmt.Errorf("invalid token")
	}
	if accessToken.InstanceID != scope.InstanceID {
		return nil, ErrAccessScopeMismatch
	}
	if accessToken.UserID <= 0 {
		return nil, fmt.Errorf("invalid token")
	}
	mode := accessToken.AccessMode
	if mode == "" {
		mode = AccessModeDesktop
	}
	if mode != scope.AccessMode {
		return nil, ErrAccessScopeMismatch
	}
	if accessToken.TargetPort != scope.TargetPort {
		return nil, ErrAccessScopeMismatch
	}
	if accessToken.RoutePrefix != "" && accessToken.RoutePrefix != scope.RoutePrefix {
		return nil, ErrAccessScopeMismatch
	}
	if scope.AccessMode == AccessModeControlUI && accessToken.RoutePrefix == "" {
		return nil, ErrAccessScopeMismatch
	}

	accessToken.AccessMode = mode
	return accessToken, nil
}

func (s *InstanceAccessService) validateSignedToken(token string) (*AccessToken, error) {
	parsed, err := jwt.ParseWithClaims(token, &instanceAccessClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(s.secret), nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, fmt.Errorf("token expired")
		}
		return nil, fmt.Errorf("invalid token")
	}

	claims, ok := parsed.Claims.(*instanceAccessClaims)
	if !ok || !parsed.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	if claims.TokenType != InstanceAccessTokenType || claims.AccessURL == "" {
		return nil, fmt.Errorf("invalid token")
	}
	accessMode := claims.AccessMode
	if accessMode == "" {
		accessMode = AccessModeDesktop
	} else if accessMode != AccessModeDesktop && accessMode != AccessModeControlUI {
		return nil, fmt.Errorf("invalid token")
	}

	expiresAt := time.Time{}
	if claims.ExpiresAt != nil {
		expiresAt = claims.ExpiresAt.Time
	}
	createdAt := time.Time{}
	if claims.IssuedAt != nil {
		createdAt = claims.IssuedAt.Time
	}

	return &AccessToken{
		Token:        token,
		InstanceID:   claims.InstanceID,
		UserID:       claims.UserID,
		InstanceType: claims.InstanceType,
		TargetPort:   claims.TargetPort,
		AccessMode:   accessMode,
		RoutePrefix:  claims.RoutePrefix,
		TokenType:    claims.TokenType,
		AccessURL:    claims.AccessURL,
		ExpiresAt:    expiresAt,
		CreatedAt:    createdAt,
	}, nil
}

func (s *InstanceAccessService) validateLegacyToken(token string) (*AccessToken, error) {
	s.mu.RLock()
	accessToken, exists := s.tokens[token]
	s.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("invalid token")
	}

	if time.Now().After(accessToken.ExpiresAt) {
		s.mu.Lock()
		delete(s.tokens, token)
		s.mu.Unlock()
		return nil, fmt.Errorf("token expired")
	}

	legacy := *accessToken
	if legacy.AccessMode == "" {
		legacy.AccessMode = AccessModeDesktop
	}
	if legacy.TokenType == "" {
		legacy.TokenType = InstanceAccessTokenType
	}
	return &legacy, nil
}

// RevokeToken revokes an access token
func (s *InstanceAccessService) RevokeToken(token string) {
	s.mu.Lock()
	delete(s.tokens, token)
	s.mu.Unlock()
}

// GetAccessURL generates access URL for an instance
func (s *InstanceAccessService) GetAccessURL(instanceID int, instanceType string, podIP string, podName string) string {
	// Generate access URL based on instance type
	switch instanceType {
	case "openclaw":
		// OpenClaw desktop typically uses VNC or web interface
		if podIP != "" {
			return fmt.Sprintf("https://%s:3001/", podIP)
		}
	case "ubuntu", "debian", "centos":
		// Linux desktops typically use noVNC or similar
		if podIP != "" {
			return fmt.Sprintf("http://%s:6901/vnc.html", podIP)
		}
	default:
		// Default VNC access
		if podIP != "" {
			return fmt.Sprintf("http://%s:6080/vnc.html", podIP)
		}
	}

	// Fallback to pod name based URL (for ingress/routing scenarios)
	if podName != "" {
		return fmt.Sprintf("/access/instance/%d", instanceID)
	}

	return ""
}

// GetAccessURLWithEndpoint generates access URL using the provided endpoint (nodeIP:port or direct IP)
func (s *InstanceAccessService) GetAccessURLWithEndpoint(instanceID int, instanceType string, endpoint string) string {
	if endpoint == "" {
		return ""
	}

	// Generate access URL based on instance type
	switch instanceType {
	case "openclaw":
		// OpenClaw desktop typically uses VNC or web interface
		return fmt.Sprintf("https://%s/", endpoint)
	case "ubuntu", "debian", "centos":
		// Linux desktops typically use noVNC or similar
		return fmt.Sprintf("http://%s/vnc.html", endpoint)
	default:
		// Default VNC access
		return fmt.Sprintf("http://%s/vnc.html", endpoint)
	}
}

// GetProxyURL generates a proxied access URL
func (s *InstanceAccessService) GetProxyURL(instanceID int, token string) string {
	return fmt.Sprintf("/api/v1/instances/%d/access?token=%s", instanceID, token)
}

// cleanupExpiredTokens periodically removes expired tokens
func (s *InstanceAccessService) cleanupExpiredTokens() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			now := time.Now()
			s.mu.Lock()
			for token, accessToken := range s.tokens {
				if now.After(accessToken.ExpiresAt) {
					delete(s.tokens, token)
				}
			}
			s.mu.Unlock()
		}
	}
}

// Stop terminates the background cleanup goroutine.
func (s *InstanceAccessService) Stop() {
	close(s.stopChan)
}

// GetActiveTokenCount returns the number of active tokens
func (s *InstanceAccessService) GetActiveTokenCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tokens)
}

func getInstanceAccessTokenSecret() string {
	if secret := os.Getenv("INSTANCE_ACCESS_TOKEN_SECRET"); secret != "" {
		return secret
	}
	if secret := os.Getenv("JWT_SECRET"); secret != "" {
		return secret
	}
	return "clawreef-instance-access-secret-change-in-production"
}
