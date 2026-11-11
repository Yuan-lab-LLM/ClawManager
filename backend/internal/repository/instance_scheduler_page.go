package repository

import (
	"clawreef/internal/models"
	"context"
	"fmt"
	"strings"
)

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
