package ownerrecords

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type UsageDay struct {
	Date       string `json:"date"`
	Tokens     uint64 `json:"tokens"`
	Prompt     uint64 `json:"prompt_tokens"`
	Completion uint64 `json:"completion_tokens"`
	Questions  uint64 `json:"questions"`
	Sessions   uint64 `json:"sessions"`
	Observed   bool   `json:"observed"`
	InRange    bool   `json:"in_statistical_range"`
}
type Window struct {
	Kind          string    `json:"kind"`
	Start         time.Time `json:"start"`
	End           time.Time `json:"end"`
	PreviousStart time.Time `json:"previous_start"`
	PreviousEnd   time.Time `json:"previous_end"`
}
type MetricComparison struct {
	Window   *Window  `json:"window,omitempty"`
	Current  uint64   `json:"current"`
	Previous uint64   `json:"previous"`
	Delta    float64  `json:"delta"`
	Percent  *float64 `json:"percent"`
	Status   string   `json:"status"`
}
type Insights struct {
	Days          []UsageDay                  `json:"days"`
	Window        Window                      `json:"window"`
	Comparison    map[string]MetricComparison `json:"comparison"`
	FirstObserved *time.Time                  `json:"first_observed"`
	AsOf          time.Time                   `json:"as_of"`
	CoverageNote  string                      `json:"coverage_note"`
}

