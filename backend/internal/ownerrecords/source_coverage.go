package ownerrecords

import (
	"context"
	"time"
)

// A new projection or ranking does not make an old numeric source fresh.
func (s *Store) sourceAsOf(ctx context.Context, now time.Time) (time.Time, error) {
	epoch, err := s.analyticsEpoch(ctx)
	if err != nil {
		return time.Time{}, err
	}
	if epoch == nil {
		return now.Truncate(summaryInterval), nil
	}
	covered := epoch.Start.Truncate(summaryInterval)
	first := sourceLocalDay(epoch.Start)
	cursors, err := s.sourceDayCursors(ctx, first)
	if err != nil {
		return time.Time{}, err
	}
	for day := first; day.Before(now.Truncate(summaryInterval)); day = day.AddDate(0, 0, 1) {
		end, ok := cursors[Key("source-day-cursor", day.Format("2006-01-02"))]
		if !ok {
			return covered, nil
		}
		// Cursor metadata from an older deployment must not certify another day.
		if end.After(day.AddDate(0, 0, 1)) {
			end = day.AddDate(0, 0, 1)
		}
		if end.After(now.Truncate(summaryInterval)) {
			end = now.Truncate(summaryInterval)
		}
		if end.After(covered) {
			covered = end
		}
		// A repair of a later date is not proof that an earlier gap is filled.
		if end.Before(day.AddDate(0, 0, 1)) {
			return covered, nil
		}
	}
	return covered, nil
}
