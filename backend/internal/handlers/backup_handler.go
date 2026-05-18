package handlers

import (
	"net/http"
	"strconv"

	"clawreef/internal/services"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

// BackupHandler handles instance backup APIs.
type BackupHandler struct {
	service services.BackupService
}

// NewBackupHandler creates a new backup handler.
func NewBackupHandler(service services.BackupService) *BackupHandler {
	return &BackupHandler{service: service}
}

// createBackupRequest is the JSON body for creating a backup.
type createBackupRequest struct {
	BackupName string `json:"backup_name" binding:"required"`
}

// CreateBackup creates a new backup for an instance.
func (h *BackupHandler) CreateBackup(c *gin.Context) {
	userID, _ := c.Get("userID")
	instanceID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	var req createBackupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}

	backup, err := h.service.CreateBackup(userID.(int), instanceID, req.BackupName)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "Backup creation started", backup)
}

// ListBackups lists all backups for an instance.
func (h *BackupHandler) ListBackups(c *gin.Context) {
	userID, _ := c.Get("userID")
	instanceID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid instance ID")
		return
	}

	backups, err := h.service.ListBackups(userID.(int), instanceID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Backups retrieved successfully", backups)
}

// GetBackup gets a single backup by ID.
func (h *BackupHandler) GetBackup(c *gin.Context) {
	userID, _ := c.Get("userID")
	backupID, err := strconv.Atoi(c.Param("backupId"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid backup ID")
		return
	}

	backup, err := h.service.GetBackup(userID.(int), backupID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Backup retrieved successfully", backup)
}

// DeleteBackup soft-deletes a backup.
func (h *BackupHandler) DeleteBackup(c *gin.Context) {
	userID, _ := c.Get("userID")
	backupID, err := strconv.Atoi(c.Param("backupId"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid backup ID")
		return
	}

	if err := h.service.DeleteBackup(userID.(int), backupID); err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Backup deleted successfully", nil)
}

// RestoreBackup restores an instance from a backup.
func (h *BackupHandler) RestoreBackup(c *gin.Context) {
	userID, _ := c.Get("userID")
	backupID, err := strconv.Atoi(c.Param("backupId"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "Invalid backup ID")
		return
	}

	if err := h.service.RestoreBackup(userID.(int), backupID); err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Backup restore started", nil)
}

