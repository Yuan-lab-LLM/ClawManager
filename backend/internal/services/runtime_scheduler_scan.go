package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"context"
)

type runtimeSchedulerScan struct{ afterID, throughID int }

func (s *RuntimeScheduler) schedulerInstanceBatch(ctx context.Context, creating bool) ([]models.Instance, error) {
	pager, ok := s.instanceRepo.(repository.RuntimeSchedulerInstancePager)
	if !ok {
		if creating {
			return s.instanceRepo.GetV2Creating(ctx, runtimeSchedulerBatchLimit)
		}
		return s.instanceRepo.GetV2DesiredRunning(ctx, runtimeSchedulerBatchLimit)
	}
	cursor := &s.desiredScan
	if creating {
		cursor = &s.creatingScan
	}
	rows, upper, err := pager.GetV2SchedulerPage(ctx, creating, cursor.afterID, cursor.throughID, runtimeSchedulerBatchLimit)
	if err != nil {
		return nil, err
	}
	cursor.throughID = upper
	if len(rows) == 0 {
		*cursor = runtimeSchedulerScan{}
		return rows, nil
	}
	cursor.afterID = rows[len(rows)-1].ID
	// Each sweep has a fixed high watermark. New arrivals join the next sweep
	// so a continuous stream of inserts cannot indefinitely postpone wrapping.
	if cursor.afterID >= upper || len(rows) < runtimeSchedulerBatchLimit {
		*cursor = runtimeSchedulerScan{}
	}
	return rows, nil
}
