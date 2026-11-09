package services

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"clawreef/internal/models"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/upper/db/v4"
)

type upgradeGroupScopeKey struct{}

func upgradeDetailsItemIsolated(d *RuntimeUpgradeDetails, id int) bool {
	for _, i := range d.Items {
		if i.InstanceID == id {
			return i.IsolatedAt != nil
		}
	}
	return false
}

var errAllUpgradeGroupsIsolated = errors.New("所有待升级实例已隔离并保留数据，请修复后单独重试")
var errUpgradeImageSuspect = errors.New("UPGRADE_IMAGE_REVIEW_REQUIRED: 多个独立实例出现相同兼容错误，已暂停该目标镜像；原版本及数据保留，请选择修复镜像")
var upgradeInstanceNumbers = regexp.MustCompile(`instance\s+[0-9]+|output_sha256=[a-f0-9]+`)

func upgradeFailureFingerprint(err error) string {
	text := err.Error()
	if !errors.Is(err, ErrRuntimeAgentConflict) || strings.Contains(text, "CUSTOM_REVIEW_REQUIRED") || strings.Contains(text, "PLUGIN_PLAN_CHANGED") {
		return ""
	}
	if _, after, ok := strings.Cut(text, "runtime agent conflict:"); ok {
		text = after
	}
	sum := sha256.Sum256([]byte(upgradeInstanceNumbers.ReplaceAllString(text, "<execution>")))
	return fmt.Sprintf("%x", sum)
}
func (s *RuntimeUpgradeService) upgradeImageCircuitOpen(ctx context.Context, r *models.RuntimeRollout) (bool, error) {
	var n int
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT JSON_UNQUOTE(JSON_EXTRACT(a.detail_json,'$.fingerprint')) AS fp FROM runtime_upgrade_audits a JOIN runtime_rollouts r ON r.id=a.rollout_id WHERE r.target_image_ref=? AND a.action='upgrade_group_isolated' AND a.phase='compatibility_check' AND JSON_UNQUOTE(JSON_EXTRACT(a.detail_json,'$.fingerprint'))<>'' GROUP BY fp HAVING COUNT(DISTINCT JSON_EXTRACT(a.detail_json,'$.group'))>=3) failures`, r.TargetImageRef)
	if err != nil {
		return false, err
	}
	err = row.Scan(&n)
	return n > 0, err
}

func (s *RuntimeUpgradeService) hasIsolatedUpgradeItems(ctx context.Context, id int64) bool {
	var n int
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_upgrade_items WHERE rollout_id=? AND isolated_at IS NOT NULL`, id)
	if err != nil {
		return true
	}
	if row.Scan(&n) != nil {
		return true
	}
	return n > 0
}
func upgradeGroupScope(ctx context.Context) map[int]bool {
	ids, _ := ctx.Value(upgradeGroupScopeKey{}).(map[int]bool)
	return ids
}
func activeUpgradeItems(items []models.RuntimeUpgradeItem) []models.RuntimeUpgradeItem {
	active := make([]models.RuntimeUpgradeItem, 0, len(items))
	for _, i := range items {
		if i.IsolatedAt == nil {
			active = append(active, i)
		}
	}
	return active
}
func upgradeGroupID(item models.RuntimeUpgradeItem) string {
	if item.TeamID != nil {
		return fmt.Sprintf("team:%d", *item.TeamID)
	}
	return fmt.Sprintf("instance:%d", item.InstanceID)
}

