package handlers

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
)

type dashboardHandlerInstances struct {
	repository.InstanceRepository
	instance *models.Instance
}

func (r *dashboardHandlerInstances) GetByID(id int) (*models.Instance, error) {
	if id == r.instance.ID {
		return r.instance, nil
	}
	return nil, nil
}

type dashboardHandlerUsers struct{ repository.UserRepository }

func (*dashboardHandlerUsers) GetByID(id int) (*models.User, error) {
	return &models.User{ID: id, IsActive: true, Role: "user"}, nil
}

type dashboardHandlerBindings struct {
	repository.InstanceRuntimeBindingRepository
	binding *models.InstanceRuntimeBinding
}

func (r *dashboardHandlerBindings) GetRunningByInstanceID(context.Context, int) (*models.InstanceRuntimeBinding, error) {
	return r.binding, nil
}

type dashboardHandlerPods struct {
	repository.RuntimePodRepository
	pod *models.RuntimePod
}

func (r *dashboardHandlerPods) GetByID(context.Context, int64) (*models.RuntimePod, error) {
	return r.pod, nil
}

type dashboardHandlerTeams struct{}

func (*dashboardHandlerTeams) IsTeamInstance(int) (bool, error) { return false, nil }

type dashboardHandlerAgent struct {
	services.RuntimeAgentClient
}

func (*dashboardHandlerAgent) HealthCapabilities(context.Context, string) (*services.RuntimeAgentHealthCapabilities, error) {
	health := &services.RuntimeAgentHealthCapabilities{}
	health.Capabilities.HermesDesktopWeb = &services.HermesDesktopRuntimeCapability{
		ContractVersion: 1,
		Enabled:         true,
		HermesRef:       services.HermesDesktopRef,
		HermesCommit:    services.HermesDesktopCommit,
		RPCProtocol:     "hermes-jsonrpc-v1",
		BackendMode:     "dashboard",
		AuthMode:        "password-cookie",
	}
	return health, nil
}

type dashboardHandlerInstanceService struct {
	services.InstanceService
	instance *models.Instance
}

func (s *dashboardHandlerInstanceService) GetByID(int) (*models.Instance, error) {
	return s.instance, nil
}

type dashboardHandlerRedis struct {
	services.PlatformRedisClient
	mu     sync.Mutex
	values map[string]string
	used   int
}

func (*dashboardHandlerRedis) Ping(context.Context) error { return nil }
func (r *dashboardHandlerRedis) Get(_ context.Context, k string) (string, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.values[k]
	return v, ok, nil
}
func (r *dashboardHandlerRedis) Set(_ context.Context, k, v string, _ time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values[k] = v
	return nil
}
func (r *dashboardHandlerRedis) SetPersistentNX(_ context.Context, k, v string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.values[k]; ok {
		return false, nil
	}
	r.values[k] = v
	return true, nil
}
func (r *dashboardHandlerRedis) SetNX(ctx context.Context, k, v string, ttl time.Duration) (bool, error) {
	r.mu.Lock()
	r.used++
	r.mu.Unlock()
	return r.SetPersistentNX(ctx, k, v)
}

