package services

import (
	"clawreef/internal/browserorigin"
	"clawreef/internal/models"
	"clawreef/internal/workbench"
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type browserPreviewStore struct{ binding browserorigin.Binding }

func (s *browserPreviewStore) Bind(_ context.Context, id int, kind, target string, first, last int) (browserorigin.Binding, error) {
	s.binding = browserorigin.Binding{Port: first, InstanceID: id, Kind: kind, Target: target}
	return s.binding, nil
}
func (s *browserPreviewStore) ByPort(_ context.Context, port int) (browserorigin.Binding, error) {
	if s.binding.Port != port {
		return browserorigin.Binding{}, browserorigin.ErrNotFound
	}
	return s.binding, nil
}
func chromePreviewFixture(t *testing.T) (*InstanceWorkbenchService, workbench.PreviewLease, browserorigin.Binding, *models.Instance, *[]string, *sync.RWMutex) {
	t.Helper()
	seen := &[]string{}
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.Header.Get("Cookie"))
		if websocket.IsWebSocketUpgrade(r) {
			c, e := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, http.Header{"Set-Cookie": []string{"instance_access_42=evil; Path=/"}})
			if e != nil {
				return
			}
			defer c.Close()
			for {
				kind, data, e := c.ReadMessage()
				if e != nil {
					return
				}
				if c.WriteMessage(kind, data) != nil {
					return
				}
			}
		}
		w.Header().Add("Set-Cookie", "app=1; Domain=localhost; HttpOnly")
		w.Header().Add("Set-Cookie", "instance_access_42=evil; Path=/")
		w.Header().Add("Set-Cookie", "cm_preview_31400=evil; Path=/")
		_, _ = io.WriteString(w, "actual service")
	}))
	t.Cleanup(runtime.Close)
	instance := &models.Instance{ID: 42, UserID: 7, Status: "running", RuntimeGeneration: 3}
	binding := &models.InstanceRuntimeBinding{InstanceID: 42, RuntimePodID: 9, Generation: 3, GatewayID: "gw-42-3", GatewayPort: 3000, WorkspacePath: "/workspace"}
	previews, _ := workbench.NewPreviews("https://p-{token}.legacy.test", &previewTestStore{values: map[string]workbench.PreviewLease{}}, time.Hour)
	var mu sync.RWMutex
	svc := NewInstanceWorkbenchService(workbenchTestInstances{instance: instance, mu: &mu}, workbenchTestBindings{binding}, workbenchTestPods{&models.RuntimePod{ID: 9, AgentEndpoint: &runtime.URL}}, NewRuntimeAgentClientWithHTTPClient("test-control", runtime.Client()).(WorkbenchAgent), previews)
	origins, _ := browserorigin.New("https://cm.test:30443", "31400-31401", &browserPreviewStore{})
	svc.SetBrowserOrigins(origins)
	route, err := origins.Bind(context.Background(), 42, browserorigin.Preview, "http://localhost:4321/")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := previews.Create(context.Background(), workbench.PreviewScope{InstanceID: 42, UserID: 7, Generation: 3, GatewayID: "gw-42-3"}, "http://localhost:4321/a%2Fb?q=1#part")
	if err != nil {
		t.Fatal(err)
	}
	return svc, lease, route, instance, seen, &mu
}
func TestBrowserPreviewBootstrapAndCookieBoundary(t *testing.T) {
	s, lease, route, _, seen, _ := chromePreviewFixture(t)
	boot := s.browserPreviewURL(route, lease)
	r := httptest.NewRequest("GET", boot, nil)
	r.Header.Set("Sec-Fetch-Dest", "iframe")
	r.Header.Set("Sec-Fetch-Site", "same-site")
	w := httptest.NewRecorder()
	s.ServePreview(w, r)
	if w.Code != http.StatusOK || w.Header().Get("Location") != "" || !strings.Contains(w.Body.String(), `location.replace(`) || !strings.Contains(w.Body.String(), `"/a%2Fb?q=1#part"`) || strings.Contains(w.Body.String(), lease.ID) || len(*seen) != 0 {
		t.Fatalf("bootstrap %d %s calls%d", w.Code, w.Header().Get("Location"), len(*seen))
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].Name != "cm_preview_31400" || !cookies[0].Partitioned {
		t.Fatalf("bad bootstrap cookie: %+v", cookies)
	}
	app := httptest.NewRequest("GET", "https://cm.test:31400/a%2Fb?q=1", nil)
	app.Header.Set("Sec-Fetch-Site", "same-origin")
	app.Header.Set("Origin", "https://cm.test:31400")
	app.AddCookie(cookies[0])
	app.Header.Add("Cookie", "instance_access_118=platform; clawmanager_iei_session=manager; cm_hermes_desktop_25=desktop; share_link_access_code=share; ordinary=value")
	got := httptest.NewRecorder()
	s.ServePreview(got, app)
	if got.Code != 200 || got.Body.String() != "actual service" || len(*seen) != 1 || (*seen)[0] != "ordinary=value" {
		t.Fatalf("app/cookies %d %q %+v", got.Code, got.Body.String(), *seen)
	}
	if cs := got.Result().Cookies(); len(cs) != 1 || cs[0].Name != "app" || cs[0].Domain != "" {
		t.Fatalf("platform Set-Cookie leak: %+v", cs)
	}
}
func TestBrowserPreviewDeniesGuessablePortAndForeignSource(t *testing.T) {
	s, lease, route, instance, seen, _ := chromePreviewFixture(t)
	boot := s.browserPreviewURL(route, lease)
	for _, tc := range []struct{ name, url, site, origin, dest, cookie string }{
		{"port alone", "https://cm.test:31400/", "same-origin", "", "iframe", ""},
		{"wrong port", strings.Replace(boot, ":31400", ":31401", 1), "same-site", "", "iframe", ""},
		{"foreign bootstrap", boot, "cross-site", "https://evil.test", "iframe", ""},
		{"foreign cookie request", "https://cm.test:31400/api", "same-site", "https://cm.test:31401", "empty", "cm_preview_31400=" + lease.ID},
		{"same-site cookie iframe", "https://cm.test:31400/", "same-site", "", "iframe", "cm_preview_31400=" + lease.ID},
		{"missing origin image", "https://cm.test:31400/", "same-site", "", "image", "cm_preview_31400=" + lease.ID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", tc.url, nil)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Dest", tc.dest)
			r.Header.Set("Cookie", tc.cookie)
			w := httptest.NewRecorder()
			s.ServePreview(w, r)
			if w.Code < 400 {
				t.Fatalf("accepted %s: %d", tc.name, w.Code)
			}
		})
	}
	instance.RuntimeGeneration = 4
	r := httptest.NewRequest("GET", boot, nil)
	r.Header.Set("Sec-Fetch-Dest", "iframe")
	r.Header.Set("Sec-Fetch-Site", "same-site")
	w := httptest.NewRecorder()
	s.ServePreview(w, r)
	if w.Code != 404 || len(*seen) != 0 {
		t.Fatal("stale bootstrap contacted app")
	}
}
func TestBrowserPreviewTopLevelOpensTrustedViewer(t *testing.T) {
	s, lease, route, _, seen, _ := chromePreviewFixture(t)
	boot := s.browserPreviewURL(route, lease)
	r := httptest.NewRequest("GET", boot, nil)
	r.Header.Set("Sec-Fetch-Dest", "document")
	r.Header.Set("Sec-Fetch-Site", "none")
	w := httptest.NewRecorder()
	s.ServePreview(w, r)
	redirect, err := url.Parse(w.Header().Get("Location"))
	if err != nil || w.Code != http.StatusSeeOther || redirect.Scheme != "https" || redirect.Host != "cm.test:30443" || redirect.Path != "/__clawmanager_browser/view" || redirect.Fragment == "" || len(*seen) != 0 {
		t.Fatalf("unsafe top level: %d %s", w.Code, w.Header().Get("Location"))
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("top-level bootstrap set ordinary profile cookie")
	}
	viewer := httptest.NewRecorder()
	s.ServeBrowserViewer(viewer, httptest.NewRequest("GET", "https://cm.test:30443/__clawmanager_browser/view", nil))
	body := viewer.Body.String()
	policy := viewer.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "frame-src https://cm.test:*;") {
		t.Fatalf("viewer cannot load application iframe: %s", policy)
	}
	if output := os.Getenv("CM_BROWSER_VIEWER_HEADERS_OUTPUT"); output != "" {
		data, _ := json.Marshal(viewer.Header())
		if err := os.WriteFile(output, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if output := os.Getenv("CM_BROWSER_VIEWER_HTML_OUTPUT"); output != "" {
		if err := os.WriteFile(output, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if viewer.Code != 200 || !strings.Contains(body, "credentialless") || strings.Contains(body, "allow-popups") || !strings.Contains(viewer.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("unsafe viewer")
	}
}

func TestBrowserPreviewWebSocketCookieAndGenerationFence(t *testing.T) {
	s, lease, _, instance, seen, mu := chromePreviewFixture(t)
	s.previewRecheckInterval = 10 * time.Millisecond
	proxy := httptest.NewServer(http.HandlerFunc(s.ServePreview))
	defer proxy.Close()
	header := http.Header{"Host": []string{"cm.test:31400"}, "Origin": []string{"https://cm.test:31400"}, "Sec-Fetch-Site": []string{"same-origin"}, "Cookie": []string{"cm_preview_31400=" + lease.ID + "; instance_access_42=platform; ordinary=ws"}}
	conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(proxy.URL, "http")+"/ws", header)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if response.Header.Get("Set-Cookie") != "" {
		t.Fatal("platform cookie leaked through101")
	}
	if err = conn.WriteMessage(websocket.BinaryMessage, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	_, data, err := conn.ReadMessage()
	if err != nil || len(data) != 3 {
		t.Fatal("WS failed", err)
	}
	if len(*seen) != 1 || (*seen)[0] != "ordinary=ws" {
		t.Fatal("websocket leaked platform cookies", *seen)
	}
	mu.Lock()
	instance.RuntimeGeneration = 4
	mu.Unlock()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, _, err = conn.ReadMessage()
	requirePreviewWebSocketClosed(t, err)
}

func TestBrowserPreviewNonDocumentBootstrapKeepsRedirect(t *testing.T) {
	for _, method := range []string{"GET", "HEAD"} {
		t.Run(method, func(t *testing.T) {
			s, lease, route, _, seen, _ := chromePreviewFixture(t)
			r := httptest.NewRequest(method, s.browserPreviewURL(route, lease), nil)
			r.Header.Set("Sec-Fetch-Site", "same-site")
			w := httptest.NewRecorder()
			s.ServePreview(w, r)
			if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/a%2Fb?q=1#part" || len(*seen) != 0 {
				t.Fatalf("non-document bootstrap changed: status=%d location=%q", w.Code, w.Header().Get("Location"))
			}
		})
	}
}
