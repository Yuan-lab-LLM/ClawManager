package handlers

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"clawreef/internal/config"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"

	"github.com/gin-gonic/gin"
)

type runtimeAgentHandlerSkillService struct {
	services.SkillService
	runtimeSyncCalls int
}

func (s *runtimeAgentHandlerSkillService) SyncRuntimeAgentSkillsReport(map[string]any) error {
	s.runtimeSyncCalls++
	return nil
}

func TestRuntimeAgentHandlerRejectsInvalidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	podRepo := &runtimeAgentHandlerPodRepo{}
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret"}, podRepo, &runtimeAgentHandlerBindingRepo{}, nil, &runtimeAgentHandlerEvents{}, nil)

	router := gin.New()
	router.POST("/api/v1/runtime-agent/metrics/report", handler.ReportMetrics)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runtime-agent/metrics/report", bytes.NewBufferString(`{"pod_id":9}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body = %s, want 401", rec.Code, rec.Body.String())
	}
	if podRepo.updateMetricsCalls != 0 {
		t.Fatalf("UpdateMetrics called %d times, want 0", podRepo.updateMetricsCalls)
	}
}

func TestRuntimeAgentSkillsReportCanAcknowledgeWithoutPersistence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	skillService := &runtimeAgentHandlerSkillService{}
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{
		AgentReportToken:       "secret",
		SkillReportPersistence: false,
	}, &runtimeAgentHandlerPodRepo{}, &runtimeAgentHandlerBindingRepo{}, nil, &runtimeAgentHandlerEvents{}, skillService)

	router := gin.New()
	router.POST("/api/v1/runtime-agent/skills/report", handler.ReportSkills)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/runtime-agent/skills/report",
		bytes.NewBufferString(`{"mode":"full","instances":[]}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClawManager-Agent-Token", "secret")
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if skillService.runtimeSyncCalls != 0 {
		t.Fatalf("SyncRuntimeAgentSkillsReport calls = %d, want 0", skillService.runtimeSyncCalls)
	}
}

func TestRuntimeAgentHandlerRegisterUsesConfiguredCapacity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	podRepo := &runtimeAgentHandlerPodRepo{}
	events := &runtimeAgentHandlerEvents{}
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{
		AgentReportToken:  "secret",
		MaxGatewaysPerPod: 33,
	}, podRepo, &runtimeAgentHandlerBindingRepo{}, nil, events, nil)

	router := gin.New()
	router.POST("/api/v1/runtime-agent/register", handler.Register)

	body := `{
		"runtime_type": "openclaw",
		"namespace": "clawmanager-system",
		"pod_name": "openclaw-runtime-test",
		"deployment_name": "openclaw-runtime",
		"image_ref": "registry/openclaw:v1",
		"openclaw_version": "2026.8.1",
		"protocol_version": "2",
		"team_plugin_version": "0.3.0",
		"session_store": "sqlite",
		"image_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"capabilities": ["redis-team.group-hooks-v1", "openclaw.workspace.snapshot-v1", "redis-team.group-hooks-v1"],
		"agent_endpoint": "http://10.0.0.1:19090",
		"state": "ready",
		"capacity": 10,
		"used_slots": 7
	}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runtime-agent/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClawManager-Agent-Token", "secret")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	pod := podRepo.podsByID[1]
	if pod == nil {
		t.Fatal("runtime pod was not persisted")
	}
	if pod.Capacity != 33 {
		t.Fatalf("stored capacity = %d, want configured capacity 33", pod.Capacity)
	}
	if pod.OpenClawVersion == nil || *pod.OpenClawVersion != "2026.8.1" || pod.SessionStore == nil || *pod.SessionStore != "sqlite" {
		t.Fatalf("runtime capability identity was not persisted: %#v", pod)
	}
	if got := pod.Capabilities(); !reflect.DeepEqual(got, []string{"openclaw.workspace.snapshot-v1", "redis-team.group-hooks-v1"}) {
		t.Fatalf("capabilities = %#v", got)
	}
	if events.lastType != "runtime_pod_state" {
		t.Fatalf("event type = %q, want runtime_pod_state", events.lastType)
	}
	payload, ok := events.lastPayload.(map[string]any)
	if !ok {
		t.Fatalf("event payload = %#v, want map", events.lastPayload)
	}
	if payload["capacity"] != 33 {
		t.Fatalf("event capacity = %#v, want configured capacity 33", payload["capacity"])
	}
}