func TestHermesDashboardHandlerManagedSSOAndBoundaries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Origin") != "http://clawmanager.internal:9001" || r.Header.Get("X-Forwarded-Host") != "clawmanager.internal:9001" {
			t.Error("internal origin missing")
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Forwarded") != "" || strings.Contains(r.Header.Get("Cookie"), "browser-forged") {
			t.Error("browser auth forwarded")
		}
		switch r.URL.Path {
		case "/auth/password-login":
			w.Header().Set("Content-Type", "application/json")
			http.SetCookie(w, &http.Cookie{Name: "hermes_session_at", Value: "upstream-cookie-canary", Path: "/", HttpOnly: true})
			fmt.Fprint(w, `{"ok":true}`)
		case "/chat/", "/chat":
			if c, err := r.Cookie("hermes_session_at"); err != nil || c.Value != "upstream-cookie-canary" {
				t.Error("server cookie missing")
			}
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("Set-Cookie", "hermes_session_at=must-not-forward")
			fmt.Fprint(w, `<!doctype html><html><head><script>window.__HERMES_AUTH_REQUIRED__=true;window.__HERMES_BASE_PATH__="/api/v1/instances/123/proxy";</script></head><body>Chat shell</body></html>`)
		default:
			t.Errorf("unexpected upstream request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	ip, rawPort, _ := net.SplitHostPort(u.Host)
	port, _ := strconv.Atoi(rawPort)
	password := "managed-password-canary"
	i := &models.Instance{ID: 123, UserID: 45, Type: "hermes", InstanceMode: "lite", RuntimeType: "gateway", RuntimeGeneration: 3, Status: "running", AccessToken: &password}
	redis := &dashboardHandlerRedis{values: map[string]string{}}
	svc := services.NewHermesDesktopService(services.HermesDesktopConfig{Enabled: true, ControlUIOrigin: "http://clawmanager.internal:9001", Secret: "CM-test-key", Instances: &dashboardHandlerInstances{instance: i}, Users: &dashboardHandlerUsers{}, Bindings: &dashboardHandlerBindings{binding: &models.InstanceRuntimeBinding{InstanceID: 123, RuntimeType: "hermes", Generation: 3, RuntimePodID: 9, GatewayPort: port, State: "running"}}, Pods: &dashboardHandlerPods{pod: &models.RuntimePod{ID: 9, RuntimeType: "hermes", PodIP: &ip, AgentEndpoint: &upstream.URL, State: "ready"}}, Teams: &dashboardHandlerTeams{}, Agent: &dashboardHandlerAgent{}, Redis: redis})
	accessTokens := services.NewInstanceAccessService()
	defer accessTokens.Stop()
	h := &InstanceHandler{
		instanceService: &dashboardHandlerInstanceService{instance: i},
		accessService:   accessTokens,
		proxyService:    services.NewInstanceProxyService(accessTokens),
		externalAccessService: &fakeSharedExternalAccessService{access: &models.InstanceExternalAccess{
			InstanceID: 123, Enabled: true, AuthMode: services.ExternalAccessModeShareLink,
			WorkspaceAccess: services.ExternalWorkspaceAccessRead,
		}},
	}
	h.SetHermesDesktopService(svc)
	router := gin.New()
	router.Use(HermesDesktopRedactTickets(), gin.Recovery())
	router.POST("/api/v1/instances/:id/access", func(c *gin.Context) { c.Set("userID", 45); c.Set("userRole", "user"); h.GenerateAccessToken(c) })
	router.GET("/api/v1/shared-instances/:code/session", h.GetSharedInstanceSession)
	router.GET("/api/v1/instances/:id/hermes-desktop/session", h.hermesDesktop.Session)
	router.Any("/api/v1/instances/:id/proxy/*path", h.ProxyInstance)
	const origin = "https://manager.example:39443"
	request := func(method, path string, cookie *http.Cookie, requestOrigin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, origin+path, nil)
		r.Header.Set("Origin", requestOrigin)
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r.Header.Set("Authorization", "Bearer browser-forged")
		r.Header.Set("Forwarded", "host=evil.example")
		r.AddCookie(&http.Cookie{Name: "hermes_session_at", Value: "browser-forged"})
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	bootstrap := request("POST", "/api/v1/instances/123/access", nil, origin)
	if bootstrap.Code != 200 {
		t.Fatalf("bootstrap status=%d body=%s", bootstrap.Code, bootstrap.Body.String())
	}
	cookies := bootstrap.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one CM cookie: %d", len(cookies))
	}
	cm := cookies[0]
	if cm.Name != "cm_hermes_dashboard_123" || cm.Path != "/api/v1/instances/123/proxy/" || !cm.HttpOnly || !cm.Secure || cm.SameSite != http.SameSiteStrictMode || cm.MaxAge != 600 {
		t.Fatal("unsafe CM cookie attributes")
	}
	for _, secret := range []string{password, "upstream-cookie-canary", "must-not-forward", "browser-forged"} {
		if strings.Contains(bootstrap.Body.String(), secret) || strings.Contains(bootstrap.Header().Get("Set-Cookie"), secret) {
			t.Fatal("secret escaped bootstrap")
		}
	}
	page := request("GET", "/api/v1/instances/123/proxy/chat/", cm, origin)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "Chat shell") || page.Header().Get("Set-Cookie") != "" {
		t.Fatalf("chat failed: %d %s", page.Code, page.Body.String())
	}

	sharedRequest := httptest.NewRequest(http.MethodGet, origin+"/api/v1/shared-instances/sl_test/session", nil)
	sharedRequest.Header.Set("X-Forwarded-Proto", "https")
	sharedResponse := httptest.NewRecorder()
	router.ServeHTTP(sharedResponse, sharedRequest)
	if sharedResponse.Code != http.StatusOK || !strings.Contains(sharedResponse.Body.String(), `"access_url":"/hermes-desktop-web/?instance_id=123"`) {
		t.Fatalf("shared Desktop Web session failed: %d %s", sharedResponse.Code, sharedResponse.Body.String())
	}
	var sharedDesktopCookie *http.Cookie
	for _, cookie := range sharedResponse.Result().Cookies() {
		if cookie.Name == "cm_hermes_desktop_123" {
			sharedDesktopCookie = cookie
			break
		}
	}
	if sharedDesktopCookie == nil || sharedDesktopCookie.Path != "/api/v1/instances/123/hermes-desktop/" || !sharedDesktopCookie.HttpOnly || !sharedDesktopCookie.Secure {
		t.Fatal("shared session did not issue the scoped Desktop Web cookie")
	}
	sharedSession := request("GET", "/api/v1/instances/123/hermes-desktop/session", sharedDesktopCookie, origin)
	if sharedSession.Code != http.StatusOK || !strings.Contains(sharedSession.Body.String(), `"renderer_url":"/hermes-desktop-web/?instance_id=123"`) {
		t.Fatalf("shared Desktop Web cookie could not open a session: %d %s", sharedSession.Code, sharedSession.Body.String())
	}
	before := calls.Load()
	for _, tc := range []struct {
		path, origin string
		cookie       *http.Cookie
		status       int
	}{
		{"/api/v1/instances/123/proxy/api/env/reveal", origin, cm, 403},
		{"/api/v1/instances/123/proxy/chat/", "https://evil.example", cm, 403},
		{"/api/v1/instances/123/proxy/chat/", origin, nil, 401},
		{"/api/v1/instances/123/proxy/chat/", origin, &http.Cookie{Name: "instance_access_123", Value: "old-instance-access"}, 401},
	} {
		w := request("GET", tc.path, tc.cookie, tc.origin)
		if w.Code != tc.status || w.Header().Get("Set-Cookie") != "" {
			t.Errorf("%s expected %d got %d", tc.path, tc.status, w.Code)
		}
	}
	if calls.Load() != before {
		t.Fatal("denied request reached Runtime")
	}
	for _, query := range []string{"ticket=first&ticket=second", "ticket=first"} {
		r := httptest.NewRequest("GET", origin+"/api/v1/instances/123/proxy/api/ws?"+query, nil)
		r.Header.Set("Origin", origin)
		r.Header.Set("Upgrade", "websocket")
		r.AddCookie(cm)
		if query == "ticket=first" {
			r.Header.Set("Sec-WebSocket-Protocol", "hermes-gateway-ticket.browser-forged")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Errorf("ambiguous/browser protocol ticket accepted: %d", w.Code)
		}
	}
	if redis.used != 0 {
		t.Fatal("invalid boundary request consumed ticket")
	}
	if err := svc.RevokeUserSessions(context.Background(), 45); err != nil {
		t.Fatal(err)
	}
	if w := request("GET", "/api/v1/instances/123/proxy/chat/", cm, origin); w.Code != 401 {
		t.Fatal("classic cookie survived logout with Desktop flag off")
	}
}

