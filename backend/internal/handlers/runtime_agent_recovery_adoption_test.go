package handlers

import (
	"bytes"
	"clawreef/internal/config"
	"clawreef/internal/models"
	"context"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

type retiredRecoveryBindingRepo struct{ runtimeAgentHandlerBindingRepo }

func (r *retiredRecoveryBindingRepo) ReportedGatewayRecoveryBlocked(context.Context, *models.InstanceRuntimeBinding) (bool, error) {
	return true, nil
}

func TestRuntimeAgentHandlerDoesNotAdoptRetiredUnprofiledOpenClaw(t *testing.T) {
	gin.SetMode(gin.TestMode)
	capabilities := `["openclaw.upgrade-preflight-v3"]`
	pods := &runtimeAgentHandlerPodRepo{podsByID: map[int64]*models.RuntimePod{9: {ID: 9, RuntimeType: "openclaw", CapabilitiesJSON: &capabilities}}}
	bindings := &retiredRecoveryBindingRepo{runtimeAgentHandlerBindingRepo: runtimeAgentHandlerBindingRepo{bindings: map[int]*models.InstanceRuntimeBinding{}}}
	workspace := "/workspaces/openclaw/user-1/instance-209"
	instances := &runtimeAgentHandlerInstanceRepo{instancesByID: map[int]*models.Instance{209: {ID: 209, UserID: 1, Type: "openclaw", InstanceMode: "lite", RuntimeType: "gateway", Status: "creating", RuntimeGeneration: 3, WorkspacePath: &workspace}}}
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret", WorkspaceRoot: "/workspaces"}, pods, bindings, instances, &runtimeAgentHandlerEvents{}, nil)
	router := gin.New()
	router.POST("/report", handler.ReportGateways)
	req := httptest.NewRequest(http.MethodPost, "/report", bytes.NewBufferString(`{"pod_id":9,"gateways":[{"instance_id":209,"gateway_id":"gw-209-2","gateway_port":20000,"workspace_path":"/workspaces/openclaw/user-1/instance-209","state":"running","generation":2}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClawManager-Agent-Token", "secret")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if bindings.reconcileCalls != 0 || bindings.bindings[209] != nil || instances.statusByID[209] == "running" || instances.generationByID[209] == 2 {
		t.Fatal("retired legacy source report recreated binding or rolled generation back")
	}
}
