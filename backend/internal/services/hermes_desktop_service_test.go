package services

import (
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

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"github.com/gorilla/websocket"
)

type desktopUserRepo struct {
	repository.UserRepository
	users map[int]*models.User
}

func (r *desktopUserRepo) GetByID(id int) (*models.User, error) { return r.users[id], nil }

type desktopTeamGuard struct {
	team bool
	err  error
}

func (r *desktopTeamGuard) IsTeamInstance(int) (bool, error) { return r.team, r.err }

type desktopAgent struct {
	RuntimeAgentClient
	health *RuntimeAgentHealthCapabilities
}

func (a *desktopAgent) HealthCapabilities(context.Context, string) (*RuntimeAgentHealthCapabilities, error) {
	return a.health, nil
}

type desktopRedis struct {
	PlatformRedisClient
	mu      sync.Mutex
	used    map[string]bool
	values  map[string]string
	lastTTL time.Duration
	err     error
}

func (r *desktopRedis) Ping(context.Context) error { return r.err }

func (r *desktopRedis) Get(_ context.Context, key string) (string, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, found := r.values[key]
	return value, found, r.err
}

func (r *desktopRedis) Set(_ context.Context, key, value string, ttl time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	if r.values == nil {
		r.values = map[string]string{}
	}
	r.values[key] = value
	r.lastTTL = ttl
	return nil
}

func (r *desktopRedis) SetPersistentNX(_ context.Context, key, value string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return false, r.err
	}
	if _, exists := r.values[key]; exists {
		return false, nil
	}
	if r.values == nil {
		r.values = map[string]string{}
	}
	r.values[key] = value
	return true, nil
}

func (r *desktopRedis) SetNX(_ context.Context, key, value string, ttl time.Duration) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return false, r.err
	}
	if r.used[key] {
		return false, nil
	}
	r.used[key] = true
	return true, nil
}

func desktopFixture(t *testing.T, upstream string) *HermesDesktopService {
	t.Helper()
	ip, port := splitURLHostPortForProxyTest(t, upstream)
	instances := newV2LifecycleInstanceRepo()
	password := "managed-Hermes-password"
	instances.byID[123] = &models.Instance{ID: 123, UserID: 45, Type: "hermes", InstanceMode: "lite", RuntimeType: RuntimeBackendGateway, Status: "running", RuntimeGeneration: 3, AccessToken: &password}
	bindings := newFakeRuntimeBindingRepo()
	bindings.bindings[123] = &models.InstanceRuntimeBinding{InstanceID: 123, RuntimeType: "hermes", RuntimePodID: 9, Generation: 3, GatewayPort: port, State: "running"}
	pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{9: {ID: 9, RuntimeType: "hermes", PodIP: &ip, AgentEndpoint: &upstream, State: "ready"}}}
	health := &RuntimeAgentHealthCapabilities{}
	health.Capabilities.HermesDesktopWeb = &HermesDesktopRuntimeCapability{ContractVersion: 1, Enabled: true, HermesRef: HermesDesktopRef, HermesCommit: HermesDesktopCommit, RPCProtocol: "hermes-jsonrpc-v1", BackendMode: "dashboard", AuthMode: "password-cookie"}
	return NewHermesDesktopService(HermesDesktopConfig{Enabled: true, ControlUIOrigin: "http://clawmanager.internal:9001", Secret: "test-secret", Instances: instances, Bindings: bindings, Pods: pods, Users: &desktopUserRepo{users: map[int]*models.User{45: {ID: 45, Role: "user", IsActive: true}, 46: {ID: 46, Role: "user", IsActive: true}, 1: {ID: 1, Role: "admin", IsActive: true}}}, Teams: &desktopTeamGuard{}, Agent: &desktopAgent{health: health}, Redis: &desktopRedis{used: map[string]bool{}, values: map[string]string{"hermes-desktop:user-epoch:45": "initial"}}})
}

