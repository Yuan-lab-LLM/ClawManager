package systembackup

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

type CaptureJobDriver interface {
	Ensure(context.Context, Attempt, string, bool) (*batchv1.Job, error)
	Existing(context.Context, Attempt) (*batchv1.Job, error)
	Observe(context.Context, Attempt, string) (CaptureJobResult, error)
	Stop(context.Context, Attempt, string) (bool, error)
}
type CaptureResourceDriver interface {
	Prepare(context.Context, Store, Leadership, Task, BackupConfig) (CapturePlanRequest, error)
	Cleanup(context.Context, Store, Leadership, Task, *CapturePlan) (bool, error)
}
type CaptureParticipantDriver interface {
	Pause(context.Context, Store, Leadership, Task, GateOwnership) error
	Resume(context.Context, Store, Leadership, Task, GateOwnership) error
}
type CapturePublicationDriver interface {
	Tick(context.Context, Store, Leadership, Task, CapturePlan, CaptureResourceDriver) error
}
type CaptureTaskRunner struct {
	Jobs         CaptureJobDriver
	Resources    CaptureResourceDriver
	Participants CaptureParticipantDriver
	Publisher    CapturePublicationDriver
}
type captureGateState struct {
	Phase, Owner      string
	Generation, Token uint64
	HoldDeadline      *time.Time
}

func (s Store) captureGate(ctx context.Context, scope string) (captureGateState, error) {
	var g captureGateState
	var deadline sql.NullTime
	err := s.DB.QueryRowContext(ctx, "SELECT phase,COALESCE(owner,''),generation,fencing_token,hold_deadline FROM system_backup_gate WHERE scope=?", scope).Scan(&g.Phase, &g.Owner, &g.Generation, &g.Token, &deadline)
	if deadline.Valid {
		g.HoldDeadline = &deadline.Time
	}
	return g, err
}

func (s Store) requestCaptureAbort(ctx context.Context, l Leadership, t Task, reason string) error {
	if !slices.Contains(FailureCategories, reason) || reason == "none" {
		return errors.New("validation_failed")
	}
	tx, x, err := s.beginCaptureMutation(ctx, l, t)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if x.CommitStarted || x.Registered {
		return errors.New("commit_started")
	}
	if _, err = tx.ExecContext(ctx, "INSERT IGNORE INTO system_backup_capture_aborts(task_id,reason) VALUES(?,?)", x.ID, reason); err != nil {
		return err
	}
	return tx.Commit()
}
func (s Store) withCaptureFence(ctx context.Context, l Leadership, t Task, fn func() error) error {
	tx, _, err := s.beginCaptureMutation(ctx, l, t)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fn(); err != nil {
		return err
	}
	return tx.Commit()
}

