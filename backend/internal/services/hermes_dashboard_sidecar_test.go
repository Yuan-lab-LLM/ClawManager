package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Exercise the production Classic socket boundary, not just the packet filter.
// The fake gateway models the pinned Lite rule (ping has no parameters) and
// counts every create so reconnects cannot silently allocate throwaway agents.
func TestHermesDashboardSidecarReconnectsUseRealStatelessPing(t *testing.T) {
	var pingCalls, createCalls, connections atomic.Int32
	closed := make(chan struct{}, 3)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/password-login":
			dashboardTestLogin(t, w, r)
		case "/api/auth/ws-ticket":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ticket":"private-upstream-ticket"}`))
		case "/api/ws":
			if r.URL.RawQuery != "" || r.Header.Get("Origin") != dashboardTestOrigin {
				t.Error("sidecar bypassed the trusted upstream URL/Origin boundary")
			}
			if cookie, err := r.Cookie("hermes_session_at"); err != nil || cookie.Value != "private-upstream-cookie" {
				t.Error("sidecar did not use the server-owned upstream session")
			}
			conn, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }, Subprotocols: []string{hermesGatewayProtocol}}).Upgrade(w, r, nil)
			if err != nil {
				return
			}
			connections.Add(1)
			defer func() {
				_ = conn.Close()
				connections.Add(-1)
				closed <- struct{}{}
			}()
			for {
				_, body, err := conn.ReadMessage()
				if err != nil {
					return
				}
				var packet struct {
					ID     json.RawMessage            `json:"id"`
					Method string                     `json:"method"`
					Params map[string]json.RawMessage `json:"params"`
				}
				if json.Unmarshal(body, &packet) != nil {
					t.Error("upstream received invalid RPC")
					return
				}
				if packet.Method == "session.create" {
					createCalls.Add(1)
				}
				if packet.Method != "ping" || len(packet.Params) != 0 {
					t.Errorf("unexpected stateful or parameterized sidecar RPC: method=%q", packet.Method)
					return
				}
				sequence := pingCalls.Add(1)
				response := map[string]any{"jsonrpc": "2.0", "id": packet.ID}
				if sequence == 3 {
					// An actual upstream failure must stay a failure, not a local
					// synthetic session.create/ping success.
					response["error"] = map[string]any{"code": -32099, "message": "Gateway probe temporarily unavailable"}
				} else {
					response["result"] = map[string]any{"pong": true, "probe_sequence": sequence}
				}
				if conn.WriteJSON(response) != nil {
					return
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	s := dashboardServiceFixture(t, upstream.URL)
	claims, raw := dashboardActivateClaims(t, s)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(HermesDashboardCookieName(123))
		if err != nil {
			http.Error(w, "denied", http.StatusUnauthorized)
			return
		}
		current, err := s.AuthenticateDashboard(r.Context(), cookie.Value, 123)
		if err != nil {
			http.Error(w, "denied", http.StatusUnauthorized)
			return
		}
		if err := s.ProxyDashboardWebSocket(r.Context(), current, r.URL.Query().Get("ticket"), "/api/ws", r.URL.Query(), w, r); err != nil {
			http.Error(w, "denied", http.StatusUnauthorized)
		}
	}))
	defer proxy.Close()

	for attempt := 1; attempt <= 3; attempt++ {
		ticketURL, _, err := s.MintTicket(claims)
		if err != nil {
			t.Fatal(err)
		}
		ticket, _ := url.Parse(ticketURL)
		headers := http.Header{"Cookie": {HermesDashboardCookieName(123) + "=" + raw}, "Origin": {proxy.URL}}
		conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(proxy.URL, "http")+"/?ticket="+url.QueryEscape(ticket.Query().Get("ticket")), headers)
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		if err != nil {
			t.Fatalf("sidecar reconnect %d failed: %v", attempt, err)
		}
		func() {
			defer conn.Close()
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			id := fmt.Sprintf("w%d", attempt)
			request := fmt.Sprintf(`{"jsonrpc":"2.0","id":%q,"method":"session.create","params":{"source":"tool","close_on_disconnect":true}}`, id)
			if err := conn.WriteMessage(websocket.TextMessage, []byte(request)); err != nil {
				t.Fatal(err)
			}
			_, body, err := conn.ReadMessage()
			if err != nil {
				t.Fatalf("sidecar probe did not receive the upstream response: %v", err)
			}
			var packet struct {
				ID     string         `json:"id"`
				Result map[string]any `json:"result"`
				Error  map[string]any `json:"error"`
			}
			if json.Unmarshal(body, &packet) != nil || packet.ID != id || strings.Contains(string(body), "session_id") {
				t.Fatalf("sidecar response changed the caller ID or fabricated a session: %s", body)
			}
			if attempt == 3 {
				if packet.Result != nil || packet.Error["code"] != float64(-32099) || packet.Error["message"] != "Gateway probe temporarily unavailable" {
					t.Fatalf("real upstream failure was hidden: %s", body)
				}
			} else if packet.Error != nil || packet.Result["pong"] != true || packet.Result["probe_sequence"] != float64(attempt) {
				t.Fatalf("real upstream ping result was not preserved: %s", body)
			}
			before := pingCalls.Load()
			if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"jsonrpc":"2.0","id":"custom","method":"session.create","params":{"source":"web"}}`)); err != nil {
				t.Fatal(err)
			}
			_, denied, err := conn.ReadMessage()
			var failure struct {
				ID    string         `json:"id"`
				Error map[string]any `json:"error"`
			}
			if err != nil || json.Unmarshal(denied, &failure) != nil || failure.ID != "custom" || failure.Error["code"] != float64(-32601) || pingCalls.Load() != before {
				t.Fatal("custom Classic session creation was not rejected before upstream")
			}
		}()
		select {
		case <-closed:
		case <-time.After(5 * time.Second):
			t.Fatal("closed Classic sidecar retained an upstream socket")
		}
	}
	if createCalls.Load() != 0 || pingCalls.Load() != 3 || connections.Load() != 0 {
		t.Fatalf("reconnects must not allocate sessions or retain sockets: creates=%d probes=%d connections=%d", createCalls.Load(), pingCalls.Load(), connections.Load())
	}
}

func TestHermesDashboardStatelessAdaptationDoesNotChangeDesktopSessionCreation(t *testing.T) {
	body, err := hermesDesktopFilterRPC([]byte(`{"jsonrpc":"2.0","id":"desktop1","method":"session.create","params":{"source":"web"}}`))
	if err != nil {
		t.Fatal(err)
	}
	var packet struct {
		Method string            `json:"method"`
		Params map[string]string `json:"params"`
	}
	if json.Unmarshal(body, &packet) != nil || packet.Method != "session.create" || packet.Params["source"] != "web" {
		t.Fatalf("Desktop must retain real session creation: %s", body)
	}
}
