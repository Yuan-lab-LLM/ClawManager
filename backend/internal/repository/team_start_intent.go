package repository

import (
	"context"
	"fmt"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// The explicit Team request already holds every member instance row and the
// Team row. Publish the same user Start intent used by ordinary instances even
// when an upgrade owns the later execution. This creates no Gateway, deadline,
// generation or stop receipt. A subsequent Stop keeps the same instance fence.
func publishExplicitTeamStartIntent(ctx context.Context, s db.Session, team int) error {
	op, err := readTeamLifecycle(ctx, s, team, true)
	if err != nil || op == nil {
		return err
	}
	if op.DesiredState != "running" || (op.State != "starting" && op.State != "running" && op.State != "recovery_required") {
		return models.ErrInstanceStopping
	}
	for _, m := range op.Members {
		var mode, workspace, stop string
		var generation int
		row, err := s.SQL().QueryRowContext(ctx, `SELECT i.instance_mode,COALESCE(i.workspace_path,''),i.runtime_generation,COALESCE(a.stop_operation_id,'') FROM instances i JOIN team_members m ON m.instance_id=i.id LEFT JOIN instance_model_activity a ON a.instance_id=i.id WHERE i.id=? AND m.id=? AND m.team_id=? AND m.status NOT IN ('deleted','deleting') FOR UPDATE`, m.InstanceID, m.MemberID, team)
		if err != nil {
			return err
		}
		if err = row.Scan(&mode, &workspace, &generation, &stop); err != nil {
			return err
		}
		if mode != "lite" || workspace == "" || workspace != m.Workspace || generation < m.Generation {
			return fmt.Errorf("frozen Team Start member changed")
		}
		if stop != "" {
			return models.ErrInstanceStopping
		}
		now := time.Now().UTC()
		if _, err = s.SQL().ExecContext(ctx, `INSERT INTO instance_desired_state(instance_id,desired_power_state,created_at,updated_at) VALUES(?,'running',?,?) ON DUPLICATE KEY UPDATE desired_power_state='running',updated_at=VALUES(updated_at)`, m.InstanceID, now, now); err != nil {
			return err
		}
	}
	return nil
}
