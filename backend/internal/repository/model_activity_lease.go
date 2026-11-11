package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

func modelActivityRequestScope(ctx context.Context, s db.Session, key string) (id, generation int, err error) {
	row, err := s.SQL().QueryRowContext(ctx, `SELECT instance_id,generation FROM instance_model_activity_requests WHERE request_id=?`, key)
	if err != nil {
		return 0, 0, err
	}
	err = row.Scan(&id, &generation)
	return
}

// The immutable request nonce is the lease identity. Neither a late heartbeat
// nor a delayed completion can recreate a scope whose lease has been revoked.
func (r *InstanceAutoStopRepository) RenewModelActivity(ctx context.Context, key string) error {
	id, generation, err := modelActivityRequestScope(ctx, r.sess, key)
	if errors.Is(err, sql.ErrNoRows) {
		return models.ErrActivityUnavailable
	}
	if err != nil {
		return err
	}
	return r.sess.TxContext(ctx, func(s db.Session) error {
		i, err := lockActivityInstance(ctx, s, id)
		if err != nil {
			return err
		}
		if i.mode != "lite" || i.generation != generation || (i.status != "running" && i.status != "creating") {
			return models.ErrActivityUnavailable
		}
		a, err := readActivity(ctx, s, id, true)
		if err != nil {
			return err
		}
		if a == nil || a.Generation != generation || a.StopOperationID != "" {
			return models.ErrActivityUnavailable
		}
		now := time.Now().UTC()
		if a.StartDeadlineAt != nil && !a.StartDeadlineAt.After(now) {
			return models.ErrActivityUnavailable
		}
		res, err := s.SQL().ExecContext(ctx, `UPDATE instance_model_activity_requests SET last_heartbeat_at=?,lease_expires_at=? WHERE request_id=? AND finished_at IS NULL AND lease_expires_at>?`, now, now.Add(models.ModelActivityLeaseDuration), key, now)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return models.ErrActivityUnavailable
		}
		_, err = s.SQL().ExecContext(ctx, `UPDATE instance_model_activity SET last_gateway_activity_at=GREATEST(COALESCE(last_gateway_activity_at,?),?),activity_version=activity_version+1,updated_at=? WHERE instance_id=? AND generation=?`, now, now, now, id, generation)
		return err
	}, nil)
}

func (r *InstanceAutoStopRepository) FinishModelActivity(ctx context.Context, key string) error {
	if key == "" {
		return nil
	}
	id, generation, err := modelActivityRequestScope(ctx, r.sess, key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return r.sess.TxContext(ctx, func(s db.Session) error {
		if _, err := lockActivityInstance(ctx, s, id); err != nil {
			return err
		}
		row, err := s.SQL().QueryRowContext(ctx, `SELECT started_at,last_heartbeat_at,lease_expires_at,finished_at FROM instance_model_activity_requests WHERE request_id=? FOR UPDATE`, key)
		if err != nil {
			return err
		}
		var started time.Time
		var heartbeat, lease, finished sql.NullTime
		if err = row.Scan(&started, &heartbeat, &lease, &finished); err != nil {
			return err
		}
		if finished.Valid {
			return nil
		}
		now := time.Now().UTC()
		stamp, reason := now, "completed"
		expired := lease.Valid && !lease.Time.After(now)
		if expired {
			stamp, reason = started, "lease_expired"
			if heartbeat.Valid {
				stamp = heartbeat.Time
			}
		}
		if _, err = s.SQL().ExecContext(ctx, `UPDATE instance_model_activity_requests SET finished_at=?,completion_reason=? WHERE request_id=? AND finished_at IS NULL`, stamp, reason, key); err != nil {
			return err
		}
		if expired {
			_, err = s.SQL().ExecContext(ctx, `UPDATE instance_model_activity SET activity_version=activity_version+1,updated_at=? WHERE instance_id=? AND generation=?`, now, id, generation)
		} else {
			_, err = s.SQL().ExecContext(ctx, `UPDATE instance_model_activity SET last_gateway_activity_at=GREATEST(COALESCE(last_gateway_activity_at,?),?),activity_version=activity_version+1,updated_at=? WHERE instance_id=? AND generation=?`, now, now, now, id, generation)
		}
		return err
	}, nil)
}

func (r *InstanceAutoStopRepository) HasLegacyModelActivities(ctx context.Context) (bool, error) {
	row, err := r.sess.SQL().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM instance_model_activity_requests WHERE finished_at IS NULL AND lease_expires_at IS NULL)`)
	if err != nil {
		return false, err
	}
	var exists bool
	err = row.Scan(&exists)
	return exists, err
}

// legacyCutoff must come from proof that the old application processes exited.
// Expiry is a recovery event, never new model activity or a new idle window.
func (r *InstanceAutoStopRepository) ReconcileModelActivities(ctx context.Context, legacyCutoff *time.Time) error {
	var cutoff any
	if legacyCutoff != nil {
		cutoff = legacyCutoff.UTC()
	}
	now := time.Now().UTC()
	const eligible = `finished_at IS NULL AND ((lease_expires_at IS NOT NULL AND lease_expires_at<=?) OR (lease_expires_at IS NULL AND started_at<?))`
	after := 0
	for {
		rows, err := r.sess.SQL().QueryContext(ctx, `SELECT DISTINCT instance_id FROM instance_model_activity_requests WHERE instance_id>? AND `+eligible+` ORDER BY instance_id LIMIT 100`, after, now, cutoff)
		if err != nil {
			return err
		}
		var ids []int
		for rows.Next() {
			var id int
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		for _, id := range ids {
			err = r.sess.TxContext(ctx, func(s db.Session) error {
				i, err := lockActivityInstance(ctx, s, id)
				if errors.Is(err, sql.ErrNoRows) {
					return nil
				}
				if err != nil {
					return err
				}
				res, err := s.SQL().ExecContext(ctx, `UPDATE instance_model_activity_requests SET completion_reason=IF(lease_expires_at IS NULL,'legacy_process_exited','lease_expired'),finished_at=COALESCE(last_heartbeat_at,started_at) WHERE instance_id=? AND `+eligible, id, now, cutoff)
				if err != nil {
					return err
				}
				n, err := res.RowsAffected()
				if err != nil || n == 0 {
					return err
				}
				_, err = s.SQL().ExecContext(ctx, `UPDATE instance_model_activity SET activity_version=activity_version+1,updated_at=? WHERE instance_id=? AND generation=?`, now, id, i.generation)
				return err
			}, nil)
			if err != nil {
				return err
			}
			after = id
		}
	}
}
