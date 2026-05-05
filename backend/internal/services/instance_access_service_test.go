package services

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestInstanceAccessServiceValidatesTokenAcrossServiceInstances(t *testing.T) {
	t.Setenv("INSTANCE_ACCESS_TOKEN_SECRET", "cluster-shared-secret")

	issuer := NewInstanceAccessService()
	validator := NewInstanceAccessService()

	token, err := issuer.GenerateToken(7, 42, "openclaw", "/api/v1/instances/42/proxy/", 3001, 5*time.Minute)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	validated, err := validator.ValidateToken(token.Token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}

	if validated.InstanceID != 42 {
		t.Fatalf("validated.InstanceID = %d, want 42", validated.InstanceID)
	}
	if validated.UserID != 7 {
		t.Fatalf("validated.UserID = %d, want 7", validated.UserID)
	}
	if validated.InstanceType != "openclaw" {
		t.Fatalf("validated.InstanceType = %q, want openclaw", validated.InstanceType)
	}
}

func TestInstanceAccessServiceRejectsExpiredSignedToken(t *testing.T) {
	t.Setenv("INSTANCE_ACCESS_TOKEN_SECRET", "cluster-shared-secret")

	service := NewInstanceAccessService()
	token, err := service.GenerateToken(7, 42, "openclaw", "/api/v1/instances/42/proxy/", 3001, -time.Second)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	if _, err := service.ValidateToken(token.Token); err == nil || err.Error() != "token expired" {
		t.Fatalf("ValidateToken() error = %v, want token expired", err)
	}
}

func TestInstanceAccessServiceFallsBackToLegacyTokens(t *testing.T) {
	t.Setenv("INSTANCE_ACCESS_TOKEN_SECRET", "cluster-shared-secret")

	service := NewInstanceAccessService()
	service.tokens["legacy-token"] = &AccessToken{
		Token:        "legacy-token",
		InstanceID:   11,
		UserID:       3,
		InstanceType: "ubuntu",
		TargetPort:   3001,
		AccessURL:    "/api/v1/instances/11/proxy/",
		ExpiresAt:    time.Now().Add(time.Minute),
		CreatedAt:    time.Now(),
	}

	validated, err := service.ValidateToken("legacy-token")
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}

	if validated.InstanceID != 11 {
		t.Fatalf("validated.InstanceID = %d, want 11", validated.InstanceID)
	}
}

func TestResolveInstanceAccessScopeDefaultsAndValidModes(t *testing.T) {
	desktopScope, err := ResolveInstanceAccessScope(42, "openclaw", "", 3001)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() default mode error = %v", err)
	}
	if desktopScope.AccessMode != AccessModeDesktop {
		t.Fatalf("default AccessMode = %q, want %q", desktopScope.AccessMode, AccessModeDesktop)
	}
	if desktopScope.TargetPort != 3001 {
		t.Fatalf("desktop TargetPort = %d, want 3001", desktopScope.TargetPort)
	}
	if desktopScope.RoutePrefix != "/api/v1/instances/42/proxy" {
		t.Fatalf("desktop RoutePrefix = %q", desktopScope.RoutePrefix)
	}

	controlScope, err := ResolveInstanceAccessScope(42, "openclaw", AccessModeControlUI, 3001)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() control-ui error = %v", err)
	}
	if controlScope.AccessMode != AccessModeControlUI {
		t.Fatalf("control-ui AccessMode = %q, want %q", controlScope.AccessMode, AccessModeControlUI)
	}
	if controlScope.TargetPort != DefaultControlUITargetPort {
		t.Fatalf("control-ui TargetPort = %d, want %d", controlScope.TargetPort, DefaultControlUITargetPort)
	}
	if controlScope.RoutePrefix != "/api/v1/instances/42/control-ui" {
		t.Fatalf("control-ui RoutePrefix = %q", controlScope.RoutePrefix)
	}

	for _, mode := range []string{"DESKTOP", "control_ui", "console", "desktop "} {
		if _, err := ResolveInstanceAccessScope(42, "openclaw", mode, 3001); !errors.Is(err, ErrInvalidAccessMode) {
			t.Fatalf("ResolveInstanceAccessScope(%q) error = %v, want ErrInvalidAccessMode", mode, err)
		}
	}
}

func TestResolveInstanceAccessScopeRejectsUnsupportedControlUIRuntime(t *testing.T) {
	_, err := ResolveInstanceAccessScope(42, "ubuntu", AccessModeControlUI, 3001)
	if !errors.Is(err, ErrUnsupportedAccessMode) {
		t.Fatalf("ResolveInstanceAccessScope() error = %v, want ErrUnsupportedAccessMode", err)
	}
}

