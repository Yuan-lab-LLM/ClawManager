package middleware

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"clawreef/internal/models"
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

// GatewayAuth accepts a user JWT, a governed Gateway Key, or a legacy instance
// lifecycle token. The legacy branch remains during the `igt_` migration.
func GatewayAuth(instanceRepo repository.InstanceRepository, gatewayKeyRepo repository.GatewayKeyRepository, bindingRepos ...repository.InstanceRuntimeBindingRepository) gin.HandlerFunc {
	var bindingRepo repository.InstanceRuntimeBindingRepository
	if len(bindingRepos) > 0 {
		bindingRepo = bindingRepos[0]
	}
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

		if gatewayKeyRepo != nil {
			key, err := gatewayKeyRepo.GetByToken(tokenString)
			if err != nil {
				c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "Invalid gateway token"})
				c.Abort()
				return
			}
			if key != nil {
				if !key.IsUsable(time.Now().UTC()) {
					c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "Gateway key is disabled, revoked, or expired"})
					c.Abort()
					return
				}
				_ = gatewayKeyRepo.Touch(c.Request.Context(), key.ID, time.Now().UTC())
				c.Set("gatewayKeyID", key.KeyID)
				c.Set("gatewayKeyDBID", key.ID)
				c.Set("gatewayKeyType", key.KeyType)
				c.Set("gatewayKeyRPMLimit", key.RPMLimit)
				c.Set("gatewayKeyTPMLimit", key.TPMLimit)
				c.Set("gatewayKeyConcurrencyLimit", key.ConcurrentRequestsLimit)
				if key.AllowedModelGroups != nil {
					var groups []string
					if json.Unmarshal([]byte(*key.AllowedModelGroups), &groups) == nil {
						c.Set("gatewayAllowedModelGroups", groups)
					}
				}
				if key.OwnerUserID != nil {
					c.Set("userID", *key.OwnerUserID)
				}
				if key.InstanceID != nil {
					instance, getErr := instanceRepo.GetByID(*key.InstanceID)
					if getErr != nil || instance == nil {
						c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "Gateway key instance is unavailable"})
						c.Abort()
						return
					}
					applyGatewayInstanceContext(c, instance, bindingRepo)
					c.Set("gatewayAuthType", "virtual_key")
				} else {
					c.Set("gatewayAuthType", "virtual_key")
				}
				principal, principalExists := c.Get("userID")
				principalID, principalOK := principal.(int)
				if !principalExists || !principalOK || principalID <= 0 {
					c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "Gateway key owner is unavailable"})
					c.Abort()
					return
				}
				c.Next()
				return
			}
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

		applyGatewayInstanceContext(c, instance, bindingRepo)
		c.Set("gatewayAuthType", "instance")
		c.Next()
	}
}

func applyGatewayInstanceContext(c *gin.Context, instance *models.Instance, bindingRepo repository.InstanceRuntimeBindingRepository) {
	c.Set("userID", instance.UserID)
	c.Set("instanceID", instance.ID)
	c.Set("runtimeGeneration", instance.RuntimeGeneration)
	c.Set("instanceType", strings.TrimSpace(instance.Type))
	c.Set("instanceMode", gatewayInstanceMode(instance.InstanceMode, instance.RuntimeType))
	c.Set("runtimeType", strings.TrimSpace(instance.RuntimeType))
	if bindingRepo != nil {
		if binding, err := bindingRepo.GetRunningByInstanceID(c.Request.Context(), instance.ID); err == nil && binding != nil {
			c.Set("gatewayID", strings.TrimSpace(binding.GatewayID))
			c.Set("runtimePodID", binding.RuntimePodID)
		}
	}
}

func gatewayInstanceMode(instanceMode, runtimeType string) string {
	mode := strings.ToLower(strings.TrimSpace(instanceMode))
	if mode == "lite" || mode == "pro" {
		return mode
	}
	switch strings.ToLower(strings.TrimSpace(runtimeType)) {
	case "gateway":
		return "lite"
	case "desktop", "shell":
		return "pro"
	default:
		return mode
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
	if token := strings.TrimSpace(c.GetHeader("x-api-key")); token != "" {
		return token, true
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
