package ownerrecords

import (
	"context"
	"github.com/upper/db/v4"
	"time"
)

// Rebuild only derived daily rows. Facts remain authoritative; replacing an
// aggregate (rather than incrementing it on scans) makes backfill/replay safe.
func (s *Store) RefreshDaily(ctx context.Context, owner string, now time.Time) error {
	owner, err := normalizedOwner(owner)
	if err != nil {
		return err
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	return s.Session.TxContext(ctx, func(tx db.Session) error {
		head := Key("daily-head", owner)
		_, err := tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status) VALUES ('projection_head',?,0,?,'active') ON DUPLICATE KEY UPDATE record_key=record_key`, head, owner)
		if err != nil {
			return err
		}
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT id FROM northbound_owner_records WHERE record_type='projection_head' AND record_key=? FOR UPDATE`, head)
		if err != nil {
			return err
		}
		var id uint64
		if err = row.Scan(&id); err != nil {
			return err
		}
		rows, err := tx.SQL().QueryContext(ctx, `SELECT DATE_FORMAT(DATE_ADD(occurred_at,INTERVAL 8 HOUR),'%Y-%m-%d'),COALESCE(SUM(CASE WHEN record_type='invocation' THEN total_tokens ELSE 0 END),0),COALESCE(SUM(CASE WHEN record_type='invocation' THEN prompt_tokens ELSE 0 END),0),COALESCE(SUM(CASE WHEN record_type='invocation' THEN completion_tokens ELSE 0 END),0),SUM(record_type='question'),SUM(record_type='session') FROM northbound_owner_records WHERE owner_normalized=? AND record_type IN ('invocation','question','session') AND occurred_at>=? AND occurred_at<=? GROUP BY 1`, owner, now.AddDate(-2, 0, 0).UTC(), now.UTC())
		if err != nil {
			return err
		}
		var days []UsageDay
		for rows.Next() {
			var d UsageDay
			if err = rows.Scan(&d.Date, &d.Tokens, &d.Prompt, &d.Completion, &d.Questions, &d.Sessions); err != nil {
				rows.Close()
				return err
			}
			days = append(days, d)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, d := range days {
			_, err = tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,business_date,total_tokens,prompt_tokens,completion_tokens,question_count,session_count,active_day_count,rule_version,occurred_at) VALUES ('daily_usage',?,0,?,'observed',?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE total_tokens=VALUES(total_tokens),prompt_tokens=VALUES(prompt_tokens),completion_tokens=VALUES(completion_tokens),question_count=VALUES(question_count),session_count=VALUES(session_count),active_day_count=VALUES(active_day_count),occurred_at=VALUES(occurred_at),version=version+1`, Key("daily", owner, d.Date, "v1"), owner, d.Date, d.Tokens, d.Prompt, d.Completion, d.Questions, d.Sessions, func() int {
				if d.Questions > 0 {
					return 1
				}
				return 0
			}(), "v1", now.UTC())
			if err != nil {
				return err
			}
		}
		_, err = tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,business_date,occurred_at,payload) VALUES ('coverage',?,0,?,'partial',?,?,JSON_OBJECT('source','confirmed-facts','unknown_dates','excluded','timezone','Asia/Shanghai')) ON DUPLICATE KEY UPDATE occurred_at=VALUES(occurred_at),payload=VALUES(payload)`, Key("coverage", owner, "v1"), owner, now.In(loc).Format("2006-01-02"), now.UTC())
		return err
	}, nil)
}

func (s *Store) RefreshOwnerProjections(ctx context.Context, after uint64) (uint64, error) {
	rows, err := s.factRows(ctx, `SELECT id,owner_normalized FROM northbound_owner_records WHERE record_type='growth_profile' AND id>? ORDER BY id LIMIT 5`, after)
	if err != nil {
		return after, err
	}
	type item struct {
		id    uint64
		owner string
	}
	var items []item
	for rows.Next() {
		var i item
		if err = rows.Scan(&i.id, &i.owner); err != nil {
			rows.Close()
			return after, err
		}
		items = append(items, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return after, err
	}
	if len(items) == 0 {
		return 0, nil
	}
	for _, i := range items {
		now := time.Now()
		if err = s.ReconcileGrowth(ctx, i.owner, now); err != nil {
			return after, err
		}
		if err = s.RefreshDaily(ctx, i.owner, now); err != nil {
			return after, err
		}
		loc, _ := time.LoadLocation("Asia/Shanghai")
		local := now.In(loc)
		if _, err = s.Report(ctx, i.owner, "week", local.AddDate(0, 0, -7).Format("2006-01-02"), now, true); err != nil {
			return after, err
		}
		lastMonth := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, 0, -1)
		if _, err = s.Report(ctx, i.owner, "month", lastMonth.Format("2006-01-02"), now, true); err != nil {
			return after, err
		}
		after = i.id
	}
	return after, nil
}