// A failed item is durable, not a skipped success. Independent groups continue;
// every admitted operation joins before group recovery or target activation.
func (s *RuntimeUpgradeService) runConvergentUpgradeItems(ctx context.Context, r *models.RuntimeRollout, phase string, items []models.RuntimeUpgradeItem, concurrency int, run func(context.Context, int) error) error {
	var mu sync.Mutex
	failed := map[string]error{}
	err := runUpgradeBounded(ctx, len(items), concurrency, func(ctx context.Context, index int) error {
		item := items[index]
		group := upgradeGroupID(item)
		mu.Lock()
		blocked := failed[group] != nil
		mu.Unlock()
		if blocked {
			return nil
		}
		operationCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
		defer cancel()
		err := s.withUpgradeClaim(operationCtx, r.ID, item.InstanceID, phase, func(c context.Context) error {
			if item.PhaseEnteredAt != nil && stringValue(item.OperationPhase) == phase && time.Since(*item.PhaseEnteredAt) >= 20*time.Minute {
				return fmt.Errorf("%w: instance %d phase %s exceeded its progress deadline", ErrUpgradeSourceState, item.InstanceID, phase)
			}
			if err := run(c, index); err != nil {
				return err
			}
			var current models.RuntimeUpgradeItem
			if err := s.sess.Collection("runtime_upgrade_items").Find(db.Cond{"rollout_id": r.ID, "instance_id": item.InstanceID}).One(&current); err != nil {
				return err
			}
			if stringValue(current.RecoveryStatus) == "needs_attention" {
				return fmt.Errorf("%w: instance %d: %s", ErrUpgradeSourceState, item.InstanceID, stringValue(current.ErrorMessage))
			}
			return nil
		})
		if err == nil || errors.Is(err, ErrRuntimeUpgradeBusy) {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var mysqlErr *mysqldriver.MySQLError
		if errors.As(err, &mysqlErr) {
			return err
		}
		for _, shared := range []string{"stale file handle", "read-only file system", "input/output error"} {
			if strings.Contains(strings.ToLower(err.Error()), shared) {
				return err
			}
		}
		if runtimeUpgradeInfrastructureRetryable(err) {
			return nil
		}
		// SQL/control-plane errors are not evidence that one workspace is bad.
		if !errors.Is(err, ErrRuntimeAgentConflict) && !errors.Is(err, ErrUpgradeSourceState) && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		mu.Lock()
		if failed[group] == nil {
			failed[group] = err
		}
		mu.Unlock()
		return nil
	})
	if err != nil {
		return err
	}
	all, err := s.listUpgradeItems(ctx, r.ID)
	if err != nil {
		return err
	}
	for group, cause := range failed {
		ids := map[int]bool{}
		for _, i := range all {
			if upgradeGroupID(i) == group {
				ids[i.InstanceID] = true
			}
		}
		if err = s.sess.TxContext(ctx, func(tx db.Session) error {
			for id := range ids {
				if _, e := tx.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET isolated_at=COALESCE(isolated_at,UTC_TIMESTAMP(6)),isolation_phase=COALESCE(isolation_phase,?),recovery_status='needs_attention',recovery_error_code=COALESCE(recovery_error_code,'UPGRADE_GROUP_ISOLATED'),error_message=COALESCE(error_message,?),updated_at=UTC_TIMESTAMP(6) WHERE rollout_id=? AND instance_id=?`, phase, cause.Error(), r.ID, id); e != nil {
					return e
				}
			}
			return nil
		}, nil); err != nil {
			return err
		}
		if err = s.audit(ctx, &r.ID, r.StartedBy, "upgrade_group_isolated", phase, "needs_attention", map[string]any{"group": group, "reason": cause.Error(), "fingerprint": upgradeFailureFingerprint(cause), "data_deleted": false}); err != nil {
			return err
		}
	}
	return nil
}

// Only scoped item state is restored. Never scale or deactivate a shared target
// while independent successful groups still need it.
func (s *RuntimeUpgradeService) settleIsolatedUpgradeGroups(ctx context.Context, r *models.RuntimeRollout) error {
	items, err := s.listUpgradeItems(ctx, r.ID)
	if err != nil {
		return err
	}
	groups := map[string][]models.RuntimeUpgradeItem{}
	for _, i := range items {
		if i.IsolatedAt != nil {
			groups[upgradeGroupID(i)] = append(groups[upgradeGroupID(i)], i)
		}
	}
	for _, group := range groups {
		var newer int
		for _, i := range group {
			row, e := s.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_upgrade_items n JOIN runtime_rollouts nr ON nr.id=n.rollout_id WHERE n.instance_id=? AND nr.id>? AND nr.status IN('pending','running','finished','error')`, i.InstanceID, r.ID)
			if e != nil {
				return e
			}
			var n int
			if e = row.Scan(&n); e != nil {
				return e
			}
			newer += n
		}
		if newer > 0 {
			continue
		} // a newer recovery owns this group now
		ids := map[int]bool{}
		mutated := false
		for _, i := range group {
			ids[i.InstanceID] = true
			if i.State != "prepared" && i.State != "compatibility_checked" && i.State != "pending" {
				mutated = true
			}
		}
		groupCtx := context.WithValue(ctx, upgradeGroupScopeKey{}, ids)
		preActivation := group[0].IsolationPhase != nil && (*group[0].IsolationPhase == "compatibility_check" || *group[0].IsolationPhase == "session_migration")
		if mutated && preActivation && r.AutoRollback {
			held := false
			for _, i := range group {
				if stringValue(i.RecoveryErrorCode) == "UPGRADE_GROUP_RESTORE_HELD" || (stringValue(i.OperationPhase) == "rollback_restore" && i.OperationFailures >= 5) {
					held = true
				}
			}
			if held {
				continue
			}
			// An individual failed restore is bounded by the claim retry budget;
			// leave its original data/identity fenced and allow other groups onward.
			if err = s.Rollback(groupCtx, r); err == nil {
				err = s.CompleteRollback(groupCtx, r)
			}
			if err != nil {
				code := "UPGRADE_GROUP_ISOLATED"
				if !runtimeUpgradeInfrastructureRetryable(err) {
					code = "UPGRADE_GROUP_RESTORE_HELD"
				}
				for id := range ids {
					_, _ = s.sess.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET operation_last_error=?,recovery_status='needs_attention',recovery_error_code=? WHERE rollout_id=? AND instance_id=?`, err.Error(), code, r.ID, id)
				}
				continue
			}
			for id := range ids {
				_, err = s.sess.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET recovery_status='source_restore_ready' WHERE rollout_id=? AND instance_id=?`, r.ID, id)
				if err != nil {
					return err
				}
			}
		}
		// No stopped writer => no data rollback or generation change is needed.
		group, err = s.listUpgradeItems(groupCtx, r.ID)
		if err != nil {
			return err
		}
		healthy := true
		for _, i := range group {
			ok, e := s.isolatedSourceServing(ctx, i)
			if e != nil {
				return e
			}
			healthy = healthy && ok
		}
		if healthy && (!mutated || preActivation) {
			for id := range ids {
				if _, err = s.sess.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET recovery_status='source_restored' WHERE rollout_id=? AND instance_id=?`, r.ID, id); err != nil {
					return err
				}
			}
			teams := map[int]struct{}{}
			for _, i := range group {
				if i.TeamID != nil {
					teams[*i.TeamID] = struct{}{}
				}
			}
			if err = s.setTeamMaintenance(ctx, teams, r.ID, false); err != nil {
				return err
			}
		} else if mutated && preActivation {
			for _, i := range group {
				if i.IsolatedAt != nil && time.Since(*i.IsolatedAt) > 20*time.Minute && stringValue(i.RecoveryStatus) == "source_restore_ready" {
					if _, err = s.sess.SQL().ExecContext(ctx, `UPDATE instances SET status='error',runtime_error_code='UPGRADE_RECOVERY_REQUIRED',runtime_error_message='原版本恢复未在期限内通过验证；数据保留，请单独修复升级。' WHERE id=? AND status='creating' AND runtime_generation=?`, i.InstanceID, intValue(i.SourceGeneration, 0)+1); err != nil {
						return err
					}
					if _, err = s.sess.SQL().ExecContext(ctx, `UPDATE runtime_upgrade_items SET recovery_status='needs_attention',recovery_error_code='UPGRADE_GROUP_RESTORE_HELD' WHERE rollout_id=? AND instance_id=?`, r.ID, i.InstanceID); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (s *RuntimeUpgradeService) isolatedSourceServing(ctx context.Context, item models.RuntimeUpgradeItem) (bool, error) {
	if item.RuntimePodID == nil || item.SourceGeneration == nil {
		return false, nil
	}
	b, err := s.bindings.GetByInstanceID(ctx, item.InstanceID)
	if err != nil || b == nil {
		return false, err
	}
	want := *item.SourceGeneration
	if item.State == "restart_ready" || item.State == "restored" {
		want++
	}
	if b.RuntimePodID != *item.RuntimePodID || b.Generation != want || b.State != "running" || b.LastHealthAt == nil {
		return false, nil
	}
	p, err := s.pods.GetByID(ctx, b.RuntimePodID)
	if err != nil || p == nil {
		return false, err
	}
	if p.State != "ready" || p.Draining || p.LastSeenAt == nil || !persistedSourcePodMatches(*p, item) || !upgradeRecoveryObservationFresh(time.Now().UTC(), sql.NullTime{Time: *p.LastSeenAt, Valid: true}, sql.NullTime{Time: *b.LastHealthAt, Valid: true}) {
		return false, nil
	}
	reader, ok := s.agent.(gatewayStateReader)
	if !ok {
		return false, nil
	}
	check, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	g, err := reader.GatewayState(check, stringValue(p.AgentEndpoint), b.GatewayID)
	return err == nil && g != nil && g.InstanceID == item.InstanceID && g.Generation == want && g.GatewayID == b.GatewayID && g.State == "running", nil
}

func (s *RuntimeUpgradeService) finishConvergentUpgrade(ctx context.Context, r *models.RuntimeRollout) (bool, error) {
	var isolated, remaining int
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT COALESCE(SUM(isolated_at IS NOT NULL),0),COALESCE(SUM(isolated_at IS NULL AND state NOT IN('gateway_verified','verified')),0) FROM runtime_upgrade_items WHERE rollout_id=?`, r.ID)
	if err != nil {
		return false, err
	}
	if err = row.Scan(&isolated, &remaining); err != nil {
		return false, err
	}
	if remaining != 0 {
		return false, nil
	}
	if isolated > 0 {
		message := fmt.Sprintf("升级处理完成，%d 个实例已隔离并保留数据；其余实例已通过升级验证。", isolated)
		r.ErrorMessage = &message
		_, err = s.sess.SQL().ExecContext(ctx, `UPDATE runtime_rollouts SET error_message=? WHERE id=?`, message, r.ID)
	}
	return true, err
}

func managedPluginPlanFromItem(item models.RuntimeUpgradeItem) string {
	var evidence struct {
		Compatibility RuntimeAgentUpgradeCompatibility `json:"compatibility"`
	}
	if item.PreflightJSON != nil {
		_ = json.Unmarshal([]byte(*item.PreflightJSON), &evidence)
	}
	return evidence.Compatibility.ManagedPluginPlanSHA256
}
