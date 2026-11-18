package handlers

import (
	"bytes"
	"clawreef/internal/config"
	"clawreef/internal/models"
	"clawreef/internal/repository"
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
	"time"
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
		for _, scenario := range []string{"valid", "canonical-creating", "canonical-wrong-pod", "canonical-wrong-generation", "canonical-wrong-port", "canonical-wrong-workspace", "canonical-wrong-id", "malformed-pending", "malformed-pending-running", "stop-after-recovery", "nil-verifier", "authority-refused", "pod-uid", "port", "workspace", "generation", "stopped", "wrong-pending-id", "wrong-pod", "wrong-owner", "wrong-profile"} {
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
				case "canonical-creating", "canonical-wrong-pod", "canonical-wrong-generation", "canonical-wrong-port", "canonical-wrong-workspace", "canonical-wrong-id":
					binding.GatewayID = "gw-10-2"
					if scenario == "canonical-wrong-pod" {
						binding.RuntimePodID++
					}
					if scenario == "canonical-wrong-generation" {
						gateway.Generation--
					}
					if scenario == "canonical-wrong-port" {
						gateway.GatewayPort++
					}
					if scenario == "canonical-wrong-workspace" {
						gateway.WorkspacePath += "/other"
					}
					if scenario == "canonical-wrong-id" {
						gateway.GatewayID = "gw-other"
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

func (r *pendingReportBindings) MarkProtectedGatewayReportMissing(ctx context.Context, binding models.InstanceRuntimeBinding, before time.Time, message string) (bool, error) {
	current := r.bindings[binding.InstanceID]
	if current == nil || current.RuntimePodID != binding.RuntimePodID || current.Generation != binding.Generation || current.GatewayID != binding.GatewayID {
		return false, nil
	}
	current.ErrorMessage = &message
	return true, nil
}

func TestProtectedGatewayOmissionRetainsOwnershipAndHealthyReturn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, state := range []string{"running", "creating"} {
		t.Run(state, func(t *testing.T) {
			old := time.Now().UTC().Add(-time.Hour)
			binding := &models.InstanceRuntimeBinding{InstanceID: 10, RuntimePodID: 5, RuntimeType: "hermes", GatewayID: "gw-10-2", GatewayPort: 20001, WorkspacePath: "/workspaces/hermes/user-1/instance-10", State: state, Generation: 2, LastHealthAt: &old, UpdatedAt: old}
			if state == "creating" {
				binding.GatewayID = "pending-10-2"
			}
			instance := &models.Instance{ID: 10, UserID: 1, Type: "hermes", InstanceMode: "lite", RuntimeType: "gateway", Status: state, RuntimeGeneration: 2, RuntimeSecurityProfile: services.RuntimeSecurityProfile}
			bindings := &pendingReportBindings{runtimeAgentHandlerBindingRepo: runtimeAgentHandlerBindingRepo{bindings: map[int]*models.InstanceRuntimeBinding{10: binding}}, instance: instance}
			instances := &runtimeAgentHandlerInstanceRepo{instancesByID: map[int]*models.Instance{10: instance}}
			uid := "pod-current"
			pods := &runtimeAgentHandlerPodRepo{podsByID: map[int64]*models.RuntimePod{5: {ID: 5, RuntimeType: "hermes", PodUID: &uid}}}
			h := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret", WorkspaceRoot: "/workspaces", HeartbeatTimeout: time.Second}, pods, bindings, instances, nil, nil)
			h.SetRuntimeGatewayReportVerifier(&pendingReportVerifier{})
			router := gin.New()
			router.POST("/report", h.ReportGateways)
			serveRuntimeAgentRequest(t, router, "/report", `{"pod_id":5,"gateways":[]}`)
			if bindings.bindings[10] != binding || len(bindings.deletedInstanceIDs) != 0 || binding.State != state || instance.Status != state || binding.ErrorMessage == nil {
				t.Fatalf("omission lost protected ownership/suspicion: binding=%+v instance=%+v deletes=%v", bindings.bindings[10], instance, bindings.deletedInstanceIDs)
			}
			serveRuntimeAgentRequest(t, router, "/report", `{"pod_id":5,"gateways":[{"instance_id":10,"gateway_id":"gw-10-2","gateway_port":20001,"workspace_path":"/workspaces/hermes/user-1/instance-10","state":"running","generation":2}]}`)
			if bindings.bindings[10] == nil || (bindings.updateRunningCalls != 1 && bindings.reconcileCalls != 1) {
				t.Fatal("healthy return was permanently rejected")
			}
			serveRuntimeAgentRequest(t, router, "/report", `{"pod_id":5,"gateways":[{"instance_id":10,"gateway_id":"gw-10-2","gateway_port":20001,"workspace_path":"/workspaces/hermes/user-1/instance-10","state":"stopped","generation":2}]}`)
			if len(bindings.deletedInstanceIDs) != 0 || bindings.bindings[10] == nil {
				t.Fatal("reported stop released ownership without exact teardown proof")
			}
		})
	}
}

func (r *pendingReportBindings) ReconcileUnboundProtectedReportedGateway(ctx context.Context, binding *models.InstanceRuntimeBinding, owner int, podUID string, block int) (bool, error) {
	if podUID != "pod-current" {
		return false, nil
	}
	return r.ReconcilePendingReportedGateway(ctx, binding, owner, block)
}

func TestProtectedUnboundGatewayReportRequiresCurrentAuthority(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []string{"valid", "creating", "authority-refused", "nil-verifier", "generation", "stopped", "deleting", "workspace", "port", "pod-uid", "wrong-profile", "wrong-owner"} {
		t.Run(scenario, func(t *testing.T) {
			instance := &models.Instance{ID: 10, UserID: 1, Type: "hermes", InstanceMode: "lite", RuntimeType: "gateway", Status: "running", RuntimeGeneration: 2, RuntimeSecurityProfile: services.RuntimeSecurityProfile}
			uid := "pod-current"
			bindings := &pendingReportBindings{runtimeAgentHandlerBindingRepo: runtimeAgentHandlerBindingRepo{bindings: map[int]*models.InstanceRuntimeBinding{}}, instance: instance}
			instances := &runtimeAgentHandlerInstanceRepo{instancesByID: map[int]*models.Instance{10: instance}}
			pods := &runtimeAgentHandlerPodRepo{podsByID: map[int64]*models.RuntimePod{5: {ID: 5, RuntimeType: "hermes", PodUID: &uid}}}
			h := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret", WorkspaceRoot: "/workspaces"}, pods, bindings, instances, nil, nil)
			verifier := &pendingReportVerifier{}
			h.SetRuntimeGatewayReportVerifier(verifier)
			report := runtimeAgentGatewayReport{InstanceID: 10, GatewayID: "gw-10-2", GatewayPort: 20001, WorkspacePath: "/workspaces/hermes/user-1/instance-10", State: "running", Generation: 2}
			switch scenario {
			case "creating":
				instance.Status = "creating"
			case "authority-refused":
				verifier.refused = true
			case "nil-verifier":
				h.SetRuntimeGatewayReportVerifier(nil)
			case "generation":
				report.Generation--
			case "stopped", "deleting":
				instance.Status = scenario
			case "workspace":
				report.WorkspacePath += "/other"
			case "port":
				report.GatewayPort++
			case "pod-uid":
				uid = "replaced"
			case "wrong-profile":
				instance.RuntimeSecurityProfile = "future-profile"
			case "wrong-owner":
				instance.UserID++
			}
			payload, _ := json.Marshal(runtimeAgentGatewaysRequest{runtimeAgentPodIdentity: runtimeAgentPodIdentity{PodID: 5}, Gateways: []runtimeAgentGatewayReport{report}})
			router := gin.New()
			router.POST("/report", h.ReportGateways)
			serveRuntimeAgentRequest(t, router, "/report", string(payload))
			valid := scenario == "valid" || scenario == "creating"
			if (bindings.bindings[10] != nil) != valid || (bindings.reconcileCalls == 1) != valid || bindings.updateRunningCalls != 0 {
				t.Fatalf("adoption=%+v calls=%d authority=%d", bindings.bindings[10], bindings.reconcileCalls, verifier.calls)
			}
		})
	}
}

func TestGatewayOmissionRetainsLegacyAndUnknownOwnership(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []string{"native-legacy", "legacy", "nil-instance-repo", "missing-instance-row", "no-marker"} {
		for _, state := range []string{"running", "creating"} {
			t.Run(scenario+"/"+state, func(t *testing.T) {
				old := time.Now().UTC().Add(-time.Hour)
				binding := &models.InstanceRuntimeBinding{InstanceID: 10, RuntimePodID: 5, RuntimeType: "openclaw", GatewayID: "gw-10-2", GatewayPort: 20001, WorkspacePath: "/workspaces/openclaw/user-1/instance-10", State: state, Generation: 2, LastHealthAt: &old, UpdatedAt: old}
				if state == "creating" {
					binding.GatewayID = "pending-10-2"
				}
				base := runtimeAgentHandlerBindingRepo{bindings: map[int]*models.InstanceRuntimeBinding{10: binding}}
				marked := &pendingReportBindings{runtimeAgentHandlerBindingRepo: base}
				var bindings repository.InstanceRuntimeBindingRepository = marked
				if scenario == "no-marker" {
					bindings = &base
				}
				instance := &models.Instance{ID: 10, UserID: 1, Type: "openclaw", InstanceMode: "lite", RuntimeType: "gateway", Status: state, RuntimeGeneration: 2}
				var instances repository.InstanceRepository = &runtimeAgentHandlerInstanceRepo{instancesByID: map[int]*models.Instance{10: instance}}
				if scenario == "nil-instance-repo" {
					instances = nil
				}
				if scenario == "missing-instance-row" {
					instances = &runtimeAgentHandlerInstanceRepo{}
				}
				capabilities := `["container-slots-v1"]`
				pod := &models.RuntimePod{ID: 5, RuntimeType: "openclaw"}
				if scenario != "legacy" {
					pod.CapabilitiesJSON = &capabilities
				}
				h := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret", HeartbeatTimeout: time.Second}, &runtimeAgentHandlerPodRepo{podsByID: map[int64]*models.RuntimePod{5: pod}}, bindings, instances, nil, nil)
				router := gin.New()
				router.POST("/report", h.ReportGateways)
				serveRuntimeAgentRequest(t, router, "/report", `{"pod_id":5,"gateways":[]}`)
				current, err := bindings.GetByInstanceID(context.Background(), 10)
				if err != nil || current != binding || binding.State != state || binding.Generation != 2 || instance.Status != state || binding.LastHealthAt != &old {
					t.Fatalf("omission changed ownership: current=%+v state=%s err=%v", current, state, err)
				}
				if scenario != "no-marker" && binding.ErrorMessage == nil {
					t.Fatal("missing gateway lacks suspicion metadata")
				}
				if len(base.deletedInstanceIDs) != 0 || len(marked.deletedInstanceIDs) != 0 {
					t.Fatal("omission released slot")
				}
			})
		}
	}
}
