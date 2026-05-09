package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"clawreef/internal/services/k8s"

	"github.com/gorilla/websocket"
)

type proxyUpstreamRequest struct {
	requestURI      string
	forwardedPrefix string
	authorization   string
	cookie          string
	openClawToken   string
}

type wsUpstreamObservation struct {
	requestURI      string
	forwardedPrefix string
	authorization   string
	cookie          string
	openClawToken   string
	messageType     int
	payload         []byte
	receivedMessage bool
}

func newProxyServiceForUpstream(t *testing.T, upstream *httptest.Server, wantTargetPort int32) (*InstanceProxyService, *InstanceAccessService) {
	t.Helper()
	t.Setenv("INSTANCE_ACCESS_TOKEN_SECRET", "proxy-service-test-secret")

	accessService := NewInstanceAccessService()
	proxyService := NewInstanceProxyService(accessService)
	proxyService.serviceResolver = func(ctx context.Context, userID, instanceID int, targetPort int32) (*k8s.ServiceInfo, error) {
		t.Helper()
		if targetPort != wantTargetPort {
			t.Fatalf("targetPort = %d, want %d", targetPort, wantTargetPort)
		}
		return serviceInfoForTestServer(t, upstream), nil
	}
	return proxyService, accessService
}

func serviceInfoForTestServer(t *testing.T, upstream *httptest.Server) *k8s.ServiceInfo {
	t.Helper()
	parsed, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", upstream.URL, err)
	}
	host, portString, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("SplitHostPort(%q) error = %v", parsed.Host, err)
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		t.Fatalf("Atoi(%q) error = %v", portString, err)
	}
	return &k8s.ServiceInfo{
		Name:       "test-upstream",
		Namespace:  "test",
		ClusterIP:  host,
		TargetPort: int32(port),
	}
}

func generateScopedProxyToken(t *testing.T, accessService *InstanceAccessService, scope InstanceAccessScope) string {
	t.Helper()
	token, err := accessService.GenerateTokenForScope(7, scope.InstanceID, "openclaw", scope, 5*time.Minute)
	if err != nil {
		t.Fatalf("GenerateTokenForScope() error = %v", err)
	}
	return token.Token
}

func proxyControlUIRequestForTest(t *testing.T, proxyService *InstanceProxyService, scope InstanceAccessScope, token string, w http.ResponseWriter, r *http.Request) error {
	t.Helper()
	return proxyService.ProxyRequestWithScopeAndUpstreamAuth(context.Background(), scope, token, InstanceProxyUpstreamAuth{
		OpenClawGatewayToken: "test-openclaw-upstream-token",
	}, w, r)
}

func proxyControlUIWebSocketForTest(t *testing.T, proxyService *InstanceProxyService, scope InstanceAccessScope, routeToken string, w http.ResponseWriter, r *http.Request) error {
	t.Helper()
	return proxyService.ProxyWebSocketWithScopeAndUpstreamAuth(context.Background(), scope, routeToken, InstanceProxyUpstreamAuth{
		OpenClawGatewayToken: "test-openclaw-upstream-token",
	}, w, r)
}

func wsURLForTestServer(t *testing.T, server *httptest.Server, path string) string {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", server.URL, err)
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	default:
		t.Fatalf("unexpected test server scheme %q", parsed.Scheme)
	}
	pathURL, err := url.Parse(path)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", path, err)
	}
	parsed.Path = pathURL.Path
	parsed.RawQuery = pathURL.RawQuery
	return parsed.String()
}

func newControlUIWebSocketProxyForTest(t *testing.T, upstream *httptest.Server) (*httptest.Server, string) {
	t.Helper()
	scope, err := ResolveInstanceAccessScope(42, "openclaw", AccessModeControlUI, DefaultDesktopTargetPort)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() error = %v", err)
	}
	proxyService, accessService := newProxyServiceForUpstream(t, upstream, scope.TargetPort)
	routeToken := generateScopedProxyToken(t, accessService, scope)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := proxyControlUIWebSocketForTest(t, proxyService, scope, routeToken, w, r); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
	}))
	return proxy, routeToken
}

func decodeJSONMapForTest(t *testing.T, payload []byte) map[string]any {
	t.Helper()
	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", string(payload), err)
	}
	asMap, ok := decoded.(map[string]any)
	if !ok {
		t.Fatalf("decoded JSON is %T, want object", decoded)
	}
	return asMap
}

func requireNoSensitiveSubstring(t *testing.T, value string, sensitive ...string) {
	t.Helper()
	for _, item := range sensitive {
		if item != "" && strings.Contains(value, item) {
			t.Fatalf("string contains sensitive value %q: %q", item, value)
		}
	}
}

func requireWebSocketReadFailureForTest(t *testing.T, conn *websocket.Conn, description string, sensitive ...string) error {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("%s SetReadDeadline() error = %v", description, err)
	}
	_, _, err := conn.ReadMessage()
	_ = conn.SetReadDeadline(time.Time{})
	if err == nil {
		t.Fatalf("%s ReadMessage() succeeded, want closed or failed websocket", description)
	}
	requireNoSensitiveSubstring(t, err.Error(), sensitive...)
	return err
}

