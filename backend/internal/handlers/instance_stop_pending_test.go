package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
)

type pendingStopHandlerService struct {
	fakeWorkspaceHandlerInstanceService
	err error
}

func (s *pendingStopHandlerService) Stop(int) error { return s.err }

func TestStopInstanceAcceptsOnlyDurablePendingStop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"durable pending", services.ErrInstanceStopPending, http.StatusAccepted},
		{"unknown failure", errors.New("source identity changed"), http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &pendingStopHandlerService{fakeWorkspaceHandlerInstanceService: fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{7: {ID: 7, UserID: 1, InstanceMode: "lite"}}}, err: tc.err}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Params = gin.Params{{Key: "id", Value: "7"}}
			c.Request = httptest.NewRequest(http.MethodPost, "/instances/7/stop", nil)
			c.Set("userID", 1)
			c.Set("userRole", "user")
			(&InstanceHandler{instanceService: svc}).StopInstance(c)
			if w.Code != tc.status {
				t.Fatalf("HTTP %d want %d: %s", w.Code, tc.status, w.Body.String())
			}
		})
	}
}
