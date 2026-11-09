package repository

import (
	"context"
	"fmt"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

// AuditEventRepository defines repository operations for governance audit events.
type AuditEventRepository interface {
	Create(event *models.AuditEvent) error
	ListByTraceID(traceID string) ([]models.AuditEvent, error)
	ListRecent(limit int) ([]models.AuditEvent, error)
	CountRecentByInstanceAndEventType(instanceID int, eventType string, since time.Time) (int, error)
}

type auditEventRepository struct {
	sess db.Session
}

// NewAuditEventRepository creates a new audit event repository (schema is owned by embedded migrations).
func NewAuditEventRepository(sess db.Session) AuditEventRepository {
	repo := &auditEventRepository{sess: sess}
	return repo
}

// NewAuditEventRepositoryExistingTable skips DDL and is intended for
// least-privilege processes after Core has applied migrations.
func NewAuditEventRepositoryExistingTable(sess db.Session) AuditEventRepository {
	return &auditEventRepository{sess: sess}
}

func (r *auditEventRepository) Create(event *models.AuditEvent) error {
	return r.CreateContext(context.Background(), event)
}

func (r *auditEventRepository) CreateContext(ctx context.Context, event *models.AuditEvent) error {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	res, err := r.sess.WithContext(ctx).Collection("audit_events").Insert(event)
	if err != nil {
		return fmt.Errorf("failed to create audit event: %w", err)
	}
	event.ID = int(res.ID().(int64))
	return nil
}

func (r *auditEventRepository) ListByTraceID(traceID string) ([]models.AuditEvent, error) {
	var items []models.AuditEvent
	if err := r.sess.Collection("audit_events").Find(db.Cond{"trace_id": traceID}).OrderBy("id").All(&items); err != nil {
		return nil, fmt.Errorf("failed to list audit events by trace: %w", err)
	}
	return items, nil
}

func (r *auditEventRepository) ListRecent(limit int) ([]models.AuditEvent, error) {
	var items []models.AuditEvent
	if limit <= 0 {
		limit = 100
	}
	if err := r.sess.Collection("audit_events").Find().OrderBy("-created_at").Limit(limit).All(&items); err != nil {
		return nil, fmt.Errorf("failed to list recent audit events: %w", err)
	}
	return items, nil
}

func (r *auditEventRepository) CountRecentByInstanceAndEventType(instanceID int, eventType string, since time.Time) (int, error) {
	row, err := r.sess.SQL().QueryRow(`
SELECT COUNT(*)
FROM audit_events
WHERE instance_id = ?
  AND event_type = ?
  AND created_at >= ?
`, instanceID, eventType, since)
	if err != nil {
		return 0, fmt.Errorf("failed to count audit events by instance and type: %w", err)
	}
	var count int
	if err := row.Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to scan audit event count: %w", err)
	}
	return count, nil
}