func TestProxyRequestWithControlUIScopeStripsPrefixAndPreservesQuery(t *testing.T) {
	tests := []struct {
		name       string
		target     string
		wantURI    string
		wantPrefix string
	}{
		{
			name:       "root",
			target:     "/api/v1/instances/42/control-ui/",
			wantURI:    "/",
			wantPrefix: "/api/v1/instances/42/control-ui",
		},
		{
			name:       "chat with auth query",
			target:     "/api/v1/instances/42/control-ui/chat?token=t&password=pw&session=main",
			wantURI:    "/chat?session=main",
			wantPrefix: "/api/v1/instances/42/control-ui",
		},
		{
			name:       "static asset",
			target:     "/api/v1/instances/42/control-ui/assets/index.css",
			wantURI:    "/assets/index.css",
			wantPrefix: "/api/v1/instances/42/control-ui",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seen := make(chan proxyUpstreamRequest, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen <- proxyUpstreamRequest{
					requestURI:      r.URL.RequestURI(),
					forwardedPrefix: r.Header.Get("X-Forwarded-Prefix"),
				}
				_, _ = io.WriteString(w, "ok")
			}))
			defer upstream.Close()

			scope, err := ResolveInstanceAccessScope(42, "openclaw", AccessModeControlUI, DefaultDesktopTargetPort)
			if err != nil {
				t.Fatalf("ResolveInstanceAccessScope() error = %v", err)
			}
			proxyService, accessService := newProxyServiceForUpstream(t, upstream, scope.TargetPort)
			token := generateScopedProxyToken(t, accessService, scope)

			req := httptest.NewRequest(http.MethodGet, tt.target, nil)
			w := httptest.NewRecorder()

			if err := proxyControlUIRequestForTest(t, proxyService, scope, token, w, req); err != nil {
				t.Fatalf("ProxyRequestWithScope() error = %v", err)
			}
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
			}

			got := <-seen
			if got.requestURI != tt.wantURI {
				t.Fatalf("upstream RequestURI = %q, want %q", got.requestURI, tt.wantURI)
			}
			if got.forwardedPrefix != tt.wantPrefix {
				t.Fatalf("X-Forwarded-Prefix = %q, want %q", got.forwardedPrefix, tt.wantPrefix)
			}
		})
	}
}

