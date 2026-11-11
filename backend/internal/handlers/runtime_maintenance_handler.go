package handlers

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/utils"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
	"time"
)

// RuntimeMaintenanceHandler accepts authenticated runtime evidence for upgrade
// isolation. Manual instance drain controls are intentionally not exposed.
type RuntimeMaintenanceHandler struct {
	store repository.RuntimeMaintenanceRepository
}

func NewRuntimeMaintenanceHandler(store repository.RuntimeMaintenanceRepository) *RuntimeMaintenanceHandler {
	return &RuntimeMaintenanceHandler{store: store}
}

func (h *RuntimeMaintenanceHandler) Poll(c *gin.Context) {
	// Missing counters must not silently decode to zero and certify an idle
	// runtime. Zero is valid only when explicitly reported.
	var input struct {
		Generation *int   `json:"generation"`
		Operation  string `json:"operation_id"`
		GateClosed *bool  `json:"gate_closed"`
		Active     *int   `json:"active_executions"`
		Admissions *int   `json:"active_admissions"`
		Jobs       *int   `json:"active_jobs"`
		Queued     *int   `json:"queued_inputs"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2048)
	if err := c.ShouldBindJSON(&input); err != nil || input.Generation == nil || input.GateClosed == nil || input.Active == nil || input.Admissions == nil || input.Jobs == nil || input.Queued == nil {
		utils.Error(c, 400, "Complete maintenance counters required")
		return
	}
	report := models.RuntimeMaintenanceReport{Generation: *input.Generation, Operation: input.Operation, GateClosed: *input.GateClosed, Active: *input.Active, Admissions: *input.Admissions, Jobs: *input.Jobs, Queued: *input.Queued}
	if !report.Valid() {
		utils.Error(c, 400, "Invalid maintenance report")
		return
	}
	actual, exists := c.Get("runtimeGeneration")
	generation, ok := actual.(int)
	if !exists || !ok || generation != report.Generation || c.GetInt("instanceID") <= 0 || c.GetInt("userID") <= 0 {
		utils.HandleError(c, models.ErrModelUnavailable)
		return
	}
	state, err := h.store.Poll(c.Request.Context(), c.GetInt("instanceID"), c.GetInt("userID"), report)
	if errors.Is(err, models.ErrModelUnavailable) {
		utils.HandleError(c, err)
		return
	}
	if err != nil {
		utils.Error(c, 503, "Maintenance control unavailable")
		return
	}
	operation := ""
	draining := false
	if state != nil {
		operation = state.Operation
		draining = state.Draining(time.Now().UTC())
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{"operation_id": operation, "draining": draining, "generation": generation})
}
