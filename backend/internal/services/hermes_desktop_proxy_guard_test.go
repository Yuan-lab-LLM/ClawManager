package services

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/models"
)

// Hermes Lite is served only through the managed Desktop BFF. Synthetic
// instance-access tokens must never make the generic proxy reach its runtime.
func hermesProxyGuardFixture(t *testing.T, tokenType, mode string) (*InstanceProxyService, string, *atomic.Int32) {
	t.Helper()
	hits := &atomic.Int32{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"upstream":true}`))
	}))
	t.Cleanup(upstream.Close)
	ip, port := splitURLHostPortForProxyTest(t, upstream.URL)
	password := "managed-test-password"
	workspace := "/workspaces/hermes/user-45/instance-127"
	instances := newV2LifecycleInstanceRepo()
	instances.byID[127] = &models.Instance{
		ID: 127, UserID: 45, Type: RuntimeTypeHermes, RuntimeType: RuntimeBackendGateway,
		InstanceMode: mode, Status: "running", AccessToken: &password,
		WorkspacePath: &workspace, RuntimeGeneration: 5,
	}
	bindings := newFakeRuntimeBindingRepo()
	bindings.bindings[127] = &models.InstanceRuntimeBinding{
		InstanceID: 127, RuntimePodID: 10, GatewayPort: port, State: "running", Generation: 5,
	}
	pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{10: {ID: 10, PodIP: &ip, State: "ready"}}}
	access := NewInstanceAccessService()
	t.Cleanup(access.Stop)
	token, err := access.GenerateToken(45, 127, tokenType, "/api/v1/instances/127/proxy/", "", 3000, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	service := NewInstanceProxyService(access, WithInstanceProxyRuntimeRepositories(instances, pods, bindings))
	service.httpClient = upstream.Client()
	return service, token.Token, hits
}

func TestHermesDesktopGuardPreservesProClassification(t *testing.T) {
	for _, tc := range []struct {
		mode    string
		blocked bool
	}{{InstanceModeLite, true}, {InstanceModePro, false}} {
		t.Run(tc.mode, func(t *testing.T) {
			service, _, hits := hermesProxyGuardFixture(t, RuntimeTypeHermes, tc.mode)
			if got := service.isHermesLiteProxyInstance(127, RuntimeTypeHermes); got != tc.blocked {
				t.Fatalf("Hermes %s classified as Lite=%v", tc.mode, got)
			}
			if hits.Load() != 0 {
				t.Fatal("classification contacted upstream")
			}
		})
	}
}
