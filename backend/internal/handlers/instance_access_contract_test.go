package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type stubInstanceService struct {
	instance *models.Instance
}

func (s *stubInstanceService) Create(userID int, req services.CreateInstanceRequest) (*models.Instance, error) {
	return nil, nil
}

func (s *stubInstanceService) GetByID(id int) (*models.Instance, error) {
	if s.instance == nil || s.instance.ID != id {
		return nil, nil
	}
	return s.instance, nil
}

func (s *stubInstanceService) GetByUserID(userID int, offset, limit int) ([]models.Instance, int, error) {
	return nil, 0, nil
}

func (s *stubInstanceService) GetVisibleInstances(userID int, userRole string, offset, limit int) ([]models.Instance, int, error) {
	return nil, 0, nil
}

func (s *stubInstanceService) Start(instanceID int) error {
	return nil
}

func (s *stubInstanceService) Stop(instanceID int) error {
	return nil
}

func (s *stubInstanceService) Restart(instanceID int) error {
	return nil
}

func (s *stubInstanceService) Delete(instanceID int) error {
	return nil
}

func (s *stubInstanceService) Update(instanceID int, req services.UpdateInstanceRequest) error {
	return nil
}

func (s *stubInstanceService) GetInstanceStatus(instanceID int) (*services.InstanceStatus, error) {
	return nil, nil
}

func (s *stubInstanceService) ForceSyncInstance(instanceID int) error {
	return nil
}

type accessHandlerTestHarness struct {
	router  *gin.Engine
	handler *InstanceHandler
}

func newAccessHandlerTestHarness(t *testing.T, instance *models.Instance, userID int, userRole string) accessHandlerTestHarness {
	t.Helper()
	t.Setenv("INSTANCE_ACCESS_TOKEN_SECRET", "access-contract-test-secret")
	gin.SetMode(gin.TestMode)

	accessService := services.NewInstanceAccessService()
	handler := &InstanceHandler{
		instanceService: &stubInstanceService{instance: instance},
		accessService:   accessService,
		proxyService:    services.NewInstanceProxyService(accessService),
	}

	router := gin.New()
	router.POST("/api/v1/instances/:id/access", func(c *gin.Context) {
		c.Set("userID", userID)
		c.Set("userRole", userRole)
		handler.GenerateAccessToken(c)
	})
	router.Any("/api/v1/instances/:id/proxy", handler.ProxyInstance)
	router.Any("/api/v1/instances/:id/proxy/*path", handler.ProxyInstance)
	router.Any("/api/v1/instances/:id/control-ui", handler.ProxyControlUIInstance)
	router.Any("/api/v1/instances/:id/control-ui/*path", handler.ProxyControlUIInstance)

	return accessHandlerTestHarness{router: router, handler: handler}
}

func performAccessRequest(t *testing.T, harness accessHandlerTestHarness, target string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(http.MethodPost, target, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	harness.router.ServeHTTP(w, req)
	return w
}

type accessResponseEnvelope struct {
	Success bool                  `json:"success"`
	Error   string                `json:"error"`
	Data    accessResponsePayload `json:"data"`
}

type accessResponsePayload struct {
	Token      string    `json:"token"`
	AccessURL  string    `json:"access_url"`
	ProxyURL   string    `json:"proxy_url"`
	AccessMode string    `json:"access_mode"`
	TargetPort int32     `json:"target_port"`
	ExpiresAt  time.Time `json:"expires_at"`
}

func decodeAccessResponse(t *testing.T, w *httptest.ResponseRecorder) accessResponseEnvelope {
	t.Helper()
	var envelope accessResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("response JSON decode error = %v", err)
	}
	return envelope
}

