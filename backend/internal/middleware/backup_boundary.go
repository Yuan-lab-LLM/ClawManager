package middleware

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"clawreef/internal/backupgate"
	"github.com/gin-gonic/gin"
)

// BackupRequestBoundary includes GET because some existing read endpoints
// create workspace directories or audit records. Only registered backup control
// routes and the pure version route are exempt; a matching unknown URL is not.
func BackupRequestBoundary(pool *sql.DB, scope string) gin.HandlerFunc {
	return backupRequestBoundary(scope, func(ctx context.Context) (func(), error) { return backupgate.HoldRequest(ctx, pool, scope) })
}
func backupRequestBoundary(scope string, acquire func(context.Context) (func(), error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.FullPath()
		if scope == "" || strings.HasPrefix(path, "/api/v1/admin/system-backup/") || (c.Request.Method == http.MethodGet && path == "/api/v1/version") {
			c.Next()
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		release, e := acquire(ctx)
		cancel()
		if e != nil {
			code := "backup_coordination_unavailable"
			if errors.Is(e, backupgate.ErrMaintenance) {
				code = "backup_maintenance"
			}
			c.Header("Retry-After", "15")
			c.Header("Cache-Control", "no-store")
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"success": false, "code": code, "error": code})
			return
		}
		defer release()
		c.Next()
	}
}
