package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
	"github.com/google/uuid"
	"github.com/upper/db/v4"
)

// Critical activity uses the same instance row lock as an idle-stop claim.
// Detailed model audit persistence is deliberately not this admission boundary.
type InstanceAutoStopRepository struct{ sess db.Session }

func NewInstanceAutoStopRepository(sess db.Session) *InstanceAutoStopRepository {
	return &InstanceAutoStopRepository{sess: sess}
}

func (r *InstanceAutoStopRepository) Settings(ctx context.Context) (*models.InstanceAutoStopSettings, error) {
	now := time.Now().UTC()
	if _, err := r.sess.SQL().ExecContext(ctx, `UPDATE instance_auto_stop_settings SET next_check_at=IF(check_interval_seconds IS NULL,?,DATE_ADD(?, INTERVAL check_interval_seconds SECOND)) WHERE id=1 AND next_check_at IS NULL`, models.NextInstanceAutoStopCheck(now), now); err != nil {
		return nil, err
	}
	if models.AutoStopTestTiming {
		// A persisted production 04:00 deadline must not postpone the first test
		// scan. Never replace an active scan or extend an already sooner deadline.
		next := models.NextInstanceAutoStopCheck(now)
		if _, err := r.sess.SQL().ExecContext(ctx, `UPDATE instance_auto_stop_settings SET next_check_at=?,updated_at=? WHERE id=1 AND check_interval_seconds IS NULL AND scan_token='' AND next_check_at>?`, next, now, next); err != nil {
			return nil, err
		}
	}
	return readAutoStopSettings(ctx, r.sess, false)
}
func readAutoStopSettings(ctx context.Context, s db.Session, lock bool) (*models.InstanceAutoStopSettings, error) {
	query := `SELECT enabled,check_interval_days,idle_timeout_days,check_interval_seconds,idle_timeout_seconds,version,next_check_at,scan_token,scan_cursor,scan_lease_until FROM instance_auto_stop_settings WHERE id=1`
	if lock {
		query += " FOR UPDATE"
	}
	row, err := s.SQL().QueryRowContext(ctx, query)
	if err != nil {
		return nil, err
	}
	var out models.InstanceAutoStopSettings
	if err = row.Scan(&out.Enabled, &out.CheckIntervalDays, &out.IdleTimeoutDays, &out.CheckIntervalSeconds, &out.IdleTimeoutSeconds, &out.Version, &out.NextCheckAt, &out.ScanID, &out.ScanCursor, &out.ScanLeaseUntil); err != nil {
		return nil, err
	}
	return &out, out.Validate()
}
func (r *InstanceAutoStopRepository) SaveSettings(ctx context.Context, want models.InstanceAutoStopSettings) (*models.InstanceAutoStopSettings, error) {
	if err := want.Validate(); err != nil {
		return nil, err
	}
	if want.Version < 1 {
		return nil, models.ErrAutoStopConflict
	}
	if _, err := r.Settings(ctx); err != nil {
		return nil, err
	}
	var saved *models.InstanceAutoStopSettings
	err := r.sess.Tx(func(s db.Session) error {
		current, err := readAutoStopSettings(ctx, s, true)
		if err != nil {
			return err
		}
		if current.Version != want.Version {
			return models.ErrAutoStopConflict
		}
		next := current.NextCheckAt
		if current.CheckInterval() != want.CheckInterval() || (!current.Enabled && want.Enabled) {
			next = want.NextCheck(time.Now().UTC())
		}
		next = next.Truncate(time.Microsecond)
		// Retain the legacy values when a new client supplies exact seconds only.
		if want.CheckIntervalSeconds != nil {
			want.CheckIntervalDays = current.CheckIntervalDays
		}
		if want.IdleTimeoutSeconds != nil {
			want.IdleTimeoutDays = current.IdleTimeoutDays
		}
		_, err = s.SQL().ExecContext(ctx, `UPDATE instance_auto_stop_settings SET enabled=?,check_interval_days=?,idle_timeout_days=?,check_interval_seconds=?,idle_timeout_seconds=?,version=version+1,next_check_at=?,scan_token='',scan_cursor=0,scan_lease_until=NULL,updated_at=? WHERE id=1`, want.Enabled, want.CheckIntervalDays, want.IdleTimeoutDays, want.CheckIntervalSeconds, want.IdleTimeoutSeconds, next, time.Now().UTC())
		if err != nil {
			return err
		}
		saved = &want
		saved.Version++
		saved.NextCheckAt = next
		return nil
	})
	return saved, err
}
func (r *InstanceAutoStopRepository) ClaimCheck(ctx context.Context, now time.Time) (*models.InstanceAutoStopSettings, error) {
	if _, err := r.Settings(ctx); err != nil {
		return nil, err
	}
	var claimed *models.InstanceAutoStopSettings
	err := r.sess.TxContext(ctx, func(tx db.Session) error {
		cfg, err := readAutoStopSettings(ctx, tx, true)
		if err != nil {
			return err
		}
		if !cfg.Enabled || cfg.NextCheckAt.After(now) {
			return nil
		}
		if cfg.ScanID != "" && cfg.ScanLeaseUntil != nil && cfg.ScanLeaseUntil.After(now) {
			return nil
		}
		cfg.ScanID = uuid.NewString()
		lease := now.UTC().Add(5 * time.Minute)
		cfg.ScanLeaseUntil = &lease
		if _, err = tx.SQL().ExecContext(ctx, `UPDATE instance_auto_stop_settings SET scan_token=?,scan_lease_until=?,updated_at=? WHERE id=1`, cfg.ScanID, lease, now.UTC()); err != nil {
			return err
		}
		claimed = cfg
		return nil
	}, nil)
	return claimed, err
}
func (r *InstanceAutoStopRepository) CheckpointScan(ctx context.Context, cfg models.InstanceAutoStopSettings, cursor int) error {
	now := time.Now().UTC()
	result, err := r.sess.SQL().ExecContext(ctx, `UPDATE instance_auto_stop_settings SET scan_cursor=GREATEST(scan_cursor,?),scan_lease_until=?,updated_at=? WHERE id=1 AND enabled=TRUE AND version=? AND scan_token=? AND scan_lease_until>?`, cursor, now.Add(5*time.Minute), now, cfg.Version, cfg.ScanID, now)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return models.ErrAutoStopConflict
	}
	return nil
}
func (r *InstanceAutoStopRepository) CompleteScan(ctx context.Context, cfg models.InstanceAutoStopSettings) error {
	now := time.Now().UTC()
	next := cfg.AdvanceCheck(cfg.NextCheckAt, now)
	result, err := r.sess.SQL().ExecContext(ctx, `UPDATE instance_auto_stop_settings SET next_check_at=?,scan_token='',scan_cursor=0,scan_lease_until=NULL,updated_at=? WHERE id=1 AND enabled=TRUE AND version=? AND scan_token=? AND scan_lease_until>?`, next, now, cfg.Version, cfg.ScanID, now)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return models.ErrAutoStopConflict
	}
	return nil
}

