package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"clawreef/internal/config"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

const runtimeAgentTokenHeader = "X-ClawManager-Agent-Token"

type runtimeEventPublisher interface {
	Publish(ctx context.Context, eventType string, payload any) error
}

type RuntimeAgentHandler struct {
	cfg          config.RuntimePoolConfig
	podRepo      repository.RuntimePodRepository
	bindingRepo  repository.InstanceRuntimeBindingRepository
	instanceRepo repository.InstanceRepository
	events       runtimeEventPublisher
}

type runtimeAgentPodIdentity struct {
	PodID     int64  `json:"pod_id"`
	Namespace string `json:"namespace"`
	PodName   string `json:"pod_name"`
}

type runtimeAgentRegisterRequest struct {
	RuntimeType    string          `json:"runtime_type" binding:"required"`
	Namespace      string          `json:"namespace" binding:"required"`
	PodName        string          `json:"pod_name" binding:"required"`
	PodUID         *string         `json:"pod_uid,omitempty"`
	PodIP          *string         `json:"pod_ip,omitempty"`
	NodeName       *string         `json:"node_name,omitempty"`
	DeploymentName string          `json:"deployment_name" binding:"required"`
	ImageRef       string          `json:"image_ref" binding:"required"`
	AgentEndpoint  *string         `json:"agent_endpoint,omitempty"`
	State          string          `json:"state"`
	Capacity       int             `json:"capacity"`
	UsedSlots      int             `json:"used_slots"`
	Draining       bool            `json:"draining"`
	Metrics        json.RawMessage `json:"metrics,omitempty"`
	ReportedAt     *time.Time      `json:"reported_at,omitempty"`
}

type runtimeAgentHeartbeatRequest struct {
	runtimeAgentPodIdentity
	State      string     `json:"state" binding:"required"`
	UsedSlots  int        `json:"used_slots"`
	Draining   bool       `json:"draining"`
	ReportedAt *time.Time `json:"reported_at,omitempty"`
}

type runtimeAgentMetricsRequest struct {
	runtimeAgentPodIdentity
	CPUMillisUsed   int64           `json:"cpu_millis_used"`
	MemoryBytesUsed int64           `json:"memory_bytes_used"`
	DiskBytesUsed   int64           `json:"disk_bytes_used"`
	NetworkRXBytes  int64           `json:"network_rx_bytes"`
	NetworkTXBytes  int64           `json:"network_tx_bytes"`
	Metrics         json.RawMessage `json:"metrics,omitempty"`
	ReportedAt      *time.Time      `json:"reported_at,omitempty"`
}

type runtimeAgentGatewaysRequest struct {
	runtimeAgentPodIdentity
	Gateways []runtimeAgentGatewayReport `json:"gateways" binding:"required"`
}

type runtimeAgentGatewayReport struct {
	InstanceID   int        `json:"instance_id" binding:"required"`
	GatewayID    string     `json:"gateway_id"`
	GatewayPort  int        `json:"gateway_port"`
	GatewayPID   *int       `json:"gateway_pid,omitempty"`
	State        string     `json:"state" binding:"required"`
	Generation   int        `json:"generation" binding:"required"`
	ErrorMessage *string    `json:"error_message,omitempty"`
	HealthAt     *time.Time `json:"health_at,omitempty"`
}

func NewRuntimeAgentHandler(cfg config.RuntimePoolConfig, podRepo repository.RuntimePodRepository, bindingRepo repository.InstanceRuntimeBindingRepository, instanceRepo repository.InstanceRepository, events runtimeEventPublisher) *RuntimeAgentHandler {
	return &RuntimeAgentHandler{
		cfg:          cfg,
		podRepo:      podRepo,
		bindingRepo:  bindingRepo,
		instanceRepo: instanceRepo,
		events:       events,
	}
}

