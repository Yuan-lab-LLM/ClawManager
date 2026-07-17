package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"clawreef/internal/services"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

type SkillHubHandler struct {
	service         services.SkillService
	instanceService services.InstanceService
}

func NewSkillHubHandler(service services.SkillService, instanceService services.InstanceService) *SkillHubHandler {
	return &SkillHubHandler{service: service, instanceService: instanceService}
}

func (h *SkillHubHandler) ListCatalog(c *gin.Context) {
	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")
	query := services.SkillHubCatalogQuery{
		TagKeys:  c.QueryArray("tag_keys"),
		Search:   strings.TrimSpace(c.Query("q")),
		Page:     parseIntDefault(c.Query("page"), 1),
		PageSize: parseIntDefault(c.Query("page_size"), 20),
	}
	result, err := h.service.ListHubCatalog(userID.(int), userRole.(string), query)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Skill hub catalog retrieved successfully", result)
}

func (h *SkillHubHandler) ListTags(c *gin.Context) {
	userRole, _ := c.Get("userRole")
	items, err := h.service.ListHubTags(userRole.(string))
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Skill hub tags retrieved successfully", items)
}

func (h *SkillHubHandler) ListMine(c *gin.Context) {
	userID, _ := c.Get("userID")
	items, err := h.service.ListMyHubSkills(userID.(int))
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "My skill hub items retrieved successfully", items)
}

func (h *SkillHubHandler) ListAttachable(c *gin.Context) {
	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")
	items, err := h.service.ListAttachableSkills(userID.(int), userRole.(string))
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Attachable skills retrieved successfully", items)
}

func (h *SkillHubHandler) GetSkill(c *gin.Context) {
	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")
	skillID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "invalid skill ID")
		return
	}
	item, err := h.service.GetSkillHubDetail(userID.(int), userRole.(string), skillID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Skill hub item retrieved successfully", item)
}

func (h *SkillHubHandler) PreviewImportSkills(c *gin.Context) {
	userID, _ := c.Get("userID")
	fileHeader, err := c.FormFile("file")
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "file is required")
		return
	}
	items, err := h.service.PreviewHubImport(c.Request.Context(), userID.(int), fileHeader)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Skill import preview generated successfully", items)
}

func (h *SkillHubHandler) ImportSkills(c *gin.Context) {
	userID, _ := c.Get("userID")
	fileHeader, err := c.FormFile("file")
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "file is required")
		return
	}
	var decisions []services.SkillImportDecision
	if raw := strings.TrimSpace(c.PostForm("decisions")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &decisions); err != nil {
			utils.Error(c, http.StatusBadRequest, "invalid decisions payload")
			return
		}
	}
	items, err := h.service.ImportHubArchiveWithDecisions(c.Request.Context(), userID.(int), fileHeader, decisions)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "Skills imported successfully", items)
}

func (h *SkillHubHandler) PublishSkill(c *gin.Context) {
	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")
	skillID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "invalid skill ID")
		return
	}
	var req services.PublishSkillHubRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	item, err := h.service.PublishToHub(userID.(int), userRole.(string), skillID, req.TagIDs)
	if err != nil {
		utils.HandleHubError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Skill published to hub successfully", item)
}

func (h *SkillHubHandler) UnpublishSkill(c *gin.Context) {
	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")
	skillID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "invalid skill ID")
		return
	}
	item, err := h.service.UnpublishFromHub(userID.(int), userRole.(string), skillID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Skill unpublished from hub successfully", item)
}

func (h *SkillHubHandler) UpdateTags(c *gin.Context) {
	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")
	skillID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "invalid skill ID")
		return
	}
	var req services.UpdateSkillHubTagsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	item, err := h.service.UpdateHubTags(userID.(int), userRole.(string), skillID, req.TagIDs)
	if err != nil {
		utils.HandleHubError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Skill hub tags updated successfully", item)
}

func (h *SkillHubHandler) DeleteSkill(c *gin.Context) {
	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")
	skillID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "invalid skill ID")
		return
	}
	if err := h.service.DeleteSkill(userID.(int), userRole.(string), skillID); err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Skill deleted successfully", nil)
}

func (h *SkillHubHandler) DownloadSkill(c *gin.Context) {
	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")
	skillID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "invalid skill ID")
		return
	}
	content, fileName, err := h.service.DownloadSkill(userID.(int), userRole.(string), skillID)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", fileName))
	c.Data(http.StatusOK, "application/zip", content)
}

func (h *SkillHubHandler) InstallSkill(c *gin.Context) {
	userID, _ := c.Get("userID")
	userRole, _ := c.Get("userRole")
	skillID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		utils.Error(c, http.StatusBadRequest, "invalid skill ID")
		return
	}
	var req services.InstallHubSkillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	item, err := h.service.InstallHubSkill(userID.(int), userRole.(string), skillID, req.InstanceID)
	if err != nil {
		utils.HandleHubError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "Skill installed to instance successfully", item)
}

func (h *SkillHubHandler) ListAdminSkills(c *gin.Context) {
	items, err := h.service.ListAllHubSkillsAdmin()
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Admin skill hub items retrieved successfully", items)
}

func parseIntDefault(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
