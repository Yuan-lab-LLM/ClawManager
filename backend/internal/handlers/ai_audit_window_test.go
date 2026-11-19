package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawreef/internal/repository"
	"github.com/gin-gonic/gin"
)

func TestAuditWindowDefaultInvalidAndReadiness(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, url string
		err       error
		status    int
	}{
		{"default", "", nil, 200},
		{"invalid timestamp", "?since=invalid", nil, 400},
		{"inverted", "?since=2026-11-17T00:00:00Z&until=2026-11-16T00:00:00Z", nil, 400},
		{"too long", "?since=2026-01-01T00:00:00Z&until=2026-11-17T00:00:00Z", nil, 400},
		{"cursor retains bounds", "?cursor=opaque", nil, 200},
		{"rebuilding", "", &repository.SummaryRebuildingError{Processed: 500, Target: 900000}, 503},
		{"invalid binding", "?cursor=opaque", repository.ErrInvalidAuditQuery, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &stubAIObservabilityHandlerService{err: tc.err}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/admin/ai-audit"+tc.url, nil)
			NewAIObservabilityHandler(service).ListAuditItems(c)
			if w.Code != tc.status {
				t.Fatalf("status %d body %s", w.Code, w.Body.String())
			}
			if tc.name == "default" && (service.auditQuery.Since == nil || service.auditQuery.Until == nil || service.auditQuery.Until.Sub(*service.auditQuery.Since) != 30*24*time.Hour) {
				t.Fatalf("default %+v", service.auditQuery)
			}
			if tc.name == "cursor retains bounds" && (service.auditQuery.Since != nil || service.auditQuery.Until != nil) {
				t.Fatal("cursor window reset to now")
			}
			if tc.name == "rebuilding" && (w.Header().Get("Retry-After") == "" || !strings.Contains(w.Body.String(), `"processed":500`) || !strings.Contains(w.Body.String(), `"target":900000`)) {
				t.Fatalf("progress missing %s", w.Body.String())
			}
		})
	}
}
