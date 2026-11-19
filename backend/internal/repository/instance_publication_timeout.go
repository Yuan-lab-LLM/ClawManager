package repository

import (
	"context"
	"database/sql"
	"time"
)

func (r *instanceRepository) RuntimePublicationOrigin(ctx context.Context, id, generation int) (time.Time, error) {
	var origin sql.NullTime
	row, err := r.sess.SQL().QueryRowContext(ctx, `SELECT CASE WHEN state = 'running' THEN NULL ELSE created_at END
		FROM instance_runtime_bindings WHERE instance_id = ? AND generation = ?`, id, generation)
	if err != nil {
		return time.Time{}, err
	}
	err = row.Scan(&origin)
	return origin.Time, err
}

// MarkRuntimePublicationTimeout never overwrites a readiness report or newer
// identity after the caller's SQL snapshot. Timeout retains all writer/slot
// ownership and only records an error for the exact expired G1 reservation.
func (r *instanceRepository) MarkRuntimePublicationTimeout(ctx context.Context, id, generation int, origin time.Time, message string) (bool, error) {
	changed := false
	if generation != 1 || origin.IsZero() {
		return false, nil
	}
	err := r.withUnfencedInstance(ctx, id, func(tx *instanceRepository) error {
		if err := checkAutoStopRuntimeState(ctx, tx.sess, id, "error", generation); err != nil {
			return err
		}
		now := time.Now().UTC()
		result, err := tx.sess.SQL().ExecContext(ctx, `UPDATE instances AS i
			JOIN instance_runtime_bindings AS b ON b.instance_id = i.id AND b.generation = i.runtime_generation
			SET i.status = 'error', i.runtime_error_code = 'RUNTIME_PUBLICATION_TIMEOUT',
				i.runtime_error_message = ?, i.updated_at = ?
			WHERE i.id = ? AND i.runtime_generation = ? AND i.status = 'creating'
				AND i.instance_mode = 'lite' AND i.runtime_security_profile = 'isolated-network-v1'
				AND b.state = 'creating' AND b.created_at = ? AND b.created_at <= ?`,
			message, now, id, generation, origin.UTC(), now.Add(-5*time.Minute))
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		changed = count == 1
		return err
	})
	return changed, err
}
