package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// OwnerTeamRepository is the owner-scoped projection and creation ledger. It
// does not replace the Team execution/event repository.
type OwnerTeamRepository struct{ sess db.Session }

func ieiSessionDigest(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}
func (r *OwnerTeamRepository) IsIEISessionRevoked(id string) (bool, error) {
	n, err := r.sess.Collection("iei_session_revocations").Find(db.Cond{"session_hash": ieiSessionDigest(id)}).Count()
	return n > 0, err
}
func (r *OwnerTeamRepository) RevokeIEISession(id string, expires time.Time) error {
	_, err := r.sess.SQL().Exec(`INSERT INTO iei_session_revocations(session_hash,expires_at) VALUES(?,?) ON DUPLICATE KEY UPDATE expires_at=GREATEST(expires_at,VALUES(expires_at))`, ieiSessionDigest(id), expires)
	return err
}

var ErrOwnerTeamQueueFull = errors.New("owner Team operation queue is full")

func NewOwnerTeamRepository(sess db.Session) *OwnerTeamRepository {
	return &OwnerTeamRepository{sess: sess}
}

// Lock a separate admission row, not the parent Team row: task inserts through
// the execution repository must still obtain their parent FK lock. Keep this
// transaction until the action returns; client cancellation is not writer exit.
func (r *OwnerTeamRepository) WithAdmission(teamID int, action func() error) error {
	if _, err := r.sess.SQL().Exec(`INSERT IGNORE INTO owner_team_admission_locks(team_id) VALUES(?)`, teamID); err != nil {
		return err
	}
	return r.sess.TxContext(context.Background(), func(tx db.Session) error {
		row, err := tx.SQL().QueryRow(`SELECT team_id FROM owner_team_admission_locks WHERE team_id=? FOR UPDATE`, teamID)
		if err != nil {
			return err
		}
		var locked int
		if err := row.Scan(&locked); err != nil {
			return err
		}
		return action()
	}, nil)
}

func (r *OwnerTeamRepository) PendingMutation(teamID int) (bool, error) {
	n, err := r.sess.Collection("northbound_operations").Find(db.Cond{"team_id": teamID, "status IN": []string{"queued", "processing"}, "operation_type IN": []string{"owner_team_teams_delete", "owner_team_members_delete"}}).Count()
	return n > 0, err
}

func ownerTeamCondition(userID int, owner string) (db.Cond, error) {
	normalized, err := models.NormalizeTeamOwner(owner)
	if err != nil {
		return nil, err
	}
	cond := db.Cond{"resource_scope": models.TeamScopeNorthbound, "owner": normalized, "status !=": models.TeamStatusDeleted}
	if userID > 0 {
		cond["user_id"] = userID
	}
	return cond, nil
}

func (r *OwnerTeamRepository) Get(userID int, owner string, id int) (*models.Team, error) {
	cond, err := ownerTeamCondition(userID, owner)
	if err != nil {
		return nil, err
	}
	cond["id"] = id
	var team models.Team
	if err := r.sess.Collection("teams").Find(cond).One(&team); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, err
	}
	if err := team.ValidateScope(); err != nil {
		return nil, err
	}
	return &team, nil
}

func (r *OwnerTeamRepository) List(userID int, owner, query string, offset, limit int) ([]models.Team, int, error) {
	cond, err := ownerTeamCondition(userID, owner)
	if err != nil {
		return nil, 0, err
	}
	if q := strings.TrimSpace(query); q != "" {
		cond["display_name LIKE"] = "%" + strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(q, "\\", "\\\\"), "%", "\\%"), "_", "\\_") + "%"
	}
	if offset < 0 {
		offset = 0
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	set := r.sess.Collection("teams").Find(cond)
	count, err := set.Count()
	if err != nil {
		return nil, 0, err
	}
	items := []models.Team{}
	err = set.OrderBy("-id").Offset(offset).Limit(limit).All(&items)
	return items, int(count), err
}

