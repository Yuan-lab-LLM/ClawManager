package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDownloadSkillVersionRequiresAgentSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewAgentHandler(nil, nil, nil, nil, nil)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/agent/skills/versions/skill-version-1/download", nil)
	c.Params = gin.Params{{Key: "skillVersion", Value: "skill-version-1"}}

	handler.DownloadSkillVersion(c)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}