func TestProxyRequestWithDesktopScopeKeepsWebtopProxyPrefix(t *testing.T) {
	seen := make(chan proxyUpstreamRequest, 1)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- proxyUpstreamRequest{
			requestURI:      r.URL.RequestURI(),
			forwardedPrefix: r.Header.Get("X-Forwarded-Prefix"),
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	scope, err := ResolveInstanceAccessScope(42, "openclaw", AccessModeDesktop, DefaultDesktopTargetPort)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() error = %v", err)
	}
	proxyService, accessService := newProxyServiceForUpstream(t, upstream, scope.TargetPort)
	token := generateScopedProxyToken(t, accessService, scope)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/42/proxy/assets/index.css?token=t&password=pw&view=desktop", nil)
	w := httptest.NewRecorder()

	if err := proxyService.ProxyRequestWithScope(context.Background(), scope, token, w, req); err != nil {
		t.Fatalf("ProxyRequestWithScope() error = %v", err)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	got := <-seen
	if got.requestURI != "/api/v1/instances/42/proxy/assets/index.css?password=pw&view=desktop" {
		t.Fatalf("desktop upstream RequestURI = %q", got.requestURI)
	}
	if got.forwardedPrefix != "/api/v1/instances/42/proxy" {
		t.Fatalf("desktop X-Forwarded-Prefix = %q", got.forwardedPrefix)
	}
}

func TestProxyRequestWithControlUIScopeInjectsServerSideGatewayToken(t *testing.T) {
	seen := make(chan proxyUpstreamRequest, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- proxyUpstreamRequest{
			requestURI:      r.URL.RequestURI(),
			forwardedPrefix: r.Header.Get("X-Forwarded-Prefix"),
			authorization:   r.Header.Get("Authorization"),
			cookie:          r.Header.Get("Cookie"),
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	scope, err := ResolveInstanceAccessScope(42, "openclaw", AccessModeControlUI, DefaultDesktopTargetPort)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() error = %v", err)
	}
	proxyService, accessService := newProxyServiceForUpstream(t, upstream, scope.TargetPort)
	token := generateScopedProxyToken(t, accessService, scope)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/42/control-ui/chat?token=route-token", nil)
	req.Header.Set("Authorization", "Bearer browser-route-token")
	req.Header.Set("Cookie", scope.CookieName+"=browser-route-cookie")
	w := httptest.NewRecorder()

	if err := proxyService.ProxyRequestWithScopeAndUpstreamAuth(context.Background(), scope, token, InstanceProxyUpstreamAuth{
		OpenClawGatewayToken: "server-side-openclaw-token",
	}, w, req); err != nil {
		t.Fatalf("ProxyRequestWithScopeAndUpstreamAuth() error = %v", err)
	}

	got := <-seen
	if got.requestURI != "/chat" {
		t.Fatalf("upstream RequestURI = %q, want /chat", got.requestURI)
	}
	if got.authorization != "Bearer server-side-openclaw-token" {
		t.Fatalf("upstream Authorization = %q, want server-side OpenClaw bearer token", got.authorization)
	}
	if got.cookie != "" {
		t.Fatalf("upstream Cookie = %q, want browser cookie stripped", got.cookie)
	}
}

func TestProxyRequestWithDesktopScopeDoesNotInjectOpenClawGatewayToken(t *testing.T) {
	seen := make(chan proxyUpstreamRequest, 1)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- proxyUpstreamRequest{
			authorization: r.Header.Get("Authorization"),
			cookie:        r.Header.Get("Cookie"),
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	scope, err := ResolveInstanceAccessScope(42, "openclaw", AccessModeDesktop, DefaultDesktopTargetPort)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() error = %v", err)
	}
	proxyService, accessService := newProxyServiceForUpstream(t, upstream, scope.TargetPort)
	token := generateScopedProxyToken(t, accessService, scope)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/42/proxy/", nil)
	req.Header.Set("Authorization", "Bearer browser-desktop-token")
	req.Header.Set("Cookie", scope.CookieName+"=browser-desktop-cookie")
	w := httptest.NewRecorder()

	if err := proxyService.ProxyRequestWithScopeAndUpstreamAuth(context.Background(), scope, token, InstanceProxyUpstreamAuth{
		OpenClawGatewayToken: "server-side-openclaw-token",
	}, w, req); err != nil {
		t.Fatalf("ProxyRequestWithScopeAndUpstreamAuth() error = %v", err)
	}

	got := <-seen
	if got.authorization == "Bearer server-side-openclaw-token" {
		t.Fatalf("desktop upstream unexpectedly received OpenClaw gateway token")
	}
	if got.authorization != "Bearer browser-desktop-token" {
		t.Fatalf("desktop Authorization = %q, want browser desktop header preserved", got.authorization)
	}
	if got.cookie == "" {
		t.Fatalf("desktop Cookie was unexpectedly stripped")
	}
}

func TestControlUIUpstreamAuthHelperAppliesSameRuleForWebSocketHeaders(t *testing.T) {
	proxyService := NewInstanceProxyService(NewInstanceAccessService())
	scope, err := ResolveInstanceAccessScope(42, "openclaw", AccessModeControlUI, DefaultDesktopTargetPort)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() error = %v", err)
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer browser-route-token")
	header.Set("Cookie", scope.CookieName+"=browser-route-cookie")
	header.Set("X-OpenClaw-Token", "browser-supplied-token")

	err = proxyService.applyControlUIUpstreamAuth(header, scope, InstanceProxyUpstreamAuth{
		OpenClawGatewayToken: "server-side-openclaw-token",
	})
	if err != nil {
		t.Fatalf("applyControlUIUpstreamAuth() error = %v", err)
	}
	if got := header.Get("Authorization"); got != "Bearer server-side-openclaw-token" {
		t.Fatalf("Authorization = %q, want server-side bearer token", got)
	}
	if got := header.Get("Cookie"); got != "" {
		t.Fatalf("Cookie = %q, want stripped", got)
	}
	if got := header.Get("X-OpenClaw-Token"); got != "" {
		t.Fatalf("X-OpenClaw-Token = %q, want stripped browser-supplied token", got)
	}
}

func TestProxyWebSocketWithControlUIScopeInjectsConnectAuthAndDropsStaleDevice(t *testing.T) {
	seen := make(chan wsUpstreamObservation, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		messageType, payload, readErr := conn.ReadMessage()
		obs := wsUpstreamObservation{
			requestURI:      r.URL.RequestURI(),
			forwardedPrefix: r.Header.Get("X-Forwarded-Prefix"),
			authorization:   r.Header.Get("Authorization"),
			cookie:          r.Header.Get("Cookie"),
			openClawToken:   r.Header.Get("X-OpenClaw-Token"),
		}
		if readErr == nil {
			obs.messageType = messageType
			obs.payload = append([]byte(nil), payload...)
			obs.receivedMessage = true
		}
		seen <- obs
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"res","id":"req-1","result":{"ok":true}}`))
	}))
	defer upstream.Close()

	proxy, _ := newControlUIWebSocketProxyForTest(t, upstream)
	defer proxy.Close()

	header := http.Header{}
	header.Set("Authorization", "Bearer fake-browser-route-token")
	header.Set("Cookie", "cm_ui=fb")
	header.Set("X-OpenClaw-Token", "fake-browser-openclaw-token")
	client, _, err := websocket.DefaultDialer.Dial(wsURLForTestServer(t, proxy, "/api/v1/instances/42/control-ui/ws?token=fq&password=pw&session=main"), header)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer client.Close()

	firstConnect := map[string]any{
		"type":   "req",
		"id":     "req-1",
		"method": "connect",
		"params": map[string]any{
			"minProtocol": 3,
			"maxProtocol": 3,
			"client": map[string]any{
				"id":   "client-1",
				"mode": "webchat",
			},
			"role":      "operator",
			"scopes":    []any{"operator.read", "operator.write"},
			"device":    map[string]any{"id": "device-1", "nonce": "nonce-1", "signature": "stale-browser-device-signature"},
			"caps":      []any{"tool-events"},
			"userAgent": "test-agent",
			"locale":    "zh-CN",
			"future":    map[string]any{"keep": true},
			"auth": map[string]any{
				"token":       "fake-browser-auth-token",
				"password":    "fake-browser-password",
				"deviceToken": "fake-browser-device-token",
			},
		},
	}
	payload, err := json.Marshal(firstConnect)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := client.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}

	got := <-seen
	if got.requestURI != "/ws?session=main" {
		t.Fatalf("upstream RequestURI = %q, want token stripped and session preserved", got.requestURI)
	}
	if got.forwardedPrefix != "/api/v1/instances/42/control-ui" {
		t.Fatalf("upstream X-Forwarded-Prefix = %q, want control-ui route prefix", got.forwardedPrefix)
	}
	if got.authorization != "Bearer test-openclaw-upstream-token" {
		t.Fatalf("upstream Authorization = %q, want fake server-side bearer token", got.authorization)
	}
	if got.cookie != "" {
		t.Fatalf("upstream Cookie = %q, want stripped", got.cookie)
	}
	if got.openClawToken != "" {
		t.Fatalf("upstream X-OpenClaw-Token = %q, want stripped", got.openClawToken)
	}
	if !got.receivedMessage || got.messageType != websocket.TextMessage {
		t.Fatalf("upstream first message type = %d received=%v, want text message", got.messageType, got.receivedMessage)
	}

	rewritten := decodeJSONMapForTest(t, got.payload)
	if rewritten["type"] != "req" || rewritten["id"] != "req-1" || rewritten["method"] != "connect" {
		t.Fatalf("rewritten envelope = %#v", rewritten)
	}
	params, ok := rewritten["params"].(map[string]any)
	if !ok {
		t.Fatalf("rewritten params = %T, want object", rewritten["params"])
	}
	for _, key := range []string{"minProtocol", "maxProtocol", "client", "role", "scopes", "caps", "userAgent", "locale", "future"} {
		if _, ok := params[key]; !ok {
			t.Fatalf("rewritten params missing non-auth key %q: %#v", key, params)
		}
	}
	if _, ok := params["device"]; ok {
		t.Fatalf("rewritten params retained stale browser device identity: %#v", params["device"])
	}
	auth, ok := params["auth"].(map[string]any)
	if !ok {
		t.Fatalf("rewritten auth = %T, want object", params["auth"])
	}
	if gotToken := auth["token"]; gotToken != "test-openclaw-upstream-token" {
		t.Fatalf("rewritten auth token = %q, want fake server-side token", gotToken)
	}
	for _, rejectedKey := range []string{"password", "deviceToken"} {
		if _, ok := auth[rejectedKey]; ok {
			t.Fatalf("rewritten auth retained browser field %q: %#v", rejectedKey, auth)
		}
	}
	if strings.Contains(string(got.payload), "fake-browser-auth-token") ||
		strings.Contains(string(got.payload), "fake-browser-password") ||
		strings.Contains(string(got.payload), "fake-browser-device-token") ||
		strings.Contains(string(got.payload), "stale-browser-device-signature") ||
		strings.Contains(string(got.payload), "device-1") {
		t.Fatalf("rewritten first connect retained browser auth or stale device material: %s", string(got.payload))
	}
}

func TestProxyWebSocketWithControlUIScopeForwardsChallengeBeforeRewrittenConnect(t *testing.T) {
	seen := make(chan wsUpstreamObservation, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	const challengePayload = `{"type":"event","event":"connect.challenge","payload":{"nonce":"nonce-1"}}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		if err := conn.WriteMessage(websocket.TextMessage, []byte(challengePayload)); err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		messageType, payload, readErr := conn.ReadMessage()
		obs := wsUpstreamObservation{
			requestURI:    r.URL.RequestURI(),
			authorization: r.Header.Get("Authorization"),
			cookie:        r.Header.Get("Cookie"),
			openClawToken: r.Header.Get("X-OpenClaw-Token"),
		}
		if readErr == nil {
			obs.messageType = messageType
			obs.payload = append([]byte(nil), payload...)
			obs.receivedMessage = true
		}
		seen <- obs
	}))
	defer upstream.Close()

	proxy, _ := newControlUIWebSocketProxyForTest(t, upstream)
	defer proxy.Close()

	header := http.Header{}
	header.Set("Authorization", "Bearer fake-browser-route-token")
	header.Set("Cookie", "cm_ui=fb")
	header.Set("X-OpenClaw-Token", "fake-browser-openclaw-token")
	client, _, err := websocket.DefaultDialer.Dial(wsURLForTestServer(t, proxy, "/api/v1/instances/42/control-ui/ws?token=fq&password=pw&session=main"), header)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer client.Close()

	_ = client.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	messageType, challenge, err := client.ReadMessage()
	_ = client.SetReadDeadline(time.Time{})
	if err != nil {
		t.Fatalf("client did not receive upstream connect.challenge before first connect: %v", err)
	}
	if messageType != websocket.TextMessage || string(challenge) != challengePayload {
		t.Fatalf("client challenge frame = type %d payload %q", messageType, string(challenge))
	}

	firstConnect := map[string]any{
		"type":   "req",
		"id":     "req-1",
		"method": "connect",
		"params": map[string]any{
			"minProtocol": 3,
			"maxProtocol": 3,
			"client": map[string]any{
				"id":   "client-1",
				"mode": "webchat",
			},
			"role":      "operator",
			"scopes":    []any{"operator.read", "operator.write"},
			"device":    map[string]any{"id": "device-1", "nonce": "nonce-1", "signature": "stale-browser-device-signature"},
			"caps":      []any{"tool-events"},
			"userAgent": "test-agent",
			"locale":    "zh-CN",
			"future":    map[string]any{"keep": true},
			"auth": map[string]any{
				"token":       "fake-browser-auth-token",
				"password":    "fake-browser-password",
				"deviceToken": "fake-browser-device-token",
			},
		},
	}
	payload, err := json.Marshal(firstConnect)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := client.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}

	got := <-seen
	if got.requestURI != "/ws?session=main" {
		t.Fatalf("upstream RequestURI = %q, want token stripped and session preserved", got.requestURI)
	}
	if got.authorization != "Bearer test-openclaw-upstream-token" {
		t.Fatalf("upstream Authorization = %q, want fake server-side bearer token", got.authorization)
	}
	if got.cookie != "" {
		t.Fatalf("upstream Cookie = %q, want stripped", got.cookie)
	}
	if got.openClawToken != "" {
		t.Fatalf("upstream X-OpenClaw-Token = %q, want stripped", got.openClawToken)
	}
	if !got.receivedMessage || got.messageType != websocket.TextMessage {
		t.Fatalf("upstream first message type = %d received=%v, want text message", got.messageType, got.receivedMessage)
	}

	rewritten := decodeJSONMapForTest(t, got.payload)
	params, ok := rewritten["params"].(map[string]any)
	if !ok {
		t.Fatalf("rewritten params = %T, want object", rewritten["params"])
	}
	for _, key := range []string{"minProtocol", "maxProtocol", "client", "role", "scopes", "caps", "userAgent", "locale", "future"} {
		if _, ok := params[key]; !ok {
			t.Fatalf("rewritten params missing non-auth key %q: %#v", key, params)
		}
	}
	if _, ok := params["device"]; ok {
		t.Fatalf("rewritten params retained stale browser device identity: %#v", params["device"])
	}
	auth, ok := params["auth"].(map[string]any)
	if !ok {
		t.Fatalf("rewritten auth = %T, want object", params["auth"])
	}
	if gotToken := auth["token"]; gotToken != "test-openclaw-upstream-token" {
		t.Fatalf("rewritten auth token = %q, want fake server-side token", gotToken)
	}
	for _, rejectedKey := range []string{"password", "deviceToken"} {
		if _, ok := auth[rejectedKey]; ok {
			t.Fatalf("rewritten auth retained browser field %q: %#v", rejectedKey, auth)
		}
	}
	if strings.Contains(string(got.payload), "fake-browser-auth-token") ||
		strings.Contains(string(got.payload), "fake-browser-password") ||
		strings.Contains(string(got.payload), "fake-browser-device-token") ||
		strings.Contains(string(got.payload), "stale-browser-device-signature") ||
		strings.Contains(string(got.payload), "device-1") {
		t.Fatalf("rewritten first connect retained browser auth or stale device material: %s", string(got.payload))
	}
}

