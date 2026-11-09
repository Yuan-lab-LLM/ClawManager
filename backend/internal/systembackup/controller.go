package systembackup

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Leadership struct {
	Owner     string
	Fence     uint64
	ExpiresAt time.Time
}

func (s Store) AcquireLeadership(ctx context.Context, owner string, ttl time.Duration) (Leadership, error) {
	var out Leadership
	if !refPattern.MatchString(owner) || ttl < 15*time.Second || ttl > 5*time.Minute {
		return out, errors.New("validation_failed")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, "INSERT IGNORE INTO system_backup_controller_leases(scope,owner,fencing_token,expires_at) VALUES(?,'',0,UTC_TIMESTAMP(6))", s.InstallationID); e != nil {
		return out, e
	}
	var now time.Time
	if e = tx.QueryRowContext(ctx, "SELECT owner,fencing_token,expires_at,UTC_TIMESTAMP(6) FROM system_backup_controller_leases WHERE scope=? FOR UPDATE", s.InstallationID).Scan(&out.Owner, &out.Fence, &out.ExpiresAt, &now); e != nil {
		return out, e
	}
	if out.Owner != owner && out.ExpiresAt.After(now) {
		return out, ErrFence
	}
	if out.Owner != owner || !out.ExpiresAt.After(now) {
		out.Fence++
	}
	out.Owner = owner
	out.ExpiresAt = now.Add(ttl)
	if _, e = tx.ExecContext(ctx, "UPDATE system_backup_controller_leases SET owner=?,fencing_token=?,expires_at=?,updated_at=UTC_TIMESTAMP(6) WHERE scope=?", owner, out.Fence, out.ExpiresAt, s.InstallationID); e != nil {
		return out, e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE system_backup_installation_state SET controller_seen_at=UTC_TIMESTAMP(6) WHERE singleton_id=1 AND origin_installation_id=?", s.InstallationID); e != nil {
		return out, e
	}
	return out, tx.Commit()
}
func (s Store) lockLeadership(ctx context.Context, tx *sql.Tx, l Leadership) error {
	var owner string
	var fence uint64
	var valid bool
	if e := tx.QueryRowContext(ctx, "SELECT owner,fencing_token,expires_at>UTC_TIMESTAMP(6) FROM system_backup_controller_leases WHERE scope=? FOR UPDATE", s.InstallationID).Scan(&owner, &fence, &valid); e != nil {
		return e
	}
	if !valid || owner != l.Owner || fence != l.Fence {
		return ErrFence
	}
	return nil
}

// ClaimLedTask obtains the task lease under the live leader fence. A pending
// claim does not start execution or reset its creation-based deadline.
func (s Store) ClaimLedTask(ctx context.Context, l Leadership, id string, version uint64, ttl time.Duration) (Task, error) {
	var zero Task
	if ttl < 15*time.Second || ttl > 5*time.Minute {
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
	res, e := tx.ExecContext(ctx, `UPDATE system_backup_tasks SET lease_owner=?,lease_until=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)),row_version=row_version+1 WHERE public_id=? AND origin_installation_id=? AND row_version=? AND status NOT IN ('succeeded','failed','canceled') AND (lease_owner IS NULL OR lease_owner=? OR lease_until<=UTC_TIMESTAMP(6))`, l.Owner, ttl.Microseconds(), id, s.InstallationID, version, l.Owner)
	if e = requireRow(res, e); e != nil {
		return zero, e
	}
	task, e := getTask(ctx, tx, id)
	if e != nil {
		return zero, e
	}
	return task, tx.Commit()
}

// SetPendingReason persists queue/backoff state without pretending a phase ran.
func (s Store) SetPendingReason(ctx context.Context, l Leadership, t Task, reason string) error {
	if reason != "global_capture_queue" && reason != "dependency_backoff" && reason != "kill_switch_disabled" && reason != "" {
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
	res, e := tx.ExecContext(ctx, "UPDATE system_backup_tasks SET pending_reason=?,row_version=row_version+1 WHERE public_id=? AND row_version=? AND lease_owner=? AND lease_until>UTC_TIMESTAMP(6) AND status='pending'", reason, t.ID, t.Version, l.Owner)
	if e = requireRow(res, e); e != nil {
		return e
	}
	return tx.Commit()
}

// ReconcileUnstarted is a safe kill-switch/deadline watchdog. It only settles
// tasks proven never to have started an attempt or external commit. Started
// work belongs to its durable runner and must not be guessed safe to cancel.
func (s Store) ReconcileUnstarted(ctx context.Context, l Leadership, t Task) (Task, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return Task{}, e
	}
	defer tx.Rollback()
	if e = s.lockLeadership(ctx, tx, l); e != nil {
		return Task{}, e
	}
	actual, e := scanTask(tx.QueryRowContext(ctx, "SELECT "+taskColumns+" FROM system_backup_tasks WHERE public_id=? FOR UPDATE", t.ID))
	if e != nil {
		return actual, e
	}
	if actual.Version != t.Version || actual.LeaseOwner != l.Owner {
		return actual, ErrFence
	}
	var now time.Time
	var attempts int
	if e = tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now); e != nil {
		return actual, e
	}
	if actual.LeaseUntil == nil || !actual.LeaseUntil.After(now) {
		return actual, ErrFence
	}
	if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM system_backup_attempts WHERE task_id=?", t.ID).Scan(&attempts); e != nil {
		return actual, e
	}
	if attempts != 0 || actual.StartedAt != nil || actual.CommitStarted || (actual.Status != "pending" && actual.Status != "canceling") {
		return actual, tx.Commit()
	}
	next, failure := "", "none"
	if !actual.DeadlineAt.After(now) {
		next, failure = "failed", "deadline_exceeded"
	} else if actual.Status == "canceling" {
		next, failure = "canceled", "canceled_by_actor"
	}
	if next == "" {
		return actual, tx.Commit()
	}
	updated, e := actual.Transition(next, failure, now)
	if e != nil {
		return actual, e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE system_backup_tasks SET status=?,failure_category=?,finished_at=?,can_cancel=0,pending_reason='',attention_reason='',row_version=row_version+1 WHERE public_id=?", next, failure, now, t.ID); e != nil {
		return actual, e
	}
	if e = appendEvent(ctx, tx, t.ID, "state_changed", map[string]string{"from": actual.Status, "to": next, "failure_category": failure}); e != nil {
		return actual, e
	}
	return updated, tx.Commit()
}

