package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

func TestHermesDesktopRendererHTTPPolicy(t *testing.T) {
	for _, raw := range []string{
		"/config", "/config/defaults", "/config/schema", "/model/options?explicit_only=1&refresh=true&profile=default",
		"/profiles/sessions?profile=all&limit=100&archived=include&exclude_sources=cron,telegram",
		"/profiles/sessions/sidebar?recents_profile=all&recents_limit=40&cron_limit=20&messaging_limit=40&recents_exclude=cron,telegram&messaging_exclude=desktop,web,tui",
		"/sessions/abc-123?profile=current", "/sessions/abc/messages?include_compacted=true&limit=500&offset=120&order=latest",
	} {
		u, _ := url.Parse(raw)
		if !hermesDesktopHTTPAllowed(http.MethodGet, u.Path, u.Query()) {
			t.Errorf("required Desktop read rejected: %s", raw)
		}
		if hermesDesktopHTTPAllowed(http.MethodPost, u.Path, u.Query()) {
			t.Errorf("write accepted: %s", raw)
		}
	}
	for _, raw := range []string{
		"/config?profile=another", "/profiles/other", "/profiles/sessions?profile=../other", "/sessions/search", "/sessions/stats",
		"/profiles/sessions/sidebar?recents_profile=another", "/profiles/sessions/sidebar?recents_limit=101",
		"/profiles/sessions/sidebar?recents_exclude=cron%26profile=other", "/profiles/sessions/sidebar?cwd_prefix=/workspace",
		"/sessions/abc/messages?include_compacted=true&include_compacted=false",
		"/sessions/abc/messages?limit=501",
		"/sessions?full=1", "/sessions?profile=all", "/sessions/abc/messages?profile=other", "/sessions/abc/export",
	} {
		u, _ := url.Parse(raw)
		if hermesDesktopHTTPAllowed(http.MethodGet, u.Path, u.Query()) {
			t.Errorf("unsafe read accepted: %s", raw)
		}
	}
	u, _ := url.Parse("/model/options?include_unconfigured=true")
	if !hermesDesktopHTTPAllowed(http.MethodGet, u.Path, u.Query()) {
		t.Error("provider settings cannot request unconfigured providers")
	}
}

func TestHermesDesktopRendererProjection(t *testing.T) {
	for _, tc := range []struct{ path, input, retain string }{
		{"/config", `{"agent":{"reasoning_effort":"high","custom":"preserved"},"terminal":{"cwd":"/workspace","font_family":"mono"},"env":{"OPENAI_API_KEY":"DO-NOT-EXPOSE"},"display":{"timestamps":true,"skin":{"name":"dark"}}}`, "reasoning_effort"},
		{"/model/options", `{"provider":"managed","model":"m","providers":[{"slug":"managed","name":"Managed","models":["m"],"authenticated":true,"key_env":"OPENAI_API_KEY","base_url":"DO-NOT-EXPOSE"},{"slug":"external","models":["x"],"authenticated":false,"name":"External"}],"api_key":"DO-NOT-EXPOSE"}`, "managed"},
		{"/sessions/abc", `{"id":"abc","title":"My chat","profile":"DO-NOT-EXPOSE","connection_id":"DO-NOT-EXPOSE","model_config":{"hidden":"DO-NOT-EXPOSE"},"cwd":"DO-NOT-EXPOSE"}`, "My chat"},
		{"/sessions/abc/messages", `{"session_id":"abc","messages":[{"role":"system","content":"DO-NOT-EXPOSE"},{"role":"assistant","content":"reply","model_config":"DO-NOT-EXPOSE"}],"pagination":{"limit":120,"offset":0,"returned":1,"order":"latest","internal":"DO-NOT-EXPOSE"}}`, "reply"},
	} {
		body, err := hermesDesktopProjectHTTP(tc.path, []byte(tc.input))
		if err == nil && (tc.path == "/config" || tc.path == "/config/defaults") {
			body, err = hermesDesktopRedactStringValues(body, "DO-NOT-EXPOSE")
		}
		if err != nil || strings.Contains(string(body), "DO-NOT-EXPOSE") || !strings.Contains(string(body), tc.retain) {
			t.Errorf("projection %s failed: %s %v", tc.path, body, err)
		}
	}
	schema, err := hermesDesktopProjectHTTP("/config/schema", []byte(`{"fields":{"security.api_key":{"category":"security","description":"Credential field","type":"string","options":["one",2,true],"private":"DO-NOT-EXPOSE"}},"category_order":["security"],"internal":"DO-NOT-EXPOSE"}`))
	if err != nil || !strings.Contains(string(schema), `"security.api_key"`) || strings.Contains(string(schema), "DO-NOT-EXPOSE") {
		t.Fatalf("schema projection lost safe field metadata: %s %v", schema, err)
	}
	if _, err := hermesDesktopProjectHTTP("/sessions", []byte(`{"sessions":[{"title":"missing id"}]}`)); err == nil {
		t.Fatal("invalid session record became fake valid history")
	}
	body, err := hermesDesktopProjectHTTP("/sessions/abc/messages", []byte(`{"session_id":"abc","messages":[{"id":1,"role":"system","content":"private-system-prompt"},{"id":2,"role":"assistant","content":"reply"}],"pagination":{"limit":2,"offset":120,"returned":2,"order":"latest"}}`))
	var page struct {
		Messages   []map[string]any                      `json:"messages"`
		Pagination struct{ Offset, Limit, Returned int } `json:"pagination"`
	}
	if err != nil || json.Unmarshal(body, &page) != nil || len(page.Messages) != 2 || page.Messages[0]["content"] != "" || page.Messages[0]["display_kind"] != "hidden" || page.Pagination.Offset+len(page.Messages) != 122 || page.Pagination.Returned != len(page.Messages) || strings.Contains(string(body), "private-system-prompt") {
		t.Fatal("system projection broke renderer pagination or exposed a prompt")
	}
}