func TestProxyWebSocketWithControlUIScopeDiagnosticsAreSanitized(t *testing.T) {
	t.Setenv("CONTROLUI_PROXY_AUTH_DIAGNOSTICS", "1")
	var logBuffer bytes.Buffer
	previousOutput := log.Writer()
	previousFlags := log.Flags()
	log.SetOutput(&logBuffer)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(previousOutput)
		log.SetFlags(previousFlags)
	}()

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"res","id":"req-1","error":{"code":"CONTROL_UI_DEVICE_IDENTITY_REQUIRED","message":"redacted by diagnostic observer"}}`))
	}))
	defer upstream.Close()

	proxy, routeToken := newControlUIWebSocketProxyForTest(t, upstream)
	defer proxy.Close()

	header := http.Header{}
	header.Set("Authorization", "Bearer fake-browser-route-token")
	header.Set("Cookie", "cm_ui=fake-browser-cookie")
	header.Set("X-OpenClaw-Token", "fake-browser-openclaw-token")
	client, _, err := websocket.DefaultDialer.Dial(wsURLForTestServer(t, proxy, "/api/v1/instances/42/control-ui/ws?token=fake-query-token&password=fake-query-password&session=main"), header)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer client.Close()

	firstConnect := map[string]any{
		"type":   "req",
		"id":     "req-1",
		"method": "connect",
		"params": map[string]any{
			"minProtocol": 3,
			"maxProtocol": 3,
			"client":      map[string]any{"id": "client-1", "mode": "webchat"},
			"role":        "operator",
			"scopes":      []any{"operator.read", "operator.write"},
			"caps":        []any{"tool-events"},
			"userAgent":   "test-agent",
			"locale":      "zh-CN",
			"device":      map[string]any{"id": "device-1", "nonce": "nonce-1", "signature": "fake-browser-device-signature"},
			"auth": map[string]any{
				"token":       "fake-browser-auth-token",
				"password":    "fake-browser-password",
				"deviceToken": "fake-browser-device-token",
			},
		},
	}
	payload, err := json.Marshal(firstConnect)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if err := client.WriteMessage(websocket.TextMessage, payload); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}
	if _, _, err := client.ReadMessage(); err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}

	logs := logBuffer.String()
	for _, want := range []string{
		"control-ui-websocket-diagnostic event=ws_upstream_shape",
		"forwarded_prefix_shape=backend_control_ui_prefix_match",
		"upstream_query_shape=known:session,unknown:0",
		"source_query_had_token=true",
		"source_query_had_password=true",
		"browser_auth_header_seen=true",
		"upstream_auth_header_present=true",
		"upstream_cookie_present=false",
		"event=ws_first_connect",
		"method=connect",
		"browser_auth_present=true",
		"browser_device_present=true",
		"rewritten_auth_token_present=true",
		"rewritten_device_present=false",
		"bridge_result=rewritten_forwarded",
		"event=ws_first_upstream_frame",
		"first_upstream_error_code=CONTROL_UI_DEVICE_IDENTITY_REQUIRED",
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("diagnostic logs missing %q; logs=%s", want, logs)
		}
	}
	requireNoSensitiveSubstring(t, logs,
		routeToken,
		"test-openclaw-upstream-token",
		"fake-browser-route-token",
		"fake-browser-cookie",
		"fake-browser-openclaw-token",
		"fake-query-token",
		"fake-query-password",
		"fake-browser-auth-token",
		"fake-browser-password",
		"fake-browser-device-token",
		"fake-browser-device-signature",
		"device-1",
		"nonce-1",
	)
}

func TestProxyWebSocketWithDesktopScopePassesThroughFirstFrameAndHeaders(t *testing.T) {
	seen := make(chan wsUpstreamObservation, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		messageType, payload, readErr := conn.ReadMessage()
		obs := wsUpstreamObservation{
			requestURI:    r.URL.RequestURI(),
			authorization: r.Header.Get("Authorization"),
			cookie:        r.Header.Get("Cookie"),
			openClawToken: r.Header.Get("X-OpenClaw-Token"),
		}
		if readErr == nil {
			obs.messageType = messageType
			obs.payload = append([]byte(nil), payload...)
			obs.receivedMessage = true
		}
		seen <- obs
	}))
	defer upstream.Close()

	scope, err := ResolveInstanceAccessScope(42, "openclaw", AccessModeDesktop, DefaultDesktopTargetPort)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() error = %v", err)
	}
	proxyService, accessService := newProxyServiceForUpstream(t, upstream, scope.TargetPort)
	routeToken := generateScopedProxyToken(t, accessService, scope)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := proxyService.ProxyWebSocketWithScopeAndUpstreamAuth(context.Background(), scope, routeToken, InstanceProxyUpstreamAuth{
			OpenClawGatewayToken: "test-openclaw-upstream-token",
		}, w, r); err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
	}))
	defer proxy.Close()

	header := http.Header{}
	header.Set("Authorization", "Bearer fake-browser-desktop-token")
	header.Set("Cookie", "cm_d=fd")
	client, _, err := websocket.DefaultDialer.Dial(wsURLForTestServer(t, proxy, "/api/v1/instances/42/proxy/websocket?token=fr&password=pw&channel=desktop"), header)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer client.Close()

	const firstPayload = `{"method":"not-connect","params":{"auth":{"token":"fake-browser-desktop-auth"}}}`
	if err := client.WriteMessage(websocket.TextMessage, []byte(firstPayload)); err != nil {
		t.Fatalf("WriteMessage() error = %v", err)
	}

	got := <-seen
	if got.requestURI != "/api/v1/instances/42/proxy/websocket?channel=desktop&password=pw" {
		t.Fatalf("desktop upstream RequestURI = %q", got.requestURI)
	}
	if got.authorization != "Bearer fake-browser-desktop-token" {
		t.Fatalf("desktop Authorization = %q, want browser header preserved", got.authorization)
	}
	if got.cookie == "" {
		t.Fatalf("desktop Cookie was unexpectedly stripped")
	}
	if !got.receivedMessage || got.messageType != websocket.TextMessage || string(got.payload) != firstPayload {
		t.Fatalf("desktop first frame = type %d payload %q received=%v", got.messageType, string(got.payload), got.receivedMessage)
	}
}

func TestProxyWebSocketWithControlUIScopeRejectsMalformedFirstFrameWithoutForwarding(t *testing.T) {
	tests := []struct {
		name        string
		messageType int
		payload     []byte
	}{
		{name: "non-text", messageType: websocket.BinaryMessage, payload: []byte("binary frame")},
		{name: "invalid JSON", messageType: websocket.TextMessage, payload: []byte("{not-json")},
		{name: "JSON not object", messageType: websocket.TextMessage, payload: []byte(`[{"method":"connect"}]`)},
		{name: "wrong method", messageType: websocket.TextMessage, payload: []byte(`{"type":"req","id":"req-1","method":"ping","params":{}}`)},
		{name: "missing params", messageType: websocket.TextMessage, payload: []byte(`{"type":"req","id":"req-1","method":"connect"}`)},
		{name: "params not object", messageType: websocket.TextMessage, payload: []byte(`{"type":"req","id":"req-1","method":"connect","params":[]}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			forwarded := make(chan wsUpstreamObservation, 1)
			upstreamReadErr := make(chan error, 1)
			done := make(chan struct{})
			upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					close(done)
					return
				}
				defer conn.Close()
				_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
				messageType, payload, readErr := conn.ReadMessage()
				if readErr == nil {
					forwarded <- wsUpstreamObservation{
						messageType:     messageType,
						payload:         append([]byte(nil), payload...),
						receivedMessage: true,
					}
				} else {
					upstreamReadErr <- readErr
				}
				close(done)
			}))
			defer upstream.Close()

			proxy, routeToken := newControlUIWebSocketProxyForTest(t, upstream)
			defer proxy.Close()

			client, _, err := websocket.DefaultDialer.Dial(wsURLForTestServer(t, proxy, "/api/v1/instances/42/control-ui/ws"), nil)
			if err != nil {
				t.Fatalf("Dial() error = %v", err)
			}
			defer client.Close()

			if err := client.WriteMessage(tt.messageType, tt.payload); err != nil {
				t.Fatalf("WriteMessage() error = %v", err)
			}
			clientErr := requireWebSocketReadFailureForTest(t, client, "browser after malformed first connect", routeToken, "test-openclaw-upstream-token")
			requireNoSensitiveSubstring(t, clientErr.Error(), routeToken, "test-openclaw-upstream-token")

			select {
			case got := <-forwarded:
				t.Fatalf("malformed first frame was forwarded upstream: type=%d payload=%q", got.messageType, string(got.payload))
			case <-done:
				select {
				case got := <-forwarded:
					t.Fatalf("malformed first frame was forwarded upstream: type=%d payload=%q", got.messageType, string(got.payload))
				default:
				}
			case <-time.After(time.Second):
				t.Fatalf("upstream did not finish waiting for malformed first frame")
			}
			select {
			case err := <-upstreamReadErr:
				requireNoSensitiveSubstring(t, err.Error(), routeToken, "test-openclaw-upstream-token")
			default:
				t.Fatalf("upstream websocket did not close after malformed first frame")
			}
		})
	}
}

