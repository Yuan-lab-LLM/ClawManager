package services

import (
	"clawreef/internal/models"
	"context"
	"database/sql"
	"fmt"
	"github.com/upper/db/v4"
	"time"
)

// Terminal business status and writer isolation are separate. A bounded failed
// upgrade must not hold the whole fleet forever; unresolved members remain
// fenced until a scoped recovery succeeds. Never restore old data after activation.
func (s *RuntimeUpgradeService) holdFailedUpgrade(ctx context.Context, r *models.RuntimeRollout, reason string) error {
	now := time.Now().UTC()
	return s.sess.TxContext(ctx, func(tx db.Session) error {
		// Fence only identities still owned by this operation, not a later recovery.
		_, err := tx.SQL().ExecContext(ctx, `UPDATE instances i JOIN runtime_upgrade_items ui ON ui.instance_id=i.id SET i.status=CASE WHEN i.status='creating' THEN 'error' ELSE i.status END,i.runtime_error_code='UPGRADE_RECOVERY_REQUIRED',i.runtime_error_message=? WHERE ui.rollout_id=? AND ui.state NOT IN ('verified','gateway_verified') AND i.runtime_generation IN (ui.source_generation,ui.source_generation+1) AND NOT EXISTS(SELECT 1 FROM runtime_upgrade_items n JOIN runtime_rollouts nr ON nr.id=n.rollout_id WHERE n.instance_id=i.id AND nr.id>? AND nr.status IN ('pending','running','finished'))`, reason, r.ID, r.ID)
		if err != nil {
			return err
		}
		_, err = tx.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET recovery_status='needs_attention',error_message=COALESCE(error_message,?),recovery_error_code=COALESCE(recovery_error_code,'UPGRADE_RECOVERY_REQUIRED') WHERE rollout_id=? AND state NOT IN ('verified','gateway_verified')`, reason, r.ID)
		if err != nil {
			return err
		}
		_, err = tx.SQL().ExecContext(ctx, `UPDATE runtime_rollouts SET status='error',error_message=?,finished_at=COALESCE(finished_at,?),updated_at=? WHERE id=? AND status='running'`, reason, now, now, r.ID)
		return err
	}, nil)
}

func (s *RuntimeUpgradeService) upgradeWaitingFailure(ctx context.Context, r *models.RuntimeRollout) (string, error) {
	var held int
	var progress sql.NullTime
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT COALESCE(SUM(recovery_status='needs_attention'),0),MAX(updated_at) FROM runtime_upgrade_items WHERE rollout_id=? AND isolated_at IS NULL AND state NOT IN ('verified','gateway_verified')`, r.ID)
	if err != nil {
		return "", err
	}
	if err = row.Scan(&held, &progress); err != nil {
		return "", err
	}
	if held > 0 {
		return "升级恢复未通过验证，已停止自动重试；数据及运行隔离保留，请对故障实例重试升级。", nil
	}
	// Item progress, not total batch elapsed time or claim renewals. Hundreds of
	// progressing instances are not failed by a single fleet-wide time budget.
	if progress.Valid && time.Since(progress.Time.UTC()) > 20*time.Minute {
		return fmt.Sprintf("升级在 %s 阶段超过20分钟没有实例进展，已暂停并保留数据，请检查原因后重试。", r.Phase), nil
	}
	return "", nil
}

func (s *RuntimeUpgradeService) waitOrHoldUpgrade(ctx context.Context, r *models.RuntimeRollout) error {
	problem, err := s.upgradeWaitingFailure(ctx, r)
	if err != nil {
		return err
	}
	if problem != "" {
		return s.holdFailedUpgrade(ctx, r, problem)
	}
	return nil
}
