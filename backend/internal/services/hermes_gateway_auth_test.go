package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const hermesGatewayTestOrigin = "https://control.example:9443"

func newHermesGatewayTestAuth(t *testing.T) *HermesGatewayAuth {
	t.Helper()
	a, err := NewHermesGatewayAuth(hermesGatewayTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.transport.CloseIdleConnections)
	return a
}

func hermesGatewayTestTarget(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func hermesGatewayTestKey() HermesGatewayAuthKey {
	return HermesGatewayAuthKey{InstanceID: 7, Generation: 3, RuntimePodID: 29, GatewayPort: 8080}
}

func TestHermesGatewayAuthOriginValidation(t *testing.T) {
	for _, origin := range []string{"http://localhost", "https://control.example:9443", "http://127.0.0.1:80", "https://[::1]:443"} {
		a, err := NewHermesGatewayAuth(origin)
		if err != nil {
			t.Errorf("valid origin %q rejected: %v", origin, err)
		} else {
			a.transport.CloseIdleConnections()
		}
	}
	for _, origin := range []string{"", " https://control.example", "https://control.example ", "ftp://control.example", "https:control.example", "//control.example", "https://user:secret@control.example", "https://control.example/", "https://control.example/chat", "https://control.example?", "https://control.example?secret=x", "https://control.example#", "https://control.example#x", "https://control.example:", "https://control.example:0", "https://control.example:65536", "https://control.example:notaport", "https://-bad.example", "https://bad_name.example", "https://control..example", "https://[::1%25eth0]"} {
		if a, err := NewHermesGatewayAuth(origin); a != nil || !errors.Is(err, ErrHermesGatewayOrigin) {
			t.Errorf("invalid origin %q accepted: %v", origin, err)
		}
	}
}

func TestHermesGatewayAuthSetHeaders(t *testing.T) {
	a := newHermesGatewayTestAuth(t)
	req, _ := http.NewRequest(http.MethodGet, "http://runtime.internal:8080/api/status", nil)
	req.Host = "browser.example"
	for _, name := range []string{"Origin", "Forwarded", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Forwarded-For", "X-Forwarded-Prefix", "Host", "Referer"} {
		req.Header.Set(name, "untrusted-browser")
	}
	a.SetHeaders(req)
	if req.Host != "runtime.internal:8080" || req.Header.Get("Origin") != hermesGatewayTestOrigin || req.Header.Get("X-Forwarded-Host") != "control.example:9443" || req.Header.Get("X-Forwarded-Proto") != "https" {
		t.Fatalf("trusted boundary not normalized: Host=%q headers=%v", req.Host, req.Header)
	}
	for _, name := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Prefix", "Host", "Referer"} {
		if req.Header.Get(name) != "" {
			t.Errorf("untrusted %s retained", name)
		}
	}
}

