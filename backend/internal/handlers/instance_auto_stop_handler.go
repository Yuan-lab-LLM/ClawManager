package handlers

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

type InstanceAutoStopHandler struct {
	repo *repository.InstanceAutoStopRepository
}

func NewInstanceAutoStopHandler(repo *repository.InstanceAutoStopRepository) *InstanceAutoStopHandler {
	return &InstanceAutoStopHandler{repo}
}
func (h *InstanceAutoStopHandler) Get(c *gin.Context) {
	cfg, err := h.repo.Settings(c.Request.Context())
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Automatic stop settings", cfg)
}
func (h *InstanceAutoStopHandler) Update(c *gin.Context) {
	var req struct {
		Enabled              *bool  `json:"enabled"`
		CheckIntervalDays    *int   `json:"check_interval_days"`
		CheckIntervalSeconds *int64 `json:"check_interval_seconds"`
		IdleTimeoutDays      *int   `json:"idle_timeout_days"`
		IdleTimeoutSeconds   *int64 `json:"idle_timeout_seconds"`
		Version              *int64 `json:"version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	if req.Enabled == nil || (req.CheckIntervalDays == nil && req.CheckIntervalSeconds == nil) || (req.IdleTimeoutDays == nil && req.IdleTimeoutSeconds == nil) || req.Version == nil {
		utils.Error(c, http.StatusBadRequest, "enabled, version, and both check interval and idle timeout (seconds or legacy days) are required")
		return
	}
	cfg := models.InstanceAutoStopSettings{Enabled: *req.Enabled, CheckIntervalSeconds: req.CheckIntervalSeconds, IdleTimeoutSeconds: req.IdleTimeoutSeconds, Version: *req.Version}
	if req.CheckIntervalDays != nil {
		cfg.CheckIntervalDays = *req.CheckIntervalDays
	}
	if req.IdleTimeoutDays != nil {
		cfg.IdleTimeoutDays = *req.IdleTimeoutDays
	}
	if err := cfg.Validate(); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := h.repo.SaveSettings(c.Request.Context(), cfg)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Automatic stop settings updated", saved)
}
func (h *InstanceAutoStopHandler) SetDisabled(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		utils.Error(c, http.StatusBadRequest, "Invalid instance ID")
		return
	}
	var req struct {
		Disabled *bool `json:"auto_stop_disabled"`
	}
	if err = c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	if req.Disabled == nil {
		utils.Error(c, http.StatusBadRequest, "auto_stop_disabled is required")
		return
	}
	if err = h.repo.SetDisabled(c.Request.Context(), id, *req.Disabled); err != nil {
		utils.HandleError(c, err)
		return
	}
	a, err := h.repo.GetActivity(c.Request.Context(), id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Instance automatic stop updated", a)
}