// Tick advances one bounded step. Every source read is owned by a frozen Job;
// replacement leaders adopt its UID and never restart a lost capture attempt.
func (r CaptureTaskRunner) Tick(ctx context.Context, s Store, l Leadership, t Task, current ConfigVersion) error {
	if r.Jobs == nil || r.Resources == nil || r.Publisher == nil || t.Type != Backup {
		return errors.New("not_ready")
	}
	var now time.Time
	if err := s.DB.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now); err != nil {
		return err
	}
	plan, err := s.GetCapturePlan(ctx, t.ID)
	hasPlan := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if t.CommitStarted || t.Status == "publishing" || t.Status == "finalization_unknown" {
		if !hasPlan || plan.Manifest == nil || plan.SourceResumedAt == nil || plan.ForcedResumeAt != nil {
			return errors.New("evidence_incomplete")
		}
		return r.Publisher.Tick(ctx, s, l, t, plan, r.Resources)
	}
	var abort string
	err = s.DB.QueryRowContext(ctx, "SELECT reason FROM system_backup_capture_aborts WHERE task_id=?", t.ID).Scan(&abort)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if !t.DeadlineAt.After(now) {
		abort = "deadline_exceeded"
	} else if t.Status == "canceling" && abort == "" {
		abort = "canceled_by_actor"
	}
	if hasPlan && plan.ForcedResumeAt != nil {
		abort = "quiesce_resume_failed"
	}
	if abort != "" {
		return r.abort(ctx, s, l, t, hasPlan, &plan, abort)
	}
	if t.Status == "pending" {
		if !current.Config.Enabled {
			return s.SetPendingReason(ctx, l, t, "kill_switch_disabled")
		}
		if publisher, ok := r.Publisher.(interface{ ReadyForPurpose(string) error }); ok {
			if err := publisher.ReadyForPurpose(t.Purpose); err != nil {
				return s.SetPendingReason(ctx, l, t, "dependency_backoff")
			}
		}
		var first string
		if err = s.DB.QueryRowContext(ctx, "SELECT public_id FROM system_backup_tasks WHERE origin_installation_id=? AND task_type='backup' AND status IN ('pending','preparing','ready_for_capture','acquiring_gate','capturing') ORDER BY created_at,public_id LIMIT 1", s.InstallationID).Scan(&first); err != nil {
			return err
		}
		if first != t.ID {
			return s.SetPendingReason(ctx, l, t, "global_capture_queue")
		}
		_, err = s.Advance(ctx, t, l.Owner, "preparing", "none")
		return err
	}
	var config BackupConfig
	var raw []byte
	if err = s.DB.QueryRowContext(ctx, "SELECT config_snapshot FROM system_backup_tasks WHERE public_id=?", t.ID).Scan(&raw); err != nil {
		return err
	}
	if strictDecode(raw, &config) != nil || config.Validate() != nil {
		return errors.New("evidence_incomplete")
	}
	if t.Status == "preparing" {
		if !hasPlan {
			request, e := r.Resources.Prepare(ctx, s, l, t, config)
			if errors.Is(e, ErrMaintenance) || PublicError(e) == "not_ready" {
				return nil
			}
			if e != nil {
				return e
			}
			plan, e = s.PrepareCapturePlan(ctx, l, t, request)
			if errors.Is(e, ErrMaintenance) || PublicError(e) == "not_ready" {
				return nil
			}
			if e != nil {
				return e
			}
			hasPlan = true
		}
		var participants int
		if err = s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM system_backup_participants WHERE scope=?", plan.Request.Ordinary.Gate.Scope).Scan(&participants); err != nil {
			return err
		}
		if participants > 0 && r.Participants == nil {
			return errors.New("not_ready")
		}
		ready := true
		for _, request := range []CaptureJobRequest{plan.Request.Ordinary, plan.Request.Secrets} {
			a, e := s.GetAttempt(ctx, t.ID, request.Phase)
			if errors.Is(e, sql.ErrNoRows) {
				a, e = s.PrepareAttempt(ctx, l, t, request.Phase, request)
			}
			if e != nil {
				return e
			}
			if a.Status == "failed" || a.Status == "abandoned" {
				return s.requestCaptureAbort(ctx, l, t, "runner_failed")
			}
			binding, e := s.ensureBackupExecution(ctx, l, t, a,
				func(step context.Context) (*batchv1.Job, error) { return r.Jobs.Ensure(step, a, "", true) },
				func(step context.Context) (*batchv1.Job, error) { return r.Jobs.Existing(step, a) },
				func(step context.Context, job *batchv1.Job) error { return s.BindCaptureJob(step, l, t, a, job) })
			if e != nil {
				return e
			}
			observed, e := r.Jobs.Observe(ctx, a, binding.UID)
			if e != nil {
				return e
			}
			if observed.State == "failed" {
				if e = s.FinishAttempt(ctx, l, t, a, "failed", observed); e != nil {
					return e
				}
				return s.requestCaptureAbort(ctx, l, t, "runner_failed")
			}
			if observed.State == "sealed" {
				return s.requestCaptureAbort(ctx, l, t, "coordination_invalid")
			}
			isReady := observed.State == "ready"
			ready = ready && isReady
			if e = s.RecordCaptureReady(ctx, l, t, a, binding, isReady); e != nil {
				return e
			}
		}
		if ready {
			_, err = s.Advance(ctx, t, l.Owner, "ready_for_capture", "none")
		}
		return err
	}
	if !hasPlan {
		return errors.New("evidence_incomplete")
	}
	if t.Status == "ready_for_capture" {
		_, err = s.Advance(ctx, t, l.Owner, "acquiring_gate", "none")
		return err
	}
	if t.Status == "acquiring_gate" {
		var started time.Time
		if err = s.DB.QueryRowContext(ctx, "SELECT MIN(created_at) FROM system_backup_events WHERE task_id=? AND event_type='state_changed' AND JSON_UNQUOTE(JSON_EXTRACT(details_json,'$.to'))='acquiring_gate'", t.ID).Scan(&started); err != nil {
			return err
		}
		if !now.Before(started.Add(time.Duration(config.GateAcquisitionSeconds) * time.Second)) {
			return s.requestCaptureAbort(ctx, l, t, "quiesce_timeout")
		}
		g, e := s.captureGate(ctx, plan.Request.Ordinary.Gate.Scope)
		if e != nil {
			return e
		}
		own := plan.Request.Ordinary.Gate
		if g.Phase == "idle" {
			if g.Generation+1 != own.Generation || g.Token+1 != own.Token {
				return s.requestCaptureAbort(ctx, l, t, "coordination_invalid")
			}
			err = s.withCaptureFence(ctx, l, t, func() error {
				_, e := (Gate{s.DB}).Begin(ctx, own.Scope, t.ID, RegistryHash(), started.Add(time.Duration(config.GateAcquisitionSeconds)*time.Second).Sub(now), min(time.Duration(config.MaxHoldSeconds)*time.Second, t.DeadlineAt.Sub(now)))
				return e
			})
			if errors.Is(err, ErrMaintenance) {
				return nil
			}
			return err
		}
		if g.Owner != t.ID || g.Generation != own.Generation {
			return s.requestCaptureAbort(ctx, l, t, "coordination_invalid")
		}
		if g.HoldDeadline == nil || !g.HoldDeadline.After(now) {
			return s.requestCaptureAbort(ctx, l, t, "quiesce_timeout")
		}
		if g.Phase == "acquiring" {
			acquiring := own
			acquiring.Token = g.Token
			if r.Participants != nil {
				if e = r.Participants.Pause(ctx, s, l, t, acquiring); e != nil {
					return e
				}
			}
			err = s.withCaptureFence(ctx, l, t, func() error { _, e := (Gate{s.DB}).Hold(ctx, acquiring); return e })
			if errors.Is(err, ErrMaintenance) {
				return nil
			}
			return err
		}
		if g.Phase != "held" || g.Token != own.Token {
			return s.requestCaptureAbort(ctx, l, t, "coordination_invalid")
		}
		_, err = s.Advance(ctx, t, l.Owner, "capturing", "none")
		return err
	}
	if t.Status == "capturing" {
		if plan.Manifest == nil {
			for _, phase := range []string{"capture", "secret_capture"} {
				a, e := s.GetAttempt(ctx, t.ID, phase)
				if e != nil {
					return e
				}
				if a.Status == "sealed" {
					continue
				}
				if a.Status != "running" {
					return s.requestCaptureAbort(ctx, l, t, "runner_failed")
				}
				binding, e := s.CaptureBinding(ctx, a.ID)
				if e != nil {
					return e
				}
				observed, e := r.Jobs.Observe(ctx, a, binding.UID)
				if e != nil {
					return e
				}
				if observed.State == "failed" {
					if e = s.FinishAttempt(ctx, l, t, a, "failed", observed); e != nil {
						return e
					}
					return s.requestCaptureAbort(ctx, l, t, "runner_failed")
				}
				if observed.State != "sealed" {
					return nil
				}
				if e = s.FinishAttempt(ctx, l, t, a, "sealed", observed); e != nil {
					return e
				}
			}
			plan, err = s.SealCapturePlan(ctx, l, t)
			if err != nil {
				return err
			}
		}
		done, e := r.resume(ctx, s, l, t, plan)
		if e != nil {
			return e
		}
		if !done {
			return nil
		}
		_, err = s.Advance(ctx, t, l.Owner, "validating", "none")
		return err
	}
	if t.Status == "validating" {
		if plan.Manifest == nil || plan.SourceResumedAt == nil || plan.ForcedResumeAt != nil {
			return errors.New("evidence_incomplete")
		}
		return r.Publisher.Tick(ctx, s, l, t, plan, r.Resources)
	}
	return errors.New("state_conflict")
}