func TestHermesGatewayAuthLoginAndCacheBinding(t *testing.T) {
	a := newHermesGatewayTestAuth(t)
	var calls atomic.Int32
	var targetHost string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/auth/password-login" || r.URL.RawQuery != "" || r.Host != targetHost {
			t.Errorf("wrong login request: %s %s Host=%s", r.Method, r.URL, r.Host)
		}
		if r.Header.Get("Origin") != hermesGatewayTestOrigin || r.Header.Get("X-Forwarded-Host") != "control.example:9443" || r.Header.Get("X-Forwarded-Proto") != "https" {
			t.Error("login missing trusted forwarding headers")
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if len(payload) != 4 || payload["provider"] != "basic" || payload["username"] != "clawmanager" || payload["next"] != "/chat" || (payload["password"] != "managed-password" && payload["password"] != "rotated-password") {
			t.Error("unexpected managed login payload")
		}
		http.SetCookie(w, &http.Cookie{Name: "hermes_session_at", Value: "server-secret", HttpOnly: true, Path: "/"})
		http.SetCookie(w, &http.Cookie{Name: "csrf_cookie", Value: "not-an-auth-cookie", Path: "/"})
		_, _ = io.WriteString(w, `{ "ok": true }`)
	}))
	defer server.Close()
	target := hermesGatewayTestTarget(t, server.URL+"/chat?ignored=secret#fragment")
	targetHost = target.Host
	key := hermesGatewayTestKey()
	cookies, err := a.Cookie(context.Background(), key, target, "managed-password")
	if err != nil || len(cookies) != 1 || cookies[0].Value != "server-secret" {
		t.Fatalf("login failed: %v", err)
	}
	cookies[0].Value = "caller-mutation"
	again, err := a.Cookie(context.Background(), key, target, "managed-password")
	if err != nil || again[0].Value != "server-secret" || calls.Load() != 1 {
		t.Fatalf("cookie not isolated/cached: %v calls=%d", err, calls.Load())
	}
	variants := []HermesGatewayAuthKey{key, key, key, key}
	variants[0].InstanceID++
	variants[1].Generation++
	variants[2].RuntimePodID++
	variants[3].GatewayPort++
	for _, variant := range variants {
		if _, err := a.Cookie(context.Background(), variant, target, "managed-password"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.Cookie(context.Background(), key, target, "rotated-password"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 6 {
		t.Fatalf("allocation/password cache binding lost, calls=%d", calls.Load())
	}
	a.Invalidate(key)
	if _, err := a.Cookie(context.Background(), key, target, "managed-password"); err != nil || calls.Load() != 7 {
		t.Fatalf("invalidate failed: %v calls=%d", err, calls.Load())
	}
}

func TestHermesGatewayAuthCookieExpiryAndCacheLimit(t *testing.T) {
	for _, tc := range []struct {
		name    string
		maxAge  int
		expires time.Duration
		want    time.Duration
	}{
		{name: "session", want: 5 * time.Minute},
		{name: "long-max-age", maxAge: 3600, want: 5 * time.Minute},
		{name: "short-max-age", maxAge: 60, want: time.Minute},
		{name: "expires", expires: 90 * time.Second, want: 90 * time.Second},
		{name: "earliest-bound", maxAge: 60, expires: 30 * time.Second, want: 30 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newHermesGatewayTestAuth(t)
			now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
			a.now = func() time.Time { return now }
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				cookie := &http.Cookie{Name: "hermes_session_at", Value: "private", MaxAge: tc.maxAge}
				if tc.expires != 0 {
					cookie.Expires = now.Add(tc.expires)
				}
				http.SetCookie(w, cookie)
				_, _ = io.WriteString(w, `{}`)
			}))
			defer server.Close()
			target := hermesGatewayTestTarget(t, server.URL)
			key := hermesGatewayTestKey()
			if _, err := a.Cookie(context.Background(), key, target, "pw"); err != nil {
				t.Fatal(err)
			}
			for _, entry := range a.cache {
				if entry.expires.Sub(now) != tc.want {
					t.Errorf("TTL=%s want=%s", entry.expires.Sub(now), tc.want)
				}
			}
			now = now.Add(tc.want)
			if _, err := a.Cookie(context.Background(), key, target, "pw"); err != nil || calls.Load() != 2 {
				t.Fatalf("expired cookie reused: %v calls=%d", err, calls.Load())
			}
		})
	}
	t.Run("limit", func(t *testing.T) {
		a := newHermesGatewayTestAuth(t)
		now := time.Now()
		var oldest hermesGatewayCacheKey
		for i := 0; i < hermesGatewayCacheMax; i++ {
			key := hermesGatewayCacheKey{allocation: HermesGatewayAuthKey{InstanceID: 1000 + i}}
			a.cache[key] = hermesGatewayCookies{expires: now.Add(time.Duration(i+1) * time.Minute)}
			if i == 0 {
				oldest = key
			}
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.SetCookie(w, &http.Cookie{Name: "hermes_session_at", Value: "private"})
			_, _ = io.WriteString(w, `{}`)
		}))
		defer server.Close()
		if _, err := a.Cookie(context.Background(), hermesGatewayTestKey(), hermesGatewayTestTarget(t, server.URL), "pw"); err != nil {
			t.Fatal(err)
		}
		if len(a.cache) != hermesGatewayCacheMax {
			t.Fatalf("cache size=%d", len(a.cache))
		}
		if _, exists := a.cache[oldest]; exists {
			t.Error("earliest expiring cache entry not evicted")
		}
	})
}

func TestHermesGatewayAuthSessionCookiePrefixes(t *testing.T) {
	for _, name := range []string{"hermes_session_at", "__Host-hermes_session_at", "__Secure-hermes_session_at"} {
		t.Run(name, func(t *testing.T) {
			a := newHermesGatewayTestAuth(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.SetCookie(w, &http.Cookie{Name: name, Value: "private", Path: "/", HttpOnly: true, Secure: true})
				_, _ = io.WriteString(w, `{}`)
			}))
			defer server.Close()
			cookies, err := a.Cookie(context.Background(), hermesGatewayTestKey(), hermesGatewayTestTarget(t, server.URL), "pw")
			if err != nil || len(cookies) != 1 || cookies[0].Name != name {
				t.Fatalf("authenticated cookie rejected: %v", err)
			}
		})
	}
}

