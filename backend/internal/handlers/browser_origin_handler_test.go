package handlers

import (
	"clawreef/internal/models"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"clawreef/internal/browserorigin"
	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
)

type browserRouteStore struct{ binding browserorigin.Binding }

func (s *browserRouteStore) Bind(context.Context, int, string, string, int, int) (browserorigin.Binding, error) {
	return s.binding, nil
}
func (s *browserRouteStore) ByPort(_ context.Context, p int) (browserorigin.Binding, error) {
	if p != s.binding.Port {
		return browserorigin.Binding{}, browserorigin.ErrNotFound
	}
	return s.binding, nil
}
func nativeRouteFixture(t *testing.T, runtimeTypes ...string) (*gin.Engine, *InstanceHandler, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	runtimeType := services.RuntimeTypeOpenCode
	if len(runtimeTypes) > 0 {
		runtimeType = runtimeTypes[0]
	}
	reg, e := browserorigin.New("https://10.130.81.54:30443", "31400-31527", &browserRouteStore{browserorigin.Binding{Port: 31400, InstanceID: 118, Kind: browserorigin.Native, Target: runtimeType}})
	if e != nil {
		t.Fatal(e)
	}
	access := services.NewInstanceAccessService()
	t.Cleanup(access.Stop)
	h := &InstanceHandler{accessService: access, proxyService: services.NewInstanceProxyService(access)}
	h.SetBrowserOrigins(reg)
	token, e := access.GenerateToken(1, 118, runtimeType, reg.URL(browserorigin.Binding{Port: 31400}), "", 3000, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	router := gin.New()
	router.Use(BrowserOriginRoutes(reg, h, nil))
	router.Any("/*path", func(c *gin.Context) { c.Status(202) })
	return router, h, token.Token
}
func TestBrowserNativeBootstrapAndRenewal(t *testing.T) {
	router, _, token := nativeRouteFixture(t)
	for _, tc := range []struct {
		name, path, origin, site, cookie string
		want                             int
	}{
		{"bootstrap", "/?token=" + token, "", "same-site", "", 307},
		{"renew", "/__clawmanager_access_refresh?token=" + token, "https://10.130.81.54:30443", "same-site", "", 204},
		{"native-cookie-renew", "/__clawmanager_access_refresh", "https://10.130.81.54:31400", "same-origin", token, 204},
		{"cross-origin", "/__clawmanager_access_refresh", "https://10.130.81.54:31401", "same-site", token, 403},
		{"missing-origin", "/__clawmanager_access_refresh", "", "same-site", token, 403},
		{"invalid-bootstrap-cookie-bypass", "/?token=invalid", "", "same-site", token, 403},
		{"unknown-port", "https://10.130.81.54:31402/", "", "same-site", "", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.path
			if !strings.HasPrefix(path, "https:") {
				path = "https://10.130.81.54:31400" + path
			}
			r := httptest.NewRequest("GET", path, nil)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			if strings.Contains(tc.path, dedicatedAccessRefreshPath) {
				r.Header.Set("Sec-Fetch-Dest", "iframe")
			}
			if tc.cookie != "" {
				r.AddCookie(&http.Cookie{Name: "instance_access_118", Value: tc.cookie})
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d want %d body=%s", w.Code, tc.want, w.Body.String())
			}
			if tc.name == "renew" && w.Header().Get("Access-Control-Allow-Origin") != "https://10.130.81.54:30443" {
				t.Fatal(w.Header())
			}
		})
	}
}
func TestBrowserManagerBoundary(t *testing.T) {
	router, _, _ := nativeRouteFixture(t)
	for _, tc := range []struct {
		origin, site string
		want         int
	}{{"https://10.130.81.54:31400", "same-site", 403}, {"", "same-site", 403}, {"https://10.130.81.54:30443", "same-origin", 202}, {"", "", 202}} {
		r := httptest.NewRequest("POST", "https://10.130.81.54:30443/api/v1/test", nil)
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-Fetch-Site", tc.site)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Fatalf("%+v status=%d", tc, w.Code)
		}
	}
}

