package handlers

import (
	"clawreef/internal/services"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

func (h *RuntimePoolHandler) RecoverCompatibilityReservation(c *gin.Context) {
	if !runtimeMaintenanceAdmin(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		utils.Error(c, http.StatusBadRequest, "invalid rollout id")
		return
	}
	var req services.CompatibilityReservationRequest
	if err = runtimeMaintenanceJSON(c, &req); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err = req.Validate(); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	actor := currentUserIDPtr(c)
	if actor == nil || *actor < 1 {
		utils.Error(c, http.StatusForbidden, "Admin actor identity required")
		return
	}
	if h == nil || h.upgrade == nil {
		utils.Error(c, http.StatusConflict, "compatibility recovery is unavailable")
		return
	}
	if err = h.upgrade.RecoverCompatibilityReservation(c.Request.Context(), id, req, *actor); err != nil {
		utils.Error(c, http.StatusConflict, err.Error())
		return
	}
	utils.Success(c, http.StatusOK, "Confirmed native compatibility execution exited", gin.H{"rollout_id": id, "operation_id": req.OperationID})
}

func (h *RuntimePoolHandler) RetireFailedCompatibilityPool(c *gin.Context) {
	if !runtimeMaintenanceAdmin(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		utils.Error(c, http.StatusBadRequest, "invalid rollout id")
		return
	}
	var req services.FailedCompatibilityPoolRequest
	if err = runtimeMaintenanceJSON(c, &req); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	actor := currentUserIDPtr(c)
	if actor == nil || *actor < 1 {
		utils.Error(c, http.StatusForbidden, "Admin actor identity required")
		return
	}
	if h == nil || h.upgrade == nil {
		utils.Error(c, http.StatusConflict, "compatibility retirement is unavailable")
		return
	}
	if err = h.upgrade.RetireFailedCompatibilityPool(c.Request.Context(), id, req, *actor); err != nil {
		utils.Error(c, http.StatusConflict, err.Error())
		return
	}
	utils.Success(c, http.StatusAccepted, "Empty failed compatibility pool retirement accepted", gin.H{"rollout_id": id, "deployment_name": req.DeploymentName})
}

func (h *RuntimePoolHandler) FreshRuntimeCapacity(c *gin.Context) {
	if !runtimeMaintenanceAdmin(c) {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		utils.Error(c, http.StatusBadRequest, "invalid instance id")
		return
	}
	var req services.FreshRuntimeCapacityRequest
	if err = runtimeMaintenanceJSON(c, &req); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	actor := currentUserIDPtr(c)
	if actor == nil || *actor < 1 {
		utils.Error(c, http.StatusForbidden, "Admin actor identity required")
		return
	}
	if h == nil || h.scheduler == nil {
		utils.Error(c, http.StatusConflict, "fresh Runtime admission is unavailable")
		return
	}
	result, err := h.scheduler.FreshRuntimeCapacity(c.Request.Context(), id, req, *actor)
	if err != nil {
		utils.Error(c, http.StatusConflict, err.Error())
		return
	}
	utils.Success(c, http.StatusOK, "Fresh Runtime capacity inspected", result)
}

func (h *RuntimePoolHandler) RecoverStoppedSource(c *gin.Context) {
	if !runtimeMaintenanceAdmin(c) {
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		utils.Error(c, http.StatusBadRequest, "invalid instance id")
		return
	}
	var req services.StoppedSourceRecoveryRequest
	if err = runtimeMaintenanceJSON(c, &req); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err = req.Validate(); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	actor := currentUserIDPtr(c)
	if actor == nil || *actor < 1 {
		utils.Error(c, http.StatusForbidden, "Admin actor identity required")
		return
	}
	if h == nil || h.scheduler == nil {
		utils.Error(c, http.StatusConflict, "Runtime recovery unavailable")
		return
	}
	if err = h.scheduler.RecoverStoppedSource(c.Request.Context(), id, req, *actor); err != nil {
		utils.Error(c, http.StatusConflict, err.Error())
		return
	}
	utils.Success(c, http.StatusOK, "Confirmed stopped predecessor; pending successor can reconcile", gin.H{"instance_id": id, "successor_generation": req.SuccessorGeneration})
}