func TestHermesGatewayAuthConcurrentLoginAndCancellation(t *testing.T) {
	a := newHermesGatewayTestAuth(t)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		http.SetCookie(w, &http.Cookie{Name: "hermes_session_at", Value: "private"})
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	target, key := hermesGatewayTestTarget(t, server.URL), hermesGatewayTestKey()
	var workers sync.WaitGroup
	errs := make(chan error, 24)
	for i := 0; i < 24; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, err := a.Cookie(context.Background(), key, target, "pw")
			errs <- err
		}()
	}
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Cookie(ctx, key, target, "pw"); !errors.Is(err, ErrHermesGatewayAuth) {
		t.Errorf("cancelled waiter: %v", err)
	}
	close(release)
	workers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("same-key login storm: calls=%d", calls.Load())
	}
}

func TestHermesGatewayAuthInvalidateDoesNotResurrectInFlightCookie(t *testing.T) {
	a := newHermesGatewayTestAuth(t)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		call := calls.Add(1)
		if call == 1 {
			close(started)
			<-release
		}
		http.SetCookie(w, &http.Cookie{Name: "hermes_session_at", Value: fmt.Sprintf("private-%d", call)})
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	target, key := hermesGatewayTestTarget(t, server.URL), hermesGatewayTestKey()
	first := make(chan error, 1)
	go func() { _, err := a.Cookie(context.Background(), key, target, "pw"); first <- err }()
	<-started
	a.Invalidate(key)
	newer, err := a.Cookie(context.Background(), key, target, "pw")
	close(release)
	if err != nil || newer[0].Value != "private-2" {
		t.Fatalf("new login failed: %v", err)
	}
	if err := <-first; !errors.Is(err, ErrHermesGatewayAuth) {
		t.Errorf("invalidated login succeeded: %v", err)
	}
	cached, err := a.Cookie(context.Background(), key, target, "pw")
	if err != nil || cached[0].Value != "private-2" || calls.Load() != 2 {
		t.Fatalf("stale in-flight login resurrected: %v calls=%d", err, calls.Load())
	}
}

func TestHermesGatewayAuthFailuresBoundedAndPrivate(t *testing.T) {
	var redirected atomic.Int32
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { redirected.Add(1) }))
	defer external.Close()
	for _, mode := range []string{"redirect", "unauthorized", "oversize", "no-cookie", "csrf-only", "expired", "deleted", "html", "null", "array"} {
		t.Run(mode, func(t *testing.T) {
			a := newHermesGatewayTestAuth(t)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				cookie := &http.Cookie{Name: "hermes_session_at", Value: "private-cookie"}
				switch mode {
				case "redirect":
					w.Header().Set("Location", external.URL)
					w.WriteHeader(http.StatusFound)
				case "unauthorized":
					w.WriteHeader(http.StatusUnauthorized)
				case "expired":
					cookie.Expires = time.Now().Add(-time.Hour)
					http.SetCookie(w, cookie)
				case "deleted":
					cookie.MaxAge = -1
					http.SetCookie(w, cookie)
				case "oversize":
					http.SetCookie(w, cookie)
					_, _ = io.WriteString(w, strings.Repeat("private-upstream-body", hermesGatewayBodyMax))
				case "csrf-only":
					cookie.Name = "csrf_cookie"
					http.SetCookie(w, cookie)
				case "html":
					http.SetCookie(w, cookie)
					_, _ = io.WriteString(w, `<html>private-upstream-body</html>`)
					return
				case "null":
					http.SetCookie(w, cookie)
					_, _ = io.WriteString(w, `null`)
					return
				case "array":
					http.SetCookie(w, cookie)
					_, _ = io.WriteString(w, `[]`)
					return
				}
				_, _ = io.WriteString(w, `{"detail":"private-upstream-body"}`)
			}))
			defer server.Close()
			cookies, err := a.Cookie(context.Background(), hermesGatewayTestKey(), hermesGatewayTestTarget(t, server.URL), "private-password")
			if len(cookies) != 0 || !errors.Is(err, ErrHermesGatewayAuth) || strings.Contains(err.Error(), "private") || calls.Load() != 1 || len(a.cache) != 0 {
				t.Fatalf("unsafe failure result: %v calls=%d cache=%d", err, calls.Load(), len(a.cache))
			}
		})
	}
	if redirected.Load() != 0 {
		t.Fatal("login followed redirect")
	}
}

