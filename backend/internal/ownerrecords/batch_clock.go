package ownerrecords

import (
	"context"
	"time"
)

// All replicas and clients share wall-clock boundaries, not process start times.
func nextSummaryBatch(now time.Time) time.Time {
	return now.UTC().Truncate(summaryInterval).Add(summaryInterval)
}

func waitSummaryBatch(ctx context.Context) bool {
	now := time.Now()
	// Let serving replicas drain their bounded observation queues first.
	timer := time.NewTimer(nextSummaryBatch(now).Add(4 * time.Second).Sub(now))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

type SnapshotState struct {
	Version       uint64     `json:"version"`
	AsOf          *time.Time `json:"as_of"`
	NextRefreshAt time.Time  `json:"next_refresh_at"`
	State         string     `json:"state"`
}