func TestBrowserCertificateEndpoints(t *testing.T) {
	router, _, _ := nativeRouteFixture(t)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, path := range []string{"/__clawmanager_cert_check", "/__clawmanager_cert_trust"} {
			r := httptest.NewRequest(method, "https://10.130.81.54:31400"+path, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			want := 204
			if strings.HasSuffix(path, "trust") {
				want = 200
			}
			if w.Code != want {
				t.Fatalf("%s %s status=%d", method, path, w.Code)
			}
		}
	}
}
func TestBrowserAccessMetadataResponses(t *testing.T) {
	for _, kind := range []string{services.RuntimeTypeOpenCode, services.RuntimeTypeDeepSeekHarness} {
		for _, entry := range []string{"normal", "iei", "share"} {
			t.Run(kind+"/"+entry, func(t *testing.T) {
				owner := "owner@example.com"
				instance := &models.Instance{ID: 118, UserID: 10, Owner: &owner, Type: kind, RuntimeType: "gateway", InstanceMode: "lite", Status: "running"}
				fake := &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{118: instance}}
				access := services.NewInstanceAccessService()
				defer access.Stop()
				h := &InstanceHandler{instanceService: fake, accessService: access, proxyService: services.NewInstanceProxyService(access)}
				reg, e := browserorigin.New("https://10.130.81.54:30443", "31400-31527", &browserRouteStore{browserorigin.Binding{Port: 31400, InstanceID: 118, Kind: browserorigin.Native, Target: kind}})
				if e != nil {
					t.Fatal(e)
				}
				h.SetBrowserOrigins(reg)
				router := gin.New()
				var request *http.Request
				switch entry {
				case "normal":
					router.Use(func(c *gin.Context) { c.Set("userID", 10); c.Set("userRole", "user") })
					router.POST("/instances/:id/access", h.GenerateAccessToken)
					request = httptest.NewRequest("POST", "/instances/118/access", nil)
				case "share":
					h.externalAccessService = &fakeSharedExternalAccessService{access: &models.InstanceExternalAccess{InstanceID: 118, Enabled: true, AuthMode: services.ExternalAccessModeShareLink, WorkspaceAccess: services.ExternalWorkspaceAccessWrite}}
					router.GET("/shared/:code/session", h.GetSharedInstanceSession)
					request = httptest.NewRequest("GET", "/shared/sl_test/session", nil)
				case "iei":
					cfg := testIEIHandlerConfig()
					sso, e := services.NewIEISSOService(cfg)
					if e != nil {
						t.Fatal(e)
					}
					iei := NewIEISystemHandler(cfg, sso, &fakeIEIInstanceService{fakeWorkspaceHandlerInstanceService: fake}, h)
					router.POST("/api/v1/ieisystem/session", iei.ExchangeSession)
					router.POST("/instances/:id/access", iei.GenerateInstanceAccess)
					cookie := exchangeIEITestSession(t, router, cfg, owner)
					request = httptest.NewRequest("POST", "/instances/118/access", nil)
					request.AddCookie(cookie)
				}
				w := httptest.NewRecorder()
				router.ServeHTTP(w, request)
				if w.Code != 200 {
					t.Fatalf("status=%d %s", w.Code, w.Body.String())
				}
				var payload struct {
					Data struct {
						Mode    string `json:"browser_origin_mode"`
						Manager string `json:"manager_origin"`
						URL     string `json:"access_url"`
					}
				}
				if e := json.Unmarshal(w.Body.Bytes(), &payload); e != nil {
					t.Fatal(e)
				}
				if payload.Data.Mode != "chrome-single-host" || payload.Data.Manager != reg.ManagerOrigin() {
					t.Fatalf("metadata missing: %s", w.Body.String())
				}
				u, e := url.Parse(payload.Data.URL)
				if e != nil || u.Host != "10.130.81.54:31400" || u.Query().Get("token") == "" {
					t.Fatalf("bootstrap URL=%s err=%v", payload.Data.URL, e)
				}
			})
		}
	}
}

