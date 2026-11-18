package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"clawreef/internal/browserorigin"
	"clawreef/internal/models"
	"clawreef/internal/workbench"
	"github.com/gorilla/websocket"
)

type previewTestStore struct {
	sync.Mutex
	values map[string]workbench.PreviewLease
}

func (s *previewTestStore) Put(_ context.Context, k string, v workbench.PreviewLease) error {
	s.Lock()
	defer s.Unlock()
	s.values[k] = v
	return nil
}
func (s *previewTestStore) Get(_ context.Context, k string) (workbench.PreviewLease, error) {
	s.Lock()
	defer s.Unlock()
	v, ok := s.values[k]
	if !ok {
		return v, workbench.ErrPreviewNotFound
	}
	return v, nil
}

func TestWorkbenchPreviewRedirectsStayOnCapabilityOrigin(t *testing.T) {
	for _, tc := range []struct {
		name, target, location, want string
	}{
		{"absolute", "http://localhost:4321", "http://localhost:4321/login", "{origin}/login"},
		{"network localhost", "http://localhost:4321", "//localhost:4321/login", "{origin}/login"},
		{"network IPv4", "http://127.0.0.1:4321", "//127.0.0.1:4321/login", "{origin}/login"},
		{"network IPv6", "http://[::1]:4321", "//[::1]:4321/login", "{origin}/login"},
		{"network escaped", "http://localhost:4321", "//localhost:4321/a%2Fb?q=%2f&x=1&x=2#part%2Ftwo", "{origin}/a%2Fb?q=%2f&x=1&x=2#part%2Ftwo"},
		{"network HTTPS default port", "https://localhost", "//localhost/login", "{origin}/login"},
		{"relative", "http://localhost:4321", "/login?q=%2f#part", "/login?q=%2f#part"},
		{"relative path", "http://localhost:4321", "login", "login"},
		{"external", "http://localhost:4321", "//example.com:4321/login", "//example.com:4321/login"},
		{"different port", "http://localhost:4321", "//localhost:4322/login", "//localhost:4322/login"},
		{"credentials", "http://localhost:4321", "//user@localhost:4321/login", "//user@localhost:4321/login"},
		{"invalid scheme", "http://localhost:4321", "ftp://localhost:4321/login", "ftp://localhost:4321/login"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", tc.location)
				w.WriteHeader(http.StatusFound)
			}))
			defer runtime.Close()
			instance := &models.Instance{ID: 42, UserID: 7, Status: "running", RuntimeGeneration: 3}
			binding := &models.InstanceRuntimeBinding{InstanceID: 42, RuntimePodID: 9, Generation: 3, GatewayID: "gw-42-3", GatewayPort: 3000, WorkspacePath: "/workspace"}
			previews, err := workbench.NewPreviews("https://p-{token}.preview.example:8443", &previewTestStore{values: map[string]workbench.PreviewLease{}}, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			service := NewInstanceWorkbenchService(workbenchTestInstances{instance: instance}, workbenchTestBindings{binding}, workbenchTestPods{&models.RuntimePod{ID: 9, AgentEndpoint: &runtime.URL}}, NewRuntimeAgentClientWithHTTPClient("test-control", runtime.Client()).(WorkbenchAgent), previews)
			lease, err := previews.Create(context.Background(), workbench.PreviewScope{InstanceID: 42, UserID: 7, Generation: 3, GatewayID: "gw-42-3"}, tc.target)
			if err != nil {
				t.Fatal(err)
			}
			publicURL, _ := url.Parse(previews.URL(lease))
			// The configured public scheme must win even when ingress forwards HTTP.
			request := httptest.NewRequest("GET", "http://"+publicURL.Host+"/", nil)
			recorder := httptest.NewRecorder()
			service.ServePreview(recorder, request)
			want := strings.ReplaceAll(tc.want, "{origin}", "https://"+publicURL.Host)
			if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != want {
				t.Fatalf("redirect = %d %q, want %d %q", recorder.Code, recorder.Header().Get("Location"), http.StatusFound, want)
			}
		})
	}
}