func TestHermesDesktopRendererModelCapabilitiesProjection(t *testing.T) {
	body, err := hermesDesktopProjectHTTP("/model/options", []byte(`{"provider":"custom:managed","model":"m","providers":[{"slug":"managed","authenticated":true,"is_user_defined":true,"aliases":["managed","custom:managed"],"api_url":"http://localhost:8000/v1","models":["m"],"capabilities":{"m":{"reasoning":false,"fast":true,"can_disable_reasoning":false,"secret":"private-key"},"unlisted":{"fast":true}}}]}`))
	if err != nil || strings.Contains(string(body), `"secret"`) || strings.Contains(string(body), "unlisted") || !strings.Contains(string(body), `"reasoning":false`) || !strings.Contains(string(body), `"fast":true`) || !strings.Contains(string(body), `"custom:managed"`) || !strings.Contains(string(body), "localhost:8000") {
		t.Fatalf("model UI capabilities were lost or unreviewed metadata escaped: %s %v", body, err)
	}
}

func TestHermesDesktopRendererSessionOptionPolicy(t *testing.T) {
	for key, values := range map[string][]string{"reasoning": {"none", "minimal", "low", "medium", "high", "xhigh", "max"}, "fast": {"fast", "normal"}} {
		for _, value := range values {
			frame, err := hermesDesktopFilterRPC(desktopRPCFrame(1, "config.set", map[string]any{"session_id": "live", "key": key, "value": value}))
			if err != nil || !(new(HermesDesktopService)).desktopRPCModelAllowed(context.Background(), nil, frame) {
				t.Fatalf("session option rejected: %s=%s: %v", key, value, err)
			}
			if !errors.Is(newHermesDesktopRPCScope().admit(frame), errHermesDesktopUnboundSession) {
				t.Fatal("option write bypassed live-session ownership")
			}
		}
	}
	for _, params := range []map[string]any{
		{"key": "reasoning", "value": "high"},
		{"session_id": "live", "key": "reasoning", "value": "show"},
		{"session_id": "live", "key": "reasoning", "value": "high", "scope": "global"},
		{"session_id": "live", "key": "fast", "value": "toggle"},
		{"session_id": "live", "key": "fast", "value": true},
		{"session_id": "live", "key": "fast", "value": "normal", "confirm_expensive_model": true},
	} {
		if _, err := hermesDesktopFilterRPC(desktopRPCFrame(1, "config.set", params)); err == nil {
			t.Errorf("unsafe session option accepted: %+v", params)
		}
	}
}

