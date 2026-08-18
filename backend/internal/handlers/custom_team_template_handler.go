package handlers

import (
	"net/http"
	"strconv"

	"clawreef/internal/teamtemplate"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

type CustomTeamTemplateHandler struct {
	service teamtemplate.Service
}

func NewCustomTeamTemplateHandler(service teamtemplate.Service) *CustomTeamTemplateHandler {
	return &CustomTeamTemplateHandler{service: service}
}

func (h *CustomTeamTemplateHandler) Generate(c *gin.Context) {
	var req teamtemplate.GenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	payload, err := h.service.Generate(c.Request.Context(), currentUserID(c), req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusCreated, "Custom Team template generated successfully", payload)
}

func (h *CustomTeamTemplateHandler) List(c *gin.Context) {
	items, err := h.service.List(currentUserID(c))
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Custom Team templates retrieved successfully", gin.H{"templates": items})
}

func (h *CustomTeamTemplateHandler) Get(c *gin.Context) {
	id, ok := parseCustomTeamTemplateID(c)
	if !ok {
		return
	}
	payload, err := h.service.Get(currentUserID(c), id)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Custom Team template retrieved successfully", payload)
}

func (h *CustomTeamTemplateHandler) UpdateMetadata(c *gin.Context) {
	id, ok := parseCustomTeamTemplateID(c)
	if !ok {
		return
	}
	var req teamtemplate.UpdateMetadataRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	payload, err := h.service.UpdateMetadata(currentUserID(c), id, req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Custom Team template updated successfully", payload)
}

func (h *CustomTeamTemplateHandler) Revise(c *gin.Context) {
	id, ok := parseCustomTeamTemplateID(c)
	if !ok {
		return
	}
	var req teamtemplate.ReviseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	payload, err := h.service.Revise(c.Request.Context(), currentUserID(c), id, req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Custom Team template revised successfully", payload)
}

func (h *CustomTeamTemplateHandler) AdjustMember(c *gin.Context) {
	id, ok := parseCustomTeamTemplateID(c)
	if !ok {
		return
	}
	var req teamtemplate.AdjustMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	payload, err := h.service.AdjustMember(c.Request.Context(), currentUserID(c), id, c.Param("memberID"), req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Custom Team member adjusted successfully", payload)
}

func (h *CustomTeamTemplateHandler) RegenerateMember(c *gin.Context) {
	id, ok := parseCustomTeamTemplateID(c)
	if !ok {
		return
	}
	var req teamtemplate.RegenerateMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	payload, err := h.service.RegenerateMember(c.Request.Context(), currentUserID(c), id, c.Param("memberID"), req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Custom Team member regenerated successfully", payload)
}

func (h *CustomTeamTemplateHandler) Regenerate(c *gin.Context) {
	id, ok := parseCustomTeamTemplateID(c)
	if !ok {
		return
	}
	var req teamtemplate.RegenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	payload, err := h.service.Regenerate(c.Request.Context(), currentUserID(c), id, req)
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Custom Team template regenerated successfully", payload)
}

func (h *CustomTeamTemplateHandler) Delete(c *gin.Context) {
	id, ok := parseCustomTeamTemplateID(c)
	if !ok {
		return
	}
	if err := h.service.Delete(currentUserID(c), id); err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, http.StatusOK, "Custom Team template deleted successfully", nil)
}

func currentUserID(c *gin.Context) int {
	value, _ := c.Get("userID")
	userID, _ := value.(int)
	return userID
}

func parseCustomTeamTemplateID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		utils.Error(c, http.StatusBadRequest, "invalid custom team template id")
		return 0, false
	}
	return id, true
}
