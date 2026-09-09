package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const dashboardTestOrigin = "http://clawmanager.internal:9001"

func dashboardServiceFixture(t *testing.T, upstream string) *HermesDesktopService {
	t.Helper()
	config := desktopFixture(t, upstream).config
	config.Enabled = false
	config.ControlUIOrigin = dashboardTestOrigin
	config.Agent.(*desktopAgent).health = nil
	return NewHermesDesktopService(config)
}

func dashboardTestLogin(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	if r.Method != http.MethodPost || r.Header.Get("Origin") != dashboardTestOrigin || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
		t.Error("login must use fixed internal Origin without browser credentials")
	}
	var body map[string]string
	if json.NewDecoder(r.Body).Decode(&body) != nil || body["provider"] != "basic" || body["username"] != "clawmanager" || body["password"] != "managed-Hermes-password" || body["next"] != "/chat" {
		t.Error("login did not use managed instance credentials")
	}
	w.Header().Set("Content-Type", "application/json")
	http.SetCookie(w, &http.Cookie{Name: "hermes_session_at", Value: "private-upstream-cookie", Path: "/", HttpOnly: true})
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func dashboardActivateClaims(t *testing.T, s *HermesDesktopService) (*HermesDesktopClaims, string) {
	t.Helper()
	descriptor, raw, err := s.ActivateDashboard(context.Background(), 45, 123)
	if err != nil || descriptor == nil || !descriptor.Available || raw == "" {
		t.Fatalf("dashboard activation failed: %v", err)
	}
	if descriptor.RendererURL != "/api/v1/instances/123/proxy/chat/" || descriptor.ExpiresAt == nil || time.Until(*descriptor.ExpiresAt) > HermesDesktopSessionTTL {
		t.Fatalf("unexpected dashboard descriptor: %+v", descriptor)
	}
	claims, err := s.AuthenticateDashboard(context.Background(), raw, 123)
	if err != nil || claims.Surface != "dashboard" {
		t.Fatalf("dashboard cookie rejected: %v", err)
	}
	return claims, raw
}

func TestHermesDashboardActivationAndHTTPKeepUpstreamCookieServerSide(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/auth/password-login" {
			dashboardTestLogin(t, w, r)
			return
		}
		if cookie, err := r.Cookie("hermes_session_at"); err != nil || cookie.Value != "private-upstream-cookie" {
			t.Error("HTTP did not use server-owned cookie")
		}
		if r.Header.Get("Origin") != dashboardTestOrigin || r.Header.Get("X-Forwarded-Prefix") != "/api/v1/instances/123/proxy" {
			t.Error("HTTP origin/base-path boundary missing")
		}
		w.Header().Set("Set-Cookie", "hermes_session_at=do-not-forward; Path=/")
		switch r.URL.Path {
		case "/chat":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><head><script>window.__HERMES_AUTH_REQUIRED__=true;window.__HERMES_BASE_PATH__="/api/v1/instances/123/proxy";</script></head><body>chat</body></html>`))
		case "/api/status":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"0.21.0","model":"configured","password":"managed-Hermes-password","api_key":"private-model-key","runtime_path":"/private/home"}`))
		default:
			t.Errorf("unexpected upstream route: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()
	s := dashboardServiceFixture(t, upstream.URL)
	claims, raw := dashboardActivateClaims(t, s)
	if strings.Contains(raw, "private-upstream-cookie") || strings.Contains(raw, "managed-Hermes-password") {
		t.Fatal("upstream credentials escaped into CM cookie")
	}
	for _, path := range []string{"/", "/chat", "/sessions", "/api/status"} {
		recorder := httptest.NewRecorder()
		if err := s.ProxyDashboardHTTP(context.Background(), claims, http.MethodGet, path, nil, recorder); err != nil {
			t.Fatalf("HTTP %s failed: %v", path, err)
		}
		if recorder.Code != http.StatusOK || recorder.Header().Get("Set-Cookie") != "" || recorder.Header().Get("Location") != "" {
			t.Fatal("upstream auth response headers escaped")
		}
		for _, secret := range []string{"private-upstream-cookie", "managed-Hermes-password", "private-model-key", "/private/home"} {
			if strings.Contains(recorder.Body.String(), secret) {
				t.Fatal("upstream credentials or private status escaped")
			}
		}
	}
	before := hits.Load()
	recorder := httptest.NewRecorder()
	if err := s.ProxyDashboardHTTP(context.Background(), claims, http.MethodGet, "/api/config", nil, recorder); err != nil || !strings.Contains(recorder.Body.String(), "show_token_analytics") {
		t.Fatalf("local managed config failed: %v", err)
	}
	for _, path := range []string{"/api/env/reveal", "/api/files/read", "/api/config/defaults", "/login"} {
		if err := s.ProxyDashboardHTTP(context.Background(), claims, http.MethodGet, path, nil, httptest.NewRecorder()); !errors.Is(err, ErrHermesDesktopForbidden) {
			t.Fatalf("sensitive route was not denied: %s %v", path, err)
		}
	}
	if hits.Load() != before {
		t.Fatal("local/forbidden APIs reached Runtime")
	}
	if _, err := s.Authenticate(context.Background(), raw, 123); err == nil {
		t.Fatal("classic cookie was accepted by optional Desktop surface")
	}
}

