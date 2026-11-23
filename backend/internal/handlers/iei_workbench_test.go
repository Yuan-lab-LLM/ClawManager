package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
)

func TestIEIWorkbenchOwnerSessionAndExplicitAction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := testIEIHandlerConfig()
	sso, err := services.NewIEISSOService(cfg)
	if err != nil {
		t.Fatal(err)
	}
	owner, other := "owner@example.com", "other@example.com"
	// Both portal owners share the same CM creator; CM user ownership alone
	// must never authorize the second instance.
	instances := &fakeIEIInstanceService{fakeWorkspaceHandlerInstanceService: &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{
		1: {ID: 1, UserID: 10, Owner: &owner, Type: "hermes", InstanceMode: "lite", Status: "running"},
		2: {ID: 2, UserID: 10, Owner: &other, Type: "hermes", InstanceMode: "lite", Status: "running"},
	}}}
	lifecycle := &fakeIEILifecycleService{}
	h := NewIEISystemHandler(cfg, sso, instances, nil, lifecycle)
	service := &workbenchHandlerService{}
	router := gin.New()
	router.POST("/api/v1/ieisystem/session", h.ExchangeSession)
	h.RegisterWorkbenchRoutes(router.Group("/portal"), service)
	cookie := exchangeIEITestSession(t, router, cfg, owner)
	for _, tc := range []struct {
		name, method, path, action, origin, site string
		auth                                     bool
		status                                   int
	}{
		{"no cookie", "GET", "/1/workbench/capabilities", "", "", "", false, 401},
		{"wrong owner same creator", "GET", "/2/workbench/capabilities", "", "", "", true, 404},
		{"valid capabilities", "GET", "/1/workbench/capabilities", "", "", "", true, 200},
		{"generation required", "GET", "/1/workbench/terminals", "", "", "", true, 409},
		{"valid list", "GET", "/1/workbench/terminals?generation=3", "", "", "", true, 200},
		{"missing explicit action", "POST", "/1/workbench/terminals?generation=3", "", "", "", true, 403},
		{"cross-site", "POST", "/1/workbench/terminals?generation=3", "1", "", "cross-site", true, 403},
		{"wrong origin", "POST", "/1/workbench/terminals?generation=3", "1", "https://evil.example", "", true, 403},
		{"valid create", "POST", "/1/workbench/terminals?generation=3", "1", "http://example.com", "", true, 200},
		{"route session pinned", "POST", "/1/workbench/terminals/owned/input?generation=3", "1", "", "", true, 200},
		{"close protected", "DELETE", "/1/workbench/terminals/owned?generation=3", "", "", "", true, 403},
		{"preview protected", "POST", "/1/workbench/previews?generation=3", "", "", "", true, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := service.calls
			r := httptest.NewRequest(tc.method, "/portal/instances"+tc.path, strings.NewReader(`{"session":"injected","input":"hello"}`))
			r.Header.Set("X-Owner-Action", tc.action)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			// A management identity must not bypass the owner cookie.
			r.Header.Set("Authorization", "Bearer irrelevant")
			if tc.auth {
				r.AddCookie(cookie)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("%d want %d: %s", w.Code, tc.status, w.Body.String())
			}
			if tc.status != 200 && service.calls != before {
				t.Fatal("denied request reached Runtime")
			}
			if tc.name == "route session pinned" && (service.user != 10 || service.instance != 1 || !strings.Contains(string(service.data), `"session":"owned"`)) {
				t.Fatal("identity/session override", string(service.data))
			}
		})
	}
	id := 1
	lifecycle.operations = map[string]*models.NorthboundOperation{"reset": {UserID: 10, InstanceID: &id, Status: "processing"}}
	before := service.calls
	r := httptest.NewRequest("POST", "/portal/instances/1/workbench/terminals?generation=3", strings.NewReader(`{}`))
	r.AddCookie(cookie)
	r.Header.Set("X-Owner-Action", "1")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != 409 || service.calls != before {
		t.Fatal("maintenance admitted workbench mutation", w.Code)
	}
}