func TestProxyWebSocketWithControlUIScopeUpstreamCloseBeforeFirstConnectFailsClosed(t *testing.T) {
	upstreamClosed := make(chan struct{}, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		deadline := time.Now().Add(time.Second)
		closeMessage := websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "upstream unavailable before connect")
		_ = conn.WriteControl(websocket.CloseMessage, closeMessage, deadline)
		_ = conn.Close()
		upstreamClosed <- struct{}{}
	}))
	defer upstream.Close()

	proxy, routeToken := newControlUIWebSocketProxyForTest(t, upstream)
	defer proxy.Close()

	header := http.Header{}
	header.Set("Authorization", "Bearer fake-browser-route-token")
	header.Set("Cookie", "cm_ui=fb")
	header.Set("X-OpenClaw-Token", "fake-browser-openclaw-token")
	client, _, err := websocket.DefaultDialer.Dial(wsURLForTestServer(t, proxy, "/api/v1/instances/42/control-ui/ws?token=fq&password=pw"), header)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer client.Close()

	select {
	case <-upstreamClosed:
	case <-time.After(time.Second):
		t.Fatalf("upstream did not close before first browser connect")
	}
	readErr := requireWebSocketReadFailureForTest(t, client, "browser after upstream pre-connect close", routeToken, "test-openclaw-upstream-token", "fake-browser-route-token", "fake-browser-openclaw-token")
	requireNoSensitiveSubstring(t, readErr.Error(), routeToken, "test-openclaw-upstream-token", "fake-browser-route-token", "fake-browser-openclaw-token")
}

