package handlers

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services"
	"clawreef/internal/utils"
	"clawreef/internal/workbench"
	"github.com/gin-gonic/gin"
)

type sharedWorkspaceInstances interface {
	GetByID(int) (*models.Instance, error)
}
type sharedWorkspaceSession struct {
	instance *models.Instance
	preview  workbench.SharedPreviewAccess
}

// Both files and workbench use the same code-bound cookie, current permissions,
// owner check and CSRF requirement. A bearer login never supplies share scope.
func resolveSharedWorkspaceSession(c *gin.Context, instances sharedWorkspaceInstances, external services.InstanceExternalAccessService, tokens *services.InstanceAccessService, requireWrite bool) (*sharedWorkspaceSession, bool) {
	fail := func(status int, message string) (*sharedWorkspaceSession, bool) {
		utils.Error(c, status, message)
		return nil, false
	}
	if external == nil || tokens == nil || instances == nil {
		return fail(http.StatusServiceUnavailable, "Shared workspace access is not configured")
	}
	code := strings.TrimSpace(c.Param("code"))
	access, err := external.ResolveShortLink(c.Request.Context(), code)
	if err != nil || access == nil || !access.Enabled || (access.ExpiresAt != nil && !time.Now().Before(*access.ExpiresAt)) {
		return fail(http.StatusUnauthorized, "Share link expired or invalid")
	}
	if access.AuthMode != services.ExternalAccessModeShareLink && access.AuthMode != services.ExternalAccessModePassword {
		return fail(http.StatusBadRequest, "Unsupported share link mode")
	}
	permission, err := services.NormalizeExternalWorkspaceAccess(access.WorkspaceAccess)
	if err != nil || permission == services.ExternalWorkspaceAccessNone {
		return fail(http.StatusForbidden, "Workspace access is not enabled for this share link")
	}
	if requireWrite && permission != services.ExternalWorkspaceAccessWrite {
		return fail(http.StatusForbidden, "This share link has read-only workspace access")
	}
	cookie, err := c.Cookie(shortExternalAccessCookieName(code))
	if err != nil || strings.TrimSpace(cookie) == "" {
		return fail(http.StatusUnauthorized, "Open the share link before accessing the workspace")
	}
	token, err := tokens.ValidateToken(cookie)
	if err != nil || token.InstanceID != access.InstanceID || token.SessionBinding != sharedExternalAccessSessionBinding(code, access) {
		return fail(http.StatusUnauthorized, "Share session expired or invalid")
	}
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
		expected := sharedExternalAccessCSRFToken(code, cookie)
		actual := strings.TrimSpace(c.GetHeader(sharedWorkspaceCSRFHeader))
		if expected == "" || subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) != 1 {
			c.JSON(http.StatusForbidden, gin.H{"success": false, "error": "Invalid share request", "error_code": "share_csrf_invalid"})
			return nil, false
		}
	}
	instance, err := instances.GetByID(access.InstanceID)
	if err != nil {
		utils.HandleError(c, err)
		return nil, false
	}
	if instance == nil {
		return fail(http.StatusNotFound, "Instance not found")
	}
	if instance.ID != access.InstanceID || token.UserID != instance.UserID {
		return fail(http.StatusUnauthorized, "Share session does not match instance owner")
	}
	expiry := token.ExpiresAt
	if access.ExpiresAt != nil && access.ExpiresAt.Before(expiry) {
		expiry = *access.ExpiresAt
	}
	return &sharedWorkspaceSession{instance: instance, preview: workbench.SharedPreviewAccess{Code: code, SessionBinding: token.SessionBinding, WorkspaceAccess: permission, ExpiresAt: expiry}}, true
}
