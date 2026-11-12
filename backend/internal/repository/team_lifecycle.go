package repository

import (
	"clawreef/internal/models"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/upper/db/v4"
	"time"
)

var ErrTeamNotRunning = errors.New("团队已停止或正在启停，请先启动团队后重试")

type TeamLifecycleRepository struct{ sess db.Session }

func NewTeamLifecycleRepository(s db.Session) *TeamLifecycleRepository {
	return &TeamLifecycleRepository{sess: s}
}

func ensureTeamLifecycle(ctx context.Context, s db.Session, id int) error {
	_, err := s.SQL().ExecContext(ctx, `INSERT IGNORE INTO team_runtime_lifecycle(team_id) SELECT id FROM teams WHERE id=?`, id)
	return err
}
func readTeamLifecycle(ctx context.Context, s db.Session, id int, lock bool) (*models.TeamLifecycleOperation, error) {
	q := `SELECT team_id,state,operation_id,desired_state,reason,COALESCE(members_json,JSON_ARRAY()),progress,attempts,error_message,started_at,stopped_at FROM team_runtime_lifecycle WHERE team_id=?`
	if lock {
		q += " FOR UPDATE"
	}
	v := &models.TeamLifecycleOperation{}
	var raw []byte
	err := teamQueryRow(ctx, s, q, id).Scan(&v.TeamID, &v.State, &v.OperationID, &v.DesiredState, &v.Reason, &raw, &v.Progress, &v.Attempts, &v.Error, &v.StartedAt, &v.StoppedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &v.Members); err != nil {
		return nil, err
	}
	v.Total = len(v.Members)
	v.CanStart = v.State == "stopped" || v.State == "recovery_required"
	v.CanStop = v.State == "running" || v.State == "recovery_required"
	return v, nil
}
func (r *TeamLifecycleRepository) Get(ctx context.Context, id int) (*models.TeamLifecycleOperation, error) {
	v, err := readTeamLifecycle(ctx, r.sess, id, false)
	if err != nil || v == nil || v.State != "running" {
		return v, err
	}
	// Completed Start is historical evidence, not permanent member readiness.
	// Keep the operation ledger unchanged; expose a repair action for explicit
	// failed/stopped members, never for transient heartbeat uncertainty.
	var failed int
	err = teamQueryRow(ctx, r.sess, `SELECT COUNT(*) FROM team_members m JOIN instances i ON i.id=m.instance_id WHERE m.team_id=? AND m.status<>'deleted' AND i.status IN ('error','stopped')`, id).Scan(&failed)
	if err != nil {
		return nil, err
	}
	if failed > 0 {
		v.State = "recovery_required"
		v.Error = fmt.Sprintf("%d 个成员未运行，请继续原团队启动操作", failed)
		v.CanStart, v.CanStop = true, true
	}
	return v, nil
}

// Same Team row lock is used by dispatch and model admission. Heartbeats never
// touch this table or the business idle clock.
func RequireTeamRunning(ctx context.Context, s db.Session, id int) error {
	if err := ensureTeamLifecycle(ctx, s, id); err != nil {
		return err
	}
	v, err := readTeamLifecycle(ctx, s, id, true)
	if err != nil {
		return err
	}
	if v == nil || v.State != "running" {
		return ErrTeamNotRunning
	}
	return nil
}

func teamForInstance(ctx context.Context, s db.Session, id int) (int, error) {
	rows, err := s.SQL().QueryContext(ctx, `SELECT DISTINCT m.team_id FROM team_members m JOIN teams t ON t.id=m.team_id JOIN instances i ON i.id=? WHERE t.status<>'deleted' AND m.status<>'deleted' AND (m.instance_id=i.id OR (t.resource_scope='northbound' AND i.provisioning_operation_id=SHA2(CONCAT('owner-team-',t.id,'-member-',m.member_key),256))) LIMIT 2`, id)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n, team := 0, 0
	for rows.Next() {
		if err = rows.Scan(&team); err != nil {
			return 0, err
		}
		n++
	}
	if err = rows.Err(); err != nil {
		return 0, err
	}
	if n > 1 {
		return 0, fmt.Errorf("ambiguous Team instance")
	}
	return team, nil
}
func RequireInstanceTeamRunning(ctx context.Context, s db.Session, id int) error {
	team, err := teamForInstance(ctx, s, id)
	if err != nil || team == 0 {
		return err
	}
	return RequireTeamRunning(ctx, s, team)
}
func (r *TeamLifecycleRepository) InstanceTeam(ctx context.Context, id int) (int, error) {
	return teamForInstance(ctx, r.sess, id)
}