func TestHermesDesktopCapabilityAndOwnershipGates(t *testing.T) {
	for _, tc := range []struct {
		name    string
		modify  func(*HermesDesktopService)
		uid     int
		reason  string
		wantErr error
	}{
		{name: "ready", uid: 45},
		{name: "other owner", uid: 46, wantErr: ErrHermesDesktopForbidden},
		{name: "admin", uid: 1},
		{name: "disabled", uid: 45, reason: "feature_disabled", modify: func(s *HermesDesktopService) { s.config.Enabled = false }},
		{name: "no Redis", uid: 45, reason: "ticket_store_unavailable", modify: func(s *HermesDesktopService) { s.config.Redis = nil }},
		{name: "Redis offline", uid: 45, reason: "ticket_store_unavailable", modify: func(s *HermesDesktopService) { s.config.Redis.(*desktopRedis).err = errors.New("offline") }},
		{name: "missing signing secret", uid: 45, reason: "runtime_auth_unavailable", modify: func(s *HermesDesktopService) { s.config.Secret = "" }},
		{name: "inactive user", uid: 45, wantErr: ErrHermesDesktopUnauthorized, modify: func(s *HermesDesktopService) { s.config.Users.(*desktopUserRepo).users[45].IsActive = false }},
		{name: "Team", uid: 45, reason: "team_not_supported", modify: func(s *HermesDesktopService) { s.config.Teams = &desktopTeamGuard{team: true} }},
		{name: "Team lookup failure", uid: 45, reason: "runtime_unavailable", modify: func(s *HermesDesktopService) { s.config.Teams = &desktopTeamGuard{err: errors.New("db")} }},
		{name: "old runtime", uid: 45, reason: "runtime_capability_unsupported", modify: func(s *HermesDesktopService) {
			s.config.Agent.(*desktopAgent).health.Capabilities.HermesDesktopWeb = nil
		}},
		{name: "different revision", uid: 45, reason: "runtime_capability_unsupported", modify: func(s *HermesDesktopService) {
			s.config.Agent.(*desktopAgent).health.Capabilities.HermesDesktopWeb.HermesCommit = "other"
		}},
		{name: "stale generation", uid: 45, reason: "runtime_not_ready", modify: func(s *HermesDesktopService) {
			s.config.Instances.(*v2LifecycleInstanceRepo).byID[123].RuntimeGeneration++
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := desktopFixture(t, "http://127.0.0.1:9000")
			if tc.modify != nil {
				tc.modify(s)
			}
			d, err := s.Describe(context.Background(), tc.uid, 123)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err=%v", err)
			}
			if err == nil && (d.Reason != tc.reason || d.Available != (tc.reason == "")) {
				t.Fatalf("descriptor=%+v", d)
			}
		})
	}
}

func TestHermesDesktopCookieScopeAndTicketReplay(t *testing.T) {
	s := desktopFixture(t, "http://127.0.0.1:9000")
	c := HermesDesktopClaims{UserID: 45, InstanceID: 123, Generation: 3, PodID: 9, Port: 9000, SessionID: "browser-session", Epoch: "initial"}
	raw, err := s.sign(&c, "session", HermesDesktopSessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Authenticate(context.Background(), raw, 124); !errors.Is(err, ErrHermesDesktopForbidden) {
		t.Fatalf("cross-instance accepted: %v", err)
	}
	if _, err = s.Authenticate(context.Background(), raw, 123); err != nil {
		t.Fatal(err)
	}
	wsURL, _, err := s.MintTicket(&c)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(wsURL)
	ticket := u.Query().Get("ticket")
	if _, err = s.parse(ticket, "session"); err == nil {
		t.Fatal("WS ticket accepted as cookie")
	}
	if err = s.redeemTicket(context.Background(), ticket, &c); err != nil {
		t.Fatal(err)
	}
	if err = s.redeemTicket(context.Background(), ticket, &c); !errors.Is(err, ErrHermesDesktopUnauthorized) {
		t.Fatalf("replayed ticket accepted: %v", err)
	}
	otherReplica := NewHermesDesktopService(s.config)
	if err = otherReplica.redeemTicket(context.Background(), ticket, &c); !errors.Is(err, ErrHermesDesktopUnauthorized) {
		t.Fatalf("ticket replay across replicas accepted: %v", err)
	}
	c.Generation++
	if _, err = s.authorizeClaims(context.Background(), &c); !errors.Is(err, ErrHermesDesktopUnauthorized) {
		t.Fatalf("stale cookie accepted: %v", err)
	}
}

func TestHermesDesktopCapabilityControlAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/health" || r.Header.Get("X-ClawManager-Control-Token") != "control-only-secret" {
			t.Error("invalid capability control request")
		}
		_, _ = w.Write([]byte(`{"capabilities":{"hermes_desktop_web":{"contract_version":1,"enabled":true}}}`))
	}))
	defer server.Close()
	client := NewRuntimeAgentClient("control-only-secret").(HermesDesktopCapabilityClient)
	result, err := client.HealthCapabilities(context.Background(), server.URL)
	if err != nil || result.Capabilities.HermesDesktopWeb == nil || result.Capabilities.HermesDesktopWeb.ContractVersion != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestHermesDesktopExpiredUpstreamCookieRetriesOnce(t *testing.T) {
	var logins atomic.Int32
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/password-login" {
			logins.Add(1)
			http.SetCookie(w, &http.Cookie{Name: "hermes_session_at", Value: "managed-session", Path: "/", HttpOnly: true})
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		requests.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	s := desktopFixture(t, server.URL)
	_, raw, err := s.Activate(context.Background(), 45, 123)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := s.Authenticate(context.Background(), raw, 123)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProxyAPI(context.Background(), claims, "GET", "/status", nil); !errors.Is(err, ErrHermesDesktopUpstream) {
		t.Fatalf("unexpected unauthorized outcome: %v", err)
	}
	if logins.Load() != 2 || requests.Load() != 2 {
		t.Fatalf("retry not bounded: logins=%d requests=%d", logins.Load(), requests.Load())
	}
}

