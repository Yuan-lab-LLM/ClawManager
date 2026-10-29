package handlers

import (
	"io"

	"github.com/gin-gonic/gin"
)

// Preview paths contain bearer grants. Their authorization failures are handled
// by the proxy; raw request paths must never enter the general access log.
func (h *EgressProxyHandler) AccessLogger(output io.Writer) gin.HandlerFunc {
	return gin.LoggerWithConfig(gin.LoggerConfig{Output: output, Skip: func(c *gin.Context) bool {
		return h.isTeamPreviewRequest(c.Request)
	}})
}
