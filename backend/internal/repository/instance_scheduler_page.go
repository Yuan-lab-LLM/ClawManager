package repository

import (
	"clawreef/internal/models"
	"context"
	"fmt"
	"strings"

	"github.com/upper/db/v4"
)

// CountRuntimeSlotDemand rechecks live, unbound Lite instances for a replica mutation.
func CountRuntimeSlotDemand(ctx context.Context, sess db.Session, runtimeType string) (int, error) {
	var count int
	row, err := sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instances i
		WHERE i.type=? AND i.runtime_type='gateway' AND i.instance_mode='lite'
		  AND i.status='creating' AND TRIM(i.workspace_path)<>''
		  AND LOWER(TRIM(COALESCE(i.description,''))) NOT LIKE 'openclaw-upgrade-lab:%'
		  AND NOT EXISTS (SELECT 1 FROM instance_runtime_bindings b WHERE b.instance_id=i.id)
		  AND NOT EXISTS (SELECT 1 FROM instance_lifecycle_manual_guards mg WHERE mg.instance_id=i.id AND mg.token LIKE 'runtime_mgmt_%')
		  AND NOT EXISTS (SELECT 1 FROM gateway_runtime_upgrades u WHERE u.instance_id=i.id AND u.state NOT IN ('cancelled','completed'))
		  AND NOT EXISTS (SELECT 1 FROM instance_desired_state ds WHERE ds.instance_id=i.id AND ds.desired_power_state<>'running')`, runtimeType)
	if err == nil {
		err = row.Scan(&count)
	}
	return count, err
}

// RuntimeSchedulerInstancePager is optional so older adapters remain compatible.
// throughID=0 starts a bounded keyset sweep; the returned watermark stays fixed
// until the scheduler finishes the sweep. Neither query uses OFFSET.
type RuntimeSchedulerInstancePager interface {
	GetV2SchedulerPage(ctx context.Context, creating bool, afterID, throughID, limit int) ([]models.Instance, int, error)
}

func (r *instanceRepository) GetV2SchedulerPage(ctx context.Context, creating bool, afterID, throughID, limit int) ([]models.Instance, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, throughID, err
	}
	if throughID <= 0 {
		row, err := r.sess.SQL().QueryRowContext(ctx, "SELECT COALESCE(MAX(id),0) FROM instances")
		if err != nil {
			return nil, 0, err
		}
		if err = row.Scan(&throughID); err != nil {
			return nil, 0, err
		}
	}
	statuses := v2DesiredRunningStatuses()
	if creating {
		statuses = []string{"creating"}
	}
	query, args := buildV2SchedulerInstancePageQuery(statuses, limit, afterID, throughID)
	var instances []models.Instance
	iter := r.sess.SQL().IteratorContext(ctx, query, args...)
	defer iter.Close()
	if err := iter.All(&instances); err != nil {
		return nil, throughID, fmt.Errorf("get scheduler instance page: %w", err)
	}
	if err := r.hydrateAutoStopSlice(instances); err != nil {
		return nil, throughID, err
	}
	return instances, throughID, nil
}

func buildV2SchedulerInstancePageQuery(statuses []string, limit, afterID, throughID int) (string, []any) {
	query, args := buildV2SchedulerInstanceQuery(statuses, limit)
	query = strings.Replace(query, "ORDER BY id", "AND instances.id > ? AND instances.id <= ?\n ORDER BY id", 1)
	last := args[len(args)-1]
	args = append(args[:len(args)-1], afterID, throughID, last)
	return query, args
}
