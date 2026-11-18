package repository

import (
	"context"
	"fmt"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// CheckRuntimeOrphanLifecycleGuard protects both lifecycle and workspace API
// boundaries. The orphan worker itself does not use either public mutation API.
func CheckRuntimeOrphanLifecycleGuard(ctx context.Context, sess db.Session, id int) error {
	row, err := sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_lifecycle_manual_guards g JOIN runtime_management_operations o ON CAST(o.id AS BINARY)=CAST(g.token AS BINARY) WHERE g.instance_id=? AND o.kind='orphan_recovery' AND o.status IN ('pending','processing','starting','needs_attention')`, id)
	if err != nil {
		return fmt.Errorf("read orphan recovery lifecycle reservation: %w", err)
	}
	var reservations int
	if err = row.Scan(&reservations); err != nil {
		return fmt.Errorf("read orphan recovery lifecycle reservation: %w", err)
	}
	if reservations > 0 {
		return models.ErrInstanceOperationInProgress
	}
	return nil
}
