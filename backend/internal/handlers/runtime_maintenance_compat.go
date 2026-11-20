package handlers

import (
	"clawreef/internal/models"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"time"
)

func (h *RuntimeMaintenanceHandler) maintenanceInstance(c *gin.Context, admin bool) *models.Instance {
	if admin && c.GetString("userRole") != "admin" {
		utils.Error(c, http.StatusForbidden, "Administrator required")
		return nil
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		utils.Error(c, http.StatusBadRequest, "Invalid instance ID")
		return nil
	}
	if h.instances == nil {
		utils.Error(c, http.StatusServiceUnavailable, "Maintenance status unavailable")
		return nil
	}
	instance, err := h.instances.GetByID(id)
	if err != nil {
		utils.HandleError(c, err)
		return nil
	}
	if instance == nil {
		utils.Error(c, http.StatusNotFound, "Instance not found")
		return nil
	}
	if c.GetString("userRole") != "admin" && instance.UserID != c.GetInt("userID") {
		utils.Error(c, http.StatusForbidden, "Access denied")
		return nil
	}
	return instance
}

// Keep existing clients readable without reintroducing the manual drain
// mutations intentionally removed by the upstream slot lifecycle redesign.
func (h *RuntimeMaintenanceHandler) Status(c *gin.Context) {
	instance := h.maintenanceInstance(c, false)
	if instance == nil {
		return
	}
	state, err := h.store.Get(c.Request.Context(), instance.ID)
	if err != nil {
		utils.Error(c, http.StatusServiceUnavailable, "Maintenance status unavailable")
		return
	}
	if state != nil && (state.OwnerID != instance.UserID || state.Generation != instance.RuntimeGeneration) {
		state = nil
	}
	draining, drained := false, false
	if state != nil && instance.Status == "running" {
		draining = state.Draining(time.Now().UTC())
		drained = state.Drained(time.Now().UTC())
	}
	c.Header("Cache-Control", "no-store")
	utils.Success(c, http.StatusOK, "Maintenance status", gin.H{"operation": state, "draining": draining, "drained": drained,
		"automatic_upgrade_available": false, "manual_controls_available": false})
}

func (h *RuntimeMaintenanceHandler) Begin(c *gin.Context) {
	if h.maintenanceInstance(c, true) == nil {
		return
	}
	utils.Error(c, http.StatusConflict, "Manual drain is unavailable with the current runtime lifecycle; no operation was started")
}
func (h *RuntimeMaintenanceHandler) Cancel(c *gin.Context) {
	if h.maintenanceInstance(c, true) == nil {
		return
	}
	utils.Error(c, http.StatusConflict, "Manual drain cancellation is unavailable with the current runtime lifecycle; no operation was changed")
}