func TestRuntimeAgentHandlerHeartbeatUsesConfiguredCapacity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	podRepo := &runtimeAgentHandlerPodRepo{}
	events := &runtimeAgentHandlerEvents{}
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{
		AgentReportToken:  "secret",
		MaxGatewaysPerPod: 44,
	}, podRepo, &runtimeAgentHandlerBindingRepo{}, nil, events, nil)

	router := gin.New()
	router.POST("/api/v1/runtime-agent/heartbeat", handler.Heartbeat)

	body := `{
		"pod_id": 9,
		"state": "ready",
		"used_slots": 8,
		"draining": false
	}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runtime-agent/heartbeat", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClawManager-Agent-Token", "secret")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	if podRepo.updatedPodID != 9 {
		t.Fatalf("updated pod id = %d, want 9", podRepo.updatedPodID)
	}
	if podRepo.lastHeartbeatCapacity != 44 {
		t.Fatalf("heartbeat capacity = %d, want configured capacity 44", podRepo.lastHeartbeatCapacity)
	}
	payload, ok := events.lastPayload.(map[string]any)
	if !ok {
		t.Fatalf("event payload = %#v, want map", events.lastPayload)
	}
	if payload["capacity"] != 44 {
		t.Fatalf("event capacity = %#v, want configured capacity 44", payload["capacity"])
	}
}

func TestRuntimeAgentHandlerUsesServerReceiveTimeForLiveness(t *testing.T) {
	gin.SetMode(gin.TestMode)
	podRepo := &runtimeAgentHandlerPodRepo{}
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret"}, podRepo, &runtimeAgentHandlerBindingRepo{}, nil, &runtimeAgentHandlerEvents{}, nil)
	router := gin.New()
	router.POST("/api/v1/runtime-agent/register", handler.Register)
	router.POST("/api/v1/runtime-agent/heartbeat", handler.Heartbeat)
	router.POST("/api/v1/runtime-agent/metrics/report", handler.ReportMetrics)
	reportedAt := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)

	before := time.Now().UTC()
	registerBody := `{"runtime_type":"openclaw","namespace":"clawmanager-system","pod_name":"openclaw-runtime-test","deployment_name":"openclaw-runtime","image_ref":"registry/openclaw:v1","reported_at":"` + reportedAt + `"}`
	serveRuntimeAgentRequest(t, router, "/api/v1/runtime-agent/register", registerBody)
	registered := podRepo.podsByID[1]
	if registered == nil || registered.LastSeenAt == nil || registered.LastSeenAt.Before(before) {
		t.Fatalf("register last_seen_at = %v, want server receive time after %s", registered, before)
	}

	serveRuntimeAgentRequest(t, router, "/api/v1/runtime-agent/heartbeat", `{"pod_id":1,"state":"ready","reported_at":"`+reportedAt+`"}`)
	if podRepo.lastHeartbeatAt.Before(before) {
		t.Fatalf("heartbeat last_seen_at = %s, want server receive time after %s", podRepo.lastHeartbeatAt, before)
	}

	serveRuntimeAgentRequest(t, router, "/api/v1/runtime-agent/metrics/report", `{"pod_id":1,"reported_at":"`+reportedAt+`"}`)
	if podRepo.lastMetrics.LastSeenAt == nil || podRepo.lastMetrics.LastSeenAt.Before(before) {
		t.Fatalf("metrics last_seen_at = %v, want server receive time after %s", podRepo.lastMetrics.LastSeenAt, before)
	}
}

func serveRuntimeAgentRequest(t *testing.T, router http.Handler, path, body string) {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-ClawManager-Agent-Token", "secret")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST %s status = %d, body = %s", path, recorder.Code, recorder.Body.String())
	}
}

func TestRuntimeAgentHandlerMetricsReportUpdatesPodAndPublishesEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	podRepo := &runtimeAgentHandlerPodRepo{}
	events := &runtimeAgentHandlerEvents{}
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret"}, podRepo, &runtimeAgentHandlerBindingRepo{}, nil, events, nil)

	router := gin.New()
	router.POST("/api/v1/runtime-agent/metrics/report", handler.ReportMetrics)

	body := `{
		"pod_id": 9,
		"cpu_millis_used": 2400,
		"memory_bytes_used": 4096,
		"disk_bytes_used": 8192,
		"network_rx_bytes": 12,
		"network_tx_bytes": 34,
		"metrics": {"load": 0.42}
	}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runtime-agent/metrics/report", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClawManager-Agent-Token", "secret")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	if podRepo.updatedPodID != 9 {
		t.Fatalf("updated pod id = %d, want 9", podRepo.updatedPodID)
	}
	if podRepo.lastMetrics.CPUMillisUsed != 2400 || podRepo.lastMetrics.MemoryBytesUsed != 4096 || podRepo.lastMetrics.DiskBytesUsed != 8192 {
		t.Fatalf("metrics = %#v, want cpu/memory/disk values from request", podRepo.lastMetrics)
	}
	if podRepo.lastMetrics.MetricsJSON == nil || *podRepo.lastMetrics.MetricsJSON == "" {
		t.Fatalf("metrics json was not persisted")
	}
	if events.lastType != "runtime_pod_metrics" {
		t.Fatalf("event type = %q, want runtime_pod_metrics", events.lastType)
	}
	payload, ok := events.lastPayload.(map[string]any)
	if !ok {
		t.Fatalf("event payload = %#v, want map", events.lastPayload)
	}
	if payload["pod_id"] != int64(9) {
		t.Fatalf("event pod_id = %#v, want 9", payload["pod_id"])
	}
}

