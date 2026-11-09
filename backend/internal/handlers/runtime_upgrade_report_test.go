package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawreef/internal/config"
	"clawreef/internal/models"
	"github.com/gin-gonic/gin"
)

type fencedReportBindings struct {
	*runtimeAgentHandlerBindingRepo
}

func (r fencedReportBindings) UpdateRunning(ctx context.Context, id, generation int, gatewayID string, port int, pid *int) error {
	if id == 10 {
		return models.ErrRuntimeUpgradeInProgress
	}
	return r.runtimeAgentHandlerBindingRepo.UpdateRunning(ctx, id, generation, gatewayID, port, pid)
}

func TestFencedReportDoesNotBlockUnrelatedInstanceInSharedPod(t *testing.T) {
	gin.SetMode(gin.TestMode)
	base := &runtimeAgentHandlerBindingRepo{bindings: map[int]*models.InstanceRuntimeBinding{
		10: {InstanceID: 10, RuntimePodID: 9, Generation: 2}, 11: {InstanceID: 11, RuntimePodID: 9, Generation: 2},
	}}
	h := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret"}, &runtimeAgentHandlerPodRepo{}, fencedReportBindings{base}, nil, &runtimeAgentHandlerEvents{}, nil)
	router := gin.New()
	router.POST("/report", h.ReportGateways)
	req := httptest.NewRequest(http.MethodPost, "/report", bytes.NewBufferString(`{"pod_id":9,"gateways":[{"instance_id":10,"generation":2,"gateway_id":"gw-10","gateway_port":20010,"state":"running"},{"instance_id":11,"generation":2,"gateway_id":"gw-11","gateway_port":20011,"state":"running"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClawManager-Agent-Token", "secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 200 || base.updateRunningCalls != 1 {
		t.Fatalf("status=%d updates=%d body=%s", w.Code, base.updateRunningCalls, w.Body.String())
	}
}
