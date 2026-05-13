package handlers

import (
	"net/http"
	"strconv"

	"clawreef/internal/services"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

// InstanceUsageHandler exposes instance resource usage data via REST.
type InstanceUsageHandler struct {
	usageService services.InstanceUsageService
}

// NewInstanceUsageHandler creates a new handler.
func NewInstanceUsageHandler(usageService services.InstanceUsageService) *InstanceUsageHandler {
	return &InstanceUsageHandler{usageService: usageService}
}

// GetCurrentUsage returns the latest usage snapshot for a single instance.
// GET /instances/:id/usage/current
func (h *InstanceUsageHandler) GetCurrentUsage(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	usage, err := h.usageService.GetCurrentUsage(id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	if usage == nil {
		utils.Success(c, http.StatusOK, "No usage data available yet", nil)
		return
	}

	utils.Success(c, http.StatusOK, "Current usage retrieved", usage)
}

// GetUsageHistory returns historical usage records for a single instance.
// GET /instances/:id/usage/history?hours=24
func (h *InstanceUsageHandler) GetUsageHistory(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	hours := 24
	if v := c.Query("hours"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			hours = parsed
		}
	}

	records, err := h.usageService.GetHistory(id, hours)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "Usage history retrieved", records)
}

// GetUsageSummary returns the latest usage snapshot for every running instance.
// Admin-only endpoint.
// GET /admin/usage/summary
func (h *InstanceUsageHandler) GetUsageSummary(c *gin.Context) {
	records, err := h.usageService.GetAllCurrentUsage()
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "Usage summary retrieved", records)
}

