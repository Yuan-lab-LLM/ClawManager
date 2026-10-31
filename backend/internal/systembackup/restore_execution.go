package systembackup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DataExecution struct {
	AttemptID, TaskID, RequestHash, JobUID string
	CreateSent                             bool
	StoppedAt                              *time.Time
}

func (s Store) DataExecution(ctx context.Context, id string) (DataExecution, error) {
	var out DataExecution
	var uid sql.NullString
	var stopped sql.NullTime
	err := s.DB.QueryRowContext(ctx, "SELECT attempt_id,task_id,request_hash,create_sent,job_uid,stopped_at FROM system_backup_restore_jobs WHERE attempt_id=?", id).Scan(&out.AttemptID, &out.TaskID, &out.RequestHash, &out.CreateSent, &uid, &stopped)
	out.JobUID = uid.String
	if stopped.Valid {
		out.StoppedAt = &stopped.Time
	}
	return out, err
}

func (s Store) beginDrillMutation(ctx context.Context, l Leadership, t Task) (*sql.Tx, Task, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, t, err
	}
	fail := func(err error) (*sql.Tx, Task, error) { tx.Rollback(); return nil, t, err }
	if err = s.lockLeadership(ctx, tx, l); err != nil {
		return fail(err)
	}
	x, err := scanTask(tx.QueryRowContext(ctx, "SELECT "+taskColumns+" FROM system_backup_tasks WHERE public_id=? AND origin_installation_id=? FOR UPDATE", t.ID, s.InstallationID))
	if err != nil {
		return fail(err)
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now); err != nil {
		return fail(err)
	}
	if x.Type != Drill || x.Terminal() || x.Version != t.Version || x.LeaseOwner != l.Owner || x.LeaseUntil == nil || !x.LeaseUntil.After(now) {
		return fail(ErrFence)
	}
	return tx, x, nil
}

