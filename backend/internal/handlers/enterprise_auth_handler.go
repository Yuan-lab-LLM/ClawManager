package handlers

import (
	"net/http"

	"clawreef/internal/services"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
)

type EnterpriseAuthHandler struct {
	diagnostics services.EnterpriseAuthDiagnostics
}

func NewEnterpriseAuthHandler(diagnostics services.EnterpriseAuthDiagnostics) *EnterpriseAuthHandler {
	return &EnterpriseAuthHandler{diagnostics: diagnostics}
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
