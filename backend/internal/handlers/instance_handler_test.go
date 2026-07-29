package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"

	"github.com/gin-gonic/gin"
)

type blockingBatchInstanceService struct {
	fakeWorkspaceHandlerInstanceService
	started chan string
	release <-chan struct{}
}

type restartRecordingInstanceService struct {
	fakeWorkspaceHandlerInstanceService
	restartCalls        int
	restartedInstanceID int
	environment         map[string]string
	removals            []string
	restartErr          error
	environmentNames    []string
	environmentNamesErr error
}

func (s *restartRecordingInstanceService) GetEnvironmentOverrideNames(instanceID int) ([]string, error) {
	return s.environmentNames, s.environmentNamesErr
}

func (s *restartRecordingInstanceService) RestartWithEnvironment(instanceID int, environmentOverrides map[string]string, environmentOverrideRemovals []string) error {
	s.restartCalls++
	s.restartedInstanceID = instanceID
	s.environment = environmentOverrides
	s.removals = environmentOverrideRemovals
	return s.restartErr
}

func (s *blockingBatchInstanceService) CreatePrevalidated(userID int, req services.CreateInstanceRequest) (*models.Instance, error) {
	s.started <- req.Name
	<-s.release
	return &models.Instance{
		ID:     len(req.Name),
		UserID: userID,
		Name:   req.Name,
	}, nil
}