func TestHermesDesktopRendererReasoningExactPolicy(t *testing.T) {
	efforts := []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}
	invalid := []string{"", " low", "low ", "LOW", "show", strings.Join(efforts, " ")}
	for i := 0; i+1 < len(efforts); i++ {
		invalid = append(invalid, efforts[i]+" "+efforts[i+1])
	}
	for _, method := range []string{"session.create", "config.set"} {
		check := func(value string, allowed bool) {
			t.Helper()
			params := map[string]any{"reasoning_effort": value}
			if method == "config.set" {
				params = map[string]any{"session_id": "live", "key": "reasoning", "value": value}
			}
			_, err := hermesDesktopFilterRPC(desktopRPCFrame(1, method, params))
			if (err == nil) != allowed {
				t.Errorf("%s reasoning %q: allowed=%t, error=%v", method, value, allowed, err)
			}
		}
		for _, effort := range efforts {
			check(effort, true)
		}
		for _, value := range invalid {
			check(value, false)
		}
	}
}

func TestHermesDesktopRendererRPCFieldNamesAreExact(t *testing.T) {
	for _, tc := range []struct {
		method string
		params map[string]any
	}{
		{"session.create", map[string]any{"model provider": "unexpected"}},
		{"session.create", map[string]any{"reasoning_effort fast": true}},
		{"session.resume", map[string]any{"session_id": "live", "session_id source": "unexpected"}},
		{"config.set", map[string]any{"session_id": "live", "key": "reasoning", "value": "high", "key value": "unexpected"}},
		{"model.options", map[string]any{"explicit_only refresh": true}},
	} {
		if _, err := hermesDesktopFilterRPC(desktopRPCFrame(1, tc.method, tc.params)); !errors.Is(err, ErrHermesDesktopForbidden) {
			t.Errorf("%s accepted combined field names: %+v, error=%v", tc.method, tc.params, err)
		}
	}
}

func TestHermesDesktopRendererCatalogBoundToRuntimeIdentity(t *testing.T) {
	var reads, edition atomic.Int32
	edition.Store(1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/password-login" {
			var request struct {
				Password string `json:"password"`
				Provider string `json:"provider"`
			}
			if json.NewDecoder(r.Body).Decode(&request) != nil || request.Provider != "basic" || (request.Password != "managed-Hermes-password" && request.Password != "rotated-managed-password") {
				t.Error("invalid managed login")
			}
			http.SetCookie(w, &http.Cookie{Name: "hermes_session_at", Value: "fixture-private-cookie", Path: "/", HttpOnly: true})
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		if r.URL.Path != "/api/model/options" || r.URL.Query().Get("explicit_only") != "1" || r.URL.Query().Has("profile") {
			t.Error("unexpected model catalogue scope")
			w.WriteHeader(500)
			return
		}
		reads.Add(1)
		if edition.Load() == 3 {
			w.WriteHeader(503)
			return
		}
		model := "m1"
		if edition.Load() == 2 {
			model = "m2"
		}
		// The configured current pair is authoritative even with an empty
		// inventory; unconfigured providers must not become selectable.
		_ = json.NewEncoder(w).Encode(map[string]any{"provider": "managed", "model": model, "providers": []any{map[string]any{"slug": "external", "authenticated": false, "models": []string{"forbidden"}}}})
	}))
	defer upstream.Close()
	s := desktopFixture(t, upstream.URL)
	target, _, err := s.resolve(context.Background(), 45, 123)
	if err != nil {
		t.Fatal(err)
	}
	if !s.desktopModelAllowed(context.Background(), target, "managed", "m1") || s.desktopModelAllowed(context.Background(), target, "external", "forbidden") || reads.Load() != 1 {
		t.Fatal("configured catalogue filtering/cache failed")
	}
	edition.Store(2)
	if s.desktopModelAllowed(context.Background(), target, "managed", "m2") {
		t.Fatal("unfetched model accepted")
	}
	target.binding.Generation++
	if !s.desktopModelAllowed(context.Background(), target, "managed", "m2") || reads.Load() != 2 {
		t.Fatal("new generation reused previous catalogue")
	}
	password := "rotated-managed-password"
	target.instance.AccessToken = &password
	if !s.desktopModelAllowed(context.Background(), target, "managed", "m2") || reads.Load() != 3 {
		t.Fatal("credential rotation reused previous catalogue")
	}
	target.instance.ID++
	if !s.desktopModelAllowed(context.Background(), target, "managed", "m2") || reads.Load() != 4 {
		t.Fatal("another instance reused previous catalogue")
	}
	edition.Store(3)
	target.binding.Generation++
	if s.desktopModelAllowed(context.Background(), target, "managed", "m2") || reads.Load() != 5 {
		t.Fatal("unavailable real catalogue became success")
	}
}

