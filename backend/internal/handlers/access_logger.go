package handlers

import (
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

// SafeAccessLogger records request metadata without credentials or query strings.
func SafeAccessLogger() gin.HandlerFunc {
	return gin.LoggerWithFormatter(safeAccessLogFormatter)
}

func safeAccessLogFormatter(param gin.LogFormatterParams) string {
	path, _, _ := strings.Cut(param.Path, "?")
	for _, prefix := range []string{"/__clawmanager_preview/", "/s/"} {
		if strings.HasPrefix(path, prefix) {
			path = prefix + "[redacted]"
			break
		}
	}
	return fmt.Sprintf("[GIN] %s | %3d | %13v | %15s | %-7s %q\n",
		param.TimeStamp.Format("2006/01/02 - 15:04:05"), param.StatusCode,
		param.Latency, param.ClientIP, param.Method, path)
}