// ReserveCreate atomically publishes the owner, team ID and replayable operation
// before any PVC, secret, member instance or model call is made.
func (r *OwnerTeamRepository) ReserveCreate(ctx context.Context, team *models.Team, operation *models.NorthboundOperation, pendingLimit ...int) error {
	if team.ResourceScope != models.TeamScopeNorthbound {
		return fmt.Errorf("not an Owner Team")
	}
	if err := team.ValidateScope(); err != nil {
		return err
	}
	return r.sess.TxContext(ctx, func(tx db.Session) error {
		limit := 0
		if len(pendingLimit) > 0 {
			limit = pendingLimit[0]
		}
		if err := reserveOwnerTeamCapacity(tx, operation.UserID, limit); err != nil {
			return err
		}
		if err := NewTeamRepository(tx).CreateTeam(team); err != nil {
			return err
		}
		operation.TeamID = &team.ID
		_, err := tx.Collection("northbound_operations").Insert(operation)
		return err
	}, nil)
}

func reserveOwnerTeamCapacity(tx db.Session, userID, limit int) error {
	row, err := tx.SQL().QueryRow(`SELECT id FROM users WHERE id=? FOR UPDATE`, userID)
	if err != nil {
		return err
	}
	var uid int
	if err := row.Scan(&uid); err != nil {
		return err
	}
	if limit > 0 {
		n, err := tx.Collection("northbound_operations").Find(db.Cond{"user_id": userID, "status IN": []string{"queued", "processing"}}).Count()
		if err != nil {
			return err
		}
		if int(n) >= limit {
			return ErrOwnerTeamQueueFull
		}
	}
	return nil
}

func (r *OwnerTeamRepository) ReserveOperation(ctx context.Context, operation *models.NorthboundOperation, limit int) error {
	return r.sess.TxContext(ctx, func(tx db.Session) error {
		if err := reserveOwnerTeamCapacity(tx, operation.UserID, limit); err != nil {
			return err
		}
		_, err := tx.Collection("northbound_operations").Insert(operation)
		return err
	}, nil)
}

func (r *OwnerTeamRepository) RequestOperation(key string) (*models.NorthboundOperation, error) {
	var op models.NorthboundOperation
	err := r.sess.Collection("northbound_operations").Find(db.Cond{"owner_request_key": key}).One(&op)
	if err == db.ErrNoMoreRows {
		return nil, nil
	}
	return &op, err
}

func (r *OwnerTeamRepository) UpdateDisplay(team *models.Team, name string, description *string) error {
	if err := team.ValidateScope(); err != nil || team.IsCM() {
		return fmt.Errorf("invalid Owner Team")
	}
	return r.sess.Collection("teams").Find(db.Cond{"id": team.ID, "user_id": team.UserID, "resource_scope": models.TeamScopeNorthbound, "owner": *team.Owner}).Update(map[string]interface{}{"display_name": name, "description": description, "updated_at": time.Now().UTC()})
}

