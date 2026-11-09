package ownerrecords

import (
	"context"
	"database/sql"
	"github.com/upper/db/v4"
	"time"
)

// Persist in the SAME transaction as the fact. Version-checked acknowledgement
// cannot discard facts committed while an owner/date is being processed.
func markUsageDay(ctx context.Context, tx db.Session, owner string, at time.Time) error {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	date := at.In(loc).Format("2006-01-02")
	_, err := tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,business_date,status,version,occurred_at) VALUES ('usage_dirty',?,0,?,?,'pending',1,?) ON DUPLICATE KEY UPDATE version=version+1,status='pending'`, Key("usage-dirty", owner, date), owner, date, at.UTC())
	return err
}

// Daily totals are rebuilt only for changed dates, not the owner's whole history.
// Empty old buckets are repaired when a session's first date moves backwards.
func (s *Store) refreshDailyDate(ctx context.Context, owner string, date, now time.Time) error {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	local := date.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)
	row, err := s.factRow(ctx, `SELECT COALESCE(SUM(CASE WHEN record_type='invocation' THEN total_tokens ELSE 0 END),0),COALESCE(SUM(CASE WHEN record_type='invocation' THEN prompt_tokens ELSE 0 END),0),COALESCE(SUM(CASE WHEN record_type='invocation' THEN completion_tokens ELSE 0 END),0),COALESCE(SUM(record_type='question'),0),COALESCE(SUM(record_type='session'),0),COUNT(*) FROM northbound_owner_records WHERE owner_normalized=? AND record_type IN ('invocation','question','session') AND occurred_at>=? AND occurred_at<?`, owner, start.UTC(), end.UTC())
	if err != nil {
		return err
	}
	var d UsageDay
	var count uint64
	if err = row.Scan(&d.Tokens, &d.Prompt, &d.Completion, &d.Questions, &d.Sessions, &count); err != nil {
		return err
	}
	key := Key("daily", owner, start.Format("2006-01-02"), "v1")
	old, err := s.factRow(ctx, `SELECT total_tokens,prompt_tokens,completion_tokens,question_count,session_count FROM northbound_owner_records WHERE record_type='daily_usage' AND record_key=?`, key)
	if err != nil {
		return err
	}
	var prev UsageDay
	err = old.Scan(&prev.Tokens, &prev.Prompt, &prev.Completion, &prev.Questions, &prev.Sessions)
	if err == nil && prev == d {
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == sql.ErrNoRows && count == 0 {
		return nil
	}
	active := 0
	if d.Questions > 0 {
		active = 1
	}
	_, err = s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,business_date,total_tokens,prompt_tokens,completion_tokens,question_count,session_count,active_day_count,rule_version,occurred_at) VALUES ('daily_usage',?,0,?,'observed',?,?,?,?,?,?,?,'v1',?) ON DUPLICATE KEY UPDATE total_tokens=VALUES(total_tokens),prompt_tokens=VALUES(prompt_tokens),completion_tokens=VALUES(completion_tokens),question_count=VALUES(question_count),session_count=VALUES(session_count),active_day_count=VALUES(active_day_count),occurred_at=VALUES(occurred_at),version=version+1`, key, owner, start.Format("2006-01-02"), d.Tokens, d.Prompt, d.Completion, d.Questions, d.Sessions, active, now.UTC())
	return err
}

type dirtyDay struct {
	key, owner string
	date       time.Time
	version    uint64
}

func (s *Store) pendingDays(ctx context.Context, owner string, limit int, now time.Time) ([]dirtyDay, error) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	query := `SELECT record_key,owner_normalized,business_date,version FROM northbound_owner_records WHERE record_type='usage_dirty' AND status='pending' AND business_date<=?`
	args := []any{now.In(loc).Format("2006-01-02")}
	if owner != "" {
		query += ` AND owner_normalized=?`
		args = append(args, owner)
	}
	// Oldest pending change first: constantly active owners cannot starve others.
	query += ` ORDER BY updated_at,id LIMIT ?`
	args = append(args, limit)
	rows, err := s.factRows(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []dirtyDay{}
	for rows.Next() {
		var d dirtyDay
		if err = rows.Scan(&d.key, &d.owner, &d.date, &d.version); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

func (s *Store) scheduleReports(ctx context.Context, d dirtyDay) error {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	day, err := time.ParseInLocation("2006-01-02", d.date.Format("2006-01-02"), loc)
	if err != nil {
		return err
	}
	for _, kind := range []string{"week", "month"} {
		start, end, err := reportWindow(kind, day.Format("2006-01-02"), day)
		if err != nil {
			return err
		}
		// The next period uses this period as its comparison baseline.
		for n := 0; n < 2; n++ {
			_, err = s.Session.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,period_type,period_start,expires_at,version) VALUES ('report_dirty',?,0,?,'pending',?,?,?,1) ON DUPLICATE KEY UPDATE status='pending',version=version+1`, Key("report-dirty", d.owner, kind, start.Format("2006-01-02")), d.owner, kind, start.Format("2006-01-02"), end.UTC())
			if err != nil {
				return err
			}
			start = end
			if kind == "week" {
				end = start.AddDate(0, 0, 7)
			} else {
				end = start.AddDate(0, 1, 0)
			}
		}
	}
	return nil
}

func (s *Store) processDirty(ctx context.Context, now time.Time) error {
	epoch, e := s.analyticsEpoch(ctx)
	if e != nil {
		return e
	}
	days, err := s.pendingDays(ctx, "", 40, now)
	if err != nil {
		return err
	}
	effective, err := s.growthEffectiveAt(ctx, now)
	if err != nil {
		return err
	}
	v3, err := s.growthV3EffectiveAt(ctx, now)
	if err != nil {
		return err
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	groups := map[string][]dirtyDay{}
	owners := []string{}
	for _, d := range days {
		if len(groups[d.owner]) == 0 {
			owners = append(owners, d.owner)
		}
		if len(groups[d.owner]) < 4 {
			groups[d.owner] = append(groups[d.owner], d)
		}
	}
	for _, owner := range owners {
		for _, d := range groups[owner] {
			day, err := time.ParseInLocation("2006-01-02", d.date.Format("2006-01-02"), loc)
			if err != nil {
				return err
			}
			if day.After(now) {
				continue
			}
			at := day.AddDate(0, 0, 1).Add(-time.Microsecond)
			if now.Before(at) {
				at = now
			}
			if epoch == nil || !at.Before(epoch.Start) {
				err = s.reconcileGrowthAt(ctx, d.owner, at, now, true, effective, v3)
			}
			if err != nil {
				return err
			}
			if err = s.refreshDailyDate(ctx, d.owner, day, now); err != nil {
				return err
			}
			if err = s.scheduleReports(ctx, d); err != nil {
				return err
			}
		}
		// Commit progress per owner, not per batch. A later timeout must not replay
		// unrelated owners. The next pass resumes persisted pending dates.
		if err = s.reconcileGrowthAt(ctx, owner, now, now, false, effective, v3); err != nil {
			return err
		}
		if epoch != nil {
			if _, err = s.buildOwnerSnapshot(ctx, owner, now, true); err != nil {
				return err
			}
		}
		for _, d := range groups[owner] {
			_, err = s.Session.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET status='done' WHERE record_type='usage_dirty' AND record_key=? AND version=? AND status='pending'`, d.key, d.version)
			if err != nil {
				return err
			}
		}
		s.InvalidateOwner(owner)
	}
	return s.processDueReports(ctx, now)
}