func TestHermesDesktopRendererSingleInstanceSidebar(t *testing.T) {
	var reads atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/password-login" {
			desktopTestLogin(t, w, r)
			return
		}
		if r.URL.Path != "/api/sessions" || r.URL.Query().Has("profile") || r.URL.Query().Has("recents_profile") {
			t.Errorf("cross-profile or unexpected request: %s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		if _, err := r.Cookie("hermes_session_at"); err != nil {
			t.Error("missing server-side authentication")
		}
		reads.Add(1)
		source := r.URL.Query().Get("source")
		if source == "" {
			source = r.URL.Query().Get("exclude_sources")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"sessions": []any{map[string]any{"id": "stored-1", "title": source, "profile": "private-home", "cwd": "private-home"}}, "total": 3, "offset": 0, "limit": 1})
	}))
	defer upstream.Close()
	s := desktopFixture(t, upstream.URL)
	_, cookie, err := s.Activate(context.Background(), 45, 123)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := s.Authenticate(context.Background(), cookie, 123)
	if err != nil {
		t.Fatal(err)
	}
	query := url.Values{"recents_profile": {"all"}, "recents_limit": {"1"}, "cron_limit": {"1"}, "messaging_limit": {"1"}, "recents_exclude": {"cron"}, "messaging_exclude": {"web,tui"}}
	body, err := s.ProxyAPI(context.Background(), claims, "GET", "/profiles/sessions/sidebar", query)
	if err != nil || reads.Load() != 3 || strings.Contains(string(body), "private-home") {
		t.Fatalf("sidebar did not use projected real single-instance reads: %s %v reads=%d", body, err, reads.Load())
	}
	var payload map[string]struct {
		Sessions  []map[string]any `json:"sessions"`
		Truncated map[string]bool  `json:"profiles_truncated"`
	}
	if json.Unmarshal(body, &payload) != nil || len(payload) != 3 || payload["cron"].Sessions[0]["title"] != "cron" || !payload["recents"].Truncated["default"] {
		t.Fatalf("bad sidebar shape: %s", body)
	}
	if _, err := s.ProxyAPI(context.Background(), claims, "GET", "/profiles/sessions/sidebar", url.Values{"recents_profile": {"other"}}); !errors.Is(err, ErrHermesDesktopForbidden) || reads.Load() != 3 {
		t.Fatal("foreign scope reached Runtime")
	}
}

func desktopRPCFrame(id int, method string, params map[string]any) []byte {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	return body
}