func TestRuntimeAgentHandlerGatewayReportOnlyUpdatesCurrentPodBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	bindingRepo := &runtimeAgentHandlerBindingRepo{
		bindings: map[int]*models.InstanceRuntimeBinding{
			10: {InstanceID: 10, RuntimePodID: 9, Generation: 2},
			11: {InstanceID: 11, RuntimePodID: 99, Generation: 2},
			12: {InstanceID: 12, RuntimePodID: 9, Generation: 3},
		},
	}
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret"}, &runtimeAgentHandlerPodRepo{}, bindingRepo, nil, &runtimeAgentHandlerEvents{}, nil)

	router := gin.New()
	router.POST("/api/v1/runtime-agent/gateways/report", handler.ReportGateways)

	body := `{
		"pod_id": 9,
		"gateways": [
			{"instance_id":10,"gateway_id":"gw-10","gateway_port":20010,"state":"running","generation":2},
			{"instance_id":11,"gateway_id":"gw-11","gateway_port":20011,"state":"running","generation":2},
			{"instance_id":12,"gateway_id":"gw-12","gateway_port":20012,"state":"running","generation":2}
		]
	}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runtime-agent/gateways/report", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClawManager-Agent-Token", "secret")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	if bindingRepo.updateRunningCalls != 1 {
		t.Fatalf("UpdateRunning calls = %d, want 1", bindingRepo.updateRunningCalls)
	}
	if len(bindingRepo.deletedInstanceIDs) != 0 {
		t.Fatalf("stale generation report deleted instances %v", bindingRepo.deletedInstanceIDs)
	}
}

func TestRuntimeAgentHandlerGatewayReportSyncsInstanceRuntimeState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	message := "gateway port 20000 is not listening"
	bindingRepo := &runtimeAgentHandlerBindingRepo{
		bindings: map[int]*models.InstanceRuntimeBinding{
			10: {InstanceID: 10, RuntimePodID: 9, Generation: 2},
			11: {InstanceID: 11, RuntimePodID: 9, Generation: 2},
			12: {InstanceID: 12, RuntimePodID: 9, Generation: 2},
		},
	}
	instanceRepo := &runtimeAgentHandlerInstanceRepo{}
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret"}, &runtimeAgentHandlerPodRepo{}, bindingRepo, instanceRepo, &runtimeAgentHandlerEvents{}, nil)

	router := gin.New()
	router.POST("/api/v1/runtime-agent/gateways/report", handler.ReportGateways)

	body := `{
		"pod_id": 9,
		"gateways": [
			{"instance_id":10,"gateway_id":"gw-10","gateway_port":20010,"state":"healthy","generation":2},
			{"instance_id":11,"gateway_id":"gw-11","gateway_port":20011,"state":"error","generation":2,"error_message":"` + message + `"},
			{"instance_id":12,"gateway_id":"gw-12","gateway_port":20012,"state":"ready","generation":2}
		]
	}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runtime-agent/gateways/report", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClawManager-Agent-Token", "secret")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	if got := instanceRepo.statusByID[10]; got != "running" {
		t.Fatalf("healthy gateway synced instance status = %q, want running", got)
	}
	if got := instanceRepo.statusByID[11]; got != "error" {
		t.Fatalf("error gateway synced instance status = %q, want error", got)
	}
	if got := instanceRepo.statusByID[12]; got != "running" {
		t.Fatalf("ready gateway synced instance status = %q, want running", got)
	}
	if instanceRepo.messageByID[11] == nil || *instanceRepo.messageByID[11] != message {
		t.Fatalf("error message = %#v, want %q", instanceRepo.messageByID[11], message)
	}
}

func TestRuntimeAgentHandlerGatewayReportIsolatesStaleInstanceGeneration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	bindingRepo := &runtimeAgentHandlerBindingRepo{bindings: map[int]*models.InstanceRuntimeBinding{
		10: {InstanceID: 10, RuntimePodID: 9, Generation: 2},
		11: {InstanceID: 11, RuntimePodID: 9, Generation: 2},
	}}
	instanceRepo := &runtimeAgentHandlerInstanceRepo{updateErrors: map[int]error{10: repository.ErrStaleRuntimeGeneration}}
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret"}, &runtimeAgentHandlerPodRepo{}, bindingRepo, instanceRepo, &runtimeAgentHandlerEvents{}, nil)
	router := gin.New()
	router.POST("/api/v1/runtime-agent/gateways/report", handler.ReportGateways)
	body := `{"pod_id":9,"gateways":[{"instance_id":10,"gateway_id":"gw-10","gateway_port":20010,"state":"running","generation":2},{"instance_id":11,"gateway_id":"gw-11","gateway_port":20011,"state":"running","generation":2}]}`
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/runtime-agent/gateways/report", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-ClawManager-Agent-Token", "secret")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", recorder.Code, recorder.Body.String())
	}
	if instanceRepo.statusByID[11] != "running" {
		t.Fatalf("current gateway after stale peer was not processed: %#v", instanceRepo.statusByID)
	}
}

