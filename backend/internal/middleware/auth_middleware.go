package middleware

import (
	"errors"
	"net/http"
	"os"
	"strings"

	"clawreef/internal/repository"
	"clawreef/internal/utils"
	"github.com/gin-gonic/gin"
)

// Auth middleware validates JWT token
func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString, ok := extractToken(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"error":   "Authorization header required",
			})
			c.Abort()
			return
		}

		claims, err := validateUserAccessToken(tokenString)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"error":   "Invalid or expired token",
			})
			c.Abort()
			return
		}

		// Check token type
		if claims.TokenType != "access" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"error":   "Invalid token type",
			})
			c.Abort()
			return
		}

		// Set user ID in context
		c.Set("userID", claims.UserID)
		c.Next()
	}
}

// GatewayAuth accepts either a normal user access JWT or an instance lifecycle gateway token.
func GatewayAuth(instanceRepo repository.InstanceRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString, ok := extractToken(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"error":   "Authorization header required",
			})
			c.Abort()
			return
		}

		if claims, err := validateUserAccessToken(tokenString); err == nil {
			c.Set("userID", claims.UserID)
			c.Set("gatewayAuthType", "user")
			c.Next()
			return
		}

		instance, err := instanceRepo.GetByAccessToken(tokenString)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"error":   "Invalid gateway token",
			})
			c.Abort()
			return
		}
		if instance == nil || instance.AccessToken == nil || strings.TrimSpace(*instance.AccessToken) == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"error":   "Invalid gateway token",
			})
			c.Abort()
			return
		}

		c.Set("userID", instance.UserID)
		c.Set("instanceID", instance.ID)
		c.Set("gatewayAuthType", "instance")
		c.Next()
	}
}

func extractToken(c *gin.Context) (string, bool) {
	authHeader := c.GetHeader("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			return "", false
		}
		return parts[1], true
	}

	// Browsers cannot set custom Authorization headers for native WebSocket
	// handshakes, so allow `?token=` specifically for upgrade requests.
	if strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
		token := strings.TrimSpace(c.Query("token"))
		if token != "" {
			return token, true
		}
	}

	return "", false
}

func getJWTSecret() string {
	// Get from environment variable, fallback to default
	// Must match the secret used in config.go
	if secret := os.Getenv("JWT_SECRET"); secret != "" {
		return secret
	}
	return "clawreef-dev-secret-key-change-in-production"
}

func validateUserAccessToken(tokenString string) (*utils.TokenClaims, error) {
	jwtSecret := getJWTSecret()
	claims, err := utils.ValidateToken(tokenString, jwtSecret)
	if err != nil {
		return nil, err
	}
	if claims.TokenType != "access" {
		return nil, errors.New("invalid token type")
	}
	return claims, nil
}