func TestHermesDashboardLoginFailureNeverFallsThroughToChat(t *testing.T) {
	var loginHits, chatHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/password-login" {
			loginHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"origin_required private-upstream-cookie"}`))
			return
		}
		chatHits.Add(1)
		_, _ = w.Write([]byte("SIGN IN"))
	}))
	defer upstream.Close()
	s := dashboardServiceFixture(t, upstream.URL)
	descriptor, raw, err := s.ActivateDashboard(context.Background(), 45, 123)
	if err == nil || descriptor != nil || raw != "" || loginHits.Load() != 1 || chatHits.Load() != 0 || strings.Contains(err.Error(), "private-upstream-cookie") {
		t.Fatalf("login failure was not closed safely: login=%d chat=%d err=%v", loginHits.Load(), chatHits.Load(), err)
	}
}

func TestHermesDashboardRejectsSuccessfulStatusLoginOrTokenHTML(t *testing.T) {
	for _, body := range []string{
		`<html><head></head><body><form>SIGN IN</form></body></html>`,
		`<html><head><script>window.__HERMES_AUTH_REQUIRED__=false;window.__HERMES_BASE_PATH__="/api/v1/instances/123/proxy";</script></head><body>chat</body></html>`,
		`<html><head><script>window.__HERMES_AUTH_REQUIRED__=true;window.__HERMES_BASE_PATH__="/api/v1/instances/123/proxy";window.__HERMES_SESSION_TOKEN__="private-browser-token";</script></head><body>chat</body></html>`,
		`<html><head><script>window.__HERMES_AUTH_REQUIRED__=true;window.__HERMES_BASE_PATH__="/api/v1/instances/456/proxy";</script></head><body>chat</body></html>`,
	} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/auth/password-login" {
				dashboardTestLogin(t, w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(body))
		}))
		s := dashboardServiceFixture(t, upstream.URL)
		claims, _ := dashboardActivateClaims(t, s)
		recorder := httptest.NewRecorder()
		err := s.ProxyDashboardHTTP(context.Background(), claims, http.MethodGet, "/chat", nil, recorder)
		upstream.Close()
		if !errors.Is(err, ErrHermesDesktopUpstream) || recorder.Body.Len() != 0 {
			t.Fatal("200 login/insecure/cross-instance HTML was not rejected before writing")
		}
	}
}

func TestHermesDashboardReadonlyAuthenticationRetryBounded(t *testing.T) {
	for _, recoverAfterLogin := range []bool{false, true} {
		t.Run(map[bool]string{false: "persistent401", true: "recover401"}[recoverAfterLogin], func(t *testing.T) {
			var logins, reads atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/auth/password-login" {
					logins.Add(1)
					dashboardTestLogin(t, w, r)
					return
				}
				count := reads.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if !recoverAfterLogin || count == 1 {
					w.Header().Set("Location", "/login")
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = w.Write([]byte(`{"error":"session_expired","login_url":"/login","secret":"private-upstream-cookie"}`))
					return
				}
				_, _ = w.Write([]byte(`{"version":"0.21.0"}`))
			}))
			defer upstream.Close()
			s := dashboardServiceFixture(t, upstream.URL)
			claims, _ := dashboardActivateClaims(t, s)
			recorder := httptest.NewRecorder()
			err := s.ProxyDashboardHTTP(context.Background(), claims, http.MethodGet, "/api/status", nil, recorder)
			if recoverAfterLogin && err != nil || !recoverAfterLogin && !errors.Is(err, ErrHermesDesktopUpstream) {
				t.Fatalf("unexpected retry result: %v", err)
			}
			if logins.Load() != 2 || reads.Load() != 2 || recorder.Header().Get("Location") != "" || strings.Contains(recorder.Body.String(), "login") || strings.Contains(recorder.Body.String(), "private-upstream-cookie") {
				t.Fatalf("retry/response boundary failed: logins=%d reads=%d", logins.Load(), reads.Load())
			}
		})
	}
}

func TestHermesDashboardAuthorizationRechecksAndSurfaceIsolation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { dashboardTestLogin(t, w, r) }))
	defer upstream.Close()
	s := dashboardServiceFixture(t, upstream.URL)
	claims, raw := dashboardActivateClaims(t, s)
	if _, _, err := s.ActivateDashboard(context.Background(), 46, 123); !errors.Is(err, ErrHermesDesktopForbidden) {
		t.Fatal("other owner activated dashboard")
	}
	if _, err := s.AuthenticateDashboard(context.Background(), raw, 124); !errors.Is(err, ErrHermesDesktopForbidden) {
		t.Fatal("cross-instance cookie accepted")
	}
	desktopClaims := *claims
	desktopClaims.Surface = ""
	desktopRaw, err := s.sign(&desktopClaims, "session", HermesDesktopSessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateDashboard(context.Background(), desktopRaw, 123); err == nil {
		t.Fatal("Desktop cookie accepted by classic surface")
	}
	if err := s.ProxyDashboardHTTP(context.Background(), &desktopClaims, http.MethodGet, "/api/auth/me", nil, httptest.NewRecorder()); !errors.Is(err, ErrHermesDesktopForbidden) {
		t.Fatal("cross-surface local identity accepted")
	}
	instances := s.config.Instances.(*v2LifecycleInstanceRepo)
	instances.byID[123].UserID = 46
	if err := s.ProxyDashboardHTTP(context.Background(), claims, http.MethodGet, "/api/auth/me", nil, httptest.NewRecorder()); err == nil {
		t.Fatal("local identity bypassed ownership recheck")
	}
	instances.byID[123].UserID = 45
	instances.byID[123].RuntimeGeneration++
	if _, err := s.AuthenticateDashboard(context.Background(), raw, 123); err == nil {
		t.Fatal("stale generation accepted")
	}
}

func TestHermesDashboardTicketConcurrentReplayAcrossThreeReplicas(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { dashboardTestLogin(t, w, r) }))
	defer upstream.Close()
	s := dashboardServiceFixture(t, upstream.URL)
	claims, _ := dashboardActivateClaims(t, s)
	recorder := httptest.NewRecorder()
	if err := s.ProxyDashboardHTTP(context.Background(), claims, http.MethodPost, "/api/auth/ws-ticket", nil, recorder); err != nil {
		t.Fatal(err)
	}
	var ticket struct {
		Ticket string `json:"ticket"`
		TTL    int    `json:"ttl_seconds"`
	}
	if json.Unmarshal(recorder.Body.Bytes(), &ticket) != nil || ticket.Ticket == "" || ticket.TTL < 1 || ticket.TTL > 30 || strings.Contains(ticket.Ticket, "private-upstream-cookie") {
		t.Fatal("CM ticket does not match stock dashboard response shape")
	}
	replicas := []*HermesDesktopService{s, NewHermesDesktopService(s.config), NewHermesDesktopService(s.config)}
	var accepted atomic.Int32
	var group sync.WaitGroup
	for i := 0; i < 24; i++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			if replicas[index%3].redeemTicket(context.Background(), ticket.Ticket, claims) == nil {
				accepted.Add(1)
			}
		}(i)
	}
	group.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("ticket accepted %d times across replicas", accepted.Load())
	}
}

func TestHermesDashboardPTYBinaryAndPrivateUpstreamSubprotocol(t *testing.T) {
	input := make(chan []byte, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/password-login":
			dashboardTestLogin(t, w, r)
		case "/api/auth/ws-ticket":
			if cookie, err := r.Cookie("hermes_session_at"); err != nil || cookie.Value != "private-upstream-cookie" {
				t.Error("ticket missed server cookie")
			}
			if r.Header.Get("Origin") != dashboardTestOrigin {
				t.Error("ticket missed internal Origin")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ticket":"private-upstream-ticket","ttl_seconds":30}`))
		case "/api/pty":
			if r.Header.Get("Origin") != dashboardTestOrigin || r.URL.Query().Get("channel") != "chat-123" || r.URL.Query().Get("ticket") != "" || r.URL.Query().Get("token") != "" || r.Header.Get("Authorization") != "" || strings.Contains(r.Header.Get("Cookie"), "browser-secret") {
				t.Error("WS upstream credential boundary failed")
			}
			protocols := websocket.Subprotocols(r)
			if len(protocols) != 2 || protocols[0] != "hermes-gateway-v1" || protocols[1] != "hermes-gateway-ticket.private-upstream-ticket" {
				t.Error("WS missed private upstream ticket subprotocol")
			}
			conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }, Subprotocols: []string{"hermes-gateway-v1"}}).Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			_ = conn.WriteMessage(websocket.BinaryMessage, []byte("hello managed-Hermes-"))
			_ = conn.WriteMessage(websocket.BinaryMessage, []byte("password done\n"))
			kind, frame, err := conn.ReadMessage()
			if err == nil && kind == websocket.BinaryMessage {
				input <- frame
			}
			_, _, _ = conn.ReadMessage()
		default:
			t.Error("unexpected PTY upstream path")
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer upstream.Close()
	s := dashboardServiceFixture(t, upstream.URL)
	claims, _ := dashboardActivateClaims(t, s)
	ticketURL, _, err := s.MintTicket(claims)
	if err != nil {
		t.Fatal(err)
	}
	ticketParsed, _ := url.Parse(ticketURL)
	ticket := ticketParsed.Query().Get("ticket")
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.ProxyDashboardWebSocket(r.Context(), claims, r.URL.Query().Get("ticket"), "/api/pty", r.URL.Query(), w, r); err != nil {
			http.Error(w, "denied", http.StatusUnauthorized)
		}
	}))
	defer proxy.Close()
	header := http.Header{"Cookie": {"hermes_session_at=browser-secret"}, "Origin": {"http://browser.example"}}
	conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(proxy.URL, "http")+"/?channel=chat-123&attach=00112233445566778899aabbccddeeff&ticket="+url.QueryEscape(ticket), header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if conn.Subprotocol() != "" || response.Header.Get("Set-Cookie") != "" {
		t.Fatal("upstream auth/subprotocol escaped into browser handshake")
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var output []byte
	for !bytes.Contains(output, []byte("done\n")) {
		kind, frame, err := conn.ReadMessage()
		if err != nil || kind != websocket.BinaryMessage {
			t.Fatalf("binary PTY lost: kind=%d err=%v", kind, err)
		}
		output = append(output, frame...)
	}
	if string(output) != "hello [redacted] done\n" {
		t.Fatal("split-frame upstream credential was not redacted")
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{0, 1, 2, 27}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-input:
		if !bytes.Equal(got, []byte{0, 1, 2, 27}) {
			t.Fatal("PTY input was transformed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("PTY input was not forwarded")
	}
}

func TestHermesDashboardStreamRedactorAllSplitPoints(t *testing.T) {
	secrets := []string{"managed-Hermes-password", "private-upstream-cookie", "private-upstream-ticket"}
	for _, secret := range secrets {
		for split := 1; split < len(secret); split++ {
			redactor := newHermesStreamRedactor(secrets[0], []*http.Cookie{{Name: "hermes_session_at", Value: secrets[1]}}, secrets[2])
			first := append([]byte(nil), redactor.Filter([]byte("before "+secret[:split]))...)
			got := append(first, redactor.Filter([]byte(secret[split:]+" after!"))...)
			if string(got) != "before [redacted] after!" {
				t.Fatalf("split %d failed", split)
			}
		}
	}
	redactor := newHermesStreamRedactor("secret", nil, "")
	if got := redactor.Filter([]byte("normal")); string(got) != "normal" {
		t.Fatal("ordinary output delayed")
	}
	if got := redactor.Filter([]byte("sec")); len(got) != 0 {
		t.Fatal("partial secret escaped")
	}
}