func TestBrowserPreviewRedirectsUsePublicOrigin(t *testing.T) {
	for _, browserMode := range []bool{false, true} {
		for _, tc := range []struct {
			name, target, location, want string
		}{
			{"absolute", "http://localhost:4321", "http://localhost:4321/login", "{origin}/login"},
			{"network localhost", "http://localhost:4321", "//localhost:4321/login", "{origin}/login"},
			{"network IPv4", "http://127.0.0.1:4321", "//127.0.0.1:4321/login", "{origin}/login"},
			{"network IPv6", "http://[::1]:4321", "//[::1]:4321/login", "{origin}/login"},
			{"network escaped", "http://localhost:4321", "//localhost:4321/a%2Fb?q=%2f&x=1&x=2#part%2Ftwo", "{origin}/a%2Fb?q=%2f&x=1&x=2#part%2Ftwo"},
			{"network HTTPS default port", "https://localhost", "//localhost/login", "{origin}/login"},
			{"relative", "http://localhost:4321", "/login?q=%2f#part", "/login?q=%2f#part"},
			{"relative path", "http://localhost:4321", "login", "login"},
			{"external", "http://localhost:4321", "//example.com:4321/login", "//example.com:4321/login"},
			{"different port", "http://localhost:4321", "//localhost:4322/login", "//localhost:4322/login"},
			{"credentials", "http://localhost:4321", "//user@localhost:4321/login", "//user@localhost:4321/login"},
			{"invalid scheme", "http://localhost:4321", "ftp://localhost:4321/login", "ftp://localhost:4321/login"},
		} {
			mode := "legacy HTTP/"
			if browserMode {
				mode = "browser HTTPS/"
			}
			t.Run(mode+tc.name, func(t *testing.T) {
				runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Location", tc.location)
					w.WriteHeader(http.StatusFound)
				}))
				defer runtime.Close()
				instance := &models.Instance{ID: 42, UserID: 7, Status: "running", RuntimeGeneration: 3}
				binding := &models.InstanceRuntimeBinding{InstanceID: 42, RuntimePodID: 9, Generation: 3, GatewayID: "gw-42-3", GatewayPort: 3000, WorkspacePath: "/workspace"}
				previews, err := workbench.NewPreviews("http://p-{token}.preview.example:8080", &previewTestStore{values: map[string]workbench.PreviewLease{}}, time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				service := NewInstanceWorkbenchService(workbenchTestInstances{instance: instance}, workbenchTestBindings{binding}, workbenchTestPods{&models.RuntimePod{ID: 9, AgentEndpoint: &runtime.URL}}, NewRuntimeAgentClientWithHTTPClient("test-control", runtime.Client()).(WorkbenchAgent), previews)
				lease, err := previews.Create(context.Background(), workbench.PreviewScope{InstanceID: 42, UserID: 7, Generation: 3, GatewayID: "gw-42-3"}, tc.target)
				if err != nil {
					t.Fatal(err)
				}
				publicURL, _ := url.Parse(previews.URL(lease))
				var route browserorigin.Binding
				if browserMode {
					origins, err := browserorigin.New("https://cm.test:30443", "31400-31401", &browserPreviewStore{})
					if err != nil {
						t.Fatal(err)
					}
					service.SetBrowserOrigins(origins)
					route, err = origins.Bind(context.Background(), 42, browserorigin.Preview, browserPreviewTargetKey(lease.Target))
					if err != nil {
						t.Fatal(err)
					}
					publicURL, _ = url.Parse(origins.URL(route))
				}
				// Ingress may forward HTTP; the configured public origin wins.
				request := httptest.NewRequest("GET", "http://"+publicURL.Host+"/", nil)
				if browserMode {
					request.AddCookie(&http.Cookie{Name: browserPreviewCookie(route.Port), Value: lease.ID})
					request.Header.Set("Sec-Fetch-Site", "same-origin")
					request.Header.Set("Origin", service.browserOrigins.URL(route))
				}
				recorder := httptest.NewRecorder()
				service.ServePreview(recorder, request)
				want := strings.ReplaceAll(tc.want, "{origin}", publicURL.Scheme+"://"+publicURL.Host)
				if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != want {
					t.Fatalf("redirect = %d %q, want %d %q", recorder.Code, recorder.Header().Get("Location"), http.StatusFound, want)
				}
			})
		}
	}
}

func TestWorkbenchPreviewStreamsBodyAndWebSocketWithScopedControlRoute(t *testing.T) {
	t.Run("generation change", func(t *testing.T) {
		testWorkbenchPreviewStreamsBodyAndWebSocket(t, false)
	})
	t.Run("expiry", func(t *testing.T) {
		testWorkbenchPreviewStreamsBodyAndWebSocket(t, true)
	})
}