func TestHermesDesktopRendererRPCScope(t *testing.T) {
	if _, err := hermesDesktopFilterRPC(desktopRPCFrame(50, "prompt.submit", map[string]any{"session_id": "live", "text": "next turn", "queued": true, "interrupted": false})); err != nil {
		t.Fatal("real Desktop queued prompt rejected")
	}
	if _, err := hermesDesktopFilterRPC(desktopRPCFrame(50, "prompt.submit", map[string]any{"session_id": "live", "text": "next turn", "surface": "hud"})); err == nil {
		t.Fatal("native HUD hint accepted")
	}
	frame, err := hermesDesktopFilterRPC(desktopRPCFrame(1, "session.create", map[string]any{"source": "desktop", "profile": "default", "cwd": "", "cols": 96, "fast": false, "reasoning_effort": "high"}))
	if err != nil || !strings.Contains(string(frame), `"source":"web"`) || strings.Contains(string(frame), `"profile"`) || strings.Contains(string(frame), `"cwd"`) {
		t.Fatalf("native surface not normalized: %s %v", frame, err)
	}
	scope := newHermesDesktopRPCScope()
	if !errors.Is(scope.admit(desktopRPCFrame(2, "session.close", map[string]any{"session_id": "foreign"})), errHermesDesktopUnboundSession) {
		t.Fatal("unbound runtime accepted")
	}
	if scope.admit(frame) != nil {
		t.Fatal("create rejected")
	}
	if scope.admit(frame) == nil {
		t.Fatal("outstanding request ID overwritten")
	}
	if _, err := scope.observe([]byte(`{"jsonrpc":"2.0","id":1,"result":{"session_id":"live-1","stored_session_id":"stored-1"}}`)); err != nil {
		t.Fatal(err)
	}
	for i, method := range []string{"session.activate", "session.usage", "config.set", "session.close"} {
		if scope.admit(desktopRPCFrame(i+2, method, map[string]any{"session_id": "live-1"})) != nil {
			t.Errorf("bound %s rejected", method)
		}
	}
	_, _ = scope.observe([]byte(`{"jsonrpc":"2.0","id":5,"result":{"closed":true}}`))
	if scope.admit(desktopRPCFrame(6, "config.set", map[string]any{"session_id": "live-1"})) == nil {
		t.Fatal("closed runtime remained writable")
	}
	for _, params := range []map[string]any{
		{"session_id": "live-1", "key": "model", "value": "m --provider managed --global"},
		{"session_id": "live-1", "key": "approvals.mode", "value": "off"},
		{"session_id": "live-1", "key": "model", "value": "m --provider managed --session --global"},
		{"session_id": "live-1", "key": "model", "value": "m\n--provider managed --session"},
		{"key": "model", "value": "m --provider managed --session"},
	} {
		if _, err := hermesDesktopFilterRPC(desktopRPCFrame(9, "config.set", params)); err == nil {
			t.Errorf("unsafe model write accepted: %+v", params)
		}
	}
	if _, err := hermesDesktopFilterRPC(desktopRPCFrame(9, "config.set", map[string]any{"session_id": "live-1", "key": "model", "value": "m --provider managed --session", "confirm_expensive_model": true})); err != nil {
		t.Fatal(err)
	}
}

