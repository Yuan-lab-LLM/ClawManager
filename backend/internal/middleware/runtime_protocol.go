package middleware

import (
	"net/http"

	"clawreef/internal/repository"
	"github.com/gin-gonic/gin"
)

// Attach after GatewayAuth and only to model-generation routes. Catalog reads
// and capability negotiation must also work before enrollment.
func RuntimeProtocolPolicy(repo repository.RuntimeProtocolRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		instanceID := c.GetInt("instanceID")
		if instanceID <= 0 {
			c.Next()
			return
		}
		state, err := repo.Get(c.Request.Context(), instanceID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "Runtime governance policy unavailable"})
			return
		}
		if state != nil {
			c.Set("runtimeGovernanceProtocol", state.Protocol)
		}
		c.Next()
	}
}
