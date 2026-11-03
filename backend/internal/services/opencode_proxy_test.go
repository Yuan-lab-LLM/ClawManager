package services

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawreef/internal/models"
)

func TestRewriteOpenCodeHTMLRootAssets(t *testing.T) {
	prefix := "/api/v1/instances/123/proxy"
	html := `<html><head>
<link rel="stylesheet" href="/assets/index.css">
<script type="module" src="/assets/index.js"></script>
<link rel="icon" href="//cdn.example.com/favicon.ico">
</head></html>`

	got := rewriteOpenCodeHTMLRootAssets(html, prefix)
	if !strings.Contains(got, `href="`+prefix+`/assets/index.css"`) {
		t.Fatalf("css href not rewritten: %s", got)
	}
	if !strings.Contains(got, `src="`+prefix+`/assets/index.js"`) {
		t.Fatalf("js src not rewritten: %s", got)
	}
	if !strings.Contains(got, `href="//cdn.example.com/favicon.ico"`) {
		t.Fatalf("protocol-relative URL should be unchanged: %s", got)
	}

	once := rewriteOpenCodeHTMLRootAssets(got, prefix)
	if strings.Count(once, prefix+"/assets/index.js") != 1 {
		t.Fatalf("rewriting twice should be idempotent: %s", once)
	}
}

func TestInjectOpenCodeAbsolutePathPatch(t *testing.T) {
	prefix := "/api/v1/instances/9/proxy"
	html := `<html><head><title>x</title></head><body></body></html>`
	got := injectOpenCodeAbsolutePathPatch(html, prefix)
	if !strings.Contains(got, "window.EventSource") {
		t.Fatalf("expected EventSource patch: %s", got)
	}
	if !strings.Contains(got, "window.WebSocket") {
		t.Fatalf("expected WebSocket patch: %s", got)
	}
	if !strings.Contains(got, "pushState") || !strings.Contains(got, "replaceState") {
		t.Fatalf("expected History API patch: %s", got)
	}
	if !strings.Contains(got, "u instanceof URL") || !strings.Contains(got, "a.host===window.location.host") {
		t.Fatalf("expected URL object and same-host patch: %s", got)
	}
	if !strings.Contains(got, `a.protocol==="ws:"||a.protocol==="wss:"`) {
		t.Fatalf("expected WebSocket URLs to remain absolute: %s", got)
	}
	if !strings.Contains(got, prefix) {
		t.Fatalf("expected prefix in patch: %s", got)
	}
}

func TestSetOpenCodeServerBasicAuthHeaders(t *testing.T) {
	header := http.Header{}
	setOpenCodeServerBasicAuthHeaders(header, "secret-token")
	got := header.Get("Authorization")
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("opencode:secret-token"))
	if got != want {
		t.Fatalf("Authorization = %q, want %q", got, want)
	}
}

func TestIsOpenCodeLiteProxyInstance(t *testing.T) {
	workspace := "/workspaces/opencode/user-1/instance-7"
	token := "igt_test"
	repo := newV2LifecycleInstanceRepo()
	repo.byID[7] = &models.Instance{
		ID:            7,
		Type:          RuntimeTypeOpenCode,
		InstanceMode:  InstanceModeLite,
		RuntimeType:   RuntimeBackendGateway,
		WorkspacePath: &workspace,
		AccessToken:   &token,
		Status:        "running",
	}
	repo.byID[8] = &models.Instance{
		ID:           8,
		Type:         RuntimeTypeOpenCode,
		InstanceMode: InstanceModePro,
		RuntimeType:  RuntimeBackendDesktop,
		Status:       "running",
	}
	service := NewInstanceProxyService(NewInstanceAccessService())
	service.instanceRepo = repo

	if !service.isOpenCodeLiteProxyInstance(7, "opencode") {
		t.Fatal("expected lite opencode instance to match")
	}
	if service.isOpenCodeLiteProxyInstance(8, "opencode") {
		t.Fatal("pro opencode must not match lite proxy helper")
	}
	if service.isOpenCodeLiteProxyInstance(7, "hermes") {
		t.Fatal("hermes type must not match")
	}
}