func (r CaptureTaskRunner) resume(ctx context.Context, s Store, l Leadership, t Task, p CapturePlan) (bool, error) {
	own := p.Request.Ordinary.Gate
	g, err := s.captureGate(ctx, own.Scope)
	if err != nil {
		return false, err
	}
	if g.Phase == "idle" {
		latest, e := s.GetCapturePlan(ctx, t.ID)
		if e != nil {
			return false, e
		}
		if latest.SourceResumedAt == nil {
			return false, errors.New("coordination_invalid")
		}
		if latest.ForcedResumeAt != nil {
			return false, errors.New("quiesce_resume_failed")
		}
		return true, nil
	}
	if g.Owner != t.ID || g.Generation != own.Generation {
		return false, errors.New("coordination_invalid")
	}
	// The frozen plan describes the token after Hold. An abort while still
	// acquiring must release the current token, before that increment exists.
	own.Token = g.Token
	if g.Phase != "releasing" {
		if err = s.withCaptureFence(ctx, l, t, func() error { return (Gate{s.DB}).BeginRelease(ctx, own) }); err != nil {
			return false, err
		}
	}
	if r.Participants != nil {
		if err = r.Participants.Resume(ctx, s, l, t, own); err != nil {
			return false, err
		}
	}
	err = s.withCaptureFence(ctx, l, t, func() error { return (Gate{s.DB}).FinishRelease(ctx, own) })
	if errors.Is(err, ErrMaintenance) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	latest, err := s.GetCapturePlan(ctx, t.ID)
	if err != nil {
		return false, err
	}
	if latest.ForcedResumeAt != nil {
		return false, errors.New("quiesce_resume_failed")
	}
	return true, nil
}

