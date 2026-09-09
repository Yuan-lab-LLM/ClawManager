package services

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestHermesDashboardHTTPPolicyChatSurface(t *testing.T) {
	for _, raw := range []string{
		"/", "/chat/", "/chat?resume=session:210", "/sessions/session:210", "/api/auth/me", "/api/auth/mode",
		"/api/profiles", "/api/profiles/active", "/api/dashboard/plugins", "/api/dashboard/themes", "/api/dashboard/font",
		"/assets/index-aB12.js", "/fonts/inter.woff2", "/ds-assets/logo.svg", "/favicon.ico",
		"/api/status", "/api/config", "/api/model/info?profile=", "/api/model/options?include_unconfigured=1",
		"/api/sessions?limit=40&offset=0&order=recent", "/api/sessions?min_messages=1&archived=exclude",
		"/api/sessions?exclude_sources=cron,tool", "/api/sessions/search?q=hello&sources=cli,web",
		"/api/sessions/session:210", "/api/sessions/session:210/messages?limit=500&order=latest",
		"/api/sessions/session:210/latest-descendant", "/api/sessions/stats", "/api/sessions/empty/count",
	} {
		t.Run(raw, func(t *testing.T) {
			u, _ := url.Parse(raw)
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				rule, err := hermesDashboardHTTPPolicy(method, u.Path, u.Query())
				wantPath := u.Path
				if u.Path == "/" || u.Path == "/chat" || u.Path == "/chat/" || u.Path == "/sessions" || strings.HasPrefix(u.Path, "/sessions/") {
					wantPath = "/chat"
				}
				if err != nil || rule.Path != wantPath {
					t.Fatalf("%s denied: %+v %v", method, rule, err)
				}
				if rule.Kind == hermesDashboardLocalJSON && !json.Valid(rule.LocalJSON) {
					t.Fatalf("invalid local JSON: %q", rule.LocalJSON)
				}
				if _, exists := rule.Query["profile"]; exists {
					t.Fatal("empty profile must not be forwarded")
				}
			}
		})
	}
	rule, err := hermesDashboardHTTPPolicy(http.MethodPost, "/api/auth/ws-ticket", nil)
	if err != nil || rule.Kind != hermesDashboardWSTicket {
		t.Fatalf("ticket route must be local: %+v %v", rule, err)
	}
}

func TestHermesDashboardHTTPPolicyRejectsManagementAndTraversal(t *testing.T) {
	for _, raw := range []string{
		"/api/env", "/api/env/reveal", "/api/config/defaults", "/api/files/read?path=.env", "/api/console", "/api/ssh", "/api/desktop",
		"/api/hermes/update/check", "/api/dashboard/plugins/rescan", "/dashboard-plugins/arbitrary/index.js", "/auth/password-login", "/login",
		"/assets/index.js.map", "/assets/../secret.js", "/assets/%2e%2e/secret.js", "/assets//index.js", "/assets/index.js?token=secret",
		"/api/status?profile=default", "/api/status?profile=current", "/api/status?profile=other", "/chat?profile=default",
		"/api/sessions?limit=101", "/api/sessions?offset=10001", "/api/sessions?limit=-1", "/api/sessions?limit=1&limit=2",
		"/api/sessions/id/messages?limit=501", "/api/sessions/id/export", "/api/sessions?order=latest", "/api/sessions?cwd=/tmp",
		"/api/auth/me?ticket=secret", "/api/model/options?refresh=1",
		"/api/sessions?source=../../profile", "/api/sessions?source=cli,web", "/api/sessions/search?q=hello&cwd=/tmp",
	} {
		t.Run(raw, func(t *testing.T) {
			parts := strings.SplitN(raw, "?", 2)
			query := make(url.Values)
			if len(parts) == 2 {
				query, _ = url.ParseQuery(parts[1])
			}
			if _, err := hermesDashboardHTTPPolicy(http.MethodGet, parts[0], query); err == nil {
				t.Fatal("unexpected allow")
			}
		})
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if _, err := hermesDashboardHTTPPolicy(method, "/api/sessions/id", nil); err == nil {
			t.Fatalf("unreviewed write %s allowed", method)
		}
	}
	if _, err := hermesDashboardHTTPPolicy(http.MethodGet, "/api/auth/ws-ticket", nil); err == nil {
		t.Fatal("GET ticket allowed")
	}
}