func TestGetProxyURLForInstanceOpenCodeLiteUsesDedicatedOrigin(t *testing.T) {
	t.Setenv(openCodePublicURLTemplateEnvVar, "https://opencode-{instance_id}.runtime.example.test/")
	workspacePath := "/workspaces/opencode/user-45/instance-123"
	accessService := NewInstanceAccessService()
	t.Cleanup(accessService.Stop)
	service := NewInstanceProxyService(accessService)
	got := service.GetProxyURLForInstance(&models.Instance{
		ID:            123,
		Type:          RuntimeTypeOpenCode,
		RuntimeType:   RuntimeBackendGateway,
		InstanceMode:  InstanceModeLite,
		WorkspacePath: &workspacePath,
	}, "token+with/slash")

	want := "https://opencode-123.runtime.example.test/?token=token%2Bwith%2Fslash"
	if got != want {
		t.Fatalf("GetProxyURLForInstance() = %q, want %q", got, want)
	}
	if strings.Contains(got, "/chat") {
		t.Fatalf("opencode lite must not use hermes /chat entry: %q", got)
	}
}

func TestShouldRewriteHTMLForProxyOpenCodeLite(t *testing.T) {
	workspace := "/workspaces/opencode/user-1/instance-7"
	token := "igt_test"
	repo := newV2LifecycleInstanceRepo()
	repo.byID[7] = &models.Instance{
		ID:            7,
		Type:          RuntimeTypeOpenCode,
		InstanceMode:  InstanceModeLite,
		RuntimeType:   RuntimeBackendGateway,
		WorkspacePath: &workspace,
		AccessToken:   &token,
		Status:        "running",
	}
	repo.byID[8] = &models.Instance{
		ID:           8,
		Type:         RuntimeTypeOpenCode,
		InstanceMode: InstanceModePro,
		RuntimeType:  RuntimeBackendDesktop,
		Status:       "running",
	}
	service := NewInstanceProxyService(NewInstanceAccessService())
	service.instanceRepo = repo

	if !service.shouldRewriteHTMLForProxy(7, "opencode", 3001) {
		t.Fatal("expected lite opencode to force HTML rewrite")
	}
	if service.shouldRewriteHTMLForProxy(8, "opencode", 3001) {
		t.Fatal("pro opencode must not force lite HTML rewrite")
	}
}

func TestIsOpenCodeEventStreamRequest(t *testing.T) {
	if !isOpenCodeEventStreamRequest(true, "/api/event") {
		t.Fatal("expected OpenCode V2 event stream to bypass the HTTP timeout")
	}
	if isOpenCodeEventStreamRequest(false, "/api/event") {
		t.Fatal("non-Lite V2 requests must keep the standard HTTP timeout")
	}
	if !isOpenCodeEventStreamRequest(true, "/global/event") {
		t.Fatal("expected OpenCode Lite global event stream to bypass the HTTP timeout")
	}
}

type proxyRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip proxyRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestAgentProxyRequestDoesNotAddHardDeadline(t *testing.T) {
	service, token := newDeepSeekHarnessV2ProxyTestService(t, "http://127.0.0.1:43210", 91, "gateway-token")
	deadlineSeen := true
	service.httpClient = &http.Client{Transport: proxyRoundTripper(func(request *http.Request) (*http.Response, error) {
		_, deadlineSeen = request.Context().Deadline()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    request,
		}, nil
	})}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/instances/91/proxy/api/respond", strings.NewReader("{}"))
	recorder := httptest.NewRecorder()
	if err := service.ProxyRequest(context.Background(), 91, token.Token, recorder, request); err != nil {
		t.Fatalf("ProxyRequest() error = %v", err)
	}
	if deadlineSeen {
		t.Fatal("agent proxy added a hard deadline instead of following the browser request lifetime")
	}
	if recorder.Code != http.StatusOK || recorder.Body.String() != "ok" {
		t.Fatalf("proxy response = %d/%q", recorder.Code, recorder.Body.String())
	}
}
