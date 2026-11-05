package repository

import (
	"clawreef/internal/models"
	"context"
	"github.com/upper/db/v4"
	"strings"
	"time"
)

type InstanceAutoStopPolicyStatus struct {
	InstanceID, Generation                              int
	PodUID, DefaultsPolicyVersion, DefaultsPolicyStatus string
}

// Report the current defaults without turning provenance into a new stop rule.
func (r *InstanceAutoStopRepository) RecordDefaultsPolicy(ctx context.Context, p InstanceAutoStopPolicyStatus) error {
	return r.sess.TxContext(ctx, func(s db.Session) error {
		i, err := lockActivityInstance(ctx, s, p.InstanceID)
		if err != nil {
			return err
		}
		if i.mode != "lite" || i.generation != p.Generation {
			return models.ErrAutoStopConflict
		}
		_, err = s.SQL().ExecContext(ctx, `INSERT INTO instance_auto_stop_policy_status(instance_id,generation,pod_uid,defaults_policy_version,defaults_policy_status,updated_at) VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE generation=VALUES(generation),pod_uid=VALUES(pod_uid),defaults_policy_version=VALUES(defaults_policy_version),defaults_policy_status=VALUES(defaults_policy_status),updated_at=VALUES(updated_at)`, p.InstanceID, p.Generation, p.PodUID, p.DefaultsPolicyVersion, p.DefaultsPolicyStatus, time.Now().UTC())
		return err
	}, nil)
}
func (r *instanceRepository) hydrateAutoStopPolicyStatus(items []*models.Instance) error {
	byID := map[int]*models.Instance{}
	args := []any{}
	marks := []string{}
	for _, i := range items {
		if i == nil || i.InstanceMode != "lite" {
			continue
		}
		i.DefaultsPolicyStatus = "unknown"
		byID[i.ID] = i
		args = append(args, i.ID)
		marks = append(marks, "?")
	}
	if len(args) == 0 {
		return nil
	}
	rows, err := r.sess.SQL().QueryContext(context.Background(), `SELECT p.instance_id,p.defaults_policy_version,p.defaults_policy_status FROM instance_auto_stop_policy_status p JOIN instances i ON i.id=p.instance_id AND i.runtime_generation=p.generation WHERE p.instance_id IN (`+strings.Join(marks, ",")+`)`, args...)
	if missingAutoStopTable(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var version, status string
		if err = rows.Scan(&id, &version, &status); err != nil {
			return err
		}
		if i := byID[id]; i != nil {
			i.DefaultsPolicyVersion = version
			i.DefaultsPolicyStatus = status
		}
	}
	return rows.Err()
}
