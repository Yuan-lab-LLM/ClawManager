package repository

import (
	"context"
	"github.com/upper/db/v4"
)

// Serialize both admission directions in short transactions, never over Agent IO.
func LockRuntimeManagementAdmission(ctx context.Context, tx db.Session) error {
	_, err := tx.SQL().ExecContext(ctx, "UPDATE runtime_management_admission_lock SET revision=revision+1 WHERE id=1")
	return err
}
func CheckRuntimeManagementRolloutAdmission(ctx context.Context, tx db.Session, runtimeTypes ...string) error {
	if err := LockRuntimeManagementAdmission(ctx, tx); err != nil {
		return err
	}
	runtimeType := ""
	if len(runtimeTypes) > 0 {
		runtimeType = runtimeTypes[0]
	}
	if err := CheckRuntimeManagementOperationsForType(ctx, tx, runtimeType, ""); err != nil {
		return err
	}
	if err := checkRuntimePreparationClaim(ctx, tx, runtimeType); err != nil {
		return err
	}
	return validateRuntimeRolloutAdmission(ctx)
}
