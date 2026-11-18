package handlers

import (
	"clawreef/internal/services"
	"clawreef/internal/utils"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func (h *RuntimeManagementHandler) ScaleInPreflight(c *gin.Context) {
	if !runtimeMaintenanceAdmin(c) {
		return
	}
	var req services.RuntimePodScaleInRequest
	if err := runtimeMaintenanceJSON(c, &req); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	result, err := h.service.ScaleInPreflight(c.Request.Context(), req)
	if err != nil {
		utils.Error(c, http.StatusConflict, err.Error())
		return
	}
	utils.Success(c, http.StatusOK, "Runtime scale-in preflight", result)
}

func (h *RuntimeManagementHandler) ScaleInSubmit(c *gin.Context) {
	if !runtimeMaintenanceAdmin(c) {
		return
	}
	var req services.RuntimePodScaleInRequest
	if err := runtimeMaintenanceJSON(c, &req); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.PlanFingerprint) == "" {
		utils.Error(c, http.StatusBadRequest, "plan_fingerprint is required; preview scale-in first")
		return
	}
	result, err := h.service.ScaleInSubmit(c.Request.Context(), req, currentUserIDPtr(c))
	if err != nil {
		utils.Error(c, http.StatusConflict, err.Error())
		return
	}
	utils.Success(c, http.StatusAccepted, "Runtime scale-in queued", result)
}