func (r CaptureTaskRunner) abort(ctx context.Context, s Store, l Leadership, t Task, hasPlan bool, p *CapturePlan, reason string) error {
	if err := s.requestCaptureAbort(ctx, l, t, reason); err != nil {
		return err
	}
	stopped := true
	for _, phase := range []string{"capture", "secret_capture"} {
		a, err := s.GetAttempt(ctx, t.ID, phase)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		b, err := s.CaptureBinding(ctx, a.ID)
		if errors.Is(err, sql.ErrNoRows) {
			sent, e := s.backupJobWasSent(ctx, a)
			if e != nil {
				return e
			}
			if !sent {
				continue
			}
			job, e := r.Jobs.Existing(ctx, a)
			if apierrors.IsNotFound(e) {
				stopped = false
				continue
			}
			if e != nil {
				return e
			}
			if e = s.BindCaptureJob(ctx, l, t, a, job); e != nil {
				return e
			}
			b, err = s.CaptureBinding(ctx, a.ID)
		}
		if err != nil {
			return err
		}
		done, err := r.Jobs.Stop(ctx, a, b.UID)
		if err != nil {
			return err
		}
		stopped = stopped && done
	}
	if !stopped {
		return nil
	}
	if publisher, ok := r.Publisher.(interface {
		Abort(context.Context, Store, Leadership, Task) (bool, error)
	}); ok {
		done, err := publisher.Abort(ctx, s, l, t)
		if err != nil {
			return err
		}
		if !done {
			return nil
		}
	}
	if hasPlan {
		if err := s.withCaptureFence(ctx, l, t, func() error { return (Gate{s.DB}).CancelAdmission(ctx, p.Request.Ordinary.Gate.Scope, t.ID) }); err != nil {
			return err
		}
		g, err := s.captureGate(ctx, p.Request.Ordinary.Gate.Scope)
		if err != nil {
			return err
		}
		if g.Owner == t.ID {
			done, e := r.resume(ctx, s, l, t, *p)
			if e != nil && PublicError(e) != "quiesce_resume_failed" {
				return e
			}
			if !done && e == nil {
				return nil
			}
		}
	}
	var plan *CapturePlan
	if hasPlan {
		plan = p
	}
	done, err := r.Resources.Cleanup(ctx, s, l, t, plan)
	if err != nil {
		return err
	}
	if !done {
		return nil
	}
	next := "failed"
	if reason == "canceled_by_actor" && t.Status == "canceling" {
		next = "canceled"
	}
	_, err = s.Advance(ctx, t, l.Owner, next, reason)
	return err
}
