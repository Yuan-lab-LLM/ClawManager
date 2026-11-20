package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInjectOpenCodeOnboardingRunsAfterStorageBootstrap(t *testing.T) {
	html := `<!doctype html><html><HEAD class="main"><script>window.app=true</script></HEAD></html>`
	withStorage := injectOpenCodeBrowserStorageScope(html, "test-scope")
	result := injectOpenCodeOnboarding(withStorage, true)

	storageIndex := strings.Index(result, `id="clawmanager-opencode-storage-scope"`)
	onboardingIndex := strings.Index(result, `id="clawmanager-opencode-onboarding"`)
	if storageIndex < 0 || onboardingIndex < 0 {
		t.Fatalf("missing injected scripts: %s", result)
	}
	if storageIndex > onboardingIndex {
		t.Fatal("onboarding must use the instance-scoped storage bootstrap")
	}
	if onboardingIndex > strings.Index(result, "</HEAD>") {
		t.Fatal("onboarding must be injected into the document head")
	}
	if strings.Count(result, `id="clawmanager-opencode-onboarding"`) != 1 {
		t.Fatal("onboarding script injected more than once")
	}
	if again := injectOpenCodeOnboarding(result, true); again != result {
		t.Fatal("onboarding injection must be idempotent")
	}
}

func TestOpenCodeOnboardingScriptKeepsUserInControl(t *testing.T) {
	for _, expected := range []string{
		"clawmanager-opencode-onboarding-v1-dismissed",
		"cm-oc-guide",
		"cm-oc-help",
		"cm-oc-primary",
		"attachShadow",
		"localStorage.setItem",
		openCodeOnboardingProjectDirectoryPath,
		openCodeOnboardingRequestHeader,
	} {
		if !strings.Contains(openCodeOnboardingScript, expected) {
			t.Fatalf("onboarding script missing %q", expected)
		}
	}
	for _, forbidden := range []string{"XMLHttpRequest", "WebSocket", "document.cookie"} {
		if strings.Contains(openCodeOnboardingScript, forbidden) {
			t.Fatalf("onboarding script must not perform runtime or credential operations: found %q", forbidden)
		}
	}
}

func TestInjectOpenCodeOnboardingReplacesCreateCapability(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		result := injectOpenCodeOnboarding("<html><head></head><body></body></html>", allowed)
		if strings.Contains(result, "__CLAWMANAGER_OPENCODE_CAN_CREATE__") {
			t.Fatal("create capability placeholder leaked into browser HTML")
		}
		want := "var canCreateProject = false;"
		if allowed {
			want = "var canCreateProject = true;"
		}
		if !strings.Contains(result, want) {
			t.Fatalf("injected capability missing %q", want)
		}
	}
}

func TestOpenCodeLiteProxyReplacesNavigationCachingWorker(t *testing.T) {
	service, token := newOpenCodeV2ProxyTestService(t, "http://127.0.0.1:43210", 123, "gateway-token")
	request := httptest.NewRequest(http.MethodGet, "/api/v1/instances/123/proxy/sw.js", nil)
	request.Header.Set(DedicatedRuntimeOriginHeader, RuntimeTypeOpenCode)
	response := httptest.NewRecorder()
	if err := service.ProxyRequest(context.Background(), 123, token.Token, response, request); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" || !strings.Contains(response.Body.String(), "skipWaiting") {
		t.Fatalf("unexpected worker response: code=%d headers=%v body=%q", response.Code, response.Header(), response.Body.String())
	}
}
