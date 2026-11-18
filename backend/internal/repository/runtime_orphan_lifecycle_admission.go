package repository

import (
	"context"
	"fmt"
	"github.com/upper/db/v4"
	"strconv"
)

// LockRuntimeOrphanLifecycleAdmission uses the same producer lock as queued and
// batch lifecycle creation. Caller takes it before management/instance locks.
func LockRuntimeOrphanLifecycleAdmission(ctx context.Context, tx db.Session) error {
	return lockLifecycleScheduler(ctx, tx)
}

// CheckRuntimeOrphanLifecycleReservations never inserts a temporary guard.
// The caller keeps the producer lock through atomic orphan journal/guard commit.
func CheckRuntimeOrphanLifecycleReservations(ctx context.Context, tx db.Session, ids []int) error {
	seen := map[int]bool{}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		reserved, e := lifecycleRetryReservation(tx, id)
		if e != nil {
			return e
		}
		if reserved {
			return fmt.Errorf("source instance %d awaits lifecycle batch retry", id)
		}
		var count int
		row, e := tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM northbound_operations WHERE status IN ('queued','processing','batch_pending') AND (instance_id=? OR JSON_UNQUOTE(JSON_EXTRACT(request_payload,'$.instance_id'))=?)`, id, strconv.Itoa(id))
		if e != nil {
			return e
		}
		if e = row.Scan(&count); e != nil {
			return e
		}
		if count > 0 {
			return fmt.Errorf("source instance %d has an unfinished queued lifecycle", id)
		}
		row, e = tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_lifecycle_batch_items i JOIN northbound_operations o ON o.operation_id=i.operation_id COLLATE utf8mb4_unicode_ci WHERE i.state IN ('pending','active') AND (i.source_id=? OR o.instance_id=?)`, id, id)
		if e != nil {
			return e
		}
		if e = row.Scan(&count); e != nil {
			return e
		}
		if count > 0 {
			return fmt.Errorf("source instance %d has an active lifecycle batch reservation", id)
		}
	}
	return nil
}