func (r *InstanceAutoStopRepository) Initialize(ctx context.Context) error {
	now := time.Now().UTC()
	_, err := r.sess.SQL().ExecContext(ctx, `INSERT IGNORE INTO instance_model_activity(instance_id,generation,monitoring_since) SELECT id,runtime_generation,? FROM instances WHERE instance_mode='lite'`, now)
	if err != nil {
		return err
	}
	// A new generation always earns its own full window. Never erase a pending
	// stop fence, even if an unrelated writer incorrectly advances generation.
	_, err = r.sess.SQL().ExecContext(ctx, `UPDATE instance_model_activity a JOIN instances i ON i.id=a.instance_id SET a.generation=i.runtime_generation,a.monitoring_since=?,a.activity_version=a.activity_version+1,a.updated_at=? WHERE i.instance_mode='lite' AND a.generation<>i.runtime_generation AND a.stop_operation_id=''`, now, now)
	return err
}

type activityInstance struct {
	userID, generation int
	mode, status       string
}

func lockActivityInstance(ctx context.Context, s db.Session, id int) (activityInstance, error) {
	row, err := s.SQL().QueryRowContext(ctx, `SELECT user_id,runtime_generation,instance_mode,status FROM instances WHERE id=? FOR UPDATE`, id)
	if err != nil {
		return activityInstance{}, err
	}
	var i activityInstance
	if err = row.Scan(&i.userID, &i.generation, &i.mode, &i.status); err != nil {
		return i, err
	}
	return i, nil
}
func ensureActivity(ctx context.Context, s db.Session, id, generation int, now time.Time) error {
	_, err := s.SQL().ExecContext(ctx, `INSERT IGNORE INTO instance_model_activity(instance_id,generation,monitoring_since) VALUES(?,?,?)`, id, generation, now)
	if err != nil {
		return err
	}
	_, err = s.SQL().ExecContext(ctx, `UPDATE instance_model_activity SET generation=?,monitoring_since=?,activity_version=activity_version+1,updated_at=? WHERE instance_id=? AND generation<>? AND stop_operation_id=''`, generation, now, now, id, generation)
	return err
}
func readActivity(ctx context.Context, s db.Session, id int, lock bool) (*models.InstanceModelActivity, error) {
	query := `SELECT a.instance_id,a.generation,a.monitoring_since,a.last_gateway_activity_at,a.activity_version,a.auto_stop_disabled,a.stop_operation_id,a.stop_phase,a.stopped_reason,a.stopped_at,a.blocker,a.start_deadline_at,(SELECT COUNT(*) FROM instance_model_activity_requests q WHERE q.instance_id=a.instance_id AND q.generation=a.generation AND q.finished_at IS NULL AND (q.lease_expires_at IS NULL OR q.lease_expires_at>UTC_TIMESTAMP(6))) FROM instance_model_activity a WHERE a.instance_id=?`
	if lock {
		query += " FOR UPDATE"
	}
	row, err := s.SQL().QueryRowContext(ctx, query, id)
	if err != nil {
		return nil, err
	}
	var a models.InstanceModelActivity
	if err = row.Scan(&a.InstanceID, &a.Generation, &a.MonitoringSince, &a.LastGatewayActivityAt, &a.ActivityVersion, &a.AutoStopDisabled, &a.StopOperationID, &a.StopPhase, &a.StoppedReason, &a.StoppedAt, &a.Blocker, &a.StartDeadlineAt, &a.ActiveRequests); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}
