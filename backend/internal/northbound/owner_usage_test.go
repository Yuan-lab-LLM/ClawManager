package northbound

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"clawreef/internal/ownerrecords"
	"github.com/gin-gonic/gin"
)

type ownerUsageSpy struct {
	calls int
	scope uint64
	owner string
}

func (s *ownerUsageSpy) Dashboard(_ context.Context, owner string, scope *uint64, _ string, _ time.Time) (*ownerrecords.Dashboard, error) {
	s.calls++
	s.owner = owner
	if scope != nil {
		s.scope = *scope
	}
	return &ownerrecords.Dashboard{}, nil
}

func TestOwnerUsageScopeAndAccountBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name      string
		principal *Principal
		query     string
		want      int
	}{
		{"anonymous", nil, "?owner=alice", 403},
		{"legacy scope not expanded", &Principal{UserID: 7, Scopes: []string{ScopeLiteRead}}, "?owner=alice", 403},
		{"negative account", &Principal{UserID: -1, Scopes: []string{ScopeOwnerUsageRead}}, "?owner=alice", 403},
		{"missing owner", &Principal{UserID: 7, Scopes: []string{ScopeOwnerUsageRead}}, "", 400},
		{"unbounded range", &Principal{UserID: 7, Scopes: []string{ScopeOwnerUsageRead}}, "?owner=alice&range=all", 400},
		{"scoped query", &Principal{UserID: 7, Scopes: []string{ScopeOwnerUsageRead}}, "?owner=alice&scope=999&user_id=999", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spy := &ownerUsageSpy{}
			h := &CoreHandler{service: &CoreService{ownerUsage: spy}}
			r := gin.New()
			r.GET("/usage", func(c *gin.Context) {
				if tc.principal != nil {
					c.Set("northboundPrincipal", tc.principal)
				}
				h.GetOwnerUsage(c)
			})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/usage"+tc.query, nil))
			if w.Code != tc.want {
				t.Fatalf("status %d, want %d", w.Code, tc.want)
			}
			if tc.want == 200 {
				if spy.calls != 1 || spy.scope != 7 || spy.owner != "alice" || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("untrusted account override or cache leak", spy)
				}
			} else if spy.calls != 0 {
				t.Fatal("unauthorized query executed")
			}
		})
	}
}
