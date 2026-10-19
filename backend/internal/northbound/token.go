package northbound

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	northboundIssuer           = "clawmanager"
	northboundAudience         = "clawmanager-northbound"
	northboundAccessType       = "northbound_access"
	northboundInternalAudience = "clawmanager-internal-northbound"
	northboundInternalType     = "northbound_internal"
)

type accessClaims struct {
	TokenType string   `json:"typ"`
	SessionID string   `json:"sid"`
	Scopes    []string `json:"scope"`
	jwt.RegisteredClaims
}

type internalClaims struct {
	AuthType  string   `json:"auth_type,omitempty"`
	KeyID     string   `json:"key_id,omitempty"`
	TokenType string   `json:"typ"`
	UserID    int      `json:"user_id"`
	SessionID string   `json:"sid"`
	Scopes    []string `json:"scope"`
	RequestID string   `json:"request_id"`
	jwt.RegisteredClaims
}

func issueAccessToken(secret string, principal Principal, ttl time.Duration) (string, time.Time, error) {
	now := time.Now().UTC()
	expires := now.Add(ttl)
	jti, err := randomToken("nbj_", 16)
	if err != nil {
		return "", time.Time{}, err
	}
	claims := accessClaims{
		TokenType: northboundAccessType,
		SessionID: principal.SessionID,
		Scopes:    principal.Scopes,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    northboundIssuer,
			Subject:   strconv.Itoa(principal.UserID),
			Audience:  jwt.ClaimStrings{northboundAudience},
			ExpiresAt: jwt.NewNumericDate(expires),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-5 * time.Second)),
			ID:        jti,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	encoded, err := token.SignedString([]byte(secret))
	return encoded, expires, err
}

func parseAccessToken(secret, encoded string) (*accessClaims, error) {
	claims := &accessClaims{}
	token, err := jwt.ParseWithClaims(encoded, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer(northboundIssuer), jwt.WithAudience(northboundAudience))
	if err != nil || !token.Valid || claims.TokenType != northboundAccessType {
		return nil, fmt.Errorf("invalid northbound access token")
	}
	return claims, nil
}

func issueInternalToken(secret string, principal Principal, requestID string) (string, error) {
	now := time.Now().UTC()
	claims := internalClaims{
		AuthType:  principal.AuthType,
		KeyID:     principal.KeyID,
		TokenType: northboundInternalType,
		UserID:    principal.UserID,
		SessionID: principal.SessionID,
		Scopes:    principal.Scopes,
		RequestID: strings.TrimSpace(requestID),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    northboundIssuer,
			Subject:   strconv.Itoa(principal.UserID),
			Audience:  jwt.ClaimStrings{northboundInternalAudience},
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-5 * time.Second)),
		},
	}
	encoded, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	return encoded, err
}

func parseInternalToken(secret, encoded string) (*internalClaims, error) {
	claims := &internalClaims{}
	token, err := jwt.ParseWithClaims(encoded, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer(northboundIssuer), jwt.WithAudience(northboundInternalAudience))
	if err != nil || !token.Valid || claims.TokenType != northboundInternalType || claims.UserID <= 0 || claims.Subject != strconv.Itoa(claims.UserID) {
		return nil, fmt.Errorf("invalid northbound internal token")
	}
	return claims, nil
}
