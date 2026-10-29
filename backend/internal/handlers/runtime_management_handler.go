package handlers

import (
	"clawreef/internal/services"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
	"net/http"
)

type RuntimeManagementHandler struct {
	service *services.RuntimeManagementService
}

func NewRuntimeManagementHandler(service *services.RuntimeManagementService) *RuntimeManagementHandler {
	return &RuntimeManagementHandler{service: service}
}
func (h *RuntimeManagementHandler) Scheduling(c *gin.Context) {
	id, ok := parseRuntimePodID(c)
	if !ok {
		return
	}
	var req struct {
		Enabled *bool `json:"enabled" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	if err := h.service.Scheduling(c.Request.Context(), id, *req.Enabled); err != nil {
		utils.Error(c, http.StatusConflict, err.Error())
		return
	}
	utils.Success(c, http.StatusOK, "Runtime scheduling updated", gin.H{"id": id, "scheduling_enabled": *req.Enabled})
}
func (h *RuntimeManagementHandler) Preflight(c *gin.Context) {
	var req services.RuntimeSchedulingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	result, err := h.service.Preflight(c.Request.Context(), req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Runtime scheduling preflight", result)
}
func (h *RuntimeManagementHandler) Submit(c *gin.Context) {
	var req services.RuntimeSchedulingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	result, err := h.service.Submit(c.Request.Context(), req, currentUserIDPtr(c))
	if err != nil {
		utils.Error(c, http.StatusConflict, err.Error())
		return
	}
	utils.Success(c, http.StatusAccepted, "Runtime scheduling queued", result)
}
func (h *RuntimeManagementHandler) Evict(c *gin.Context) {
	id, ok := parseRuntimePodID(c)
	if !ok {
		return
	}
	var req services.RuntimeSchedulingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	result, err := h.service.Evict(c.Request.Context(), id, req, currentUserIDPtr(c))
	if err != nil {
		utils.Error(c, http.StatusConflict, err.Error())
		return
	}
	utils.Success(c, http.StatusAccepted, "Runtime eviction queued", result)
}
func (h *RuntimeManagementHandler) History(c *gin.Context) {
	items, err := h.service.History(c.Request.Context())
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	data := gin.H{"items": items, "supported_modes": h.service.SupportedEvictionModes()}
	if len(h.service.SupportedEvictionModes()) < 2 {
		data["force_unavailable_reason"] = services.RuntimeForceEvictionUnavailable
	}
	utils.Success(c, http.StatusOK, "Runtime scheduling history", data)
}

func (h *RuntimeManagementHandler) Cancel(c *gin.Context) {
	if err := h.service.Cancel(c.Request.Context(), c.Param("id")); err != nil {
		utils.Error(c, http.StatusConflict, err.Error())
		return
	}
	utils.Success(c, http.StatusOK, "Remaining Runtime scheduling items will be cancelled", nil)
}

func (h *RuntimeManagementHandler) BalancePlan(c *gin.Context) {
	var req services.RuntimeBalanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	result, err := h.service.BalancePlan(c.Request.Context(), req)
	if err != nil {
		utils.Error(c, http.StatusConflict, err.Error())
		return
	}
	utils.Success(c, http.StatusOK, "Runtime balance plan", result)
}

func (h *RuntimeManagementHandler) Balance(c *gin.Context) {
	var req services.RuntimeBalanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	result, err := h.service.Balance(c.Request.Context(), req, currentUserIDPtr(c))
	if err != nil {
		utils.Error(c, http.StatusConflict, err.Error())
		return
	}
	utils.Success(c, http.StatusAccepted, "Runtime balance queued", result)
}
