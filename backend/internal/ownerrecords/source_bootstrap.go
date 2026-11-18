package ownerrecords

import (
	"context"
	"database/sql"
	"time"
)

// One bounded day per pass, newest first. The cursor advances only after the
// aggregate commits; a restart may replay that day safely, never add it twice.
func (s *Store) bootstrapSource(ctx context.Context, now time.Time) (bool, error) {
	epoch, e := s.analyticsEpoch(ctx)
	if e != nil {
		return false, e
	}
	if epoch == nil {
		v, e := s.prepareAnalyticsEpoch(ctx, now)
		if e != nil {
			return false, e
		}
		epoch = &v
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	local := epoch.Start.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	key := Key("source-bootstrap", "v4")
	row, e := s.Session.SQL().QueryRowContext(ctx, `SELECT interval_start,interval_end,status FROM northbound_owner_records WHERE record_type='source_bootstrap' AND record_key=?`, key)
	if e != nil {
		return false, e
	}
	var first, cursor time.Time
	var state string
	e = row.Scan(&first, &cursor, &state)
	if e == sql.ErrNoRows {
		row, e = s.Session.SQL().QueryRowContext(ctx, `SELECT MIN(created_at) FROM model_invocations`)
		if e != nil {
			return false, e
		}
		var oldest sql.NullTime
		if e = row.Scan(&oldest); e != nil {
			return false, e
		}
		first = today
		if oldest.Valid {
			at := oldest.Time.In(loc)
			first = time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, loc)
		}
		cursor = today
		_, e = s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,interval_start,interval_end) VALUES('source_bootstrap',?,0,'initializing',?,?) ON DUPLICATE KEY UPDATE record_key=record_key`, key, first.UTC(), cursor.UTC())
		if e != nil {
			return false, e
		}
	} else if e != nil {
		return false, e
	}
	if state == "done" {
		return true, nil
	}
	end := cursor.AddDate(0, 0, 1)
	if end.After(epoch.Start) {
		end = epoch.Start.Truncate(summaryInterval)
	}
	if end.After(cursor) {
		if _, e = s.aggregateSourceDay(ctx, cursor, end); e != nil {
			return false, e
		}
	}
	done := !cursor.After(first)
	state = "initializing"
	if done {
		state = "done"
	}
	_, e = s.Session.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET interval_end=?,status=?,version=version+1 WHERE record_type='source_bootstrap' AND record_key=?`, cursor.AddDate(0, 0, -1).UTC(), state, key)
	if e != nil {
		return false, e
	}
	return done, e
}

func (s *Store) refreshSource(ctx context.Context, now time.Time) error {
	today := sourceLocalDay(now)
	end := now.Truncate(summaryInterval)
	first := today.AddDate(0, 0, -1)
	epoch, err := s.analyticsEpoch(ctx)
	if err != nil {
		return err
	}
	if epoch != nil {
		first = sourceLocalDay(epoch.Start)
	}
	cursors, err := s.sourceDayCursors(ctx, first)
	if err != nil {
		return err
	}
	return runSourceLanes(ctx, []sourceLane{
		{budget: 15 * time.Second, run: func(work context.Context) error {
			// Publish the newest closed numeric bucket before doing any backfill.
			// aggregateSourceWindow cannot advance a cursor across a preceding gap.
			if end.After(today) {
				if _, e := s.aggregateSourceWindow(work, end.Add(-summaryInterval), end); e != nil {
					return e
				}
			}
			return s.refreshSourceDays(work, today, today.AddDate(0, 0, 1), end, cursors)
		}},
		{budget: 30 * time.Second, run: func(work context.Context) error {
			return s.refreshSourceDays(work, first, today, end, cursors)
		}},
	})
}

// Read the small cursor metadata set once, not the invocation table once per
// missed day. A missing day is still a gap, not implicit evidence of no usage.
func (s *Store) sourceDayCursors(ctx context.Context, first time.Time) (map[string]time.Time, error) {
	rows, err := s.Session.SQL().QueryContext(ctx, `SELECT record_key,interval_end FROM northbound_owner_records WHERE record_type='source_day_cursor' AND status='ready' AND interval_end>=?`, first.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cursors := map[string]time.Time{}
	for rows.Next() {
		var key string
		var end time.Time
		if err = rows.Scan(&key, &end); err != nil {
			return nil, err
		}
		cursors[key] = end
	}
	return cursors, rows.Err()
}

func (s *Store) refreshSourceDays(ctx context.Context, first, until, end time.Time, cursors map[string]time.Time) error {
	units := 0
	for day := first; day.Before(until) && day.Before(end); day = day.AddDate(0, 0, 1) {
		target := day.AddDate(0, 0, 1)
		if target.After(end) {
			target = end
		}
		cursor := cursors[Key("source-day-cursor", day.Format("2006-01-02"))]
		if cursor.Before(day) {
			cursor = day
		}
		cursor = cursor.Truncate(summaryInterval)
		for cursor.Before(target) {
			if units >= 144 || !scanHasBudget(ctx) {
				return errSyncYield
			}
			if _, e := s.aggregateSourceWindow(ctx, cursor, cursor.Add(summaryInterval)); e != nil {
				return e
			}
			cursor = cursor.Add(summaryInterval)
			units++
		}
	}
	return nil
}
