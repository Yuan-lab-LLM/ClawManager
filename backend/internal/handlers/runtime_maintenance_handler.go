package handlers

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/utils"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
	"strconv"
	"time"
)

type RuntimeMaintenanceHandler struct {
	store     repository.RuntimeMaintenanceRepository
	instances repository.InstanceRepository
}

func NewRuntimeMaintenanceHandler(store repository.RuntimeMaintenanceRepository, instances repository.InstanceRepository) *RuntimeMaintenanceHandler {
	return &RuntimeMaintenanceHandler{store, instances}
}
func (h *RuntimeMaintenanceHandler) instance(c *gin.Context, admin bool) *models.Instance {
	if admin && c.GetString("userRole") != "admin" {
		utils.Error(c, 403, "Administrator required")
		return nil
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		utils.Error(c, 400, "Invalid instance ID")
		return nil
	}
	instance, err := h.instances.GetByID(id)
	if err != nil {
		utils.HandleError(c, err)
		return nil
	}
	if instance == nil {
		utils.Error(c, 404, "Instance not found")
		return nil
	}
	if c.GetString("userRole") != "admin" && instance.UserID != c.GetInt("userID") {
		utils.Error(c, 403, "Access denied")
		return nil
	}
	if instance.Type != "opencode" {
		utils.Error(c, 409, "Maintenance drain currently supports OpenCode only")
		return nil
	}
	return instance
}
func maintenanceResult(state *models.RuntimeMaintenance, instance *models.Instance) gin.H {
	now := time.Now().UTC()
	draining, drained := false, false
	if state != nil && state.OwnerID == instance.UserID && state.Generation == instance.RuntimeGeneration && instance.Status == "running" {
		draining = state.Draining(now)
		drained = state.Drained(now)
	}
	return gin.H{"operation": state, "draining": draining, "drained": drained, "automatic_upgrade_available": false}
}
func (h *RuntimeMaintenanceHandler) Status(c *gin.Context) {
	instance := h.instance(c, false)
	if instance == nil {
		return
	}
	state, err := h.store.Get(c.Request.Context(), instance.ID)
	if err != nil {
		utils.Error(c, 503, "Maintenance status unavailable")
		return
	}
	c.Header("Cache-Control", "no-store")
	utils.Success(c, 200, "Maintenance status", maintenanceResult(state, instance))
}
func (h *RuntimeMaintenanceHandler) Begin(c *gin.Context) {
	instance := h.instance(c, true)
	if instance == nil {
		return
	}
	if instance.Status != "running" {
		utils.Error(c, 409, "Instance must be running to confirm drain")
		return
	}
	var input struct {
		Timeout int `json:"timeout_seconds"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	if err := c.ShouldBindJSON(&input); err != nil || input.Timeout < 10 || input.Timeout > 600 {
		utils.Error(c, 400, "timeout_seconds must be between 10 and 600")
		return
	}
	state, err := h.store.Begin(c.Request.Context(), instance.ID, instance.UserID, instance.RuntimeGeneration, c.GetInt("userID"), time.Duration(input.Timeout)*time.Second)
	if errors.Is(err, models.ErrRuntimeMaintenanceConflict) || errors.Is(err, models.ErrRuntimeUpgradeInProgress) {
		utils.Error(c, 409, err.Error())
		return
	}
	if err != nil {
		utils.Error(c, 503, "Cannot begin maintenance")
		return
	}
	c.Header("Cache-Control", "no-store")
	utils.Success(c, 202, "Drain requested; no process stopped or image changed", maintenanceResult(state, instance))
}
func (h *RuntimeMaintenanceHandler) Cancel(c *gin.Context) {
	instance := h.instance(c, true)
	if instance == nil {
		return
	}
	var input struct {
		Operation string `json:"operation_id"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	if err := c.ShouldBindJSON(&input); err != nil || len(input.Operation) != 32 {
		utils.Error(c, 400, "Matching operation_id required")
		return
	}
	err := h.store.Cancel(c.Request.Context(), instance.ID, input.Operation)
	if errors.Is(err, models.ErrRuntimeMaintenanceConflict) || errors.Is(err, models.ErrRuntimeUpgradeInProgress) {
		utils.Error(c, 409, err.Error())
		return
	}
	if err != nil {
		utils.Error(c, 503, "Cannot cancel maintenance")
		return
	}
	utils.Success(c, 200, "Drain cancelled; runtime reopens admission on its next successful poll", nil)
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
