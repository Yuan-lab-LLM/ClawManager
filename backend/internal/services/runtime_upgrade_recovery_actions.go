package services

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

type RuntimeRecoveryInfo struct {
	InstanceID  int                    `json:"instance_id"`
	Generation  int                    `json:"generation"`
	ParentID    int64                  `json:"parent_rollout_id"`
	TargetImage string                 `json:"target_image_ref"`
	CanRetry    bool                   `json:"can_retry"`
	CanRebuild  bool                   `json:"can_rebuild"`
	Isolated    bool                   `json:"isolated"`
	Reason      string                 `json:"reason"`
	Current     *RuntimeUpgradeDetails `json:"current,omitempty"`
}

type RuntimeRecoveryRequest struct {
	Mode            string `json:"mode"`
	ParentID        int64  `json:"parent_rollout_id"`
	Generation      int    `json:"generation"`
	TargetImage     string `json:"target_image_ref,omitempty"`
	IdempotencyKey  string `json:"idempotency_key"`
	ConfirmPreserve bool   `json:"confirm_preserve"`
}

// Read-only. Returning a recovery plan must never enqueue a command or acquire
// a writer lease. Instance/Team ownership is checked by the HTTP handler.
func (s *RuntimeUpgradeService) RecoveryInfo(ctx context.Context, id int) (*RuntimeRecoveryInfo, error) {
	info := &RuntimeRecoveryInfo{InstanceID: id}
	var kind, mode, status string
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT type,instance_mode,status,runtime_generation FROM instances WHERE id=?`, id)
	if err != nil {
		return nil, err
	}
	if err = row.Scan(&kind, &mode, &status, &info.Generation); err != nil {
		return nil, err
	}
	if !strings.EqualFold(kind, "openclaw") || !strings.EqualFold(mode, "lite") {
		info.Reason = "当前运行类型暂不支持升级恢复"
		return info, nil
	}
	var parent int64
	row, err = s.sess.SQL().QueryRowContext(ctx, `SELECT r.id FROM runtime_rollouts r JOIN runtime_upgrade_items ui ON ui.rollout_id=r.id WHERE ui.instance_id=? AND r.preflight_id IS NOT NULL ORDER BY (r.status IN ('pending','running')) DESC,(r.status NOT IN ('preflight_passed','preflight_blocked') OR r.recovery_key IS NOT NULL) DESC,r.id DESC LIMIT 1`, id)
	if err != nil {
		return nil, err
	}
	if err = row.Scan(&parent); errors.Is(err, sql.ErrNoRows) {
		info.Reason = "没有可恢复的升级记录"
		return info, nil
	} else if err != nil {
		return nil, err
	}
	details, err := s.Details(ctx, parent)
	if err != nil {
		return nil, err
	}
	info.ParentID, info.TargetImage, info.Current = parent, details.Rollout.TargetImageRef, details
	info.Isolated = upgradeDetailsItemIsolated(details, id)
	switch details.Rollout.Status {
	case "pending", "running":
		info.Reason = "升级或恢复正在执行，请等待本次结果"
		return info, nil
	case "finished":
		if status == "running" && !upgradeDetailsItemIsolated(details, id) {
			info.Reason = "该实例最近的升级已通过验证"
			return info, nil
		}
	}
	// A historical batch failure is not evidence that every healthy member needs
	// repair. Pending target validation remains a genuine recovery case.
	if status == "running" || status == "stopped" {
		for _, item := range details.Items {
			if item.InstanceID == id && item.IsolatedAt == nil && (item.State == "verified" || stringValue(item.RecoveryStatus) == "restored") {
				info.Reason = "该实例已恢复运行，无需重复修复"
				return info, nil
			}
		}
	}
	if healthy, err := s.restoredSourceHealthy(ctx, parent, id); err != nil {
		return nil, err
	} else if healthy {
		info.Reason = "该实例已在原版本恢复运行，可参与正常升级，无需重复恢复。"
		return info, nil
	}
	if status != "error" && status != "running" && status != "stopped" {
		info.Reason = "实例状态正在变化，请等待检查完成后重试"
		return info, nil
	}
	info.CanRetry = true
	if details.Rollout.Status == "error" || upgradeDetailsItemIsolated(details, id) {
		for _, item := range details.Items {
			if item.InstanceID == id && (item.IsolatedAt != nil || (item.State != "verified" && item.State != "gateway_verified" && (status == "error" || stringValue(item.RecoveryStatus) == "needs_attention"))) {
				info.Isolated = true
			}
		}
	}
	info.CanRebuild = info.Isolated || (runtimeUpgradeScopeFromRollout(details.Rollout).RecoveryMode != "" && (details.Rollout.Status == "error" || details.Rollout.Status == "preflight_blocked")) || strings.Contains(stringValue(details.Rollout.ErrorMessage), "OPENCLAW_PLUGIN_REBUILD_REQUIRED")
	info.Reason = "重试仅处理该实例；Team 成员同时保护所属 Team。文件、配置和历史不会被重置。"
	if info.CanRebuild {
		info.Reason = "上次重试未完成，可保全数据后重建目标版本运行环境。"
	}
	if info.Isolated {
		info.Reason = "该实例已单独隔离，数据保留；其他正常实例可以继续升级。修复不会清空文件、配置或历史。"
	}
	return info, nil
}

// A recovery is a scoped child rollout, not a reset and not resurrection of a
// historical failed rollout. Existing receipts, fencing and leader election own
// all execution; HTTP only freezes the request and queues it.
func (s *RuntimeUpgradeService) RequestRecovery(ctx context.Context, id int, actor int, admin bool, req RuntimeRecoveryRequest) (*models.RuntimeRollout, error) {
	if req.Mode != "retry" && req.Mode != "rebuild" {
		return nil, errors.New("invalid recovery action")
	}
	if len(req.IdempotencyKey) < 8 || len(req.IdempotencyKey) > 128 {
		return nil, errors.New("recovery idempotency key is required")
	}
	if req.Mode == "rebuild" && !req.ConfirmPreserve {
		return nil, errors.New("confirm data-preserving rebuild first")
	}
	keyHash := sha256.Sum256([]byte(fmt.Sprintf("%d/%d/%s", actor, id, req.IdempotencyKey)))
	key := "recovery-" + hex.EncodeToString(keyHash[:])
	var existing models.RuntimeRollout
	if err := s.sess.Collection("runtime_rollouts").Find(db.Cond{"recovery_key": key}).One(&existing); err == nil {
		return s.resumeRecoveryAdmission(ctx, &existing, actor, req)
	} else if err != db.ErrNoMoreRows {
		return nil, err
	}
	info, err := s.RecoveryInfo(ctx, id)
	if err != nil {
		return nil, err
	}
	if !info.CanRetry || info.ParentID != req.ParentID || info.Generation != req.Generation {
		return nil, errors.New("instance recovery state changed; refresh before retrying")
	}
	if req.Mode == "rebuild" && !info.CanRebuild {
		return nil, errors.New("retry upgrade before requesting a data-preserving rebuild")
	}
	ids, err := s.recoveryInstanceScope(ctx, id, actor, admin)
	if err != nil {
		return nil, err
	}
	target := strings.TrimSpace(req.TargetImage)
	if target == "" {
		target = info.TargetImage
	}
	if !admin && target != info.TargetImage {
		return nil, errors.New("只有管理员可以为升级恢复选择其他镜像")
	}
	var excluded []int
	rows, err := s.sess.SQL().QueryContext(ctx, `SELECT id FROM instances WHERE type='openclaw' AND (instance_mode='lite' OR runtime_type='gateway') AND status<>'deleting' AND id<>? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var other int
		if err = rows.Scan(&other); err != nil {
			rows.Close()
			return nil, err
		}
		excluded = append(excluded, other)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Never auto-restore old data after a user-requested forward recovery. A failed
	// child remains isolated with its own capsules, original data and diagnostics.
	result, err := s.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target, BatchSize: 2, AutoRollback: false, ActorUserID: &actor, CandidateInstanceIDs: ids, ExcludeInstanceIDs: excluded, RecoveryMode: req.Mode, RecoveryParentID: info.ParentID, RecoveryKey: key, RecoveryRequestHash: recoveryRequestHash(req)})
	if err != nil {
		if lookup := s.sess.Collection("runtime_rollouts").Find(db.Cond{"recovery_key": key}).One(&existing); lookup == nil {
			return s.resumeRecoveryAdmission(ctx, &existing, actor, req)
		}
		return nil, err
	}
	if result.Rollout == nil {
		return nil, fmt.Errorf("recovery preflight failed: %s", strings.Join(result.Blockers, "; "))
	}
	if !result.Passed {
		return result.Rollout, nil
	}
	return s.ConfirmPreflight(ctx, stringValue(result.Rollout.PreflightID), result.TargetImageRef, &actor)
}

