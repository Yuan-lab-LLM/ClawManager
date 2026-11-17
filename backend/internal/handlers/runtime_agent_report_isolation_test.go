package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawreef/internal/config"
	"clawreef/internal/models"
	"github.com/gin-gonic/gin"
)

type stoppingReportInstances struct {
	*runtimeAgentHandlerInstanceRepo
}

func (r stoppingReportInstances) UpdateReportedRuntimeStateForBinding(ctx context.Context, id int, state string, generation int, message *string, podID int64, gatewayID string, port int, workspace string) error {
	return r.UpdateRuntimeState(ctx, id, state, generation, message)
}
func TestStoppedInstanceReportDoesNotBlockHealthySharedPodInstance(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		success bool
	}{
		{"stopping", fmt.Errorf("runtime state fence: %w", models.ErrInstanceStopping), true},
		{"database", errors.New("database connection unavailable"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			bindings := &runtimeAgentHandlerBindingRepo{bindings: map[int]*models.InstanceRuntimeBinding{
				242: {InstanceID: 242, RuntimePodID: 9, Generation: 1, State: "creating"}, 299: {InstanceID: 299, RuntimePodID: 9, Generation: 1, State: "creating"},
			}}
			instances := &runtimeAgentHandlerInstanceRepo{statusByID: map[int]string{242: "stopped", 299: "creating"}, updateErrors: map[int]error{242: tc.err}}
			h := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret"}, &runtimeAgentHandlerPodRepo{}, bindings, stoppingReportInstances{instances}, &runtimeAgentHandlerEvents{}, nil)
			router := gin.New()
			router.POST("/report", h.ReportGateways)
			req := httptest.NewRequest(http.MethodPost, "/report", bytes.NewBufferString(`{"pod_id":9,"gateways":[{"instance_id":242,"generation":1,"gateway_id":"gw-242","gateway_port":20000,"state":"running"},{"instance_id":299,"generation":1,"gateway_id":"gw-299","gateway_port":20001,"state":"running"}]}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-ClawManager-Agent-Token", "secret")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if tc.success {
				if w.Code != 200 || instances.statusByID[299] != "running" {
					t.Fatalf("healthy instance blocked: code=%d state=%s body=%s", w.Code, instances.statusByID[299], w.Body.String())
				}
			} else if w.Code < 400 || instances.statusByID[299] != "creating" {
				t.Fatalf("unexpected infrastructure error suppressed: code=%d state=%s", w.Code, instances.statusByID[299])
			}
			if instances.statusByID[242] != "stopped" {
				t.Fatal("stopped instance resurrected")
			}
		})
	}
}
