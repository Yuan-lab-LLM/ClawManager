package services

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestInstanceAccessServiceValidatesTokenAcrossServiceInstances(t *testing.T) {
	t.Setenv("INSTANCE_ACCESS_TOKEN_SECRET", "cluster-shared-secret")

	issuer := NewInstanceAccessService()
	validator := NewInstanceAccessService()

	token, err := issuer.GenerateToken(7, 7, 42, "openclaw", "/api/v1/instances/42/proxy/", 3001, 5*time.Minute)
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
	if validated.OwnerID != 7 {
		t.Fatalf("validated.OwnerID = %d, want 7", validated.OwnerID)
	}
	if validated.InstanceType != "openclaw" {
		t.Fatalf("validated.InstanceType = %q, want openclaw", validated.InstanceType)
	}
}

func TestInstanceAccessServiceRejectsExpiredSignedToken(t *testing.T) {
	t.Setenv("INSTANCE_ACCESS_TOKEN_SECRET", "cluster-shared-secret")

	service := NewInstanceAccessService()
	token, err := service.GenerateToken(7, 7, 42, "openclaw", "/api/v1/instances/42/proxy/", 3001, -time.Second)
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

// Test 1: Token roundtrip with OwnerID — admin (caller=2) accessing user 3's instance
func TestGenerateToken_AdminAccessingOtherUsersInstance_OwnerIDPreserved(t *testing.T) {
	t.Setenv("INSTANCE_ACCESS_TOKEN_SECRET", "cluster-shared-secret")

	service := NewInstanceAccessService()
	token, err := service.GenerateToken(2, 3, 4, "openclaw", "/api/v1/instances/4/proxy/", 3001, 5*time.Minute)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	if token.UserID != 2 {
		t.Fatalf("token.UserID = %d, want 2 (caller)", token.UserID)
	}
	if token.OwnerID != 3 {
		t.Fatalf("token.OwnerID = %d, want 3 (owner)", token.OwnerID)
	}

	validated, err := service.ValidateToken(token.Token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}

	if validated.UserID != 2 {
		t.Fatalf("validated.UserID = %d, want 2", validated.UserID)
	}
	if validated.OwnerID != 3 {
		t.Fatalf("validated.OwnerID = %d, want 3", validated.OwnerID)
	}
}

// Test 2: Legacy JWT without owner_id claim — should succeed with OwnerID=0
func TestValidateToken_LegacyJWTWithoutOwnerID_ReturnsOwnerIDZero(t *testing.T) {
	t.Setenv("INSTANCE_ACCESS_TOKEN_SECRET", "cluster-shared-secret")

	service := NewInstanceAccessService()

	// Hand-sign a JWT without the owner_id claim to simulate a pre-fix token
	now := time.Now()
	claims := jwt.MapClaims{
		"instance_id":   42,
		"user_id":       7,
		"instance_type": "openclaw",
		"target_port":   3001,
		"access_url":    "/api/v1/instances/42/proxy/",
		"token_type":    "instance_access",
		"iat":           jwt.NewNumericDate(now),
		"exp":           jwt.NewNumericDate(now.Add(5 * time.Minute)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte("cluster-shared-secret"))
	if err != nil {
		t.Fatalf("failed to sign legacy token: %v", err)
	}

	validated, err := service.ValidateToken(tokenString)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}

	if validated.UserID != 7 {
		t.Fatalf("validated.UserID = %d, want 7", validated.UserID)
	}
	if validated.OwnerID != 0 {
		t.Fatalf("validated.OwnerID = %d, want 0 (legacy fallback)", validated.OwnerID)
	}
}

// Test 3: Caller equals owner — both fields should be the same
func TestGenerateToken_CallerEqualsOwner_BothFieldsMatch(t *testing.T) {
	t.Setenv("INSTANCE_ACCESS_TOKEN_SECRET", "cluster-shared-secret")

	service := NewInstanceAccessService()
	token, err := service.GenerateToken(3, 3, 4, "ubuntu", "/api/v1/instances/4/proxy/", 3001, 5*time.Minute)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	if token.UserID != 3 {
		t.Fatalf("token.UserID = %d, want 3", token.UserID)
	}
	if token.OwnerID != 3 {
		t.Fatalf("token.OwnerID = %d, want 3", token.OwnerID)
	}

	validated, err := service.ValidateToken(token.Token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}

	if validated.UserID != validated.OwnerID {
		t.Fatalf("validated.UserID (%d) != validated.OwnerID (%d)", validated.UserID, validated.OwnerID)
	}
}
