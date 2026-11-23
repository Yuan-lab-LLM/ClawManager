package handlers

import (
	"errors"
	"net/http"

	"clawreef/internal/models"
	"clawreef/internal/services"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
)

// StartInstance uses the owner cookie, not the management JWT or reset flow.
func (h *IEISystemHandler) StartInstance(c *gin.Context) {
	h.noStore(c)
	session, ok := h.requireSession(c)
	if !ok {
		return
	}
	instance, ok := h.requireOwnedSupportedInstance(c, session.Email)
	if !ok {
		return
	}
	if c.GetHeader("X-Owner-Action") != "1" || c.GetHeader("Sec-Fetch-Site") == "cross-site" {
		utils.Error(c, http.StatusForbidden, "Explicit owner action required")
		return
	}
	if instance.Status == "running" || instance.Status == "creating" {
		utils.Success(c, http.StatusAccepted, "Instance start already accepted", gin.H{"status": instance.Status})
		return
	}
	if instance.Status != "stopped" {
		utils.Error(c, http.StatusConflict, "Only stopped instances can be started here")
		return
	}
	if h.lifecycle != nil {
		op, err := h.lifecycle.GetLatestLifecycleOperation(instance.UserID, instance.ID)
		if err != nil {
			h.lifecycleError(c, err, "Unable to verify instance lifecycle status")
			return
		}
		if op != nil && (op.Status == "queued" || op.Status == "processing" || op.Status == "batch_pending") {
			utils.Error(c, http.StatusConflict, "Instance operation is in progress")
			return
		}
	}
	starter, ok := h.instances.(interface{ StartStopped(int) error })
	if !ok {
		utils.Error(c, http.StatusServiceUnavailable, "Instance start service is unavailable")
		return
	}
	if err := starter.StartStopped(instance.ID); err != nil {
		if errors.Is(err, services.ErrInstanceNotStopped) || errors.Is(err, models.ErrInstanceOperationInProgress) || errors.Is(err, models.ErrRuntimeUpgradeInProgress) || errors.Is(err, models.ErrInstanceStopping) {
			utils.Error(c, http.StatusConflict, "实例状态或操作已变化，请刷新后确认。")
		} else {
			utils.Error(c, http.StatusServiceUnavailable, "暂时无法确认启动，原数据保留。请刷新状态后再试；Team 成员请从团队入口启动。")
		}
		return
	}
	utils.Success(c, http.StatusAccepted, "Instance start accepted", gin.H{"status": "starting"})
}
