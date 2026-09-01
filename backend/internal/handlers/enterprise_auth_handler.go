package handlers

import (
	"errors"
	"net/http"

	"clawreef/internal/repository"
	"clawreef/internal/services"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

type EnterpriseAuthHandler struct {
	diagnostics services.EnterpriseAuthDiagnostics
	manager     *services.EnterpriseAuthManager
}

func NewEnterpriseAuthHandler(diagnostics services.EnterpriseAuthDiagnostics, managers ...*services.EnterpriseAuthManager) *EnterpriseAuthHandler {
	var manager *services.EnterpriseAuthManager
	if len(managers) > 0 {
		manager = managers[0]
	}
	return &EnterpriseAuthHandler{diagnostics: diagnostics, manager: manager}
}

func (h *EnterpriseAuthHandler) Status(c *gin.Context) {
	if h.diagnostics == nil {
		utils.Success(c, http.StatusOK, "Enterprise authentication status retrieved successfully", services.EnterpriseAuthStatus{
			Enabled:    false,
			Provider:   services.AuthProviderLDAP,
			Configured: false,
			Checks: map[string]string{
				"dial":         "skipped",
				"service_bind": "skipped",
				"user_search":  "skipped",
				"group_search": "skipped",
			},
		})
		return
	}
	utils.Success(c, http.StatusOK, "Enterprise authentication status retrieved successfully", h.diagnostics.Status(c.Request.Context()))
}

func (h *EnterpriseAuthHandler) Config(c *gin.Context) {
	if h.manager == nil {
		utils.Error(c, http.StatusServiceUnavailable, "enterprise auth settings are unavailable")
		return
	}
	utils.Success(c, http.StatusOK, "Enterprise authentication config retrieved successfully", h.manager.Config(c.Request.Context()))
}

func (h *EnterpriseAuthHandler) TestConfig(c *gin.Context) {
	if h.manager == nil {
		utils.Error(c, http.StatusServiceUnavailable, "enterprise auth settings are unavailable")
		return
	}
	var req services.EnterpriseAuthConfigUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	status, err := h.manager.TestConfig(c.Request.Context(), req)
	if err != nil {
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	utils.Success(c, http.StatusOK, "Enterprise authentication config tested successfully", status)
}

func (h *EnterpriseAuthHandler) UpdateConfig(c *gin.Context) {
	if h.manager == nil {
		utils.Error(c, http.StatusServiceUnavailable, "enterprise auth settings are unavailable")
		return
	}
	var req services.EnterpriseAuthConfigUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.ValidationError(c, err)
		return
	}
	var updatedBy *int
	if userIDValue, exists := c.Get("userID"); exists {
		if userID, ok := userIDValue.(int); ok {
			updatedBy = &userID
		}
	}
	response, err := h.manager.UpdateConfig(c.Request.Context(), req, updatedBy)
	if err != nil {
		if errors.Is(err, repository.ErrEnterpriseAuthVersionConflict) {
			utils.Error(c, http.StatusConflict, err.Error())
			return
		}
		utils.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	utils.Success(c, http.StatusOK, "Enterprise authentication config saved successfully", response)
}