func (s *Store) processDueReports(ctx context.Context, now time.Time) error {
	rows, err := s.factRows(ctx, `SELECT record_key,owner_normalized,period_type,period_start,version FROM northbound_owner_records WHERE record_type='report_dirty' AND status='pending' AND expires_at<=? ORDER BY expires_at LIMIT 5`, now.UTC())
	if err != nil {
		return err
	}
	type job struct {
		key, owner, kind string
		date             time.Time
		version          uint64
	}
	jobs := []job{}
	for rows.Next() {
		var j job
		if err = rows.Scan(&j.key, &j.owner, &j.kind, &j.date, &j.version); err != nil {
			rows.Close()
			return err
		}
		jobs = append(jobs, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, j := range jobs {
		loc, _ := time.LoadLocation("Asia/Shanghai")
		if j.date.Format("2006-01-02") < now.In(loc).AddDate(-2, 0, 0).Format("2006-01-02") {
			_, err = s.Session.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET status='outside_report_range' WHERE record_type='report_dirty' AND record_key=? AND version=?`, j.key, j.version)
			if err != nil {
				return err
			}
			continue
		}
		if _, err = s.Report(ctx, j.owner, j.kind, j.date.Format("2006-01-02"), now, true); err != nil {
			return err
		}
		if _, err = s.Session.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET status='done' WHERE record_type='report_dirty' AND record_key=? AND version=?`, j.key, j.version); err != nil {
			return err
		}
	}
	return nil
}

// One-time resumable upgrade scan; ongoing events create their own dirty entries.
func (s *Store) bootstrapDirty(ctx context.Context) error {
	if s.perf.bootstrapDone {
		return nil
	}
	key := Key("usage-bootstrap", "v1")
	return s.Session.TxContext(ctx, func(tx db.Session) error {
		_, err := tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,version) VALUES ('usage_bootstrap',?,0,'pending',0) ON DUPLICATE KEY UPDATE record_key=record_key`, key)
		if err != nil {
			return err
		}
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT version,status FROM northbound_owner_records WHERE record_type='usage_bootstrap' AND record_key=? FOR UPDATE`, key)
		if err != nil {
			return err
		}
		var after uint64
		var status string
		if err = row.Scan(&after, &status); err != nil {
			return err
		}
		if status == "done" {
			s.perf.bootstrapDone = true
			return nil
		}
		rows, err := tx.SQL().QueryContext(ctx, `SELECT id,owner_normalized FROM northbound_owner_records WHERE record_type='growth_profile' AND id>? ORDER BY id LIMIT 5`, after)
		if err != nil {
			return err
		}
		type ownerRow struct {
			id    uint64
			owner string
		}
		owners := []ownerRow{}
		for rows.Next() {
			var o ownerRow
			if err = rows.Scan(&o.id, &o.owner); err != nil {
				rows.Close()
				return err
			}
			owners = append(owners, o)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, o := range owners {
			rows, err := tx.SQL().QueryContext(ctx, `SELECT MIN(occurred_at) FROM northbound_owner_records WHERE owner_normalized=? AND record_type IN ('invocation','question','session','login_day','activity_tick') AND occurred_at IS NOT NULL GROUP BY DATE(DATE_ADD(occurred_at,INTERVAL 8 HOUR))`, o.owner)
			if err != nil {
				return err
			}
			var dates []time.Time
			for rows.Next() {
				var at time.Time
				if err = rows.Scan(&at); err != nil {
					rows.Close()
					return err
				}
				dates = append(dates, at)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			for _, at := range dates {
				if err = markUsageDay(ctx, tx, o.owner, at); err != nil {
					return err
				}
			}
			after = o.id
		}
		if len(owners) == 0 {
			status = "done"
		}
		_, err = tx.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET version=?,status=? WHERE record_type='usage_bootstrap' AND record_key=?`, after, status, key)
		return err
	}, nil)
}