func TestBatchCreateLiteInstancesUsesBoundedConcurrencyAndPreservesOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	release := make(chan struct{})
	instanceService := &blockingBatchInstanceService{
		started: make(chan string, 10),
		release: release,
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/instances/batch/lite",
		strings.NewReader(`{"name_prefix":"batch-lite","count":6}`),
	)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("userID", 7)
	c.Set("userRole", "user")

	handler := &InstanceHandler{instanceService: instanceService}
	done := make(chan struct{})
	go func() {
		handler.BatchCreateLiteInstances(c)
		close(done)
	}()

	for idx := 0; idx < liteBatchCreateConcurrency; idx++ {
		select {
		case <-instanceService.started:
		case <-time.After(time.Second):
			t.Fatalf("only %d batch workers started", idx)
		}
	}
	select {
	case name := <-instanceService.started:
		t.Fatalf("batch concurrency exceeded %d; unexpected start for %q", liteBatchCreateConcurrency, name)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("batch handler did not finish")
	}

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	var payload struct {
		Data BatchCreateLiteInstancesResponse `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	if payload.Data.Created != 6 || payload.Data.Failed != 0 || len(payload.Data.Results) != 6 {
		t.Fatalf("unexpected batch response: %#v", payload.Data)
	}
	for idx, result := range payload.Data.Results {
		want := fmt.Sprintf("batch-lite-%03d", idx+1)
		if result.Name != want {
			t.Fatalf("result %d name = %q, want %q", idx, result.Name, want)
		}
	}
}

func TestRestartInstanceAcceptsOptionalEnvironmentOverrides(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name            string
		body            string
		userID          int
		userRole        string
		wantStatus      int
		wantCalls       int
		wantEnvironment map[string]string
		wantRemovals    []string
		restartErr      error
	}{
		{
			name:            "merge environment and restart",
			body:            `{"environment_overrides":{"SKILL_CACHE_PATH":"/workspace/cache","LOG_LEVEL":"debug"}}`,
			userID:          7,
			userRole:        "user",
			wantStatus:      http.StatusOK,
			wantCalls:       1,
			wantEnvironment: map[string]string{"SKILL_CACHE_PATH": "/workspace/cache", "LOG_LEVEL": "debug"},
		},
		{
			name:         "remove environment and restart",
			body:         `{"environment_override_removals":["OLD_SKILL_TOKEN","LEGACY_MODE"]}`,
			userID:       7,
			userRole:     "user",
			wantStatus:   http.StatusOK,
			wantCalls:    1,
			wantRemovals: []string{"OLD_SKILL_TOKEN", "LEGACY_MODE"},
		},
		{
			name:       "empty body keeps plain restart compatible",
			userID:     7,
			userRole:   "user",
			wantStatus: http.StatusOK,
			wantCalls:  1,
		},
		{
			name:       "other user cannot restart",
			body:       `{"environment_overrides":{"LOG_LEVEL":"debug"}}`,
			userID:     8,
			userRole:   "user",
			wantStatus: http.StatusForbidden,
			wantCalls:  0,
		},
		{
			name:       "invalid json is rejected",
			body:       `{"environment_overrides":`,
			userID:     7,
			userRole:   "user",
			wantStatus: http.StatusBadRequest,
			wantCalls:  0,
		},
		{
			name:       "service validation is a bad request",
			body:       `{"environment_overrides":{"INVALID-NAME":"value"}}`,
			userID:     7,
			userRole:   "user",
			wantStatus: http.StatusBadRequest,
			wantCalls:  1,
			wantEnvironment: map[string]string{
				"INVALID-NAME": "value",
			},
			restartErr: fmt.Errorf(
				"%w: invalid environment variable name: INVALID-NAME",
				services.ErrInvalidEnvironmentOverrides,
			),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instanceService := &restartRecordingInstanceService{
				fakeWorkspaceHandlerInstanceService: fakeWorkspaceHandlerInstanceService{
					instances: map[int]*models.Instance{
						42: {ID: 42, UserID: 7, Name: "skill-runtime"},
					},
				},
				restartErr: test.restartErr,
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Params = gin.Params{{Key: "id", Value: "42"}}
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/instances/42/restart", strings.NewReader(test.body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set("userID", test.userID)
			c.Set("userRole", test.userRole)

			handler := &InstanceHandler{instanceService: instanceService}
			handler.RestartInstance(c)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if instanceService.restartCalls != test.wantCalls {
				t.Fatalf("restart calls = %d, want %d", instanceService.restartCalls, test.wantCalls)
			}
			if test.wantCalls == 1 && instanceService.restartedInstanceID != 42 {
				t.Fatalf("restarted instance id = %d, want 42", instanceService.restartedInstanceID)
			}
			if !reflect.DeepEqual(instanceService.environment, test.wantEnvironment) {
				t.Fatalf("environment = %#v, want %#v", instanceService.environment, test.wantEnvironment)
			}
			if !reflect.DeepEqual(instanceService.removals, test.wantRemovals) {
				t.Fatalf("removals = %#v, want %#v", instanceService.removals, test.wantRemovals)
			}
		})
	}
}

func TestGetInstanceEnvironmentOverridesReturnsNamesOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		id         string
		userID     int
		userRole   string
		names      []string
		serviceErr error
		wantStatus int
	}{
		{
			name:       "owner receives configured names",
			id:         "42",
			userID:     7,
			userRole:   "user",
			names:      []string{"LOG_LEVEL", "SKILL_CACHE_PATH"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "other user is forbidden",
			id:         "42",
			userID:     8,
			userRole:   "user",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "invalid instance id",
			id:         "bad",
			userID:     7,
			userRole:   "user",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "service failure",
			id:         "42",
			userID:     7,
			userRole:   "user",
			serviceErr: fmt.Errorf("failed to decode environment overrides"),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instanceService := &restartRecordingInstanceService{
				fakeWorkspaceHandlerInstanceService: fakeWorkspaceHandlerInstanceService{
					instances: map[int]*models.Instance{
						42: {ID: 42, UserID: 7, Name: "skill-runtime"},
					},
				},
				environmentNames:    test.names,
				environmentNamesErr: test.serviceErr,
			}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Params = gin.Params{{Key: "id", Value: test.id}}
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/instances/"+test.id+"/environment-overrides", nil)
			c.Set("userID", test.userID)
			c.Set("userRole", test.userRole)

			handler := &InstanceHandler{instanceService: instanceService}
			handler.GetInstanceEnvironmentOverrides(c)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if test.wantStatus == http.StatusOK {
				var payload struct {
					Data struct {
						Names []string `json:"names"`
					} `json:"data"`
				}
				if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if !reflect.DeepEqual(payload.Data.Names, test.names) {
					t.Fatalf("names = %#v, want %#v", payload.Data.Names, test.names)
				}
				if strings.Contains(recorder.Body.String(), "secret-value") {
					t.Fatal("response must not expose environment values")
				}
			}
		})
	}
}

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

type stubSessionUsageObservabilityService struct {
	usage  *services.InstanceSessionUsageResult
	detail *services.InstanceSessionUsageDetail
	err    error
}

func (s *stubSessionUsageObservabilityService) ListAuditItems(services.AuditQuery) (*services.AuditListResult, error) {
	return nil, nil
}
func (s *stubSessionUsageObservabilityService) GetTraceDetail(string) (*services.AuditTraceDetail, error) {
	return nil, nil
}
func (s *stubSessionUsageObservabilityService) GetCostOverview(services.CostQuery) (*services.CostOverview, error) {
	return nil, nil
}
func (s *stubSessionUsageObservabilityService) GetInstanceSessionUsage(int, services.InstanceSessionUsageQuery) (*services.InstanceSessionUsageResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.usage, nil
}
func (s *stubSessionUsageObservabilityService) GetInstanceSessionUsageDetail(int, string, repository.SessionUsageFilter) (*services.InstanceSessionUsageDetail, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.detail == nil {
		return nil, fmt.Errorf("session usage not found")
	}
	return s.detail, nil
}
func (s *stubSessionUsageObservabilityService) GetInstanceLLMGovernanceStatus(int, map[string]interface{}) (*services.InstanceLLMGovernanceStatus, error) {
	return nil, nil
}
func (s *stubSessionUsageObservabilityService) GetLLMGovernanceOverview() (*services.LLMGovernanceOverview, error) {
	return nil, nil
}
func (s *stubSessionUsageObservabilityService) GetAdminSessionUsageOverview(services.InstanceSessionUsageOverviewQuery) (*services.InstanceSessionUsageOverview, error) {
	return nil, nil
}

func TestGetInstanceSessionUsageReturns200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/instances/9/session-usage?page=1&limit=10", nil)
	c.Params = gin.Params{{Key: "id", Value: "9"}}
	c.Set("userID", 7)
	c.Set("userRole", "user")

	handler := &InstanceHandler{
		instanceService: &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{
			9: {ID: 9, UserID: 7, Name: "openclaw-lite", Type: "openclaw"},
		}},
		aiObservabilityService: &stubSessionUsageObservabilityService{
			usage: &services.InstanceSessionUsageResult{
				Summary: services.InstanceSessionUsageSummary{Currency: "USD"},
				Items:   []services.InstanceSessionUsageItem{},
			},
		},
	}

	handler.GetInstanceSessionUsage(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "Instance session usage retrieved successfully") {
		t.Fatalf("unexpected body: %s", recorder.Body.String())
	}
}

func TestGetInstanceSessionUsageInvalidSince(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/instances/9/session-usage?since=bad-timestamp", nil)
	c.Params = gin.Params{{Key: "id", Value: "9"}}
	c.Set("userID", 7)
	c.Set("userRole", "user")

	handler := &InstanceHandler{
		instanceService: &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{
			9: {ID: 9, UserID: 7, Name: "openclaw-lite", Type: "openclaw"},
		}},
		aiObservabilityService: &stubSessionUsageObservabilityService{},
	}

	handler.GetInstanceSessionUsage(c)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestGetInstanceSessionUsageRejectsUntilBeforeSince(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/instances/9/session-usage?since=2026-07-10T00:00:00Z&until=2026-07-01T00:00:00Z", nil)
	c.Params = gin.Params{{Key: "id", Value: "9"}}
	c.Set("userID", 7)
	c.Set("userRole", "user")

	handler := &InstanceHandler{
		instanceService: &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{
			9: {ID: 9, UserID: 7, Name: "openclaw-lite", Type: "openclaw"},
		}},
		aiObservabilityService: &stubSessionUsageObservabilityService{},
	}

	handler.GetInstanceSessionUsage(c)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestGetInstanceSessionUsageDetailRequiresSessionID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/instances/9/session-usage/detail", nil)
	c.Params = gin.Params{{Key: "id", Value: "9"}}
	c.Set("userID", 7)
	c.Set("userRole", "user")

	handler := &InstanceHandler{
		instanceService: &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{
			9: {ID: 9, UserID: 7, Name: "openclaw-lite", Type: "openclaw"},
		}},
		aiObservabilityService: &stubSessionUsageObservabilityService{},
	}

	handler.GetInstanceSessionUsageDetail(c)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

func TestGetInstanceSessionUsageDetailNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/instances/9/session-usage/detail?session_id=missing", nil)
	c.Params = gin.Params{{Key: "id", Value: "9"}}
	c.Set("userID", 7)
	c.Set("userRole", "user")

	handler := &InstanceHandler{
		instanceService: &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{
			9: {ID: 9, UserID: 7, Name: "openclaw-lite", Type: "openclaw"},
		}},
		aiObservabilityService: &stubSessionUsageObservabilityService{},
	}

	handler.GetInstanceSessionUsageDetail(c)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, http.StatusNotFound, recorder.Body.String())
	}
}
