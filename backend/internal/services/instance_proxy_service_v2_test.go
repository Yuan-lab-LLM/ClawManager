package services

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"

	"github.com/gorilla/websocket"
)

func TestInstanceProxyServiceUsesRuntimeBindingForV2(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/instances/123/proxy/apps/openclaw" {
			t.Fatalf("unexpected upstream path %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Forwarded-Prefix"); got != "/api/v1/instances/123/proxy" {
			t.Fatalf("X-Forwarded-Prefix = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer internal-openclaw-token" {
			t.Fatalf("Authorization = %q", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	podIP, gatewayPort := splitURLHostPortForProxyTest(t, upstream.URL)
	workspacePath := "/workspaces/openclaw/user-45/instance-123"
	instanceRepo := newV2LifecycleInstanceRepo()
	instanceRepo.byID[123] = &models.Instance{
		ID:                123,
		UserID:            45,
		Type:              "openclaw",
		RuntimeType:       "gateway",
		Status:            "running",
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 3,
	}
	bindingRepo := newFakeRuntimeBindingRepo()
	bindingRepo.bindings[123] = &models.InstanceRuntimeBinding{
		InstanceID:   123,
		RuntimePodID: 9,
		GatewayPort:  gatewayPort,
		State:        "running",
		Generation:   3,
	}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			9: {ID: 9, PodIP: &podIP, State: "ready"},
		},
	}
	accessService := NewInstanceAccessService()
	defer accessService.Stop()
	token, err := accessService.GenerateToken(45, 123, "openclaw", "/api/v1/instances/123/proxy/", "", 3000, time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	service := NewInstanceProxyService(accessService)
	service.instanceRepo = instanceRepo
	service.bindingRepo = bindingRepo
	service.runtimePodRepo = podRepo
	service.httpClient = upstream.Client()
	service.openClawGatewayToken = "internal-openclaw-token"

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/123/proxy/apps/openclaw?token="+url.QueryEscape(token.Token), nil)
	rec := httptest.NewRecorder()

	if err := service.ProxyRequest(req.Context(), 123, token.Token, rec, req); err != nil {
		t.Fatalf("ProxyRequest returned error: %v", err)
	}
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("unexpected proxy response %d %q", rec.Code, rec.Body.String())
	}
}

func TestInstanceProxyServiceInjectsInstanceTokenForHermesLite(t *testing.T) {
	instanceToken := "igt_hermes_instance"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat" {
			t.Fatalf("unexpected upstream path %s", r.URL.Path)
		}
		if r.URL.RawQuery != "" {
			t.Fatalf("unexpected upstream query %q", r.URL.RawQuery)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+instanceToken {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-Api-Key"); got != instanceToken {
			t.Fatalf("X-Api-Key = %q", got)
		}
		if got := r.Header.Get("X-ClawManager-Instance-Token"); got != instanceToken {
			t.Fatalf("X-ClawManager-Instance-Token = %q", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	podIP, gatewayPort := splitURLHostPortForProxyTest(t, upstream.URL)
	workspacePath := "/workspaces/hermes/user-45/instance-127"
	instanceRepo := newV2LifecycleInstanceRepo()
	instanceRepo.byID[127] = &models.Instance{
		ID:                127,
		UserID:            45,
		Type:              "hermes",
		RuntimeType:       "gateway",
		InstanceMode:      InstanceModeLite,
		Status:            "running",
		AccessToken:       &instanceToken,
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 5,
	}
	bindingRepo := newFakeRuntimeBindingRepo()
	bindingRepo.bindings[127] = &models.InstanceRuntimeBinding{
		InstanceID:   127,
		RuntimePodID: 10,
		GatewayPort:  gatewayPort,
		State:        "running",
		Generation:   5,
	}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			10: {ID: 10, PodIP: &podIP, State: "ready"},
		},
	}
	accessService := NewInstanceAccessService()
	defer accessService.Stop()
	token, err := accessService.GenerateToken(45, 127, "hermes", "/api/v1/instances/127/proxy/", "", 3000, time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	service := NewInstanceProxyService(accessService)
	service.instanceRepo = instanceRepo
	service.bindingRepo = bindingRepo
	service.runtimePodRepo = podRepo
	service.httpClient = upstream.Client()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/127/proxy/chat?token="+url.QueryEscape(token.Token), nil)
	req.Header.Set("X-Api-Key", "stale-session-key")
	rec := httptest.NewRecorder()

	if err := service.ProxyRequest(req.Context(), 127, token.Token, rec, req); err != nil {
		t.Fatalf("ProxyRequest returned error: %v", err)
	}
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("unexpected proxy response %d %q", rec.Code, rec.Body.String())
	}
}

func TestInstanceProxyServiceUsesHermesLiteAccessURLForRootEntry(t *testing.T) {
	instanceToken := "igt_hermes_instance"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/" {
			t.Fatalf("unexpected upstream path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+instanceToken {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<!doctype html><html><head><title>Hermes</title></head><body>ok</body></html>"))
	}))
	defer upstream.Close()

	podIP, gatewayPort := splitURLHostPortForProxyTest(t, upstream.URL)
	workspacePath := "/workspaces/hermes/user-45/instance-131"
	instanceRepo := newV2LifecycleInstanceRepo()
	instanceRepo.byID[131] = &models.Instance{
		ID:                131,
		UserID:            45,
		Type:              "hermes",
		RuntimeType:       "gateway",
		InstanceMode:      InstanceModeLite,
		Status:            "running",
		AccessToken:       &instanceToken,
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 5,
	}
	bindingRepo := newFakeRuntimeBindingRepo()
	bindingRepo.bindings[131] = &models.InstanceRuntimeBinding{
		InstanceID:   131,
		RuntimePodID: 14,
		GatewayPort:  gatewayPort,
		State:        "running",
		Generation:   5,
	}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			14: {ID: 14, PodIP: &podIP, State: "ready"},
		},
	}
	accessService := NewInstanceAccessService()
	defer accessService.Stop()
	token, err := accessService.GenerateToken(45, 131, "hermes", "/api/v1/instances/131/proxy/chat/", "", 3000, time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	service := NewInstanceProxyService(accessService)
	service.instanceRepo = instanceRepo
	service.bindingRepo = bindingRepo
	service.runtimePodRepo = podRepo
	service.httpClient = upstream.Client()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/131/proxy/?token="+url.QueryEscape(token.Token), nil)
	rec := httptest.NewRecorder()

	if err := service.ProxyRequest(req.Context(), 131, token.Token, rec, req); err != nil {
		t.Fatalf("ProxyRequest returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected proxy response %d %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `<base href="/api/v1/instances/131/proxy/chat/">`) {
		t.Fatalf("expected Hermes Lite HTML to include chat proxy base, got %q", rec.Body.String())
	}
}

func TestInstanceProxyServicePreservesHermesRuntimeQueryToken(t *testing.T) {
	instanceToken := "igt_hermes_instance"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ws" {
			t.Fatalf("unexpected upstream path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("token"); got != "hermes-session-token" {
			t.Fatalf("upstream token query = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+instanceToken {
			t.Fatalf("Authorization = %q", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	podIP, gatewayPort := splitURLHostPortForProxyTest(t, upstream.URL)
	workspacePath := "/workspaces/hermes/user-45/instance-130"
	instanceRepo := newV2LifecycleInstanceRepo()
	instanceRepo.byID[130] = &models.Instance{
		ID:                130,
		UserID:            45,
		Type:              "hermes",
		RuntimeType:       "gateway",
		InstanceMode:      InstanceModeLite,
		Status:            "running",
		AccessToken:       &instanceToken,
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 5,
	}
	bindingRepo := newFakeRuntimeBindingRepo()
	bindingRepo.bindings[130] = &models.InstanceRuntimeBinding{
		InstanceID:   130,
		RuntimePodID: 13,
		GatewayPort:  gatewayPort,
		State:        "running",
		Generation:   5,
	}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			13: {ID: 13, PodIP: &podIP, State: "ready"},
		},
	}
	accessService := NewInstanceAccessService()
	defer accessService.Stop()
	token, err := accessService.GenerateToken(45, 130, "hermes", "/api/v1/instances/130/proxy/chat/", "", 3000, time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	service := NewInstanceProxyService(accessService)
	service.instanceRepo = instanceRepo
	service.bindingRepo = bindingRepo
	service.runtimePodRepo = podRepo
	service.httpClient = upstream.Client()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/130/proxy/api/ws?token=hermes-session-token", nil)
	rec := httptest.NewRecorder()

	if err := service.ProxyRequest(req.Context(), 130, token.Token, rec, req); err != nil {
		t.Fatalf("ProxyRequest returned error: %v", err)
	}
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("unexpected proxy response %d %q", rec.Code, rec.Body.String())
	}
}

func TestInstanceProxyServiceStripsStaleHermesGatewayQueryToken(t *testing.T) {
	instanceToken := "igt_hermes_instance"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ws" {
			t.Fatalf("unexpected upstream path %s", r.URL.Path)
		}
		if got := r.URL.RawQuery; got != "" {
			t.Fatalf("upstream query = %q, want empty", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+instanceToken {
			t.Fatalf("Authorization = %q", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	podIP, gatewayPort := splitURLHostPortForProxyTest(t, upstream.URL)
	workspacePath := "/workspaces/hermes/user-45/instance-132"
	instanceRepo := newV2LifecycleInstanceRepo()
	instanceRepo.byID[132] = &models.Instance{
		ID:                132,
		UserID:            45,
		Type:              "hermes",
		RuntimeType:       "gateway",
		InstanceMode:      InstanceModeLite,
		Status:            "running",
		AccessToken:       &instanceToken,
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 5,
	}
	bindingRepo := newFakeRuntimeBindingRepo()
	bindingRepo.bindings[132] = &models.InstanceRuntimeBinding{
		InstanceID:   132,
		RuntimePodID: 15,
		GatewayPort:  gatewayPort,
		State:        "running",
		Generation:   5,
	}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			15: {ID: 15, PodIP: &podIP, State: "ready"},
		},
	}
	accessService := NewInstanceAccessService()
	defer accessService.Stop()
	accessToken, err := accessService.GenerateToken(45, 132, "hermes", "/api/v1/instances/132/proxy/chat/", "", 3000, time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	service := NewInstanceProxyService(accessService)
	service.instanceRepo = instanceRepo
	service.bindingRepo = bindingRepo
	service.runtimePodRepo = podRepo
	service.httpClient = upstream.Client()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/132/proxy/api/ws?token=igt_stale_gateway", nil)
	rec := httptest.NewRecorder()

	if err := service.ProxyRequest(req.Context(), 132, accessToken.Token, rec, req); err != nil {
		t.Fatalf("ProxyRequest returned error: %v", err)
	}
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("unexpected proxy response %d %q", rec.Code, rec.Body.String())
	}
}

func TestInstanceProxyServiceStripsStaleProxyAccessTokenQuery(t *testing.T) {
	accessService := NewInstanceAccessService()
	defer accessService.Stop()
	currentToken, err := accessService.GenerateToken(45, 133, "hermes", "/api/v1/instances/133/proxy/chat/", "", 3000, time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken current returned error: %v", err)
	}
	oldToken, err := accessService.GenerateToken(45, 133, "hermes", "/api/v1/instances/133/proxy/chat/", "", 3000, time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken old returned error: %v", err)
	}
	service := NewInstanceProxyService(accessService)
	query := url.Values{"token": []string{oldToken.Token, "hermes-session-token"}}

	service.removeProxyAccessTokenQuery(query, currentToken.Token, "igt_hermes_instance")

	got := query["token"]
	if len(got) != 1 || got[0] != "hermes-session-token" {
		t.Fatalf("token query = %#v, want only Hermes session token", got)
	}
}
func TestInstanceProxyServiceRewritesHermesLiteHTMLBase(t *testing.T) {
	instanceToken := "igt_hermes_instance"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Fatalf("unexpected upstream path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+instanceToken {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<!doctype html><html><head><title>Hermes</title></head><body>ok</body></html>"))
	}))
	defer upstream.Close()

	podIP, gatewayPort := splitURLHostPortForProxyTest(t, upstream.URL)
	workspacePath := "/workspaces/hermes/user-45/instance-128"
	instanceRepo := newV2LifecycleInstanceRepo()
	instanceRepo.byID[128] = &models.Instance{
		ID:                128,
		UserID:            45,
		Type:              "hermes",
		RuntimeType:       "gateway",
		InstanceMode:      InstanceModeLite,
		Status:            "running",
		AccessToken:       &instanceToken,
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 5,
	}
	bindingRepo := newFakeRuntimeBindingRepo()
	bindingRepo.bindings[128] = &models.InstanceRuntimeBinding{
		InstanceID:   128,
		RuntimePodID: 11,
		GatewayPort:  gatewayPort,
		State:        "running",
		Generation:   5,
	}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			11: {ID: 11, PodIP: &podIP, State: "ready"},
		},
	}
	accessService := NewInstanceAccessService()
	defer accessService.Stop()
	token, err := accessService.GenerateToken(45, 128, "hermes", "/api/v1/instances/128/proxy/", "", 3000, time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	service := NewInstanceProxyService(accessService)
	service.instanceRepo = instanceRepo
	service.bindingRepo = bindingRepo
	service.runtimePodRepo = podRepo
	service.httpClient = upstream.Client()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/128/proxy/?token="+url.QueryEscape(token.Token), nil)
	rec := httptest.NewRecorder()

	if err := service.ProxyRequest(req.Context(), 128, token.Token, rec, req); err != nil {
		t.Fatalf("ProxyRequest returned error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected proxy response %d %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `<base href="/api/v1/instances/128/proxy/">`) {
		t.Fatalf("expected Hermes Lite HTML to include proxy base, got %q", rec.Body.String())
	}
}

func TestInstanceProxyServiceProxiesHermesLiteWebSocket(t *testing.T) {
	instanceToken := "igt_hermes_instance"
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws" {
			t.Fatalf("unexpected upstream websocket path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("token"); got != "hermes-ws-token" {
			t.Fatalf("upstream websocket token query = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+instanceToken {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-Forwarded-Prefix"); got != "/api/v1/instances/129/proxy" {
			t.Fatalf("X-Forwarded-Prefix = %q", got)
		}
		if got := r.Header.Get("X-Forwarded-Host"); got == "" {
			t.Fatalf("X-Forwarded-Host is empty")
		}
		if got := r.Header.Get("X-Forwarded-For"); got == "" {
			t.Fatalf("X-Forwarded-For is empty")
		}
		if got, want := r.Header.Get("Origin"), "http://"+r.Host; got != want {
			t.Fatalf("Origin = %q, want %q", got, want)
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatalf("upstream websocket upgrade failed: %v", err)
		}
		defer conn.Close()
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("upstream websocket read failed: %v", err)
		}
		if string(message) != "ping" {
			t.Fatalf("upstream websocket message = %q", message)
		}
		if err := conn.WriteMessage(messageType, []byte("pong")); err != nil {
			t.Fatalf("upstream websocket write failed: %v", err)
		}
	}))
	defer upstream.Close()

	podIP, gatewayPort := splitURLHostPortForProxyTest(t, upstream.URL)
	workspacePath := "/workspaces/hermes/user-45/instance-129"
	instanceRepo := newV2LifecycleInstanceRepo()
	instanceRepo.byID[129] = &models.Instance{
		ID:                129,
		UserID:            45,
		Type:              "hermes",
		RuntimeType:       "gateway",
		InstanceMode:      InstanceModeLite,
		Status:            "running",
		AccessToken:       &instanceToken,
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 5,
	}
	bindingRepo := newFakeRuntimeBindingRepo()
	bindingRepo.bindings[129] = &models.InstanceRuntimeBinding{
		InstanceID:   129,
		RuntimePodID: 12,
		GatewayPort:  gatewayPort,
		State:        "running",
		Generation:   5,
	}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			12: {ID: 12, PodIP: &podIP, State: "ready"},
		},
	}
	accessService := NewInstanceAccessService()
	defer accessService.Stop()
	token, err := accessService.GenerateToken(45, 129, "hermes", "/api/v1/instances/129/proxy/", "", 3000, time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	service := NewInstanceProxyService(accessService)
	service.instanceRepo = instanceRepo
	service.bindingRepo = bindingRepo
	service.runtimePodRepo = podRepo

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := service.ProxyWebSocket(r.Context(), 129, token.Token, w, r); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
	}))
	defer proxy.Close()

	wsURL := "ws" + strings.TrimPrefix(proxy.URL, "http") + "/api/v1/instances/129/proxy/ws?token=hermes-ws-token"
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("client websocket dial failed: %v", err)
	}
	defer clientConn.Close()
	if err := clientConn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatalf("client websocket write failed: %v", err)
	}
	_, message, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("client websocket read failed: %v", err)
	}
	if string(message) != "pong" {
		t.Fatalf("client websocket message = %q", message)
	}
}