func testWorkbenchPreviewStreamsBodyAndWebSocket(t *testing.T, expire bool) {
	t.Helper()
	var routes []workbenchPreviewRoute
	var mu sync.Mutex
	upstreamClosed := make(chan error, 1)
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/workbench/preview" || r.Header.Get("X-ClawManager-Control-Token") != "test-control" {
			t.Error("untrusted preview transport")
			w.WriteHeader(403)
			return
		}
		encoded, err := base64.RawURLEncoding.DecodeString(r.Header.Get(workbenchPreviewHeader))
		var route workbenchPreviewRoute
		if err != nil || json.Unmarshal(encoded, &route) != nil {
			t.Error("missing preview route")
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		routes = append(routes, route)
		mu.Unlock()
		if r.Header.Get("X-ClawManager-Other") != "" || r.Header.Get("Referer") != "" {
			t.Error("CM metadata leaked")
		}
		if route.Path == "/ws" {
			upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			for {
				kind, payload, err := conn.ReadMessage()
				if err != nil {
					upstreamClosed <- err
					return
				}
				if err = conn.WriteMessage(kind, payload); err != nil {
					return
				}
			}
			return
		}
		w.Header().Set("Location", "http://localhost:4321/login?from=app")
		w.Header().Set("Set-Cookie", "app=1; Domain=localhost; HttpOnly")
		w.Header().Set("X-ClawManager-Internal", "secret")
		w.WriteHeader(201)
		_, _ = io.Copy(w, r.Body)
	}))
	defer runtime.Close()
	instance := &models.Instance{ID: 42, UserID: 7, Status: "running", RuntimeGeneration: 3}
	binding := &models.InstanceRuntimeBinding{InstanceID: 42, RuntimePodID: 9, Generation: 3, GatewayID: "gw-42-3", GatewayPort: 3000, WorkspacePath: "/workspace"}
	ttl := time.Hour
	if expire {
		ttl = 2 * time.Second
	}
	previews, _ := workbench.NewPreviews("http://p-{token}.localhost", &previewTestStore{values: map[string]workbench.PreviewLease{}}, ttl)
	var instanceMu sync.RWMutex
	service := NewInstanceWorkbenchService(workbenchTestInstances{instance: instance, mu: &instanceMu}, workbenchTestBindings{binding}, workbenchTestPods{&models.RuntimePod{ID: 9, AgentEndpoint: &runtime.URL}}, NewRuntimeAgentClientWithHTTPClient("test-control", runtime.Client()).(WorkbenchAgent), previews)
	service.previewRecheckInterval = 10 * time.Millisecond
	lease, err := previews.Create(context.Background(), workbench.PreviewScope{InstanceID: 42, UserID: 7, Generation: 3, GatewayID: "gw-42-3"}, "localhost:4321")
	if err != nil {
		t.Fatal(err)
	}
	host, _ := url.Parse(previews.URL(lease))
	body := []byte{0, 255, 1, 2, 3}
	request := httptest.NewRequest("POST", "http://"+host.Host+"/a%2Fb?q=%2f&x=1&x=2", bytes.NewReader(body))
	request.Header.Set("X-ClawManager-Other", "secret")
	request.Header.Set("Referer", "http://cm.example/private")
	recorder := httptest.NewRecorder()
	service.ServePreview(recorder, request)
	if recorder.Code != 201 || !bytes.Equal(recorder.Body.Bytes(), body) {
		t.Fatalf("stream corrupted: %d %q", recorder.Code, recorder.Body.Bytes())
	}
	if recorder.Header().Get("Location") != "http://"+host.Host+"/login?from=app" || strings.Contains(recorder.Header().Get("Set-Cookie"), "Domain=") || recorder.Header().Get("X-ClawManager-Internal") != "" {
		t.Fatalf("origin boundary incorrect: %+v", recorder.Header())
	}
	mu.Lock()
	route := routes[0]
	mu.Unlock()
	if route.InstanceID != 42 || route.Generation != 3 || route.Port != 4321 || route.RawPath != "/a%2Fb" || route.RawQuery != "q=%2f&x=1&x=2" {
		t.Fatalf("route corrupted: %+v", route)
	}
	proxy := httptest.NewServer(http.HandlerFunc(service.ServePreview))
	defer proxy.Close()
	header := http.Header{"Host": []string{host.Host}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(proxy.URL, "http")+"/ws", header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err = conn.WriteMessage(websocket.BinaryMessage, body); err != nil {
		t.Fatal(err)
	}
	_, output, err := conn.ReadMessage()
	if err != nil || !bytes.Equal(output, body) {
		t.Fatalf("websocket corrupted: %q %v", output, err)
	}
	if !expire {
		instanceMu.Lock()
		instance.RuntimeGeneration = 4
		instanceMu.Unlock()
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, _, err = conn.ReadMessage()
	requirePreviewWebSocketClosed(t, err)
	select {
	case err := <-upstreamClosed:
		requirePreviewWebSocketClosed(t, err)
	case <-time.After(time.Second):
		t.Fatal("runtime upstream WebSocket remained open")
	}
	denied := httptest.NewRecorder()
	service.ServePreview(denied, httptest.NewRequest("GET", "http://"+host.Host+"/", nil))
	if denied.Code != 404 {
		t.Fatalf("stale preview accepted: %d", denied.Code)
	}
}

func requirePreviewWebSocketClosed(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("stale WebSocket remained open")
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		t.Fatalf("timed out waiting for WebSocket closure: %v", err)
	}
	var closeError *websocket.CloseError
	if !errors.Is(err, io.EOF) && !errors.As(err, &closeError) {
		t.Fatalf("expected EOF or WebSocket close error, got %T: %v", err, err)
	}
}

