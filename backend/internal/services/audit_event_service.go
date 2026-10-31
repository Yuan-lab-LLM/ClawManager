package services

import (
	"context"
	"fmt"
	"strings"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

// AuditEventService defines application-level operations for governance audit events.
type AuditEventService interface {
	RecordEvent(event *models.AuditEvent) error
	ListEventsByTraceID(traceID string) ([]models.AuditEvent, error)
}

type auditEventService struct {
	repo repository.AuditEventRepository
}

// NewAuditEventService creates a new audit event service.
func NewAuditEventService(repo repository.AuditEventRepository) AuditEventService {
	return &auditEventService{repo: repo}
}

func (s *auditEventService) RecordEvent(event *models.AuditEvent) error {
	if err := prepareAuditEvent(event); err != nil {
		return err
	}
	return s.repo.Create(event)
}

// RecordEventContext never falls back to an unbounded repository write.
func (s *auditEventService) RecordEventContext(ctx context.Context, event *models.AuditEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := prepareAuditEvent(event); err != nil {
		return err
	}
	writer, ok := s.repo.(interface {
		CreateContext(context.Context, *models.AuditEvent) error
	})
	if !ok {
		return fmt.Errorf("audit repository does not support context-aware writes")
	}
	return writer.CreateContext(ctx, event)
}

func prepareAuditEvent(event *models.AuditEvent) error {
	if event == nil {
		return fmt.Errorf("audit event is required")
	}
	if strings.TrimSpace(event.TraceID) == "" {
		return fmt.Errorf("trace id is required")
	}
	if strings.TrimSpace(event.EventType) == "" {
		return fmt.Errorf("event type is required")
	}
	if strings.TrimSpace(event.TrafficClass) == "" {
		event.TrafficClass = models.TrafficClassLLM
	}
	if strings.TrimSpace(event.Severity) == "" {
		event.Severity = models.AuditSeverityInfo
	}
	if strings.TrimSpace(event.Message) == "" {
		return fmt.Errorf("message is required")
	}

	return nil
}

func (s *auditEventService) ListEventsByTraceID(traceID string) ([]models.AuditEvent, error) {
	items, err := s.repo.ListByTraceID(strings.TrimSpace(traceID))
	if err != nil {
		return nil, fmt.Errorf("failed to list audit events by trace: %w", err)
	}
	return items, nil
}