func TestRuntimeAgentHandlerUnhealthyReportRetainsBindingAndRecovers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pid := 1234
	binding := &models.InstanceRuntimeBinding{InstanceID: 10, RuntimePodID: 9, Generation: 2, GatewayID: "gw-10-2", GatewayPort: 20010, GatewayPID: &pid, State: "running"}
	bindingRepo := &runtimeAgentHandlerBindingRepo{bindings: map[int]*models.InstanceRuntimeBinding{10: binding}}
	instanceRepo := &runtimeAgentHandlerInstanceRepo{}
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret"}, &runtimeAgentHandlerPodRepo{}, bindingRepo, instanceRepo, &runtimeAgentHandlerEvents{}, nil)
	router := gin.New()
	router.POST("/api/v1/runtime-agent/gateways/report", handler.ReportGateways)
	for _, state := range []string{"unhealthy", "running"} {
		body := fmt.Sprintf(`{"pod_id":9,"gateways":[{"instance_id":10,"gateway_id":"gw-10-2","gateway_port":20010,"gateway_pid":1234,"state":%q,"generation":2,"error_message":"gateway_health_unavailable"}]}`, state)
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/runtime-agent/gateways/report", bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-ClawManager-Agent-Token", "secret")
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK || len(bindingRepo.deletedInstanceIDs) != 0 {
			t.Fatalf("report lost the writer: status=%d deleted=%v", recorder.Code, bindingRepo.deletedInstanceIDs)
		}
		if state == "unhealthy" && (bindingRepo.updateStateCalls != 1 || bindingRepo.lastUpdatedState != "unhealthy" || bindingRepo.lastUpdatedGeneration != 2 || bindingRepo.bindings[10] != binding || instanceRepo.statusByID[10] != "error") {
			t.Fatal("unhealthy was not projected as a retained unhealthy binding")
		}
	}
	if instanceRepo.statusByID[10] != "running" || instanceRepo.messageByID[10] != nil {
		t.Fatal("healthy heartbeat did not recover the same instance")
	}
}

func TestRuntimeAgentHandlerReconcilesExactPreviousOpenClawGateway(t *testing.T) {
	gin.SetMode(gin.TestMode)
	capabilities := `["openclaw.upgrade-preflight-v3"]`
	podRepo := &runtimeAgentHandlerPodRepo{podsByID: map[int64]*models.RuntimePod{
		9: {ID: 9, RuntimeType: services.RuntimeTypeOpenClaw, CapabilitiesJSON: &capabilities},
	}}
	bindingRepo := &runtimeAgentHandlerBindingRepo{bindings: map[int]*models.InstanceRuntimeBinding{}}
	errorMessage := "no schedulable openclaw runtime pod"
	workspace := "/workspaces/openclaw/user-1/instance-209"
	instanceRepo := &runtimeAgentHandlerInstanceRepo{instancesByID: map[int]*models.Instance{
		209: {ID: 209, UserID: 1, Type: services.RuntimeTypeOpenClaw, InstanceMode: services.InstanceModeLite, Status: "error", RuntimeGeneration: 3, RuntimeErrorMessage: &errorMessage, WorkspacePath: &workspace},
	}}
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret", WorkspaceRoot: "/workspaces"}, podRepo, bindingRepo, instanceRepo, &runtimeAgentHandlerEvents{}, nil)
	router := gin.New()
	router.POST("/api/v1/runtime-agent/gateways/report", handler.ReportGateways)
	body := `{"pod_id":9,"gateways":[{"instance_id":209,"gateway_id":"gw-209-2","gateway_port":20000,"workspace_path":"/workspaces/openclaw/user-1/instance-209","state":"running","generation":2}]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runtime-agent/gateways/report", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClawManager-Agent-Token", "secret")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	if bindingRepo.reconcileCalls != 1 || bindingRepo.bindings[209] == nil {
		t.Fatalf("previous gateway was not reconciled: calls=%d binding=%#v", bindingRepo.reconcileCalls, bindingRepo.bindings[209])
	}
	if instanceRepo.statusByID[209] != "running" || instanceRepo.generationByID[209] != 2 {
		t.Fatalf("reconciled lifecycle = %q/%d, want running/2", instanceRepo.statusByID[209], instanceRepo.generationByID[209])
	}
}

func TestRuntimeAgentHandlerReconcilesExactCurrentOpenClawGateway(t *testing.T) {
	gin.SetMode(gin.TestMode)
	capabilities := `["openclaw.upgrade-preflight-v3"]`
	podRepo := &runtimeAgentHandlerPodRepo{podsByID: map[int64]*models.RuntimePod{
		9: {ID: 9, RuntimeType: services.RuntimeTypeOpenClaw, CapabilitiesJSON: &capabilities},
	}}
	bindingRepo := &runtimeAgentHandlerBindingRepo{bindings: map[int]*models.InstanceRuntimeBinding{}}
	workspace := "/workspaces/openclaw/user-1/instance-210"
	instanceRepo := &runtimeAgentHandlerInstanceRepo{instancesByID: map[int]*models.Instance{
		210: {ID: 210, UserID: 1, Type: services.RuntimeTypeOpenClaw, InstanceMode: services.InstanceModeLite, Status: "creating", RuntimeGeneration: 3, WorkspacePath: &workspace},
	}}
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret", WorkspaceRoot: "/workspaces"}, podRepo, bindingRepo, instanceRepo, &runtimeAgentHandlerEvents{}, nil)
	router := gin.New()
	router.POST("/api/v1/runtime-agent/gateways/report", handler.ReportGateways)
	body := `{"pod_id":9,"gateways":[{"instance_id":210,"gateway_id":"gw-210-3","gateway_port":20009,"workspace_path":"/workspaces/openclaw/user-1/instance-210","state":"running","generation":3}]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runtime-agent/gateways/report", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClawManager-Agent-Token", "secret")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || bindingRepo.reconcileCalls != 1 || bindingRepo.bindings[210] == nil {
		t.Fatalf("current Gateway was not reconciled: status=%d calls=%d binding=%#v", rec.Code, bindingRepo.reconcileCalls, bindingRepo.bindings[210])
	}
	if instanceRepo.statusByID[210] != "running" || instanceRepo.generationByID[210] != 3 {
		t.Fatalf("reconciled lifecycle = %q/%d, want running/3", instanceRepo.statusByID[210], instanceRepo.generationByID[210])
	}
}

