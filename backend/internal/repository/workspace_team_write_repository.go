package repository

import (
	"clawreef/internal/models"
	"context"
	"github.com/upper/db/v4"
	"time"
)

func (r *workspaceWriteRepository) beginTeam(ctx context.Context, w models.WorkspaceWriteIntent) error {
	if w.Action == "erase" || w.Generation != 0 {
		return models.ErrRuntimeUpgradeConflict
	}
	return r.sess.TxContext(ctx, func(tx db.Session) error {
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT user_id,status FROM teams WHERE id=? FOR UPDATE`, w.TeamID)
		if err != nil {
			return err
		}
		var owner int
		var status string
		if err = row.Scan(&owner, &status); err != nil {
			return err
		}
		if owner != w.OwnerID || status == "deleting" || status == "deleted" {
			return models.ErrRuntimeUpgradeConflict
		}
		row, err = tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM team_workspace_write_intents WHERE team_id=?`, w.TeamID)
		if err != nil {
			return err
		}
		var count int
		if err = row.Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return models.ErrRuntimeUpgradeWritersActive
		}
		_, err = tx.SQL().ExecContext(ctx, `INSERT INTO team_workspace_write_intents(operation_id,team_id,owner_user_id,action,created_at) VALUES(?,?,?,?,?)`, w.Operation, w.TeamID, w.OwnerID, w.Action, time.Now().UTC())
		return err
	}, nil)
}
func (r *workspaceWriteRepository) completeTeam(ctx context.Context, w models.WorkspaceWriteIntent) error {
	result, err := r.sess.SQL().ExecContext(ctx, `DELETE FROM team_workspace_write_intents WHERE operation_id=? AND team_id=? AND owner_user_id=? AND action=?`, w.Operation, w.TeamID, w.OwnerID, w.Action)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return models.ErrRuntimeUpgradeConflict
	}
	return nil
}
