package handlers

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestObservationRebuildingReturnsRetryableProgress(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	handleObservationQueryError(c, &repository.SummaryRebuildingError{Processed: 500, Target: 900000})
	if w.Code != 503 || w.Header().Get("Retry-After") != "2" {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestObservationChangedPopulationRequiresVisibleRestart(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	handleObservationQueryError(c, repository.ErrSessionCursorStale)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "cursor_stale") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestGetInstanceSessionUsageOwnerBoundary(t *testing.T) {
	h := &InstanceHandler{instanceService: &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{9: {ID: 9, UserID: 7, Type: "openclaw"}}}}
	for name, serve := range map[string]func(*gin.Context){"list": h.GetInstanceSessionUsage, "detail": h.GetInstanceSessionUsageDetail} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/instances/9/session-usage?session_id=main", nil)
			c.Params = gin.Params{{Key: "id", Value: "9"}}
			c.Set("userID", 8)
			c.Set("userRole", "user")
			serve(c)
			if w.Code != 403 {
				t.Fatalf("wrong owner status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