func TestInstanceProxyServiceUsesBaseProxyEntryForV2OpenClaw(t *testing.T) {
	workspacePath := "/workspaces/openclaw/user-45/instance-123"
	accessService := NewInstanceAccessService()
	t.Cleanup(accessService.Stop)
	service := NewInstanceProxyService(accessService)
	got := service.GetProxyURLForInstance(&models.Instance{
		ID:            123,
		Type:          "openclaw",
		RuntimeType:   "gateway",
		WorkspacePath: &workspacePath,
	}, "token+with/slash")

	want := "/api/v1/instances/123/proxy/?token=token%2Bwith%2Fslash"
	if got != want {
		t.Fatalf("GetProxyURLForInstance() = %q, want %q", got, want)
	}
}

func TestInstanceProxyServiceUsesChatEntryForHermesLite(t *testing.T) {
	workspacePath := "/workspaces/hermes/user-45/instance-123"
	accessService := NewInstanceAccessService()
	t.Cleanup(accessService.Stop)
	service := NewInstanceProxyService(accessService)
	got := service.GetProxyURLForInstance(&models.Instance{
		ID:            123,
		Type:          "hermes",
		RuntimeType:   "gateway",
		InstanceMode:  InstanceModeLite,
		WorkspacePath: &workspacePath,
	}, "token+with/slash")

	want := "/api/v1/instances/123/proxy/chat/?token=token%2Bwith%2Fslash"
	if got != want {
		t.Fatalf("GetProxyURLForInstance() = %q, want %q", got, want)
	}
}

