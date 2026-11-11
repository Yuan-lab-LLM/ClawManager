package handlers

import (
	"io"
	"strings"

	"github.com/gin-gonic/gin"
)

// Preview paths contain bearer grants. Their authorization failures are handled
// by the proxy; raw request paths must never enter the general access log.
func (h *EgressProxyHandler) AccessLogger(output io.Writer) gin.HandlerFunc {
	return gin.LoggerWithConfig(gin.LoggerConfig{Output: output, Formatter: safeAccessLogFormatter, Skip: func(c *gin.Context) bool {
		return h.isTeamPreviewRequest(c.Request) || strings.HasPrefix(c.Request.URL.Path, "/__clawmanager_preview/")
	}})
}