func TestBrowserAccessCapacityResponses(t *testing.T) {
	for _, successfulBinds := range []int{0, 1} {
		for _, kind := range []string{services.RuntimeTypeOpenCode, services.RuntimeTypeDeepSeekHarness} {
			for _, entry := range []string{"normal", "iei", "share"} {
				stage := "initial URL/"
				if successfulBinds == 1 {
					stage = "token URL/"
				}
				t.Run(stage+kind+"/"+entry, func(t *testing.T) {
					owner := "owner@example.com"
					instance := &models.Instance{ID: 118, UserID: 10, Owner: &owner, Type: kind, RuntimeType: "gateway", InstanceMode: "lite", Status: "running"}
					fake := &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{118: instance}}
					access := services.NewInstanceAccessService()
					defer access.Stop()
					h := &InstanceHandler{instanceService: fake, accessService: access, proxyService: services.NewInstanceProxyService(access)}
					reg, e := browserorigin.New("https://10.130.81.54:30443", "31400-31527", &fullBrowserOriginStore{successfulBinds: successfulBinds})
					if e != nil {
						t.Fatal(e)
					}
					h.SetBrowserOrigins(reg)
					router := gin.New()
					var request *http.Request
					switch entry {
					case "normal":
						router.Use(func(c *gin.Context) { c.Set("userID", 10); c.Set("userRole", "user") })
						router.POST("/instances/:id/access", h.GenerateAccessToken)
						request = httptest.NewRequest("POST", "/instances/118/access", nil)
					case "share":
						h.externalAccessService = &fakeSharedExternalAccessService{access: &models.InstanceExternalAccess{InstanceID: 118, Enabled: true, AuthMode: services.ExternalAccessModeShareLink, WorkspaceAccess: services.ExternalWorkspaceAccessWrite}}
						router.GET("/shared/:code/session", h.GetSharedInstanceSession)
						request = httptest.NewRequest("GET", "/shared/sl_test/session", nil)
					case "iei":
						cfg := testIEIHandlerConfig()
						sso, e := services.NewIEISSOService(cfg)
						if e != nil {
							t.Fatal(e)
						}
						iei := NewIEISystemHandler(cfg, sso, &fakeIEIInstanceService{fakeWorkspaceHandlerInstanceService: fake}, h)
						router.POST("/api/v1/ieisystem/session", iei.ExchangeSession)
						router.POST("/instances/:id/access", iei.GenerateInstanceAccess)
						cookie := exchangeIEITestSession(t, router, cfg, owner)
						request = httptest.NewRequest("POST", "/instances/118/access", nil)
						request.AddCookie(cookie)
					}
					w := httptest.NewRecorder()
					router.ServeHTTP(w, request)
					assertBrowserCapacityResponse(t, w)
				})
			}
		}
	}
}

func TestBrowserNativeHeaderRenewal(t *testing.T) {
	router, _, token := nativeRouteFixture(t)
	r := httptest.NewRequest("GET", "https://10.130.81.54:31400/__clawmanager_access_refresh", nil)
	r.Header.Set("Origin", "https://10.130.81.54:30443")
	r.Header.Set("Sec-Fetch-Site", "same-site")
	r.Header.Set(dedicatedAccessRefreshHeader, token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Header().Get("Set-Cookie"), "instance_access_118=") {
		t.Fatal("renewal cookie missing")
	}
	if r.Header.Get(dedicatedAccessRefreshHeader) != "" {
		t.Fatal("renewal token retained in logged request")
	}
}
func TestBrowserNativePreflight(t *testing.T) {
	router, _, _ := nativeRouteFixture(t)
	for _, origin := range []string{"https://10.130.81.54:30443", "https://10.130.81.54:31401"} {
		r := httptest.NewRequest("OPTIONS", "https://10.130.81.54:31400/__clawmanager_access_refresh", nil)
		r.Header.Set("Origin", origin)
		r.Header.Set("Sec-Fetch-Site", "same-site")
		r.Header.Set("Access-Control-Request-Method", "GET")
		r.Header.Set("Access-Control-Request-Headers", dedicatedAccessRefreshHeader)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		want := 204
		if strings.HasSuffix(origin, "31401") {
			want = 403
		}
		if w.Code != want {
			t.Fatalf("origin=%s status=%d", origin, w.Code)
		}
		if want == 204 && w.Header().Get("Access-Control-Allow-Origin") != origin {
			t.Fatal(w.Header())
		}
	}
}
func TestBrowserWrongHostPortIsNotManager(t *testing.T) {
	router, _, _ := nativeRouteFixture(t)
	r := httptest.NewRequest("POST", "https://wrong.example:31400/api/v1/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatalf("wrong host got manager status %d", w.Code)
	}
}

