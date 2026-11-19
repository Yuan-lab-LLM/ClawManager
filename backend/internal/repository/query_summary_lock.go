package repository

import (
	"clawreef/internal/models"
	"context"
	"database/sql"
	"github.com/upper/db/v4"
	"sort"
	"strings"
	"time"
)

// Take the shared fence before the source INSERT, too: otherwise a rebuild's
// locking INSERT...SELECT could wait for that uncommitted source while its
// writer waits for the rebuild. Include the adjacent second for source columns
// whose SQL timestamp precision can round across a UTC day boundary.
func lockObservationSourceDays(ctx context.Context, tx db.Session, value interface{}) error {
	var days []time.Time
	switch v := value.(type) {
	case *models.ModelInvocation:
		days = []time.Time{v.CreatedAt, v.CreatedAt.Add(time.Second)}
	case *models.CostRecord:
		days = []time.Time{v.RecordedAt, v.RecordedAt.Add(time.Second)}
		if v.InvocationID != nil {
			row, err := tx.SQL().QueryRowContext(ctx, `SELECT created_at FROM observation_invocation_summaries WHERE id=?`, *v.InvocationID)
			if err != nil {
				return err
			}
			var at time.Time
			if err = row.Scan(&at); err == nil {
				days = append(days, at)
			} else if err != sql.ErrNoRows {
				return err
			}
		}
	default:
		return nil
	}
	return lockSummaryDays(ctx, tx, false, days...)
}

// Ordinary deliveries share day locks and never serialize each other. A rebuild
// owns the day exclusively before reading receipts or replacing sums. The locks
// live until transaction commit; reporting performs no locking reads.
// Fixed slots avoid creating lock rows (exclusive inserts) on the writer path.
// Days 256 apart share a rebuild fence; this only delays them during repair.
func lockSummaryDays(ctx context.Context, tx db.Session, exclusive bool, days ...time.Time) error {
	keys := map[int64]bool{}
	for _, day := range days {
		if !day.IsZero() {
			keys[(day.UTC().Truncate(24*time.Hour).Unix()/86400%256+256)%256] = true
		}
	}
	sorted := make([]int64, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	for _, day := range sorted {
		suffix := " FOR SHARE"
		if exclusive {
			suffix = " FOR UPDATE"
		}
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT day_slot FROM observation_summary_day_locks WHERE day_slot=?`+suffix, day)
		if err != nil {
			return err
		}
		var existing int64
		if err = row.Scan(&existing); err != nil {
			return err
		}
	}
	return nil
}

func lockCostSummaryDays(ctx context.Context, tx db.Session, ids []interface{}) error {
	if len(ids) == 0 {
		return nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	rows, err := tx.SQL().QueryContext(ctx, `SELECT recorded_at FROM cost_records WHERE id IN (`+marks+`) UNION SELECT mi.created_at FROM observation_cost_facts f JOIN observation_invocation_summaries mi ON mi.id=f.invocation_id WHERE f.id IN (`+marks+`) UNION SELECT mi.created_at FROM cost_records cr JOIN observation_invocation_summaries mi ON mi.id=cr.invocation_id WHERE cr.id IN (`+marks+`)`, append(append(append([]interface{}{}, ids...), ids...), ids...)...)
	if err != nil {
		return err
	}
	var days []time.Time
	for rows.Next() {
		var day time.Time
		if err = rows.Scan(&day); err != nil {
			rows.Close()
			return err
		}
		days = append(days, day)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	return lockSummaryDays(ctx, tx, false, days...)
}
