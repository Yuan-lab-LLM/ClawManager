package systembackup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Attempt is written before an external Job can be created. Its identity and
// frozen request survive leader replacement; a replacement adopts that Job.
type Attempt struct {
	ID      string          `json:"public_id"`
	TaskID  string          `json:"task_id"`
	Phase   string          `json:"phase"`
	Number  uint64          `json:"attempt_number"`
	Owner   string          `json:"-"`
	Fence   uint64          `json:"-"`
	Status  string          `json:"status"`
	Request json.RawMessage `json:"request"`
	Result  json.RawMessage `json:"result,omitempty"`
}

func scanAttempt(r rowScanner) (Attempt, error) {
	var a Attempt
	var result sql.NullString
	e := r.Scan(&a.ID, &a.TaskID, &a.Phase, &a.Number, &a.Owner, &a.Fence, &a.Status, &a.Request, &result)
	if result.Valid {
		a.Result = json.RawMessage(result.String)
	}
	return a, e
}

const attemptColumns = "public_id,task_id,phase,attempt_number,controller_owner,fencing_token,status,request_json,result_json"

// PrepareAttempt commits the exact request before the first network mutation.
// Only one attempt per phase is currently supported: retries must reconcile
// the existing object, never create a second writer after an unknown response.
func (s Store) PrepareAttempt(ctx context.Context, l Leadership, t Task, phase string, request any) (Attempt, error) {
	var zero Attempt
	allowed := map[TaskType]map[string]bool{Backup: {"capture": true, "secret_capture": true, "upload": true, "publish": true, "verify": true}, Drill: {"restore": true, "normalize": true, "verify": true}, Preflight: {"preflight": true}, ArtifactVerify: {"verify": true}}
	if !allowed[t.Type][phase] {
		return zero, errors.New("validation_failed")
	}
	raw, e := json.Marshal(request)
	if e != nil || len(raw) > 256<<10 {
		return zero, errors.New("validation_failed")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return zero, e
	}
	defer tx.Rollback()
	if e = s.lockLeadership(ctx, tx, l); e != nil {
		return zero, e
	}
	actual, e := scanTask(tx.QueryRowContext(ctx, "SELECT "+taskColumns+" FROM system_backup_tasks WHERE public_id=? FOR UPDATE", t.ID))
	if e != nil {
		return zero, e
	}
	var now time.Time
	if e = tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now); e != nil {
		return zero, e
	}
	if actual.Version != t.Version || actual.LeaseOwner != l.Owner || actual.LeaseUntil == nil || !actual.LeaseUntil.After(now) || actual.Terminal() || actual.Status == "canceling" || !actual.DeadlineAt.After(now) {
		return zero, ErrFence
	}
	a, e := scanAttempt(tx.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM system_backup_attempts WHERE task_id=? AND phase=? AND attempt_number=1 FOR UPDATE", t.ID, phase))
	if e == nil {
		if string(a.Request) != string(raw) {
			return zero, errors.New("artifact_conflict")
		}
		return a, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return zero, e
	}
	id, e := RandomID("sat_")
	if e != nil {
		return zero, e
	}
	a = Attempt{ID: id, TaskID: t.ID, Phase: phase, Number: 1, Owner: l.Owner, Fence: l.Fence, Status: "running", Request: raw}
	if _, e = tx.ExecContext(ctx, "INSERT INTO system_backup_attempts(public_id,task_id,phase,attempt_number,controller_owner,fencing_token,status,request_json) VALUES(?,?,?,?,?,?,'running',?)", id, t.ID, phase, 1, l.Owner, l.Fence, raw); e != nil {
		return zero, e
	}
	if e = appendEvent(ctx, tx, t.ID, "attempt_prepared", map[string]string{"attempt_id": id, "phase": phase}); e != nil {
		return zero, e
	}
	return a, tx.Commit()
}

func (s Store) GetAttempt(ctx context.Context, taskID, phase string) (Attempt, error) {
	return scanAttempt(s.DB.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM system_backup_attempts WHERE task_id=? AND phase=? AND attempt_number=1", taskID, phase))
}

// FinishAttempt requires the CURRENT leader and live task lease. Original
// attempt ownership is historical evidence, not permission for a stale worker.
func (s Store) FinishAttempt(ctx context.Context, l Leadership, t Task, a Attempt, status string, result any) error {
	if status != "succeeded" && status != "failed" && status != "abandoned" && status != "sealed" {
		return errors.New("validation_failed")
	}
	if status == "sealed" && (t.Type != Backup || a.Phase != "capture" && a.Phase != "secret_capture") {
		return errors.New("validation_failed")
	}
	raw, e := json.Marshal(result)
	if e != nil || len(raw) > 8192 {
		return errors.New("validation_failed")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = s.lockLeadership(ctx, tx, l); e != nil {
		return e
	}
	var valid bool
	if e = tx.QueryRowContext(ctx, "SELECT row_version=? AND lease_owner=? AND lease_until>UTC_TIMESTAMP(6) FROM system_backup_tasks WHERE public_id=? FOR UPDATE", t.Version, l.Owner, t.ID).Scan(&valid); e != nil {
		return e
	}
	if !valid {
		return ErrFence
	}
	old, e := scanAttempt(tx.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM system_backup_attempts WHERE public_id=? AND task_id=? FOR UPDATE", a.ID, t.ID))
	if e != nil {
		return e
	}
	if old.Status != "running" {
		if old.Status == status && string(old.Result) == string(raw) {
			return tx.Commit()
		}
		return errors.New("state_conflict")
	}
	if _, e = tx.ExecContext(ctx, "UPDATE system_backup_attempts SET status=?,result_json=?,finished_at=UTC_TIMESTAMP(6) WHERE public_id=?", status, raw, a.ID); e != nil {
		return e
	}
	if e = appendEvent(ctx, tx, t.ID, "attempt_finished", map[string]string{"attempt_id": a.ID, "status": status}); e != nil {
		return e
	}
	return tx.Commit()
}