func (r *InstanceAutoStopRepository) GetActivity(ctx context.Context, id int) (*models.InstanceModelActivity, error) {
	return readActivity(ctx, r.sess, id, false)
}

func (r *InstanceAutoStopRepository) BeginModelActivity(ctx context.Context, userID, id, generation int, gatewayID string) (string, error) {
	key := ""
	err := r.sess.Tx(func(s db.Session) error {
		i, err := lockActivityInstance(ctx, s, id)
		if err != nil {
			return err
		}
		if i.userID != userID {
			return models.ErrInstanceStopping
		}
		if i.mode != "lite" {
			return nil
		}
		if (i.status != "running" && i.status != "creating") || (generation > 0 && generation != i.generation) {
			return models.ErrInstanceStopping
		}
		if gatewayID != "" {
			row, e := s.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_runtime_bindings WHERE instance_id=? AND generation=? AND gateway_id=? AND state IN ('creating','starting','running','ready','healthy')`, id, i.generation, gatewayID)
			if e != nil {
				return e
			}
			var n int
			if e = row.Scan(&n); e != nil {
				return e
			}
			if n != 1 {
				return models.ErrInstanceStopping
			}
		}
		if err = RequireInstanceTeamRunning(ctx, s, id); err != nil {
			if errors.Is(err, ErrTeamNotRunning) {
				return models.ErrInstanceStopping
			}
			return err
		}
		now := time.Now().UTC()
		if err = ensureActivity(ctx, s, id, i.generation, now); err != nil {
			return err
		}
		a, err := readActivity(ctx, s, id, true)
		if err != nil {
			return err
		}
		if a == nil || a.StopOperationID != "" || a.Generation != i.generation || (a.StartDeadlineAt != nil && !a.StartDeadlineAt.After(now)) {
			return models.ErrInstanceStopping
		}
		key = uuid.NewString()
		if _, err = s.SQL().ExecContext(ctx, `INSERT INTO instance_model_activity_requests(request_id,instance_id,generation,started_at,last_heartbeat_at,lease_expires_at) VALUES(?,?,?,?,?,?)`, key, id, i.generation, now, now, now.Add(models.ModelActivityLeaseDuration)); err != nil {
			return err
		}
		_, err = s.SQL().ExecContext(ctx, `UPDATE instance_model_activity SET last_gateway_activity_at=GREATEST(COALESCE(last_gateway_activity_at,?),?),activity_version=activity_version+1,updated_at=? WHERE instance_id=?`, now, now, now, id)
		return err
	})
	if err != nil {
		return "", err
	}
	return key, nil
}
func (r *InstanceAutoStopRepository) SetDisabled(ctx context.Context, id int, disabled bool) error {
	return r.sess.Tx(func(s db.Session) error {
		i, err := lockActivityInstance(ctx, s, id)
		if err != nil {
			return err
		}
		if i.mode != "lite" {
			return fmt.Errorf("automatic stop settings apply only to Lite instances")
		}
		now := time.Now().UTC()
		if err = ensureActivity(ctx, s, id, i.generation, now); err != nil {
			return err
		}
		a, err := readActivity(ctx, s, id, true)
		if err != nil {
			return err
		}
		if a.StopOperationID != "" {
			return models.ErrInstanceStopping
		}
		_, err = s.SQL().ExecContext(ctx, `UPDATE instance_model_activity SET auto_stop_disabled=?,activity_version=activity_version+1,updated_at=? WHERE instance_id=?`, disabled, now, id)
		return err
	})
}
func (r *InstanceAutoStopRepository) Candidates(ctx context.Context, now time.Time, days, after, limit int) ([]models.InstanceModelActivity, error) {
	if err := (models.InstanceAutoStopSettings{CheckIntervalDays: 1, IdleTimeoutDays: days}).Validate(); err != nil {
		return nil, err
	}
	return r.CandidatesForTimeout(ctx, now, models.InstanceAutoStopIdleTimeout(days), after, limit)
}
func (r *InstanceAutoStopRepository) CandidatesForTimeout(ctx context.Context, now time.Time, timeout time.Duration, after, limit int) ([]models.InstanceModelActivity, error) {
	if timeout < time.Second {
		return nil, fmt.Errorf("idle timeout must be at least one second")
	}
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	cutoff := now.Add(-timeout)
	rows, err := r.sess.SQL().QueryContext(ctx, `SELECT a.instance_id FROM instance_model_activity a JOIN instances i ON i.id=a.instance_id WHERE a.instance_id>? AND NOT EXISTS (SELECT 1 FROM team_members m JOIN teams t ON t.id=m.team_id WHERE t.status<>'deleted' AND m.status<>'deleted' AND (m.instance_id=i.id OR (t.resource_scope='northbound' AND i.provisioning_operation_id=SHA2(CONCAT('owner-team-',t.id,'-member-',m.member_key),256)))) AND i.instance_mode='lite' AND i.status='running' AND i.runtime_generation=a.generation AND a.auto_stop_disabled=FALSE AND a.stop_operation_id='' AND a.monitoring_since<=? AND (a.last_gateway_activity_at IS NULL OR a.last_gateway_activity_at<=?) AND (i.started_at IS NULL OR i.started_at<=?) ORDER BY a.instance_id LIMIT ?`, after, cutoff, cutoff, cutoff, limit)
	if err != nil {
		return nil, err
	}
	var ids []int
	for rows.Next() {
		var id int
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	out := make([]models.InstanceModelActivity, 0, len(ids))
	for _, id := range ids {
		a, e := r.GetActivity(ctx, id)
		if e != nil {
			return nil, e
		}
		if a != nil {
			out = append(out, *a)
		}
	}
	return out, nil
}
func (r *InstanceAutoStopRepository) RecordBlocker(ctx context.Context, id int, message string) error {
	message = strings.TrimSpace(message)
	if len(message) > 500 {
		message = message[:500]
	}
	_, err := r.sess.SQL().ExecContext(ctx, `UPDATE instance_model_activity SET blocker=?,updated_at=? WHERE instance_id=? AND stop_operation_id=''`, message, time.Now().UTC(), id)
	return err
}
