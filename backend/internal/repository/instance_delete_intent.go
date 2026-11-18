package repository

import (
	"context"
	"fmt"
	"time"
)

// MarkDeleting accepts terminal user intent without waiting for an in-flight
// native stop's named lock. Physical cleanup still acquires that lock and needs
// matching termination proof before releasing a binding or deleting the row.
func (r *instanceRepository) MarkDeleting(ctx context.Context, id, userID, generation int) error {
	return r.withUnfencedInstance(ctx, id, func(tx *instanceRepository) error {
		row, err := tx.sess.SQL().QueryRowContext(ctx, `SELECT user_id,runtime_generation FROM instances WHERE id=?`, id)
		if err != nil {
			return err
		}
		var owner, current int
		if err = row.Scan(&owner, &current); err != nil {
			return err
		}
		if owner != userID || current != generation {
			return fmt.Errorf("instance deletion identity changed")
		}
		if err = revokeStoppedInstanceAuthority(ctx, tx.sess, id); err != nil {
			return err
		}
		_, err = tx.sess.SQL().ExecContext(ctx, `UPDATE instances SET status='deleting',updated_at=? WHERE id=? AND user_id=? AND runtime_generation=?`, time.Now().UTC(), id, userID, generation)
		return err
	})
}
