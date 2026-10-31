package handlers

import (
	"clawreef/internal/aigateway"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
	"net/http"
)

// Request-body attribution is never a source of authorization identity.
func (h *AIGatewayHandler) validateGatewayInstanceAttribution(c *gin.Context, req *aigateway.ChatCompletionRequest) bool {
	if value, exists := c.Get("instanceID"); exists {
		id, ok := value.(int)
		if !ok || id <= 0 || (req.InstanceID != nil && *req.InstanceID != id) {
			utils.Error(c, http.StatusForbidden, "Gateway token does not match requested instance")
			return false
		}
		req.InstanceID = &id
		return true
	}
	if req.InstanceID == nil {
		return true
	}
	userID, ok := gatewayUserID(c)
	if !ok || h.instanceService == nil {
		utils.Error(c, http.StatusForbidden, "Instance attribution is unavailable")
		return false
	}
	instance, err := h.instanceService.GetByID(*req.InstanceID)
	if err != nil || instance == nil || instance.UserID != userID {
		utils.Error(c, http.StatusForbidden, "Gateway owner does not match requested instance")
		return false
	}
	return true
}
