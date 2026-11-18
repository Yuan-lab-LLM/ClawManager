package handlers

import (
	"encoding/json"
	"errors"
	"io"
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

// Inspect is a read-only preflight, not enrollment or proof of runtime support.
// Keep it outside RuntimeProtocolPolicy so an old runtime can inspect before
// opting in, but behind GatewayAuth so generation is server-owned.
func (h *RuntimeProtocolHandler) Inspect(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	actual, exists := c.Get("runtimeGeneration")
	generation, valid := actual.(int)
	if c.GetInt("instanceID") <= 0 || c.GetInt("userID") <= 0 || !exists || !valid || generation < 0 {
		utils.HandleError(c, models.ErrModelUnavailable)
		return
	}
	state, err := h.protocols.Get(c.Request.Context(), c.GetInt("instanceID"))
	if err != nil {
		utils.Error(c, 503, "Runtime governance policy unavailable")
		return
	}
	if state != nil && state.Protocol != models.RuntimeGovernanceProtocol {
		utils.Error(c, 503, "Unsupported runtime governance policy")
		return
	}
	var enrolledGeneration *int
	if state != nil {
		value := state.Generation
		enrolledGeneration = &value
	}
	_, renewalSupported := h.protocols.(repository.RuntimeProtocolRenewer)
	_, enrollmentSupported := h.protocols.(repository.RuntimeProtocolConditionalEnroller)
	var enrollmentRevision *string
	if state != nil && enrollmentSupported {
		revision, err := state.EnrollmentRevision()
		if err != nil {
			// Preserve the old inspection response, but do not advertise a safe
			// conditional transition out of corrupt or incomplete stored state.
			enrollmentSupported = false
		} else {
			enrollmentRevision = &revision
		}
	}
	var currentReport any
	if state.CurrentReport(c.GetInt("instanceID"), c.GetInt("userID"), generation, time.Now()) {
		currentReport = gin.H{"runtime_version": state.RuntimeVersion, "adapter_version": state.AdapterVersion}
	}
	c.JSON(http.StatusOK, gin.H{
		"protocol": models.RuntimeGovernanceProtocol, "generation": generation,
		"required_capabilities": models.RequiredRuntimeCapabilities(),
		"enrolled":              state != nil, "enrollment_generation": enrolledGeneration,
		"registration_is_sticky": true,
		"renewal_supported":      renewalSupported, "report_ttl_seconds": int(models.RuntimeCapabilityReportTTL.Seconds()),
		"conditional_enrollment_supported": enrollmentSupported, "enrollment_revision": enrollmentRevision,
		"current_report": currentReport,
	})
}

func (h *RuntimeProtocolHandler) Negotiate(c *gin.Context) {
	h.writeCapabilityReport(c, false)
}

// Renew cannot enroll or change a contract. It only refreshes an existing
// matching report; legacy runtimes remain legacy until explicit negotiation.
func (h *RuntimeProtocolHandler) Renew(c *gin.Context) {
	h.writeCapabilityReport(c, true)
}

func (h *RuntimeProtocolHandler) writeCapabilityReport(c *gin.Context, renewal bool) {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var report models.RuntimeCapabilityReport
	decoder := json.NewDecoder(c.Request.Body)
	if err := decoder.Decode(&report); err != nil {
		utils.Error(c, 400, "Invalid runtime capability report")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
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
	var err error
	if renewal {
		renewer, ok := h.protocols.(repository.RuntimeProtocolRenewer)
		if !ok {
			utils.Error(c, 503, "Runtime capability renewal unavailable")
			return
		}
		err = renewer.Renew(c.Request.Context(), instanceID, ownerID, report)
	} else {
		err = h.protocols.Negotiate(c.Request.Context(), instanceID, ownerID, report)
	}
	if errors.Is(err, models.ErrRuntimeCapabilityRenewalConflict) {
		utils.Error(c, 409, "Runtime capability contract changed; explicit negotiation is required")
		return
	}
	if errors.Is(err, models.ErrModelUnavailable) {
		utils.HandleError(c, models.ErrModelUnavailable)
		return
	}
	if err != nil {
		utils.Error(c, 503, "Runtime capability report unavailable")
		return
	}
	c.JSON(http.StatusOK, gin.H{"protocol": models.RuntimeGovernanceProtocol, "generation": generation, "task_identity_required": true, "renewed": renewal, "report_ttl_seconds": int(models.RuntimeCapabilityReportTTL.Seconds())})
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
	capabilities := []string{}
	if state != nil {
		if err := json.Unmarshal([]byte(state.CapabilitiesJSON), &capabilities); err != nil {
			utils.Error(c, 503, "Invalid runtime capability state")
			return
		}
	}
	mode, reason := state.Readiness(instance, capabilities, time.Now())
	c.Header("Cache-Control", "no-store")
	utils.Success(c, 200, "Runtime governance status retrieved", gin.H{
		"mode": mode, "reason": reason, "task_identity_required": state != nil && state.Protocol > 0,
		"report": state, "capabilities": capabilities,
	})
}
