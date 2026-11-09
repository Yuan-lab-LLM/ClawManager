package northbound

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"clawreef/internal/models"
	"github.com/gin-gonic/gin"
)

func requireStrongSecret(name, value string) error {
	if len(value) < 32 {
		return fmt.Errorf("%s must contain at least 32 bytes", name)
	}
	return nil
}

const (
	ScopeOwnerUsageRead  = "owners:usage:read"
	ScopeLiteCreate      = "lite-instances:create"
	ScopeLiteRead        = "lite-instances:read"
	ScopeProCreate       = "pro-instances:create"
	ScopeProRead         = "pro-instances:read"
	ScopeLiteRestart     = "lite-instances:restart"
	ScopeLiteReset       = "lite-instances:reset"
	ScopeLiteDelete      = "lite-instances:delete"
	ScopeProRestart      = "pro-instances:restart"
	ScopeProReset        = "pro-instances:reset"
	ScopeProDelete       = "pro-instances:delete"
	ScopeShareLinkManage = "lite-instances:share-link:manage"
	ScopeShareLinkReset  = "lite-instances:share-link:reset"

	OperationTypeLiteInstance = "lite_instance"
	OperationTypeProInstance  = "pro_instance"
	OperationTypeLiteRestart  = "lite_instance_restart"
	OperationTypeLiteReset    = "lite_instance_reset"
	OperationTypeLiteDelete   = "lite_instance_delete"
	OperationTypeProRestart   = "pro_instance_restart"
	OperationTypeProReset     = "pro_instance_reset"
	OperationTypeProDelete    = "pro_instance_delete"
)

type APIError struct {
	Status     int
	Code       string
	Message    string
	Cause      error
	AuditEvent string
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return e.Code + ": " + e.Cause.Error()
	}
	return e.Code + ": " + e.Message
}

func apiError(status int, code, message string, cause error) *APIError {
	return &APIError{Status: status, Code: code, Message: message, Cause: cause}
}

func writeError(c *gin.Context, err error) {
	var target *APIError
	if !errors.As(err, &target) {
		target = apiError(http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "A required dependency is temporarily unavailable", err)
	}
	c.Set("northboundErrorCode", target.Code)
	if target.AuditEvent != "" {
		c.Set("northboundAuditEvent", target.AuditEvent)
	}
	requestID, _ := c.Get("requestID")
	c.JSON(target.Status, gin.H{
		"code":       target.Code,
		"message":    target.Message,
		"request_id": requestID,
	})
}

type TokenResponse struct {
	UserID           int      `json:"-"`
	AccessToken      string   `json:"access_token"`
	RefreshToken     string   `json:"refresh_token"`
	TokenType        string   `json:"token_type"`
	ExpiresIn        int64    `json:"expires_in"`
	RefreshExpiresIn int64    `json:"refresh_expires_in"`
	Scopes           []string `json:"scopes"`
	SessionID        string   `json:"session_id"`
}

type Principal struct {
	RequestScope string
	AuthType     string
	KeyID        string
	UserID       int
	SessionID    string
	Scopes       []string
}

func (p Principal) HasScope(required string) bool {
	for _, scope := range p.Scopes {
		if scope == required {
			return true
		}
	}
	return false
}

type CreateLiteInstanceRequest struct {
	InstanceMode string  `json:"instance_mode,omitempty"`
	Name         string  `json:"name" binding:"required,min=3,max=50"`
	Alias        *string `json:"alias,omitempty"`
	Owner        string  `json:"owner" binding:"required,min=1,max=128"`
	Type         string  `json:"type" binding:"required,oneof=openclaw hermes opencode deepseek-harness"`
	Description  *string `json:"description,omitempty"`
}

// CreateProInstanceRequest intentionally mirrors the existing Lite request.
// The endpoint fixes Pro/desktop mode and the server owns image and resource
// selection, so callers cannot mix Lite and Pro provisioning parameters.
type CreateProInstanceRequest struct {
	InstanceMode string  `json:"instance_mode,omitempty"`
	Name         string  `json:"name" binding:"required,min=3,max=50"`
	Alias        *string `json:"alias,omitempty"`
	Owner        string  `json:"owner" binding:"required,min=1,max=128"`
	Type         string  `json:"type" binding:"required,oneof=openclaw hermes opencode deepseek-harness"`
	Description  *string `json:"description,omitempty"`
}

type InstanceLifecycleRequest struct {
	PreviousOperationID string `json:"-"`
	InstanceID          int    `json:"instance_id"`
}