func TestProxyWebSocketWithControlUIScopeBrowserDisconnectBeforeFirstConnectDoesNotHang(t *testing.T) {
	upstreamReadErr := make(chan error, 1)
	forwarded := make(chan wsUpstreamObservation, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		messageType, payload, readErr := conn.ReadMessage()
		if readErr == nil {
			forwarded <- wsUpstreamObservation{
				messageType:     messageType,
				payload:         append([]byte(nil), payload...),
				receivedMessage: true,
			}
			return
		}
		upstreamReadErr <- readErr
	}))
	defer upstream.Close()

	proxy, routeToken := newControlUIWebSocketProxyForTest(t, upstream)
	defer proxy.Close()

	client, _, err := websocket.DefaultDialer.Dial(wsURLForTestServer(t, proxy, "/api/v1/instances/42/control-ui/ws"), nil)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	closeMessage := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "browser closed before connect")
	if err := client.WriteControl(websocket.CloseMessage, closeMessage, time.Now().Add(time.Second)); err != nil {
		t.Fatalf("client close before first connect error = %v", err)
	}
	_ = client.Close()

	select {
	case got := <-forwarded:
		t.Fatalf("browser disconnect before first connect forwarded upstream message: type=%d payload=%q", got.messageType, string(got.payload))
	case err := <-upstreamReadErr:
		requireNoSensitiveSubstring(t, err.Error(), routeToken, "test-openclaw-upstream-token")
	case <-time.After(time.Second):
		t.Fatalf("upstream did not observe browser disconnect before first connect")
	}
}

