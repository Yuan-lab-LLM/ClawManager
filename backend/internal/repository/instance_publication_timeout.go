package repository

import (
	"context"
	"time"
)

// MarkRuntimePublicationTimeout never overwrites a readiness report or newer
// identity after the caller's SQL snapshot. Timeout retains all writer/slot
// ownership and only records an error on the exact expired creating G1 row.
func (r *instanceRepository) MarkRuntimePublicationTimeout(ctx context.Context, id, generation int, origin time.Time, message string) (bool, error) {
	changed := false
	if generation != 1 || origin.IsZero() {
		return false, nil
	}
	err := r.withUnfencedInstance(ctx, id, func(tx *instanceRepository) error {
		if err := checkAutoStopRuntimeState(ctx, tx.sess, id, "error", generation); err != nil {
			return err
		}
		result, err := tx.sess.SQL().ExecContext(ctx, `UPDATE instances SET status='error',runtime_error_code='RUNTIME_PUBLICATION_TIMEOUT',runtime_error_message=?,updated_at=? WHERE id=? AND runtime_generation=? AND status='creating' AND instance_mode='lite' AND runtime_security_profile='isolated-network-v1' AND created_at=? AND created_at<=?`, message, time.Now().UTC(), id, generation, origin.UTC(), time.Now().UTC().Add(-5*time.Minute))
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		changed = count == 1
		return err
	})
	return changed, err
}
