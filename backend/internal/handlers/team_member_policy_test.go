package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"

	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
)

func TestCMMemberRemovalNeverInvokesDestructiveEngine(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewTeamHandler(nil) // Calling the engine would panic, including on retry.
	r.DELETE("/teams/:id/members/:memberID", h.DeleteMember)
	for _, member := range []string{"worker", "42", "leader"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("DELETE", "/teams/1/members/"+member, nil))
		if w.Code != 409 || !strings.Contains(w.Body.String(), services.TeamMemberRemovalUnsupportedCode) {
			t.Fatalf("unexpected removal response: %d %s", w.Code, w.Body.String())
		}
	}
}