func TestRuntimeAgentHandlerDoesNotApplyEightOneReconciliationToLegacyOpenClaw(t *testing.T) {
	gin.SetMode(gin.TestMode)
	podRepo := &runtimeAgentHandlerPodRepo{podsByID: map[int64]*models.RuntimePod{
		9: {ID: 9, RuntimeType: services.RuntimeTypeOpenClaw},
	}}
	bindingRepo := &runtimeAgentHandlerBindingRepo{bindings: map[int]*models.InstanceRuntimeBinding{}}
	workspace := "/workspaces/openclaw/user-1/instance-210"
	instanceRepo := &runtimeAgentHandlerInstanceRepo{instancesByID: map[int]*models.Instance{
		210: {ID: 210, UserID: 1, Type: services.RuntimeTypeOpenClaw, InstanceMode: services.InstanceModeLite, Status: "error", RuntimeGeneration: 2, WorkspacePath: &workspace},
	}}
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret", WorkspaceRoot: "/workspaces"}, podRepo, bindingRepo, instanceRepo, &runtimeAgentHandlerEvents{}, nil)
	router := gin.New()
	router.POST("/api/v1/runtime-agent/gateways/report", handler.ReportGateways)
	body := `{"pod_id":9,"gateways":[{"instance_id":210,"gateway_id":"gw-210-2","gateway_port":20009,"workspace_path":"/workspaces/openclaw/user-1/instance-210","state":"running","generation":2}]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runtime-agent/gateways/report", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClawManager-Agent-Token", "secret")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || bindingRepo.reconcileCalls != 0 || bindingRepo.bindings[210] != nil {
		t.Fatalf("legacy OpenClaw unexpectedly used 8.1 reconciliation: status=%d calls=%d binding=%#v", rec.Code, bindingRepo.reconcileCalls, bindingRepo.bindings[210])
	}
}

func TestRuntimeAgentHandlerDoesNotReconcileMismatchedGatewayIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	capabilities := `["openclaw.upgrade-preflight-v3"]`
	podRepo := &runtimeAgentHandlerPodRepo{podsByID: map[int64]*models.RuntimePod{
		9: {ID: 9, RuntimeType: services.RuntimeTypeOpenClaw, CapabilitiesJSON: &capabilities},
	}}
	bindingRepo := &runtimeAgentHandlerBindingRepo{bindings: map[int]*models.InstanceRuntimeBinding{}}
	errorMessage := "some unrelated runtime error"
	workspace := "/workspaces/openclaw/user-1/instance-209"
	instanceRepo := &runtimeAgentHandlerInstanceRepo{instancesByID: map[int]*models.Instance{
		209: {ID: 209, UserID: 1, Type: services.RuntimeTypeOpenClaw, InstanceMode: services.InstanceModeLite, Status: "error", RuntimeGeneration: 3, RuntimeErrorMessage: &errorMessage, WorkspacePath: &workspace},
	}}
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret", WorkspaceRoot: "/workspaces"}, podRepo, bindingRepo, instanceRepo, &runtimeAgentHandlerEvents{}, nil)
	router := gin.New()
	router.POST("/api/v1/runtime-agent/gateways/report", handler.ReportGateways)
	body := `{"pod_id":9,"gateways":[{"instance_id":209,"gateway_id":"gw-209-2","gateway_port":20000,"workspace_path":"/workspaces/openclaw/user-2/instance-209","state":"running","generation":2}]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runtime-agent/gateways/report", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClawManager-Agent-Token", "secret")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || bindingRepo.reconcileCalls != 0 || bindingRepo.bindings[209] != nil {
		t.Fatalf("unmatched drift was reconciled: status=%d calls=%d binding=%#v", rec.Code, bindingRepo.reconcileCalls, bindingRepo.bindings[209])
	}
}

