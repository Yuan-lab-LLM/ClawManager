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

	switch errStr {
	case "username already exists", "email already exists", "instance name already exists":
		Error(c, http.StatusConflict, errStr)
	case "display name already exists":
		Error(c, http.StatusConflict, errStr)
	case "unsupported instance type", "image is required", "display name is required", "provider type is required", "base URL is required", "provider model name is required", "input price must be non-negative", "output price must be non-negative", "base URL is invalid", "automatic model discovery for azure-openai is not supported yet", "provider discovery is not supported", "model is required", "messages are required", "streaming is not supported yet", "provider type is not supported yet", "trace id is required", "event type is required", "message is required", "risk hit record is incomplete", "rule id is required", "rule display name is required", "rule pattern is required", "rule pattern is invalid", "risk severity is invalid", "risk action is invalid", "sample text is required", "secret ref format is invalid", "secret namespace is required in secret ref":
		Error(c, http.StatusBadRequest, errStr)
	case "model is not active or does not exist":
		Error(c, http.StatusNotFound, errStr)
	case "risk rule not found":
		Error(c, http.StatusNotFound, errStr)
	case "sensitive content requires an active secure model", "request was blocked by risk policy":
		Error(c, http.StatusForbidden, errStr)
	case "invalid username or password", "account is disabled":
		Error(c, http.StatusUnauthorized, errStr)
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
