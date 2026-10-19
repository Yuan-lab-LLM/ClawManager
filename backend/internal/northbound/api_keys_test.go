package northbound

import (
	"clawreef/internal/models"
	"clawreef/internal/nbkeys"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAPIKeyIdentityAndPolicyIntersection(t *testing.T) {
	key, raw, err := nbkeys.New(12, "A", []string{ScopeProRead, ScopeProRestart}, time.Now().Add(time.Hour), 1)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal([]nbkeys.Key{key})
	document := string(encoded)
	policy := &models.NorthboundCallerPolicy{UserID: 12, Enabled: true, Scopes: []string{ScopeProRead}, APIKeysJSON: &document}
	provider := staticRuntimeSettings{callers: map[int]*models.NorthboundCallerPolicy{12: policy}}
	users := &userRepositoryStub{user: &models.User{ID: 12, IsActive: true}}
	service := &AuthService{runtimeSettings: provider, users: users}
	principal, err := service.AuthenticateAPIKey(raw)
	if err != nil || principal.UserID != 12 || principal.SessionID != "" || principal.KeyID != key.ID || !principal.HasScope(ScopeProRead) || principal.HasScope(ScopeProRestart) {
		t.Fatalf("wrong identity: %v %v", principal, err)
	}
	core := &CoreService{runtimeSettings: provider, users: users}
	if err := core.ValidatePrincipal(*principal); err != nil {
		t.Fatal(err)
	}
	forged := *principal
	forged.Scopes = append(forged.Scopes, ScopeProDelete)
	if err := core.ValidatePrincipal(forged); err == nil {
		t.Fatal("Core accepted elevated scope")
	}
	policy.Enabled = false
	if _, err := service.AuthenticateAPIKey(raw); err == nil {
		t.Fatal("disabled caller authenticated")
	}
	if err := core.ValidatePrincipal(*principal); err == nil {
		t.Fatal("Core accepted disabled caller")
	}
	policy.Enabled = true
	key.Enabled = false
	encoded, _ = json.Marshal([]nbkeys.Key{key})
	document = string(encoded)
	if _, err := service.AuthenticateAPIKey(raw); err == nil {
		t.Fatal("disabled key authenticated")
	}
	if err := core.ValidatePrincipal(*principal); err == nil {
		t.Fatal("Core accepted revoked key")
	}
}

func TestAPIKeyMiddlewareRejectsAmbiguousCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RejectSuspiciousRequest())
	r.GET("/api/northbound/v1/pro-instances", NewAuthHandler(&AuthService{}).Middleware(), func(c *gin.Context) { c.Status(204) })
	for _, headers := range []map[string][]string{
		{"X-Api-Key": {""}},
		{"X-Api-Key": {"bad"}, "Authorization": {"Bearer legacy"}},
		{"X-Api-Key": {"one", "two"}},
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/northbound/v1/pro-instances", nil)
		for k, v := range headers {
			req.Header[k] = v
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != 400 && rec.Code != 401 {
			t.Fatalf("unexpected status %d", rec.Code)
		}
	}
}

func TestAPIKeyInternalTokenBinding(t *testing.T) {
	secret := strings.Repeat("s", 32)
	p := Principal{UserID: 12, AuthType: "api_key", KeyID: "public-id", Scopes: []string{ScopeProRead}}
	encoded, err := issueInternalToken(secret, p, "req-test")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := parseInternalToken(secret, encoded)
	if err != nil || claims.AuthType != "api_key" || claims.KeyID != p.KeyID || claims.SessionID != "" {
		t.Fatal("lost key identity", err)
	}
}
