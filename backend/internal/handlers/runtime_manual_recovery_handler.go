package handlers

import (
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"strconv"
	"strings"
)

func manualRecoveryInstanceID(c *gin.Context) (int, bool) {
	if !runtimeMaintenanceAdmin(c) {
		return 0, false
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		utils.Error(c, http.StatusBadRequest, "invalid instance id")
		return 0, false
	}
	return id, true
}
func (h *RuntimePoolHandler) InspectInstanceRecovery(c *gin.Context) {
	id, ok := manualRecoveryInstanceID(c)
	if !ok {
		return
	}
	if h == nil || h.scheduler == nil {
		utils.Error(c, http.StatusConflict, "Runtime recovery unavailable")
		return
	}
	info, err := h.scheduler.InspectInstanceRecovery(c.Request.Context(), id)
	if err != nil {
		utils.Error(c, http.StatusConflict, err.Error())
		return
	}
	utils.Success(c, http.StatusOK, "Runtime recovery inspected", info)
}
func (h *RuntimePoolHandler) RetryInstanceRecovery(c *gin.Context) {
	id, ok := manualRecoveryInstanceID(c)
	if !ok {
		return
	}
	var req struct {
		Generation int `json:"generation"`
	}
	if err := runtimeMaintenanceJSON(c, &req); err != nil || req.Generation < 1 {
		utils.Error(c, http.StatusBadRequest, "exact instance generation is required")
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
	info, err := h.scheduler.RetryInstanceRecovery(c.Request.Context(), id, req.Generation, *actor)
	if err != nil {
		utils.Error(c, http.StatusConflict, err.Error())
		return
	}
	utils.Success(c, http.StatusAccepted, "Runtime recovery checked and continued", info)
}
func (h *RuntimePoolHandler) RecheckPod(c *gin.Context) {
	if !runtimeMaintenanceAdmin(c) {
		return
	}
	id, ok := parseRuntimePodID(c)
	if !ok {
		return
	}
	var req struct {
		PodUID string `json:"pod_uid"`
	}
	if err := runtimeMaintenanceJSON(c, &req); err != nil || strings.TrimSpace(req.PodUID) == "" {
		utils.Error(c, http.StatusBadRequest, "exact Runtime Pod UID is required")
		return
	}
	actor := currentUserIDPtr(c)
	if actor == nil || *actor < 1 {
		utils.Error(c, http.StatusForbidden, "Admin actor identity required")
		return
	}
	if h == nil || h.scheduler == nil {
		utils.Error(c, http.StatusConflict, "Runtime inspection unavailable")
		return
	}
	pod, err := h.scheduler.RecheckRuntimePod(c.Request.Context(), id, req.PodUID, *actor)
	if err != nil {
		utils.Error(c, http.StatusConflict, err.Error())
		return
	}
	utils.Success(c, http.StatusOK, "Runtime physical slots rechecked", pod)
}
func (h *RuntimeManagementHandler) Retry(c *gin.Context) {
	if !runtimeMaintenanceAdmin(c) {
		return
	}
	actor := currentUserIDPtr(c)
	if actor == nil || *actor < 1 {
		utils.Error(c, http.StatusForbidden, "Admin actor identity required")
		return
	}
	if h == nil || h.service == nil {
		utils.Error(c, http.StatusConflict, "Runtime management unavailable")
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		utils.Error(c, http.StatusBadRequest, "operation id is required")
		return
	}
	log.Printf("admin Runtime operation retry actor=%d operation=%s", *actor, id)
	operation, err := h.service.RetryOperation(c.Request.Context(), id)
	if err != nil {
		utils.Error(c, http.StatusConflict, err.Error())
		return
	}
	utils.Success(c, http.StatusAccepted, "Runtime operation recheck queued", operation)
}
