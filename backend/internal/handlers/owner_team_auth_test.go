package handlers

import (
	"clawreef/internal/northbound"
	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type ownerTeamRevocationFixture map[string]bool

func (r ownerTeamRevocationFixture) IsIEISessionRevoked(id string) (bool, error) { return r[id], nil }
func (r ownerTeamRevocationFixture) RevokeIEISession(id string, _ time.Time) error {
	r[id] = true
	return nil
}

func TestOwnerTeamIEICookieCSRFAndRevocation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := testIEIHandlerConfig()
	sso, err := services.NewIEISSOService(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sso.SetSessionRevocationStore(ownerTeamRevocationFixture{})
	handler := NewIEISystemHandler(cfg, sso, nil, nil, nil)
	handler.SetOwnerTeamAPI(&northbound.OwnerTeamAPI{})
	router := gin.New()
	router.POST("/api/v1/ieisystem/session", handler.ExchangeSession)
	router.DELETE("/api/v1/ieisystem/session", handler.DeleteSession)
	handler.RegisterOwnerTeamRoutes(router.Group("/api/v1/ieisystem"))
	do := func(method, path, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(`{}`))
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	if out := do("GET", "/api/v1/ieisystem/team-capabilities", "", nil); out.Code != 401 {
		t.Fatalf("missing cookie allowed: %d", out.Code)
	}
	cookie := exchangeIEITestSession(t, router, cfg, "owner@example.com")
	if out := do("GET", "/api/v1/ieisystem/team-capabilities", "", cookie); out.Code != 200 {
		t.Fatalf("valid cookie denied: %d", out.Code)
	}
	if out := do("POST", "/api/v1/ieisystem/teams", "https://foreign.invalid", cookie); out.Code != 403 {
		t.Fatalf("cross origin allowed: %d", out.Code)
	}
	if out := do("POST", "/api/v1/ieisystem/teams", "http://example.com", cookie); out.Code != 503 {
		t.Fatalf("same-origin did not reach disabled gate: %d", out.Code)
	}
	if out := do("DELETE", "/api/v1/ieisystem/session", "", cookie); out.Code != 200 {
		t.Fatalf("logout failed: %d", out.Code)
	}
	if out := do("GET", "/api/v1/ieisystem/team-capabilities", "", cookie); out.Code != 401 {
		t.Fatalf("revoked cookie allowed: %d", out.Code)
	}
}
