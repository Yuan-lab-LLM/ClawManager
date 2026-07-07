package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"clawreef/internal/config"
	"clawreef/internal/models"
	"clawreef/internal/repository"

	"github.com/gin-gonic/gin"
)

func TestRuntimeAgentHandlerRejectsInvalidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	podRepo := &runtimeAgentHandlerPodRepo{}
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret"}, podRepo, &runtimeAgentHandlerBindingRepo{}, nil, &runtimeAgentHandlerEvents{})

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

func TestRuntimeAgentHandlerRegisterUsesConfiguredCapacity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	podRepo := &runtimeAgentHandlerPodRepo{}
	events := &runtimeAgentHandlerEvents{}
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{
		AgentReportToken:  "secret",
		MaxGatewaysPerPod: 33,
	}, podRepo, &runtimeAgentHandlerBindingRepo{}, nil, events)

	router := gin.New()
	router.POST("/api/v1/runtime-agent/register", handler.Register)

	body := `{
		"runtime_type": "openclaw",
		"namespace": "clawmanager-system",
		"pod_name": "openclaw-runtime-test",
		"deployment_name": "openclaw-runtime",
		"image_ref": "registry/openclaw:v1",
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
	}, podRepo, &runtimeAgentHandlerBindingRepo{}, nil, events)

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

func TestRuntimeAgentHandlerMetricsReportUpdatesPodAndPublishesEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	podRepo := &runtimeAgentHandlerPodRepo{}
	events := &runtimeAgentHandlerEvents{}
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret"}, podRepo, &runtimeAgentHandlerBindingRepo{}, nil, events)

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
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret"}, &runtimeAgentHandlerPodRepo{}, bindingRepo, nil, &runtimeAgentHandlerEvents{})

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
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret"}, &runtimeAgentHandlerPodRepo{}, bindingRepo, instanceRepo, &runtimeAgentHandlerEvents{})

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

type runtimeAgentHandlerPodRepo struct {
	updatedPodID          int64
	lastMetrics           repository.RuntimePodMetricsUpdate
	lastHeartbeatCapacity int
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
	updateRunningCalls int
	updateStateCalls   int
	bindings           map[int]*models.InstanceRuntimeBinding
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
	return nil, nil
}

func (r *runtimeAgentHandlerBindingRepo) ListByRuntimePodIDs(ctx context.Context, runtimePodIDs []int64) ([]models.InstanceRuntimeBinding, error) {
	return nil, nil
}

func (r *runtimeAgentHandlerBindingRepo) UpdateRunning(ctx context.Context, instanceID int, generation int, gatewayID string, port int, pid *int) error {
	r.updateRunningCalls++
	return nil
}

func (r *runtimeAgentHandlerBindingRepo) UpdateState(ctx context.Context, instanceID int, generation int, state string, message *string) error {
	r.updateStateCalls++
	return nil
}

func (r *runtimeAgentHandlerBindingRepo) DeleteByInstanceID(ctx context.Context, instanceID int) error {
	return nil
}

func (r *runtimeAgentHandlerBindingRepo) DeleteByInstanceIDAndReleaseSlot(ctx context.Context, instanceID int, runtimePodID int64) error {
	return nil
}

type runtimeAgentHandlerInstanceRepo struct {
	statusByID     map[int]string
	generationByID map[int]int
	messageByID    map[int]*string
}

func (r *runtimeAgentHandlerInstanceRepo) Create(instance *models.Instance) error {
	return nil
}

func (r *runtimeAgentHandlerInstanceRepo) GetByID(id int) (*models.Instance, error) {
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
