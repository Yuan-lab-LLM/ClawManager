package utils

import (
	"clawreef/internal/models"
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
	if errors.Is(err, models.ErrChatNotSupported) {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "model_operation_not_supported", "type": "invalid_request_error", "message": err.Error()}})
		return
	}
	if errors.Is(err, models.ErrInvalidModelOperations) {
		Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, models.ErrVisionNotSupported) || errors.Is(err, models.ErrInvalidImageInput) || errors.Is(err, models.ErrNoVisionModel) {
		code := "invalid_image_input"
		if errors.Is(err, models.ErrVisionNotSupported) {
			code = "vision_not_supported"
		}
		if errors.Is(err, models.ErrNoVisionModel) {
			code = "vision_model_unavailable"
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": code, "type": "invalid_request_error", "message": err.Error()}})
		return
	}
	if errors.Is(err, models.ErrRuntimeUpgradeInProgress) || errors.Is(err, models.ErrRuntimeUpgradeConflict) || errors.Is(err, models.ErrRuntimeUpgradeWritersActive) {
		Error(c, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, models.ErrInstanceOperationInProgress) {
		Error(c, http.StatusConflict, models.ErrInstanceOperationInProgress.Error())
		return
	}
	if errors.Is(err, models.ErrModelUnavailable) {
		c.Header("Content-Type", "application/json")
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "model_unavailable", "type": "permission_error", "message": models.ErrModelUnavailable.Error()}})
		return
	}
	if errors.Is(err, models.ErrModelAccessConflict) {
		Error(c, http.StatusConflict, models.ErrModelAccessConflict.Error())
		return
	}
	if errors.Is(err, models.ErrInvalidModelAccess) {
		Error(c, http.StatusBadRequest, models.ErrInvalidModelAccess.Error())
		return
	}
	// Log the actual error for debugging
	log.Printf("[ERROR] %v", err)
	var coded interface {
		ErrorCode() string
		HTTPStatus() int
	}
	if errors.As(err, &coded) {
		c.JSON(coded.HTTPStatus(), gin.H{"success": false, "error": err.Error(), "error_code": coded.ErrorCode()})
		return
	}

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
	if strings.HasPrefix(errStr, "duplicate team member id:") || strings.HasPrefix(errStr, "redis url is invalid:") || strings.HasPrefix(errStr, "redis db index is invalid:") || strings.HasPrefix(errStr, "instance limit reached:") || strings.HasPrefix(errStr, "CPU cores exceed quota:") || strings.HasPrefix(errStr, "memory exceed quota:") || strings.HasPrefix(errStr, "storage exceed quota:") || strings.HasPrefix(errStr, "GPU count exceed quota:") {
		Error(c, http.StatusBadRequest, errStr)
		return
	}
	if errStr == "LDAP users must be imported from LDAP" || errStr == "context window tokens must be non-negative" || errStr == "context window tokens exceeds the supported maximum" ||
		errStr == "max concurrent requests must be non-negative" || errStr == "deployment priority must be non-negative" ||
		errStr == "model group must be at most 100 characters" {
		Error(c, http.StatusBadRequest, errStr)
		return
	}

	switch errStr {
	case "invalid generation limits or request body", "output token limit must be between 1 and 2147483647", "conflicting output token limits", "conflicting completion counts", "invalid completion count", "provider protocol or completion count is not supported", "quality tier must be between 0 (unclassified) and 4 (reasoning)":
		Error(c, http.StatusBadRequest, errStr)
	case "no eligible model for automatic routing":
		Error(c, http.StatusServiceUnavailable, errStr)
	case "gateway deployment is unavailable":
		c.Header("Retry-After", "1")
		Error(c, http.StatusServiceUnavailable, errStr)
	case "gateway budget only supports USD models", "gateway budget currency requires administrator review and explicit usage reset":
		Error(c, http.StatusConflict, errStr)
	case "gateway key budget exceeded":
		Error(c, http.StatusPaymentRequired, errStr)
	case "gateway key budget limit is invalid", "gateway key budget period is invalid", "gateway key limits must be non-negative", "gateway key limits are invalid", "gateway key name is required and must be at most 160 characters", "gateway key owner is required", "gateway key type is not supported", "gateway key expiration must be in the future":
		Error(c, http.StatusBadRequest, errStr)
	case "gateway key not found":
		Error(c, http.StatusNotFound, errStr)
	case "managed instance gateway keys cannot be revoked manually", "managed instance gateway keys require coordinated rotation", "only active gateway keys can be rotated", "gateway key is no longer active":
		Error(c, http.StatusConflict, errStr)
	case "username already exists", "user already exists", "email already exists", "instance name already exists", "team name already exists", "openclaw config resource key already exists", "team task message id already exists":
		Error(c, http.StatusConflict, errStr)
	case "display name already exists":
		Error(c, http.StatusConflict, errStr)
	case "unsupported instance type", "instance name is required", "image is required", "display name is required", "provider type is required", "base URL is required", "provider model name is required", "input price must be non-negative", "output price must be non-negative", "base URL is invalid", "automatic model discovery for azure-openai is not supported yet", "provider discovery is not supported", "model is required", "messages are required", "streaming is not supported yet", "provider type is not supported yet", "trace id is required", "event type is required", "message is required", "risk hit record is incomplete", "rule id is required", "rule display name is required", "rule pattern is required", "rule pattern is invalid", "risk severity is invalid", "risk action is invalid", "sample text is required", "secret ref format is invalid", "secret namespace is required in secret ref", "invalid openclaw resource type", "invalid openclaw config plan mode", "openclaw config resource name is required", "openclaw config resource key is invalid", "openclaw config schemaVersion is required", "openclaw config kind does not match resource type", "openclaw config format is required", "openclaw config content is required", "openclaw config content must be valid JSON", "openclaw config config payload is required", "openclaw config dependency type is invalid", "openclaw config dependency key is required", "openclaw config dependency is invalid", "openclaw config bundle name is required", "openclaw config bundle must include at least one resource", "openclaw config bundle resource id is required", "openclaw config bundle contains duplicate resources", "openclaw config bundle is required", "openclaw config bundle is disabled", "openclaw config bundle is empty", "at least one openclaw config resource must be selected", "openclaw config resource id is invalid", "openclaw config resource is disabled", "openclaw config bundle contains a disabled resource", "openclaw bootstrap payload is too large", "agent bootstrap token is required", "agent id is required", "unsupported agent protocol version", "invalid agent bootstrap token", "invalid instance command type", "invalid instance command finish status", "team name is required", "team must include at least one member", "team must include exactly one leader", "team redis url is required", "redis url scheme must be redis or rediss", "team member id is invalid", "target member id is required", "task payload is required", "team leader cannot be deleted before assigning a new leader", "invalid egress private exception scope type", "invalid egress private exception cidr", "egress private exception cidr must be private or link-local", "invalid egress private exception port":
		Error(c, http.StatusBadRequest, errStr)
	case "model is not active or does not exist", "openclaw config resource not found", "openclaw config bundle not found", "openclaw injection snapshot not found", "instance command not found", "instance config revision not found", "team not found", "team member not found":
		Error(c, http.StatusNotFound, errStr)
	case "risk rule not found", "egress private exception not found", "instance not found":
		Error(c, http.StatusNotFound, errStr)
	case "sensitive content requires an active secure model", "request was blocked by risk policy":
		Error(c, http.StatusForbidden, errStr)
	case "invalid username or password", "account is disabled", "invalid or expired agent session token":
		Error(c, http.StatusUnauthorized, errStr)
	case "agent registration is only supported for openclaw instances", "agent registration is only supported for openclaw or hermes instances", "agent registration is only supported for managed runtime instances", "agent id does not match session", "access denied", "skill_attach_forbidden":
		Error(c, http.StatusForbidden, errStr)
	case "current password is incorrect", "enterprise users must change password in the enterprise identity platform":
		Error(c, http.StatusBadRequest, errStr)
	case "user not found", "model not found", "skill not found", "skill hub tag not found":
		Error(c, http.StatusNotFound, errStr)
	case "skill_not_scanned", "skill_risk_blocked", "skill_tags_required", "skill is not published to hub":
		Error(c, http.StatusBadRequest, errStr)
	case "skill_package_pending", "skill_package_materialize_failed", "skill_package_materializing":
		Error(c, http.StatusConflict, err.Error())
	default:
		// For development, show actual error; for production, hide details
		Error(c, http.StatusInternalServerError, errStr)
	}
}

// ValidationError handles validation errors from gin binding
func ValidationError(c *gin.Context, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		Error(c, http.StatusRequestEntityTooLarge, "Gateway request body exceeds the configured limit")
		return
	}
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
