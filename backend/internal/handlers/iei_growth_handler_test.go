package handlers

import (
	"clawreef/internal/models"
	"clawreef/internal/ownerrecords"
	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOwnerGrowthEndpointsKeepSessionOriginAndOwnershipBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := testIEIHandlerConfig()
	sso, err := services.NewIEISSOService(cfg)
	if err != nil {
		t.Fatal(err)
	}
	other := "other@example.com"
	instances := &fakeIEIInstanceService{fakeWorkspaceHandlerInstanceService: &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{2: {ID: 2, UserID: 1, Owner: &other, Type: "openclaw", InstanceMode: "lite", Status: "running"}}}}
	h := NewIEISystemHandler(cfg, sso, instances, nil)
	h.SetOwnerDashboard(&ownerrecords.Store{})
	router := gin.New()
	router.POST("/api/v1/ieisystem/session", h.ExchangeSession)
	router.GET("/growth", h.GetGrowth)
	router.POST("/growth", h.GetGrowth)
	router.POST("/visit", h.RecordOwnerVisit)
	router.GET("/reports", h.GetOwnerReport)
	router.POST("/instances/:id/activity", h.RecordOwnerActivity)
	for _, path := range []string{"/growth", "/reports"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != 401 {
			t.Fatal(path, w.Code)
		}
	}
	cookie := exchangeIEITestSession(t, router, cfg, "secure-owner@example.com")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/visit", nil))
	if w.Code != 401 {
		t.Fatal("visit without session", w.Code)
	}
	for _, tc := range []struct {
		path, method, origin, header string
		code                         int
	}{{"/visit", "POST", "", "", 403}, {"/visit", "POST", "https://evil.example", "1", 403}, {"/growth", "POST", "", "", 403}, {"/growth", "POST", "https://evil.example", "1", 403}, {"/instances/2/activity", "POST", "", "1", 404}, {"/reports?kind=month&date=not-a-date", "GET", "", "", 400}} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.AddCookie(cookie)
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("X-Owner-Action", tc.header)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != tc.code {
			t.Fatal(tc, w.Code)
		}
	}
}

func TestOwnerWriteOriginKeepsNodePort(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/", func(c *gin.Context) {
		if ownerWriteRequest(c) {
			c.Status(204)
		}
	})
	for _, tc := range []struct {
		origin string
		want   int
	}{
		{"https://10.130.14.23:32443", 204},
		{"https://10.130.14.23:32543", 403},
		{"https://evil.example:32443", 403},
	} {
		req := httptest.NewRequest("POST", "https://10.130.14.23:32443/", nil)
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("X-Owner-Action", "1")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("origin %s: %d", tc.origin, w.Code)
		}
	}
}
