package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCMTeamCreationPolicyRejectsBeforeService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewTeamHandler(nil)
	r.POST("/teams", func(c *gin.Context) { c.Set("userID", 1); h.CreateTeam(c) })
	for _, body := range []string{
		`{"name":"x","members":[{"member_id":"leader","is_leader":true,"runtime_type":"opencode"}]}`,
		`{"name":"x","members":[{"member_id":"leader","is_leader":true,"runtime_type":"openclaw","instance_mode":"pro"}]}`,
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", "/teams", strings.NewReader(body)))
		if w.Code != 422 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
}