func TestHermesDesktopRendererRealWebSocketBridge(t *testing.T) {
	var forwarded atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/password-login":
			desktopTestLogin(t, w, r)
		case "/api/auth/ws-ticket":
			_, _ = w.Write([]byte(`{"ticket":"upstream-ticket"}`))
		case "/api/model/options":
			_, _ = w.Write([]byte(`{"provider":"managed","model":"m","providers":[{"slug":"managed","name":"Managed","models":["m","m2"],"authenticated":true}]}`))
		case "/api/ws":
			conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }, Subprotocols: []string{"hermes-gateway-v1"}}).Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			for {
				_, raw, err := conn.ReadMessage()
				if err != nil {
					return
				}
				var p struct {
					ID     json.RawMessage `json:"id"`
					Method string          `json:"method"`
					Params map[string]any  `json:"params"`
				}
				_ = json.Unmarshal(raw, &p)
				forwarded.Add(1)
				result := map[string]any{"ok": true}
				if p.Method == "session.create" || p.Method == "session.resume" {
					if p.Params["source"] != "web" || p.Params["profile"] != nil {
						t.Error("native/profile selector reached runtime")
					}
					result = map[string]any{"session_id": "live-1", "stored_session_id": "stored-1"}
					if p.Method == "session.resume" {
						result["session_id"] = "live-2"
					}
				}
				if p.Method == "config.set" && p.Params["value"] != "m2 --provider managed --session" {
					t.Error("non-session model write reached runtime")
				}
				if err := conn.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": p.ID, "result": result}); err != nil {
					return
				}
			}
		default:
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	s := desktopFixture(t, upstream.URL)
	_, cookie, err := s.Activate(context.Background(), 45, 123)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := s.Authenticate(context.Background(), cookie, 123)
	if err != nil {
		t.Fatal(err)
	}
	ticket, _, err := s.MintTicket(claims)
	if err != nil {
		t.Fatal(err)
	}
	ticketURL, _ := url.Parse(ticket)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.ProxyWebSocket(r.Context(), claims, r.URL.Query().Get("ticket"), w, r); err != nil {
			http.Error(w, "bridge failed", 502)
		}
	}))
	defer proxy.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(proxy.URL, "http")+"?ticket="+url.QueryEscape(ticketURL.Query().Get("ticket")), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(8 * time.Second))
	roundtrip := func(id int, method string, params map[string]any, wantError bool) map[string]json.RawMessage {
		t.Helper()
		if err := conn.WriteMessage(websocket.TextMessage, desktopRPCFrame(id, method, params)); err != nil {
			t.Fatal(err)
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		var response map[string]json.RawMessage
		_ = json.Unmarshal(raw, &response)
		if (len(response["error"]) > 0) != wantError {
			t.Fatalf("unexpected %s outcome: %s", method, raw)
		}
		return response
	}
	roundtrip(1, "config.set", map[string]any{"session_id": "unowned", "key": "model", "value": "m2 --provider managed --session"}, true)
	roundtrip(2, "session.create", map[string]any{"source": "desktop", "cols": 96, "profile": "default", "model": "m", "provider": "managed", "fast": false}, false)
	roundtrip(3, "config.set", map[string]any{"session_id": "live-1", "key": "model", "value": "m2 --provider managed --session"}, false)
	roundtrip(4, "config.set", map[string]any{"session_id": "live-1", "key": "model", "value": "evil --provider external --session"}, true)
	roundtrip(5, "session.activate", map[string]any{"session_id": "live-1", "cols": 96, "omit_messages": true}, false)
	if forwarded.Load() != 3 {
		t.Fatalf("blocked operations reached upstream: %d", forwarded.Load())
	}
	_ = conn.Close()
	ticket, _, err = s.MintTicket(claims)
	if err != nil {
		t.Fatal(err)
	}
	ticketURL, _ = url.Parse(ticket)
	conn, _, err = websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(proxy.URL, "http")+"?ticket="+url.QueryEscape(ticketURL.Query().Get("ticket")), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(8 * time.Second))
	response := roundtrip(6, "session.activate", map[string]any{"session_id": "live-1", "cols": 96, "omit_messages": true}, true)
	var failure struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(response["error"], &failure)
	if failure.Code != 4001 || !strings.Contains(failure.Message, "session not found") || strings.Contains(failure.Message, "-32601") {
		t.Fatal("warm cache would not select original renderer's cold-resume branch")
	}
	roundtrip(7, "session.events.since", map[string]any{"session_id": "live-1", "last_seen": 3}, false)
	roundtrip(8, "session.resume", map[string]any{"session_id": "stored-1", "source": "desktop", "cols": 96, "profile": "current", "omit_messages": true, "defer_history": true}, false)
	roundtrip(9, "session.activate", map[string]any{"session_id": "live-2", "cols": 96, "omit_messages": true}, false)
	if forwarded.Load() != 6 {
		t.Fatalf("reconnect recovery forwarded wrong requests: %d", forwarded.Load())
	}
}

