package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestHermesManagedUpdateCheckDoesNotReadRuntimeStatus(t *testing.T) {
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "runtime status unavailable", http.StatusServiceUnavailable)
	}))
	defer upstream.Close()
	for _, tc := range []struct {
		name   string
		change func(*HermesDesktopService, *HermesDesktopClaims)
		want   error
	}{
		{name: "verified release"},
		{name: "expired session", change: func(_ *HermesDesktopService, c *HermesDesktopClaims) {
			c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
		}, want: ErrHermesDesktopUnauthorized},
		{name: "stale generation", change: func(_ *HermesDesktopService, c *HermesDesktopClaims) { c.Generation++ }, want: ErrHermesDesktopUnauthorized},
		{name: "revoked session", change: func(_ *HermesDesktopService, c *HermesDesktopClaims) { c.Epoch = "revoked" }, want: ErrHermesDesktopUnauthorized},
		{name: "wrong owner", change: func(s *HermesDesktopService, _ *HermesDesktopClaims) {
			s.config.Instances.(*v2LifecycleInstanceRepo).byID[123].UserID = 46
		}, want: ErrHermesDesktopForbidden},
		{name: "incompatible runtime", change: func(s *HermesDesktopService, _ *HermesDesktopClaims) {
			s.config.Agent.(*desktopAgent).health.Capabilities.HermesDesktopWeb.HermesCommit = "other"
		}, want: ErrHermesDesktopUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := desktopFixture(t, upstream.URL)
			_, port := splitURLHostPortForProxyTest(t, upstream.URL)
			claims := &HermesDesktopClaims{UserID: 45, InstanceID: 123, Generation: 3, PodID: 9, Port: port, Epoch: "initial", RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))}}
			if tc.change != nil {
				tc.change(s, claims)
			}
			body, err := s.ProxyAPI(context.Background(), claims, http.MethodGet, "/hermes/update/check", nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if tc.want != nil {
				return
			}
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatal(err)
			}
			if payload["current_version"] != "0.22.5" || payload["install_method"] != "clawmanager" || payload["can_apply"] != false || payload["update_available"] != false || payload["update_command"] != nil {
				t.Fatalf("unexpected managed update response: %s", body)
			}
		})
	}
	if requests.Load() != 0 {
		t.Fatalf("managed update check made %d unnecessary runtime requests", requests.Load())
	}
}