func TestBrowserNativeRejectedRequestRedactsCredentials(t *testing.T) {
	router, _, token := nativeRouteFixture(t)
	r := httptest.NewRequest("GET", "https://10.130.81.54:31400/?token="+token+"&secret=private&keep=yes", nil)
	r.Header.Set("Origin", "https://10.130.81.54:31401")
	r.Header.Set("Sec-Fetch-Site", "same-site")
	r.Header.Set("Authorization", "Bearer secret")
	r.Header.Set(dedicatedAccessRefreshHeader, token)
	r.AddCookie(&http.Cookie{Name: "instance_access_118", Value: token})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	if r.URL.Query().Get("token") != "" || r.URL.Query().Get("secret") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get(dedicatedAccessRefreshHeader) != "" {
		t.Fatal("credentials remain in logged request")
	}
	if r.URL.Query().Get("keep") != "yes" {
		t.Fatal("safe query removed")
	}
}
func TestBrowserNativeRejectsMismatchedBootstrapAndCookie(t *testing.T) {
	router, h, valid := nativeRouteFixture(t)
	wrongType, e := h.accessService.GenerateToken(1, 118, services.RuntimeTypeDeepSeekHarness, "/", "", 3000, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	wrongInstance, e := h.accessService.GenerateToken(1, 119, services.RuntimeTypeOpenCode, "/", "", 3000, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	for _, token := range []string{wrongType.Token, wrongInstance.Token} {
		r := httptest.NewRequest("GET", "https://10.130.81.54:31400/?token="+token, nil)
		r.Header.Set("Sec-Fetch-Site", "same-site")
		r.AddCookie(&http.Cookie{Name: "instance_access_118", Value: valid})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("mismatched bootstrap accepted: %d", w.Code)
		}
	}
	r := httptest.NewRequest("GET", "https://10.130.81.54:31400/__clawmanager_access_refresh", nil)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.AddCookie(&http.Cookie{Name: "instance_access_118", Value: wrongType.Token})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("wrong runtime cookie accepted: %d", w.Code)
	}
}
func TestBrowserModeOffLeavesRoutingAlone(t *testing.T) {
	router := gin.New()
	router.Use(BrowserOriginRoutes(nil, nil, nil))
	router.Any("/*path", func(c *gin.Context) { c.Status(202) })
	r := httptest.NewRequest("GET", "https://10.130.81.54:31400/api/v1/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 202 {
		t.Fatal(w.Code)
	}
}

func TestBrowserOriginIngressNeverFallsThroughManager(t *testing.T) {
	reg, err := browserorigin.New("https://10.130.81.54:30443", "31400-31527", &browserRouteStore{})
	if err != nil {
		t.Fatal(err)
	}
	for _, registry := range []*browserorigin.Registry{nil, reg} {
		router := gin.New()
		router.Use(BrowserOriginRoutes(registry, nil, nil))
		router.GET("/api/v1/me", func(c *gin.Context) { c.Status(200) })
		r := httptest.NewRequest("GET", "https://10.130.81.54:30443/api/v1/me", nil)
		r.Header.Set("X-ClawManager-Browser-Port", "1")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatalf("browser ingress reached manager: enabled=%v status=%d", registry != nil, w.Code)
		}
	}
}

// A full permanent ledger returns the same cause for global and per-instance limits.
type fullBrowserOriginStore struct{ successfulBinds, calls int }

