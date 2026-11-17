package historymaintenance

import (
	"context"
	"time"
)

// Cancelled payloads have their own retention proof and are never represented as
// delivered. Permanent cancellation identities remain after bounded reclamation.
// Tick owns the backup work unit and includes these deletes in its 500-row limit.
// Indexed equality joins also require byte-exact guards under Unicode CI collation.
func (s *Service) pruneCancelledOutbox(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	if limit <= 0 {
		return 0, nil
	}
	limit = min(limit, BatchLimit)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT o.id FROM team_event_outbox o JOIN teams t ON t.id=o.team_id JOIN history_outbox_cancellations c ON c.team_id=o.team_id AND c.destination=o.destination AND c.message_id=o.message_id AND BINARY c.destination=BINARY o.destination AND BINARY c.message_id=BINARY o.message_id JOIN history_replay_closures closure ON closure.team_id=o.team_id WHERE o.status='cancelled' AND o.delivered_at IS NULL AND t.status='deleted' AND c.source_event_id=o.source_event_id AND BINARY c.source_event_id=BINARY o.source_event_id AND c.payload_sha256=SHA2(o.payload_json,256) AND BINARY c.payload_sha256=BINARY SHA2(o.payload_json,256) AND c.cancelled_at<? AND c.reason<>'' AND c.closure_sha256 REGEXP '^[0-9a-f]{64}$' AND BINARY c.closure_sha256=BINARY LOWER(c.closure_sha256) AND c.closure_sha256=closure.proof_sha256 AND BINARY c.closure_sha256=BINARY closure.proof_sha256 AND closure.replay_closed=TRUE AND closure.recovery_closed=TRUE AND closure.idempotency_durable=TRUE AND o.created_at<closure.closed_before AND c.cancelled_at<closure.closed_before ORDER BY o.id LIMIT ? FOR UPDATE`, cutoff, limit)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		if _, err = tx.ExecContext(ctx, `DELETE FROM team_event_outbox WHERE id=? AND status='cancelled'`, id); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return int64(len(ids)), nil
}