func runningInstance(id int, ownerID int, instanceType string) *models.Instance {
	return &models.Instance{
		ID:        id,
		UserID:    ownerID,
		Name:      "access-contract-instance",
		Type:      instanceType,
		Status:    "running",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

func setCookieHeaders(w *httptest.ResponseRecorder) []string {
	return w.Result().Header.Values("Set-Cookie")
}

func TestGenerateAccessTokenMissingModeDefaultsToDesktop(t *testing.T) {
	harness := newAccessHandlerTestHarness(t, runningInstance(42, 7, "openclaw"), 7, "user")

	w := performAccessRequest(t, harness, "/api/v1/instances/42/access", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	envelope := decodeAccessResponse(t, w)
	if envelope.Data.AccessMode != services.AccessModeDesktop {
		t.Fatalf("access_mode = %q, want %q", envelope.Data.AccessMode, services.AccessModeDesktop)
	}
	if envelope.Data.TargetPort != 3001 {
		t.Fatalf("target_port = %d, want 3001", envelope.Data.TargetPort)
	}
	if envelope.Data.AccessURL != "/api/v1/instances/42/proxy/" {
		t.Fatalf("access_url = %q", envelope.Data.AccessURL)
	}
	if envelope.Data.ProxyURL != "/api/v1/instances/42/proxy/" {
		t.Fatalf("proxy_url was not the expected tokenless desktop proxy URL")
	}

	validated, err := harness.handler.accessService.ValidateToken(envelope.Data.Token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	if validated.AccessMode != services.AccessModeDesktop {
		t.Fatalf("token AccessMode = %q, want %q", validated.AccessMode, services.AccessModeDesktop)
	}
	if validated.RoutePrefix != "/api/v1/instances/42/proxy" {
		t.Fatalf("token RoutePrefix = %q", validated.RoutePrefix)
	}

	cookies := setCookieHeaders(w)
	if len(cookies) != 1 {
		t.Fatalf("Set-Cookie count = %d, want 1", len(cookies))
	}
	if got := cookies[0]; !bytes.Contains([]byte(got), []byte("instance_access_42=")) ||
		!bytes.Contains([]byte(got), []byte("Path=/api/v1/instances/42/proxy")) ||
		!bytes.Contains([]byte(got), []byte("HttpOnly")) {
		t.Fatalf("desktop cookie missing expected route scope")
	}
}

func TestGenerateAccessTokenAcceptsOnlyExactValidModes(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		wantStatus int
		wantMode   string
	}{
		{name: "desktop", target: "/api/v1/instances/42/access?mode=desktop", wantStatus: http.StatusOK, wantMode: services.AccessModeDesktop},
		{name: "control-ui", target: "/api/v1/instances/42/access?mode=control-ui", wantStatus: http.StatusOK, wantMode: services.AccessModeControlUI},
		{name: "uppercase invalid", target: "/api/v1/instances/42/access?mode=DESKTOP", wantStatus: http.StatusBadRequest},
		{name: "underscore invalid", target: "/api/v1/instances/42/access?mode=control_ui", wantStatus: http.StatusBadRequest},
		{name: "trailing-space invalid", target: "/api/v1/instances/42/access?mode=desktop+", wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := newAccessHandlerTestHarness(t, runningInstance(42, 7, "openclaw"), 7, "user")
			w := performAccessRequest(t, harness, tt.target, nil)
			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			envelope := decodeAccessResponse(t, w)
			if tt.wantStatus != http.StatusOK {
				if envelope.Data.Token != "" {
					t.Fatalf("error response unexpectedly included token")
				}
				if len(setCookieHeaders(w)) != 0 {
					t.Fatalf("error response unexpectedly set route cookie")
				}
				return
			}
			if envelope.Data.AccessMode != tt.wantMode {
				t.Fatalf("access_mode = %q, want %q", envelope.Data.AccessMode, tt.wantMode)
			}
		})
	}
}

func TestGenerateAccessTokenInvalidModeDoesNotIssueTokenOrCookie(t *testing.T) {
	harness := newAccessHandlerTestHarness(t, runningInstance(42, 7, "openclaw"), 7, "user")

	w := performAccessRequest(t, harness, "/api/v1/instances/42/access?mode=console", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	envelope := decodeAccessResponse(t, w)
	if envelope.Data.Token != "" {
		t.Fatalf("invalid mode response unexpectedly included token")
	}
	if len(setCookieHeaders(w)) != 0 {
		t.Fatalf("invalid mode response unexpectedly set route cookie")
	}
}

func TestGenerateAccessTokenRejectsJSONBodyMode(t *testing.T) {
	tests := []struct {
		name   string
		target string
		body   []byte
	}{
		{
			name:   "body-only mode",
			target: "/api/v1/instances/42/access",
			body:   []byte(`{"mode":"control-ui"}`),
		},
		{
			name:   "body-query conflict",
			target: "/api/v1/instances/42/access?mode=desktop",
			body:   []byte(`{"mode":"control-ui"}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := newAccessHandlerTestHarness(t, runningInstance(42, 7, "openclaw"), 7, "user")
			w := performAccessRequest(t, harness, tt.target, tt.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
			}
			envelope := decodeAccessResponse(t, w)
			if envelope.Data.Token != "" {
				t.Fatalf("JSON body mode response unexpectedly included token")
			}
			if len(setCookieHeaders(w)) != 0 {
				t.Fatalf("JSON body mode response unexpectedly set route cookie")
			}
		})
	}
}

func TestGenerateAccessTokenRejectsControlUIForUnsupportedRuntimeWithoutTokenOrCookie(t *testing.T) {
	harness := newAccessHandlerTestHarness(t, runningInstance(42, 7, "ubuntu"), 7, "user")

	w := performAccessRequest(t, harness, "/api/v1/instances/42/access?mode=control-ui", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	envelope := decodeAccessResponse(t, w)
	if envelope.Data.Token != "" {
		t.Fatalf("unsupported runtime response unexpectedly included token")
	}
	if len(setCookieHeaders(w)) != 0 {
		t.Fatalf("unsupported runtime response unexpectedly set route cookie")
	}
}

func TestGenerateAccessTokenAuthorizationRunsBeforeTokenIssuance(t *testing.T) {
	for _, mode := range []string{services.AccessModeDesktop, services.AccessModeControlUI} {
		t.Run(mode, func(t *testing.T) {
			harness := newAccessHandlerTestHarness(t, runningInstance(42, 8, "openclaw"), 7, "user")

			w := performAccessRequest(t, harness, "/api/v1/instances/42/access?mode="+mode, nil)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
			}
			envelope := decodeAccessResponse(t, w)
			if envelope.Data.Token != "" {
				t.Fatalf("forbidden response unexpectedly included token")
			}
			if len(setCookieHeaders(w)) != 0 {
				t.Fatalf("forbidden response unexpectedly set route cookie")
			}
		})
	}
}

func TestGenerateAccessTokenUsesServerSelectedTargetPort(t *testing.T) {
	harness := newAccessHandlerTestHarness(t, runningInstance(42, 7, "openclaw"), 7, "user")

	w := performAccessRequest(t, harness, "/api/v1/instances/42/access?mode=control-ui&target_port=12345", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	envelope := decodeAccessResponse(t, w)
	if envelope.Data.TargetPort != services.DefaultControlUITargetPort {
		t.Fatalf("response target_port = %d, want server-selected %d", envelope.Data.TargetPort, services.DefaultControlUITargetPort)
	}

	scope, err := services.ResolveInstanceAccessScope(42, "openclaw", services.AccessModeControlUI, 3001)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() error = %v", err)
	}
	validated, err := harness.handler.accessService.ValidateTokenForScope(envelope.Data.Token, scope)
	if err != nil {
		t.Fatalf("ValidateTokenForScope() error = %v", err)
	}
	if validated.TargetPort != services.DefaultControlUITargetPort {
		t.Fatalf("token target_port = %d, want server-selected %d", validated.TargetPort, services.DefaultControlUITargetPort)
	}
}

func TestGenerateAccessTokenAdminCanRequestControlUI(t *testing.T) {
	harness := newAccessHandlerTestHarness(t, runningInstance(42, 8, "openclaw"), 7, "admin")

	w := performAccessRequest(t, harness, "/api/v1/instances/42/access?mode=control-ui", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	envelope := decodeAccessResponse(t, w)
	if envelope.Data.AccessMode != services.AccessModeControlUI {
		t.Fatalf("access_mode = %q, want %q", envelope.Data.AccessMode, services.AccessModeControlUI)
	}
}

func TestGenerateAccessTokenRequestContextIsUsedOnlyForAuthorizationSetup(t *testing.T) {
	harness := newAccessHandlerTestHarness(t, runningInstance(42, 7, "openclaw"), 7, "user")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/instances/42/access?mode=desktop", nil)
	req = req.WithContext(context.Background())
	w := httptest.NewRecorder()
	harness.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestProxyInstanceRejectsControlUITokenBeforeCookiePromotion(t *testing.T) {
	harness := newAccessHandlerTestHarness(t, runningInstance(42, 7, "openclaw"), 7, "user")
	scope, err := services.ResolveInstanceAccessScope(42, "openclaw", services.AccessModeControlUI, 3001)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() error = %v", err)
	}
	token, err := harness.handler.accessService.GenerateTokenForScope(7, 42, "openclaw", scope, 5*time.Minute)
	if err != nil {
		t.Fatalf("GenerateTokenForScope() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/42/proxy/?token="+token.Token, nil)
	w := httptest.NewRecorder()
	harness.router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
	if len(setCookieHeaders(w)) != 0 {
		t.Fatalf("control-ui token was unexpectedly promoted to desktop proxy cookie")
	}
}

func TestProxyControlUIRejectsDesktopAndLegacyTokensBeforeCookiePromotion(t *testing.T) {
	harness := newAccessHandlerTestHarness(t, runningInstance(42, 7, "openclaw"), 7, "user")
	desktopScope, err := services.ResolveInstanceAccessScope(42, "openclaw", services.AccessModeDesktop, 3001)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() desktop error = %v", err)
	}
	desktopToken, err := harness.handler.accessService.GenerateTokenForScope(7, 42, "openclaw", desktopScope, 5*time.Minute)
	if err != nil {
		t.Fatalf("GenerateTokenForScope() desktop error = %v", err)
	}
	legacyToken := signLegacyDesktopAccessToken(t, 42, 7)

	tests := []struct {
		name  string
		token string
	}{
		{name: "desktop scoped token", token: desktopToken.Token},
		{name: "legacy desktop-only signed token", token: legacyToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/42/control-ui/?token="+tt.token, nil)
			w := httptest.NewRecorder()
			harness.router.ServeHTTP(w, req)

			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusForbidden)
			}
			if len(setCookieHeaders(w)) != 0 {
				t.Fatalf("desktop token was unexpectedly promoted to control-ui cookie")
			}
		})
	}
}

func TestProxyControlUIQueryTokenPromotionSetsOnlyControlUICookie(t *testing.T) {
	harness := newAccessHandlerTestHarness(t, runningInstance(42, 7, "openclaw"), 7, "user")
	controlScope, err := services.ResolveInstanceAccessScope(42, "openclaw", services.AccessModeControlUI, 3001)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() control-ui error = %v", err)
	}
	controlToken, err := harness.handler.accessService.GenerateTokenForScope(7, 42, "openclaw", controlScope, 5*time.Minute)
	if err != nil {
		t.Fatalf("GenerateTokenForScope() control-ui error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/42/control-ui/?token="+controlToken.Token, nil)
	w := httptest.NewRecorder()
	harness.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
	cookies := setCookieHeaders(w)
	if len(cookies) != 1 {
		t.Fatalf("Set-Cookie count = %d, want 1", len(cookies))
	}
	if got := cookies[0]; !strings.Contains(got, "instance_control_ui_access_42=") ||
		!strings.Contains(got, "Path=/api/v1/instances/42/control-ui") ||
		strings.Contains(got, "instance_access_42=") {
		t.Fatalf("control-ui query token did not set only the control-ui route cookie: %q", got)
	}
}

func TestProxyControlUIReportsUpstreamFailureWithoutDesktopFallback(t *testing.T) {
	harness := newAccessHandlerTestHarness(t, runningInstance(42, 7, "openclaw"), 7, "user")
	controlScope, err := services.ResolveInstanceAccessScope(42, "openclaw", services.AccessModeControlUI, 3001)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() control-ui error = %v", err)
	}
	controlToken, err := harness.handler.accessService.GenerateTokenForScope(7, 42, "openclaw", controlScope, 5*time.Minute)
	if err != nil {
		t.Fatalf("GenerateTokenForScope() control-ui error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/42/control-ui/chat?session=main", nil)
	req.AddCookie(&http.Cookie{
		Name:  controlScope.CookieName,
		Value: controlToken.Token,
		Path:  controlScope.CookiePath,
	})
	w := httptest.NewRecorder()
	harness.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Failed to proxy request") {
		t.Fatalf("body = %q, want explicit upstream proxy failure", body)
	}
	if strings.Contains(body, "/api/v1/instances/42/proxy") {
		t.Fatalf("body suggests desktop proxy fallback: %q", body)
	}
}

func signLegacyDesktopAccessToken(t *testing.T, instanceID int, userID int) string {
	t.Helper()
	now := time.Now()
	claims := jwt.MapClaims{
		"instance_id":   instanceID,
		"user_id":       userID,
		"instance_type": "openclaw",
		"target_port":   3001,
		"access_url":    "/api/v1/instances/42/proxy/",
		"token_type":    services.InstanceAccessTokenType,
		"iat":           now.Unix(),
		"exp":           now.Add(5 * time.Minute).Unix(),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("access-contract-test-secret"))
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}
	return token
}
