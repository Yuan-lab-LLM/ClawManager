package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/upper/db/v4"
)

// Protect database mutations from stale snapshots and out-of-band reporters.
// External K8s/Agent actions additionally need the lifecycle lock around the
// entire operation; a database check alone cannot fence those side effects.
func (r *instanceRepository) withUnfencedInstance(ctx context.Context, id int, write func(*instanceRepository) error) error {
	return r.withInstanceRowLock(ctx, id, func(tx *instanceRepository) error {
		if err := checkRuntimeUpgrade(ctx, tx.sess, id); err != nil {
			return err
		}
		return write(tx)
	})
}

// Terminal command acknowledgements may settle under a fence, but still need
// to serialize with reservation and cannot change the command's instance.
func (r *instanceRepository) withInstanceRowLock(ctx context.Context, id int, write func(*instanceRepository) error) error {
	return r.sess.TxContext(ctx, func(sess db.Session) error {
		row, err := sess.SQL().QueryRowContext(ctx, `SELECT id FROM instances WHERE id=? FOR UPDATE`, id)
		if err != nil {
			return err
		}
		var found int
		if err := row.Scan(&found); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		return write(&instanceRepository{sess})
	}, nil)
}
