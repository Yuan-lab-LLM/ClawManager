package handlers

import (
	"net/http/httptest"
	"testing"
)

func TestBrowserWorkerTargetIsInternalService(t *testing.T) {
	t.Setenv(browserWorkerNamespaceEnv, "test-system")
	target := browserWorkerTarget(11)
	if got, want := target.String(), "http://clawbrowser-11.test-system.svc.cluster.local:5800"; got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
}

func TestBrowserWorkerOriginAllowed(t *testing.T) {
	request := httptest.NewRequest("GET", "https://manager.example/api/v1/instances/11/browser-proxy/websockify", nil)
	request.Host = "manager.example"
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("Origin", "https://manager.example")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	if !browserWorkerOriginAllowed(request) {
		t.Fatal("same-origin request should be allowed")
	}
	request.Header.Set("Origin", "https://attacker.example")
	if browserWorkerOriginAllowed(request) {
		t.Fatal("cross-origin request must be rejected")
	}
}

func TestBrowserWorkerOriginAllowedBehindReverseProxy(t *testing.T) {
	request := httptest.NewRequest("GET", "http://clawmanager-app:9001/api/v1/instances/11/browser-proxy/app/ui.js", nil)
	request.Host = "clawmanager-app:9001"
	request.Header.Set("X-Forwarded-Host", "manager.example:30443")
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("Origin", "https://manager.example:30443")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	if !browserWorkerOriginAllowed(request) {
		t.Fatal("same-origin module request behind Nginx should be allowed")
	}
}
