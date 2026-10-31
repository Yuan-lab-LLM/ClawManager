package repository

import (
	"context"
	"encoding/json"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

type RuntimeProtocolRepository interface {
	Get(context.Context, int) (*models.RuntimeProtocolState, error)
	Negotiate(context.Context, int, int, models.RuntimeCapabilityReport) error
}

type runtimeProtocolRepository struct{ sess db.Session }

func NewRuntimeProtocolRepository(sess db.Session) RuntimeProtocolRepository {
	return &runtimeProtocolRepository{sess: sess}
}

func (r *runtimeProtocolRepository) Get(ctx context.Context, instanceID int) (*models.RuntimeProtocolState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var state models.RuntimeProtocolState
	err := r.sess.WithContext(ctx).Collection("gateway_runtime_protocols").Find(db.Cond{"instance_id": instanceID}).One(&state)
	if err == db.ErrNoMoreRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &state, nil
}

// Enrollment is sticky across process restarts. There is deliberately no
// runtime-facing downgrade API. Stale processes cannot enroll a new generation.
func (r *runtimeProtocolRepository) Negotiate(ctx context.Context, instanceID, ownerID int, report models.RuntimeCapabilityReport) error {
	if err := report.Validate(); err != nil {
		return err
	}
	if instanceID <= 0 || ownerID <= 0 {
		return models.ErrModelUnavailable
	}
	return r.sess.TxContext(ctx, func(sess db.Session) error {
		row, err := sess.SQL().QueryRowContext(ctx, `SELECT i.runtime_generation FROM instances i JOIN users u ON u.id=i.user_id
 WHERE i.id=? AND i.user_id=? AND u.is_active=TRUE FOR UPDATE`, instanceID, ownerID)
		if err != nil {
			return err
		}
		var generation int
		if err := row.Scan(&generation); err != nil {
			return models.ErrModelUnavailable
		}
		if generation != report.Generation {
			return models.ErrModelUnavailable
		}
		raw, _ := json.Marshal(report.Capabilities)
		_, err = sess.SQL().ExecContext(ctx, `INSERT INTO gateway_runtime_protocols
 (instance_id,owner_user_id,generation,protocol,runtime_version,adapter_version,capabilities_json,reported_at)
 VALUES (?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE owner_user_id=VALUES(owner_user_id),generation=VALUES(generation),
 protocol=GREATEST(protocol,VALUES(protocol)),runtime_version=VALUES(runtime_version),adapter_version=VALUES(adapter_version),
 capabilities_json=VALUES(capabilities_json),reported_at=VALUES(reported_at)`,
			instanceID, ownerID, generation, report.Protocol, report.RuntimeVersion, report.AdapterVersion, string(raw), time.Now().UTC())
		return err
	}, nil)
}
