package handlers

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"clawreef/internal/services"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
)

type runtimeSlotResetService interface {
	PreflightSlotReset(context.Context, int64, string, services.RuntimeSlotResetRequest) (*services.RuntimeSlotResetPreflight, error)
	ResetSlot(context.Context, int64, string, services.RuntimeSlotResetRequest, *int) (*services.RuntimeManagementOperation, error)
	SlotResetOperation(context.Context, string) (*services.RuntimeManagementOperation, error)
}

type RuntimeSlotResetHandler struct {
	service runtimeSlotResetService
}

func NewRuntimeSlotResetHandler(service runtimeSlotResetService) *RuntimeSlotResetHandler {
	return &RuntimeSlotResetHandler{service: service}
}

var runtimeSlotResetName = regexp.MustCompile(`^slot-(0|[1-9][0-9]*)$`)

func readRuntimeSlotResetRequest(c *gin.Context) (int64, string, *services.RuntimeSlotResetRequest) {
	if !runtimeMaintenanceAdmin(c) {
		return 0, "", nil
	}
	id, ok := parseRuntimePodID(c)
	if !ok {
		return 0, "", nil
	}
	slot := c.Param("slot")
	if !runtimeSlotResetName.MatchString(slot) {
		utils.Error(c, http.StatusBadRequest, "invalid runtime slot name")
		return 0, "", nil
	}
	var req *services.RuntimeSlotResetRequest
	if err := runtimeMaintenanceJSON(c, &req); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return 0, "", nil
	}
	if req == nil {
		utils.Error(c, http.StatusBadRequest, "request must contain a JSON object")
		return 0, "", nil
	}
	return id, slot, req
}

func (h *RuntimeSlotResetHandler) Preflight(c *gin.Context) {
	id, slot, req := readRuntimeSlotResetRequest(c)
	if req == nil {
		return
	}
	result, err := h.service.PreflightSlotReset(c.Request.Context(), id, slot, *req)
	if err != nil {
		utils.Error(c, http.StatusConflict, err.Error())
		return
	}
	utils.Success(c, http.StatusOK, "Runtime slot reset preflight", result)
}

func (h *RuntimeSlotResetHandler) Submit(c *gin.Context) {
	id, slot, req := readRuntimeSlotResetRequest(c)
	if req == nil {
		return
	}
	if strings.TrimSpace(req.SourcePodUID) == "" || strings.TrimSpace(req.SourceContainerID) == "" ||
		strings.TrimSpace(req.ScopeFingerprint) == "" || strings.TrimSpace(req.RequestID) == "" {
		utils.Error(c, http.StatusBadRequest, "source_pod_uid, source_container_id, scope_fingerprint and request_id are required")
		return
	}
	actor := currentUserIDPtr(c)
	if actor == nil || *actor < 1 {
		utils.Error(c, http.StatusForbidden, "Admin actor identity required")
		return
	}
	result, err := h.service.ResetSlot(c.Request.Context(), id, slot, *req, actor)
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, services.ErrRuntimeSlotResetCommitUncertain) {
			status = http.StatusInternalServerError
		}
		utils.Error(c, status, err.Error())
		return
	}
	utils.Success(c, http.StatusAccepted, "Runtime slot reset queued", result)
}

func (h *RuntimeSlotResetHandler) History(history gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID, exact := c.GetQuery("slot_reset_request_id")
		if !exact {
			history(c)
			return
		}
		if !runtimeMaintenanceAdmin(c) {
			return
		}
		if strings.TrimSpace(requestID) == "" || len(requestID) > 80 {
			utils.Error(c, http.StatusBadRequest, "invalid slot_reset_request_id")
			return
		}
		result, err := h.service.SlotResetOperation(c.Request.Context(), requestID)
		if err != nil {
			utils.HandleError(c, err)
			return
		}
		utils.Success(c, http.StatusOK, "Runtime slot reset operation", result)
	}
}
