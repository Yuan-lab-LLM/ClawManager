package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
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
	skillService services.SkillService
}

type runtimeAgentPodIdentity struct {
	PodID     int64  `json:"pod_id"`
	Namespace string `json:"namespace"`
	PodName   string `json:"pod_name"`
}

type runtimeAgentRegisterRequest struct {
	RuntimeType       string          `json:"runtime_type" binding:"required"`
	Namespace         string          `json:"namespace" binding:"required"`
	PodName           string          `json:"pod_name" binding:"required"`
	PodUID            *string         `json:"pod_uid,omitempty"`
	PodIP             *string         `json:"pod_ip,omitempty"`
	NodeName          *string         `json:"node_name,omitempty"`
	DeploymentName    string          `json:"deployment_name" binding:"required"`
	ImageRef          string          `json:"image_ref" binding:"required"`
	OpenClawVersion   string          `json:"openclaw_version,omitempty"`
	ProtocolVersion   string          `json:"protocol_version,omitempty"`
	TeamPluginVersion string          `json:"team_plugin_version,omitempty"`
	SessionStore      string          `json:"session_store,omitempty"`
	ImageDigest       string          `json:"image_digest,omitempty"`
	Capabilities      []string        `json:"capabilities,omitempty"`
	AgentEndpoint     *string         `json:"agent_endpoint,omitempty"`
	State             string          `json:"state"`
	Capacity          int             `json:"capacity"`
	UsedSlots         int             `json:"used_slots"`
	Draining          bool            `json:"draining"`
	Metrics           json.RawMessage `json:"metrics,omitempty"`
	ReportedAt        *time.Time      `json:"reported_at,omitempty"`
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
	InstanceID     int        `json:"instance_id" binding:"required"`
	GatewayID      string     `json:"gateway_id"`
	GatewayPort    int        `json:"gateway_port"`
	GatewayPID     *int       `json:"gateway_pid,omitempty"`
	WorkspacePath  string     `json:"workspace_path,omitempty"`
	State          string     `json:"state" binding:"required"`
	Generation     int        `json:"generation" binding:"required"`
	ErrorMessage   *string    `json:"error_message,omitempty"`
	FailureClass   string     `json:"failure_class,omitempty"`
	ExitCode       *int       `json:"exit_code,omitempty"`
	Retryable      *bool      `json:"retryable,omitempty"`
	RestartAttempt int        `json:"restart_attempt,omitempty"`
	HealthAt       *time.Time `json:"health_at,omitempty"`
}