func TestProxyWebSocketWithControlUIScopeContextCancelBeforeFirstConnectDoesNotHang(t *testing.T) {
	upstreamAccepted := make(chan struct{}, 1)
	upstreamReadErr := make(chan error, 1)
	handlerReturned := make(chan error, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		upstreamAccepted <- struct{}{}
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _, readErr := conn.ReadMessage()
		upstreamReadErr <- readErr
	}))
	defer upstream.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	scope, err := ResolveInstanceAccessScope(42, "openclaw", AccessModeControlUI, DefaultDesktopTargetPort)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() error = %v", err)
	}
	proxyService, accessService := newProxyServiceForUpstream(t, upstream, scope.TargetPort)
	routeToken := generateScopedProxyToken(t, accessService, scope)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerReturned <- proxyService.ProxyWebSocketWithScopeAndUpstreamAuth(ctx, scope, routeToken, InstanceProxyUpstreamAuth{
			OpenClawGatewayToken: "test-openclaw-upstream-token",
		}, w, r)
	}))
	defer proxy.Close()

	client, _, err := websocket.DefaultDialer.Dial(wsURLForTestServer(t, proxy, "/api/v1/instances/42/control-ui/ws"), nil)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer client.Close()

	select {
	case <-upstreamAccepted:
	case <-time.After(time.Second):
		t.Fatalf("upstream did not accept websocket before context cancel")
	}
	cancel()

	readErr := requireWebSocketReadFailureForTest(t, client, "browser after context cancel before first connect", routeToken, "test-openclaw-upstream-token")
	requireNoSensitiveSubstring(t, readErr.Error(), routeToken, "test-openclaw-upstream-token")

	select {
	case err := <-handlerReturned:
		if err != nil {
			t.Fatalf("ProxyWebSocketWithScopeAndUpstreamAuth() error after context cancel = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("proxy handler did not return after context cancel before first connect")
	}
	select {
	case err := <-upstreamReadErr:
		if err == nil {
			t.Fatalf("upstream read succeeded after context cancel before first connect")
		}
		requireNoSensitiveSubstring(t, err.Error(), routeToken, "test-openclaw-upstream-token")
	case <-time.After(time.Second):
		t.Fatalf("upstream did not close after context cancel before first connect")
	}
}

func TestProxyWebSocketWithControlUIScopeUpstreamDialFailureIsSanitized(t *testing.T) {
	t.Setenv("INSTANCE_ACCESS_TOKEN_SECRET", "proxy-service-test-secret")
	accessService := NewInstanceAccessService()
	scope, err := ResolveInstanceAccessScope(42, "openclaw", AccessModeControlUI, DefaultDesktopTargetPort)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() error = %v", err)
	}
	routeToken := generateScopedProxyToken(t, accessService, scope)
	proxyService := NewInstanceProxyService(accessService)
	proxyService.serviceResolver = func(ctx context.Context, userID, instanceID int, targetPort int32) (*k8s.ServiceInfo, error) {
		return &k8s.ServiceInfo{
			Name:        "missing-upstream",
			Namespace:   "test",
			ClusterIP:   "127.0.0.1",
			ServicePort: 1,
			TargetPort:  scope.TargetPort,
		}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/42/control-ui/ws", nil)
	w := httptest.NewRecorder()
	err = proxyService.ProxyWebSocketWithScopeAndUpstreamAuth(context.Background(), scope, routeToken, InstanceProxyUpstreamAuth{
		OpenClawGatewayToken: "test-openclaw-upstream-token",
	}, w, req)
	if err == nil {
		t.Fatalf("ProxyWebSocketWithScopeAndUpstreamAuth() error = nil, want upstream dial failure")
	}
	if got := err.Error(); got != "failed to connect upstream websocket" {
		t.Fatalf("ProxyWebSocketWithScopeAndUpstreamAuth() error = %q, want sanitized upstream dial failure", got)
	}
	requireNoSensitiveSubstring(t, err.Error(), "test-openclaw-upstream-token", routeToken)
}

func TestProxyRequestWithControlUIScopeRewritesRedirectLocation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/chat?session=main")
		w.WriteHeader(http.StatusFound)
	}))
	defer upstream.Close()

	scope, err := ResolveInstanceAccessScope(42, "openclaw", AccessModeControlUI, DefaultDesktopTargetPort)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() error = %v", err)
	}
	proxyService, accessService := newProxyServiceForUpstream(t, upstream, scope.TargetPort)
	token := generateScopedProxyToken(t, accessService, scope)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/42/control-ui/login", nil)
	w := httptest.NewRecorder()

	if err := proxyControlUIRequestForTest(t, proxyService, scope, token, w, req); err != nil {
		t.Fatalf("ProxyRequestWithScope() error = %v", err)
	}
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusFound)
	}
	if got := w.Header().Get("Location"); got != "/api/v1/instances/42/control-ui/chat?session=main" {
		t.Fatalf("Location = %q", got)
	}
}

func TestProxyRequestWithControlUIScopeInjectsControlUIBase(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<html><head><title>Control</title></head><body></body></html>")
	}))
	defer upstream.Close()

	scope, err := ResolveInstanceAccessScope(42, "openclaw", AccessModeControlUI, DefaultDesktopTargetPort)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() error = %v", err)
	}
	proxyService, accessService := newProxyServiceForUpstream(t, upstream, scope.TargetPort)
	token := generateScopedProxyToken(t, accessService, scope)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/42/control-ui/", nil)
	w := httptest.NewRecorder()

	if err := proxyControlUIRequestForTest(t, proxyService, scope, token, w, req); err != nil {
		t.Fatalf("ProxyRequestWithScope() error = %v", err)
	}
	body := w.Body.String()
	if !strings.Contains(body, `<base href="/api/v1/instances/42/control-ui/">`) {
		t.Fatalf("response body missing control-ui base tag: %s", body)
	}
	if strings.Contains(body, `/api/v1/instances/42/proxy/`) {
		t.Fatalf("response body unexpectedly used desktop proxy base: %s", body)
	}
}

