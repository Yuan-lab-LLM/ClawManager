package handlers

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
	"strconv"
)

type RuntimeShutdownAuditHandler struct {
	instances repository.InstanceRepository
	audits    repository.RuntimeShutdownAuditRepository
}

func NewRuntimeShutdownAuditHandler(instances repository.InstanceRepository, audits repository.RuntimeShutdownAuditRepository) *RuntimeShutdownAuditHandler {
	return &RuntimeShutdownAuditHandler{instances: instances, audits: audits}
}
func (h *RuntimeShutdownAuditHandler) List(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		utils.Error(c, 400, "Invalid instance ID")
		return
	}
	before := int64(0)
	if raw, exists := c.GetQuery("before_id"); exists {
		before, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || before <= 0 {
			utils.Error(c, 400, "Invalid history cursor")
			return
		}
	}
	instance, err := h.instances.GetByID(id)
	if err != nil {
		utils.Error(c, 503, "Runtime shutdown history unavailable")
		return
	}
	if instance == nil {
		utils.Error(c, 404, "Instance not found")
		return
	}
	if c.GetInt("userID") <= 0 || (c.GetString("userRole") != "admin" && instance.UserID != c.GetInt("userID")) {
		utils.Error(c, 403, "Access denied")
		return
	}
	if h.audits == nil {
		utils.Error(c, 503, "Runtime shutdown history unavailable")
		return
	}
	items, err := h.audits.List(c.Request.Context(), id, instance.UserID, before)
	if err != nil {
		utils.Error(c, 503, "Runtime shutdown history unavailable")
		return
	}
	// Never call this a current cleanup proof, even for the same generation.
	utils.Success(c, 200, "Runtime shutdown observations retrieved", gin.H{"items": items, "generation": instance.RuntimeGeneration, "diagnostic_only": true, "next_before_id": nextShutdownCursor(items)})
}
func nextShutdownCursor(items []models.RuntimeShutdownAudit) int64 {
	if len(items) == 50 {
		return items[len(items)-1].ID
	}
	return 0
}