// ConfirmInstanceResetRequest makes the destructive reset contract explicit.
// Older clients that submit an empty body must fail closed instead of silently
// changing from the former data-preserving reset behavior.
type ConfirmInstanceResetRequest struct {
	ConfirmDataLoss bool `json:"confirm_data_loss"`
}

// ConfirmInstanceDeleteRequest makes permanent deletion opt-in even when the
// caller already has a destructive delete scope.
type ConfirmInstanceDeleteRequest struct {
	ConfirmDataLoss bool `json:"confirm_data_loss"`
}

type EnableShareLinkPasswordRequest struct {
	ExpiresMode     string     `json:"expires_mode,omitempty" binding:"omitempty,oneof=preset custom permanent"`
	ExpiresPreset   string     `json:"expires_preset,omitempty" binding:"omitempty,oneof=1h 24h 7d 30d"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	WorkspaceAccess string     `json:"workspace_access,omitempty" binding:"omitempty,oneof=none read write"`
}

type OperationResponse struct {
	AttemptCount int        `json:"attempt_count"`
	TeamID       *int       `json:"team_id,omitempty"`
	TemplateID   *int       `json:"template_id,omitempty"`
	OperationID  string     `json:"operation_id"`
	Status       string     `json:"status"`
	ResourceType string     `json:"resource_type"`
	InstanceID   *int       `json:"instance_id,omitempty"`
	ErrorCode    *string    `json:"error_code,omitempty"`
	ErrorMessage *string    `json:"error_message,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func operationResponse(item *models.NorthboundOperation) OperationResponse {
	return OperationResponse{
		AttemptCount: item.AttemptCount,
		TeamID:       item.TeamID,
		TemplateID:   item.TemplateID,
		OperationID:  item.OperationID,
		Status:       item.Status,
		ResourceType: item.OperationType,
		InstanceID:   item.InstanceID,
		ErrorCode:    item.ErrorCode,
		ErrorMessage: item.ErrorMessage,
		CreatedAt:    item.CreatedAt,
		StartedAt:    item.StartedAt,
		FinishedAt:   item.FinishedAt,
		UpdatedAt:    item.UpdatedAt,
	}
}

type LiteInstanceResponse struct {
	ID             int        `json:"id"`
	Name           string     `json:"name"`
	Alias          *string    `json:"alias,omitempty"`
	Owner          string     `json:"owner"`
	Description    *string    `json:"description,omitempty"`
	Type           string     `json:"type"`
	InstanceMode   string     `json:"instance_mode"`
	RuntimeType    string     `json:"runtime_type"`
	RuntimeVariant string     `json:"runtime_variant,omitempty"`
	Status         string     `json:"status"`
	Availability   string     `json:"availability,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
}

type ShareLinkResetResponse struct {
	InstanceID      int        `json:"instance_id"`
	AuthMode        string     `json:"auth_mode"`
	ShareURL        string     `json:"share_url"`
	Password        string     `json:"password,omitempty"`
	WorkspaceAccess string     `json:"workspace_access"`
	ExpiresAt       *time.Time `json:"expires_at,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func liteInstanceResponse(item *models.Instance) LiteInstanceResponse {
	availability := "unavailable"
	switch item.Status {
	case "running":
		availability = "available"
	case "creating":
		availability = "starting"
	}
	return LiteInstanceResponse{
		ID:             item.ID,
		Name:           item.Name,
		Alias:          item.Alias,
		Owner:          instanceOwner(item),
		Description:    item.Description,
		Type:           item.Type,
		InstanceMode:   item.InstanceMode,
		RuntimeType:    item.RuntimeType,
		RuntimeVariant: item.RuntimeVariant,
		Status:         item.Status,
		Availability:   availability,
		CreatedAt:      item.CreatedAt,
		UpdatedAt:      item.UpdatedAt,
		StartedAt:      item.StartedAt,
	}
}

func normalizeInstanceAlias(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	alias := strings.TrimSpace(*value)
	if alias == "" {
		return nil, nil
	}
	if !utf8.ValidString(alias) || utf8.RuneCountInString(alias) > 50 {
		return nil, errors.New("alias must contain at most 50 characters")
	}
	for _, character := range alias {
		if unicode.IsControl(character) {
			return nil, errors.New("alias must not contain control characters")
		}
	}
	return &alias, nil
}

func instanceOwner(item *models.Instance) string {
	if item == nil || item.Owner == nil {
		return ""
	}
	return *item.Owner
}