type Controller struct {
	Store        Store
	Owner        string
	PollInterval time.Duration
	Verifier     *VerifyTaskRunner
	Capturer     *CaptureTaskRunner
}

// Run maintains the leader and safely closes unstarted canceled/expired work.
// Capture admission stays closed until concrete adapters publish every required
// readiness proof. This process has no business service or migration startup.
func (c Controller) Run(ctx context.Context) error {
	if c.PollInterval < time.Second || c.PollInterval > 10*time.Second {
		return errors.New("invalid controller polling interval")
	}
	ticker := time.NewTicker(c.PollInterval)
	defer ticker.Stop()
	for {
		if e := c.Tick(ctx); e != nil && !errors.Is(e, ErrFence) {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func (c Controller) Tick(ctx context.Context) error {
	l, e := c.Store.AcquireLeadership(ctx, c.Owner, 30*time.Second)
	if e != nil {
		return e
	}
	cfg, e := c.Store.Config(ctx)
	if e != nil {
		return e
	}
	if e = c.Store.RecordHealth(ctx, cfg.Version, "controller", "healthy", "none", RegistryHash(), 30); e != nil {
		return e
	}
	rows, e := c.Store.DB.QueryContext(ctx, "SELECT public_id,row_version FROM system_backup_tasks WHERE origin_installation_id=? AND (status IN ('pending','canceling') OR (task_type='artifact_verify' AND status='running') OR (task_type='backup' AND status NOT IN ('succeeded','failed','canceled'))) AND (lease_owner IS NULL OR lease_owner=? OR lease_until<=UTC_TIMESTAMP(6)) ORDER BY created_at,public_id LIMIT 100", c.Store.InstallationID, c.Owner)
	if e != nil {
		return e
	}
	type item struct {
		id      string
		version uint64
	}
	var items []item
	for rows.Next() {
		var i item
		if e = rows.Scan(&i.id, &i.version); e != nil {
			rows.Close()
			return e
		}
		items = append(items, i)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, i := range items {
		t, e := c.Store.ClaimLedTask(ctx, l, i.id, i.version, 30*time.Second)
		if errors.Is(e, ErrFence) {
			continue
		}
		if e != nil {
			return e
		}
		t, e = c.Store.ReconcileUnstarted(ctx, l, t)
		if errors.Is(e, ErrFence) {
			continue
		}
		if e != nil {
			return e
		}
		if c.Capturer != nil && t.Type == Backup && !t.Terminal() {
			if e = c.Capturer.Tick(ctx, c.Store, l, t, cfg); e != nil && !errors.Is(e, ErrFence) {
				if errors.Is(e, errBackupExecutionUnknown) {
					if e = c.Store.awaitOriginalBackupExecution(ctx, l, t); e != nil && !errors.Is(e, ErrFence) {
						return e
					}
					continue
				}
				return e
			}
			continue
		}
		if c.Verifier != nil && t.Type == ArtifactVerify && !t.Terminal() {
			if e = c.Verifier.Tick(ctx, c.Store, l, t, cfg); e != nil && !errors.Is(e, ErrFence) {
				return e
			}
			continue
		}
		if t.Status == "pending" {
			reason := "dependency_backoff"
			if !cfg.Config.Enabled {
				reason = "kill_switch_disabled"
			}
			if e = c.Store.SetPendingReason(ctx, l, t, reason); e != nil && !errors.Is(e, ErrFence) {
				return e
			}
		}
	}
	return nil
}