func TestHermesDesktopHTTPPolicyAndSanitization(t *testing.T) {
	for _, raw := range []string{"/auth/ws-ticket", "/sessions/../../config", "/sessions?profile=other", "/sessions?limit=99999", "/sessions?limit=1&limit=2", "/fs/read?path=/etc/passwd"} {
		u, _ := url.Parse(raw)
		if hermesDesktopHTTPAllowed("GET", u.Path, u.Query()) {
			t.Errorf("allowed %s", raw)
		}
	}
	for _, raw := range []string{"/status", "/config/schema", "/sessions?limit=40&offset=0&min_messages=0&archived=exclude&order=recent", "/sessions/abc-123/messages?limit=200&order=latest", "/model/options?explicit_only=1&include_unconfigured=true"} {
		u, _ := url.Parse(raw)
		if !hermesDesktopHTTPAllowed("GET", u.Path, u.Query()) {
			t.Errorf("rejected %s", raw)
		}
	}
	if hermesDesktopHTTPAllowed("POST", "/sessions", nil) {
		t.Fatal("mutation allowed")
	}
	body, err := hermesDesktopSanitize([]byte(`{"session_token":"sensitive","nested":{"api_key":"key","apiKey":"camel-secret","accessToken":"camel-access","sessionToken":"camel-session","message":"private-password cookie-value"},"input_tokens":40}`), "private-password", []*http.Cookie{{Value: "cookie-value"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"sensitive", "api_key", "private-password", "cookie-value", "camel-secret", "camel-access", "camel-session"} {
		if strings.Contains(string(body), secret) {
			t.Errorf("leaked %s", secret)
		}
	}
	if !strings.Contains(string(body), "input_tokens") {
		t.Fatal("usage counts removed")
	}
}

func TestHermesDesktopRPCPolicy(t *testing.T) {
	for _, raw := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"session.create","params":{"source":"native"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"shell.exec","params":{"command":"id"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"session.create","params":{"cwd":"/workspaces/another"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"session.resume","params":{"session_id":"abc","profile":"other"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"prompt.submit","params":{"session_id":"abc","text":"hello","_hosted_task":{}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"approval.respond","params":{"session_id":"abc","choice":"always"}}`,
		`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`,
	} {
		if _, err := hermesDesktopFilterRPC([]byte(raw)); err == nil {
			t.Errorf("unsafe RPC accepted: %s", raw)
		}
	}
	for _, raw := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"session.create","params":{"source":"web"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"session.events.since","params":{"session_id":"abc","last_seen":2}}`,
		`{"jsonrpc":"2.0","id":"__heartbeat__1","method":"gateway.ping","params":{}}`,
	} {
		if _, err := hermesDesktopFilterRPC([]byte(raw)); err != nil {
			t.Errorf("valid RPC rejected: %s", raw)
		}
	}
}

func TestHermesDesktopLogoutRevokesAllReplicas(t *testing.T) {
	s := desktopFixture(t, "http://127.0.0.1:9000")
	claims := HermesDesktopClaims{UserID: 45, InstanceID: 123, Generation: 3, PodID: 9, Port: 9000, SessionID: "browser-session", Epoch: "initial"}
	raw, err := s.sign(&claims, "session", HermesDesktopSessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	otherReplica := NewHermesDesktopService(s.config)
	if _, err := otherReplica.Authenticate(context.Background(), raw, 123); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeUserSessions(context.Background(), 45); err != nil {
		t.Fatal(err)
	}
	if s.config.Redis.(*desktopRedis).lastTTL != 0 {
		t.Fatal("logout epoch must not expire while fresh leases remain valid")
	}
	if _, err := otherReplica.Authenticate(context.Background(), raw, 123); !errors.Is(err, ErrHermesDesktopUnauthorized) {
		t.Fatalf("logout left cookie valid: %v", err)
	}
	epoch, err := otherReplica.sessionEpoch(context.Background(), 45)
	if err != nil || epoch == "initial" {
		t.Fatalf("new epoch unavailable: %v", err)
	}
	claims.Epoch = epoch
	raw, err = otherReplica.sign(&claims, "session", HermesDesktopSessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(context.Background(), raw, 123); err != nil {
		t.Fatalf("fresh lease rejected: %v", err)
	}
	delete(s.config.Redis.(*desktopRedis).values, "hermes-desktop:user-epoch:45")
	if _, err := s.Authenticate(context.Background(), raw, 123); !errors.Is(err, ErrHermesDesktopUnauthorized) {
		t.Fatalf("missing Redis epoch resurrected cookie: %v", err)
	}
	newEpoch, err := s.ensureSessionEpoch(context.Background(), 45)
	if err != nil || newEpoch == epoch || newEpoch == "" {
		t.Fatalf("bootstrap failed to establish fresh epoch: %v", err)
	}
	s.config.Redis.(*desktopRedis).err = errors.New("offline")
	if _, err := s.Authenticate(context.Background(), raw, 123); !errors.Is(err, ErrHermesDesktopUnavailable) {
		t.Fatalf("Redis outage did not fail closed: %v", err)
	}
}

func TestHermesDesktopLogoutWinsOverInflightActivation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		http.SetCookie(w, &http.Cookie{Name: "hermes_session_at", Value: "private-session", Path: "/", HttpOnly: true})
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	s := desktopFixture(t, server.URL)
	done := make(chan error, 1)
	go func() {
		_, cookie, err := s.Activate(context.Background(), 45, 123)
		if cookie != "" {
			done <- errors.New("inflight activation emitted a post-logout cookie")
			return
		}
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("activation did not reach login")
	}
	if err := s.RevokeUserSessions(context.Background(), 45); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; !errors.Is(err, ErrHermesDesktopUnauthorized) {
		t.Fatalf("logout did not cancel activation: %v", err)
	}
}

func TestHermesDesktopUpstreamAuthenticationAndWebSocket(t *testing.T) {
	var loginCount atomic.Int32
	var configWrites atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "http://clawmanager.internal:9001" || r.Header.Get("X-Forwarded-Host") != "clawmanager.internal:9001" {
			t.Error("internal origin not normalized")
		}
		switch r.URL.Path {
		case "/auth/password-login":
			loginCount.Add(1)
			var b map[string]string
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b["password"] != "managed-Hermes-password" || b["username"] != "clawmanager" {
				t.Error("wrong managed login")
			}
			http.SetCookie(w, &http.Cookie{Name: "hermes_session_at", Value: "upstream-cookie-secret", HttpOnly: true, Path: "/"})
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/auth/ws-ticket":
			if cookie, err := r.Cookie("hermes_session_at"); err != nil || cookie.Value != "upstream-cookie-secret" {
				t.Error("upstream cookie absent")
			}
			_, _ = w.Write([]byte(`{"ticket":"upstream-only-ticket","ttl_seconds":30}`))
		case "/api/status":
			w.Header().Set("Set-Cookie", "evil=do-not-forward")
			_, _ = w.Write([]byte(`{"version":"0.21.0","session_token":"should-not-leak","model":"configured","api_key":"do-not-leak"}`))
		case "/api/config/schema":
			_, _ = w.Write([]byte(`{"fields":{"display.language":{"category":"appearance","description":"Language","type":"select","options":["en","zh-CN"]}},"category_order":["appearance"]}`))
		case "/api/config":
			if r.Method != http.MethodPut {
				t.Errorf("unexpected config method %s", r.Method)
			}
			var request map[string]map[string]json.RawMessage
			if json.NewDecoder(r.Body).Decode(&request) != nil || request["config"] == nil {
				t.Error("invalid config write body")
			}
			configWrites.Add(1)
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/ws":
			if r.URL.RawQuery != "" || strings.Join(websocket.Subprotocols(r), ",") != "hermes-gateway-v1,hermes-gateway-ticket.upstream-only-ticket" {
				t.Error("wrong upstream ticket")
			}
			if cookie, err := r.Cookie("hermes_session_at"); err != nil || cookie.Value != "upstream-cookie-secret" {
				t.Error("websocket cookie absent")
			}
			conn, err := (&websocket.Upgrader{Subprotocols: []string{"hermes-gateway-v1"}, CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"jsonrpc":"2.0","method":"event","params":{"type":"gateway.ready","token":"should-not-leak","ticket":"upstream-only-ticket","message":"upstream-only-ticket"}}`))
			for {
				_, msg, err := conn.ReadMessage()
				if err != nil {
					return
				}
				var packet map[string]any
				_ = json.Unmarshal(msg, &packet)
				_ = conn.WriteJSON(map[string]any{"jsonrpc": "2.0", "id": packet["id"], "result": map[string]any{"method": packet["method"]}})
			}
		default:
			t.Errorf("unexpected upstream path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	s := desktopFixture(t, upstream.URL)
	d, cookie, err := s.Activate(context.Background(), 45, 123)
	if err != nil || !d.Available {
		t.Fatalf("activate: %+v %v", d, err)
	}
	claims, err := s.Authenticate(context.Background(), cookie, 123)
	if err != nil {
		t.Fatal(err)
	}
	status, err := s.ProxyAPI(context.Background(), claims, "GET", "/status", nil)
	if err != nil || strings.Contains(string(status), "token") || strings.Contains(string(status), "api_key") {
		t.Fatalf("status=%s err=%v", status, err)
	}
	schema, err := s.ProxyAPI(context.Background(), claims, "GET", "/config/schema", nil)
	if err != nil || !strings.Contains(string(schema), `"fields"`) {
		t.Fatalf("managed browser schema=%s err=%v", schema, err)
	}
	written, err := s.ProxyAPIRequest(context.Background(), claims, http.MethodPut, "/config", nil, []byte(`{"config":{"display":{"language":"zh-CN"}}}`))
	if err != nil || !strings.Contains(string(written), `"ok":true`) || configWrites.Load() != 1 {
		t.Fatalf("managed config write=%s writes=%d err=%v", written, configWrites.Load(), err)
	}
	wsURL, _, err := s.MintTicket(claims)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(wsURL)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.ProxyWebSocket(r.Context(), claims, r.URL.Query().Get("ticket"), w, r); err != nil {
			http.Error(w, "denied", 401)
		}
	}))
	defer proxy.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(proxy.URL, "http")+"/?"+u.RawQuery, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, ready, err := conn.ReadMessage()
	if err != nil || strings.Contains(string(ready), "should-not-leak") || strings.Contains(string(ready), "upstream-only-ticket") {
		t.Fatalf("ready=%s %v", ready, err)
	}
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"jsonrpc":"2.0","id":5,"method":"shell.exec","params":{}}`))
	_, denied, err := conn.ReadMessage()
	if err != nil || !strings.Contains(string(denied), "-32601") {
		t.Fatalf("denied=%s %v", denied, err)
	}
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"jsonrpc":"2.0","id":6,"method":"gateway.ping","params":{}}`))
	_, pong, err := conn.ReadMessage()
	if err != nil || !strings.Contains(string(pong), `"method":"ping"`) {
		t.Fatalf("pong=%s %v", pong, err)
	}
	if err := s.RevokeUserSessions(context.Background(), 45); err != nil {
		t.Fatal(err)
	}
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"jsonrpc":"2.0","id":7,"method":"ping","params":{}}`))
	if _, frame, err := conn.ReadMessage(); err == nil {
		t.Fatalf("revoked websocket still forwarded a response: %s", frame)
	}
	if loginCount.Load() != 1 {
		t.Fatalf("cached login count=%d", loginCount.Load())
	}
}