func (s *RuntimeUpgradeService) resumeRecoveryAdmission(ctx context.Context, r *models.RuntimeRollout, actor int, req RuntimeRecoveryRequest) (*models.RuntimeRollout, error) {
	scope := runtimeUpgradeScopeFromRollout(r)
	var frozen struct {
		Hash string `json:"recovery_request_hash"`
	}
	if r.PreflightJSON != nil {
		_ = json.Unmarshal([]byte(*r.PreflightJSON), &frozen)
	}
	if scope.RecoveryMode != req.Mode || scope.RecoveryParentID != req.ParentID || frozen.Hash != recoveryRequestHash(req) {
		return nil, errors.New("recovery request key was already used for a different action")
	}
	if r.Status == "preflight_passed" {
		queued, err := s.ConfirmPreflight(ctx, stringValue(r.PreflightID), r.TargetImageRef, &actor)
		if err == nil {
			return queued, nil
		}
		fresh, readErr := s.rollouts.GetByID(ctx, r.ID)
		if readErr == nil && fresh != nil && fresh.Status != "preflight_passed" {
			return fresh, nil
		}
		return nil, err
	}
	return r, nil
}

func recoveryRequestHash(req RuntimeRecoveryRequest) string {
	raw, _ := json.Marshal([]any{req.Mode, req.ParentID, req.Generation, strings.TrimSpace(req.TargetImage), req.ConfirmPreserve})
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func (s *RuntimeUpgradeService) recoveryInstanceScope(ctx context.Context, id, actor int, admin bool) ([]int, error) {
	// Maintenance protects the Team, but recovery changes ONLY this instance.
	// Healthy teammates (including Hermes) keep their data, bindings and processes.
	rows, err := s.sess.SQL().QueryContext(ctx, `SELECT id,user_id FROM instances WHERE id=?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var instance, owner int
		if err := rows.Scan(&instance, &owner); err != nil {
			return nil, err
		}
		if !admin && owner != actor {
			return nil, errors.New("recovery scope contains another owner's instance")
		}
		ids = append(ids, instance)
	}
	return ids, rows.Err()
}

func (s *RuntimeUpgradeService) lockUpgradeScope(ctx context.Context, tx db.Session, rolloutID int64) error {
	rows, err := tx.SQL().QueryContext(ctx, `SELECT i.id FROM instances i JOIN runtime_upgrade_items ui ON ui.instance_id=i.id WHERE ui.rollout_id=? ORDER BY i.id FOR UPDATE`, rolloutID)
	if err != nil {
		return err
	}
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		var count int
		row, e := tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_upgrade_items ui JOIN runtime_rollouts r ON r.id=ui.rollout_id WHERE ui.instance_id=? AND r.id<>? AND r.status IN ('pending','running')`, id, rolloutID)
		if e != nil {
			return e
		}
		if e = row.Scan(&count); e != nil {
			return e
		}
		if count > 0 {
			return fmt.Errorf("instance %d is already owned by an active upgrade", id)
		}
	}
	return nil
}