func teamHasOpenWork(ctx context.Context, s db.Session, id int) error {
	// Adapters retire execution for terminal unsuccessful roots, but their old
	// child ledger may retain a nonterminal status. Do not rewrite that history
	// or let it prevent maintenance forever. Missing/foreign/successful parents
	// still block. Runtime execution/write proofs remain mandatory, and automatic
	// stop separately requires the latest user task to have formally succeeded.
	var n int
	err := teamQueryRow(ctx, s, `SELECT
 (SELECT COUNT(*) FROM team_tasks WHERE team_id=? AND status NOT IN ('succeeded','failed','cancelled','skipped','superseded')) +
 (SELECT COUNT(*) FROM team_work_items w WHERE w.team_id=? AND w.superseded_by IS NULL AND w.status NOT IN ('succeeded','failed','cancelled','skipped','superseded') AND NOT EXISTS (SELECT 1 FROM team_tasks t WHERE t.id=w.root_task_id AND t.team_id=w.team_id AND t.status IN ('failed','cancelled','skipped','superseded'))) +
 (SELECT COUNT(*) FROM team_event_outbox WHERE team_id=? AND status<>'delivered') +
 (SELECT COUNT(*) FROM team_workspace_write_intents WHERE team_id=?)`, id, id, id, id).Scan(&n)
	if err != nil {
		return err
	}
	if n != 0 {
		return fmt.Errorf("团队仍有未结束任务、审核或待处理通知")
	}
	return nil
}
func (r *TeamLifecycleRepository) Busy(ctx context.Context, id int) error {
	return teamHasOpenWork(ctx, r.sess, id)
}