func TestInstanceAccessServiceGeneratesScopedClaims(t *testing.T) {
	t.Setenv("INSTANCE_ACCESS_TOKEN_SECRET", "cluster-shared-secret")

	service := NewInstanceAccessService()
	scope, err := ResolveInstanceAccessScope(42, "openclaw", AccessModeControlUI, 3001)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() error = %v", err)
	}

	token, err := service.GenerateTokenForScope(7, 42, "openclaw", scope, 5*time.Minute)
	if err != nil {
		t.Fatalf("GenerateTokenForScope() error = %v", err)
	}

	validated, err := service.ValidateTokenForScope(token.Token, scope)
	if err != nil {
		t.Fatalf("ValidateTokenForScope() error = %v", err)
	}

	if validated.InstanceID != 42 {
		t.Fatalf("InstanceID = %d, want 42", validated.InstanceID)
	}
	if validated.UserID != 7 {
		t.Fatalf("UserID = %d, want 7", validated.UserID)
	}
	if validated.TokenType != InstanceAccessTokenType {
		t.Fatalf("TokenType = %q, want %q", validated.TokenType, InstanceAccessTokenType)
	}
	if validated.AccessMode != AccessModeControlUI {
		t.Fatalf("AccessMode = %q, want %q", validated.AccessMode, AccessModeControlUI)
	}
	if validated.TargetPort != DefaultControlUITargetPort {
		t.Fatalf("TargetPort = %d, want %d", validated.TargetPort, DefaultControlUITargetPort)
	}
	if validated.RoutePrefix != "/api/v1/instances/42/control-ui" {
		t.Fatalf("RoutePrefix = %q", validated.RoutePrefix)
	}
}

func TestInstanceAccessServiceRejectsCrossModeTokens(t *testing.T) {
	t.Setenv("INSTANCE_ACCESS_TOKEN_SECRET", "cluster-shared-secret")

	service := NewInstanceAccessService()
	desktopScope, err := ResolveInstanceAccessScope(42, "openclaw", AccessModeDesktop, 3001)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() desktop error = %v", err)
	}
	controlScope, err := ResolveInstanceAccessScope(42, "openclaw", AccessModeControlUI, 3001)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() control-ui error = %v", err)
	}

	desktopToken, err := service.GenerateTokenForScope(7, 42, "openclaw", desktopScope, 5*time.Minute)
	if err != nil {
		t.Fatalf("GenerateTokenForScope() desktop error = %v", err)
	}
	controlToken, err := service.GenerateTokenForScope(7, 42, "openclaw", controlScope, 5*time.Minute)
	if err != nil {
		t.Fatalf("GenerateTokenForScope() control-ui error = %v", err)
	}

	if _, err := service.ValidateTokenForScope(desktopToken.Token, controlScope); !errors.Is(err, ErrAccessScopeMismatch) {
		t.Fatalf("desktop token against control-ui scope error = %v, want ErrAccessScopeMismatch", err)
	}
	if _, err := service.ValidateTokenForScope(controlToken.Token, desktopScope); !errors.Is(err, ErrAccessScopeMismatch) {
		t.Fatalf("control-ui token against desktop scope error = %v, want ErrAccessScopeMismatch", err)
	}
}

func TestInstanceAccessServiceLegacySignedTokenIsDesktopOnly(t *testing.T) {
	t.Setenv("INSTANCE_ACCESS_TOKEN_SECRET", "cluster-shared-secret")

	service := NewInstanceAccessService()
	now := time.Now()
	legacyClaims := instanceAccessClaims{
		InstanceID:   42,
		UserID:       7,
		InstanceType: "openclaw",
		TargetPort:   3001,
		AccessURL:    "/api/v1/instances/42/proxy/",
		TokenType:    InstanceAccessTokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
		},
	}
	legacyToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, legacyClaims).SignedString([]byte("cluster-shared-secret"))
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}

	desktopScope, err := ResolveInstanceAccessScope(42, "openclaw", AccessModeDesktop, 3001)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() desktop error = %v", err)
	}
	controlScope, err := ResolveInstanceAccessScope(42, "openclaw", AccessModeControlUI, 3001)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() control-ui error = %v", err)
	}

	validated, err := service.ValidateTokenForScope(legacyToken, desktopScope)
	if err != nil {
		t.Fatalf("ValidateTokenForScope() desktop legacy error = %v", err)
	}
	if validated.AccessMode != AccessModeDesktop {
		t.Fatalf("legacy AccessMode = %q, want %q", validated.AccessMode, AccessModeDesktop)
	}

	if _, err := service.ValidateTokenForScope(legacyToken, controlScope); !errors.Is(err, ErrAccessScopeMismatch) {
		t.Fatalf("legacy token against control-ui scope error = %v, want ErrAccessScopeMismatch", err)
	}
}
