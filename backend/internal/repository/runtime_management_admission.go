package repository

import (
	"context"
	"errors"
	"github.com/upper/db/v4"
)

// Serialize both admission directions in short transactions, never over Agent IO.
func LockRuntimeManagementAdmission(ctx context.Context, tx db.Session) error {
	_, err := tx.SQL().ExecContext(ctx, "UPDATE runtime_management_admission_lock SET revision=revision+1 WHERE id=1")
	return err
}
func CheckRuntimeManagementRolloutAdmission(ctx context.Context, tx db.Session) error {
	if err := LockRuntimeManagementAdmission(ctx, tx); err != nil {
		return err
	}
	var active int
	row, err := tx.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM runtime_management_operations WHERE status IN ('pending','processing','starting','needs_attention')")
	if err != nil {
		return err
	}
	if err = row.Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return errors.New("Runtime scheduling operations must finish before starting a Runtime rollout")
	}
	return nil
}