func TestProxyBaseForRequestPathUsesSubdirectory(t *testing.T) {
	got := proxyBaseForRequestPath("/api/v1/instances/123/proxy/__openclaw__/canvas/", 123)
	want := "/api/v1/instances/123/proxy/__openclaw__/canvas/"
	if got != want {
		t.Fatalf("proxyBaseForRequestPath() = %q, want %q", got, want)
	}

	got = proxyBaseForRequestPath("/api/v1/instances/123/proxy/__openclaw__/canvas/app.js", 123)
	if got != want {
		t.Fatalf("proxyBaseForRequestPath(file) = %q, want %q", got, want)
	}
}

func TestResolveOpenClawProxyOriginFromEnvUsesInternalOrigin(t *testing.T) {
	t.Setenv("OPENCLAW_PROXY_ORIGIN", "")
	t.Setenv("CLAWMANAGER_BACKEND_URL", "")
	t.Setenv("CLAWMANAGER_TEAM_MANAGER_BASE_URL", "http://clawmanager-gateway.clawmanager-system.svc.cluster.local:9001/api")

	got := resolveOpenClawProxyOriginFromEnv()
	want := "http://clawmanager-gateway.clawmanager-system.svc.cluster.local:9001"
	if got != want {
		t.Fatalf("resolveOpenClawProxyOriginFromEnv() = %q, want %q", got, want)
	}
}

