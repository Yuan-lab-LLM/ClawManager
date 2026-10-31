package services

import (
	"clawreef/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Exception is restricted to the frozen, positively inspected unstarted IDs.
// A merely missing binding on an older instance is never sufficient.
func (s *RuntimeUpgradeService) poolSwitchWaitingInstance(ctx context.Context, r *models.RuntimeRollout, id int) bool {
	if !isEmptyOpenClawPoolReset(r) {
		return false
	}
	var plan struct {
		Waiting []int `json:"pool_switch_waiting_instance_ids"`
	}
	if json.Unmarshal([]byte(stringValue(r.PreflightJSON)), &plan) != nil {
		return false
	}
	for _, candidate := range plan.Waiting {
		if candidate == id {
			// A finished switch certifies that this ID had no old Runtime state
			// at the commit boundary. Preserve the proof across later restarts.
			return true
		}
	}
	return false
}

// Eligibility describes durable user state, independently of Agent health.
// A crashed Agent cannot be a prerequisite for repairing its own pool.
type openClawPoolSwitchAssessment struct {
	Busy     bool
	Eligible bool
	Waiting  []int
	Reason   string
}

func (s *RuntimeUpgradeService) assessOpenClawPoolSwitch(ctx context.Context, exclude int64) (*openClawPoolSwitchAssessment, error) {
	if s == nil || s.sess == nil || s.rollouts == nil {
		return nil, fmt.Errorf("pool inspection unavailable")
	}
	result := &openClawPoolSwitchAssessment{Eligible: true}
	active, err := s.rollouts.ListActive(ctx, RuntimeTypeOpenClaw)
	if err != nil {
		return nil, err
	}
	for _, r := range active {
		if exclude > 0 && r.ID > exclude && r.Status == "pending" {
			continue
		}
		if r.ID != exclude && runtimeUpgradeScopeFromRollout(&r).UpgradeLabRunID == nil {
			result.Busy = true
			result.Eligible = false
			result.Reason = fmt.Sprintf("运行池操作 #%d 尚未结束", r.ID)
			return result, nil
		}
	}
	// Older test/compatibility schemas omit description; current installations
	// use it to keep the existing isolated upgrade lab out of business scope.
	var hasDescription int
	column, err := s.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='instances' AND COLUMN_NAME='description'`)
	if err != nil {
		return nil, err
	}
	if err = column.Scan(&hasDescription); err != nil {
		return nil, err
	}
	labFilter := ""
	if hasDescription > 0 {
		labFilter = " AND LOWER(TRIM(COALESCE(i.description,''))) NOT LIKE 'openclaw-upgrade-lab:%'"
	}
	rows, err := s.sess.SQL().QueryContext(ctx, `SELECT i.id,i.user_id,i.status,i.runtime_generation,COALESCE(i.workspace_path,'') FROM instances i WHERE LOWER(TRIM(i.type))='openclaw' AND (LOWER(TRIM(COALESCE(i.instance_mode,'')))='lite' OR (LOWER(TRIM(COALESCE(i.instance_mode,''))) NOT IN ('lite','pro') AND LOWER(TRIM(COALESCE(i.runtime_type,'')))='gateway'))`+labFilter+` ORDER BY i.id`)
	if err != nil {
		return nil, err
	}
	type entry struct {
		id, user, gen int
		status, path  string
	}
	var entries []entry
	for rows.Next() {
		var e entry
		if err = rows.Scan(&e.id, &e.user, &e.status, &e.gen, &e.path); err != nil {
			rows.Close()
			return nil, err
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.gen > 1 || (e.status != "creating" && e.status != "error") {
			result.Eligible = false
			result.Reason = fmt.Sprintf("实例 #%d 需要兼容检查或数据迁移", e.id)
			return result, nil
		}
		var used int
		row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM instance_runtime_bindings WHERE instance_id=?)+(SELECT COUNT(*) FROM runtime_upgrade_items WHERE instance_id=? AND (source_gateway_id IS NOT NULL OR snapshot_ref IS NOT NULL OR state IN ('migrated','verified','restart_ready')))`, e.id, e.id)
		if err != nil {
			return nil, err
		}
		if err = row.Scan(&used); err != nil {
			return nil, err
		}
		if used > 0 {
			result.Eligible = false
			result.Reason = fmt.Sprintf("实例 #%d 已有运行或迁移记录", e.id)
			return result, nil
		}
		root := RuntimeWorkspacePathWithRoot(s.workspaceRoot, RuntimeTypeOpenClaw, e.user, e.id)
		if e.path != "" && !sameCleanPath(root, e.path) {
			result.Eligible = false
			result.Reason = fmt.Sprintf("实例 #%d 工作区归属需要核实", e.id)
			return result, nil
		}
		clean, err := unstartedOpenClawWorkspace(root)
		if err != nil {
			return nil, fmt.Errorf("检查实例 #%d 工作区: %w", e.id, err)
		}
		if !clean {
			result.Eligible = false
			result.Reason = fmt.Sprintf("实例 #%d 包含已有 Runtime 数据，需要兼容检查", e.id)
			return result, nil
		}
		result.Waiting = append(result.Waiting, e.id)
	}
	// Orphan reservations still represent potential writes. A failed read never
	// becomes permission to switch an occupied pool.
	var bindings int
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_runtime_bindings b JOIN instances i ON i.id=b.instance_id WHERE b.runtime_type='openclaw' AND b.state IN ('creating','running','stopping')`+labFilter)
	if err != nil {
		return nil, err
	}
	if err = row.Scan(&bindings); err != nil {
		return nil, err
	}
	if bindings > 0 {
		result.Eligible = false
		result.Reason = "仍有 Gateway 运行或启动预留，需要先完成实例恢复"
	}
	return result, nil
}

// Project inputs are retained. Native configs/session stores establish that a
// workspace may already have been opened; no format is inferred from status.
func unstartedOpenClawWorkspace(root string) (bool, error) {
	found := false
	count := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if os.IsNotExist(err) && path == root {
			return nil
		}
		if err != nil {
			return err
		}
		count++
		if count > 10000 {
			return fmt.Errorf("工作区需要进一步检查")
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.Type()&os.ModeSymlink != 0 {
			if rel != "." && rel != "home" && rel != ".openclaw" && rel != "home/.openclaw" && !strings.HasPrefix(rel, "home/.openclaw/") && !strings.HasPrefix(rel, ".openclaw/") {
				return nil
			}
			found = true
			return filepath.SkipAll
		}
		if d.IsDir() {
			if rel != "." && rel != "home" && rel != ".openclaw" && !strings.HasPrefix(rel, "home/.openclaw") && !strings.HasPrefix(rel, ".openclaw/") {
				return filepath.SkipDir
			}
			if d.Name() == "node_modules" || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		name := strings.ToLower(d.Name())
		if strings.Contains(rel, ".openclaw/") && (name == "openclaw.json" || name == "auth-profiles.json" || name == "models.json" || name == "sessions.json" || strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, ".sqlite") || strings.HasSuffix(name, ".db")) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return !found, err
}

func validateOpenClawServingImage(env map[string]string) error {
	if strings.EqualFold(env["CLAWMANAGER_OPENCLAW_UPGRADE_ONLY"], "true") || strings.EqualFold(env["CLAWMANAGER_OPENCLAW_UPGRADE_WORKER_ENABLED"], "true") {
		return fmt.Errorf("该镜像是升级迁移专用镜像，不能作为团队普通运行池；请选择可创建普通实例的运行镜像")
	}
	if strings.EqualFold(env["CLAWMANAGER_OPENCLAW_SERVING"], "true") || strings.EqualFold(env["CLAWMANAGER_OPENCLAW_SOURCE_COMPATIBILITY"], "true") || strings.EqualFold(env["CLAWMANAGER_OPENCLAW_AUTHENTICATED_INGRESS"], "true") {
		return fmt.Errorf("该镜像需要个人版专用身份及鉴权协议，当前团队运行池尚不支持；请选择团队版兼容运行镜像")
	}
	return nil
}

func (s *RuntimeUpgradeService) ValidateEmptyOpenClawPoolReset(ctx context.Context, exclude int64) error {
	a, err := s.assessOpenClawPoolSwitch(ctx, exclude)
	if err != nil {
		return err
	}
	if !a.Eligible {
		return fmt.Errorf("%s", a.Reason)
	}
	return nil
}
