package handlers

import (
	"clawreef/internal/models"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
)

func (h *InstanceHandler) GetAdminInstanceSummary(c *gin.Context) {
	if role, _ := c.Get("userRole"); role != "admin" {
		utils.Error(c, 403, "Access denied")
		return
	}
	service, ok := h.instanceService.(interface {
		GetAdminSummary() (*models.InstanceSummary, error)
	})
	if !ok {
		utils.Error(c, 503, "Instance summary is unavailable")
		return
	}
	summary, err := service.GetAdminSummary()
	if err != nil {
		utils.HandleError(c, err)
		return
	}
	utils.Success(c, 200, "Instance summary retrieved", summary)
}
