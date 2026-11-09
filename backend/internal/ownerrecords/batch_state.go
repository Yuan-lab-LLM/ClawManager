package ownerrecords

import (
	"context"
	"time"
)

// One small shared status record per batch, not one heartbeat per owner.
func (s *Store) batchState(ctx context.Context, now time.Time, state string) error {
	_, err := s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,occurred_at,interval_end,version) VALUES('summary_batch',?,0,?,?,IF(?='ready',?,NULL),1) ON DUPLICATE KEY UPDATE status=VALUES(status),occurred_at=VALUES(occurred_at),interval_end=IF(VALUES(status)='ready',VALUES(interval_end),interval_end),version=version+1`, Key("summary-batch", "v4"), state, now.UTC(), state, now.Truncate(summaryInterval).UTC())
	return err
}
