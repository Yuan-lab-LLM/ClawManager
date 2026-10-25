package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/services"

	"github.com/gin-gonic/gin"
)

type agentHandlerSessionService struct {
	services.InstanceAgentService
	session *services.AgentSession
}

func (s *agentHandlerSessionService) AuthenticateSession(string) (*services.AgentSession, error) {
	return s.session, nil
}

type agentHandlerSkillService struct {
	services.SkillService
	syncCalls int
}

func (s *agentHandlerSkillService) SyncAgentSkills(int, services.AgentSkillInventoryReportRequest) error {
	s.syncCalls++
	return nil
}

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

func TestAgentSkillInventoryCanAcknowledgeWithoutPersistence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	agentService := &agentHandlerSessionService{session: &services.AgentSession{
		Instance: &models.Instance{ID: 81},
		Agent:    &models.InstanceAgent{AgentID: "agent-81"},
	}}
	skillService := &agentHandlerSkillService{}
	handler := NewAgentHandler(
		agentService,
		nil,
		nil,
		nil,
		skillService,
		WithAgentSkillReportPersistence(false),
	)
	router := gin.New()
	router.POST("/api/v1/agent/skills/report", handler.ReportSkillInventory)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/agent/skills/report",
		bytes.NewBufferString(`{"agent_id":"agent-81","mode":"full","skills":[]}`),
	)
	req.Header.Set("Authorization", "Bearer session-token")
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if skillService.syncCalls != 0 {
		t.Fatalf("SyncAgentSkills calls = %d, want 0", skillService.syncCalls)
	}
	if !strings.Contains(recorder.Body.String(), `"persisted":false`) {
		t.Fatalf("response body = %s, want persisted=false", recorder.Body.String())
	}
}
