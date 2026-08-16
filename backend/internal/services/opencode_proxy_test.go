package services

import (
	"encoding/base64"
	"net/http"
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

func TestGetProxyURLForInstanceOpenCodeLiteUsesRoot(t *testing.T) {
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

	want := "/api/v1/instances/123/proxy/?token=token%2Bwith%2Fslash"
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

	if !service.shouldRewriteHTMLForProxy(7, "opencode") {
		t.Fatal("expected lite opencode to force HTML rewrite")
	}
	if service.shouldRewriteHTMLForProxy(8, "opencode") {
		t.Fatal("pro opencode must not force lite HTML rewrite")
	}
}

func TestIsOpenCodeEventStreamRequest(t *testing.T) {
	if !isOpenCodeEventStreamRequest(true, "/global/event") {
		t.Fatal("expected OpenCode Lite global event stream to bypass the HTTP timeout")
	}
	if isOpenCodeEventStreamRequest(false, "/global/event") {
		t.Fatal("non-Lite requests must keep the standard HTTP timeout")
	}
	if isOpenCodeEventStreamRequest(true, "/api/health") {
		t.Fatal("non-event OpenCode requests must keep the standard HTTP timeout")
	}
}
