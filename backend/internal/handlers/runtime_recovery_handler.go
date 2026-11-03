package handlers

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

// Deliberately separate from factory reset: no reset/deletion service is wired.
type RuntimeRecoveryHandler struct {
	upgrade   *services.RuntimeUpgradeService
	instances repository.InstanceRepository
}

func NewRuntimeRecoveryHandler(u *services.RuntimeUpgradeService, r repository.InstanceRepository) *RuntimeRecoveryHandler {
	return &RuntimeRecoveryHandler{u, r}
}
func (h *RuntimeRecoveryHandler) owned(c *gin.Context) (int, int, bool, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		utils.Error(c, 400, "invalid instance id")
		return 0, 0, false, false
	}
	actor, ok := c.Get("userID")
	user, valid := actor.(int)
	if !ok || !valid {
		utils.Error(c, 401, "authentication required")
		return 0, 0, false, false
	}
	row, err := h.instances.GetByID(id)
	if err != nil {
		utils.HandleError(c, err)
		return 0, 0, false, false
	}
	if row == nil {
		utils.Error(c, 404, "instance not found")
		return 0, 0, false, false
	}
	role, _ := c.Get("userRole")
	admin := role == "admin"
	if !admin && row.UserID != user {
		utils.Error(c, 403, "access denied")
		return 0, 0, false, false
	}
	return id, user, admin, true
}
func (h *RuntimeRecoveryHandler) Inspect(c *gin.Context) {
	id, _, admin, ok := h.owned(c)
	if !ok {
		return
	}
	info, err := h.upgrade.RecoveryInfo(c.Request.Context(), id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	if !admin && info.Current != nil {
		items := make([]models.RuntimeUpgradeItem, 0, 1)
		for _, item := range info.Current.Items {
			if item.InstanceID == id {
				items = append(items, item)
			}
		}
		r := *info.Current.Rollout
		r.ErrorMessage, r.RollbackError, r.StartedBy = nil, nil, nil
		info.Current = &services.RuntimeUpgradeDetails{Rollout: &r, Items: items, Audits: []models.RuntimeUpgradeAudit{}}
	}
	utils.Success(c, http.StatusOK, "Upgrade recovery inspected", info)
}
func (h *RuntimeRecoveryHandler) Recover(c *gin.Context) {
	id, actor, admin, ok := h.owned(c)
	if !ok {
		return
	}
	var req services.RuntimeRecoveryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	// Action names are upgrade-only. "reset"/"delete" are always rejected.
	r, err := h.upgrade.RequestRecovery(c.Request.Context(), id, actor, admin, req)
	if err != nil {
		utils.Error(c, http.StatusConflict, err.Error())
		return
	}
	utils.Success(c, http.StatusAccepted, "Upgrade recovery queued", gin.H{"rollout": r})
}