func TestProxyRequestWithControlUIScopeRewritesIconHrefsForHistoryFallback(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<html><head>
<link rel="icon" href="favicon.svg">
<link href="./favicon.svg" rel="shortcut icon">
<link rel="apple-touch-icon" href="/favicon.svg">
<link rel="stylesheet" href="./assets/index.css">
<script type="module" src="./assets/index.js"></script>
</head><body></body></html>`)
	}))
	defer upstream.Close()

	scope, err := ResolveInstanceAccessScope(42, "openclaw", AccessModeControlUI, DefaultDesktopTargetPort)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() error = %v", err)
	}
	proxyService, accessService := newProxyServiceForUpstream(t, upstream, scope.TargetPort)
	token := generateScopedProxyToken(t, accessService, scope)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/42/control-ui/history-fallback-check", nil)
	w := httptest.NewRecorder()

	if err := proxyControlUIRequestForTest(t, proxyService, scope, token, w, req); err != nil {
		t.Fatalf("ProxyRequestWithScope() error = %v", err)
	}
	body := w.Body.String()
	wantIcon := `href="/api/v1/instances/42/control-ui/favicon.svg"`
	if got := strings.Count(body, wantIcon); got != 3 {
		t.Fatalf("rewritten icon href count = %d, want 3; body: %s", got, body)
	}
	for _, nestedOrRelative := range []string{
		`href="favicon.svg"`,
		`href="./favicon.svg"`,
		`href="/favicon.svg"`,
		`/control-ui/history-fallback-check/favicon.svg`,
	} {
		if strings.Contains(body, nestedOrRelative) {
			t.Fatalf("response body contains nested or relative icon path %q: %s", nestedOrRelative, body)
		}
	}
	if !strings.Contains(body, `<base href="/api/v1/instances/42/control-ui/">`) {
		t.Fatalf("response body missing control-ui base tag: %s", body)
	}
	if !strings.Contains(body, `href="./assets/index.css"`) {
		t.Fatalf("response body changed asset href unexpectedly: %s", body)
	}
	if strings.Contains(body, "token=") {
		t.Fatalf("response body unexpectedly contains token-bearing URL: %s", body)
	}
}

func TestProxyRequestWithControlUIScopeDoesNotRewriteNonHTMLAssets(t *testing.T) {
	const assetBody = `body { background-image: url("favicon.svg"); }`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		_, _ = io.WriteString(w, assetBody)
	}))
	defer upstream.Close()

	scope, err := ResolveInstanceAccessScope(42, "openclaw", AccessModeControlUI, DefaultDesktopTargetPort)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() error = %v", err)
	}
	proxyService, accessService := newProxyServiceForUpstream(t, upstream, scope.TargetPort)
	token := generateScopedProxyToken(t, accessService, scope)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/42/control-ui/assets/index.css", nil)
	w := httptest.NewRecorder()

	if err := proxyControlUIRequestForTest(t, proxyService, scope, token, w, req); err != nil {
		t.Fatalf("ProxyRequestWithScope() error = %v", err)
	}
	if got := w.Body.String(); got != assetBody {
		t.Fatalf("asset body = %q, want %q", got, assetBody)
	}
}

func TestProxyRequestWithDesktopScopeInjectsDesktopProxyBase(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<html><head><title>Desktop</title><link rel="icon" href="favicon.svg"></head><body></body></html>`)
	}))
	defer upstream.Close()

	scope, err := ResolveInstanceAccessScope(42, "openclaw", AccessModeDesktop, DefaultDesktopTargetPort)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() error = %v", err)
	}
	proxyService, accessService := newProxyServiceForUpstream(t, upstream, scope.TargetPort)
	token := generateScopedProxyToken(t, accessService, scope)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/42/proxy/", nil)
	w := httptest.NewRecorder()

	if err := proxyService.ProxyRequestWithScope(context.Background(), scope, token, w, req); err != nil {
		t.Fatalf("ProxyRequestWithScope() error = %v", err)
	}
	body := w.Body.String()
	if !strings.Contains(body, `<base href="/api/v1/instances/42/proxy/">`) {
		t.Fatalf("response body missing desktop proxy base tag: %s", body)
	}
	if strings.Contains(body, `/api/v1/instances/42/control-ui/`) {
		t.Fatalf("response body unexpectedly used control-ui base: %s", body)
	}
	if !strings.Contains(body, `href="favicon.svg"`) {
		t.Fatalf("desktop icon href changed unexpectedly: %s", body)
	}
}

func TestProxyRequestWithControlUIScopeDoesNotFallbackToDesktopOnUpstreamFailure(t *testing.T) {
	t.Setenv("INSTANCE_ACCESS_TOKEN_SECRET", "proxy-service-test-secret")
	accessService := NewInstanceAccessService()
	scope, err := ResolveInstanceAccessScope(42, "openclaw", AccessModeControlUI, DefaultDesktopTargetPort)
	if err != nil {
		t.Fatalf("ResolveInstanceAccessScope() error = %v", err)
	}
	token := generateScopedProxyToken(t, accessService, scope)

	proxyService := NewInstanceProxyService(accessService)
	proxyService.serviceResolver = func(ctx context.Context, userID, instanceID int, targetPort int32) (*k8s.ServiceInfo, error) {
		if targetPort != scope.TargetPort {
			t.Fatalf("targetPort = %d, want control-ui target port %d", targetPort, scope.TargetPort)
		}
		return nil, errors.New("control-ui upstream unavailable")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/42/control-ui/", nil)
	w := httptest.NewRecorder()

	err = proxyService.ProxyRequestWithScopeAndUpstreamAuth(context.Background(), scope, token, InstanceProxyUpstreamAuth{
		OpenClawGatewayToken: "test-openclaw-upstream-token",
	}, w, req)
	if err == nil {
		t.Fatalf("ProxyRequestWithScope() error = nil, want upstream failure")
	}
	if !strings.Contains(err.Error(), "control-ui upstream unavailable") {
		t.Fatalf("ProxyRequestWithScope() error = %v", err)
	}
}

func TestResolveProxyHostUsesServiceDialPortWhenItDiffersFromPodTargetPort(t *testing.T) {
	proxyService := NewInstanceProxyService(NewInstanceAccessService())
	serviceInfo := &k8s.ServiceInfo{
		ClusterIP:   "10.0.0.42",
		ServicePort: 3001,
		TargetPort:  18789,
	}

	if got := proxyService.resolveProxyHost(context.Background(), 7, 42, serviceInfo); got != "10.0.0.42:3001" {
		t.Fatalf("resolveProxyHost() = %q, want service dial port 10.0.0.42:3001", got)
	}
}

func TestControlUICreateServiceRequestKeepsDesktopPortAsAdditionalPort(t *testing.T) {
	proxyService := NewInstanceProxyService(NewInstanceAccessService())

	ports := proxyService.getAdditionalPorts(DefaultControlUITargetPort)
	if len(ports) != 1 || ports[0] != DefaultDesktopTargetPort {
		t.Fatalf("getAdditionalPorts(control-ui) = %#v, want desktop 3001 preserved", ports)
	}
}