func TestHermesDashboardWebSocketPolicy(t *testing.T) {
	input := url.Values{"channel": {"chat-fresh-abc-123"}, "resume": {"session:210"}, "fresh": {"1"}, "attach": {"00112233445566778899aabbccddeeff"}, "ticket": {"cm.jwt.ticket"}, "profile": {""}}
	query, err := hermesDashboardWebSocketPolicy("/api/pty", input)
	if err != nil || query.Get("channel") != input.Get("channel") || query.Get("resume") != input.Get("resume") {
		t.Fatalf("classic PTY denied: %v %v", query, err)
	}
	if query.Get("ticket") != "" || input.Get("ticket") == "" {
		t.Fatal("must remove ticket from copy, preserving original for CM verification")
	}
	if _, err := hermesDashboardWebSocketPolicy("/api/events", url.Values{"channel": {"chat-123"}}); err != nil {
		t.Fatalf("protocol-ticket events denied: %v", err)
	}
	if _, err := hermesDashboardWebSocketPolicy("/api/ws", nil); err != nil {
		t.Fatalf("protocol-ticket RPC denied: %v", err)
	}
	for _, raw := range []string{
		"/api/pty", "/api/events", "/api/console?channel=c", "/api/ws?token=x", "/api/ws?internal=x", "/api/ws?ticket=a&ticket=b",
		"/api/pty?channel=c&command=bash", "/api/pty?channel=c&profile=default", "/api/pty?channel=c&profile=current",
		"/api/pty?channel=c&attach=not32hex", "/api/pty?channel=c&fresh=true", "/api/pty?channel=c&resume=../other",
		"/api/events?channel=c&resume=x", "/api/ws?channel=c", "/api/events?channel=a%2Fb",
	} {
		u, _ := url.Parse(raw)
		if _, err := hermesDashboardWebSocketPolicy(u.Path, u.Query()); err == nil {
			t.Errorf("unexpected WS allow: %s", raw)
		}
	}
}

func TestHermesDashboardRPCPolicySidecarOnly(t *testing.T) {
	for _, raw := range []string{
		`{"jsonrpc":"2.0","id":"w1","method":"session.create","params":{"source":"tool","close_on_disconnect":true}}`,
		`{"jsonrpc":"2.0","id":1,"method":"session.create","params":{"source":"tool","close_on_disconnect":true,"profile":""}}`,
		`{"jsonrpc":"2.0","id":"w2","method":"gateway.ping","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
	} {
		body, err := hermesDashboardFilterRPC([]byte(raw))
		if err != nil || !json.Valid(body) {
			t.Fatalf("sidecar denied: %s %v", raw, err)
		}
		var original, filtered struct {
			ID     json.RawMessage            `json:"id"`
			Method string                     `json:"method"`
			Params map[string]json.RawMessage `json:"params"`
		}
		if json.Unmarshal([]byte(raw), &original) != nil || json.Unmarshal(body, &filtered) != nil ||
			string(filtered.ID) != string(original.ID) || filtered.Method != "ping" || len(filtered.Params) != 0 {
			t.Fatalf("sidecar must preserve the ID and issue only a stateless real ping: %s", body)
		}
	}
	for _, raw := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"session.create"}`,
		`{"jsonrpc":"2.0","id":1,"method":"session.create","params":{}}`,
		`{"jsonrpc":"2.0","id":1,"method":"session.create","params":{"source":"tool"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"session.create","params":{"close_on_disconnect":true}}`,
		`{"jsonrpc":"2.0","id":1,"method":"session.create","params":{"source":"web","close_on_disconnect":true}}`,
		`{"jsonrpc":"2.0","id":1,"method":"session.create","params":{"source":"tool","close_on_disconnect":true,"profile":"default"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"session.create","params":{"source":"tool","close_on_disconnect":true,"cwd":"/tmp"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"session.create","params":{"source":"tool","close_on_disconnect":true,"model":"custom"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"session.create","params":{"source":"tool","close_on_disconnect":true,"lazy":true}}`,
		`{"jsonrpc":"2.0","id":1,"method":"session.create","params":{"source":"tool","close_on_disconnect":true,"profile":null}}`,
		`{"jsonrpc":"2.0","id":1,"method":"session.create","params":{"profile":"default"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"session.create","params":{"source":"desktop"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"session.create","params":{"close_on_disconnect":false}}`,
		`{"jsonrpc":"2.0","id":1,"method":"session.create","params":{"cwd":"/tmp"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"prompt.submit","params":{"session_id":"s","text":"x"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"ping","params":{"anything":true}}`,
		`{"jsonrpc":"2.0","id":null,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":1,"method":"ping"}{"extra":true}`,
		`{"jsonrpc":"2.0","id":1,"method":"ping","unknown":true}`,
		`raw PTY bytes`,
	} {
		if _, err := hermesDashboardFilterRPC([]byte(raw)); err == nil {
			t.Errorf("unexpected RPC allow: %s", raw)
		}
	}
}