func TestHermesGatewayAuthDisablesHTTPProxy(t *testing.T) {
	var proxyCalls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { proxyCalls.Add(1); w.WriteHeader(http.StatusBadGateway) }))
	defer proxy.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "hermes_session_at", Value: "private"})
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	a := newHermesGatewayTestAuth(t)
	a.client = &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(hermesGatewayTestTarget(t, proxy.URL))}}
	if _, err := a.Cookie(context.Background(), hermesGatewayTestKey(), hermesGatewayTestTarget(t, server.URL), "private-password"); err != nil {
		t.Fatal(err)
	}
	if proxyCalls.Load() != 0 {
		t.Fatal("internal password sent through proxy")
	}
}

func TestHermesGatewayWebSocketUsesServerOnlySubprotocolTicket(t *testing.T) {
	for _, path := range []string{"/api/ws", "/api/pty", "/api/events"} {
		t.Run(path, func(t *testing.T) {
			a := newHermesGatewayTestAuth(t)
			var loginCalls, ticketCalls, wsCalls, proxyCalls atomic.Int32
			var runtimeHost string
			query := url.Values{}
			if path != "/api/ws" {
				query.Set("channel", "web")
				query.Set("resume", "session-7")
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Host != runtimeHost || r.Header.Get("Origin") != hermesGatewayTestOrigin || r.Header.Get("X-Forwarded-Host") != "control.example:9443" || r.Header.Get("X-Forwarded-Proto") != "https" {
					t.Error("untrusted runtime boundary")
				}
				if r.URL.Path == "/auth/password-login" {
					loginCalls.Add(1)
					http.SetCookie(w, &http.Cookie{Name: "hermes_session_at", Value: "server-cookie"})
					_, _ = io.WriteString(w, `{}`)
					return
				}
				cookie, err := r.Cookie("hermes_session_at")
				if err != nil || cookie.Value != "server-cookie" {
					t.Error("missing server cookie")
				}
				if r.URL.Path == "/api/auth/ws-ticket" {
					ticketCalls.Add(1)
					if r.Method != http.MethodPost || r.URL.RawQuery != "" {
						t.Error("invalid ticket POST")
					}
					_, _ = io.WriteString(w, `{"ticket":"server-ticket_123-abc"}`)
					return
				}
				wsCalls.Add(1)
				if r.URL.Path != path || r.URL.RawQuery != query.Encode() {
					t.Error("unexpected WS path/query")
				}
				if !reflect.DeepEqual(websocket.Subprotocols(r), []string{hermesGatewayProtocol, "hermes-gateway-ticket.server-ticket_123-abc"}) {
					t.Error("ticket not sent exclusively by subprotocol")
				}
				upgrader := websocket.Upgrader{Subprotocols: []string{hermesGatewayProtocol}, CheckOrigin: func(*http.Request) bool { return true }}
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"ok":true}`))
			}))
			defer server.Close()
			target := hermesGatewayTestTarget(t, server.URL+"/ignored?password=not-forwarded")
			runtimeHost = target.Host
			a.dialer.Proxy = func(*http.Request) (*url.URL, error) { proxyCalls.Add(1); return nil, errors.New("proxy secret") }
			result, err := a.DialWebSocket(context.Background(), hermesGatewayTestKey(), target, "managed-password", path, query)
			if err != nil {
				t.Fatal(err)
			}
			defer result.Conn.Close()
			_ = result.Conn.SetReadDeadline(time.Now().Add(time.Second))
			if _, body, err := result.Conn.ReadMessage(); err != nil || string(body) != `{"ok":true}` {
				t.Fatalf("WS read failed: %v", err)
			}
			if result.Ticket != "server-ticket_123-abc" || len(result.Cookies) != 1 {
				t.Error("missing server-side filter metadata")
			}
			encoded, _ := json.Marshal(result)
			if string(encoded) != "{}" || strings.Contains(fmt.Sprint(result), "server-ticket") {
				t.Error("server credential metadata serializes")
			}
			if loginCalls.Load() != 1 || ticketCalls.Load() != 1 || wsCalls.Load() != 1 || proxyCalls.Load() != 0 {
				t.Fatalf("unexpected request counts: %d/%d/%d/%d", loginCalls.Load(), ticketCalls.Load(), wsCalls.Load(), proxyCalls.Load())
			}
		})
	}
}

func TestHermesGatewayWebSocketRejectsCredentialQueriesBeforeLogin(t *testing.T) {
	a := newHermesGatewayTestAuth(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1) }))
	defer server.Close()
	target := hermesGatewayTestTarget(t, server.URL)
	for _, name := range []string{"token", "ticket", "accessToken", "ACCESS_TOKEN", "internal_key", "password", "api-key", "secret", "Cookie", "authorization", "credential", "channel.token"} {
		if result, err := a.DialWebSocket(context.Background(), hermesGatewayTestKey(), target, "pw", "/api/pty", url.Values{name: {"private"}}); result != nil || !errors.Is(err, ErrHermesGatewayAuth) {
			t.Errorf("credential query %q accepted", name)
		}
	}
	for _, tc := range []struct {
		path  string
		query url.Values
	}{
		{path: "/api/ws", query: url.Values{"channel": {"web"}}},
		{path: "/api/other"},
		{path: "/api/pty", query: url.Values{"channel": {"one", "two"}}},
		{path: "/api/events", query: url.Values{"channel": {strings.Repeat("x", 4097)}}},
	} {
		if result, err := a.DialWebSocket(context.Background(), hermesGatewayTestKey(), target, "pw", tc.path, tc.query); result != nil || !errors.Is(err, ErrHermesGatewayAuth) {
			t.Error("unsafe WS request accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid query triggered upstream authentication")
	}
}

func TestHermesGatewayWebSocketTicketAndHandshakeFailuresNotRetried(t *testing.T) {
	for _, mode := range []string{"unauthorized", "forbidden", "redirect", "invalid-ticket", "oversize", "bad-handshake", "no-subprotocol"} {
		t.Run(mode, func(t *testing.T) {
			a := newHermesGatewayTestAuth(t)
			var ticketCalls, wsCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/auth/password-login":
					http.SetCookie(w, &http.Cookie{Name: "hermes_session_at", Value: "private-cookie"})
					_, _ = io.WriteString(w, `{}`)
				case "/api/auth/ws-ticket":
					ticketCalls.Add(1)
					switch mode {
					case "unauthorized":
						w.WriteHeader(http.StatusUnauthorized)
						_, _ = io.WriteString(w, "private-error")
					case "forbidden":
						w.WriteHeader(http.StatusForbidden)
						_, _ = io.WriteString(w, "private-error")
					case "redirect":
						w.Header().Set("Location", "/auth/private-location")
						w.WriteHeader(http.StatusFound)
					case "invalid-ticket":
						_, _ = io.WriteString(w, `{"ticket":"private ticket with spaces"}`)
					case "oversize":
						_, _ = io.WriteString(w, strings.Repeat("x", hermesGatewayBodyMax+1))
					default:
						_, _ = io.WriteString(w, `{"ticket":"private-ticket"}`)
					}
				case "/api/ws":
					wsCalls.Add(1)
					if mode == "no-subprotocol" {
						upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
						conn, err := upgrader.Upgrade(w, r, nil)
						if err == nil {
							_ = conn.Close()
						}
					} else {
						w.WriteHeader(http.StatusForbidden)
						_, _ = io.WriteString(w, "private-ticket")
					}
				}
			}))
			defer server.Close()
			result, err := a.DialWebSocket(context.Background(), hermesGatewayTestKey(), hermesGatewayTestTarget(t, server.URL), "private-password", "/api/ws", nil)
			if result != nil || !errors.Is(err, ErrHermesGatewayAuth) || strings.Contains(err.Error(), "private") || ticketCalls.Load() != 1 || wsCalls.Load() > 1 {
				t.Fatalf("unsafe or retried failure: %v ticket=%d ws=%d", err, ticketCalls.Load(), wsCalls.Load())
			}
			if mode == "unauthorized" || mode == "forbidden" || mode == "redirect" || mode == "bad-handshake" {
				if len(a.cache) != 0 {
					t.Error("failed authentication did not invalidate cached credentials")
				}
			} else if len(a.cache) != 1 {
				t.Error("non-authentication failure unnecessarily invalidated credentials")
			}
		})
	}
}