func frozenTeamMembers(ctx context.Context, s db.Session, id int) ([]models.TeamLifecycleMember, error) {
	rows, err := s.SQL().QueryContext(ctx, `SELECT m.id,i.id,i.runtime_generation,m.role,COALESCE(i.workspace_path,'') FROM team_members m JOIN instances i ON i.id=m.instance_id WHERE m.team_id=? AND m.status NOT IN ('deleted','deleting','creating','failed') ORDER BY i.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.TeamLifecycleMember
	for rows.Next() {
		var v models.TeamLifecycleMember
		if err = rows.Scan(&v.MemberID, &v.InstanceID, &v.Generation, &v.Role, &v.Workspace); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	var total int
	if err = teamQueryRow(ctx, s, `SELECT COUNT(*) FROM team_members WHERE team_id=? AND status<>'deleted'`, id).Scan(&total); err != nil {
		return nil, err
	}
	if total == 0 || total != len(out) {
		return nil, fmt.Errorf("团队成员创建或清理尚未完成")
	}
	return out, nil
}

// Eligibility uses accepted terminal business facts and Gateway requests, never
// Team/member updated_at, Redis keepalives, LastSeenAt or polling timestamps.
func teamIdle(ctx context.Context, s db.Session, id int, timeout time.Duration) error {
	if err := teamHasOpenWork(ctx, s, id); err != nil {
		return err
	}
	var status string
	var finished sql.NullTime
	err := teamQueryRow(ctx, s, `SELECT status,finished_at FROM team_tasks WHERE team_id=? AND created_by IS NOT NULL AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(IF(JSON_VALID(payload_json),payload_json,'{}'),'$.intent')),'')<>'team_bootstrap_introduction' ORDER BY id DESC LIMIT 1`, id).Scan(&status, &finished)
	if err != nil {
		return fmt.Errorf("上一条用户任务完成事实不可确认: %w", err)
	}
	if status != "succeeded" || !finished.Valid {
		return fmt.Errorf("上一条用户任务尚未正式成功完成")
	}
	cutoff := time.Now().UTC().Add(-timeout)
	if finished.Time.After(cutoff) {
		return fmt.Errorf("用户任务完成后尚未达到空闲期限")
	}
	var n int
	err = teamQueryRow(ctx, s, `SELECT COUNT(*) FROM team_runtime_lifecycle WHERE team_id=? AND observed_since<=? AND (started_at IS NULL OR started_at<=?)`, id, cutoff, cutoff).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("团队可靠观察或启动保护期尚未结束")
	}
	err = teamQueryRow(ctx, s, `SELECT COUNT(*) FROM team_members m JOIN instances i ON i.id=m.instance_id LEFT JOIN instance_model_activity a ON a.instance_id=i.id WHERE m.team_id=? AND m.status<>'deleted' AND (i.status NOT IN ('running','stopped') OR a.instance_id IS NULL OR a.generation<>i.runtime_generation OR a.auto_stop_disabled=TRUE OR a.start_deadline_at IS NOT NULL OR a.stop_operation_id<>'' OR a.monitoring_since>? OR a.last_gateway_activity_at>?)`, id, cutoff, cutoff).Scan(&n)
	if err != nil {
		return err
	}
	if n != 0 {
		return fmt.Errorf("成员活动、代数或停机设置不满足整体休眠条件")
	}
	return teamMembersNoRequests(ctx, s, id)
}
func teamMembersNoRequests(ctx context.Context, s db.Session, id int) error {
	var n int
	err := teamQueryRow(ctx, s, `SELECT
 (SELECT COUNT(*) FROM instance_model_activity_requests q JOIN team_members m ON m.instance_id=q.instance_id JOIN instances i ON i.id=q.instance_id WHERE m.team_id=? AND q.generation=i.runtime_generation AND q.finished_at IS NULL) +
 (SELECT COUNT(*) FROM gateway_runtime_tasks q JOIN team_members m ON m.instance_id=q.instance_id JOIN instances i ON i.id=q.instance_id WHERE m.team_id=? AND q.generation=i.runtime_generation AND q.finished=FALSE) +
 (SELECT COUNT(*) FROM instance_commands q JOIN team_members m ON m.instance_id=q.instance_id WHERE m.team_id=? AND q.status IN ('pending','queued','accepted','batch_pending','dispatched','running')) +
 (SELECT COUNT(*) FROM instance_workspace_write_intents q JOIN team_members m ON m.instance_id=q.instance_id WHERE m.team_id=?)`, id, id, id, id).Scan(&n)
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("成员仍有模型请求、工具命令或未确认写入")
	}
	return nil
}

// Manual stop first fences new model admission, then drains existing requests
// in preflight. Commands, native tasks and writes must already be settled.
// Automatic idle stop retains the stronger zero-request admission above.
func teamMembersCanDrain(ctx context.Context, s db.Session, id int) error {
	var n int
	err := teamQueryRow(ctx, s, `SELECT
 (SELECT COUNT(*) FROM gateway_runtime_tasks q JOIN team_members m ON m.instance_id=q.instance_id JOIN instances i ON i.id=q.instance_id WHERE m.team_id=? AND q.generation=i.runtime_generation AND q.finished=FALSE) +
 (SELECT COUNT(*) FROM instance_commands q JOIN team_members m ON m.instance_id=q.instance_id WHERE m.team_id=? AND q.status IN ('pending','queued','accepted','batch_pending','dispatched','running')) +
 (SELECT COUNT(*) FROM instance_workspace_write_intents q JOIN team_members m ON m.instance_id=q.instance_id WHERE m.team_id=?)`, id, id, id).Scan(&n)
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("成员仍有工具命令或未确认写入")
	}
	return nil
}

func (r *TeamLifecycleRepository) Request(ctx context.Context, id int, target, reason string, days int) (*models.TeamLifecycleOperation, error) {
	return r.RequestForTimeout(ctx, id, target, reason, models.InstanceAutoStopIdleTimeout(days))
}

// RequestForTimeout preserves the ordinary-instance seconds setting without
// treating a member heartbeat as business activity.
func (r *TeamLifecycleRepository) RequestForTimeout(ctx context.Context, id int, target, reason string, timeout time.Duration) (*models.TeamLifecycleOperation, error) {
	if reason == "idle_timeout" && timeout < time.Second {
		return nil, fmt.Errorf("idle timeout must be at least one second")
	}
	err := r.sess.Tx(func(s db.Session) error {
		// Lock instance rows first, matching model admission's lock order.
		rows, e := s.SQL().QueryContext(ctx, `SELECT i.id FROM instances i JOIN team_members m ON m.instance_id=i.id WHERE m.team_id=? ORDER BY i.id FOR UPDATE`, id)
		if e != nil {
			return e
		}
		for rows.Next() {
			var ignored int
			if e = rows.Scan(&ignored); e != nil {
				rows.Close()
				return e
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if e = ensureTeamLifecycle(ctx, s, id); e != nil {
			return e
		}
		v, e := readTeamLifecycle(ctx, s, id, true)
		if e != nil {
			return e
		}
		if v == nil {
			return fmt.Errorf("Team not found")
		}
		var teamStatus string
		if e = teamQueryRow(ctx, s, `SELECT status FROM teams WHERE id=? FOR UPDATE`, id).Scan(&teamStatus); e != nil {
			return e
		}
		if teamStatus != "running" {
			return fmt.Errorf("团队创建、删除或修复尚未完成")
		}
		if v.DesiredState == target && (v.State == target || v.State == "starting" || v.State == "stopping") {
			// An accepted running projection is not permanent readiness evidence.
			// An explicit whole-Team Start may recover an errored member through
			// the existing confirmed cleanup/start path. Healthy members are kept;
			// active model requests prohibit this maintenance transition.
			if target != "running" || v.State != "running" || reason != "manual" {
				return nil
			}
			var failed int
			if e = teamQueryRow(ctx, s, `SELECT COUNT(*) FROM team_members m JOIN instances i ON i.id=m.instance_id WHERE m.team_id=? AND m.status<>'deleted' AND i.status='error'`, id).Scan(&failed); e != nil {
				return e
			}
			if failed == 0 {
				return nil
			}
			if e = teamMembersNoRequests(ctx, s, id); e != nil {
				return e
			}
		}
		if v.State == "recovery_required" && v.DesiredState == target {
			phase := "starting"
			if target == "stopped" {
				phase = "stopping"
			}
			_, e = s.SQL().ExecContext(ctx, `UPDATE team_runtime_lifecycle SET state=?,error_message='',next_attempt_at=UTC_TIMESTAMP(6) WHERE team_id=? AND operation_id=?`, phase, id, v.OperationID)
			return e
		}
		if v.State == "starting" || v.State == "stopping" {
			return fmt.Errorf("团队已有启停操作，请等待原操作完成")
		}
		if target == "stopped" {
			if e = teamHasOpenWork(ctx, s, id); e != nil {
				return e
			}
			if reason == "manual" {
				e = teamMembersCanDrain(ctx, s, id)
			} else {
				e = teamMembersNoRequests(ctx, s, id)
			}
			if e != nil {
				return e
			}
			if reason == "idle_timeout" {
				if e = teamIdle(ctx, s, id, timeout); e != nil {
					return e
				}
			}
		}
		members, e := frozenTeamMembers(ctx, s, id)
		if e != nil {
			return e
		}
		// Reuse the coordinator's completed whole-Team stop as durable provenance
		// for unbound start timeouts, which upstream does not log as per-gateway
		// stops. These fields are server-owned; a partial stop never grants them.
		if target == "running" && v.State == "stopped" && v.DesiredState == "stopped" && v.Progress == len(v.Members) && v.StoppedAt != nil && v.OperationID != "" {
			var unconfirmed int
			e = teamQueryRow(ctx, s, `SELECT COUNT(*) FROM team_members m JOIN instances i ON i.id=m.instance_id LEFT JOIN instance_model_activity a ON a.instance_id=i.id WHERE m.team_id=? AND m.status<>'deleted' AND (i.status<>'stopped' OR COALESCE(a.stop_operation_id,'')<>'' OR EXISTS(SELECT 1 FROM instance_runtime_bindings b WHERE b.instance_id=i.id))`, id).Scan(&unconfirmed)
			if e != nil {
				return e
			}
			if unconfirmed == 0 {
				for n := range members {
					members[n].ColdStopOperationID = v.OperationID
					members[n].ColdStoppedAt = v.StoppedAt
				}
			}
		}
		raw, e := json.Marshal(members)
		if e != nil {
			return e
		}
		phase := "starting"
		if target == "stopped" {
			phase = "stopping"
		}
		_, e = s.SQL().ExecContext(ctx, `UPDATE team_runtime_lifecycle SET state=?,desired_state=?,operation_id=?,reason=?,members_json=?,progress=0,attempts=0,error_message='',next_attempt_at=UTC_TIMESTAMP(6),updated_at=UTC_TIMESTAMP(6) WHERE team_id=?`, phase, target, uuid.NewString(), reason, string(raw), id)
		return e
	})
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}
func (r *TeamLifecycleRepository) Initialize(ctx context.Context) error {
	_, err := r.sess.SQL().ExecContext(ctx, `INSERT IGNORE INTO team_runtime_lifecycle(team_id,state,desired_state,reason)
 SELECT id,initial_state,initial_state,IF(initial_state='stopped','reconciled_stop','') FROM (
 SELECT t.id,IF(
 EXISTS(SELECT 1 FROM team_members m WHERE m.team_id=t.id AND m.status<>'deleted') AND
 NOT EXISTS(SELECT 1 FROM team_members m LEFT JOIN instances i ON i.id=m.instance_id WHERE m.team_id=t.id AND m.status<>'deleted' AND (i.id IS NULL OR i.status<>'stopped')) AND
 NOT EXISTS(SELECT 1 FROM team_members m JOIN instance_runtime_bindings b ON b.instance_id=m.instance_id WHERE m.team_id=t.id AND m.status<>'deleted') AND
 NOT EXISTS(SELECT 1 FROM team_members m JOIN instance_model_activity a ON a.instance_id=m.instance_id WHERE m.team_id=t.id AND a.stop_operation_id<>''),
 'stopped','running') AS initial_state FROM teams t WHERE t.status='running') observed`)
	return err
}
func (r *TeamLifecycleRepository) Candidates(ctx context.Context) ([]int, error) {
	return r.ids(ctx, `SELECT l.team_id FROM team_runtime_lifecycle l JOIN teams t ON t.id=l.team_id WHERE l.state='running' AND t.status='running' ORDER BY l.team_id`)
}
func (r *TeamLifecycleRepository) Pending(ctx context.Context) ([]int, error) {
	return r.ids(ctx, `SELECT team_id FROM team_runtime_lifecycle WHERE state IN ('starting','stopping') AND (next_attempt_at IS NULL OR next_attempt_at<=UTC_TIMESTAMP(6)) ORDER BY updated_at LIMIT 20`)
}
func (r *TeamLifecycleRepository) ids(ctx context.Context, q string) ([]int, error) {
	rows, err := r.sess.SQL().QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (r *TeamLifecycleRepository) Claim(ctx context.Context, op *models.TeamLifecycleOperation) (bool, error) {
	res, err := r.sess.SQL().ExecContext(ctx, `UPDATE team_runtime_lifecycle SET attempts=attempts+1,next_attempt_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 5 MINUTE) WHERE team_id=? AND operation_id=? AND state IN ('starting','stopping') AND (next_attempt_at IS NULL OR next_attempt_at<=UTC_TIMESTAMP(6))`, op.TeamID, op.OperationID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
func (r *TeamLifecycleRepository) Result(ctx context.Context, op *models.TeamLifecycleOperation, progress int, failure error) error {
	phase := op.DesiredState
	message := ""
	waiting := failure != nil && errors.Is(failure, models.ErrInstanceStopping) && op.DesiredState == "stopped" && op.Attempts < max(6, 3*len(op.Members))
	if failure != nil {
		phase = "recovery_required"
		message = failure.Error()
		if waiting {
			phase = "stopping"
			message = "正在确认成员停止，保留原团队操作"
		}
		if text := []rune(message); len(text) > 160 {
			message = string(text[:160])
		}
	}
	_, err := r.sess.SQL().ExecContext(ctx, `UPDATE team_runtime_lifecycle SET state=?,progress=?,error_message=?,next_attempt_at=IF(?,DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 5 SECOND),next_attempt_at),started_at=IF(?='running',UTC_TIMESTAMP(6),started_at),stopped_at=IF(?='stopped',UTC_TIMESTAMP(6),stopped_at),updated_at=UTC_TIMESTAMP(6) WHERE team_id=? AND operation_id=? AND state IN ('starting','stopping')`, phase, progress, message, waiting, phase, phase, op.TeamID, op.OperationID)
	return err
}
func (r *TeamLifecycleRepository) ValidateStopMember(ctx context.Context, team, id, generation int) error {
	v, err := r.Get(ctx, team)
	if err != nil {
		return err
	}
	if v == nil || v.State != "stopping" {
		return ErrTeamNotRunning
	}
	for _, m := range v.Members {
		if m.InstanceID == id && m.Generation == generation {
			return nil
		}
	}
	return fmt.Errorf("成员不属于本次团队停机快照")
}

type teamScanRow struct {
	row *sql.Row
	err error
}

func (r teamScanRow) Scan(values ...any) error {
	if r.err != nil {
		return r.err
	}
	return r.row.Scan(values...)
}
func teamQueryRow(ctx context.Context, s db.Session, q string, args ...any) teamScanRow {
	row, err := s.SQL().QueryRowContext(ctx, q, args...)
	return teamScanRow{row, err}
}

type teamStopAuthority struct {
	TeamID      int
	OperationID string
}
type teamStopAuthorityKey struct{}

func WithTeamStopAuthority(ctx context.Context, team int, operation string) context.Context {
	return context.WithValue(ctx, teamStopAuthorityKey{}, teamStopAuthority{team, operation})
}
func requireTeamStopAuthority(ctx context.Context, s db.Session, id, generation int) error {
	team, err := teamForInstance(ctx, s, id)
	if err != nil || team == 0 {
		return err
	}
	authority, ok := ctx.Value(teamStopAuthorityKey{}).(teamStopAuthority)
	if !ok || authority.TeamID != team {
		return fmt.Errorf("Team member requires a Team stop operation")
	}
	op, err := readTeamLifecycle(ctx, s, team, true)
	if err != nil {
		return err
	}
	if op == nil || op.State != "stopping" || op.OperationID != authority.OperationID {
		return ErrTeamNotRunning
	}
	for _, m := range op.Members {
		if m.InstanceID == id && m.Generation == generation {
			return nil
		}
	}
	return fmt.Errorf("Team stop member snapshot changed")
}

func (r *TeamLifecycleRepository) Progress(ctx context.Context, op *models.TeamLifecycleOperation, n int) error {
	_, err := r.sess.SQL().ExecContext(ctx, `UPDATE team_runtime_lifecycle SET progress=?,next_attempt_at=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 5 MINUTE),updated_at=UTC_TIMESTAMP(6) WHERE team_id=? AND operation_id=? AND state IN ('starting','stopping')`, n, op.TeamID, op.OperationID)
	return err
}
func (r *TeamLifecycleRepository) AbortUnstartedStop(ctx context.Context, op *models.TeamLifecycleOperation) (bool, error) {
	// Only safe before any member changed desired state or admitted a native stop.
	var n int
	err := teamQueryRow(ctx, r.sess, `SELECT COUNT(*) FROM team_members m JOIN instances i ON i.id=m.instance_id LEFT JOIN instance_model_activity a ON a.instance_id=i.id WHERE m.team_id=? AND m.status<>'deleted' AND i.status='running' AND COALESCE(a.stop_operation_id,'')=''`, op.TeamID).Scan(&n)
	if err != nil || n != len(op.Members) {
		return false, err
	}
	res, err := r.sess.SQL().ExecContext(ctx, `UPDATE team_runtime_lifecycle SET state='running',desired_state='running',error_message='',progress=0 WHERE team_id=? AND operation_id=? AND state='stopping'`, op.TeamID, op.OperationID)
	if err != nil {
		return false, err
	}
	n64, err := res.RowsAffected()
	return n64 == 1, err
}

// ProtectColdStart revokes legacy Team grants atomically while every original
// member is stopped. Sticky instance profiles forbid a later legacy placement.
// A partially started legacy Team must complete Stop before this transition.
func (r *TeamLifecycleRepository) ProtectColdStart(ctx context.Context, op *models.TeamLifecycleOperation, profile string) error {
	if profile != "isolated-network-v1" {
		return fmt.Errorf("unsupported Team runtime protection")
	}
	return r.sess.Tx(func(s db.Session) error {
		rows, err := s.SQL().QueryContext(ctx, `SELECT i.id,m.id,i.status,COALESCE(i.runtime_security_profile,''),COALESCE(i.instance_mode,''),COALESCE(i.type,''),COALESCE(b.gateway_id,'') FROM instances i JOIN team_members m ON m.instance_id=i.id LEFT JOIN instance_runtime_bindings b ON b.instance_id=i.id WHERE m.team_id=? AND m.status<>'deleted' ORDER BY i.id FOR UPDATE`, op.TeamID)
		if err != nil {
			return err
		}
		n := 0
		allCold, allProtected := true, true
		for rows.Next() {
			var id, member int
			var status, current, mode, kind, binding string
			if err = rows.Scan(&id, &member, &status, &current, &mode, &kind, &binding); err != nil {
				rows.Close()
				return err
			}
			match := false
			for _, m := range op.Members {
				if m.InstanceID == id && m.MemberID == member {
					match = true
					break
				}
			}
			if !match || mode != "lite" || (kind != "openclaw" && kind != "hermes" && kind != "opencode" && kind != "deepseek-harness") || (current != "" && current != profile) {
				rows.Close()
				return fmt.Errorf("Team protection member identity or mode changed")
			}
			allCold = allCold && status == "stopped" && binding == ""
			allProtected = allProtected && current == profile
			n++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if n == 0 || n != len(op.Members) {
			return fmt.Errorf("Team protection member set changed")
		}
		current, err := readTeamLifecycle(ctx, s, op.TeamID, true)
		if err != nil {
			return err
		}
		if current == nil || current.OperationID != op.OperationID || current.State != "starting" || current.DesiredState != "running" {
			return ErrTeamNotRunning
		}
		var preview string
		if err = teamQueryRow(ctx, s, `SELECT preview_security_profile FROM teams WHERE id=? AND status='running' FOR UPDATE`, op.TeamID).Scan(&preview); err != nil {
			return err
		}
		if preview == models.TeamPreviewSecurityProfile && allProtected {
			return nil
		}
		if preview != "" && preview != models.TeamPreviewSecurityProfile {
			return fmt.Errorf("unknown Team preview protection")
		}
		if !allCold {
			return fmt.Errorf("历史 Team 需先确认整队停止，再启用受保护的启动")
		}
		if err = teamHasOpenWork(ctx, s, op.TeamID); err != nil {
			return err
		}
		if err = teamMembersNoRequests(ctx, s, op.TeamID); err != nil {
			return err
		}
		if _, err = s.SQL().ExecContext(ctx, `UPDATE instances i JOIN team_members m ON m.instance_id=i.id SET i.runtime_security_profile=? WHERE m.team_id=? AND m.status<>'deleted'`, profile, op.TeamID); err != nil {
			return err
		}
		_, err = s.SQL().ExecContext(ctx, `UPDATE teams SET preview_security_profile=? WHERE id=?`, models.TeamPreviewSecurityProfile, op.TeamID)
		return err
	})
}

func (r *teamRepository) AuthorizeTeamMemberStartup(ctx context.Context, teamID, memberID, instanceID int) (bool, error) {
	op, err := NewTeamLifecycleRepository(r.sess).Get(ctx, teamID)
	if err != nil {
		return false, err
	}
	// An already-started member must retain its collaboration connection while
	// another frozen member is recovering, and during the online projection handoff.
	// This is only lease admission: task/model admission still requires the whole
	// Team to be running, and the security authority checks the current generation,
	// assignment and Pod independently. A stop intent revokes admission immediately.
	if op == nil || op.DesiredState != "running" {
		return false, nil
	}
	switch op.State {
	case "starting", "running", "recovery_required":
	default:
		return false, nil
	}
	for _, m := range op.Members {
		if m.MemberID == memberID && m.InstanceID == instanceID {
			return true, ctx.Err()
		}
	}
	return false, nil
}
