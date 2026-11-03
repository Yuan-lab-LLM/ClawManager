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
	loc, _ := time.LoadLocation("Asia/Shanghai")
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	end := now.Truncate(summaryInterval)
	if end.After(today) {
		if _, e := s.aggregateSourceDay(ctx, today, end); e != nil {
			return e
		}
	}
	if _, e := s.aggregateSourceDay(ctx, today.AddDate(0, 0, -1), today); e != nil {
		return e
	}
	// A small rotating repair window handles late changes without rescanning all
	// owners/history in every pass. Historical dates outside it remain explicit
	// backfill work, never a request-triggered full scan.
	day := today.AddDate(0, 0, -2-int(now.Unix()/int64(summaryInterval/time.Second))%29)
	_, e := s.aggregateSourceDay(ctx, day, day.AddDate(0, 0, 1))
	return e
}
