package handlers

import (
	"context"
	"errors"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"clawreef/internal/runtimeauth"
	"clawreef/internal/services"
)

func TestRuntimeLeaseSeparatesDependencyFailureFromRevocation(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		err    error
		status int
	}{
		{"deadline", context.DeadlineExceeded, 503},
		{"dependency", runtimeauth.Unavailable(errors.New("secret database address")), 503},
		{"revoked", errors.New("revoked identity"), 403},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			h, err := NewRuntimeSecurityLeaseHandler(strings.Repeat("c", 32), leaseIssuerFunc(func(context.Context, runtimeauth.Identity, netip.Addr) (services.RuntimeSecurityLease, error) {
				return services.RuntimeSecurityLease{}, scenario.err
			}))
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "/api/v1/runtime/security/lease", strings.NewReader(`{"instance_id":7,"user_id":3,"generation":2,"pod_uid":"pod-7"}`))
			r.RemoteAddr = "10.2.0.3:1234"
			r.Header.Set("X-ClawManager-Control-Token", strings.Repeat("c", 32))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != scenario.status || strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "revoked") {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
