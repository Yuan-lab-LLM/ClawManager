package repository

import (
	"context"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

type WorkspaceWriteRepository interface {
	Begin(context.Context, models.WorkspaceWriteIntent) error
	Complete(context.Context, models.WorkspaceWriteIntent) error
}

type workspaceWriteRepository struct{ sess db.Session }

func NewWorkspaceWriteRepository(sess db.Session) WorkspaceWriteRepository {
	return &workspaceWriteRepository{sess}
}

func (r *workspaceWriteRepository) Begin(ctx context.Context, w models.WorkspaceWriteIntent) error {
	if !w.Valid() {
		return models.ErrRuntimeUpgradeConflict
	}
	return (&instanceRepository{r.sess}).withUnfencedInstance(ctx, w.InstanceID, func(tx *instanceRepository) error {
		row, err := tx.sess.SQL().QueryRowContext(ctx, `SELECT user_id,runtime_generation,status FROM instances WHERE id=?`, w.InstanceID)
		if err != nil {
			return err
		}
		var owner, generation int
		var status string
		if err := row.Scan(&owner, &generation, &status); err != nil {
			return err
		}
		if owner != w.OwnerID || generation != w.Generation || status == "deleting" {
			return models.ErrRuntimeUpgradeConflict
		}
		// No upsert: a duplicate ID cannot overwrite another operation or authorize redispatch.
		_, err = tx.sess.SQL().ExecContext(ctx, `INSERT INTO instance_workspace_write_intents(operation_id,instance_id,owner_user_id,generation,action,created_at) VALUES(?,?,?,?,?,?)`, w.Operation, w.InstanceID, w.OwnerID, w.Generation, w.Action, time.Now().UTC())
		return err
	})
}

func (r *workspaceWriteRepository) Complete(ctx context.Context, w models.WorkspaceWriteIntent) error {
	if !w.Valid() {
		return models.ErrRuntimeUpgradeConflict
	}
	// May settle under a fence or after a generation change, but only for the exact
	// originally dispatched operation. No public manual-clear or expiry mechanism.
	return (&instanceRepository{r.sess}).withInstanceRowLock(ctx, w.InstanceID, func(tx *instanceRepository) error {
		result, err := tx.sess.SQL().ExecContext(ctx, `DELETE FROM instance_workspace_write_intents WHERE operation_id=? AND instance_id=? AND owner_user_id=? AND generation=? AND action=?`, w.Operation, w.InstanceID, w.OwnerID, w.Generation, w.Action)
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
	})
}
