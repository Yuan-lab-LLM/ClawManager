package handlers

import (
	"clawreef/internal/services"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
)

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
