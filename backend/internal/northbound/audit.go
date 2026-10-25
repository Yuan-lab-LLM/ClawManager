package northbound

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"github.com/gin-gonic/gin"
)

// AuditRequests records northbound access outcomes without persisting request
// bodies, credentials, tokens, JWE ciphertext, or raw idempotency keys.
func AuditRequests(audit repository.AuditEventRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now().UTC()
		c.Next()
		if audit == nil {
			return
		}
		requestID := requestIDFromContext(c)
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		result := "success"
		severity := models.AuditSeverityInfo
		if c.Writer.Status() >= 400 {
			result = "rejected"
			severity = models.AuditSeverityWarn
		}
		if c.Writer.Status() >= 500 {
			severity = models.AuditSeverityError
		}
		details, _ := json.Marshal(map[string]any{
			"auth_type":           contextString(c, "northboundAuthType"),
			"api_key_id":          contextString(c, "northboundKeyID"),
			"operation_id":        contextString(c, "northboundOperationID"),
			"route":               route,
			"method":              c.Request.Method,
			"status":              c.Writer.Status(),
			"result":              result,
			"source_ip":           c.ClientIP(),
			"duration_ms":         time.Since(started).Milliseconds(),
			"idempotent_replayed": c.Writer.Header().Get("Idempotent-Replayed") == "true",
			"error_code":          contextString(c, "northboundErrorCode"),
		})
		eventType := northboundEventType(c.Request.Method, route)
		if override := contextString(c, "northboundAuditEvent"); override != "" {
			eventType = override
		}
		event := &models.AuditEvent{
			TraceID:      requestID,
			EventType:    eventType,
			TrafficClass: "northbound",
			Severity:     severity,
			Message:      "Northbound request completed",
			CreatedAt:    time.Now().UTC(),
		}
		if requestID != "" {
			event.RequestID = &requestID
		}
		if principal := currentPrincipal(c); principal != nil {
			event.UserID = &principal.UserID
			event.SessionID = &principal.SessionID
		}
		encoded := string(details)
		event.Details = &encoded
		if err := audit.Create(event); err != nil {
			log.Printf("northbound audit write failed (request_id=%s): %v", requestID, err)
		}
	}
}

func contextString(c *gin.Context, key string) string {
	value, _ := c.Get(key)
	encoded, _ := value.(string)
	return encoded
}

func northboundEventType(method, route string) string {
	key := method + " " + route
	switch key {
	case "POST /api/northbound/v1/auth/challenge":
		return "northbound.auth.challenge"
	case "POST /api/northbound/v1/auth/login":
		return "northbound.auth.login"
	case "POST /api/northbound/v1/auth/refresh":
		return "northbound.auth.refresh"
	case "POST /api/northbound/v1/auth/logout":
		return "northbound.auth.logout"
	case "POST /api/northbound/v1/lite-instances":
		return "northbound.lite.create"
	case "POST /api/northbound/v1/pro-instances":
		return "northbound.pro.create"
	case "POST /api/northbound/v1/lite-instances/:id/restart":
		return "northbound.lite.restart"
	case "POST /api/northbound/v1/lite-instances/:id/reset":
		return "northbound.lite.reset"
	case "POST /api/northbound/v1/lite-instances/:id/delete":
		return "northbound.lite.delete"
	case "POST /api/northbound/v1/pro-instances/:id/restart":
		return "northbound.pro.restart"
	case "POST /api/northbound/v1/pro-instances/:id/reset":
		return "northbound.pro.reset"
	case "POST /api/northbound/v1/pro-instances/:id/delete":
		return "northbound.pro.delete"
	case "POST /api/northbound/v1/lite-instances/:id/external-access/password":
		return "northbound.share_link.password_enabled"
	case "POST /api/northbound/v1/lite-instances/:id/external-access/share-link/reset":
		return "northbound.share_link.url_reset"
	case "POST /api/northbound/v1/lite-instances/:id/external-access/password/reset":
		return "northbound.share_link.password_reset"
	default:
		return "northbound.request"
	}
}

func (s *CoreService) SetAuditRepository(audit repository.AuditEventRepository) {
	if s != nil {
		s.audit = audit
	}
}

func (s *CoreService) auditOperation(item *models.NorthboundOperation, eventType, severity, message string, instanceID *int, errorCode string) {
	if s == nil || s.audit == nil || item == nil {
		return
	}
	details, _ := json.Marshal(map[string]any{
		"operation_id":  item.OperationID,
		"api_key_id":    item.APIKeyID,
		"auth_scope":    item.AuthScope,
		"status":        item.Status,
		"attempt_count": item.AttemptCount,
		"error_code":    errorCode,
	})
	traceID := item.OperationID
	requestID := item.OperationID
	userID := item.UserID
	sessionID := item.SessionID
	event := &models.AuditEvent{
		TraceID:      traceID,
		SessionID:    &sessionID,
		RequestID:    &requestID,
		UserID:       &userID,
		InstanceID:   instanceID,
		InstanceMode: stringPointer(operationInstanceMode(item)),
		EventType:    eventType,
		TrafficClass: "northbound",
		Severity:     severity,
		Message:      message,
		Details:      stringPointer(string(details)),
		CreatedAt:    time.Now().UTC(),
	}
	if err := s.audit.Create(event); err != nil {
		log.Printf("northbound operation audit write failed (operation_id=%s): %v", item.OperationID, err)
	}
}

func operationInstanceMode(item *models.NorthboundOperation) string {
	if item == nil {
		return services.InstanceModeLite
	}
	if item.OperationType == OperationTypeProInstance || item.OperationType == OperationTypeProRestart || item.OperationType == OperationTypeProReset || item.OperationType == OperationTypeProDelete {
		return services.InstanceModePro
	}
	if item.OperationType == OperationTypeLiteInstance {
		var request CreateLiteInstanceRequest
		if json.Unmarshal([]byte(item.RequestPayload), &request) == nil && strings.EqualFold(strings.TrimSpace(request.Type), "workbuddy") {
			return services.InstanceModePro
		}
	}
	return services.InstanceModeLite
}

func stringPointer(value string) *string { return &value }

func auditOperationMessage(operationID, outcome string) string {
	return fmt.Sprintf("Northbound operation %s: %s", outcome, operationID)
}
