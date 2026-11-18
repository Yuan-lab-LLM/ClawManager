package handlers

import (
	"clawreef/internal/models"
	"clawreef/internal/services"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

func unavailableInstanceAccess(c *gin.Context, instance *models.Instance) {
	if instance != nil && strings.EqualFold(strings.TrimSpace(instance.Type), services.RuntimeTypeOpenCode) {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false, "error_code": "runtime_access_not_configured",
			"error": "OpenCode dedicated access origin is not configured correctly. Contact the administrator; restarting the instance will not fix this configuration.",
		})
		return
	}
	utils.Error(c, http.StatusServiceUnavailable, "Unable to generate access URL")
}
