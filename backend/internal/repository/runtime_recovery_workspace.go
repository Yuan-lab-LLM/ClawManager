package repository

import (
	"context"
	"fmt"
)

type RecoveryWorkspaceIsolation interface {
	VerifyRecoveryWorkspaceIsolation(context.Context, int) error
}

// Do not infer standalone ownership from a removed Team environment alone.
// Historical memberships and overlapping instance workspace references remain
// blockers even when those peers are currently stopped or deleting.
func (r *browserWorkerRepository) VerifyRecoveryWorkspaceIsolation(ctx context.Context, id int) error {
	row, err := r.sess.SQL().QueryRowContext(ctx, `SELECT
 (SELECT COUNT(*) FROM team_members WHERE instance_id=?) +
 (SELECT COUNT(*) FROM instances source JOIN instances other ON other.id<>source.id
  WHERE source.id=? AND COALESCE(other.workspace_path,'')<>'' AND
  (other.workspace_path=source.workspace_path OR other.workspace_path LIKE CONCAT(source.workspace_path,'/%') OR source.workspace_path LIKE CONCAT(other.workspace_path,'/%'))) +
 (SELECT COUNT(*) FROM teams t JOIN instances source ON source.id=? WHERE
  COALESCE(t.shared_mount_path,'')<>'' AND (t.shared_mount_path=source.workspace_path OR t.shared_mount_path LIKE CONCAT(source.workspace_path,'/%') OR source.workspace_path LIKE CONCAT(t.shared_mount_path,'/%')))`, id, id, id)
	if err != nil {
		return err
	}
	var references int
	if err = row.Scan(&references); err != nil {
		return err
	}
	if references != 0 {
		return fmt.Errorf("Team membership or shared workspace requires coordinated writer teardown")
	}
	return nil
}