func (s *RuntimeUpgradeService) upgradeItemRecovery(ctx context.Context, item models.RuntimeUpgradeItem) runtimeUpgradeScope {
	r, err := s.rollouts.GetByID(ctx, item.RolloutID)
	if err != nil {
		return runtimeUpgradeScope{}
	}
	return runtimeUpgradeScopeFromRollout(r)
}

// New repair controls can be required per action without breaking legacy normal
// upgrades. Older Runtime images must not silently ignore preservation flags.
func upgradeRecoveryCapability(mode string) string {
	if mode == "rebuild" {
		return "openclaw.rebuild-preserve-v2"
	}
	return ""
}

func (s *RuntimeUpgradeService) attachRecoveryMigrationReceipt(ctx context.Context, r *models.RuntimeRollout, item models.RuntimeUpgradeItem, candidate runtimeUpgradeCandidate, req *RuntimeAgentWorkspaceRequest) {
	scope := runtimeUpgradeScopeFromRollout(r)
	if scope.RecoveryMode != "retry" || scope.RecoveryParentID <= 0 || candidate.SourceImageDigest == "" || candidate.SourceImageDigest != stringValue(r.TargetImageDigest) {
		return
	}
	parent, err := s.rollouts.GetByID(ctx, scope.RecoveryParentID)
	if err != nil || parent == nil || stringValue(parent.RollbackStatus) != "" || parent.TargetImageRef != r.TargetImageRef {
		return
	}
	var prior models.RuntimeUpgradeItem
	if s.sess.Collection("runtime_upgrade_items").Find(db.Cond{"rollout_id": parent.ID, "instance_id": item.InstanceID}).One(&prior) != nil || prior.SourceGeneration == nil {
		return
	}
	switch prior.State {
	case "migrated", "restart_ready", "gateway_verified", "verified":
	default:
		return
	}
	req.ResumeRolloutID = fmt.Sprint(parent.ID)
	req.ResumeGeneration = *prior.SourceGeneration
}
