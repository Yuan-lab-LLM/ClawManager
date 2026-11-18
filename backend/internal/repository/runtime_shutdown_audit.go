package repository

import (
	"clawreef/internal/models"
	"context"
	"github.com/upper/db/v4"
	"time"
)

type RuntimeShutdownAuditRepository interface {
	Record(context.Context, int, int, string, models.RuntimeShutdownObservation) error
	List(context.Context, int, int, int64) ([]models.RuntimeShutdownAudit, error)
}
type RuntimeShutdownDeliveryRecorder interface {
	RecordDelivery(context.Context, string, models.RuntimeShutdownDelivery) error
}
type runtimeShutdownAuditRepository struct{ sess db.Session }

func NewRuntimeShutdownAuditRepository(sess db.Session) RuntimeShutdownAuditRepository {
	return &runtimeShutdownAuditRepository{sess: sess}
}
func (r *runtimeShutdownAuditRepository) Record(ctx context.Context, instanceID, ownerID int, source string, observation models.RuntimeShutdownObservation) error {
	return r.record(ctx, instanceID, ownerID, source, observation, false)
}
func (r *runtimeShutdownAuditRepository) RecordDelivery(ctx context.Context, source string, delivery models.RuntimeShutdownDelivery) error {
	if err := delivery.Validate(); err != nil {
		return err
	}
	e := delivery.Event
	return r.record(ctx, e.InstanceID, e.OwnerUserID, source, e.Observation(), true)
}
func (r *runtimeShutdownAuditRepository) record(ctx context.Context, instanceID, ownerID int, source string, observation models.RuntimeShutdownObservation, historical bool) error {
	if err := observation.Validate(); err != nil {
		return err
	}
	if instanceID <= 0 || ownerID <= 0 || (source != "pro_agent" && source != "lite_agent") {
		return models.ErrInvalidShutdownObservation
	}
	return r.sess.TxContext(ctx, func(tx db.Session) error {
		// Serialize with owner/generation changes. Do not attribute a delayed
		// observation to a new owner, restarted process or a non-OpenClaw runtime.
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT i.runtime_generation FROM instances i JOIN users u ON u.id=i.user_id
 WHERE i.id=? AND i.user_id=? AND u.is_active=TRUE AND i.type IN ('openclaw','openclaw-shell') FOR UPDATE`, instanceID, ownerID)
		if err != nil {
			return err
		}
		var generation int
		if row.Scan(&generation) != nil || generation < observation.Generation || (!historical && generation != observation.Generation) {
			return models.ErrModelUnavailable
		}
		_, err = tx.SQL().ExecContext(ctx, `INSERT INTO runtime_shutdown_audits
 (instance_id,owner_user_id,generation,claim_id,outcome,source,received_at) VALUES (?,?,?,?,?,?,?)
 ON DUPLICATE KEY UPDATE id=id`, instanceID, ownerID, observation.Generation, observation.ClaimID, observation.Outcome, source, time.Now().UTC())
		return err
	}, nil)
}
func (r *runtimeShutdownAuditRepository) List(ctx context.Context, instanceID, ownerID int, beforeID int64) ([]models.RuntimeShutdownAudit, error) {
	if instanceID <= 0 || ownerID <= 0 || beforeID < 0 {
		return nil, models.ErrInvalidShutdownObservation
	}
	// The join repeats owner authorization at query time, avoiding disclosure
	// when ownership changes after the HTTP handler's initial access check.
	rows, err := r.sess.SQL().QueryContext(ctx, `SELECT a.id,a.instance_id,a.owner_user_id,a.generation,a.claim_id,a.outcome,a.source,a.received_at
 FROM runtime_shutdown_audits a JOIN instances i ON i.id=a.instance_id AND i.user_id=a.owner_user_id
 WHERE a.instance_id=? AND a.owner_user_id=? AND (?=0 OR a.id<?) ORDER BY a.id DESC LIMIT 50`, instanceID, ownerID, beforeID, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []models.RuntimeShutdownAudit{}
	for rows.Next() {
		var item models.RuntimeShutdownAudit
		if err := rows.Scan(&item.ID, &item.InstanceID, &item.OwnerUserID, &item.Generation, &item.ClaimID, &item.Outcome, &item.Source, &item.ReceivedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
