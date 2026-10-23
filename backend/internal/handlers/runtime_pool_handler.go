package handlers

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

type RuntimePoolHandler struct {
	podRepo     repository.RuntimePodRepository
	bindingRepo repository.InstanceRuntimeBindingRepository
	rolloutRepo repository.RuntimeRolloutRepository
	scheduler   *services.RuntimeScheduler
	events      runtimeEventPublisher
}

const (
	runtimePodListFallbackHeartbeatTimeout = 10 * time.Second
	runtimePodListStaleWindowMultiplier    = 3
)

type startRuntimeRolloutRequest struct {
	RuntimeType    string `json:"runtime_type" binding:"required"`
	TargetImageRef string `json:"target_image_ref" binding:"required"`
	BatchSize      int    `json:"batch_size"`
	MaxUnavailable int    `json:"max_unavailable"`
}

type runtimePoolPodListItem struct {
	models.RuntimePod
	AgentReported bool `json:"agent_reported"`
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
	rollout := &models.RuntimeRollout{
		RuntimeType:    runtimeType,
		TargetImageRef: targetImage,
		Status:         "pending",
		BatchSize:      batchSize,
		MaxUnavailable: maxUnavailable,
		StartedBy:      startedBy,
	}
	if err := h.rolloutRepo.Create(c.Request.Context(), rollout); err != nil {
		utils.HandleError(c, err)
		return
	}
	if h.scheduler != nil {
		if err := h.scheduler.StartRollout(c.Request.Context(), rollout.ID); err != nil {
			utils.HandleError(c, err)
			return
		}
	}
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
		})
	}
	return items
}

func mergeRuntimePoolDeploymentPods(items []runtimePoolPodListItem, deploymentPods []models.RuntimePod) []runtimePoolPodListItem {
	seen := map[string]struct{}{}
	for _, item := range items {
		seen[runtimePoolPodKey(item.Namespace, item.PodName)] = struct{}{}
	}
	for _, pod := range deploymentPods {
		key := runtimePoolPodKey(pod.Namespace, pod.PodName)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, runtimePoolPodListItem{
			RuntimePod:    pod,
			AgentReported: false,
		})
	}
	return items
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
