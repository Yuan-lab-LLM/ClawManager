package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services"

	"github.com/gin-gonic/gin"
)

func TestWorkspaceArchiveMaxMiB(t *testing.T) {
	t.Setenv(workspaceArchiveMaxMiBEnv, "")
	if got := workspaceArchiveMaxMiB(); got != defaultWorkspaceArchiveMaxMiB {
		t.Fatalf("expected default archive limit %d MiB, got %d", defaultWorkspaceArchiveMaxMiB, got)
	}

	t.Setenv(workspaceArchiveMaxMiBEnv, "750")
	if got := workspaceArchiveMaxMiB(); got != 750 {
		t.Fatalf("expected env archive limit 750 MiB, got %d", got)
	}

	t.Setenv(workspaceArchiveMaxMiBEnv, "0")
	if got := workspaceArchiveMaxMiB(); got != defaultWorkspaceArchiveMaxMiB {
		t.Fatalf("expected invalid archive limit to fall back to %d MiB, got %d", defaultWorkspaceArchiveMaxMiB, got)
	}

	t.Setenv(workspaceArchiveMaxMiBEnv, "not-a-number")
	if got := workspaceArchiveMaxMiB(); got != defaultWorkspaceArchiveMaxMiB {
		t.Fatalf("expected unparsable archive limit to fall back to %d MiB, got %d", defaultWorkspaceArchiveMaxMiB, got)
	}
}

func TestDesktopAccessUpstreamSkipsDirectProxyForRuntimeGateway(t *testing.T) {
	t.Setenv(desktopDirectProxyEnv, "true")
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		instance *models.Instance
	}{
		{
			name: "gateway runtime type",
			instance: &models.Instance{
				ID:          50,
				UserID:      1,
				Type:        "openclaw",
				RuntimeType: "gateway",
			},
		},
		{
			name: "lite instance mode",
			instance: &models.Instance{
				ID:           51,
				UserID:       1,
				Type:         "openclaw",
				InstanceMode: services.InstanceModeLite,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/instances/50/access", nil)

			upstream, directEnabled := (&InstanceHandler{}).desktopAccessUpstream(c, tt.instance, 3001)
			if upstream != "" || directEnabled {
				t.Fatalf("desktopAccessUpstream() = upstream %q direct %t, want control-plane fallback", upstream, directEnabled)
			}
		})
	}
}

func TestBuildUserSafeInstanceStatusHidesRuntimeSchedulingDetails(t *testing.T) {
	podName := "runtime-openclaw-abc"
	podNamespace := "clawmanager-system"
	podIP := "10.42.0.12"
	startedAt := time.Now().UTC()
	workspacePath := "/workspaces/openclaw/user-45/instance-123"
	instance := &models.Instance{
		ID:                  123,
		Type:                "openclaw",
		RuntimeType:         "gateway",
		Status:              "running",
		WorkspacePath:       &workspacePath,
		WorkspaceUsageBytes: 123456,
	}
	status := &services.InstanceStatus{
		InstanceID:   123,
		Status:       "running",
		PodName:      &podName,
		PodNamespace: &podNamespace,
		PodIP:        &podIP,
		PodStatus:    "Running",
		StartedAt:    &startedAt,
	}

	payload := buildUserSafeInstanceStatus(instance, status)

	for _, forbiddenKey := range []string{"pod_name", "pod_namespace", "pod_ip", "pod_status", "gateway_port", "capacity", "node_name"} {
		if _, exists := payload[forbiddenKey]; exists {
			t.Fatalf("user-safe status exposed %q: %#v", forbiddenKey, payload)
		}
	}
	if payload["availability"] != "available" {
		t.Fatalf("availability = %v, want available", payload["availability"])
	}
	if payload["agent_type"] != "openclaw" {
		t.Fatalf("agent_type = %v, want openclaw", payload["agent_type"])
	}
	if payload["workspace_usage_bytes"] != int64(123456) {
		t.Fatalf("workspace_usage_bytes = %v, want 123456", payload["workspace_usage_bytes"])
	}
}

func TestShortExternalAccessProxyPath(t *testing.T) {
	tests := []struct {
		name        string
		requestPath string
		want        string
	}{
		{name: "entry without slash", requestPath: "/s/abc123", want: "/api/v1/instances/71/proxy/"},
		{name: "entry with slash", requestPath: "/s/abc123/", want: "/api/v1/instances/71/proxy/"},
		{name: "asset", requestPath: "/s/abc123/assets/index.js", want: "/api/v1/instances/71/proxy/assets/index.js"},
		{name: "nested", requestPath: "/s/abc123/apps/openclaw/settings", want: "/api/v1/instances/71/proxy/apps/openclaw/settings"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shortExternalAccessProxyPath(tt.requestPath, "abc123", 71); got != tt.want {
				t.Fatalf("shortExternalAccessProxyPath(%q) = %q, want %q", tt.requestPath, got, tt.want)
			}
		})
	}
}