func (h *RuntimeAgentHandler) Register(c *gin.Context) {
	if !h.requireAgentToken(c) {
		return
	}
	var req runtimeAgentRegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	runtimeType, ok := services.NormalizeV2RuntimeType(req.RuntimeType)
	if !ok {
		utils.Error(c, http.StatusBadRequest, "unsupported runtime type")
		return
	}
	state := strings.TrimSpace(req.State)
	if state == "" {
		state = "ready"
	}
	capacity := runtimePodCapacityFromReport(req.Capacity, h.cfg.MaxGatewaysPerPod)
	lastSeen := time.Now().UTC()
	if req.ReportedAt != nil && !req.ReportedAt.IsZero() {
		lastSeen = req.ReportedAt.UTC()
	}
	var metricsJSON *string
	if len(req.Metrics) > 0 {
		if !json.Valid(req.Metrics) {
			utils.Error(c, http.StatusBadRequest, "metrics must be valid JSON")
			return
		}
		raw := string(req.Metrics)
		metricsJSON = &raw
	}
	pod := &models.RuntimePod{
		RuntimeType:     runtimeType,
		Namespace:       strings.TrimSpace(req.Namespace),
		PodName:         strings.TrimSpace(req.PodName),
		PodUID:          trimStringPtr(req.PodUID),
		PodIP:           trimStringPtr(req.PodIP),
		NodeName:        trimStringPtr(req.NodeName),
		DeploymentName:  strings.TrimSpace(req.DeploymentName),
		ImageRef:        strings.TrimSpace(req.ImageRef),
		AgentEndpoint:   trimStringPtr(req.AgentEndpoint),
		State:           state,
		Capacity:        capacity,
		UsedSlots:       req.UsedSlots,
		Draining:        req.Draining,
		MetricsJSON:     metricsJSON,
		LastSeenAt:      &lastSeen,
		CPUMillisUsed:   0,
		MemoryBytesUsed: 0,
		DiskBytesUsed:   0,
		NetworkRXBytes:  0,
		NetworkTXBytes:  0,
	}
	if err := h.podRepo.UpsertFromAgent(c.Request.Context(), pod); err != nil {
		utils.HandleError(c, err)
		return
	}
	h.publish(c.Request.Context(), "runtime_pod_state", map[string]any{
		"pod_id":       pod.ID,
		"runtime_type": runtimeType,
		"namespace":    pod.Namespace,
		"pod_name":     pod.PodName,
		"state":        state,
		"used_slots":   req.UsedSlots,
		"capacity":     capacity,
		"draining":     req.Draining,
		"last_seen_at": lastSeen,
	})
	utils.Success(c, http.StatusOK, "Runtime pod registered successfully", gin.H{"pod": pod})
}

func (h *RuntimeAgentHandler) Heartbeat(c *gin.Context) {
	if !h.requireAgentToken(c) {
		return
	}
	var req runtimeAgentHeartbeatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	podID, ok := h.resolvePodID(c, req.runtimeAgentPodIdentity)
	if !ok {
		return
	}
	lastSeen := time.Now().UTC()
	if req.ReportedAt != nil && !req.ReportedAt.IsZero() {
		lastSeen = req.ReportedAt.UTC()
	}
	capacity := runtimePodCapacityFromReport(0, h.cfg.MaxGatewaysPerPod)
	if err := h.podRepo.UpdateHeartbeat(c.Request.Context(), podID, strings.TrimSpace(req.State), req.UsedSlots, capacity, req.Draining, lastSeen); err != nil {
		utils.HandleError(c, err)
		return
	}
	h.publish(c.Request.Context(), "runtime_pod_state", map[string]any{
		"pod_id":       podID,
		"state":        strings.TrimSpace(req.State),
		"used_slots":   req.UsedSlots,
		"capacity":     capacity,
		"draining":     req.Draining,
		"last_seen_at": lastSeen,
	})
	utils.Success(c, http.StatusOK, "Runtime pod heartbeat accepted", nil)
}

func (h *RuntimeAgentHandler) ReportMetrics(c *gin.Context) {
	if !h.requireAgentToken(c) {
		return
	}
	var req runtimeAgentMetricsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	podID, ok := h.resolvePodID(c, req.runtimeAgentPodIdentity)
	if !ok {
		return
	}
	var metricsJSON *string
	if len(req.Metrics) > 0 {
		if !json.Valid(req.Metrics) {
			utils.Error(c, http.StatusBadRequest, "metrics must be valid JSON")
			return
		}
		raw := string(req.Metrics)
		metricsJSON = &raw
	}
	lastSeen := time.Now().UTC()
	if req.ReportedAt != nil && !req.ReportedAt.IsZero() {
		lastSeen = req.ReportedAt.UTC()
	}
	update := repository.RuntimePodMetricsUpdate{
		CPUMillisUsed:   req.CPUMillisUsed,
		MemoryBytesUsed: req.MemoryBytesUsed,
		DiskBytesUsed:   req.DiskBytesUsed,
		NetworkRXBytes:  req.NetworkRXBytes,
		NetworkTXBytes:  req.NetworkTXBytes,
		MetricsJSON:     metricsJSON,
		LastSeenAt:      &lastSeen,
	}
	if err := h.podRepo.UpdateMetrics(c.Request.Context(), podID, update); err != nil {
		utils.HandleError(c, err)
		return
	}
	payload := map[string]any{
		"pod_id":            podID,
		"cpu_millis_used":   req.CPUMillisUsed,
		"memory_bytes_used": req.MemoryBytesUsed,
		"disk_bytes_used":   req.DiskBytesUsed,
		"network_rx_bytes":  req.NetworkRXBytes,
		"network_tx_bytes":  req.NetworkTXBytes,
		"last_seen_at":      lastSeen,
	}
	if metricsJSON != nil {
		payload["metrics"] = json.RawMessage(*metricsJSON)
	}
	h.publish(c.Request.Context(), "runtime_pod_metrics", payload)
	utils.Success(c, http.StatusOK, "Runtime pod metrics accepted", nil)
}

