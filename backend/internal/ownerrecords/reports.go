package ownerrecords

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/upper/db/v4"
	"time"
)

type RuntimeUsage struct {
	Runtime   string `json:"runtime"`
	Tokens    uint64 `json:"tokens"`
	Questions uint64 `json:"questions"`
}
type Report struct {
	Comparison     map[string]MetricComparison `json:"comparison,omitempty"`
	Kind           string                      `json:"kind"`
	Start          time.Time                   `json:"start"`
	End            time.Time                   `json:"end"`
	AsOf           time.Time                   `json:"as_of"`
	Ongoing        bool                        `json:"ongoing"`
	Counts         map[string]uint64           `json:"counts"`
	Previous       map[string]uint64           `json:"previous"`
	Days           []UsageDay                  `json:"days"`
	Runtime        []RuntimeUsage              `json:"runtime"`
	ActiveDays     int                         `json:"active_days"`
	ObservedDays   int                         `json:"observed_days"`
	Instances      uint64                      `json:"instances"`
	UsedSessions   uint64                      `json:"used_sessions"`
	Revision       uint64                      `json:"revision"`
	SavedRevisions []uint64                    `json:"saved_revisions"`
	Note           string                      `json:"note"`
}

func reportWindow(kind, date string, now time.Time) (time.Time, time.Time, error) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	start, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return start, start, fmt.Errorf("invalid report date")
	}
	if start.After(now) || start.Before(now.In(loc).AddDate(-2, 0, 0)) {
		return start, start, fmt.Errorf("report date outside supported range")
	}
	switch kind {
	case "week":
		start = start.AddDate(0, 0, -(int(start.Weekday())+6)%7)
		return start, start.AddDate(0, 0, 7), nil
	case "month":
		start = time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, loc)
		return start, start.AddDate(0, 1, 0), nil
	}
	return start, start, fmt.Errorf("unsupported report kind")
}
func (s *Store) Report(ctx context.Context, owner, kind, date string, now time.Time, save bool) (*Report, error) {
	owner, err := normalizedOwner(owner)
	if err != nil {
		return nil, err
	}
	start, end, err := reportWindow(kind, date, now)
	if err != nil {
		return nil, err
	}
	until := end
	if now.Before(until) {
		until = now
	}
	r := &Report{Kind: kind, Start: start, End: end, AsOf: now, Ongoing: now.Before(end), Runtime: []RuntimeUsage{}, Note: "依据已采集事实生成；覆盖不足不推断为零。新增会话与期间使用过的会话分别统计。"}
	r.Counts, err = s.PeriodCounts(ctx, owner, nil, start, until)
	if err != nil {
		return nil, err
	}
	previousStart := start.AddDate(0, 0, -7)
	if kind == "month" {
		previousStart = start.AddDate(0, -1, 0)
	}
	previousEnd := start
	if r.Ongoing {
		previousEnd = previousStart.Add(until.Sub(start))
		if previousEnd.After(start) {
			previousEnd = start
		}
		equal := previousEnd.Sub(previousStart)
		if until.Sub(start) > equal {
			until = start.Add(equal)
			r.Counts, err = s.PeriodCounts(ctx, owner, nil, start, until)
			if err != nil {
				return nil, err
			}
			r.AsOf = until
		}
	}
	r.Previous, err = s.PeriodCounts(ctx, owner, nil, previousStart, previousEnd)
	if err != nil {
		return nil, err
	}
	starts, err := s.metricStarts(ctx, owner, nil)
	if err != nil {
		return nil, err
	}
	r.Comparison = map[string]MetricComparison{}
	for _, metric := range []string{"tokens", "questions", "sessions"} {
		at, ok := starts[metric]
		r.Comparison[metric] = CompareMetric(r.Counts[metric], r.Previous[metric], r.Previous[metric] > 0 || ok && !at.After(previousStart))
	}
	r.Days, _, err = s.UsageDays(ctx, owner, nil, start, until)
	if err != nil {
		return nil, err
	}
	for _, d := range r.Days {
		if d.Questions > 0 {
			r.ActiveDays++
		}
		if d.Observed {
			r.ObservedDays++
		}
	}
	rows, err := s.factRows(ctx, `SELECT COALESCE(runtime_type,'unknown'),COALESCE(SUM(CASE WHEN record_type='invocation' THEN total_tokens ELSE 0 END),0),SUM(record_type='question') FROM northbound_owner_records WHERE owner_normalized=? AND record_type IN ('invocation','question') AND occurred_at>=? AND occurred_at<? GROUP BY runtime_type ORDER BY runtime_type`, owner, start.UTC(), until.UTC())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var u RuntimeUsage
		if err = rows.Scan(&u.Runtime, &u.Tokens, &u.Questions); err != nil {
			rows.Close()
			return nil, err
		}
		r.Runtime = append(r.Runtime, u)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	row, err := s.factRow(ctx, `SELECT COUNT(DISTINCT instance_id),COUNT(DISTINCT CASE WHEN record_type='invocation' AND session_key<>'' AND session_key NOT LIKE 'sess_trc_%' AND session_key NOT LIKE 'trc_%' THEN CONCAT(instance_id,':',session_key) ELSE NULL END) FROM northbound_owner_records WHERE owner_normalized=? AND record_type IN ('invocation','question') AND occurred_at>=? AND occurred_at<?`, owner, start.UTC(), until.UTC())
	if err != nil {
		return nil, err
	}
	if err = row.Scan(&r.Instances, &r.UsedSessions); err != nil {
		return nil, err
	}
	if epoch, e := s.analyticsEpoch(ctx); e != nil {
		return nil, e
	} else if epoch != nil {
		row, e = s.Session.SQL().QueryRowContext(ctx, `SELECT COUNT(DISTINCT instance_id,session_key) FROM northbound_owner_records WHERE record_type='session_presence' AND owner_normalized=? AND occurred_at>=? AND occurred_at<?`, owner, start.UTC(), until.UTC())
		if e != nil {
			return nil, e
		}
		if e = row.Scan(&r.UsedSessions); e != nil {
			return nil, e
		}
	}
	if save && !r.Ongoing {
		if err = s.saveReport(ctx, owner, r); err != nil {
			return nil, err
		}
	}
	r.SavedRevisions, err = s.ReportRevisions(ctx, owner, kind, start)
	return r, err
}

