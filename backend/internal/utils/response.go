package utils

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

// Success sends a successful response
func Success(c *gin.Context, status int, message string, data interface{}) {
	c.JSON(status, gin.H{
		"success": true,
		"message": message,
		"data":    data,
	})
}

// Error sends an error response
func Error(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{
		"success": false,
		"error":   message,
	})
}

// HandleError handles different types of errors and sends appropriate responses
func HandleError(c *gin.Context, err error) {
	// Log the actual error for debugging
	log.Printf("[ERROR] %v", err)

	// Handle validation errors
	if validationErrors, ok := err.(validator.ValidationErrors); ok {
		Error(c, http.StatusBadRequest, formatValidationErrors(validationErrors))
		return
	}

	// Handle known errors
	errStr := err.Error()
	if strings.HasPrefix(errStr, "provider discovery failed:") || strings.HasPrefix(errStr, "failed to call provider discovery endpoint:") || strings.HasPrefix(errStr, "failed to decode provider discovery response:") {
		Error(c, http.StatusBadGateway, errStr)
		return
	}
	if strings.HasPrefix(errStr, "failed to get secret ") || strings.HasPrefix(errStr, "secret key ") || strings.HasPrefix(errStr, "secret value is empty") {
		Error(c, http.StatusBadGateway, errStr)
		return
	}
	if strings.HasPrefix(errStr, "missing required openclaw config dependency:") || strings.HasPrefix(errStr, "required openclaw config dependency is disabled:") {
		Error(c, http.StatusBadRequest, errStr)
		return
	}

	switch errStr {
	case "username already exists", "email already exists", "instance name already exists", "openclaw config resource key already exists":
		Error(c, http.StatusConflict, errStr)
	case "display name already exists":
		Error(c, http.StatusConflict, errStr)
	case "unsupported instance type", "image is required", "display name is required", "provider type is required", "base URL is required", "provider model name is required", "input price must be non-negative", "output price must be non-negative", "base URL is invalid", "automatic model discovery for azure-openai is not supported yet", "provider discovery is not supported", "model is required", "messages are required", "streaming is not supported yet", "provider type is not supported yet", "trace id is required", "event type is required", "message is required", "risk hit record is incomplete", "rule id is required", "rule display name is required", "rule pattern is required", "rule pattern is invalid", "risk severity is invalid", "risk action is invalid", "sample text is required", "secret ref format is invalid", "secret namespace is required in secret ref", "invalid openclaw resource type", "invalid openclaw config plan mode", "openclaw config resource name is required", "openclaw config resource key is invalid", "openclaw config schemaVersion is required", "openclaw config kind does not match resource type", "openclaw config format is required", "openclaw config content is required", "openclaw config content must be valid JSON", "openclaw config config payload is required", "openclaw config dependency type is invalid", "openclaw config dependency key is required", "openclaw config dependency is invalid", "openclaw config bundle name is required", "openclaw config bundle must include at least one resource", "openclaw config bundle resource id is required", "openclaw config bundle contains duplicate resources", "openclaw config bundle is required", "openclaw config bundle is disabled", "openclaw config bundle is empty", "at least one openclaw config resource must be selected", "openclaw config resource id is invalid", "openclaw config resource is disabled", "openclaw config bundle contains a disabled resource", "openclaw bootstrap payload is too large", "agent bootstrap token is required", "agent id is required", "unsupported agent protocol version", "invalid agent bootstrap token", "invalid instance command type", "invalid instance command finish status":
		Error(c, http.StatusBadRequest, errStr)
	case "model is not active or does not exist", "openclaw config resource not found", "openclaw config bundle not found", "openclaw injection snapshot not found", "instance command not found", "instance config revision not found":
		Error(c, http.StatusNotFound, errStr)
	case "risk rule not found":
		Error(c, http.StatusNotFound, errStr)
	case "sensitive content requires an active secure model", "request was blocked by risk policy":
		Error(c, http.StatusForbidden, errStr)
	case "invalid username or password", "account is disabled", "invalid or expired agent session token":
		Error(c, http.StatusUnauthorized, errStr)
	case "agent registration is only supported for openclaw instances", "agent registration is only supported for openclaw or hermes instances", "agent id does not match session", "access denied":
		Error(c, http.StatusForbidden, errStr)
	case "current password is incorrect":
		Error(c, http.StatusBadRequest, errStr)
	case "user not found", "model not found":
		Error(c, http.StatusNotFound, errStr)
	default:
		// For development, show actual error; for production, hide details
		Error(c, http.StatusInternalServerError, errStr)
	}
}

// ValidationError handles validation errors from gin binding
func ValidationError(c *gin.Context, err error) {
	var ve validator.ValidationErrors
	if errors.As(err, &ve) {
		Error(c, http.StatusBadRequest, formatValidationErrors(ve))
		return
	}
	Error(c, http.StatusBadRequest, err.Error())
}

func formatValidationErrors(errs validator.ValidationErrors) string {
	var messages []string
	for _, err := range errs {
		switch err.Tag() {
		case "required":
			messages = append(messages, err.Field()+" is required")
		case "min":
			messages = append(messages, err.Field()+" must be at least "+err.Param()+" characters")
		case "max":
			messages = append(messages, err.Field()+" must be at most "+err.Param()+" characters")
		case "email":
			messages = append(messages, err.Field()+" must be a valid email")
		case "alphanum":
			messages = append(messages, err.Field()+" must be alphanumeric")
		default:
			messages = append(messages, err.Field()+" is invalid")
		}
	}
	return messages[0]
}