func TestHermesDashboardProxyRecoveryRedactsCredentialsWithoutBreakingClones(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var logs bytes.Buffer
	router := gin.New()
	router.Use(HermesDesktopRedactTickets(), gin.LoggerWithWriter(&logs), gin.RecoveryWithWriter(&logs))
	router.GET("/api/v1/instances/:id/proxy/api/ws", func(c *gin.Context) {
		original := originalInstanceProxyRequest(c)
		if original.URL.Query().Get("ticket") != "ticket-canary" || original.Header.Get("Cookie") == "" {
			t.Error("private clone lost proxy credentials")
		}
		if c.GetHeader("Cookie") != "" || c.GetHeader("Sec-WebSocket-Protocol") != "" {
			t.Error("log-visible headers not redacted")
		}
		panic("test recovery")
	})
	r := httptest.NewRequest("GET", "/api/v1/instances/123/proxy/api/ws?ticket=ticket-canary&token=token-canary&password=password-canary", nil)
	r.Header.Set("Cookie", "cm_hermes_dashboard_123=cookie-canary")
	r.Header.Set("Sec-WebSocket-Protocol", "hermes-gateway-ticket.protocol-canary")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 500 {
		t.Fatal("recovery did not run")
	}
	if strings.Contains(logs.String(), "canary") {
		t.Fatal("access or panic log leaked credentials")
	}
}