func NewRuntimeAgentHandler(cfg config.RuntimePoolConfig, podRepo repository.RuntimePodRepository, bindingRepo repository.InstanceRuntimeBindingRepository, instanceRepo repository.InstanceRepository, events runtimeEventPublisher, skillService services.SkillService) *RuntimeAgentHandler {
	return &RuntimeAgentHandler{
		cfg:          cfg,
		podRepo:      podRepo,
		bindingRepo:  bindingRepo,
		instanceRepo: instanceRepo,
		events:       events,
		skillService: skillService,
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
	// Liveness is measured at the control plane. Agent clocks may drift and a
	// request can sit behind a slow database or network path before it arrives.
	// Using reported_at here can make a newly received report look stale and
	// trigger a destructive failover immediately.
	lastSeen := time.Now().UTC()
	var metricsJSON *string
	if len(req.Metrics) > 0 {
		if !json.Valid(req.Metrics) {
			utils.Error(c, http.StatusBadRequest, "metrics must be valid JSON")
			return
		}
		raw := string(req.Metrics)
		metricsJSON = &raw
	}
	var capabilitiesJSON *string
	if len(req.Capabilities) > 0 {
		raw, err := json.Marshal(normalizeRuntimeCapabilities(req.Capabilities))
		if err != nil {
			utils.Error(c, http.StatusBadRequest, "capabilities must be valid")
			return
		}
		encoded := string(raw)
		capabilitiesJSON = &encoded
	}
	pod := &models.RuntimePod{
		RuntimeType:          runtimeType,
		Namespace:            strings.TrimSpace(req.Namespace),
		PodName:              strings.TrimSpace(req.PodName),
		PodUID:               trimStringPtr(req.PodUID),
		PodIP:                trimStringPtr(req.PodIP),
		NodeName:             trimStringPtr(req.NodeName),
		DeploymentName:       strings.TrimSpace(req.DeploymentName),
		ImageRef:             strings.TrimSpace(req.ImageRef),
		OpenClawVersion:      trimStringValuePtr(req.OpenClawVersion),
		AgentProtocolVersion: trimStringValuePtr(req.ProtocolVersion),
		TeamPluginVersion:    trimStringValuePtr(req.TeamPluginVersion),
		SessionStore:         trimStringValuePtr(req.SessionStore),
		ImageDigest:          trimStringValuePtr(req.ImageDigest),
		CapabilitiesJSON:     capabilitiesJSON,
		AgentEndpoint:        trimStringPtr(req.AgentEndpoint),
		State:                state,
		Capacity:             capacity,
		UsedSlots:            req.UsedSlots,
		Draining:             req.Draining,
		MetricsJSON:          metricsJSON,
		LastSeenAt:           &lastSeen,
		CPUMillisUsed:        0,
		MemoryBytesUsed:      0,
		DiskBytesUsed:        0,
		NetworkRXBytes:       0,
		NetworkTXBytes:       0,
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
		"reported_at":  req.ReportedAt,
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
		"reported_at":  req.ReportedAt,
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
		"reported_at":       req.ReportedAt,
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
	reported := make(map[int]int, len(req.Gateways))
	for _, gateway := range req.Gateways {
		reported[gateway.InstanceID] = gateway.Generation
		binding, err := h.bindingRepo.GetByInstanceID(c.Request.Context(), gateway.InstanceID)
		if err != nil {
			utils.HandleError(c, err)
			return
		}
		if binding == nil || pendingBindingCanYieldToReportedGateway(binding, podID, gateway) {
			reconciled, reconcileErr := h.reconcileReportedOpenClawGateway(c.Request.Context(), podID, gateway)
			if reconcileErr != nil {
				utils.HandleError(c, reconcileErr)
				return
			}
			if !reconciled {
				continue
			}
			binding, err = h.bindingRepo.GetByInstanceID(c.Request.Context(), gateway.InstanceID)
			if err != nil {
				utils.HandleError(c, err)
				return
			}
		}
		if binding == nil || binding.RuntimePodID != podID || binding.Generation != gateway.Generation {
			continue
		}
		lifecycle := services.NormalizeRuntimeGatewayLifecycle(gateway.State, gateway.ErrorMessage)
		if lifecycle.Running {
			if err := h.bindingRepo.UpdateRunning(c.Request.Context(), gateway.InstanceID, gateway.Generation, strings.TrimSpace(gateway.GatewayID), gateway.GatewayPort, gateway.GatewayPID); err != nil {
				if errors.Is(err, models.ErrRuntimeUpgradeInProgress) {
					continue
				}
				utils.HandleError(c, err)
				return
			}
		} else {
			if err := h.bindingRepo.UpdateState(c.Request.Context(), gateway.InstanceID, gateway.Generation, lifecycle.BindingState, lifecycle.Message); err != nil {
				if errors.Is(err, models.ErrRuntimeUpgradeInProgress) {
					continue
				}
				utils.HandleError(c, err)
				return
			}
		}
		if err := h.syncInstanceRuntimeState(c.Request.Context(), gateway, lifecycle); err != nil {
			if errors.Is(err, models.ErrRuntimeUpgradeInProgress) || errors.Is(err, repository.ErrStaleRuntimeGeneration) {
				continue
			}
			utils.HandleError(c, err)
			return
		}
	}
	bindings, err := h.bindingRepo.ListByRuntimePodID(c.Request.Context(), podID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	now := time.Now().UTC()
	for _, binding := range bindings {
		if _, reportedByInstance := reported[binding.InstanceID]; reportedByInstance {
			continue
		}
		if !h.missingGatewayCleanupEligible(binding, now) {
			if !h.missingPendingGatewayCleanupEligible(binding, now) {
				continue
			}
			releaser, ok := h.bindingRepo.(repository.PendingGatewayBindingReleaser)
			if !ok {
				continue
			}
			if _, err := releaser.DeletePendingByInstanceIDGenerationAndReleaseSlot(c.Request.Context(), binding.InstanceID, podID, binding.Generation, now.Add(-pendingGatewayReconcileGrace(h.cfg.HeartbeatTimeout))); err != nil {
				utils.HandleError(c, err)
				return
			}
			continue
		}
		if _, err := h.bindingRepo.DeleteRunningByInstanceIDGenerationAndReleaseSlot(c.Request.Context(), binding.InstanceID, podID, binding.Generation); err != nil {
			if errors.Is(err, models.ErrRuntimeUpgradeInProgress) {
				continue
			}
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

func pendingBindingCanYieldToReportedGateway(binding *models.InstanceRuntimeBinding, podID int64, gateway runtimeAgentGatewayReport) bool {
	if binding == nil {
		return false
	}
	expectedPendingID := fmt.Sprintf("pending-%d-%d", gateway.InstanceID, binding.Generation)
	return binding.RuntimePodID == podID &&
		(binding.Generation == gateway.Generation || binding.Generation == gateway.Generation+1) &&
		strings.EqualFold(strings.TrimSpace(binding.State), services.RuntimeGatewayBindingCreating) &&
		strings.TrimSpace(binding.GatewayID) == expectedPendingID
}

func (h *RuntimeAgentHandler) reconcileReportedOpenClawGateway(ctx context.Context, podID int64, gateway runtimeAgentGatewayReport) (bool, error) {
	if h == nil || h.instanceRepo == nil || h.podRepo == nil || h.bindingRepo == nil {
		return false, nil
	}
	reconciler, ok := h.bindingRepo.(repository.ReportedGatewayBindingReconciler)
	if !ok {
		return false, nil
	}
	pod, err := h.podRepo.GetByID(ctx, podID)
	if err != nil || pod == nil {
		return false, err
	}
	if !strings.EqualFold(strings.TrimSpace(pod.RuntimeType), services.RuntimeTypeOpenClaw) || !runtimePodHasCapability(*pod, "openclaw.upgrade-preflight-v3") {
		return false, nil
	}
	instance, err := h.instanceRepo.GetByID(gateway.InstanceID)
	if err != nil || instance == nil {
		return false, err
	}
	status := strings.ToLower(strings.TrimSpace(instance.Status))
	if !strings.EqualFold(strings.TrimSpace(instance.Type), services.RuntimeTypeOpenClaw) || !strings.EqualFold(strings.TrimSpace(instance.InstanceMode), services.InstanceModeLite) || (status != "error" && status != "creating") {
		return false, nil
	}
	expectedGatewayID := fmt.Sprintf("gw-%d-%d", gateway.InstanceID, gateway.Generation)
	expectedWorkspace := services.RuntimeWorkspacePathWithRoot(h.cfg.WorkspaceRoot, services.RuntimeTypeOpenClaw, instance.UserID, instance.ID)
	actualWorkspace := strings.TrimSpace(gateway.WorkspacePath)
	if gateway.Generation <= 0 || (instance.RuntimeGeneration != gateway.Generation && instance.RuntimeGeneration != gateway.Generation+1) || strings.TrimSpace(gateway.GatewayID) != expectedGatewayID || actualWorkspace == "" || !sameRuntimePath(actualWorkspace, expectedWorkspace) {
		return false, nil
	}
	lifecycle := services.NormalizeRuntimeGatewayLifecycle(gateway.State, gateway.ErrorMessage)
	if !lifecycle.Running || gateway.GatewayPort <= 0 {
		return false, nil
	}
	healthAt := gateway.HealthAt
	if healthAt == nil {
		now := time.Now().UTC()
		healthAt = &now
	}
	return reconciler.ReconcileReportedGateway(ctx, &models.InstanceRuntimeBinding{
		InstanceID: gateway.InstanceID, RuntimePodID: podID, RuntimeType: services.RuntimeTypeOpenClaw,
		GatewayID: gateway.GatewayID, GatewayPort: gateway.GatewayPort, GatewayPID: gateway.GatewayPID,
		WorkspacePath: expectedWorkspace, State: services.RuntimeGatewayBindingRunning, Generation: gateway.Generation, LastHealthAt: healthAt,
	}, instance.RuntimeGeneration, services.RuntimeGatewayPortBlockSize(services.RuntimeTypeOpenClaw))
}

func runtimePodHasCapability(pod models.RuntimePod, required string) bool {
	for _, capability := range pod.Capabilities() {
		if strings.TrimSpace(capability) == required {
			return true
		}
	}
	return false
}

func sameRuntimePath(left, right string) bool {
	return filepath.Clean(strings.TrimSpace(left)) == filepath.Clean(strings.TrimSpace(right))
}

func (h *RuntimeAgentHandler) missingGatewayCleanupEligible(binding models.InstanceRuntimeBinding, now time.Time) bool {
	if h == nil || h.cfg.HeartbeatTimeout <= 0 || binding.LastHealthAt == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(binding.State), services.RuntimeGatewayBindingRunning) {
		return false
	}
	return !binding.LastHealthAt.After(now.Add(-h.cfg.HeartbeatTimeout))
}

func (h *RuntimeAgentHandler) missingPendingGatewayCleanupEligible(binding models.InstanceRuntimeBinding, now time.Time) bool {
	if h == nil || h.cfg.HeartbeatTimeout <= 0 || binding.UpdatedAt.IsZero() {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(binding.State), services.RuntimeGatewayBindingCreating) || strings.TrimSpace(binding.GatewayID) != fmt.Sprintf("pending-%d-%d", binding.InstanceID, binding.Generation) {
		return false
	}
	return !binding.UpdatedAt.After(now.Add(-pendingGatewayReconcileGrace(h.cfg.HeartbeatTimeout)))
}

func pendingGatewayReconcileGrace(heartbeatTimeout time.Duration) time.Duration {
	grace := 4 * heartbeatTimeout
	if grace < 45*time.Second {
		grace = 45 * time.Second
	}
	return grace
}

func (h *RuntimeAgentHandler) syncInstanceRuntimeState(ctx context.Context, gateway runtimeAgentGatewayReport, lifecycle services.RuntimeGatewayLifecycleState) error {
	if h.instanceRepo == nil {
		return nil
	}
	return h.instanceRepo.UpdateRuntimeState(ctx, gateway.InstanceID, lifecycle.InstanceState, gateway.Generation, lifecycle.Message)
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
	if !h.cfg.SkillReportPersistence {
		utils.Success(c, http.StatusOK, "Runtime agent skills report accepted without persistence", gin.H{"persisted": false})
		return
	}
	if h.skillService != nil {
		if err := h.skillService.SyncRuntimeAgentSkillsReport(payload); err != nil {
			utils.HandleError(c, err)
			return
		}
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

func trimStringValuePtr(value string) *string {
	return trimStringPtr(&value)
}

func normalizeRuntimeCapabilities(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
