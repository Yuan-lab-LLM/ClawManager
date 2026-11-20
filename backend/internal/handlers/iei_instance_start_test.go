package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
)

type dashboardStartInstances struct {
	*fakeIEIInstanceService
	starts  []int
	failure error
}

func (s *dashboardStartInstances) StartStopped(id int) error {
	s.starts = append(s.starts, id)
	if s.failure != nil {
		return s.failure
	}
	s.instances[id].Status = "creating"
	return nil
}
func TestOwnerDashboardStartKeepsIdentityAndRequiresSessionOwnershipAndIntent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := testIEIHandlerConfig()
	sso, err := services.NewIEISSOService(cfg)
	if err != nil {
		t.Fatal(err)
	}
	owner, other, workspace := "owner@example.com", "other@example.com", "/original/saved/workspace"
	instances := &dashboardStartInstances{fakeIEIInstanceService: &fakeIEIInstanceService{fakeWorkspaceHandlerInstanceService: &fakeWorkspaceHandlerInstanceService{instances: map[int]*models.Instance{}}}}
	for n, runtime := range []string{"openclaw", "hermes", "opencode", "deepseek-harness", "workbuddy"} {
		instances.instances[n+1] = &models.Instance{ID: n + 1, UserID: 10, Owner: &owner, Name: "saved", Type: runtime, InstanceMode: "lite", RuntimeType: "gateway", Status: "stopped", WorkspacePath: &workspace}
		if runtime == "workbuddy" {
			instances.instances[n+1].InstanceMode = "pro"
			instances.instances[n+1].RuntimeVariant = services.WorkbuddyRuntimeLinux
		}
	}
	instances.instances[98] = &models.Instance{ID: 98, Owner: &owner, Type: "openclaw", InstanceMode: "lite", Status: "error"}
	instances.instances[99] = &models.Instance{ID: 99, Owner: &other, Type: "openclaw", InstanceMode: "lite", Status: "stopped"}
	h := NewIEISystemHandler(cfg, sso, instances, nil)
	r := gin.New()
	r.POST("/api/v1/ieisystem/session", h.ExchangeSession)
	r.POST("/api/v1/ieisystem/instances/:id/start", h.StartInstance)
	cookie := exchangeIEITestSession(t, r, cfg, owner)
	call := func(id int, authenticated, intent bool) *httptest.ResponseRecorder {
		q := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/ieisystem/instances/%d/start", id), nil)
		if authenticated {
			q.AddCookie(cookie)
		}
		if intent {
			q.Header.Set("X-Owner-Action", "1")
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, q)
		return w
	}
	for _, tc := range []struct {
		id           int
		auth, intent bool
		want         int
	}{{1, false, true, 401}, {99, true, true, 404}, {1, true, false, 403}, {98, true, true, 409}} {
		if w := call(tc.id, tc.auth, tc.intent); w.Code != tc.want {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if len(instances.starts) != 0 {
		t.Fatal("rejected request dispatched Start")
	}
	for id := 1; id <= 5; id++ {
		for n := 0; n < 2; n++ {
			if w := call(id, true, true); w.Code != 202 {
				t.Fatal(w.Code, w.Body.String())
			}
		}
		if *instances.instances[id].WorkspacePath != workspace || instances.instances[id].ID != id {
			t.Fatal("identity or storage changed")
		}
	}
	if len(instances.starts) != 5 || len(instances.resetCalls) != 0 || len(instances.restartCalls) != 0 {
		t.Fatal("duplicate start, restart or reset", instances.starts)
	}
	instances.instances[1].Status = "stopped"
	instances.failure = errors.New("private runtime diagnostic")
	w := call(1, true, true)
	if w.Code != 503 || strings.Contains(w.Body.String(), "private runtime diagnostic") {
		t.Fatal(w.Code, w.Body.String())
	}
}