func TestHermesDesktopRendererLeaseExpiresAndFreshLeaseReconnects(t *testing.T) {
	var dials atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/password-login":
			desktopTestLogin(t, w, r)
		case "/api/auth/ws-ticket":
			_, _ = w.Write([]byte(`{"ticket":"fixture-private-ticket"}`))
		case "/api/ws":
			conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }, Subprotocols: []string{hermesGatewayProtocol}}).Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			dials.Add(1)
			for {
				_, raw, err := conn.ReadMessage()
				if err != nil {
					return
				}
				var p map[string]json.RawMessage
				_ = json.Unmarshal(raw, &p)
				if conn.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": p["id"], "result": map[string]any{"pong": true}}) != nil {
					return
				}
			}
		default:
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	s := desktopFixture(t, upstream.URL)
	_, raw, err := s.Activate(context.Background(), 45, 123)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Authenticate(context.Background(), raw, 123)
	if err != nil {
		t.Fatal(err)
	}
	first.ExpiresAt = jwt.NewNumericDate(time.Now().Add(3 * time.Second))
	firstRaw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, first).SignedString(s.key)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(HermesDesktopCookieName(123))
		if err != nil {
			http.Error(w, "denied", 401)
			return
		}
		claims, err := s.Authenticate(r.Context(), cookie.Value, 123)
		if err != nil {
			http.Error(w, "denied", 401)
			return
		}
		if err := s.ProxyWebSocket(r.Context(), claims, r.URL.Query().Get("ticket"), w, r); err != nil {
			http.Error(w, "denied", 401)
		}
	}))
	defer proxy.Close()
	dial := func(claims *HermesDesktopClaims, cookie string) (*websocket.Conn, error) {
		ticket, _, err := s.MintTicket(claims)
		if err != nil {
			return nil, err
		}
		u, _ := url.Parse(ticket)
		conn, resp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(proxy.URL, "http")+"?ticket="+url.QueryEscape(u.Query().Get("ticket")), http.Header{"Cookie": {HermesDesktopCookieName(123) + "=" + cookie}})
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return conn, err
	}
	old, err := dial(first, firstRaw)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	_, freshRaw, err := s.Activate(context.Background(), 45, 123)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := s.Authenticate(context.Background(), freshRaw, 123)
	if err != nil {
		t.Fatal(err)
	}
	_ = old.SetReadDeadline(time.Now().Add(6 * time.Second))
	_, _, err = old.ReadMessage()
	if !websocket.IsCloseError(err, 4401) {
		t.Fatalf("old Desktop lease did not end: %v", err)
	}
	// The pinned shared client treats all close codes as closed/reconnect;
	// this proves the fresh-ticket transport path without extending old claims.
	next, err := dial(fresh, freshRaw)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	_ = next.SetReadDeadline(time.Now().Add(3 * time.Second))
	if next.WriteMessage(websocket.TextMessage, desktopRPCFrame(1, "ping", nil)) != nil {
		t.Fatal("fresh socket send failed")
	}
	if _, _, err := next.ReadMessage(); err != nil || dials.Load() != 2 {
		t.Fatalf("fresh authenticated reconnect failed: %v", err)
	}
	if err := s.RevokeUserSessions(context.Background(), 45); err != nil {
		t.Fatal(err)
	}
	if conn, err := dial(fresh, freshRaw); err == nil {
		_ = conn.Close()
		t.Fatal("revoked lease reconnected")
	}
	if dials.Load() != 2 {
		t.Fatal("revoked reconnect reached upstream")
	}
}

func TestHermesDesktopRendererLogoutWinsDelayedModelValidation(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var forwarded atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/password-login":
			desktopTestLogin(t, w, r)
		case "/api/auth/ws-ticket":
			_, _ = w.Write([]byte(`{"ticket":"fixture-private-ticket"}`))
		case "/api/model/options":
			close(started)
			<-release
			_, _ = w.Write([]byte(`{"provider":"managed","model":"m","providers":[]}`))
		case "/api/ws":
			conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }, Subprotocols: []string{hermesGatewayProtocol}}).Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
				forwarded.Add(1)
			}
		default:
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	s := desktopFixture(t, upstream.URL)
	_, raw, err := s.Activate(context.Background(), 45, 123)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := s.Authenticate(context.Background(), raw, 123)
	if err != nil {
		t.Fatal(err)
	}
	ticket, _, err := s.MintTicket(claims)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(ticket)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.ProxyWebSocket(r.Context(), claims, u.Query().Get("ticket"), w, r); err != nil {
			http.Error(w, "denied", 401)
		}
	}))
	defer proxy.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(proxy.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if conn.WriteMessage(websocket.TextMessage, desktopRPCFrame(1, "session.create", map[string]any{"source": "web", "model": "m", "provider": "managed"})) != nil {
		t.Fatal("send failed")
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("model validation did not reach catalogue")
	}
	if err := s.RevokeUserSessions(context.Background(), 45); err != nil {
		t.Fatal(err)
	}
	close(release)
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("post-logout request returned success")
	}
	if forwarded.Load() != 0 {
		t.Fatal("model validation forwarded a write after logout")
	}
}
