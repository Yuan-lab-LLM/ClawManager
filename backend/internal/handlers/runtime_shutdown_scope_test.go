package handlers

import (
	"clawreef/internal/models"
	"clawreef/internal/services"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

type shutdownScopeAuthFailure struct{ services.InstanceAgentService }

func (shutdownScopeAuthFailure) AuthenticateSession(string) (*services.AgentSession, error) {
	return nil, errors.New("password=secret-db")
}
func TestShutdownDeliveryAuthenticationErrorsAreSanitized(t *testing.T) {
	h := NewAgentHandler(shutdownScopeAuthFailure{}, nil, nil, nil, nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/scope", nil)
	c.Request.Header.Set("Authorization", "Bearer private-token")
	h.ShutdownDeliveryScope(c)
	if w.Code != 401 || strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "private-token") {
		t.Fatal(w.Body.String())
	}
}

func TestShutdownDeliveryScopeComesFromAuthenticatedInstance(t *testing.T) {
	for _, owner := range []int{0, 7} {
		h := NewAgentHandler(&agentHandlerSessionService{session: &services.AgentSession{Instance: &models.Instance{ID: 8, UserID: owner}, Agent: &models.InstanceAgent{InstanceID: 8}}}, nil, nil, nil, nil)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/scope?owner_user_id=999", nil)
		c.Request.Header.Set("Authorization", "Bearer cached-session")
		h.ShutdownDeliveryScope(c)
		if owner == 0 && w.Code != 403 {
			t.Fatal(w.Body.String())
		}
		if owner == 7 && (w.Code != 200 || !strings.Contains(w.Body.String(), `"owner_user_id":7`) || strings.Contains(w.Body.String(), "999") || w.Header().Get("Cache-Control") != "no-store") {
			t.Fatal(w.Body.String())
		}
	}
}
