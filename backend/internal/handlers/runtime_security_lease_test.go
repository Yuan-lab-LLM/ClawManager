package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/runtimeauth"
	"clawreef/internal/services"
)

type leaseIssuerFunc func(context.Context, runtimeauth.Identity, netip.Addr) (services.RuntimeSecurityLease, error)

func (f leaseIssuerFunc) IssueLease(c context.Context, i runtimeauth.Identity, p netip.Addr) (services.RuntimeSecurityLease, error) {
	return f(c, i, p)
}

func TestRuntimeLeaseEarlyRejectionBoundsRealTCPBodyDrain(t *testing.T) {
	for _, scenario := range []string{"missing-control", "invalid-control", "wrong-method"} {
		t.Run(scenario, func(t *testing.T) {
			var calls atomic.Int32
			h, err := NewRuntimeSecurityLeaseHandler(strings.Repeat("c", 32), leaseIssuerFunc(func(context.Context, runtimeauth.Identity, netip.Addr) (services.RuntimeSecurityLease, error) {
				calls.Add(1)
				return services.RuntimeSecurityLease{}, errors.New("must not issue")
			}))
			if err != nil {
				t.Fatal(err)
			}
			closed := make(chan struct{})
			var once sync.Once
			server := httptest.NewUnstartedServer(h)
			server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateClosed {
					once.Do(func() { close(closed) })
				}
			}
			server.Start()
			defer server.Close()
			conn, err := net.Dial("tcp", server.Listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			method, control := "POST", ""
			if scenario == "invalid-control" {
				control = "X-ClawManager-Control-Token: wrong\r\n"
			}
			if scenario == "wrong-method" {
				method = "PUT"
				control = "X-ClawManager-Control-Token: " + strings.Repeat("c", 32) + "\r\n"
			}
			if _, err = conn.Write([]byte(method + " / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 64\r\n" + control + "\r\n{")); err != nil {
				t.Fatal(err)
			}
			conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			response, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				conn.Close()
				t.Fatalf("early rejection did not finish without completing body: %v", err)
			}
			response.Body.Close()
			want := http.StatusUnauthorized
			if scenario == "wrong-method" {
				want = http.StatusMethodNotAllowed
			}
			if response.StatusCode != want || response.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("unexpected rejection: %d %v", response.StatusCode, response.Header)
			}
			if !response.Close {
				t.Fatal("rejected incomplete upload must close the connection")
			}
			select {
			case <-closed:
			case <-time.After(time.Second):
				conn.Close()
				t.Fatal("rejected connection remains alive while client body is incomplete")
			}
			shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := server.Config.Shutdown(shutdownCtx); err != nil {
				t.Fatal("server drain blocked", err)
			}
			if calls.Load() != 0 {
				t.Fatal("unauthorized request reached issuer")
			}
		})
	}
}

func TestRuntimeLeaseUsesActualPeerAndControlCredential(t *testing.T) {
	for _, scenario := range []string{"ok", "missing-control", "model-capability", "wrong-peer", "unknown-field", "trailing-json", "wrong-method", "issuer-error"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			token := strings.Repeat("c", 32)
			h, err := NewRuntimeSecurityLeaseHandler(token, leaseIssuerFunc(func(ctx context.Context, id runtimeauth.Identity, peer netip.Addr) (services.RuntimeSecurityLease, error) {
				calls++
				if id != (runtimeauth.Identity{InstanceID: 7, UserID: 3, Generation: 2, PodUID: "pod-7"}) {
					t.Errorf("wrong identity: %+v", id)
				}
				if peer.String() != "10.2.0.3" || scenario == "issuer-error" {
					return services.RuntimeSecurityLease{}, errors.New("sentinel-secret must not leak")
				}
				return services.RuntimeSecurityLease{ModelToken: "model-secret", EgressToken: "egress-secret"}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			body := `{"instance_id":7,"user_id":3,"generation":2,"pod_uid":"pod-7"}`
			if scenario == "unknown-field" {
				body = strings.TrimSuffix(body, "}") + `,"url":"http://evil"}`
			}
			if scenario == "trailing-json" {
				body += "{}"
			}
			method := "POST"
			if scenario == "wrong-method" {
				method = "GET"
			}
			r := httptest.NewRequest(method, "/api/v1/runtime/security/lease", strings.NewReader(body))
			r.RemoteAddr = "10.2.0.3:45123"
			r.Header.Set("X-ClawManager-Control-Token", token)
			r.Header.Set("X-Forwarded-For", "10.2.0.3")
			if scenario == "wrong-peer" {
				r.RemoteAddr = "10.2.0.99:45123"
			}
			if scenario == "missing-control" || scenario == "model-capability" {
				r.Header.Del("X-ClawManager-Control-Token")
				r.Header.Set("Authorization", "Bearer v1.model-token")
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if scenario == "ok" {
				if w.Code != http.StatusOK {
					t.Fatalf("%d %s", w.Code, w.Body.String())
				}
				var lease services.RuntimeSecurityLease
				if json.Unmarshal(w.Body.Bytes(), &lease) != nil || lease.ModelToken != "model-secret" {
					t.Fatal("missing lease")
				}
			} else {
				if w.Code == http.StatusOK || strings.Contains(w.Body.String(), "secret") {
					t.Fatalf("bad failure %d %s", w.Code, w.Body.String())
				}
			}
			if (scenario == "missing-control" || scenario == "model-capability" || scenario == "unknown-field" || scenario == "trailing-json" || scenario == "wrong-method") && calls != 0 {
				t.Fatal("issued before authorization/validation")
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("lease response cacheable")
			}
		})
	}
}
