package northbound

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawreef/internal/config"
	"clawreef/internal/models"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type authRepositoryStub struct {
	challenge *models.NorthboundAuthChallenge
	session   *models.NorthboundSession
}

func (r *authRepositoryStub) CreateChallenge(item *models.NorthboundAuthChallenge) error {
	clone := *item
	r.challenge = &clone
	return nil
}

func (r *authRepositoryStub) ClaimChallenge(_ context.Context, id string, now time.Time) (*models.NorthboundAuthChallenge, error) {
	if r.challenge == nil || r.challenge.ChallengeID != id || r.challenge.Status != "issued" || !r.challenge.ExpiresAt.After(now) {
		return nil, nil
	}
	r.challenge.Status = "processing"
	clone := *r.challenge
	return &clone, nil
}

func (r *authRepositoryStub) ConsumeChallenge(_ context.Context, id string, now time.Time) error {
	if r.challenge != nil && r.challenge.ChallengeID == id {
		r.challenge.Status = "consumed"
		r.challenge.UsedAt = &now
	}
	return nil
}

func (r *authRepositoryStub) CreateSession(item *models.NorthboundSession) error {
	clone := *item
	r.session = &clone
	return nil
}

func (r *authRepositoryStub) GetSessionByID(id string) (*models.NorthboundSession, error) {
	if r.session == nil || r.session.SessionID != id {
		return nil, nil
	}
	clone := *r.session
	return &clone, nil
}

func (r *authRepositoryStub) GetActiveSessionByRefreshHash(hash string, now time.Time) (*models.NorthboundSession, error) {
	if r.session == nil || r.session.Status != "active" || r.session.RefreshTokenHash != hash || !r.session.RefreshExpiresAt.After(now) {
		return nil, nil
	}
	clone := *r.session
	return &clone, nil
}
func (r *authRepositoryStub) GetActiveSessionByPreviousRefreshHash(hash string) (*models.NorthboundSession, error) {
	if r.session == nil || r.session.Status != "active" || r.session.PreviousRefreshTokenHash == nil || *r.session.PreviousRefreshTokenHash != hash {
		return nil, nil
	}
	clone := *r.session
	return &clone, nil
}
func (r *authRepositoryStub) RotateSession(_ context.Context, sessionID, oldHash, newHash string, accessExpiry, refreshExpiry, now time.Time) (bool, error) {
	if r.session == nil || r.session.SessionID != sessionID || r.session.Status != "active" || r.session.RefreshTokenHash != oldHash {
		return false, nil
	}
	r.session.PreviousRefreshTokenHash = &oldHash
	r.session.RefreshTokenHash = newHash
	r.session.AccessExpiresAt = accessExpiry
	r.session.RefreshExpiresAt = refreshExpiry
	r.session.LastUsedAt = &now
	return true, nil
}
func (r *authRepositoryStub) RevokeSession(_ context.Context, sessionID string, now time.Time) error {
	if r.session != nil && r.session.SessionID == sessionID {
		r.session.Status = "revoked"
		r.session.RevokedAt = &now
	}
	return nil
}

type userRepositoryStub struct{ user *models.User }

func (r *userRepositoryStub) Create(*models.User) error { return nil }
func (r *userRepositoryStub) GetByID(id int) (*models.User, error) {
	if r.user == nil || r.user.ID != id {
		return nil, nil
	}
	clone := *r.user
	return &clone, nil
}
func (r *userRepositoryStub) GetByUsername(username string) (*models.User, error) {
	if r.user == nil || r.user.Username != username {
		return nil, nil
	}
	clone := *r.user
	return &clone, nil
}
func (r *userRepositoryStub) GetByAuthProviderUsername(_ string, username string) (*models.User, error) {
	return r.GetByUsername(username)
}
func (r *userRepositoryStub) GetByEmail(string) (*models.User, error) { return nil, nil }
func (r *userRepositoryStub) GetByLoginAlias(string, string) (*models.User, error) {
	return nil, nil
}
func (r *userRepositoryStub) GetByExternalIdentity(string, string) (*models.User, error) {
	return nil, nil
}
func (r *userRepositoryStub) Update(*models.User) error            { return nil }
func (r *userRepositoryStub) Delete(int) error                     { return nil }
func (r *userRepositoryStub) List(int, int) ([]models.User, error) { return nil, nil }
func (r *userRepositoryStub) Count() (int, error)                  { return 0, nil }
func (r *userRepositoryStub) CountByAuthProviderUsername(string, string) (int, error) {
	return 0, nil
}

