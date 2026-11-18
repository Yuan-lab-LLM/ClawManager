package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// Optional extension keeps legacy repository implementations compatible.
type RuntimeProtocolRenewer interface {
	Renew(context.Context, int, int, models.RuntimeCapabilityReport) error
}

func (r *runtimeProtocolRepository) Renew(ctx context.Context, instanceID, ownerID int, report models.RuntimeCapabilityReport) error {
	if err := report.Validate(); err != nil {
		return err
	}
	if instanceID <= 0 || ownerID <= 0 {
		return models.ErrModelUnavailable
	}
	return r.sess.TxContext(ctx, func(sess db.Session) error {
		// Lock the current owner and generation before locking the report, in the
		// same order as enrollment. Restart/transfer/disable cannot race the write.
		row, err := sess.SQL().QueryRowContext(ctx, `SELECT i.runtime_generation FROM instances i JOIN users u ON u.id=i.user_id
 WHERE i.id=? AND i.user_id=? AND u.is_active=TRUE FOR UPDATE`, instanceID, ownerID)
		if err != nil {
			return err
		}
		var generation int
		if err := row.Scan(&generation); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return models.ErrModelUnavailable
			}
			return err
		}
		if generation != report.Generation {
			return models.ErrModelUnavailable
		}
		row, err = sess.SQL().QueryRowContext(ctx, `SELECT owner_user_id,generation,protocol,runtime_version,adapter_version,capabilities_json
 FROM gateway_runtime_protocols WHERE instance_id=? FOR UPDATE`, instanceID)
		if err != nil {
			return err
		}
		state := models.RuntimeProtocolState{InstanceID: instanceID}
		if err := row.Scan(&state.OwnerUserID, &state.Generation, &state.Protocol, &state.RuntimeVersion, &state.AdapterVersion, &state.CapabilitiesJSON); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return models.ErrRuntimeCapabilityRenewalConflict
			}
			return err
		}
		if !state.MatchesReport(ownerID, report) {
			return models.ErrRuntimeCapabilityRenewalConflict
		}
		// Deliberately no INSERT and no capability/version/generation updates.
		// Same-timestamp (zero changed rows) is still a successful renewal: the
		// existing matching row was read and locked above.
		_, err = sess.SQL().ExecContext(ctx, `UPDATE gateway_runtime_protocols SET reported_at=? WHERE instance_id=?`, time.Now().UTC(), instanceID)
		return err
	}, nil)
}
