package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"clawreef/internal/models"
	"clawreef/internal/services"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

type SystemSettingsHandler struct {
	systemImageSettingService services.SystemImageSettingService
}

type UpsertSystemImageSettingRequest struct {
	ID           int    `json:"id,omitempty"`
	InstanceType string `json:"instance_type" binding:"required"`
	DisplayName  string `json:"display_name"`
	Image        string `json:"image" binding:"required"`
}

func NewSystemSettingsHandler(systemImageSettingService services.SystemImageSettingService) *SystemSettingsHandler {
	return &SystemSettingsHandler{systemImageSettingService: systemImageSettingService}
}

func (h *SystemSettingsHandler) ListSystemImageSettings(c *gin.Context) {
	settings, err := h.systemImageSettingService.List()
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "System image settings retrieved successfully", gin.H{
		"items": settings,
	})
}

func (h *SystemSettingsHandler) UpsertSystemImageSetting(c *gin.Context) {
	var req UpsertSystemImageSettingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	setting := &models.SystemImageSetting{
		ID:           req.ID,
		InstanceType: strings.TrimSpace(req.InstanceType),
		DisplayName:  strings.TrimSpace(req.DisplayName),
		Image:        strings.TrimSpace(req.Image),
	}

	saved, err := h.systemImageSettingService.Save(setting)
	if err != nil {
		utils.HandleError(c, err)
		return
	}

	utils.Success(c, http.StatusOK, "System image setting saved successfully", saved)
}

func (h *SystemSettingsHandler) DeleteSystemImageSetting(c *gin.Context) {
	target := strings.TrimSpace(c.Param("instanceType"))
	if id, err := strconv.Atoi(target); err == nil {
		if err := h.systemImageSettingService.DeleteByID(id); err != nil {
			utils.HandleError(c, err)
			return
		}
	} else {
		if err := h.systemImageSettingService.DisableType(target); err != nil {
			utils.HandleError(c, err)
			return
		}
	}

	utils.Success(c, http.StatusOK, "System image setting deleted successfully", nil)
}
