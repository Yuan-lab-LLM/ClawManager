package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Opt-in, loopback-only browser QA of the REAL Dashboard build through the
// production BFF service. Only static build artifacts are read from disk;
// users, auth, sessions and upstream APIs are synthetic, never cluster data.
// Set HERMES_DASHBOARD_ASSET_FIXTURE to a pinned hermes_cli/web_dist directory,
// then run go test ./internal/services -run '^TestHermesDashboardBrowserFixture$'
// -v -count=1 -timeout=16m. Open http://127.0.0.1:9329 and use "Finish fixture".
// This proves asset loading/rendering, NOT real Runtime chat/WS compatibility.
func TestHermesDashboardBrowserFixture(t *testing.T) {
	root := os.Getenv("HERMES_DASHBOARD_ASSET_FIXTURE")
	if root == "" {
		t.Skip("manual browser fixture requires pinned static build artifacts")
	}
	index, err := os.ReadFile(filepath.Join(root, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	prefix := HermesDashboardBase(123)
	index = []byte(strings.Replace(string(index), "</head>", `<script>window.__HERMES_AUTH_REQUIRED__=true;window.__HERMES_BASE_PATH__="`+prefix+`";</script></head>`, 1))
	assets := http.FileServer(http.Dir(root))
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/password-login" {
			dashboardTestLogin(t, w, r)
			return
		}
		if c, err := r.Cookie("hermes_session_at"); err != nil || c.Value != "private-upstream-cookie" {
			http.Error(w, "synthetic upstream auth required", http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/chat" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(index)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			assets.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		body := `{}`
		switch r.URL.Path {
		case "/api/status":
			body = `{"version":"fixture","model":"fixture-only","gateway_running":true,"active_sessions":0}`
		case "/api/model/info":
			body = `{"model":"fixture-only","provider":"fixture"}`
		case "/api/model/options":
			body = `{"models":[],"providers":[]}`
		case "/api/sessions", "/api/sessions/search":
			body = `{"sessions":[],"total":0}`
		case "/api/dashboard/themes":
			body = `{"themes":[]}`
		case "/api/dashboard/font":
			body = `{"font":"default"}`
		case "/api/sessions/empty/count":
			body = `{"count":0}`
		}
		_, _ = w.Write([]byte(body))
	}))
	defer upstream.Close()
	s := dashboardServiceFixture(t, upstream.URL)
	_, raw := dashboardActivateClaims(t, s)
	var mu sync.Mutex
	requests := map[string]int{}
	failures := map[string]int{}
	syntheticWS := map[string]int{}
	done := make(chan struct{})
	var finish sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("/fixture/stats", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"requests": requests, "failures": failures, "synthetic_websocket_501": syntheticWS})
	})
	mux.HandleFunc("/fixture/finish", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Origin") != "http://127.0.0.1:9329" {
			http.Error(w, "local POST required", http.StatusForbidden)
			return
		}
		fmt.Fprint(w, "Fixture finished. No Runtime or workspace was accessed.")
		finish.Do(func() { close(done) })
	})
	mux.HandleFunc(prefix+"/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, prefix)
		mu.Lock()
		requests[r.URL.Path]++
		mu.Unlock()
		fail := func(status int) {
			mu.Lock()
			failures[r.URL.Path]++
			mu.Unlock()
			http.Error(w, "synthetic fixture response", status)
		}
		cookie, err := r.Cookie(HermesDashboardCookieName(123))
		if err != nil {
			fail(http.StatusUnauthorized)
			return
		}
		claims, err := s.AuthenticateDashboard(r.Context(), cookie.Value, 123)
		if err != nil {
			fail(http.StatusUnauthorized)
			return
		}
		// Deliberately no fake chat socket: browser QA here only proves loading.
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			mu.Lock()
			syntheticWS[r.URL.Path]++
			mu.Unlock()
			http.Error(w, "No live WebSocket in asset fixture", http.StatusNotImplemented)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if err := s.ProxyDashboardHTTP(r.Context(), claims, r.Method, path, r.URL.Query(), w); err != nil {
			fail(http.StatusForbidden)
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			mu.Lock()
			failures[r.URL.Path]++
			mu.Unlock()
			http.NotFound(w, r)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: HermesDashboardCookieName(123), Value: raw, Path: prefix + "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><meta charset="utf-8"><title>Hermes real-build asset fixture</title><body style="margin:0;font:14px system-ui"><header style="padding:8px;background:#fff3ce">真实构建资源 + ClawManager BFF · API 数据为模拟 · 不验证聊天/WS <a href="/fixture/stats" target="_blank">Request results</a> <form style="display:inline" method="post" action="/fixture/finish"><button>Finish fixture</button></form></header><iframe title="Classic Dashboard actual build" src="`+prefix+`/chat/" style="width:100%;height:calc(100vh - 42px);border:0"></iframe></body></html>`)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:9329")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go func() { _ = server.Serve(listener) }()
	t.Log("Asset browser fixture ready: http://127.0.0.1:9329 (synthetic authentication and API only)")
	select {
	case <-done:
	case <-time.After(15 * time.Minute):
		t.Log("fixture lifetime reached")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}