func (s Store) mutateDataExecution(ctx context.Context, l Leadership, t Task, a Attempt, action, uid string) error {
	_, r, err := dataJob(a)
	if err != nil {
		return err
	}
	tx, x, err := s.beginDrillMutation(ctx, l, t)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stored, err := scanAttempt(tx.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM system_backup_attempts WHERE public_id=? AND task_id=? FOR UPDATE", a.ID, x.ID))
	if err != nil {
		return err
	}
	if string(stored.Request) != string(a.Request) || stored.Phase != a.Phase || r.Binding.BackupID != x.SourceID || r.Binding.Origin != s.InstallationID || !r.DeadlineAt.Equal(x.DeadlineAt) {
		return errors.New("artifact_conflict")
	}
	var valid bool
	if err = tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)<?", x.DeadlineAt).Scan(&valid); err != nil {
		return err
	}
	expected := "restoring"
	if a.Phase == "verify" {
		expected = "verifying"
	}
	hash, err := RequestHash(json.RawMessage(a.Request))
	if err != nil {
		return err
	}
	if action == "prepare" {
		if x.Status != expected || stored.Status != "running" || !valid {
			return errors.New("state_conflict")
		}
		_, err = tx.ExecContext(ctx, "INSERT IGNORE INTO system_backup_restore_jobs(attempt_id,task_id,request_hash) VALUES(?,?,?)", a.ID, x.ID, hash)
		if err != nil {
			return err
		}
	}
	var actualHash string
	var sent bool
	var oldUID sql.NullString
	var stopped sql.NullTime
	if err = tx.QueryRowContext(ctx, "SELECT request_hash,create_sent,job_uid,stopped_at FROM system_backup_restore_jobs WHERE attempt_id=? AND task_id=? FOR UPDATE", a.ID, x.ID).Scan(&actualHash, &sent, &oldUID, &stopped); err != nil {
		return err
	}
	if actualHash != hash {
		return errors.New("artifact_conflict")
	}
	switch action {
	case "prepare":
	case "send":
		if x.Status != expected || stored.Status != "running" || !valid || sent || oldUID.Valid || stopped.Valid {
			return errors.New("state_conflict")
		}
		_, err = tx.ExecContext(ctx, "UPDATE system_backup_restore_jobs SET create_sent=1 WHERE attempt_id=?", a.ID)
	case "bind":
		if !sent || uid == "" || oldUID.Valid && oldUID.String != uid || stopped.Valid {
			return errors.New("artifact_conflict")
		}
		_, err = tx.ExecContext(ctx, "UPDATE system_backup_restore_jobs SET job_uid=? WHERE attempt_id=?", uid, a.ID)
	case "stopped":
		if uid == "" || !oldUID.Valid || oldUID.String != uid {
			return errors.New("cleanup_unsafe_target")
		}
		_, err = tx.ExecContext(ctx, "UPDATE system_backup_restore_jobs SET stopped_at=COALESCE(stopped_at,UTC_TIMESTAMP(6)) WHERE attempt_id=?", a.ID)
	default:
		return errors.New("validation_failed")
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (j DataJobs) existing(ctx context.Context, a Attempt) (*batchv1.Job, error) {
	want, r, err := dataJob(a)
	if err != nil {
		return nil, err
	}
	if err = (VerifyJobs{Client: j.Client}).namespace(ctx, r.VerifyJobRequest); err != nil {
		return nil, err
	}
	job, err := j.Client.BatchV1().Jobs(r.Namespace).Get(ctx, want.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if job.UID == "" || !matchesCaptureJob(want, job) {
		return nil, errors.New("artifact_conflict")
	}
	return job, nil
}

// EnsureExecution commits its send intent before external creation. After an
// unknown create result only the exact existing Job may be adopted. Absence is
// not permission to start another restore into a potentially populated target.
func (j DataJobs) EnsureExecution(ctx context.Context, s Store, l Leadership, t Task, a Attempt) (DataExecution, error) {
	var zero DataExecution
	b, err := s.DataExecution(ctx, a.ID)
	if errors.Is(err, sql.ErrNoRows) {
		if err = s.mutateDataExecution(ctx, l, t, a, "prepare", ""); err != nil {
			return zero, err
		}
		b, err = s.DataExecution(ctx, a.ID)
	}
	if err != nil {
		return b, err
	}
	if b.StoppedAt != nil {
		return b, errors.New("state_conflict")
	}
	if b.JobUID != "" {
		_, err = j.Ensure(ctx, a, b.JobUID, false)
		return b, err
	}
	var job *batchv1.Job
	if !b.CreateSent {
		if err = s.mutateDataExecution(ctx, l, t, a, "send", ""); err != nil {
			return b, err
		}
		// Recheck the live leader/task fence around this one bounded mutation.
		tx, x, e := s.beginDrillMutation(ctx, l, t)
		if e != nil {
			return b, e
		}
		defer tx.Rollback()
		var live bool
		e = tx.QueryRowContext(ctx, "SELECT status IN ('restoring','verifying') AND UTC_TIMESTAMP(6)<deadline_at FROM system_backup_tasks WHERE public_id=?", x.ID).Scan(&live)
		if e != nil {
			return b, e
		}
		if !live {
			return b, ErrFence
		}
		step, cancel := context.WithTimeout(ctx, 5*time.Second)
		job, err = j.Ensure(step, a, "", true)
		cancel()
		if err != nil {
			return b, err
		}
		if err = tx.Commit(); err != nil {
			return b, err
		}
	} else {
		job, err = j.existing(ctx, a)
		if err != nil {
			return b, err
		}
	}
	want, _, err := dataJob(a)
	if err != nil || !matchesCaptureJob(want, job) {
		return b, errors.New("artifact_conflict")
	}
	if err = s.mutateDataExecution(ctx, l, t, a, "bind", string(job.UID)); err != nil {
		return b, err
	}
	return s.DataExecution(ctx, a.ID)
}

func (j DataJobs) StopExecution(ctx context.Context, s Store, l Leadership, t Task, a Attempt) (bool, error) {
	b, err := s.DataExecution(ctx, a.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if b.StoppedAt != nil {
		return true, nil
	}
	if b.JobUID == "" {
		if !b.CreateSent {
			return true, nil
		}
		job, e := j.existing(ctx, a)
		if e != nil {
			return false, e
		}
		if err = s.mutateDataExecution(ctx, l, t, a, "bind", string(job.UID)); err != nil {
			return false, err
		}
		b.JobUID = string(job.UID)
	}
	tx, _, err := s.beginDrillMutation(ctx, l, t)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	step, cancel := context.WithTimeout(ctx, 5*time.Second)
	stopped, err := j.Stop(step, a, b.JobUID)
	cancel()
	if err != nil || !stopped {
		return stopped, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	if err = s.mutateDataExecution(ctx, l, t, a, "stopped", b.JobUID); err != nil {
		return false, err
	}
	return true, nil
}

// RestoreIntentFromExecution reads only immutable controller evidence from the
// original completed, stopped Job. A caller must still revoke/confirm all
// restore write credentials before launching the independent verifier.
func (s Store) RestoreIntentFromExecution(ctx context.Context, runID string) (RestoreVerificationIntent, string, error) {
	var zero RestoreVerificationIntent
	a, err := s.GetAttempt(ctx, runID, "restore")
	if err != nil {
		return zero, "", err
	}
	b, err := s.DataExecution(ctx, a.ID)
	if err != nil {
		return zero, "", err
	}
	_, r, err := dataJob(a)
	if err != nil {
		return zero, "", err
	}
	var result DataJobResult
	if r.Binding.Origin != s.InstallationID || b.TaskID != runID || a.Status != "succeeded" || b.StoppedAt == nil || b.JobUID == "" || strictDecode(a.Result, &result) != nil || result.JobUID != b.JobUID || result.State != "succeeded" || result.Report == nil || result.Report.validateBinding(r.Binding) != nil {
		return zero, "", errors.New("evidence_incomplete")
	}
	proof := result.Report
	return RestoreVerificationIntent{RunID: runID, NormalizedAt: proof.NormalizedAt, HistoryParts: proof.HistoryParts}, proof.SQLSHA256, nil
}