func TestRuntimeAgentHandlerGatewayReportDeletesMissingCurrentPodBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	lastHealthAt := time.Now().UTC().Add(-time.Minute)
	bindingRepo := &runtimeAgentHandlerBindingRepo{
		bindings: map[int]*models.InstanceRuntimeBinding{
			10: {InstanceID: 10, RuntimePodID: 9, Generation: 2, State: "running", LastHealthAt: &lastHealthAt},
			11: {InstanceID: 11, RuntimePodID: 99, Generation: 2, State: "running"},
			12: {InstanceID: 12, RuntimePodID: 9, Generation: 2, State: "error"},
		},
	}
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret", HeartbeatTimeout: 10 * time.Second}, &runtimeAgentHandlerPodRepo{}, bindingRepo, nil, &runtimeAgentHandlerEvents{}, nil)

	router := gin.New()
	router.POST("/api/v1/runtime-agent/gateways/report", handler.ReportGateways)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runtime-agent/gateways/report", bytes.NewBufferString(`{"pod_id":9,"gateways":[]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClawManager-Agent-Token", "secret")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	if got, want := bindingRepo.deletedInstanceIDs, []int{10}; !reflect.DeepEqual(got, want) {
		t.Fatalf("deleted instance IDs = %v, want %v", got, want)
	}
	if got, want := bindingRepo.deletedRuntimePodIDs, []int64{9}; !reflect.DeepEqual(got, want) {
		t.Fatalf("released runtime pod IDs = %v, want %v", got, want)
	}
}

func TestRuntimeAgentHandlerGatewayReportDoesNotDeleteFreshBindingFromFirstEmptySnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	lastHealthAt := time.Now().UTC()
	bindingRepo := &runtimeAgentHandlerBindingRepo{
		bindings: map[int]*models.InstanceRuntimeBinding{
			10: {InstanceID: 10, RuntimePodID: 9, Generation: 3, State: "running", LastHealthAt: &lastHealthAt},
		},
	}
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret", HeartbeatTimeout: 10 * time.Second}, &runtimeAgentHandlerPodRepo{}, bindingRepo, nil, &runtimeAgentHandlerEvents{}, nil)

	router := gin.New()
	router.POST("/api/v1/runtime-agent/gateways/report", handler.ReportGateways)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runtime-agent/gateways/report", bytes.NewBufferString(`{"pod_id":9,"gateways":[]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClawManager-Agent-Token", "secret")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	if len(bindingRepo.deletedInstanceIDs) != 0 {
		t.Fatalf("fresh binding deleted by first empty snapshot: %v", bindingRepo.deletedInstanceIDs)
	}
	if bindingRepo.bindings[10] == nil {
		t.Fatal("fresh binding was removed")
	}
}

func TestRuntimeAgentHandlerGatewayReportReleasesOnlyExpiredPendingBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	old := time.Now().UTC().Add(-time.Minute)
	fresh := time.Now().UTC()
	bindingRepo := &runtimeAgentHandlerBindingRepo{bindings: map[int]*models.InstanceRuntimeBinding{
		10: {InstanceID: 10, RuntimePodID: 9, GatewayID: "pending-10-3", Generation: 3, State: "creating", UpdatedAt: old},
		11: {InstanceID: 11, RuntimePodID: 9, GatewayID: "pending-11-3", Generation: 3, State: "creating", UpdatedAt: fresh},
	}}
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret", HeartbeatTimeout: 10 * time.Second}, &runtimeAgentHandlerPodRepo{}, bindingRepo, nil, &runtimeAgentHandlerEvents{}, nil)
	router := gin.New()
	router.POST("/api/v1/runtime-agent/gateways/report", handler.ReportGateways)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runtime-agent/gateways/report", bytes.NewBufferString(`{"pod_id":9,"gateways":[]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ClawManager-Agent-Token", "secret")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}
	if got, want := bindingRepo.deletedInstanceIDs, []int{10}; !reflect.DeepEqual(got, want) {
		t.Fatalf("released pending instances = %v, want %v", got, want)
	}
	if bindingRepo.bindings[11] == nil {
		t.Fatal("fresh pending binding was released by the first empty snapshot")
	}
}

type runtimeAgentHandlerPodRepo struct {
	updatedPodID          int64
	lastMetrics           repository.RuntimePodMetricsUpdate
	lastHeartbeatCapacity int
	lastHeartbeatAt       time.Time
	updateMetricsCalls    int
	podsByID              map[int64]*models.RuntimePod
}

func (r *runtimeAgentHandlerPodRepo) UpsertFromAgent(ctx context.Context, pod *models.RuntimePod) error {
	if r.podsByID == nil {
		r.podsByID = map[int64]*models.RuntimePod{}
	}
	if pod.ID == 0 {
		pod.ID = int64(len(r.podsByID) + 1)
	}
	cp := *pod
	r.podsByID[pod.ID] = &cp
	return nil
}

func (r *runtimeAgentHandlerPodRepo) GetByID(ctx context.Context, id int64) (*models.RuntimePod, error) {
	if r.podsByID == nil {
		return nil, nil
	}
	return r.podsByID[id], nil
}

func (r *runtimeAgentHandlerPodRepo) GetByNamespaceName(ctx context.Context, namespace, podName string) (*models.RuntimePod, error) {
	for _, pod := range r.podsByID {
		if pod.Namespace == namespace && pod.PodName == podName {
			return pod, nil
		}
	}
	return nil, nil
}

