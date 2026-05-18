package handlers

import (
	"net/http"
	"strconv"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

// BackupScheduleHandler handles backup schedule CRUD APIs.
type BackupScheduleHandler struct {
	repo repository.BackupScheduleRepository
}

// NewBackupScheduleHandler creates a new backup schedule handler.
func NewBackupScheduleHandler(repo repository.BackupScheduleRepository) *BackupScheduleHandler {
	return &BackupScheduleHandler{repo: repo}
}

type createScheduleRequest struct {
	ScheduleName   string `json:"schedule_name"`
	CronExpression string `json:"cron_expression" binding:"required"`
	RetentionDays  int    `json:"retention_days" binding:"required"`
}

type updateScheduleRequest struct {
	ScheduleName   *string `json:"schedule_name"`
	CronExpression *string `json:"cron_expression"`
	RetentionDays  *int    `json:"retention_days"`
	IsActive       *bool   `json:"is_active"`
}

// CreateSchedule creates a new backup schedule for an instance.
func (h *BackupScheduleHandler) CreateSchedule(c *gin.Context) {
	instanceID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	var req createScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	if req.RetentionDays < 1 {
		utils.Error(c, http.StatusBadRequest, "retention_days must be at least 1")
		return
	}

	if err := services.ValidateCronExpression(req.CronExpression); err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	schedule := &models.BackupSchedule{
		InstanceID:     instanceID,
		CronExpression: req.CronExpression,
		RetentionDays:  req.RetentionDays,
	}
	if req.ScheduleName != "" {
		schedule.ScheduleName = &req.ScheduleName
	}

	if err := h.repo.Create(schedule); err != nil {
		utils.Error(c, http.StatusInternalServerError, "Failed to create backup schedule")
		return
	}

	utils.Success(c, http.StatusCreated, "Backup schedule created", schedule)
}

// ListSchedules lists all backup schedules for an instance.
func (h *BackupScheduleHandler) ListSchedules(c *gin.Context) {
	instanceID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	schedules, err := h.repo.ListByInstanceID(instanceID)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "Failed to list backup schedules")
		return
	}

	utils.Success(c, http.StatusOK, "Backup schedules retrieved", schedules)
}

// UpdateSchedule updates an existing backup schedule.
func (h *BackupScheduleHandler) UpdateSchedule(c *gin.Context) {
	scheduleID, err := strconv.Atoi(c.Param("sid"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid schedule ID")
		return
	}

	existing, err := h.repo.GetByID(scheduleID)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "Failed to get backup schedule")
		return
	}
	if existing == nil {
		utils.Error(c, http.StatusNotFound, "Backup schedule not found")
		return
	}

	var req updateScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	if req.ScheduleName != nil {
		existing.ScheduleName = req.ScheduleName
	}
	if req.CronExpression != nil {
		if err := services.ValidateCronExpression(*req.CronExpression); err != nil {
			utils.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		existing.CronExpression = *req.CronExpression
	}
	if req.RetentionDays != nil {
		if *req.RetentionDays < 1 {
			utils.Error(c, http.StatusBadRequest, "retention_days must be at least 1")
			return
		}
		existing.RetentionDays = *req.RetentionDays
	}
	if req.IsActive != nil {
		existing.IsActive = *req.IsActive
	}

	if err := h.repo.Update(existing); err != nil {
		utils.Error(c, http.StatusInternalServerError, "Failed to update backup schedule")
		return
	}

	utils.Success(c, http.StatusOK, "Backup schedule updated", existing)
}


// DeleteSchedule deletes a backup schedule.
func (h *BackupScheduleHandler) DeleteSchedule(c *gin.Context) {
	scheduleID, err := strconv.Atoi(c.Param("sid"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid schedule ID")
		return
	}

	existing, err := h.repo.GetByID(scheduleID)
	if err != nil {
		utils.Error(c, http.StatusInternalServerError, "Failed to get backup schedule")
		return
	}
	if existing == nil {
		utils.Error(c, http.StatusNotFound, "Backup schedule not found")
		return
	}

	if err := h.repo.Delete(scheduleID); err != nil {
		utils.Error(c, http.StatusInternalServerError, "Failed to delete backup schedule")
		return
	}

	utils.Success(c, http.StatusOK, "Backup schedule deleted", nil)
}

