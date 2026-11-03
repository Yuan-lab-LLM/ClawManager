package repository

import (
	"context"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// Called with the instance row locked. Command creation/dispatch also locks
// that row, closing the gap between an empty queue check and the durable fence.
// A timeout/status label is not proof an external command process has exited.
func requireNoPlatformWriters(ctx context.Context, sess db.Session, id int) error {
	row, err := sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_commands WHERE instance_id=? AND status NOT IN ('succeeded','failed')`, id)
	if err != nil {
		return err
	}
	var count int
	if err := row.Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return models.ErrRuntimeUpgradeWritersActive
	}
	// Outstanding intents include management-process crashes and uncertain remote
	// exec outcomes. Never filter by generation or age to make an upgrade pass.
	row, err = sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_workspace_write_intents WHERE instance_id=?`, id)
	if err != nil {
		return err
	}
	if err := row.Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return models.ErrRuntimeUpgradeWritersActive
	}
	return nil
}