func TestOpenClawWebSocketOriginPrefersConfiguredProxyOrigin(t *testing.T) {
	accessService := NewInstanceAccessService()
	t.Cleanup(accessService.Stop)
	service := NewInstanceProxyService(accessService)
	service.openClawProxyOrigin = "http://clawmanager-gateway.clawmanager-system.svc.cluster.local:9001"

	got := service.openClawWebSocketOrigin(&url.URL{Scheme: "ws", Host: "10.42.0.63:20000"})
	want := "http://clawmanager-gateway.clawmanager-system.svc.cluster.local:9001"
	if got != want {
		t.Fatalf("openClawWebSocketOrigin() = %q, want %q", got, want)
	}
}

func TestWebSocketUpstreamOriginFallsBackToGatewayHost(t *testing.T) {
	tests := []struct {
		name string
		url  *url.URL
		want string
	}{
		{
			name: "plain websocket",
			url:  &url.URL{Scheme: "ws", Host: "10.42.0.63:20000"},
			want: "http://10.42.0.63:20000",
		},
		{
			name: "tls websocket",
			url:  &url.URL{Scheme: "wss", Host: "gateway.example.test"},
			want: "https://gateway.example.test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := websocketUpstreamOrigin(tt.url); got != tt.want {
				t.Fatalf("websocketUpstreamOrigin() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInstanceProxyServiceRejectsStoppedV2InstanceWithStaleBinding(t *testing.T) {
	workspacePath := "/workspaces/openclaw/user-45/instance-125"
	instanceRepo := newV2LifecycleInstanceRepo()
	instanceRepo.byID[125] = &models.Instance{
		ID:                125,
		UserID:            45,
		Type:              "openclaw",
		RuntimeType:       "gateway",
		Status:            "stopped",
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 4,
	}
	bindingRepo := newFakeRuntimeBindingRepo()
	bindingRepo.bindings[125] = &models.InstanceRuntimeBinding{
		InstanceID:   125,
		RuntimePodID: 9,
		GatewayPort:  20025,
		State:        "running",
		Generation:   4,
	}
	podIP := "10.42.0.125"
	service, token := newV2ProxyTestService(t, instanceRepo, bindingRepo, &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			9: {ID: 9, PodIP: &podIP},
		},
	}, 45, 125, "openclaw")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/125/proxy/", nil)
	err := service.ProxyRequest(req.Context(), 125, token.Token, httptest.NewRecorder(), req)
	if !errors.Is(err, ErrInstanceGatewayUnavailable) {
		t.Fatalf("ProxyRequest error = %v, want ErrInstanceGatewayUnavailable", err)
	}
}

func TestInstanceProxyServiceRejectsV2BindingGenerationMismatch(t *testing.T) {
	workspacePath := "/workspaces/hermes/user-45/instance-126"
	instanceRepo := newV2LifecycleInstanceRepo()
	instanceRepo.byID[126] = &models.Instance{
		ID:                126,
		UserID:            45,
		Type:              "hermes",
		RuntimeType:       "gateway",
		Status:            "running",
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 8,
	}
	bindingRepo := newFakeRuntimeBindingRepo()
	bindingRepo.bindings[126] = &models.InstanceRuntimeBinding{
		InstanceID:   126,
		RuntimePodID: 10,
		GatewayPort:  20026,
		State:        "running",
		Generation:   7,
	}
	podIP := "10.42.0.126"
	service, token := newV2ProxyTestService(t, instanceRepo, bindingRepo, &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			10: {ID: 10, PodIP: &podIP},
		},
	}, 45, 126, "hermes")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/126/proxy/", nil)
	err := service.ProxyRequest(req.Context(), 126, token.Token, httptest.NewRecorder(), req)
	if !errors.Is(err, ErrInstanceGatewayUnavailable) {
		t.Fatalf("ProxyRequest error = %v, want ErrInstanceGatewayUnavailable", err)
	}
}