func (r *runtimeAgentHandlerPodRepo) List(ctx context.Context, runtimeType string) ([]models.RuntimePod, error) {
	return nil, nil
}

func (r *runtimeAgentHandlerPodRepo) ListSchedulable(ctx context.Context, runtimeType string) ([]models.RuntimePod, error) {
	return nil, nil
}

func (r *runtimeAgentHandlerPodRepo) TryClaimSlot(ctx context.Context, podID int64) (bool, error) {
	return false, nil
}

func (r *runtimeAgentHandlerPodRepo) ReleaseSlot(ctx context.Context, podID int64) error {
	return nil
}

func (r *runtimeAgentHandlerPodRepo) MarkState(ctx context.Context, podID int64, state string, draining bool) error {
	return nil
}

func (r *runtimeAgentHandlerPodRepo) UpdateHeartbeat(ctx context.Context, podID int64, state string, usedSlots int, capacity int, draining bool, lastSeenAt time.Time) error {
	r.updatedPodID = podID
	r.lastHeartbeatCapacity = capacity
	r.lastHeartbeatAt = lastSeenAt
	return nil
}

func (r *runtimeAgentHandlerPodRepo) MarkUnseenUnhealthy(ctx context.Context, cutoff time.Time) error {
	return nil
}

func (r *runtimeAgentHandlerPodRepo) UpdateMetrics(ctx context.Context, podID int64, metrics repository.RuntimePodMetricsUpdate) error {
	r.updatedPodID = podID
	r.lastMetrics = metrics
	r.updateMetricsCalls++
	return nil
}

type runtimeAgentHandlerBindingRepo struct {
	updateRunningCalls    int
	updateStateCalls      int
	lastUpdatedState      string
	lastUpdatedGeneration int
	bindings              map[int]*models.InstanceRuntimeBinding
	deletedInstanceIDs    []int
	deletedRuntimePodIDs  []int64
	reconcileCalls        int
}

func (r *runtimeAgentHandlerBindingRepo) ReconcileReportedGateway(ctx context.Context, binding *models.InstanceRuntimeBinding, expectedInstanceGeneration, portBlockSize int) (bool, error) {
	r.reconcileCalls++
	if r.bindings == nil {
		r.bindings = map[int]*models.InstanceRuntimeBinding{}
	}
	cp := *binding
	r.bindings[binding.InstanceID] = &cp
	return true, nil
}

func (r *runtimeAgentHandlerBindingRepo) DeletePendingByInstanceIDGenerationAndReleaseSlot(ctx context.Context, instanceID int, runtimePodID int64, generation int, updatedBefore time.Time) (bool, error) {
	binding := r.bindings[instanceID]
	if binding == nil || binding.RuntimePodID != runtimePodID || binding.Generation != generation || binding.State != "creating" || binding.UpdatedAt.After(updatedBefore) {
		return false, nil
	}
	r.deletedInstanceIDs = append(r.deletedInstanceIDs, instanceID)
	r.deletedRuntimePodIDs = append(r.deletedRuntimePodIDs, runtimePodID)
	delete(r.bindings, instanceID)
	return true, nil
}

func (r *runtimeAgentHandlerBindingRepo) Create(ctx context.Context, binding *models.InstanceRuntimeBinding) error {
	return nil
}

func (r *runtimeAgentHandlerBindingRepo) GetByInstanceID(ctx context.Context, instanceID int) (*models.InstanceRuntimeBinding, error) {
	return r.bindings[instanceID], nil
}

func (r *runtimeAgentHandlerBindingRepo) GetRunningByInstanceID(ctx context.Context, instanceID int) (*models.InstanceRuntimeBinding, error) {
	return nil, nil
}

func (r *runtimeAgentHandlerBindingRepo) ListByRuntimePodID(ctx context.Context, runtimePodID int64) ([]models.InstanceRuntimeBinding, error) {
	bindings := make([]models.InstanceRuntimeBinding, 0, len(r.bindings))
	for _, binding := range r.bindings {
		if binding.RuntimePodID != runtimePodID {
			continue
		}
		bindings = append(bindings, *binding)
	}
	return bindings, nil
}

func (r *runtimeAgentHandlerBindingRepo) ListByRuntimePodIDs(ctx context.Context, runtimePodIDs []int64) ([]models.InstanceRuntimeBinding, error) {
	return nil, nil
}

func (r *runtimeAgentHandlerBindingRepo) UpdateRunning(ctx context.Context, instanceID int, generation int, gatewayID string, port int, pid *int) error {
	r.updateRunningCalls++
	return nil
}
func (r *runtimeAgentHandlerBindingRepo) UpdateGatewayAssignment(ctx context.Context, instanceID int, generation int, gatewayID string, pid *int, state string, lastHealthAt *time.Time) error {
	return nil
}

func (r *runtimeAgentHandlerBindingRepo) UpdateState(ctx context.Context, instanceID int, generation int, state string, message *string) error {
	r.updateStateCalls++
	r.lastUpdatedState = state
	r.lastUpdatedGeneration = generation
	return nil
}