func (s *fullBrowserOriginStore) Bind(_ context.Context, id int, kind, target string, first, _ int) (browserorigin.Binding, error) {
	if s.calls < s.successfulBinds {
		s.calls++
		return browserorigin.Binding{Port: first, InstanceID: id, Kind: kind, Target: target}, nil
	}
	return browserorigin.Binding{}, browserorigin.ErrCapacity
}
func (*fullBrowserOriginStore) ByPort(context.Context, int) (browserorigin.Binding, error) {
	return browserorigin.Binding{}, browserorigin.ErrNotFound
}
func assertBrowserCapacityResponse(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	var payload struct {
		Success bool   `json:"success"`
		Code    string `json:"error_code"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusConflict || payload.Success || payload.Code != "browser_origin_capacity_exhausted" ||
		!strings.Contains(payload.Error, "permanent") || !strings.Contains(payload.Error, "administrator") || !strings.Contains(payload.Error, "target limit") ||
		strings.Contains(strings.ToLower(payload.Error), "retry") || strings.Contains(strings.ToLower(payload.Error), "delete") {
		t.Fatalf("capacity response = %d %s", w.Code, w.Body.String())
	}
}

func TestBrowserNativeBootstrapStartsSameOriginNavigation(t *testing.T) {
	for _, runtimeType := range []string{services.RuntimeTypeOpenCode, services.RuntimeTypeDeepSeekHarness} {
		for _, dest := range []string{"iframe", "document"} {
			t.Run(runtimeType+"/"+dest, func(t *testing.T) {
				router, _, token := nativeRouteFixture(t, runtimeType)
				r := httptest.NewRequest("GET", "https://10.130.81.54:31400/app?token="+token+"&keep=yes", nil)
				r.Header.Set("Sec-Fetch-Site", "same-site")
				r.Header.Set("Sec-Fetch-Dest", dest)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				if w.Code != http.StatusOK || w.Header().Get("Location") != "" || !strings.Contains(w.Body.String(), `location.replace(`) || !strings.Contains(w.Body.String(), `"/app?keep=yes"`) || strings.Contains(w.Body.String(), token) {
					t.Fatalf("bootstrap must start a clean navigation from trusted HTML: status=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
				}
				cookies := w.Result().Cookies()
				if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || !cookies[0].Partitioned {
					t.Fatalf("bad bootstrap cookies: %+v", cookies)
				}
				// Redirects inherit same-site metadata; cookie-only requests with it
				// must remain forbidden even after successful bootstrap.
				for _, site := range []string{"same-site", "same-origin"} {
					next := httptest.NewRequest("GET", "https://10.130.81.54:31400/__clawmanager_access_refresh", nil)
					next.Header.Set("Sec-Fetch-Site", site)
					next.AddCookie(cookies[0])
					got := httptest.NewRecorder()
					router.ServeHTTP(got, next)
					want := http.StatusNoContent
					if site == "same-site" {
						want = http.StatusForbidden
					}
					if got.Code != want {
						t.Fatalf("cookie-only %s status=%d want=%d", site, got.Code, want)
					}
				}
			})
		}
	}
}

func TestBrowserNativeBootstrapOverridesGinNoRouteStatus(t *testing.T) {
	for _, runtimeType := range []string{services.RuntimeTypeOpenCode, services.RuntimeTypeDeepSeekHarness} {
		t.Run(runtimeType, func(t *testing.T) {
			registered, _, token := nativeRouteFixture(t, runtimeType)
			// Production application-port paths run in middleware without matching
			// a manager route. Gin selects its 404 handler before this middleware.
			router := gin.New()
			router.Use(registered.Handlers...)
			router.GET("/api/v1/manager-route", func(c *gin.Context) { c.Status(202) })
			r := httptest.NewRequest("GET", "https://10.130.81.54:31400/app?token="+token, nil)
			r.Header.Set("Sec-Fetch-Site", "same-site")
			r.Header.Set("Sec-Fetch-Dest", "iframe")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != http.StatusOK {
				t.Fatalf("bootstrap inherited Gin NoRoute status: got %d, want 200", w.Code)
			}
			if !strings.Contains(w.Body.String(), "location.replace(") || !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'sha256-") {
				t.Fatalf("bootstrap document or CSP missing: %v %s", w.Header(), w.Body.String())
			}
			cookies := w.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Value != token || !cookies[0].Secure || !cookies[0].HttpOnly || !cookies[0].Partitioned {
				t.Fatalf("bootstrap cookie missing: %+v", cookies)
			}
		})
	}
}

func TestNativeWebSocketWithoutMetadataStillRequiresInstanceCapability(t *testing.T) {
	router, _, _ := nativeRouteFixture(t, services.RuntimeTypeDeepSeekHarness)
	for _, cookie := range []string{"", "invalid-instance-capability"} {
		r := httptest.NewRequest("GET", "https://10.130.81.54:31400/api/remote.mux", nil)
		r.Header.Set("Origin", "https://10.130.81.54:31400")
		r.Header.Set("Connection", "Upgrade")
		r.Header.Set("Upgrade", "websocket")
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "instance_access_118", Value: cookie})
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "Access token required") {
			t.Fatalf("socket must reach the unchanged capability check: HTTP %d", w.Code)
		}
	}
}
