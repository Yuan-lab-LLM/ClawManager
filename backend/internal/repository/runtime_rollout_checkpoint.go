package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (r *runtimeRolloutRepository) LoadRolloutCheckpoint(ctx context.Context, id int64) (string, error) {
	var state string
	row, err := r.sess.SQL().QueryRowContext(ctx, "SELECT state_json FROM runtime_rollout_checkpoints WHERE rollout_id = ?", id)
	if err != nil {
		return "", err
	}
	err = row.Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return state, err
}

func (r *runtimeRolloutRepository) SaveRolloutCheckpoint(ctx context.Context, id int64, state string) error {
	_, err := r.sess.SQL().ExecContext(ctx, `INSERT INTO runtime_rollout_checkpoints (rollout_id, state_json, updated_at) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE state_json = VALUES(state_json), updated_at = VALUES(updated_at)`, id, state, time.Now().UTC())
	return err
}
