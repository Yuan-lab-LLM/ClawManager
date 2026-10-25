package ownerrecords

import (
	"context"
	"time"
)

func (s *Store) taskCounts(ctx context.Context, owner string, started, daily, now time.Time) (map[string]uint64, error) {
	result := map[string]uint64{}
	v4 := false
	if epoch, e := s.analyticsEpoch(ctx); e != nil {
		return nil, e
	} else if epoch != nil {
		v4 = true
		started = time.Date(1000, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	rows, err := s.factRows(ctx, `SELECT record_type,SUM(CASE WHEN record_type='invocation' THEN total_tokens WHEN record_type IN ('activity_tick','activity_window') THEN active_work_seconds ELSE 1 END),SUM(CASE WHEN occurred_at>=? THEN CASE WHEN record_type='invocation' THEN total_tokens WHEN record_type IN ('activity_tick','activity_window') THEN active_work_seconds ELSE 1 END ELSE 0 END) FROM northbound_owner_records WHERE owner_normalized=? AND record_type IN ('invocation','question','login_day','activity_tick','activity_window') AND occurred_at>=? AND occurred_at<=? GROUP BY record_type`, daily.UTC(), owner, started.UTC(), now.UTC())
	if err != nil {
		return nil, err
	}
	metrics := map[string]string{"invocation": "tokens", "question": "questions", "login_day": "login", "activity_tick": "seconds", "activity_window": "seconds"}
	for rows.Next() {
		var kind string
		var all, day uint64
		if err = rows.Scan(&kind, &all, &day); err != nil {
			rows.Close()
			return nil, err
		}
		result["all:"+metrics[kind]] += all
		result["day:"+metrics[kind]] += day
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if v4 {
		return result, nil
	}
	row, err := s.factRow(ctx, `SELECT COUNT(*) FROM(SELECT business_date FROM northbound_owner_records WHERE record_type='xp_event' AND owner_normalized=? AND business_date>=DATE(DATE_ADD(?,INTERVAL 8 HOUR)) AND business_date<=DATE(DATE_ADD(?,INTERVAL 8 HOUR)) AND definition_version IN ('growth-v2','growth-v3') GROUP BY business_date HAVING SUM(xp_value)=100) d`, owner, started.UTC(), now.UTC())
	if err != nil {
		return nil, err
	}
	var full uint64
	if err = row.Scan(&full); err != nil {
		return nil, err
	}
	result["all:full_days"] = full
	return result, nil
}