// StartStep is intentionally fail-closed after an ambiguous side effect. A
// lease expiry alone is not proof the previous provisioner stopped writing.
func (r *OwnerTeamRepository) StartStep(teamID int, key, operationID string) (bool, error) {
	result, err := r.sess.SQL().Exec(`INSERT IGNORE INTO owner_team_provision_steps(team_id,step_key,operation_id,status) VALUES(?,?,?,'started')`, teamID, key, operationID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 1 {
		return true, nil
	}
	var step struct {
		Status      string `db:"status"`
		OperationID string `db:"operation_id"`
	}
	err = r.sess.Collection("owner_team_provision_steps").Find(db.Cond{"team_id": teamID, "step_key": key}).One(&step)
	if err != nil {
		return false, err
	}
	if step.OperationID != operationID || step.Status != "done" {
		if step.OperationID == operationID && step.Status == "failed" {
			result, err := r.sess.SQL().Exec(`UPDATE owner_team_provision_steps SET status='started' WHERE team_id=? AND step_key=? AND operation_id=? AND status='failed'`, teamID, key, operationID)
			if err != nil {
				return false, err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return false, err
			}
			if n == 1 {
				return true, nil
			}
		}
		return false, fmt.Errorf("OWNER_TEAM_STEP_UNCERTAIN: verify existing %s before retrying", key)
	}
	return false, nil
}

// Only the provisioner whose synchronous side effect has returned may mark a
// step failed. An expired lease/HTTP timeout must leave it started/uncertain.
func (r *OwnerTeamRepository) FailStep(teamID int, key, operationID string) error {
	_, err := r.sess.SQL().Exec(`UPDATE owner_team_provision_steps SET status='failed' WHERE team_id=? AND step_key=? AND operation_id=? AND status='started'`, teamID, key, operationID)
	return err
}

func (r *OwnerTeamRepository) RetryOperation(op *models.NorthboundOperation, expectedAttempt int) (bool, error) {
	result, err := r.sess.SQL().Exec(`UPDATE northbound_operations SET status='queued',available_at=?,started_at=NULL,finished_at=NULL,error_code=NULL,error_message=NULL WHERE operation_id=? AND owner=? AND status='failed' AND attempt_count=? AND lease_owner IS NULL`, time.Now().UTC(), op.OperationID, op.Owner, expectedAttempt)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (r *OwnerTeamRepository) ProvisionStepsDone(teamID int, operationID string, keys []string) (bool, error) {
	count, err := r.sess.Collection("owner_team_provision_steps").Find(db.Cond{"team_id": teamID, "operation_id": operationID, "step_key IN": keys, "status": "done"}).Count()
	return count == uint64(len(keys)), err
}

// Requeue an observation, retaining its original deadline and attempt count.
// CAS prevents a stale observer from completing or requeueing another lease.
func (r *NorthboundRepository) DeferOwnerTeamReadiness(ctx context.Context, op *models.NorthboundOperation, message string) error {
	now := time.Now().UTC()
	result, err := r.sess.SQL().ExecContext(ctx, `UPDATE northbound_operations SET status='queued',available_at=?,error_code='OWNER_TEAM_RUNTIME_WAITING',error_message=?,updated_at=?,lease_owner=NULL,lease_expires_at=NULL WHERE operation_id=? AND status='processing' AND lease_owner=?`, now.Add(15*time.Second), message, now, op.OperationID, op.LeaseOwner)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return fmt.Errorf("operation lease lost")
	}
	return err
}

func (r *OwnerTeamRepository) CompleteStep(teamID int, key, operationID string) error {
	result, err := r.sess.SQL().Exec(`UPDATE owner_team_provision_steps SET status='done' WHERE team_id=? AND step_key=? AND operation_id=? AND status IN ('started','done')`, teamID, key, operationID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	count, err := r.sess.Collection("owner_team_provision_steps").Find(db.Cond{"team_id": teamID, "step_key": key, "operation_id": operationID, "status": "done"}).Count()
	if err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("Owner Team step completion ownership changed")
	}
	return nil
}

func (r *OwnerTeamRepository) Members(teamID int) ([]models.TeamMember, error) {
	return NewTeamRepository(r.sess).ListMembersByTeamID(teamID)
}

func (r *OwnerTeamRepository) MemberGroups(teams []models.Team) (map[int][]models.TeamMember, error) {
	groups := map[int][]models.TeamMember{}
	if len(teams) == 0 {
		return groups, nil
	}
	ids := make([]int, 0, len(teams))
	for _, team := range teams {
		ids = append(ids, team.ID)
	}
	var members []models.TeamMember
	if err := r.sess.Collection("team_members").Find(db.Cond{"team_id IN": ids, "status !=": models.TeamMemberStatusDeleted}).OrderBy("id").All(&members); err != nil {
		return nil, err
	}
	for _, member := range members {
		groups[member.TeamID] = append(groups[member.TeamID], member)
	}
	return groups, nil
}

func (r *OwnerTeamRepository) InstanceTeam(instanceID int) (*models.Team, error) {
	rows, err := r.sess.SQL().Query(`SELECT DISTINCT t.id FROM team_members m JOIN teams t ON t.id=m.team_id JOIN instances i ON i.id=? WHERE t.resource_scope='northbound' AND m.status<>'deleted' AND t.status<>'deleted' AND (m.instance_id=i.id OR i.provisioning_operation_id=SHA2(CONCAT('owner-team-',t.id,'-member-',m.member_key),256)) LIMIT 2`, instanceID)
	if err != nil {
		return nil, err
	}
	var teamID int
	found := 0
	for rows.Next() {
		if err := rows.Scan(&teamID); err != nil {
			rows.Close()
			return nil, err
		}
		found++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if found == 0 {
		return nil, nil
	}
	if found > 1 {
		return nil, fmt.Errorf("ambiguous instance Team identity")
	}
	team, err := NewTeamRepository(r.sess).GetTeamByID(teamID)
	if err != nil || team == nil || team.IsCM() {
		return nil, err
	}
	return team, team.ValidateScope()
}

func (r *OwnerTeamRepository) HasActiveTasks(teamID int) (bool, error) {
	n, err := r.sess.Collection("team_tasks").Find(db.Cond{"team_id": teamID, "status IN": []string{"pending", "dispatched", "running", "waiting", "stale"}}).Count()
	return n > 0, err
}

func (r *NorthboundRepository) FinishOwnerTeamOperation(ctx context.Context, op *models.NorthboundOperation, templateID *int, cause error) error {
	status, code, message := "succeeded", "", ""
	if cause != nil {
		status, code, message = "failed", "OWNER_TEAM_OPERATION_FAILED", "操作未完成；已保留现有资源，请查看操作记录"
		text := strings.ToLower(cause.Error())
		switch {
		case strings.Contains(text, "runtime readiness timed out"):
			code, message = "OWNER_TEAM_RUNTIME_TIMEOUT", "成员启动验证超时；现有实例和数据已保留，解决运行异常后可重试原操作"
		case strings.Contains(text, "owner_team_step_uncertain"):
			code, message = "OWNER_TEAM_STEP_UNCERTAIN", "上次资源创建结果未完全确认；现有数据已保留，禁止重复创建，请由管理员核对该步骤"
		case strings.Contains(text, "quota") || strings.Contains(text, "limit reached"):
			code, message = "OWNER_TEAM_QUOTA_EXCEEDED", "后台账号可用配额不足；调整配额后可重试原操作，已创建资源保留"
		case strings.Contains(text, "template_result_uncertain"):
			code, message = "TEMPLATE_RESULT_UNCERTAIN", "模型生成结果未确认，系统未重复扣费；请先检查模板，再决定是否重新生成"
		case strings.Contains(text, "revision conflict"):
			code, message = "TEMPLATE_REVISION_CONFLICT", "模板已被其他操作修改，请重新载入；现有版本未覆盖"
		case strings.Contains(text, "ownership"):
			code, message = "OWNER_TEAM_OWNERSHIP_INVALID", "资源归属核验未通过，已停止操作并保留资源，请联系管理员"
		}
	}
	result, err := r.sess.SQL().ExecContext(ctx, `UPDATE northbound_operations SET status=?,template_id=COALESCE(?,template_id),error_code=NULLIF(?,''),error_message=NULLIF(?,''),finished_at=?,updated_at=?,lease_owner=NULL,lease_expires_at=NULL WHERE operation_id=? AND status='processing' AND lease_owner=?`, status, templateID, code, message, time.Now().UTC(), time.Now().UTC(), op.OperationID, op.LeaseOwner)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return fmt.Errorf("operation lease lost")
	}
	return err
}
