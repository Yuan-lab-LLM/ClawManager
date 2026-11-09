package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// New pool replacement checkpoints use compare-and-swap. A delayed controller
// cannot overwrite resumed progress or revive an already terminal operation.
func (r *runtimeRolloutRepository) CompareAndSavePoolCheckpoint(ctx context.Context, id int64, previous, state string) error {
	var result sql.Result
	var err error
	if previous == "" {
		result, err = r.sess.SQL().ExecContext(ctx, `INSERT IGNORE INTO runtime_rollout_checkpoints(rollout_id,state_json,updated_at) SELECT id,?,? FROM runtime_rollouts WHERE id=? AND status IN ('pending','running')`, state, time.Now().UTC(), id)
	} else {
		result, err = r.sess.SQL().ExecContext(ctx, `UPDATE runtime_rollout_checkpoints c JOIN runtime_rollouts r ON r.id=c.rollout_id SET c.state_json=?,c.updated_at=? WHERE c.rollout_id=? AND BINARY c.state_json=BINARY ? AND r.status IN ('pending','running')`, state, time.Now().UTC(), id, previous)
	}
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("pool replacement ownership changed")
	}
	return nil
}

func (r *runtimeRolloutRepository) UpdatePoolReplacementStatus(ctx context.Context, id int64, checkpoint, status string, started, finished *time.Time, message *string) error {
	result, err := r.sess.SQL().ExecContext(ctx, `UPDATE runtime_rollouts r JOIN runtime_rollout_checkpoints c ON c.rollout_id=r.id SET r.status=?,r.started_at=?,r.finished_at=?,r.error_message=?,r.updated_at=? WHERE r.id=? AND r.status IN ('pending','running') AND BINARY c.state_json=BINARY ?`, status, started, finished, message, time.Now().UTC(), id, checkpoint)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("pool replacement status ownership changed")
	}
	return nil
}

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
