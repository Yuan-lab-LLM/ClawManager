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
	if handled, err := persistObservation(ctx, r.sess, "audit_events", event); handled {
		return err
	}
	return r.sess.WithContext(ctx).Tx(func(tx db.Session) error {
		if event.TraceID != "" {
			if err := lockAuditTraces(ctx, tx, []string{event.TraceID}); err != nil {
				return err
			}
		}
		res, err := tx.Collection("audit_events").Insert(event)
		if err != nil {
			return fmt.Errorf("failed to create audit event: %w", err)
		}
		event.ID = int(res.ID().(int64))
		return maintainObservationSummary(ctx, tx, "audit_events", event)
	})
}

func (r *auditEventRepository) ListByTraceID(traceID string) ([]models.AuditEvent, error) {
	return routedLogDetails[models.AuditEvent](r.sess, "audit_events", traceID, "", 0, 200, false)
}

func (r *auditEventRepository) ListRecent(limit int) ([]models.AuditEvent, error) {
	return routedRecentLogs[models.AuditEvent](r.sess, "audit_events", 0, limit)
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