// Equal elapsed windows, in the caller's business timezone. Calendar months
// are clamped symmetrically, never normalized into the following month.
func ComparisonWindow(first, now time.Time) Window {
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	age := int(midnight.Sub(time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, now.Location())).Hours() / 24)
	w := Window{Kind: "day", Start: midnight, End: now, PreviousStart: midnight.AddDate(0, 0, -1), PreviousEnd: now.AddDate(0, 0, -1)}
	if age >= 7 {
		offset := (int(now.Weekday()) + 6) % 7
		w.Kind = "week"
		w.Start = midnight.AddDate(0, 0, -offset)
		w.PreviousStart = w.Start.AddDate(0, 0, -7)
		w.PreviousEnd = now.AddDate(0, 0, -7)
	}
	if age > 30 {
		w.Kind = "month"
		w.Start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		w.PreviousStart = w.Start.AddDate(0, -1, 0)
		days := w.Start.AddDate(0, 0, -1).Day()
		day := now.Day()
		if day > days {
			day = days
		}
		w.End = time.Date(now.Year(), now.Month(), day, now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), now.Location())
		w.PreviousEnd = time.Date(w.PreviousStart.Year(), w.PreviousStart.Month(), day, now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), now.Location())
	}
	return w
}
func CompareMetric(current, previous uint64, comparable bool) MetricComparison {
	c := MetricComparison{Current: current, Previous: previous, Delta: float64(current) - float64(previous), Status: "insufficient"}
	if !comparable {
		return c
	}
	if previous == 0 {
		c.Status = "new"
		if current == 0 {
			c.Status = "flat"
		}
		return c
	}
	p := c.Delta / float64(previous) * 100
	c.Percent = &p
	c.Status = "change"
	if current == previous {
		c.Status = "flat"
	}
	return c
}
func ownerFilter(owner string, scope *uint64) (string, []any, error) {
	owner = strings.ToLower(strings.TrimSpace(owner))
	if owner == "" {
		return "", nil, fmt.Errorf("verified owner required")
	}
	where := "owner_normalized=?"
	args := []any{owner}
	if scope != nil {
		if *scope == 0 {
			return "", nil, fmt.Errorf("account scope required")
		}
		where += " AND scope_id=?"
		args = append(args, *scope)
	}
	return where, args, nil
}
func (s *Store) UsageDays(ctx context.Context, owner string, scope *uint64, start, end time.Time) ([]UsageDay, *time.Time, error) {
	where, args, err := ownerFilter(owner, scope)
	if err != nil {
		return nil, nil, err
	}
	var first sql.NullTime
	row, err := s.factRow(ctx, `SELECT MIN(occurred_at) FROM northbound_owner_records WHERE record_type IN ('invocation','question','session') AND `+where, args...)
	if err != nil {
		return nil, nil, err
	}
	if err = row.Scan(&first); err != nil {
		return nil, nil, err
	}
	bound := append(append([]any{}, args...), start.UTC(), end.UTC())
	rows, err := s.factRows(ctx, `SELECT DATE_FORMAT(DATE_ADD(occurred_at,INTERVAL 8 HOUR),'%Y-%m-%d'),COALESCE(SUM(CASE WHEN record_type='invocation' THEN total_tokens ELSE 0 END),0),COALESCE(SUM(CASE WHEN record_type='invocation' THEN prompt_tokens ELSE 0 END),0),COALESCE(SUM(CASE WHEN record_type='invocation' THEN completion_tokens ELSE 0 END),0),SUM(record_type='question'),SUM(record_type='session') FROM northbound_owner_records WHERE record_type IN ('invocation','question','session') AND `+where+` AND occurred_at>=? AND occurred_at<? GROUP BY 1`, bound...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	daily := map[string]UsageDay{}
	for rows.Next() {
		var d UsageDay
		if err = rows.Scan(&d.Date, &d.Tokens, &d.Prompt, &d.Completion, &d.Questions, &d.Sessions); err != nil {
			return nil, nil, err
		}
		d.Observed = true
		daily[d.Date] = d
	}
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}
	result := []UsageDay{}
	for date := start; date.Before(end); date = date.AddDate(0, 0, 1) {
		key := date.Format("2006-01-02")
		d := daily[key]
		d.Date = key
		d.InRange = first.Valid && key >= first.Time.In(start.Location()).Format("2006-01-02")
		result = append(result, d)
	}
	if first.Valid {
		t := first.Time.In(start.Location())
		return result, &t, nil
	}
	return result, nil, nil
}
func (s *Store) PeriodCounts(ctx context.Context, owner string, scope *uint64, start, end time.Time) (map[string]uint64, error) {
	where, args, err := ownerFilter(owner, scope)
	if err != nil {
		return nil, err
	}
	args = append(args, start.UTC(), end.UTC())
	row, err := s.factRow(ctx, `SELECT COALESCE(SUM(CASE WHEN record_type='invocation' THEN total_tokens ELSE 0 END),0),COALESCE(SUM(record_type='question'),0),COALESCE(SUM(record_type='session'),0) FROM northbound_owner_records WHERE record_type IN ('invocation','question','session') AND `+where+` AND occurred_at>=? AND occurred_at<?`, args...)
	if err != nil {
		return nil, err
	}
	var tokens, questions, sessions uint64
	if err = row.Scan(&tokens, &questions, &sessions); err != nil {
		return nil, err
	}
	return map[string]uint64{"tokens": tokens, "questions": questions, "sessions": sessions}, nil
}
func (s *Store) Insights(ctx context.Context, owner string, scope *uint64, now time.Time) (*Insights, error) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now = now.In(loc)
	end := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, loc)
	days, first, err := s.UsageDays(ctx, owner, scope, end.AddDate(0, 0, -90), end)
	if err != nil {
		return nil, err
	}
	start := now
	if first != nil {
		start = *first
	}
	w := ComparisonWindow(start, now)
	current, err := s.PeriodCounts(ctx, owner, scope, w.Start, w.End)
	if err != nil {
		return nil, err
	}
	previous, err := s.PeriodCounts(ctx, owner, scope, w.PreviousStart, w.PreviousEnd)
	if err != nil {
		return nil, err
	}
	comparisons := map[string]MetricComparison{}
	starts, err := s.metricStarts(ctx, owner, scope)
	if err != nil {
		return nil, err
	}
	for _, metric := range []string{"tokens", "questions", "sessions"} {
		// Observations alone do not prove a complete previous zero. Do not invent a
		// percentage when no previous evidence exists. Nonzero comparisons explicitly
		// refer to collected evidence, not full historical usage.
		chosen := w
		cur, prev := current[metric], previous[metric]
		known := func(window Window) bool { at, ok := starts[metric]; return ok && !at.After(window.PreviousStart) }
		if !known(chosen) && prev == 0 && chosen.Kind == "month" {
			chosen = ComparisonWindow(now.AddDate(0, 0, -7), now)
			c, e := s.PeriodCounts(ctx, owner, scope, chosen.Start, chosen.End)
			if e != nil {
				return nil, e
			}
			p, e := s.PeriodCounts(ctx, owner, scope, chosen.PreviousStart, chosen.PreviousEnd)
			if e != nil {
				return nil, e
			}
			cur, prev = c[metric], p[metric]
		}
		if !known(chosen) && prev == 0 && chosen.Kind == "week" {
			chosen = ComparisonWindow(now, now)
			c, e := s.PeriodCounts(ctx, owner, scope, chosen.Start, chosen.End)
			if e != nil {
				return nil, e
			}
			p, e := s.PeriodCounts(ctx, owner, scope, chosen.PreviousStart, chosen.PreviousEnd)
			if e != nil {
				return nil, e
			}
			cur, prev = c[metric], p[metric]
		}
		comparison := CompareMetric(cur, prev, prev > 0 || known(chosen))
		comparison.Window = &chosen
		comparisons[metric] = comparison
	}
	return &Insights{Days: days, Window: w, Comparison: comparisons, FirstObserved: first, AsOf: now, CoverageNote: "按已采集记录比较等长时段；无记录的日期不等于已确认零用量。日期不明的历史仅保留累计，不参与日期比较。"}, nil
}

// Per-metric collection bounds: zero means no collected usage within a known
// range, never a claim of complete source coverage. Before that range is unknown.
func (s *Store) metricStarts(ctx context.Context, owner string, scope *uint64) (map[string]time.Time, error) {
	where, args, err := ownerFilter(owner, scope)
	if err != nil {
		return nil, err
	}
	row, err := s.factRow(ctx, `SELECT MIN(CASE WHEN record_type='invocation' THEN occurred_at END),MIN(CASE WHEN record_type='question' THEN occurred_at END),MIN(CASE WHEN record_type='session' THEN occurred_at END) FROM northbound_owner_records WHERE record_type IN ('invocation','question','session') AND `+where, args...)
	if err != nil {
		return nil, err
	}
	var tokens, questions, sessions sql.NullTime
	if err = row.Scan(&tokens, &questions, &sessions); err != nil {
		return nil, err
	}
	result := map[string]time.Time{}
	for k, v := range map[string]sql.NullTime{"tokens": tokens, "questions": questions, "sessions": sessions} {
		if v.Valid {
			result[k] = v.Time
		}
	}
	return result, nil
}