func TestJWELoginAuthenticatesExistingBcryptUser(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	decryptor, _ := NewJWEDecryptor(key, "login-key")
	passwordHash, err := utils.HashPassword("existing-password")
	if err != nil {
		t.Fatalf("hash existing password: %v", err)
	}
	repo := &authRepositoryStub{}
	users := &userRepositoryStub{user: &models.User{
		ID: 7, Username: "existing-user", PasswordHash: passwordHash, IsActive: true,
		CreatedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now().Add(-time.Hour),
	}}
	cfg := config.NorthboundConfig{
		JWTSecret: strings.Repeat("j", 32), RefreshTokenPepper: strings.Repeat("p", 32),
		ChallengeTTL: time.Minute, AccessTokenTTL: 30 * time.Minute, RefreshTokenTTL: 24 * time.Hour,
	}
	service, err := NewAuthService(repo, users, cfg, decryptor)
	if err != nil {
		t.Fatalf("create auth service: %v", err)
	}
	challenge, err := service.CreateChallenge(context.Background(), "192.0.2.1")
	if err != nil {
		t.Fatalf("create challenge: %v", err)
	}
	challengeID := challenge["challenge_id"].(string)
	nonce := challenge["nonce"].(string)
	credential, _ := json.Marshal(loginEnvelope{
		Username: "existing-user", Password: "existing-password", ChallengeID: challengeID,
		Nonce: nonce, ClientNonce: "client-nonce", IssuedAt: time.Now().Unix(),
	})
	compact := encryptTestJWE(t, &key.PublicKey, "login-key", credential)
	outerBody, _ := json.Marshal(map[string]string{"challenge_id": challengeID, "credential_jwe": compact})
	if strings.Contains(string(outerBody), "existing-user") || strings.Contains(string(outerBody), "existing-password") {
		t.Fatal("outer login request must not contain plaintext credentials")
	}
	result, err := service.Login(context.Background(), challengeID, compact, "192.0.2.1", "test-client")
	if err != nil {
		t.Fatalf("JWE login failed: %v", err)
	}
	if result.UserID != 7 || result.AccessToken == "" || result.RefreshToken == "" || repo.session == nil {
		t.Fatalf("incomplete login result: %+v", result)
	}
	requiredScopes := map[string]bool{
		ScopeProCreate:       false,
		ScopeProRead:         false,
		ScopeLiteRestart:     false,
		ScopeLiteReset:       false,
		ScopeProRestart:      false,
		ScopeProReset:        false,
		ScopeShareLinkReset:  false,
		ScopeShareLinkManage: false,
	}
	for _, scope := range result.Scopes {
		if _, required := requiredScopes[scope]; required {
			requiredScopes[scope] = true
		}
	}
	for scope, present := range requiredScopes {
		if !present {
			t.Fatalf("login scopes = %v, missing %s", result.Scopes, scope)
		}
	}
	if repo.challenge.Status != "consumed" {
		t.Fatalf("challenge status = %q, want consumed", repo.challenge.Status)
	}
	if _, err := parseAccessToken(cfg.JWTSecret, result.AccessToken); err != nil {
		t.Fatalf("issued access token is invalid: %v", err)
	}
	if _, err := service.Login(context.Background(), challengeID, compact, "192.0.2.1", "test-client"); err == nil {
		t.Fatal("consumed challenge must not be replayable")
	}
}

func TestRefreshTokenReplayRevokesSession(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	decryptor, _ := NewJWEDecryptor(key, "login-key")
	repo := &authRepositoryStub{}
	users := &userRepositoryStub{user: &models.User{
		ID: 9, Username: "active-user", IsActive: true,
		CreatedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now().Add(-time.Hour),
	}}
	cfg := config.NorthboundConfig{
		JWTSecret: strings.Repeat("j", 32), RefreshTokenPepper: strings.Repeat("p", 32),
		ChallengeTTL: time.Minute, AccessTokenTTL: 30 * time.Minute, RefreshTokenTTL: 24 * time.Hour,
	}
	service, err := NewAuthService(repo, users, cfg, decryptor)
	if err != nil {
		t.Fatalf("create auth service: %v", err)
	}
	initial, err := service.createSession(9, "192.0.2.1", "test-client", []string{ScopeLiteRead})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := service.Refresh(context.Background(), initial.RefreshToken); err != nil {
		t.Fatalf("rotate refresh token: %v", err)
	}
	if _, err := service.Refresh(context.Background(), initial.RefreshToken); err == nil {
		t.Fatal("replayed refresh token must be rejected")
	}
	if repo.session.Status != "revoked" {
		t.Fatalf("session status = %q, want revoked", repo.session.Status)
	}
}

