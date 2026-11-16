package handlers

import (
	"context"
	"fmt"
	"time"

	"clawreef/internal/admindashboard"
	"clawreef/internal/models"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/upper/db/v4"
)

type AdminDashboardHandler struct{ store *admindashboard.Store }

func NewAdminDashboardHandler(sess db.Session, inventory ...*RuntimePoolHandler) *AdminDashboardHandler {
	h := &AdminDashboardHandler{store: admindashboard.NewStore(sess)}
	if len(inventory) > 0 && inventory[0] != nil {
		h.store.SetRuntimeReader(inventory[0].dashboardRuntimePods)
	}
	return h
}

// Reuse the Runtime management inventory: Kubernetes determines existence,
// registry rows matched by UID determine online status. Slots never enter counts.
func (h *RuntimePoolHandler) dashboardRuntimePods(ctx context.Context) ([]models.RuntimePod, error) {
	if h.scheduler == nil {
		return nil, fmt.Errorf("runtime inventory unavailable")
	}
	pods, err := h.podRepo.List(ctx, "")
	if err != nil {
		return nil, err
	}
	deploymentPods, err := h.scheduler.RuntimeDeploymentPods(ctx, "")
	if err != nil {
		return nil, err
	}
	items := mergeRuntimePoolDeploymentPods(runtimePoolPodListItems(filterCurrentRuntimePods(pods, time.Now().UTC(), h.runtimePodListHeartbeatTimeout()), true), deploymentPods)
	result := make([]models.RuntimePod, 0, len(items))
	for _, item := range items {
		pod := item.RuntimePod
		if !item.AgentReported {
			pod.State = "unknown"
		}
		result = append(result, pod)
	}
	return result, nil
}
func (h *AdminDashboardHandler) Get(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if role, _ := c.Get("userRole"); role != "admin" {
		utils.Error(c, 403, "Access denied")
		return
	}
	utils.Success(c, 200, "Admin overview retrieved", h.store.Overview(c.Request.Context(), time.Now()))
}
