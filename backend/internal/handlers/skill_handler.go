package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"clawreef/internal/services"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

type SkillHandler struct {
	service         services.SkillService
	instanceService services.InstanceService
}

func NewSkillHandler(service services.SkillService, instanceService services.InstanceService) *SkillHandler {
	return &SkillHandler{service: service, instanceService: instanceService}
}

func (h *SkillHandler) ImportSkills(c *gin.Context) {
	userID, _ := c.Get("userID")
	fileHeader, err := c.FormFile("file")
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "file is required")
		return
	}
	items, err := h.service.ImportArchive(c.Request.Context(), userID.(int), fileHeader)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "Skills imported successfully", items)
}

func (h *SkillHandler) ListSkills(c *gin.Context) {
	userID, _ := c.Get("userID")
	items, err := h.service.ListSkills(userID.(int))
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Skills retrieved successfully", items)
}

func (h *SkillHandler) ListAllSkills(c *gin.Context) {
	items, err := h.service.ListAllSkills()
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "All skills retrieved successfully", items)
}

func (h *SkillHandler) GetSkill(c *gin.Context) {
	userID, _ := c.Get("userID")
	skillID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "invalid skill ID")
		return
	}
	item, err := h.service.GetSkill(userID.(int), skillID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Skill retrieved successfully", item)
}

func (h *SkillHandler) UpdateSkill(c *gin.Context) {
	userID, _ := c.Get("userID")
	skillID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "invalid skill ID")
		return
	}
	var req services.UpdateSkillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	item, err := h.service.UpdateSkill(userID.(int), skillID, req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Skill updated successfully", item)
}

func (h *SkillHandler) DeleteSkill(c *gin.Context) {
	userID, _ := c.Get("userID")
	skillID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "invalid skill ID")
		return
	}
	if err := h.service.DeleteSkill(userID.(int), skillID); err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Skill deleted successfully", nil)
}

func (h *SkillHandler) DownloadSkill(c *gin.Context) {
	userID, _ := c.Get("userID")
	skillID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "invalid skill ID")
		return
	}
	content, fileName, err := h.service.DownloadSkill(userID.(int), skillID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", fileName))
	c.Data(http.StatusOK, "application/zip", content)
}

func (h *SkillHandler) DownloadSkillVersionForAgent(c *gin.Context) {
	content, fileName, err := h.service.DownloadSkillVersionByExternalID(c.Param("skillVersion"))
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", fileName))
	c.Data(http.StatusOK, "application/octet-stream", content)
}

func (h *SkillHandler) ListVersions(c *gin.Context) {
	userID, _ := c.Get("userID")
	skillID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "invalid skill ID")
		return
	}
	items, err := h.service.ListVersions(userID.(int), skillID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Skill versions retrieved successfully", items)
}

func (h *SkillHandler) ListScanResults(c *gin.Context) {
	userID, _ := c.Get("userID")
	skillID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "invalid skill ID")
		return
	}
	items, err := h.service.ListScanResults(userID.(int), skillID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Skill scan results retrieved successfully", items)
}

func (h *SkillHandler) ListAvailableInstanceSkills(c *gin.Context) {
	instanceID, ok := h.authorizeOwnedInstance(c)
	if !ok {
		return
	}
	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")
	items, err := h.service.ListAvailableSkillsForInstance(instanceID, userID.(int), fmt.Sprint(userRole))
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Available instance skills retrieved successfully", items)
}
func (h *SkillHandler) ListInstanceSkills(c *gin.Context) {
	instanceID, ok := h.authorizeOwnedInstance(c)
	if !ok {
		return
	}
	items, err := h.service.ListInstanceSkills(instanceID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Instance skills retrieved successfully", items)
}

func (h *SkillHandler) AttachSkillToInstance(c *gin.Context) {
	instanceID, ok := h.authorizeOwnedInstance(c)
	if !ok {
		return
	}
	var req services.AttachSkillToInstanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")
	item, err := h.service.AttachSkillToInstanceForActor(instanceID, req.SkillID, userID.(int), fmt.Sprint(userRole))
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "Skill attached to instance successfully", item)
}

func (h *SkillHandler) RemoveSkillFromInstance(c *gin.Context) {
	instanceID, ok := h.authorizeOwnedInstance(c)
	if !ok {
		return
	}
	skillID, err := strconv.Atoi(c.Param("skillId"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "invalid skill ID")
		return
	}
	if err := h.service.RemoveSkillFromInstance(instanceID, skillID); err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Skill removed from instance successfully", nil)
}

func (h *SkillHandler) authorizeOwnedInstance(c *gin.Context) (int, bool) {
	instanceID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "invalid instance ID")
		return 0, false
	}
	instance, err := h.instanceService.GetByID(instanceID)
	if err != nil {
		utils.HandleError(c, err)
		return 0, false
	}
	if instance == nil {
		utils.Error(c, http.StatusNotFound, "Instance not found")
		return 0, false
	}
	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")
	if userRole != "admin" && instance.UserID != userID.(int) {
		utils.Error(c, http.StatusForbidden, "Access denied")
		return 0, false
	}
	return instanceID, true
}
