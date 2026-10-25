package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/heartbeat"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"clawreef/internal/services/k8s"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

type RuntimePoolHandler struct {
	podRepo            repository.RuntimePodRepository
	bindingRepo        repository.InstanceRuntimeBindingRepository
	rolloutRepo        repository.RuntimeRolloutRepository
	scheduler          *services.RuntimeScheduler
	events             runtimeEventPublisher
	upgrade            *services.RuntimeUpgradeService
	hermesWebInspector func(context.Context, string) (bool, error)
}

const (
	runtimePodListFallbackHeartbeatTimeout = heartbeat.Timeout
	runtimePodListStaleWindowMultiplier    = 3
)

type startRuntimeRolloutRequest struct {
	RuntimeType    string `json:"runtime_type" binding:"required"`
	TargetImageRef string `json:"target_image_ref" binding:"required"`
	BatchSize      int    `json:"batch_size"`
	MaxUnavailable int    `json:"max_unavailable"`
	PreflightID    string `json:"preflight_id"`
	AutoRollback   *bool  `json:"auto_rollback,omitempty"`
}

type runtimeUpgradePreflightRequest struct {
	TargetImageRef     string `json:"target_image_ref" binding:"required"`
	BatchSize          int    `json:"batch_size"`
	MaxUnavailable     int    `json:"max_unavailable"`
	AutoRollback       *bool  `json:"auto_rollback,omitempty"`
	ExcludeInstanceIDs []int  `json:"exclude_instance_ids,omitempty"`
}

type runtimePoolPodListItem struct {
	models.RuntimePod
	AgentReported bool     `json:"agent_reported"`
	Capabilities  []string `json:"capabilities"`
}

func (h *RuntimePoolHandler) SetUpgradeService(service *services.RuntimeUpgradeService) {
	h.upgrade = service
}

func (h *RuntimePoolHandler) PreflightOpenClawRollout(c *gin.Context) {
	if h.upgrade == nil {
		utils.Error(c, http.StatusServiceUnavailable, "runtime upgrade service is unavailable")
		return
	}
	var req runtimeUpgradePreflightRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	autoRollback := true
	if req.AutoRollback != nil {
		autoRollback = *req.AutoRollback
	}
	result, err := h.upgrade.Preflight(c.Request.Context(), services.RuntimeUpgradePreflightRequest{
		TargetImageRef: strings.TrimSpace(req.TargetImageRef), BatchSize: req.BatchSize,
		MaxUnavailable: req.MaxUnavailable, AutoRollback: autoRollback, ActorUserID: currentUserIDPtr(c),
		ExcludeInstanceIDs: req.ExcludeInstanceIDs,
	})
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "OpenClaw runtime rollout preflight completed", result)
}

func (h *RuntimePoolHandler) GetRollout(c *gin.Context) {
	if h.upgrade == nil {
		utils.Error(c, http.StatusServiceUnavailable, "runtime upgrade service is unavailable")
		return
	}
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id <= 0 {
		utils.Error(c, http.StatusBadRequest, "invalid rollout id")
		return
	}
	details, err := h.upgrade.Details(c.Request.Context(), id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	if details == nil {
		utils.Error(c, http.StatusNotFound, "runtime rollout not found")
		return
	}
	progress, err := services.LoadRuntimePoolProgress(c.Request.Context(), h.rolloutRepo, id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Runtime rollout retrieved successfully", gin.H{
		"rollout": details.Rollout, "items": details.Items, "audits": details.Audits, "pool_progress": progress, "execution": details.Execution,
	})
}

func NewRuntimePoolHandler(
	podRepo repository.RuntimePodRepository,
	bindingRepo repository.InstanceRuntimeBindingRepository,
	rolloutRepo repository.RuntimeRolloutRepository,
	scheduler *services.RuntimeScheduler,
	events runtimeEventPublisher,
) *RuntimePoolHandler {
	return &RuntimePoolHandler{
		podRepo:     podRepo,
		bindingRepo: bindingRepo,
		rolloutRepo: rolloutRepo,
		scheduler:   scheduler,
		events:      events,
	}
}

