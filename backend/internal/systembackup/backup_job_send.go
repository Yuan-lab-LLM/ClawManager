package systembackup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
)

var errBackupExecutionUnknown = errors.New("external_result_unknown")

func unknownBackupExecution(err error) error {
	return fmt.Errorf("%w: %s", errBackupExecutionUnknown, PublicError(err))
}

func (s Store) awaitOriginalBackupExecution(ctx context.Context, l Leadership, t Task) error {
	tx, x, err := s.beginCaptureMutation(ctx, l, t)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if x.AttentionReason == "external_result_unknown" {
		return tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, "UPDATE system_backup_tasks SET attention_reason='external_result_unknown',row_version=row_version+1 WHERE public_id=?", x.ID); err != nil {
		return err
	}
	if err = appendEvent(ctx, tx, x.ID, "diagnostic.job.awaiting_original", map[string]string{"reason": "external_result_unknown"}); err != nil {
		return err
	}
	return tx.Commit()
}

func backupSendState(phase, status string) bool {
	switch phase {
	case "capture", "secret_capture":
		return status == "preparing"
	case "upload":
		return status == "validating"
	case "verify":
		return status == "publishing" || status == "finalization_unknown"
	}
	return false
}

func (s Store) reserveBackupJobSend(ctx context.Context, l Leadership, t Task, a Attempt) (bool, error) {
	tx, x, err := s.beginCaptureMutation(ctx, l, t)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	stored, err := scanAttempt(tx.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM system_backup_attempts WHERE public_id=? AND task_id=? FOR UPDATE", a.ID, x.ID))
	if err != nil {
		return false, err
	}
	var live bool
	if err = tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)<? AND NOT EXISTS(SELECT 1 FROM system_backup_capture_aborts WHERE task_id=?)", x.DeadlineAt, x.ID).Scan(&live); err != nil {
		return false, err
	}
	if stored.Status != "running" || stored.Phase != a.Phase || string(stored.Request) != string(a.Request) || !backupSendState(a.Phase, x.Status) || !live {
		return false, errors.New("state_conflict")
	}
	hash, err := RequestHash(json.RawMessage(a.Request))
	if err != nil {
		return false, err
	}
	r, err := tx.ExecContext(ctx, "INSERT IGNORE INTO system_backup_job_sends(attempt_id,task_id,request_hash) VALUES(?,?,?)", a.ID, x.ID, hash)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return false, err
	}
	var oldHash sql.NullString
	var taskID string
	if err = tx.QueryRowContext(ctx, "SELECT task_id,request_hash FROM system_backup_job_sends WHERE attempt_id=?", a.ID).Scan(&taskID, &oldHash); err != nil {
		return false, err
	}
	if taskID != x.ID || oldHash.Valid && oldHash.String != hash {
		return false, errors.New("artifact_conflict")
	}
	if n == 1 {
		if err = appendEvent(ctx, tx, x.ID, "diagnostic.job.create_sent", map[string]string{"attempt_id": a.ID, "phase": a.Phase}); err != nil {
			return false, err
		}
	}
	return n == 1, tx.Commit()
}

func (s Store) backupJobWasSent(ctx context.Context, a Attempt) (bool, error) {
	var sent bool
	err := s.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM system_backup_job_sends WHERE attempt_id=? AND task_id=?)", a.ID, a.TaskID).Scan(&sent)
	return sent, err
}

// Only a newly committed send intent authorizes Create. A replacement process
// can adopt the exact existing Job, but missing/unknown never authorizes reread.
func (s Store) ensureBackupExecution(ctx context.Context, l Leadership, t Task, a Attempt, create func(context.Context) (*batchv1.Job, error), existing func(context.Context) (*batchv1.Job, error), bind func(context.Context, *batchv1.Job) error) (CaptureJobBinding, error) {
	b, err := s.CaptureBinding(ctx, a.ID)
	if err == nil {
		return b, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return b, err
	}
	fresh, err := s.reserveBackupJobSend(ctx, l, t, a)
	if err != nil {
		return b, err
	}
	var job *batchv1.Job
	if fresh {
		tx, x, e := s.beginCaptureMutation(ctx, l, t)
		if e != nil {
			return b, e
		}
		defer tx.Rollback()
		var live bool
		e = tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)<? AND NOT EXISTS(SELECT 1 FROM system_backup_capture_aborts WHERE task_id=?)", x.DeadlineAt, x.ID).Scan(&live)
		if e != nil {
			return b, e
		}
		if !live || !backupSendState(a.Phase, x.Status) {
			return b, ErrFence
		}
		step, cancel := context.WithTimeout(ctx, 5*time.Second)
		job, err = create(step)
		cancel()
		if err != nil {
			return b, unknownBackupExecution(err)
		}
		if err = tx.Commit(); err != nil {
			return b, err
		}
	} else {
		job, err = existing(ctx)
		if err != nil {
			return b, unknownBackupExecution(err)
		}
	}
	if err = bind(ctx, job); err != nil {
		return b, err
	}
	return s.CaptureBinding(ctx, a.ID)
}
