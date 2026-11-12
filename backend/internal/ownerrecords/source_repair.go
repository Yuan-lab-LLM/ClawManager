package ownerrecords

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/upper/db/v4"
)

// IDs discover candidates only, never certify source completeness. A rotating
// time-window repair also revisits old rows and out-of-order commits/corrections.
// Nothing is inserted into the inference request's critical path.
func (s *Store) discoverSourceWindows(ctx context.Context, now time.Time) error {
	return s.Session.TxContext(ctx, func(tx db.Session) error {
		key := Key("source-discovery", "v1")
		row, e := tx.SQL().QueryRowContext(ctx, `SELECT version FROM northbound_owner_records WHERE record_type='source_change_cursor' AND record_key=? FOR UPDATE`, key)
		if e != nil {
			return e
		}
		var after uint64
		e = row.Scan(&after)
		if e == sql.ErrNoRows {
			row, e = tx.SQL().QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM model_invocations`)
			if e != nil {
				return e
			}
			if e = row.Scan(&after); e != nil {
				return e
			}
			_, e = tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,version) VALUES('source_change_cursor',?,0,'ready',?) ON DUPLICATE KEY UPDATE record_key=record_key`, key, after)
			return e
		}
		if e != nil {
			return e
		}
		rows, e := tx.SQL().QueryContext(ctx, `SELECT id,created_at FROM model_invocations WHERE id>? ORDER BY id LIMIT 1000`, after)
		if e != nil {
			return e
		}
		buckets := map[time.Time]bool{}
		last := after
		for rows.Next() {
			var id uint64
			var at time.Time
			if e = rows.Scan(&id, &at); e != nil {
				rows.Close()
				return e
			}
			last = id
			if !at.Before(now.AddDate(0, 0, -31)) && !at.After(now) {
				buckets[at.Truncate(summaryInterval)] = true
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for at := range buckets {
			_, e = tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,interval_start,interval_end,version) VALUES('source_window_dirty',?,0,'pending',?,?,1) ON DUPLICATE KEY UPDATE status='pending',version=version+1`, Key("source-window", at.UTC().Format(time.RFC3339)), at.UTC(), at.Add(summaryInterval).UTC())
			if e != nil {
				return e
			}
		}
		_, e = tx.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET version=? WHERE record_type='source_change_cursor' AND record_key=?`, last, key)
		return e
	}, nil)
}

func (s *Store) repairSource(ctx context.Context, now time.Time) error {
	return runSourceLanes(ctx, []sourceLane{
		{budget: 15 * time.Second, run: func(work context.Context) error { return s.repairDiscoveredSource(work, now) }},
		{budget: 10 * time.Second, run: func(work context.Context) error { return s.repairLatestSource(work, now) }},
		{budget: 15 * time.Second, run: func(work context.Context) error { return s.repairRecentSource(work, now) }},
		{budget: 20 * time.Second, run: func(work context.Context) error { return s.repairOlderSource(work, now) }},
	})
}

func (s *Store) repairDiscoveredSource(ctx context.Context, now time.Time) error {
	if e := s.discoverSourceWindows(ctx, now); e != nil {
		return e
	}
	rows, e := s.Session.SQL().QueryContext(ctx, `SELECT record_key,interval_start,version FROM northbound_owner_records WHERE record_type='source_window_dirty' AND status='pending' AND interval_end<=? ORDER BY id LIMIT 24`, now.UTC())
	if e != nil {
		return e
	}
	type window struct {
		key     string
		at      time.Time
		version uint64
	}
	var windows []window
	for rows.Next() {
		var w window
		if e = rows.Scan(&w.key, &w.at, &w.version); e != nil {
			rows.Close()
			return e
		}
		windows = append(windows, w)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, w := range windows {
		if !scanHasBudget(ctx) {
			return errSyncYield
		}
		if _, e = s.aggregateSourceWindow(ctx, w.at, w.at.Add(summaryInterval)); e != nil {
			return e
		}
		_, e = s.Session.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET status='done' WHERE record_type='source_window_dirty' AND record_key=? AND version=?`, w.key, w.version)
		if e != nil {
			return e
		}
	}
	if len(windows) == 24 {
		row, err := s.Session.SQL().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM northbound_owner_records WHERE record_type='source_window_dirty' AND status='pending' AND interval_end<=?)`, now.UTC())
		if err != nil {
			return err
		}
		var pending bool
		if err = row.Scan(&pending); err != nil {
			return err
		}
		if pending {
			return errSyncYield
		}
	}
	return nil
}

