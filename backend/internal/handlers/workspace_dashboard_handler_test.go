package handlers

import (
	"clawreef/internal/workspacedashboard"
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type workspaceSpy struct {
	userID         int
	period         string
	err            error
	dashboardCalls int
	usageCalls     int
}

func (s *workspaceSpy) Dashboard(_ context.Context, id int, period string, _ time.Time) (*workspacedashboard.Dashboard, error) {
	s.dashboardCalls++
	s.userID = id
	s.period = period
	return &workspacedashboard.Dashboard{}, s.err
}
func (s *workspaceSpy) Usage(_ context.Context, id int, period string, now time.Time) (*workspacedashboard.UsageDashboard, error) {
	s.userID = id
	s.period = period
	s.usageCalls++
	return &workspacedashboard.UsageDashboard{Range: period, Timezone: "Asia/Shanghai", AsOf: now}, s.err
}

func TestWorkspaceUsageVerifiedIdentityRangeAndIsolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name     string
		identity any
		query    string
		period   string
		want     int
	}{
		{"missing identity", nil, "", "", 401},
		{"unverified identity", "1", "", "", 401},
		{"zero identity", 0, "", "", 401},
		{"negative identity", -1, "", "", 401},
		{"invalid range", 2, "?range=all", "", 400},
		{"empty range", 2, "?range=", "", 400},
		{"default range and spoofed owner", 2, "?owner=other&user_id=99", "7d", 200},
		{"today", 2, "?range=today", "today", 200},
		{"week", 2, "?range=7d", "7d", 200},
		{"month", 2, "?range=30d", "30d", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spy := &workspaceSpy{}
			r := gin.New()
			r.Use(func(c *gin.Context) {
				if tc.identity != nil {
					c.Set("userID", tc.identity)
				}
				c.Set("isAdmin", true)
			})
			r.GET("/dashboard/workspace/usage", NewWorkspaceDashboardHandler(spy).GetUsage)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/dashboard/workspace/usage"+tc.query, nil))
			if w.Code != tc.want || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status/cache: %d %s %s", w.Code, w.Header(), w.Body.String())
			}
			if spy.dashboardCalls != 0 {
				t.Fatal("usage request loaded full dashboard")
			}
			if tc.want != 200 {
				if spy.usageCalls != 0 {
					t.Fatal("invalid request reached store")
				}
				return
			}
			if spy.usageCalls != 1 || spy.userID != 2 || spy.period != tc.period {
				t.Fatalf("wrong scoped usage request: %+v", spy)
			}
			var payload struct {
				Data map[string]json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Data) != 8 {
				t.Fatalf("unexpected usage fields: %s", w.Body.String())
			}
			for _, key := range []string{"range", "timezone", "as_of", "window", "usage", "usage_history", "agent_usage", "coverage"} {
				if _, ok := payload.Data[key]; !ok {
					t.Errorf("missing %s", key)
				}
			}
			var coverage map[string]json.RawMessage
			if err := json.Unmarshal(payload.Data["coverage"], &coverage); err != nil {
				t.Fatal(err)
			}
			if len(coverage) != 2 || coverage["usage"] == nil || coverage["costs"] == nil {
				t.Fatalf("unexpected coverage: %s", payload.Data["coverage"])
			}
		})
	}
	for _, reader := range []workspaceDashboardReader{nil, &workspaceSpy{err: errors.New("private database failure")}} {
		r := gin.New()
		r.Use(func(c *gin.Context) { c.Set("userID", 1) })
		r.GET("/", NewWorkspaceDashboardHandler(reader).GetUsage)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code != 503 || w.Body.String() != `{"error":"Workspace temporarily unavailable"}` {
			t.Fatalf("failure leaked: %d %s", w.Code, w.Body.String())
		}
	}
}
func TestWorkspaceVerifiedIdentityAndRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		identity any
		query    string
		want     int
	}{{nil, "", 401}, {"1", "", 401}, {0, "", 401}, {-1, "", 401}, {2, "?range=all", 400}, {2, "?owner=other&user_id=99", 200}} {
		spy := &workspaceSpy{}
		h := NewWorkspaceDashboardHandler(spy)
		r := gin.New()
		r.Use(func(c *gin.Context) {
			if tc.identity != nil {
				c.Set("userID", tc.identity)
			}
			c.Set("isAdmin", true)
		})
		r.GET("/", h.Get)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/"+tc.query, nil))
		if w.Code != tc.want {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body.String())
		}
		if tc.want == 200 && (spy.userID != 2 || spy.period != "7d") {
			t.Fatal(spy)
		}
	}
	spy := &workspaceSpy{err: errors.New("db down")}
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", 1) })
	r.GET("/", NewWorkspaceDashboardHandler(spy).Get)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}