func (h *RuntimeAgentHandler) ReportGateways(c *gin.Context) {
	if !h.requireAgentToken(c) {
		return
	}
	var req runtimeAgentGatewaysRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	podID, ok := h.resolvePodID(c, req.runtimeAgentPodIdentity)
	if !ok {
		return
	}
	for _, gateway := range req.Gateways {
		binding, err := h.bindingRepo.GetByInstanceID(c.Request.Context(), gateway.InstanceID)
		if err != nil {
			utils.HandleError(c, err)
			return
		}
		if binding == nil || binding.RuntimePodID != podID || binding.Generation != gateway.Generation {
			continue
		}
		state := strings.ToLower(strings.TrimSpace(gateway.State))
		switch state {
		case "running", "ready", "healthy":
			if err := h.bindingRepo.UpdateRunning(c.Request.Context(), gateway.InstanceID, gateway.Generation, strings.TrimSpace(gateway.GatewayID), gateway.GatewayPort, gateway.GatewayPID); err != nil {
				utils.HandleError(c, err)
				return
			}
		default:
			if err := h.bindingRepo.UpdateState(c.Request.Context(), gateway.InstanceID, gateway.Generation, state, gateway.ErrorMessage); err != nil {
				utils.HandleError(c, err)
				return
			}
		}
		if err := h.syncInstanceRuntimeState(c.Request.Context(), gateway, state); err != nil {
			utils.HandleError(c, err)
			return
		}
	}
	h.publish(c.Request.Context(), "runtime_pod_gateways_reported", map[string]any{
		"pod_id":        podID,
		"gateway_count": len(req.Gateways),
	})
	utils.Success(c, http.StatusOK, "Runtime gateway report accepted", nil)
}

func (h *RuntimeAgentHandler) syncInstanceRuntimeState(ctx context.Context, gateway runtimeAgentGatewayReport, state string) error {
	if h.instanceRepo == nil {
		return nil
	}
	instanceState, message := instanceRuntimeStateFromGatewayReport(state, gateway.ErrorMessage)
	return h.instanceRepo.UpdateRuntimeState(ctx, gateway.InstanceID, instanceState, gateway.Generation, message)
}

func instanceRuntimeStateFromGatewayReport(state string, errorMessage *string) (string, *string) {
	normalized := strings.ToLower(strings.TrimSpace(state))
	switch normalized {
	case "running", "ready", "healthy":
		return "running", nil
	case "error", "failed", "failure", "errored":
		if errorMessage != nil && strings.TrimSpace(*errorMessage) != "" {
			msg := strings.TrimSpace(*errorMessage)
			return "error", &msg
		}
		msg := "runtime gateway reported " + normalized
		return "error", &msg
	case "stopped", "deleted":
		return "stopped", nil
	default:
		msg := "runtime gateway starting"
		if errorMessage != nil && strings.TrimSpace(*errorMessage) != "" {
			msg = strings.TrimSpace(*errorMessage)
		}
		return "creating", &msg
	}
}

func (h *RuntimeAgentHandler) ReportSkills(c *gin.Context) {
	if !h.requireAgentToken(c) {
		return
	}
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		utils.ValidationError(c, err)
		return
	}
	h.publish(c.Request.Context(), "runtime_agent_skills_reported", payload)
	utils.Success(c, http.StatusOK, "Runtime agent skills report accepted", nil)
}

func (h *RuntimeAgentHandler) requireAgentToken(c *gin.Context) bool {
	if strings.TrimSpace(h.cfg.AgentReportToken) == "" || c.GetHeader(runtimeAgentTokenHeader) != h.cfg.AgentReportToken {
		utils.Error(c, http.StatusUnauthorized, "invalid runtime agent token")
		return false
	}
	return true
}

func (h *RuntimeAgentHandler) resolvePodID(c *gin.Context, identity runtimeAgentPodIdentity) (int64, bool) {
	if identity.PodID > 0 {
		return identity.PodID, true
	}
	namespace := strings.TrimSpace(identity.Namespace)
	podName := strings.TrimSpace(identity.PodName)
	if namespace == "" || podName == "" {
		utils.Error(c, http.StatusBadRequest, "pod_id or namespace and pod_name are required")
		return 0, false
	}
	pod, err := h.podRepo.GetByNamespaceName(c.Request.Context(), namespace, podName)
	if err != nil {
		utils.HandleError(c, err)
		return 0, false
	}
	if pod == nil {
		utils.Error(c, http.StatusNotFound, "runtime pod not found")
		return 0, false
	}
	return pod.ID, true
}

func (h *RuntimeAgentHandler) publish(ctx context.Context, eventType string, payload any) {
	if h.events == nil {
		return
	}
	_ = h.events.Publish(ctx, eventType, payload)
}

func runtimePodCapacityFromReport(reported, configured int) int {
	if configured > 0 {
		return configured
	}
	capacity := reported
	if capacity <= 0 {
		capacity = services.RuntimePodCapacity
	}
	return capacity
}

func trimStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
