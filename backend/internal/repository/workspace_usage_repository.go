package repository

import (
	"clawreef/internal/models"
	"context"
	"fmt"
	"github.com/upper/db/v4"
	"time"
)

type WorkspaceUsageRepository struct{ sess db.Session }

func NewWorkspaceUsageRepository(sess db.Session) *WorkspaceUsageRepository {
	return &WorkspaceUsageRepository{sess: sess}
}

// Keyset pages include stopped/archived native instances. The worker advances
// after every attempt, so a slow or inaccessible tree cannot monopolize a pass.
func (r *WorkspaceUsageRepository) Candidates(ctx context.Context, after, limit int) ([]models.Instance, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	rows, err := r.sess.SQL().QueryContext(ctx, `SELECT id,user_id,owner,type,runtime_type,instance_mode,workspace_path,runtime_generation,workspace_usage_epoch,workspace_usage_observed_at FROM instances WHERE id>? AND workspace_path IS NOT NULL AND workspace_path<>'' AND workspace_usage_blocked=FALSE AND type IN ('openclaw','opencode','hermes','deepseek-harness') AND instance_mode='lite' ORDER BY id LIMIT ?`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.Instance, 0)
	for rows.Next() {
		var i models.Instance
		if err := rows.Scan(&i.ID, &i.UserID, &i.Owner, &i.Type, &i.RuntimeType, &i.InstanceMode, &i.WorkspacePath, &i.RuntimeGeneration, &i.WorkspaceUsageEpoch, &i.WorkspaceUsageObservedAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// A stale generation/path/owner/reset observation cannot publish. Observation
// updates deliberately leave updated_at alone: they are not business activity.
func (r *WorkspaceUsageRepository) Publish(ctx context.Context, i models.Instance, n int64, observed time.Time) (bool, error) {
	if i.ID <= 0 || i.UserID <= 0 || i.WorkspacePath == nil || n < 0 || observed.IsZero() {
		return false, fmt.Errorf("invalid workspace observation")
	}
	result, err := r.sess.SQL().ExecContext(ctx, `UPDATE instances SET workspace_usage_bytes=?,workspace_usage_observed_at=?,updated_at=updated_at WHERE id=? AND user_id=? AND BINARY owner<=>BINARY ? AND BINARY type=BINARY ? AND BINARY runtime_type=BINARY ? AND BINARY instance_mode=BINARY ? AND BINARY workspace_path=BINARY ? AND runtime_generation=? AND workspace_usage_epoch=? AND workspace_usage_blocked=FALSE AND (workspace_usage_observed_at IS NULL OR workspace_usage_observed_at<=?)`, n, observed.UTC(), i.ID, i.UserID, i.Owner, i.Type, i.RuntimeType, i.InstanceMode, *i.WorkspacePath, i.RuntimeGeneration, i.WorkspaceUsageEpoch, observed.UTC())
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}