func TestHermesDashboardAccessLoggingRedactsCredentialsAndPreservesBearer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, recoverPanic := range []bool{false, true} {
		name := "access-log"
		if recoverPanic {
			name = "panic-recovery"
		}
		t.Run(name, func(t *testing.T) {
			var logs bytes.Buffer
			const path = "/api/v1/instances/199/access"
			const bearer = "Bearer access-bearer-canary"
			authenticated := false
			router := gin.New()
			router.Use(HermesDesktopRedactTickets(), gin.LoggerWithWriter(&logs), gin.RecoveryWithWriter(&logs))
			router.POST("/api/v1/instances/:id/access", func(c *gin.Context) {
				// The access bootstrap still uses the ordinary bearer middleware;
				// unlike proxy cookies, its Authorization must survive redaction.
				if c.GetHeader("Authorization") != bearer {
					t.Error("access redactor removed the normal bearer credential")
					c.AbortWithStatus(http.StatusUnauthorized)
					return
				}
				authenticated = true
				if c.Request.URL.RawQuery != "" || c.Request.RequestURI != path {
					t.Error("unused access query remains visible to request-dump loggers")
				}
				for _, header := range []string{"Cookie", "Referer", "Sec-WebSocket-Protocol"} {
					if c.GetHeader(header) != "" {
						t.Errorf("access redactor retained %s", header)
					}
				}
				if recoverPanic {
					panic("test access recovery")
				}
				c.Status(http.StatusNoContent)
			})
			request := httptest.NewRequest(http.MethodPost, path+"?token=access-token-canary&ticket=access-ticket-canary&unexpected=access-query-canary", nil)
			request.Header.Set("Authorization", bearer)
			request.Header.Set("Cookie", "cm_hermes_dashboard_199=access-cookie-canary")
			request.Header.Set("Referer", "https://manager.example/?token=access-referer-canary")
			request.Header.Set("Sec-WebSocket-Protocol", "hermes-gateway-ticket.access-protocol-canary")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			want := http.StatusNoContent
			if recoverPanic {
				want = http.StatusInternalServerError
			}
			if recorder.Code != want || !authenticated {
				t.Fatalf("access request did not reach authenticated handler: status=%d", recorder.Code)
			}
			if !strings.Contains(logs.String(), path) {
				t.Fatal("test did not capture access logs")
			}
			if strings.Contains(logs.String(), "canary") {
				t.Fatal("access or recovery log leaked a query, header or bearer credential")
			}
		})
	}
}
