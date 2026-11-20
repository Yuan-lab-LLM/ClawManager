package repository

import (
	"context"
	"fmt"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

func checkTeamDeletionLifecycle(op *models.TeamLifecycleOperation) error {
	if op == nil {
		return nil
	}
	if op.FaultRecoveryOperationID != "" {
		return fmt.Errorf("团队正在核实成员退出，请等待原恢复操作完成后删除")
	}
	switch op.State {
	case "running", "stopped", "recovery_required":
		return nil
	default:
		return fmt.Errorf("请先完成或核实原团队启停操作后删除")
	}
}

// A failed member is a readiness projection, not an active lifecycle operation.
// Deletion still requires each member's independent, confirmed writer teardown.
func (r *TeamLifecycleRepository) CheckDeletion(ctx context.Context, id int) error {
	op, err := readTeamLifecycle(ctx, r.sess, id, false)
	if err != nil {
		return err
	}
	return checkTeamDeletionLifecycle(op)
}

// Lock in the same order as RequestForTimeout. A concurrent Start/Stop either
// owns the lifecycle first or sees the durable deleting Team and cannot start.
func (r *TeamLifecycleRepository) MarkDeleting(ctx context.Context, id int) error {
	return r.sess.TxContext(ctx, func(tx db.Session) error {
		rows, err := tx.SQL().QueryContext(ctx, `SELECT i.id FROM instances i JOIN team_members m ON m.instance_id=i.id WHERE m.team_id=? ORDER BY i.id FOR UPDATE`, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var instance int
			if err = rows.Scan(&instance); err != nil {
				rows.Close()
				return err
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		op, err := readTeamLifecycle(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if err = checkTeamDeletionLifecycle(op); err != nil {
			return err
		}
		var status string
		if err = teamQueryRow(ctx, tx, `SELECT status FROM teams WHERE id=? FOR UPDATE`, id).Scan(&status); err != nil {
			return err
		}
		if status == models.TeamStatusDeleted {
			return nil
		}
		_, err = tx.SQL().ExecContext(ctx, `UPDATE teams SET status='deleting' WHERE id=?`, id)
		return err
	}, nil)
}