func (r *runtimeAgentHandlerBindingRepo) DeleteErrorByRuntimePodIDAndGatewayPort(ctx context.Context, runtimePodID int64, gatewayPort int) (int64, error) {
	return 0, nil
}
func (r *runtimeAgentHandlerBindingRepo) DeleteByInstanceID(ctx context.Context, instanceID int) error {
	return nil
}

func (r *runtimeAgentHandlerBindingRepo) DeleteByInstanceIDAndReleaseSlot(ctx context.Context, instanceID int, runtimePodID int64) error {
	r.deletedInstanceIDs = append(r.deletedInstanceIDs, instanceID)
	r.deletedRuntimePodIDs = append(r.deletedRuntimePodIDs, runtimePodID)
	delete(r.bindings, instanceID)
	return nil
}

func (r *runtimeAgentHandlerBindingRepo) DeleteRunningByInstanceIDGenerationAndReleaseSlot(ctx context.Context, instanceID int, runtimePodID int64, generation int) (bool, error) {
	binding := r.bindings[instanceID]
	if binding == nil || binding.RuntimePodID != runtimePodID || binding.Generation != generation || binding.State != "running" {
		return false, nil
	}
	r.deletedInstanceIDs = append(r.deletedInstanceIDs, instanceID)
	r.deletedRuntimePodIDs = append(r.deletedRuntimePodIDs, runtimePodID)
	delete(r.bindings, instanceID)
	return true, nil
}

type runtimeAgentHandlerInstanceRepo struct {
	statusByID     map[int]string
	generationByID map[int]int
	messageByID    map[int]*string
	instancesByID  map[int]*models.Instance
	updateErrors   map[int]error
}

func (r *runtimeAgentHandlerInstanceRepo) Create(instance *models.Instance) error {
	return nil
}

func (r *runtimeAgentHandlerInstanceRepo) GetByID(id int) (*models.Instance, error) {
	return r.instancesByID[id], nil
}

func (r *runtimeAgentHandlerInstanceRepo) FindByPodIP(string) (*models.Instance, error) {
	return nil, nil
}

func (r *runtimeAgentHandlerInstanceRepo) GetByAccessToken(accessToken string) (*models.Instance, error) {
	return nil, nil
}

func (r *runtimeAgentHandlerInstanceRepo) GetByAgentBootstrapToken(bootstrapToken string) (*models.Instance, error) {
	return nil, nil
}

func (r *runtimeAgentHandlerInstanceRepo) GetAll(offset, limit int) ([]models.Instance, error) {
	return nil, nil
}

func (r *runtimeAgentHandlerInstanceRepo) CountAll() (int, error) {
	return 0, nil
}

func (r *runtimeAgentHandlerInstanceRepo) GetByUserID(userID int, offset, limit int) ([]models.Instance, error) {
	return nil, nil
}

func (r *runtimeAgentHandlerInstanceRepo) CountByUserID(userID int) (int, error) {
	return 0, nil
}

func (r *runtimeAgentHandlerInstanceRepo) CountActiveByMode(ctx context.Context, mode string) (int, error) {
	return 0, nil
}

func (r *runtimeAgentHandlerInstanceRepo) ExistsByUserIDAndName(userID int, name string) (bool, error) {
	return false, nil
}

func (r *runtimeAgentHandlerInstanceRepo) GetAllRunning() ([]models.Instance, error) {
	return nil, nil
}

func (r *runtimeAgentHandlerInstanceRepo) GetV2DesiredRunning(ctx context.Context, limit int) ([]models.Instance, error) {
	return nil, nil
}

func (r *runtimeAgentHandlerInstanceRepo) GetV2Creating(ctx context.Context, limit int) ([]models.Instance, error) {
	return nil, nil
}

func (r *runtimeAgentHandlerInstanceRepo) UpdateRuntimeState(ctx context.Context, id int, status string, generation int, message *string) error {
	if err := r.updateErrors[id]; err != nil {
		return err
	}
	if r.statusByID == nil {
		r.statusByID = map[int]string{}
	}
	if r.generationByID == nil {
		r.generationByID = map[int]int{}
	}
	if r.messageByID == nil {
		r.messageByID = map[int]*string{}
	}
	r.statusByID[id] = status
	r.generationByID[id] = generation
	r.messageByID[id] = message
	return nil
}

func (r *runtimeAgentHandlerInstanceRepo) SetWorkspacePath(ctx context.Context, id int, workspacePath string) error {
	return nil
}

func (r *runtimeAgentHandlerInstanceRepo) UpdateWorkspaceUsage(ctx context.Context, id int, usageBytes int64) error {
	return nil
}

func (r *runtimeAgentHandlerInstanceRepo) Update(instance *models.Instance) error {
	return nil
}

func (r *runtimeAgentHandlerInstanceRepo) Delete(id int) error {
	return nil
}

type runtimeAgentHandlerEvents struct {
	lastType    string
	lastPayload any
}

func (e *runtimeAgentHandlerEvents) Publish(ctx context.Context, eventType string, payload any) error {
	e.lastType = eventType
	e.lastPayload = payload
	return nil
}
