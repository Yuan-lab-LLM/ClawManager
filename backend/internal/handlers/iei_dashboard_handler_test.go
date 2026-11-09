package handlers

import (
	"clawreef/internal/ownerrecords"
	"clawreef/internal/services"
	"context"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type dashboardSpy struct {
	owner string
	calls int
}

func (s *dashboardSpy) Dashboard(_ context.Context, owner string, scope *uint64, _ string, _ time.Time) (*ownerrecords.Dashboard, error) {
	s.owner = owner
	s.calls++
	return &ownerrecords.Dashboard{}, nil
}
func TestOwnerDashboardUsesVerifiedSessionOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := testIEIHandlerConfig()
	sso, err := services.NewIEISSOService(cfg)
	if err != nil {
		t.Fatal(err)
	}
	h := NewIEISystemHandler(cfg, sso, nil, nil)
	spy := &dashboardSpy{}
	h.SetOwnerDashboard(spy)
	r := gin.New()
	r.POST("/api/v1/ieisystem/session", h.ExchangeSession)
	r.GET("/api/v1/ieisystem/dashboard", h.GetDashboard)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/ieisystem/dashboard?owner=other@example.com", nil))
	if w.Code != 401 || spy.calls != 0 {
		t.Fatal("anonymous access", w.Code, spy.calls)
	}
	cookie := exchangeIEITestSession(t, r, cfg, "owner@example.com")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ieisystem/dashboard?owner=other@example.com", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 || spy.owner != "owner@example.com" || w.Header().Get("Cache-Control") == "" {
		t.Fatal("owner override or cache leakage", w.Code, spy.owner)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/ieisystem/dashboard?range=unbounded", nil)
	req.AddCookie(cookie)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 400 || spy.calls != 1 {
		t.Fatal("invalid range accepted", w.Code)
	}
}