func TestInstanceProxyServiceReturnsUnavailableWhenV2BindingMissing(t *testing.T) {
	workspacePath := "/workspaces/hermes/user-45/instance-124"
	instanceRepo := newV2LifecycleInstanceRepo()
	instanceRepo.byID[124] = &models.Instance{
		ID:            124,
		UserID:        45,
		Type:          "hermes",
		RuntimeType:   "gateway",
		Status:        "running",
		WorkspacePath: &workspacePath,
	}
	accessService := NewInstanceAccessService()
	defer accessService.Stop()
	token, err := accessService.GenerateToken(45, 124, "hermes", "/api/v1/instances/124/proxy/", "", 3000, time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	service := NewInstanceProxyService(accessService)
	service.instanceRepo = instanceRepo
	service.bindingRepo = newFakeRuntimeBindingRepo()
	service.runtimePodRepo = &fakeRuntimePodRepo{}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/124/proxy/", nil)
	rec := httptest.NewRecorder()

	err = service.ProxyRequest(req.Context(), 124, token.Token, rec, req)
	if !errors.Is(err, ErrInstanceGatewayUnavailable) {
		t.Fatalf("ProxyRequest error = %v, want ErrInstanceGatewayUnavailable", err)
	}
}

func newV2ProxyTestService(t *testing.T, instanceRepo repository.InstanceRepository, bindingRepo repository.InstanceRuntimeBindingRepository, podRepo repository.RuntimePodRepository, userID int, instanceID int, instanceType string) (*InstanceProxyService, *AccessToken) {
	t.Helper()
	accessService := NewInstanceAccessService()
	t.Cleanup(accessService.Stop)
	token, err := accessService.GenerateToken(userID, instanceID, instanceType, "/api/v1/instances/"+strconv.Itoa(instanceID)+"/proxy/", "", 3000, time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken returned error: %v", err)
	}
	service := NewInstanceProxyService(accessService)
	service.instanceRepo = instanceRepo
	service.bindingRepo = bindingRepo
	service.runtimePodRepo = podRepo
	return service, token
}

func splitURLHostPortForProxyTest(t *testing.T, rawURL string) (string, int) {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	host, portText, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("split upstream host port: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse upstream port: %v", err)
	}
	return host, port
}
