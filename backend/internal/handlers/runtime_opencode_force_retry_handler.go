package handlers

import (
	"clawreef/internal/models"
	"clawreef/internal/services"
	"clawreef/internal/utils"
	"context"
	"encoding/hex"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
	"strconv"
	"strings"
)

type openCodeForceRetryService interface {
	PreflightOpenCodeForceRetry(context.Context, int64, string) (*services.OpenCodeForceRetryPreflight, error)
	ConfirmOpenCodeForceRetry(context.Context, int64, services.OpenCodeForceRetryRequest, int) (*models.RuntimeRollout, error)
}

func (h *RuntimePoolHandler) openCodeForceService() openCodeForceRetryService {
	if h.forceRetry != nil {
		return h.forceRetry
	}
	if h.scheduler != nil {
		return h.scheduler
	}
	return nil
}
func readOpenCodeForceID(c *gin.Context) (int64, bool) {
	if !runtimeMaintenanceAdmin(c) {
		return 0, false
	}
	id, e := strconv.ParseInt(c.Param("id"), 10, 64)
	if e != nil || id <= 0 {
		utils.Error(c, 400, "invalid rollout id")
		return 0, false
	}
	return id, true
}
func (h *RuntimePoolHandler) PreflightOpenCodeForceRetry(c *gin.Context) {
	id, ok := readOpenCodeForceID(c)
	if !ok {
		return
	}
	var req *struct {
		Target string `json:"target_image_ref"`
	}
	if err := runtimeMaintenanceJSON(c, &req); err != nil || req == nil {
		utils.Error(c, 400, "invalid force preflight request")
		return
	}
	if !services.IsImmutableRuntimeImage(req.Target) || strings.TrimSpace(req.Target) != req.Target {
		utils.Error(c, 400, "immutable target image required")
		return
	}
	svc := h.openCodeForceService()
	if svc == nil {
		utils.Error(c, 503, "OpenCode force retry unavailable")
		return
	}
	pf, err := svc.PreflightOpenCodeForceRetry(c.Request.Context(), id, req.Target)
	if err != nil {
		utils.Error(c, 409, "OpenCode force preflight refused; operation remains protected")
		return
	}
	utils.Success(c, http.StatusOK, "OpenCode force preflight verified", pf)
}
func (h *RuntimePoolHandler) ConfirmOpenCodeForceRetry(c *gin.Context) {
	id, ok := readOpenCodeForceID(c)
	if !ok {
		return
	}
	var req *services.OpenCodeForceRetryRequest
	if err := runtimeMaintenanceJSON(c, &req); err != nil || req == nil {
		utils.Error(c, 400, "invalid force confirmation request")
		return
	}
	parsed, err := uuid.Parse(req.RequestID)
	fingerprint, hexErr := hex.DecodeString(req.Fingerprint)
	actor := currentUserIDPtr(c)
	if !req.Force || actor == nil || *actor <= 0 || err != nil || parsed.String() != req.RequestID || hexErr != nil || len(fingerprint) != 32 || hex.EncodeToString(fingerprint) != req.Fingerprint || !services.IsImmutableRuntimeImage(req.TargetImageRef) {
		utils.Error(c, 400, "explicit force, actor, UUID, fingerprint and immutable image required")
		return
	}
	svc := h.openCodeForceService()
	if svc == nil {
		utils.Error(c, 503, "OpenCode force retry unavailable")
		return
	}
	r, err := svc.ConfirmOpenCodeForceRetry(c.Request.Context(), id, *req, *actor)
	if err != nil {
		utils.Error(c, 409, "OpenCode force confirmation refused or outcome unknown; observe the existing operation")
		return
	}
	view, err := services.LoadOpenCodeUpgradeOperation(c.Request.Context(), h.rolloutRepo, r)
	if err != nil && h.rolloutRepo != nil {
		utils.Error(c, 503, "OpenCode confirmation recorded; observe the existing operation")
		return
	}
	utils.Success(c, http.StatusAccepted, "OpenCode force retry confirmed", gin.H{"rollout": r, "native_operation": view})
}