func TestJWECompactRoundTripAndTamperRejection(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	decryptor, err := NewJWEDecryptor(key, "login-key")
	if err != nil {
		t.Fatalf("create decryptor: %v", err)
	}
	plaintext := []byte(`{"username":"existing","password":"not-on-wire"}`)
	compact := encryptTestJWE(t, &key.PublicKey, "login-key", plaintext)
	decrypted, err := decryptor.DecryptCompact(compact)
	if err != nil {
		t.Fatalf("decrypt compact JWE: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Fatalf("unexpected plaintext: %q", decrypted)
	}

	parts := strings.Split(compact, ".")
	tag, err := rawURL.DecodeString(parts[4])
	if err != nil {
		t.Fatalf("decode authentication tag: %v", err)
	}
	tag[0] ^= 0xff
	parts[4] = rawURL.EncodeToString(tag)
	if _, err := decryptor.DecryptCompact(strings.Join(parts, ".")); err == nil {
		t.Fatal("tampered JWE must be rejected")
	}
}

func TestJWERejectsAlgorithmDowngrade(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	decryptor, _ := NewJWEDecryptor(key, "login-key")
	compact := encryptTestJWEWithAlgorithm(t, &key.PublicKey, "login-key", "RSA-OAEP", []byte("secret"))
	if _, err := decryptor.DecryptCompact(compact); err == nil {
		t.Fatal("JWE algorithm downgrade must be rejected")
	}
}

func TestNorthboundTokensAreTypeAndAudienceSeparated(t *testing.T) {
	secret := strings.Repeat("s", 32)
	principal := Principal{UserID: 42, SessionID: "nbs_test", Scopes: []string{ScopeLiteRead}}
	access, _, err := issueAccessToken(secret, principal, time.Minute)
	if err != nil {
		t.Fatalf("issue access token: %v", err)
	}
	claims, err := parseAccessToken(secret, access)
	if err != nil || claims.Subject != "42" {
		t.Fatalf("parse access token: claims=%+v err=%v", claims, err)
	}
	if _, err := parseInternalToken(secret, access); err == nil {
		t.Fatal("northbound access token must not be accepted as an internal token")
	}

	webLike := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "42", "iss": northboundIssuer, "aud": northboundAudience,
		"exp": time.Now().Add(time.Minute).Unix(),
	})
	encoded, _ := webLike.SignedString([]byte(secret))
	if _, err := parseAccessToken(secret, encoded); err == nil {
		t.Fatal("token without northbound type must be rejected")
	}
}

func TestRejectSuspiciousRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestContext(), RejectSuspiciousRequest())
	router.POST("/api/northbound/v1/auth/challenge", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.POST("/api/northbound/v1/auth/logout", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.POST("/api/northbound/v1/lite-instances", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	for _, requestPath := range []string{
		"/api/northbound/v1/auth/challenge",
		"/api/northbound/v1/auth/logout",
	} {
		request := httptest.NewRequest(http.MethodPost, requestPath, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("bodyless POST %s status = %d, want 204", requestPath, response.Code)
		}
	}

	request := httptest.NewRequest(http.MethodPost, "/api/northbound/v1/lite-instances", strings.NewReader("{}"))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("missing content-type status: %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/northbound/v1/lite-instances", strings.NewReader("{}"))
	request.Header.Set("Content-Type", "text/plain")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("unexpected content-type status: %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/northbound/v1/%252e%252e/secret", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("double-encoded path status = %d, want 404", response.Code)
	}
}

func encryptTestJWE(t *testing.T, publicKey *rsa.PublicKey, keyID string, plaintext []byte) string {
	t.Helper()
	return encryptTestJWEWithAlgorithm(t, publicKey, keyID, "RSA-OAEP-256", plaintext)
}

func encryptTestJWEWithAlgorithm(t *testing.T, publicKey *rsa.PublicKey, keyID, algorithm string, plaintext []byte) string {
	t.Helper()
	header, _ := json.Marshal(jweHeader{KeyID: keyID, Algorithm: algorithm, Encryption: "A256GCM"})
	protected := rawURL.EncodeToString(header)
	cek := make([]byte, 32)
	if _, err := rand.Read(cek); err != nil {
		t.Fatalf("generate CEK: %v", err)
	}
	encryptedKey, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, publicKey, cek, nil)
	if err != nil {
		t.Fatalf("encrypt CEK: %v", err)
	}
	block, _ := aes.NewCipher(cek)
	var gcm cipher.AEAD
	gcm, _ = cipher.NewGCM(block)
	iv := make([]byte, gcm.NonceSize())
	_, _ = rand.Read(iv)
	sealed := gcm.Seal(nil, iv, plaintext, []byte(protected))
	ciphertext := sealed[:len(sealed)-gcm.Overhead()]
	tag := sealed[len(sealed)-gcm.Overhead():]
	return strings.Join([]string{
		protected,
		rawURL.EncodeToString(encryptedKey),
		rawURL.EncodeToString(iv),
		rawURL.EncodeToString(ciphertext),
		rawURL.EncodeToString(tag),
	}, ".")
}
