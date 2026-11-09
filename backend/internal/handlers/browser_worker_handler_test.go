package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	cmk8s "clawreef/internal/services/k8s"
	"github.com/gin-gonic/gin"
	"k8s.io/client-go/kubernetes/fake"
)

func TestBrowserWorkerTargetIsInternalService(t *testing.T) {
	t.Setenv(browserWorkerNamespaceEnv, "test-system")
	service := services.NewBrowserWorkerService(nil, nil, &cmk8s.Client{Namespace: "custom"})
	target := browserWorkerTarget(11, service.Namespace())
	if got, want := target.String(), "http://clawbrowser-11.test-system.svc.cluster.local:5800"; got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
}

type browserWorkerHandlerRepo struct {
	repository.BrowserWorkerRepository
	config *models.InstanceBrowserWorker
	writes int
}

func (r *browserWorkerHandlerRepo) Get(int) (*models.InstanceBrowserWorker, error) {
	return r.config, nil
}
func (r *browserWorkerHandlerRepo) UpdateObserved(int, int, string, *string) error {
	r.writes++
	return nil
}
func (r *browserWorkerHandlerRepo) UpdateStatus(int, int, string, *string) error {
	r.writes++
	return nil
}

func TestBrowserWorkerHealthReadCannotAcknowledgePendingConfiguration(t *testing.T) {
	repo := &browserWorkerHandlerRepo{config: &models.InstanceBrowserWorker{InstanceID: 42, Enabled: true, Generation: 1, ObservedGeneration: 0, Status: models.BrowserWorkerStatusError}}
	handler := &InstanceHandler{browserWorkerService: services.NewBrowserWorkerService(repo, nil, &cmk8s.Client{Namespace: "custom"})}
	descriptor := handler.describeBrowserWorker(&models.Instance{ID: 42, Type: "openclaw", InstanceMode: "lite", Status: "running"})
	if descriptor.Available || descriptor.Reason != "browser_worker_config_pending" || repo.writes != 0 || repo.config.ObservedGeneration != 0 {
		t.Fatal("health read acknowledged or hid failed configuration")
	}
}

func TestBrowserWorkerGUIProxyRejectsMissingInvalidAndCrossInstanceCookies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	access := services.NewInstanceAccessService()
	defer access.Stop()
	repo := &browserWorkerHandlerRepo{config: &models.InstanceBrowserWorker{Enabled: true}}
	handler := &InstanceHandler{browserWorkerService: services.NewBrowserWorkerService(repo, nil, nil), accessService: access}
	other, err := access.GenerateToken(7, 43, browserWorkerTokenType, browserWorkerBase(43)+"/", "", browserWorkerPort, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	wrongType, err := access.GenerateToken(7, 42, "openclaw", browserWorkerBase(42)+"/", "", browserWorkerPort, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "invalid", other.Token, wrongType.Token} {
		response := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(response)
		ctx.Params = gin.Params{{Key: "id", Value: "42"}, {Key: "path", Value: "/websockify"}}
		ctx.Request = httptest.NewRequest("GET", "/api/v1/instances/42/browser-proxy/websockify", nil)
		if value != "" {
			ctx.Request.Header.Set("Cookie", browserWorkerCookieName(42)+"="+value)
		}
		ctx.Request.Header.Set("Upgrade", "websocket")
		handler.ProxyBrowserWorker(ctx)
		if response.Code != 401 {
			t.Fatalf("invalid GUI access was not rejected: %d", response.Code)
		}
	}
}

func TestBrowserWorkerCustomNamespaceUsesSameCreationAndProxyTarget(t *testing.T) {
	t.Setenv(browserWorkerNamespaceEnv, "")
	service := services.NewBrowserWorkerService(nil, nil, &cmk8s.Client{Namespace: "custom"})
	if got := browserWorkerTarget(42, service.Namespace()).Host; got != "clawbrowser-42.custom-system.svc.cluster.local:5800" {
		t.Fatalf("wrong proxy host: %s", got)
	}
}

func TestBrowserWorkerInvalidOptInDoesNotCreateInstance(t *testing.T) {
	t.Setenv("CLAWMANAGER_BROWSER_WORKER_IMAGE", "")
	t.Setenv("CLAWMANAGER_CDP_PROXY_IMAGE", "")
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Set("userID", 7)
	ctx.Request = httptest.NewRequest("POST", "/api/v1/instances", strings.NewReader(`{"name":"test","type":"openclaw","os_type":"openclaw","os_version":"latest","cpu_cores":1,"memory_gb":1,"disk_gb":5,"browser_worker":{"enabled":true}}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	handler := &InstanceHandler{browserWorkerService: services.NewBrowserWorkerService(nil, nil, &cmk8s.Client{Clientset: fake.NewSimpleClientset(), Namespace: "custom", WorkspacePVCClaimName: "workspaces"})}
	// instanceService is intentionally nil: touching it would panic.
	handler.CreateInstance(ctx)
	if response.Code != 400 || !strings.Contains(response.Body.String(), "CLAWMANAGER_BROWSER_WORKER_IMAGE") {
		t.Fatalf("unexpected rejection: %d %s", response.Code, response.Body.String())
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