func (s *Store) repairLatestSource(ctx context.Context, now time.Time) error {
	// Revisit the three latest closed buckets every round: terminal status and
	// token corrections may update an existing ID, not insert a new one.
	for n := 1; n <= 3; n++ {
		if !scanHasBudget(ctx) {
			return errSyncYield
		}
		at := now.Truncate(summaryInterval).Add(-time.Duration(n) * summaryInterval)
		if _, e := s.aggregateSourceWindow(ctx, at, at.Add(summaryInterval)); e != nil {
			return e
		}
	}
	return nil
}

func (s *Store) repairOlderSource(ctx context.Context, now time.Time) error {
	// Short, fixed repair slices rather than a full old-day transaction. The
	// durable cursor advances only after successful replacement. Current day is
	// included so a late lower-ID commit is eventually observed as well.
	key := Key("source-repair", "v1")
	today := sourceLocalDay(now)
	row, e := s.Session.SQL().QueryRowContext(ctx, `SELECT interval_start FROM northbound_owner_records WHERE record_type='source_repair_cursor' AND record_key=?`, key)
	if e != nil {
		return e
	}
	cursor := today.AddDate(0, 0, -1)
	if e = row.Scan(&cursor); e != nil && e != sql.ErrNoRows {
		return e
	}
	if cursor.Before(today.AddDate(0, 0, -30)) || cursor.After(now) {
		cursor = today.AddDate(0, 0, -1)
	}
	for n := 0; n < 24; n++ {
		if !scanHasBudget(ctx) {
			return errSyncYield
		}
		if cursor.Add(summaryInterval).After(now.Truncate(summaryInterval)) {
			cursor = today.AddDate(0, 0, -1)
		}
		if _, e = s.aggregateSourceWindow(ctx, cursor, cursor.Add(summaryInterval)); e != nil {
			return e
		}
		next := cursor.Add(summaryInterval)
		if !next.Before(sourceLocalDay(cursor).AddDate(0, 0, 1)) {
			next = sourceLocalDay(cursor).AddDate(0, 0, -1)
			if next.Before(today.AddDate(0, 0, -30)) {
				next = today
			}
		}
		_, e = s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,interval_start) VALUES('source_repair_cursor',?,0,'ready',?) ON DUPLICATE KEY UPDATE interval_start=VALUES(interval_start),version=version+1`, key, next.UTC())
		if e != nil {
			return fmt.Errorf("repair cursor: %w", e)
		}
		cursor = next
	}
	return nil
}

// A separate hot cursor guarantees coverage of today/yesterday without waiting
// for the deeper 30-day repair cycle. Both cursors are durable and bounded.
func (s *Store) repairRecentSource(ctx context.Context, now time.Time) error {
	key := Key("source-repair", "recent-v1")
	end := now.Truncate(summaryInterval)
	start := sourceLocalDay(now).AddDate(0, 0, -1)
	cursor := start
	row, e := s.Session.SQL().QueryRowContext(ctx, `SELECT interval_start FROM northbound_owner_records WHERE record_type='source_repair_cursor' AND record_key=?`, key)
	if e != nil {
		return e
	}
	if e = row.Scan(&cursor); e != nil && e != sql.ErrNoRows {
		return e
	}
	if cursor.Before(start) || !cursor.Before(end) {
		cursor = start
	}
	for n := 0; n < 24; n++ {
		if !scanHasBudget(ctx) {
			return errSyncYield
		}
		if _, e = s.aggregateSourceWindow(ctx, cursor, cursor.Add(summaryInterval)); e != nil {
			return e
		}
		cursor = cursor.Add(summaryInterval)
		if !cursor.Before(end) {
			cursor = start
		}
		_, e = s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,interval_start) VALUES('source_repair_cursor',?,0,'ready',?) ON DUPLICATE KEY UPDATE interval_start=VALUES(interval_start),version=version+1`, key, cursor.UTC())
		if e != nil {
			return e
		}
	}
	return nil
}
