package northbound

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type AuthHandler struct{ service *AuthService }

func NewAuthHandler(service *AuthService) *AuthHandler { return &AuthHandler{service: service} }

func (h *AuthHandler) Challenge(c *gin.Context) {
	result, err := h.service.CreateChallenge(c.Request.Context(), c.ClientIP())
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req struct {
		ChallengeID   string `json:"challenge_id" binding:"required"`
		CredentialJWE string `json:"credential_jwe" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, apiError(400, "INVALID_REQUEST", "Invalid login request", err))
		return
	}
	result, err := h.service.Login(c.Request.Context(), req.ChallengeID, req.CredentialJWE, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		writeError(c, err)
		return
	}
	c.Set("northboundPrincipal", &Principal{UserID: result.UserID, SessionID: result.SessionID, Scopes: result.Scopes})
	c.JSON(http.StatusOK, result)
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeError(c, apiError(400, "INVALID_REQUEST", "Invalid refresh request", err))
		return
	}
	result, err := h.service.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Set("northboundPrincipal", &Principal{UserID: result.UserID, SessionID: result.SessionID, Scopes: result.Scopes})
	c.JSON(http.StatusOK, result)
}

func (h *AuthHandler) Logout(c *gin.Context) {
	principal := currentPrincipal(c)
	if principal != nil && principal.AuthType == "api_key" {
		writeError(c, apiError(400, "AUTH_METHOD_UNSUPPORTED", "Revoke API keys in administrator settings", nil))
		return
	}
	if principal == nil {
		writeError(c, apiError(401, "AUTH_INVALID", "Authentication required", nil))
		return
	}
	if err := h.service.Logout(c.Request.Context(), principal.SessionID); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *AuthHandler) Me(c *gin.Context) {
	principal := currentPrincipal(c)
	if principal == nil {
		writeError(c, apiError(401, "AUTH_INVALID", "Authentication required", nil))
		return
	}
	user, err := h.service.CurrentUser(principal.UserID)
	if err != nil || user == nil {
		writeError(c, apiError(401, "AUTH_INVALID", "Authentication required", err))
		return
	}
	if principal.AuthType == "api_key" {
		c.JSON(http.StatusOK, gin.H{"id": user.ID, "username": user.Username, "scopes": principal.Scopes, "auth_type": "api_key", "key_id": principal.KeyID})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id":         user.ID,
		"username":   user.Username,
		"scopes":     principal.Scopes,
		"session_id": principal.SessionID,
	})
}

func (h *AuthHandler) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if values, exists := c.Request.Header["X-Api-Key"]; exists {
			if len(values) != 1 || len(c.Request.Header.Values("Authorization")) > 0 {
				writeError(c, apiError(400, "INVALID_REQUEST", "Use exactly one authentication credential", nil))
				c.Abort()
				return
			}
			principal, err := h.service.AuthenticateAPIKey(values[0])
			if err != nil {
				writeError(c, err)
				c.Abort()
				return
			}
			c.Set("northboundPrincipal", principal)
			c.Set("northboundAuthType", "api_key")
			c.Set("northboundKeyID", principal.KeyID)
			c.Next()
			return
		}
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
			writeError(c, apiError(401, "AUTH_INVALID", "Authentication required", nil))
			c.Abort()
			return
		}
		principal, err := h.service.AuthenticateAccess(strings.TrimSpace(parts[1]))
		if err != nil {
			writeError(c, err)
			c.Abort()
			return
		}
		c.Set("northboundPrincipal", principal)
		c.Next()
	}
}

func RequireScope(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		principal := currentPrincipal(c)
		if principal == nil || !principal.HasScope(scope) {
			writeError(c, apiError(403, "SCOPE_DENIED", "Insufficient permission", nil))
			c.Abort()
			return
		}
		c.Next()
	}
}

func RequireAnyScope(scopes ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		principal := currentPrincipal(c)
		if principal != nil {
			for _, scope := range scopes {
				if principal.HasScope(scope) {
					c.Next()
					return
				}
			}
		}
		writeError(c, apiError(403, "SCOPE_DENIED", "Insufficient permission", nil))
		c.Abort()
	}
}

func currentPrincipal(c *gin.Context) *Principal {
	value, ok := c.Get("northboundPrincipal")
	if !ok {
		return nil
	}
	principal, _ := value.(*Principal)
	return principal
}
