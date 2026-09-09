package services

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/models"
)

// All tests below use synthetic legacy instance-access tokens. Hermes Lite may
// no longer bootstrap its upstream session through the generic proxy, including
// external/shared entry points that still possess one of these old tokens.
func legacyHermesProxyFixture(t *testing.T, tokenType, mode string) (*InstanceProxyService, string, *atomic.Int32) {
	t.Helper()
	hits := &atomic.Int32{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"upstream":true}`))
	}))
	t.Cleanup(upstream.Close)
	ip, port := splitURLHostPortForProxyTest(t, upstream.URL)
	password := "legacy-test-managed-password"
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
	token, err := access.GenerateToken(45, 127, tokenType, "/api/v1/instances/127/proxy/chat/", "", 3000, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	service := NewInstanceProxyService(access, WithInstanceProxyRuntimeRepositories(instances, pods, bindings))
	service.httpClient = upstream.Client()
	return service, token.Token, hits
}

func TestLegacyHermesGuardPreservesProClassification(t *testing.T) {
	for _, tc := range []struct {
		mode    string
		blocked bool
	}{{InstanceModeLite, true}, {InstanceModePro, false}} {
		t.Run(tc.mode, func(t *testing.T) {
			service, _, hits := legacyHermesProxyFixture(t, RuntimeTypeHermes, tc.mode)
			if got := service.isHermesLiteProxyInstance(127, RuntimeTypeHermes); got != tc.blocked {
				t.Fatalf("Hermes %s classified as Lite=%v", tc.mode, got)
			}
			if hits.Load() != 0 {
				t.Fatal("classification contacted upstream")
			}
		})
	}
}