func (s *Store) ReportRevisions(ctx context.Context, owner, kind string, start time.Time) ([]uint64, error) {
	rows, err := s.factRows(ctx, `SELECT revision FROM northbound_owner_records WHERE record_type='period_report' AND owner_normalized=? AND period_type=? AND period_start=? ORDER BY revision DESC LIMIT 100`, owner, kind, start.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []uint64{}
	for rows.Next() {
		var revision uint64
		if err = rows.Scan(&revision); err != nil {
			return nil, err
		}
		result = append(result, revision)
	}
	return result, rows.Err()
}

// GET reuses a closed, clean snapshot. Dirty/ongoing periods remain read-only
// live calculations until the collector publishes a new revision.
func (s *Store) ReadReport(ctx context.Context, owner, kind, date string, now time.Time) (*Report, error) {
	owner, err := normalizedOwner(owner)
	if err != nil {
		return nil, err
	}
	start, end, err := reportWindow(kind, date, now)
	if err != nil {
		return nil, err
	}
	if !now.Before(end) {
		row, e := s.factRow(ctx, `SELECT h.version FROM northbound_owner_records h WHERE h.record_type='report_head' AND h.record_key=? AND h.version>0 AND NOT EXISTS(SELECT 1 FROM northbound_owner_records d WHERE d.record_type='report_dirty' AND d.record_key=? AND d.status='pending')`, Key("report", owner, kind, start.Format("2006-01-02")), Key("report-dirty", owner, kind, start.Format("2006-01-02")))
		if e != nil {
			return nil, e
		}
		var revision uint64
		e = row.Scan(&revision)
		if e == nil {
			return s.SavedReport(ctx, owner, kind, date, revision, now)
		}
		if e != sql.ErrNoRows {
			return nil, e
		}
	}
	return s.Report(ctx, owner, kind, date, now, false)
}
func (s *Store) SavedReport(ctx context.Context, owner, kind, date string, revision uint64, now time.Time) (*Report, error) {
	owner, err := normalizedOwner(owner)
	if err != nil {
		return nil, err
	}
	start, _, err := reportWindow(kind, date, now)
	if err != nil {
		return nil, err
	}
	key := Key("report", owner, kind, start.Format("2006-01-02"))
	row, err := s.factRow(ctx, `SELECT payload FROM northbound_owner_records WHERE record_type='period_report' AND record_key=? AND owner_normalized=?`, Key(key, fmt.Sprint(revision)), owner)
	if err != nil {
		return nil, err
	}
	var raw []byte
	if err = row.Scan(&raw); err != nil {
		return nil, err
	}
	var r Report
	if err = json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	r.SavedRevisions, err = s.ReportRevisions(ctx, owner, kind, start)
	return &r, err
}
func (s *Store) saveReport(ctx context.Context, owner string, r *Report) error {
	// The content hash excludes generation time; replay creates no new revision.
	content := *r
	content.AsOf = r.End
	content.Revision = 0
	raw, _ := json.Marshal(content)
	hash := sha256.Sum256(raw)
	digest := hex.EncodeToString(hash[:])
	key := Key("report", owner, r.Kind, r.Start.Format("2006-01-02"))
	return s.Session.TxContext(ctx, func(tx db.Session) error {
		_, err := tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,period_type,period_start,period_end,version,payload) VALUES ('report_head',?,0,?,'ready',?,?,?,0,JSON_OBJECT()) ON DUPLICATE KEY UPDATE record_key=record_key`, key, owner, r.Kind, r.Start.Format("2006-01-02"), r.End.Format("2006-01-02"))
		if err != nil {
			return err
		}
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT version,JSON_UNQUOTE(JSON_EXTRACT(payload,'$.hash')) FROM northbound_owner_records WHERE record_type='report_head' AND record_key=? FOR UPDATE`, key)
		if err != nil {
			return err
		}
		var revision uint64
		var old sql.NullString
		if err = row.Scan(&revision, &old); err != nil {
			return err
		}
		if old.String == digest {
			r.Revision = revision
			return nil
		}
		revision++
		r.Revision = revision
		body, _ := json.Marshal(r)
		_, err = tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,owner_normalized,status,period_type,period_start,period_end,revision,occurred_at,payload) VALUES ('period_report',?,0,?,'snapshot',?,?,?,?,?,?)`, Key(key, fmt.Sprint(revision)), owner, r.Kind, r.Start.Format("2006-01-02"), r.End.Format("2006-01-02"), revision, r.AsOf.UTC(), string(body))
		if err != nil {
			return err
		}
		_, err = tx.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET version=?,payload=JSON_OBJECT('hash',?),occurred_at=? WHERE record_type='report_head' AND record_key=?`, revision, digest, r.AsOf.UTC(), key)
		return err
	}, nil)
}
