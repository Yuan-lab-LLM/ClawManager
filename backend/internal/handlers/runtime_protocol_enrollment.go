package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
)

// Enroll is explicit and conditional. An omitted precondition is not equivalent
// to a preflight that found no enrollment; clients must send JSON null for that.
func (h *RuntimeProtocolHandler) Enroll(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var payload struct {
		Report   models.RuntimeCapabilityReport `json:"report"`
		Expected json.RawMessage                `json:"expected_enrollment_revision"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil || decoder.Decode(new(any)) != io.EOF || len(payload.Expected) == 0 {
		utils.Error(c, 400, "Invalid conditional capability enrollment")
		return
	}
	var expected *string
	if json.Unmarshal(payload.Expected, &expected) != nil || models.ValidateEnrollmentRevision(expected) != nil || payload.Report.Validate() != nil {
		utils.Error(c, 400, "Invalid conditional capability enrollment")
		return
	}
	actual, exists := c.Get("runtimeGeneration")
	generation, valid := actual.(int)
	if c.GetInt("instanceID") <= 0 || c.GetInt("userID") <= 0 || !exists || !valid || generation != payload.Report.Generation {
		utils.HandleError(c, models.ErrModelUnavailable)
		return
	}
	enroller, ok := h.protocols.(repository.RuntimeProtocolConditionalEnroller)
	if !ok {
		utils.Error(c, 503, "Conditional runtime capability enrollment unavailable")
		return
	}
	err := enroller.Enroll(c.Request.Context(), c.GetInt("instanceID"), c.GetInt("userID"), payload.Report, expected)
	if errors.Is(err, models.ErrRuntimeCapabilityEnrollmentConflict) {
		utils.Error(c, 409, "Runtime capability enrollment changed; inspect again before explicit enrollment")
		return
	}
	if errors.Is(err, models.ErrModelUnavailable) {
		utils.HandleError(c, models.ErrModelUnavailable)
		return
	}
	if err != nil {
		utils.Error(c, 503, "Runtime capability enrollment unavailable")
		return
	}
	c.JSON(http.StatusOK, gin.H{"protocol": models.RuntimeGovernanceProtocol, "generation": generation, "enrolled": true, "task_identity_required": true, "report_ttl_seconds": int(models.RuntimeCapabilityReportTTL.Seconds())})
}
