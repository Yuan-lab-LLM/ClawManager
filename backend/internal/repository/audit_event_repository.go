package repository

import (
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
}

type auditEventRepository struct {
	sess db.Session
}

// NewAuditEventRepository creates a new audit event repository and ensures its table exists.
func NewAuditEventRepository(sess db.Session) AuditEventRepository {
	repo := &auditEventRepository{sess: sess}
	repo.ensureTable()
	return repo
}

func (r *auditEventRepository) ensureTable() {
	const query = `
CREATE TABLE IF NOT EXISTS audit_events (
  id INT AUTO_INCREMENT PRIMARY KEY,
  trace_id VARCHAR(100) NOT NULL,
  session_id VARCHAR(100) NULL,
  request_id VARCHAR(100) NULL,
  user_id INT NULL,
  instance_id INT NULL,
  invocation_id INT NULL,
  event_type VARCHAR(100) NOT NULL,
  traffic_class VARCHAR(50) NOT NULL,
  severity VARCHAR(20) NOT NULL,
  message VARCHAR(500) NOT NULL,
  details LONGTEXT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_audit_events_trace_id (trace_id),
  INDEX idx_audit_events_request_id (request_id),
  INDEX idx_audit_events_user_id (user_id),
  INDEX idx_audit_events_invocation_id (invocation_id),
  INDEX idx_audit_events_event_type (event_type),
  INDEX idx_audit_events_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
`

	if _, err := r.sess.SQL().Exec(query); err != nil {
		panic(fmt.Errorf("failed to ensure audit_events table: %w", err))
	}
}

func (r *auditEventRepository) Create(event *models.AuditEvent) error {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	res, err := r.sess.Collection("audit_events").Insert(event)
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