func (h *RuntimePoolHandler) ListPods(c *gin.Context) {
	runtimeType := strings.TrimSpace(c.Query("runtime_type"))
	if runtimeType != "" {
		normalized, ok := services.NormalizeV2RuntimeType(runtimeType)
		if !ok {
			utils.Error(c, http.StatusBadRequest, "unsupported runtime type")
			return
		}
		runtimeType = normalized
	}
	pods, err := h.podRepo.List(c.Request.Context(), runtimeType)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	currentPods := filterCurrentRuntimePods(pods, time.Now().UTC(), h.runtimePodListHeartbeatTimeout())
	items := runtimePoolPodListItems(currentPods, true)
	if h.scheduler != nil {
		deploymentPods, err := h.scheduler.RuntimeDeploymentPods(c.Request.Context(), runtimeType)
		if err != nil {
			log.Printf("runtime pool list deployment pods failed: %v", err)
		} else {
			items = mergeRuntimePoolDeploymentPods(items, deploymentPods)
		}
	}
	utils.Success(c, http.StatusOK, "Runtime pods retrieved successfully", gin.H{"pods": items})
}

func (h *RuntimePoolHandler) GetPodGateways(c *gin.Context) {
	podID, ok := parseRuntimePodID(c)
	if !ok {
		return
	}
	bindings, err := h.bindingRepo.ListByRuntimePodID(c.Request.Context(), podID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Runtime pod gateways retrieved successfully", gin.H{"gateways": bindings})
}

func (h *RuntimePoolHandler) DrainPod(c *gin.Context) {
	podID, ok := parseRuntimePodID(c)
	if !ok {
		return
	}
	if h.scheduler != nil {
		if err := h.scheduler.DrainPod(c.Request.Context(), podID); err != nil {
			utils.HandleError(c, err)
			return
		}
	} else if err := h.podRepo.MarkState(c.Request.Context(), podID, "draining", true); err != nil {
		utils.HandleError(c, err)
		return
	}
	h.publish(c.Request.Context(), "runtime_pod_state", map[string]any{
		"pod_id":   podID,
		"state":    "draining",
		"draining": true,
	})
	utils.Success(c, http.StatusOK, "Runtime pod drain started", nil)
}

func (h *RuntimePoolHandler) StartRollout(c *gin.Context) {
	var req startRuntimeRolloutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	runtimeType, ok := services.NormalizeV2RuntimeType(req.RuntimeType)
	if !ok {
		utils.Error(c, http.StatusBadRequest, "unsupported runtime type")
		return
	}
	targetImage := strings.TrimSpace(req.TargetImageRef)
	if runtimeType == "opencode" {
		utils.Error(c, http.StatusConflict, models.ErrOpenCodeSafeUpgradeRequired.Error())
		return
	}
	if targetImage == "" {
		utils.Error(c, http.StatusBadRequest, "target image ref is required")
		return
	}
	batchSize := req.BatchSize
	if batchSize <= 0 {
		batchSize = 1
	}
	maxUnavailable := req.MaxUnavailable
	if maxUnavailable <= 0 {
		maxUnavailable = 1
	}
	startedBy := currentUserIDPtr(c)
	rolloutPhase := "requested"
	var sourceImagesJSON *string
	var targetImageDigest *string
	if runtimeType == services.RuntimeTypeHermes {
		resolved, digest, err := services.ResolveHermesRolloutImage(c.Request.Context(), targetImage)
		if err != nil {
			utils.Error(c, http.StatusConflict, err.Error())
			return
		}
		targetImage, targetImageDigest = resolved, &digest
		inspect := h.hermesWebInspector
		if inspect == nil {
			inspect = services.HermesImageNeedsWebTrust
		}
		web, err := inspect(c.Request.Context(), targetImage)
		if err != nil {
			utils.Error(c, http.StatusConflict, err.Error())
			return
		}
		if web {
			rolloutPhase = k8s.HermesWebRolloutPhase
		}
	}
	if runtimeType == services.RuntimeTypeOpenClaw {
		if h.upgrade == nil {
			utils.Error(c, http.StatusServiceUnavailable, "runtime upgrade service is unavailable")
			return
		}
		if strings.TrimSpace(req.PreflightID) != "" {
			rollout, err := h.upgrade.ConfirmPreflight(c.Request.Context(), req.PreflightID, targetImage, startedBy)
			if err != nil {
				utils.Error(c, http.StatusConflict, err.Error())
				return
			}
			if h.scheduler != nil {
				if err := h.scheduler.StartRollout(c.Request.Context(), rollout.ID); err != nil {
					utils.HandleError(c, err)
					return
				}
			}
			h.publish(c.Request.Context(), "runtime_rollout", map[string]any{"rollout_id": rollout.ID, "runtime_type": rollout.RuntimeType, "target_image_ref": rollout.TargetImageRef, "status": rollout.Status, "phase": rollout.Phase})
			utils.Success(c, http.StatusCreated, "Runtime rollout created successfully", gin.H{"rollout": rollout})
			return
		}
		classification, err := h.upgrade.ClassifyOpenClawTarget(c.Request.Context(), targetImage)
		if err != nil {
			utils.Error(c, http.StatusConflict, err.Error())
			return
		}
		if classification.Strategy == services.RuntimeUpgradeStrategyOpenClawDataSafe {
			utils.Error(c, http.StatusConflict, "OpenClaw 2026.8.1 or newer requires a successful data-safe preflight")
			return
		}
		// A recognized pre-8.1 OpenClaw target deliberately rejoins the
		// unchanged generic Lite rolling-update path below.
		targetImage = classification.ImageRef
		if classification.EmptyPoolReset {
			encoded, marshalErr := json.Marshal(classification.SourceImages)
			if marshalErr != nil {
				utils.Error(c, http.StatusInternalServerError, "failed to preserve the current OpenClaw pool image inventory")
				return
			}
			value := string(encoded)
			sourceImagesJSON = &value
			rolloutPhase = services.RuntimeUpgradePhaseEmptyPoolReset
		}
		if classification.ImageDigest != "" {
			value := classification.ImageDigest
			targetImageDigest = &value
		}
	}
	rollout := &models.RuntimeRollout{
		RuntimeType:       runtimeType,
		TargetImageRef:    targetImage,
		SourceImagesJSON:  sourceImagesJSON,
		TargetImageDigest: targetImageDigest,
		Status:            "pending",
		Phase:             rolloutPhase,
		BatchSize:         batchSize,
		MaxUnavailable:    maxUnavailable,
		StartedBy:         startedBy,
		AutoRollback:      req.AutoRollback == nil || *req.AutoRollback,
	}
	if err := h.rolloutRepo.Create(c.Request.Context(), rollout); err != nil {
		utils.HandleError(c, err)
		return
	}
	// The elected scheduler owns execution. A request on a follower must not
	// mutate runtime pools concurrently with the leader.
	h.publish(c.Request.Context(), "runtime_rollout", map[string]any{
		"rollout_id":       rollout.ID,
		"runtime_type":     rollout.RuntimeType,
		"target_image_ref": rollout.TargetImageRef,
		"status":           rollout.Status,
		"batch_size":       rollout.BatchSize,
		"max_unavailable":  rollout.MaxUnavailable,
		"started_by":       rollout.StartedBy,
	})
	utils.Success(c, http.StatusCreated, "Runtime rollout created successfully", gin.H{"rollout": rollout})
}

func (h *RuntimePoolHandler) ListRollouts(c *gin.Context) {
	repo, ok := h.rolloutRepo.(interface {
		ListRecent(context.Context, string) ([]models.RuntimeRollout, error)
	})
	if !ok {
		utils.Error(c, http.StatusServiceUnavailable, "Runtime rollout history unavailable")
		return
	}
	items, err := repo.ListRecent(c.Request.Context(), strings.TrimSpace(c.Query("runtime_type")))
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	active, err := h.rolloutRepo.ListActive(c.Request.Context(), strings.TrimSpace(c.Query("runtime_type")))
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	seen := make(map[int64]struct{}, len(items))
	for _, item := range items {
		seen[item.ID] = struct{}{}
	}
	for _, item := range active {
		if _, exists := seen[item.ID]; !exists {
			items = append(items, item)
		}
	}
	utils.Success(c, http.StatusOK, "Runtime rollout history", gin.H{"items": items})
}

func (h *RuntimePoolHandler) publish(ctx context.Context, eventType string, payload any) {
	if h.events == nil {
		return
	}
	_ = h.events.Publish(ctx, eventType, payload)
}

func (h *RuntimePoolHandler) runtimePodListHeartbeatTimeout() time.Duration {
	if h != nil && h.scheduler != nil {
		if timeout := h.scheduler.HeartbeatTimeout(); timeout > 0 {
			return timeout
		}
	}
	return runtimePodListFallbackHeartbeatTimeout
}

func filterCurrentRuntimePods(pods []models.RuntimePod, now time.Time, heartbeatTimeout time.Duration) []models.RuntimePod {
	if heartbeatTimeout <= 0 {
		return pods
	}
	cutoff := now.UTC().Add(-runtimePodListStaleWindowMultiplier * heartbeatTimeout)
	current := pods[:0]
	for _, pod := range pods {
		if pod.LastSeenAt != nil && pod.LastSeenAt.UTC().Before(cutoff) {
			continue
		}
		current = append(current, pod)
	}
	return current
}

func runtimePoolPodListItems(pods []models.RuntimePod, agentReported bool) []runtimePoolPodListItem {
	items := make([]runtimePoolPodListItem, 0, len(pods))
	for _, pod := range pods {
		items = append(items, runtimePoolPodListItem{
			RuntimePod:    pod,
			AgentReported: agentReported,
			Capabilities:  pod.Capabilities(),
		})
	}
	return items
}

func mergeRuntimePoolDeploymentPods(items []runtimePoolPodListItem, deploymentPods []models.RuntimePod) []runtimePoolPodListItem {
	if len(deploymentPods) == 0 {
		return items
	}
	reported := make(map[string]runtimePoolPodListItem, len(items))
	for _, item := range items {
		reported[runtimePoolPodKey(item.Namespace, item.PodName)] = item
	}
	merged := make([]runtimePoolPodListItem, 0, len(deploymentPods))
	seen := make(map[string]struct{}, len(deploymentPods))
	for _, pod := range deploymentPods {
		key := runtimePoolPodKey(pod.Namespace, pod.PodName)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if item, ok := reported[key]; ok {
			// Kubernetes owns pod existence and the image actually running. The
			// Agent owns capabilities, version, occupancy and heartbeat data.
			item.DeploymentName = pod.DeploymentName
			item.ImageRef = pod.ImageRef
			item.ImageDigest = pod.ImageDigest
			item.PoolRole = pod.PoolRole
			item.PoolPurpose = pod.PoolPurpose
			item.UpgradeID = pod.UpgradeID
			item.SourceDeployment = pod.SourceDeployment
			item.SchedulingEnabled = pod.SchedulingEnabled
			merged = append(merged, item)
			continue
		}
		merged = append(merged, runtimePoolPodListItem{
			RuntimePod:    pod,
			AgentReported: false,
			Capabilities:  pod.Capabilities(),
		})
	}
	return merged
}

func runtimePoolPodKey(namespace, podName string) string {
	namespace = strings.TrimSpace(namespace)
	podName = strings.TrimSpace(podName)
	if namespace == "" || podName == "" {
		return ""
	}
	return namespace + "/" + podName
}

func parseRuntimePodID(c *gin.Context) (int64, bool) {
	podID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || podID <= 0 {
		utils.Error(c, http.StatusBadRequest, "invalid runtime pod ID")
		return 0, false
	}
	return podID, true
}

func currentUserIDPtr(c *gin.Context) *int {
	raw, ok := c.Get("userID")
	if !ok {
		return nil
	}
	userID, ok := raw.(int)
	if !ok {
		return nil
	}
	return &userID
}
