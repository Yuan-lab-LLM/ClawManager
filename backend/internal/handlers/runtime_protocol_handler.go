package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
)

type RuntimeProtocolHandler struct {
	protocols repository.RuntimeProtocolRepository
	instances repository.InstanceRepository
}

func NewRuntimeProtocolHandler(protocols repository.RuntimeProtocolRepository, instances repository.InstanceRepository) *RuntimeProtocolHandler {
	return &RuntimeProtocolHandler{protocols: protocols, instances: instances}
}

func (h *RuntimeProtocolHandler) Negotiate(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var report models.RuntimeCapabilityReport
	if err := c.ShouldBindJSON(&report); err != nil {
		utils.Error(c, 400, "Invalid runtime capability report")
		return
	}
	if err := report.Validate(); err != nil {
		utils.Error(c, 400, err.Error())
		return
	}
	instanceID := c.GetInt("instanceID")
	ownerID := c.GetInt("userID")
	actual, exists := c.Get("runtimeGeneration")
	generation, valid := actual.(int)
	if instanceID <= 0 || ownerID <= 0 || !exists || !valid || generation != report.Generation {
		utils.HandleError(c, models.ErrModelUnavailable)
		return
	}
	if err := h.protocols.Negotiate(c.Request.Context(), instanceID, ownerID, report); err != nil {
		utils.HandleError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"protocol": models.RuntimeGovernanceProtocol, "generation": generation, "task_identity_required": true})
}

// Status is owner/admin only. Capabilities are runtime reports, not proof that
// any individual cancellation completed. Cancellation receipts remain separate.
func (h *RuntimeProtocolHandler) Status(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		utils.Error(c, 400, "Invalid instance ID")
		return
	}
	instance, err := h.instances.GetByID(id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	if instance == nil {
		utils.Error(c, 404, "Instance not found")
		return
	}
	if c.GetString("userRole") != "admin" && instance.UserID != c.GetInt("userID") {
		utils.Error(c, 403, "Access denied")
		return
	}
	state, err := h.protocols.Get(c.Request.Context(), id)
	if err != nil {
		utils.Error(c, 503, "Runtime governance status unavailable")
		return
	}
	mode := "legacy"
	capabilities := []string{}
	if state != nil {
		mode = "awaiting_runtime"
		fresh := time.Since(state.ReportedAt)
		if state.Protocol == models.RuntimeGovernanceProtocol && state.OwnerUserID == instance.UserID &&
			state.Generation == instance.RuntimeGeneration && fresh >= 0 && fresh < 30*time.Second && instance.Status == "running" {
			mode = "governed"
		}
		if err := json.Unmarshal([]byte(state.CapabilitiesJSON), &capabilities); err != nil {
			utils.Error(c, 503, "Invalid runtime capability state")
			return
		}
	}
	c.Header("Cache-Control", "no-store")
	utils.Success(c, 200, "Runtime governance status retrieved", gin.H{
		"mode": mode, "task_identity_required": state != nil && state.Protocol > 0,
		"report": state, "capabilities": capabilities,
	})
}
