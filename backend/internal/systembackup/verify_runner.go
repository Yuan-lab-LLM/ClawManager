package systembackup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type VerifyJobDriver interface {
	Observe(context.Context, Attempt) (VerifyJobResult, error)
	Stop(context.Context, Attempt) (bool, error)
}
type VerifyTaskRunner struct {
	Jobs     VerifyJobDriver
	Template VerifyJobRequest
}

func (r VerifyTaskRunner) Tick(ctx context.Context, s Store, l Leadership, t Task, c ConfigVersion) error {
	if r.Jobs == nil || t.Type != ArtifactVerify {
		return errors.New("validation_failed")
	}
	if t.Status == "pending" {
		if !c.Config.Enabled {
			return s.SetPendingReason(ctx, l, t, "kill_switch_disabled")
		}
		var e error
		t, e = s.Advance(ctx, t, l.Owner, "running", "none")
		if e != nil {
			return e
		}
	}
	var now time.Time
	if e := s.DB.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now); e != nil {
		return e
	}
	expired := !t.DeadlineAt.After(now)
	a, e := s.GetAttempt(ctx, t.ID, "verify")
	if errors.Is(e, sql.ErrNoRows) {
		if t.Status == "canceling" || expired {
			next, reason := "failed", "deadline_exceeded"
			if t.Status == "canceling" && !expired {
				next, reason = "canceled", "canceled_by_actor"
			}
			_, e = s.Advance(ctx, t, l.Owner, next, reason)
			return e
		}
		request := r.Template
		var frozen BackupConfig
		var raw string
		if e = s.DB.QueryRowContext(ctx, "SELECT config_snapshot FROM system_backup_tasks WHERE public_id=? AND origin_installation_id=? AND config_version=?", t.ID, s.InstallationID, t.ConfigVersion).Scan(&raw); e != nil {
			return e
		}
		if json.Unmarshal([]byte(raw), &frozen) != nil || frozen.Validate() != nil {
			return errors.New("validation_failed")
		}
		request.Origin = s.InstallationID
		request.BackupID = t.SourceID
		request.MaxBytes = frozen.MaxArtifactBytes
		request.DeadlineSeconds = int64(t.DeadlineAt.Sub(now) / time.Second)
		request.DeadlineAt = t.DeadlineAt
		if request.DeadlineSeconds < 1 {
			return errors.New("deadline_exceeded")
		}
		if e = request.Validate(); e != nil {
			return e
		}
		a, e = s.PrepareAttempt(ctx, l, t, "verify", request)
	}
	if e != nil {
		return e
	}
	if t.Status == "canceling" || expired {
		stopped, e := r.Jobs.Stop(ctx, a)
		if e != nil {
			return e
		}
		if !stopped {
			return nil
		}
		next, reason := "failed", "deadline_exceeded"
		if t.Status == "canceling" && !expired {
			next, reason = "canceled", "canceled_by_actor"
		}
		if a.Status == "running" {
			if e = s.FinishAttempt(ctx, l, t, a, "abandoned", map[string]string{"reason": reason}); e != nil {
				return e
			}
		}
		_, e = s.Advance(ctx, t, l.Owner, next, reason)
		return e
	}
	if a.Status == "succeeded" {
		return r.completeVerified(ctx, s, l, t, a)
	}
	if a.Status == "failed" {
		_, e = s.Advance(ctx, t, l.Owner, "failed", "runner_failed")
		return e
	}
	if a.Status != "running" {
		return errors.New("state_conflict")
	}
	result, e := r.Jobs.Observe(ctx, a)
	if e != nil {
		return e
	}
	if result.State == "running" {
		return nil
	}
	if result.State != "succeeded" && result.State != "failed" {
		return errors.New("evidence_incomplete")
	}
	if result.State == "succeeded" && (result.Report == nil || !result.Report.IntegrityVerified || result.Report.AcceptanceEligible || result.Report.Origin != s.InstallationID || result.Report.BackupID != t.SourceID || result.Report.PartsVerified != len(RequiredParts)) {
		return errors.New("evidence_incomplete")
	}
	if e = s.FinishAttempt(ctx, l, t, a, result.State, result); e != nil {
		return e
	}
	if result.State == "succeeded" {
		return r.completeVerified(ctx, s, l, t, a)
	}
	failure := "none"
	if result.State == "failed" {
		failure = "runner_failed"
	}
	_, e = s.Advance(ctx, t, l.Owner, result.State, failure)
	return e
}

// Persist evidence before deleting the exact owned Job. A lost deletion
// response is retried from the durable successful attempt, without executing
// verification again. The externally supplied bootstrap Secret is reusable
// configuration and is deliberately not deleted by this task.
func (r VerifyTaskRunner) completeVerified(ctx context.Context, s Store, l Leadership, t Task, a Attempt) error {
	// FinishAttempt does not mutate the caller's Attempt value. Read the
	// committed result even on the first cleanup tick so Stop sees its Job UID.
	persisted, e := s.GetAttempt(ctx, t.ID, "verify")
	if e != nil {
		return e
	}
	if persisted.ID != a.ID || persisted.Status != "succeeded" {
		return errors.New("state_conflict")
	}
	a = persisted
	stopped, e := r.Jobs.Stop(ctx, a)
	if e != nil || !stopped {
		return e
	}
	_, e = s.Advance(ctx, t, l.Owner, "succeeded", "none")
	return e
}