func TestShortExternalAccessEntryRedirectTarget(t *testing.T) {
	tests := []struct {
		name          string
		method        string
		requestPath   string
		code          string
		canonicalPath string
		want          string
	}{
		{
			name:          "html entry redirects to canonical proxy path",
			method:        http.MethodGet,
			requestPath:   "/s/sl_abc123/",
			code:          "sl_abc123",
			canonicalPath: "/api/v1/instances/71/proxy/chat/",
			want:          "/api/v1/instances/71/proxy/chat/",
		},
		{
			name:          "entry without trailing slash redirects",
			method:        http.MethodGet,
			requestPath:   "/s/sl_abc123",
			code:          "sl_abc123",
			canonicalPath: "/api/v1/instances/71/proxy/chat/",
			want:          "/api/v1/instances/71/proxy/chat/",
		},
		{
			name:          "asset path keeps short proxy handling",
			method:        http.MethodGet,
			requestPath:   "/s/sl_abc123/assets/index.js",
			code:          "sl_abc123",
			canonicalPath: "/api/v1/instances/71/proxy/chat/",
			want:          "",
		},
		{
			name:          "post keeps password form handling",
			method:        http.MethodPost,
			requestPath:   "/s/sl_abc123/",
			code:          "sl_abc123",
			canonicalPath: "/api/v1/instances/71/proxy/chat/",
			want:          "",
		},
		{
			name:          "target strips accidental token query",
			method:        http.MethodGet,
			requestPath:   "/s/sl_abc123/",
			code:          "sl_abc123",
			canonicalPath: "/api/v1/instances/71/proxy/chat/?token=secret",
			want:          "/api/v1/instances/71/proxy/chat/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shortExternalAccessEntryRedirectTarget(tt.method, tt.requestPath, tt.code, tt.canonicalPath); got != tt.want {
				t.Fatalf("shortExternalAccessEntryRedirectTarget() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderShortLinkPasswordForm(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/s/sl_abc123/", nil)

	renderShortLinkPasswordForm(c, "sl_abc123", "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	for _, want := range []string{
		"Share link password",
		`name="password"`,
		`type="password"`,
		`action="/s/sl_abc123/"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("password form missing %q in body:\n%s", want, body)
		}
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("content type = %q, want text/html", contentType)
	}
}

func TestProxyAccessTokenPrefersCookieOverRuntimeQueryToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	accessService := services.NewInstanceAccessService()
	defer accessService.Stop()
	token, err := accessService.GenerateToken(1, 76, "hermes", "/api/v1/instances/76/proxy/chat/", "", 3000, time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/instances/76/proxy/api/ws?token=hermes-session-token", nil)
	c.Request.AddCookie(&http.Cookie{Name: "instance_access_76", Value: token.Token})

	handler := &InstanceHandler{accessService: accessService}
	got, ok := handler.proxyAccessToken(c, 76)
	if !ok {
		t.Fatal("proxyAccessToken rejected valid cookie")
	}
	if got != token.Token {
		t.Fatalf("proxyAccessToken = %q, want cookie token", got)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected response status %d", recorder.Code)
	}
}

func TestProxyAccessTokenRejectsRuntimeQueryTokenWithoutCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	accessService := services.NewInstanceAccessService()
	defer accessService.Stop()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/instances/76/proxy/api/ws?token=hermes-session-token", nil)

	handler := &InstanceHandler{accessService: accessService}
	if got, ok := handler.proxyAccessToken(c, 76); ok || got != "" {
		t.Fatalf("proxyAccessToken = %q/%v, want rejected", got, ok)
	}
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestBuildLiteBatchCreateRequestsDefaultsToGatewayLite(t *testing.T) {
	requests, handlerRequests, err := buildLiteBatchCreateRequests(BatchCreateLiteInstancesRequest{
		NamePrefix: "batch-lite",
		Count:      2,
	})
	if err != nil {
		t.Fatalf("buildLiteBatchCreateRequests returned error: %v", err)
	}
	if len(requests) != 2 || len(handlerRequests) != 2 {
		t.Fatalf("request count = %d/%d, want 2/2", len(requests), len(handlerRequests))
	}
	for idx, req := range requests {
		if req.Name == "" || req.Mode != services.InstanceModeLite || req.InstanceMode != services.InstanceModeLite || req.RuntimeType != services.RuntimeBackendGateway {
			t.Fatalf("request %d was not normalized to lite gateway: %#v", idx, req)
		}
		if req.Type != "openclaw" || req.OSType != "openclaw" || req.OSVersion != "latest" {
			t.Fatalf("request %d defaults = type %q os %q version %q", idx, req.Type, req.OSType, req.OSVersion)
		}
	}
}

func TestBatchDeleteLiteInstancesRejectsProInstance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/instances/batch/delete", strings.NewReader(`{"instance_ids":[42]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("userID", 7)
	c.Set("userRole", "user")

	handler := &InstanceHandler{
		instanceService: &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{
			42: {
				ID:           42,
				UserID:       7,
				Name:         "pro-openclaw",
				RuntimeType:  services.RuntimeBackendDesktop,
				InstanceMode: services.InstanceModePro,
			},
		}},
	}

	handler.BatchDeleteLiteInstances(c)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "not a lite instance") {
		t.Fatalf("response did not explain lite-only rejection: %s", recorder.Body.String())
	}
}