func TestWorkbenchPreviewCertificateEndpointsDoNotContactApp(t *testing.T) {
	calls := 0
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; http.Error(w, "application unavailable", 502) }))
	defer runtime.Close()
	instance := &models.Instance{ID: 42, UserID: 7, Status: "running", RuntimeGeneration: 3}
	binding := &models.InstanceRuntimeBinding{InstanceID: 42, RuntimePodID: 9, Generation: 3, GatewayID: "gw-42-3", GatewayPort: 3000, WorkspacePath: "/workspace"}
	previews, _ := workbench.NewPreviews("https://p-{token}.preview.example:8443", &previewTestStore{values: map[string]workbench.PreviewLease{}}, time.Hour)
	service := NewInstanceWorkbenchService(workbenchTestInstances{instance: instance}, workbenchTestBindings{binding}, workbenchTestPods{&models.RuntimePod{ID: 9, AgentEndpoint: &runtime.URL}}, NewRuntimeAgentClientWithHTTPClient("test-control", runtime.Client()).(WorkbenchAgent), previews)
	lease, err := previews.Create(context.Background(), workbench.PreviewScope{InstanceID: 42, UserID: 7, Generation: 3, GatewayID: "gw-42-3"}, "http://localhost:4321")
	if err != nil {
		t.Fatal(err)
	}
	origin := previews.URL(lease)
	for _, tc := range []struct {
		path   string
		status int
	}{{"/__clawmanager_cert_check", 204}, {"/__clawmanager_cert_trust", 200}} {
		for _, method := range []string{"GET", "HEAD"} {
			rec := httptest.NewRecorder()
			service.ServePreview(rec, httptest.NewRequest(method, origin+strings.TrimPrefix(tc.path, "/"), nil))
			if rec.Code != tc.status || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("certificate endpoint = %d; want %d without caching", rec.Code, tc.status)
			}
			if rec.Header().Get("Access-Control-Allow-Origin") != "*" || rec.Header().Get("Cross-Origin-Resource-Policy") != "cross-origin" {
				t.Fatal("TLS probe must be readable across origins")
			}
			if method == "HEAD" && rec.Body.Len() != 0 {
				t.Fatal("HEAD confirmation must not write HTML")
			}
			if method == "GET" && tc.status == 200 && !strings.Contains(rec.Body.String(), "Close this tab and return to ClawManager.") {
				t.Fatal("confirmation page lacks close-tab instructions")
			}
		}
	}
	if calls != 0 {
		t.Fatal("certificate confirmation must not depend on the app/runtime being available")
	}
	rec := httptest.NewRecorder()
	service.ServePreview(rec, httptest.NewRequest("GET", origin, nil))
	if rec.Code != 502 {
		t.Fatal("actual app failure must remain a service error")
	}
	instance.RuntimeGeneration++
	rec = httptest.NewRecorder()
	service.ServePreview(rec, httptest.NewRequest("GET", origin+"__clawmanager_cert_check", nil))
	if rec.Code != 404 {
		t.Fatal("stale capability must remain fenced")
	}
	if calls != 1 {
		t.Fatal("stale certificate probe reached app")
	}
}
