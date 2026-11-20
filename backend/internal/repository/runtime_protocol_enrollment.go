package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// Optional: existing runtimes retain their old explicit negotiation contract.
// New lifecycle clients must use this compare-and-set entry, never fall back to
// unconditional negotiation on conflict or on an older server.
type RuntimeProtocolConditionalEnroller interface {
	Enroll(context.Context, int, int, models.RuntimeCapabilityReport, *string) error
}

func (r *runtimeProtocolRepository) Enroll(ctx context.Context, instanceID, ownerID int, report models.RuntimeCapabilityReport, expected *string) error {
	if err := report.Validate(); err != nil {
		return err
	}
	if err := models.ValidateEnrollmentRevision(expected); err != nil {
		return err
	}
	if instanceID <= 0 || ownerID <= 0 {
		return models.ErrModelUnavailable
	}
	// Snapshot the caller-owned precondition before waiting on a transaction.
	wantRevision := ""
	if expected != nil {
		wantRevision = *expected
	}
	return r.sess.TxContext(ctx, func(sess db.Session) error {
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
		err = row.Scan(&state.OwnerUserID, &state.Generation, &state.Protocol, &state.RuntimeVersion, &state.AdapterVersion, &state.CapabilitiesJSON)
		missing := errors.Is(err, sql.ErrNoRows)
		if err != nil && !missing {
			return err
		}
		if missing {
			if wantRevision != "" {
				return models.ErrRuntimeCapabilityEnrollmentConflict
			}
		} else if state.MatchesReport(ownerID, report) {
			// Lost-ack retry: accept the identical contract without rewriting it or
			// refreshing liveness. The caller must start the separate renewal loop.
			return nil
		} else {
			revision, err := state.EnrollmentRevision()
			// No same-generation contract replacement, ownership takeover,
			// downgrade, repair of corrupt state, or guessed stale precondition.
			if err != nil || wantRevision == "" || revision != wantRevision || state.OwnerUserID != ownerID || state.Generation >= report.Generation {
				return models.ErrRuntimeCapabilityEnrollmentConflict
			}
		}
		raw, err := json.Marshal(report.Capabilities)
		if err != nil {
			return err
		}
		if missing {
			_, err = sess.SQL().ExecContext(ctx, `INSERT INTO gateway_runtime_protocols
 (instance_id,owner_user_id,generation,protocol,runtime_version,adapter_version,capabilities_json,reported_at) VALUES (?,?,?,?,?,?,?,?)`,
				instanceID, ownerID, report.Generation, report.Protocol, report.RuntimeVersion, report.AdapterVersion, string(raw), time.Now().UTC())
		} else {
			_, err = sess.SQL().ExecContext(ctx, `UPDATE gateway_runtime_protocols SET generation=?,protocol=?,runtime_version=?,adapter_version=?,capabilities_json=?,reported_at=? WHERE instance_id=?`,
				report.Generation, report.Protocol, report.RuntimeVersion, report.AdapterVersion, string(raw), time.Now().UTC(), instanceID)
		}
		return err
	}, nil)
}
