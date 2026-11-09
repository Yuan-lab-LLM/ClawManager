package handlers

import (
	"bytes"
	"clawreef/internal/config"
	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	"clawreef/internal/services"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

type pendingReportVerifier struct {
	calls   int
	refused bool
}

func (v *pendingReportVerifier) VerifyReportedGateway(ctx context.Context, id runtimeauth.Identity, kind, workspace string, port int) error {
	v.calls++
	if v.refused || id != (runtimeauth.Identity{InstanceID: 10, UserID: 1, Generation: 2, PodUID: "pod-current"}) || workspace != fmt.Sprintf("/workspaces/%s/user-1/instance-10", kind) || port != 20001 {
		return errors.New("refused")
	}
	return ctx.Err()
}

type pendingReportBindings struct {
	runtimeAgentHandlerBindingRepo
	block     int
	owner     int
	instance  *models.Instance
	stopAfter bool
}

func (r *pendingReportBindings) ReconcilePendingReportedGateway(ctx context.Context, binding *models.InstanceRuntimeBinding, owner, block int) (bool, error) {
	r.owner = owner
	r.block = block
	ok, err := r.ReconcileReportedGateway(ctx, binding, binding.Generation, block)
	if ok {
		r.instance.Status = "running"
		if r.stopAfter {
			r.instance.Status = "stopped"
		}
	}
	return ok, err
}
func TestProtectedPendingGatewayReports(t *testing.T) {
	for _, kind := range []string{"hermes", "openclaw", "opencode", "deepseek-harness"} {
		for _, scenario := range []string{"valid", "canonical-creating", "canonical-wrong-pod", "canonical-wrong-generation", "malformed-pending", "malformed-pending-running", "stop-after-recovery", "nil-verifier", "authority-refused", "pod-uid", "port", "workspace", "generation", "stopped", "missing-binding", "wrong-pending-id", "wrong-pod", "wrong-owner", "wrong-profile"} {
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				gin.SetMode(gin.TestMode)
				uid := "pod-current"
				workspace := fmt.Sprintf("/workspaces/%s/user-1/instance-10", kind)
				binding := &models.InstanceRuntimeBinding{InstanceID: 10, RuntimePodID: 5, RuntimeType: kind, GatewayID: "pending-10-2", GatewayPort: 20001, WorkspacePath: workspace, State: "creating", Generation: 2}
				bindings := &pendingReportBindings{runtimeAgentHandlerBindingRepo: runtimeAgentHandlerBindingRepo{bindings: map[int]*models.InstanceRuntimeBinding{10: binding}}}
				instance := &models.Instance{ID: 10, UserID: 1, Type: kind, InstanceMode: "lite", RuntimeType: "gateway", Status: "creating", RuntimeGeneration: 2, RuntimeSecurityProfile: services.RuntimeSecurityProfile}
				bindings.instance = instance
				instances := &runtimeAgentHandlerInstanceRepo{instancesByID: map[int]*models.Instance{10: instance}}
				pods := &runtimeAgentHandlerPodRepo{podsByID: map[int64]*models.RuntimePod{5: {ID: 5, RuntimeType: kind, PodUID: &uid}}}
				verifier := &pendingReportVerifier{}
				h := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret", WorkspaceRoot: "/workspaces"}, pods, bindings, instances, nil, nil)
				h.SetRuntimeGatewayReportVerifier(verifier)
				gateway := runtimeAgentGatewayReport{InstanceID: 10, GatewayID: "gw-10-2", GatewayPort: 20001, WorkspacePath: workspace, State: "running", Generation: 2}
				switch scenario {
				case "canonical-creating", "canonical-wrong-pod", "canonical-wrong-generation":
					binding.GatewayID = "gw-10-2"
					if scenario == "canonical-wrong-pod" {
						binding.RuntimePodID++
					}
					if scenario == "canonical-wrong-generation" {
						gateway.Generation--
					}
				case "malformed-pending", "malformed-pending-running":
					binding.GatewayID = "pending-invalid"
					if scenario == "malformed-pending-running" {
						binding.State = "running"
					}
				case "stop-after-recovery":
					bindings.stopAfter = true
				case "nil-verifier":
					h.SetRuntimeGatewayReportVerifier(nil)
				case "authority-refused":
					verifier.refused = true
				case "pod-uid":
					uid = "replaced"
				case "port":
					gateway.GatewayPort++
				case "workspace":
					gateway.WorkspacePath += "/other"
				case "generation":
					gateway.Generation--
				case "stopped":
					instance.Status = "stopped"
				case "missing-binding":
					delete(bindings.bindings, 10)
				case "wrong-pending-id":
					binding.GatewayID = "pending-10-1"
				case "wrong-pod":
					binding.RuntimePodID++
				case "wrong-owner":
					instance.UserID++
				case "wrong-profile":
					instance.RuntimeSecurityProfile = "future-profile"
				}
				payload, _ := json.Marshal(runtimeAgentGatewaysRequest{runtimeAgentPodIdentity: runtimeAgentPodIdentity{PodID: 5}, Gateways: []runtimeAgentGatewayReport{gateway}})
				router := gin.New()
				router.POST("/report", h.ReportGateways)
				req := httptest.NewRequest(http.MethodPost, "/report", bytes.NewReader(payload))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set(runtimeAgentTokenHeader, "secret")
				response := httptest.NewRecorder()
				router.ServeHTTP(response, req)
				if response.Code != 200 {
					t.Fatalf("HTTP=%d", response.Code)
				}
				if scenario == "canonical-creating" {
					if bindings.reconcileCalls != 0 || verifier.calls != 0 || bindings.updateRunningCalls != 1 || instances.statusByID[10] != "running" {
						t.Fatalf("normal assigned gateway report blocked: reconciliations=%d authority=%d runningUpdates=%d status=%q", bindings.reconcileCalls, verifier.calls, bindings.updateRunningCalls, instances.statusByID[10])
					}
				} else if scenario == "valid" || scenario == "stop-after-recovery" {
					if bindings.reconcileCalls != 1 || verifier.calls != 1 || bindings.updateRunningCalls != 0 || instances.statusByID[10] != "" || bindings.owner != 1 || bindings.block != services.RuntimeGatewayPortBlockSize(kind) {
						t.Fatalf("not recovered: reconciliations=%d authority=%d status=%q", bindings.reconcileCalls, verifier.calls, instances.statusByID[10])
					}
					expected := "running"
					if scenario == "stop-after-recovery" {
						expected = "stopped"
					}
					if instance.Status != expected {
						t.Fatal("post-transaction status changed")
					}
				} else if bindings.reconcileCalls != 0 || bindings.updateRunningCalls != 0 || instances.statusByID[10] == "running" {
					t.Fatal("rejected report changed binding or SQL")
				}
			})
		}
	}
}
