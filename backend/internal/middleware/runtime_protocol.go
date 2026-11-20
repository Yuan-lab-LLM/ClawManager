package middleware

import (
	"net/http"

	"clawreef/internal/models"
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
			// Only an absent enrollment is legacy. Unknown/corrupt protocol state
			// cannot be interpreted as compatible or silently downgraded to v1.
			if state.Protocol != models.RuntimeGovernanceProtocol {
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "Unsupported runtime governance policy"})
				return
			}
			c.Set("runtimeGovernanceProtocol", state.Protocol)
		}
		c.Next()
	}
}
