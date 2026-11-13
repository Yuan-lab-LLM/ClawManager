package workspacedashboard

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// Usage refreshes only the authenticated user's Token and recorded cost card.
func (s *Store) Usage(ctx context.Context, userID int, period string, now time.Time) (*UsageDashboard, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("invalid user")
	}
	w, err := makeWindow(period, now)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	d := &Dashboard{Range: period, Window: w, Usage: Usage{Currencies: []CurrencyUsage{}}, UsageHistory: []UsagePoint{}, AgentUsage: []AgentUsage{}}
	for at := w.Start; at.Before(w.End); at = nextBucket(at, period) {
		d.UsageHistory = append(d.UsageHistory, UsagePoint{At: at, Costs: map[string]*float64{}})
	}
	s.usage(ctx, userID, d)
	s.usageAttributionCoverage(ctx, userID, d)
	return &UsageDashboard{
		Range: period, Timezone: "Asia/Shanghai", AsOf: now.UTC(), Window: w,
		Usage: d.Usage, UsageHistory: d.UsageHistory, AgentUsage: d.AgentUsage,
		Coverage: UsageCoverage{Usage: d.Coverage.Usage, Costs: d.Coverage.Costs},
	}, nil
}

// Archived rows retain provable caller attribution without appearing in current resources.
const historicalInstanceScope = `i.user_id=? AND i.owner IS NULL
 AND NOT EXISTS(SELECT 1 FROM team_members hidden_m JOIN teams hidden_t ON hidden_t.id=hidden_m.team_id
 WHERE hidden_m.instance_id=i.id AND (hidden_t.resource_scope<>'cm' OR hidden_t.user_id<>i.user_id))`
const scopedInv = `SELECT v.id,v.trace_id,v.instance_id,v.created_at,v.prompt_tokens,v.completion_tokens,v.total_tokens
 FROM model_invocations v JOIN instances i ON i.id=v.instance_id WHERE ` + historicalInstanceScope + `
 AND v.user_id=i.user_id AND v.status<>'blocked' AND v.created_at>=? AND v.created_at<?`

// Native cost snapshots are ranked BEFORE aggregation: actual wins over estimated,
// and duplicate records never multiply invocation tokens or recorded amounts.
// Legacy trace-only rows bind to one invocation, not every retry sharing a trace.
const costFacts = `WITH inv AS (` + scopedInv + `), ranked AS (
 SELECT v.id,v.instance_id,v.created_at,c.currency,c.actual_cost,c.estimated_cost,c.input_unit_price,c.output_unit_price,
 ROW_NUMBER() OVER(PARTITION BY v.id,c.currency ORDER BY c.actual_cost IS NOT NULL DESC,c.id DESC) AS rn
 FROM inv v JOIN cost_records c ON c.instance_id=v.instance_id AND c.user_id=? AND
 (c.invocation_id=v.id OR (c.invocation_id IS NULL AND c.trace_id<>'' AND c.trace_id=v.trace_id AND (SELECT COUNT(*) FROM model_invocations v2 WHERE v2.trace_id=v.trace_id AND v2.instance_id=v.instance_id AND v2.user_id=c.user_id)=1))
 WHERE c.currency<>''), facts AS (
 SELECT *, (actual_cost IS NOT NULL OR input_unit_price>0 OR output_unit_price>0) AS known,
 CASE WHEN actual_cost IS NOT NULL THEN actual_cost WHEN input_unit_price>0 OR output_unit_price>0 THEN estimated_cost END AS amount
 FROM ranked WHERE rn=1) `

func (s *Store) usage(ctx context.Context, id int, d *Dashboard) {
	w := d.Window
	d.Coverage.Usage = ready()
	d.Coverage.Costs = ready()
	// SQL aggregates keep request memory independent of invocation count.
	rows, err := s.sess.SQL().QueryContext(ctx, `SELECT created_at>=?,COALESCE(SUM(total_tokens),0),COALESCE(SUM(prompt_tokens),0),COALESCE(SUM(completion_tokens),0),COUNT(*) FROM (`+scopedInv+`) scoped WHERE created_at<? OR created_at>=? GROUP BY 1`, w.Start, id, w.PreviousStart, w.End, w.PreviousEnd, w.Start)
	var currentCalls, previousCalls int64
	if err != nil {
		d.Coverage.Usage = unavailable("调用统计暂不可用")
	} else {
		var total, prompt, completion, calls, previous int64
		for rows.Next() {
			var current bool
			var t, p, c, n int64
			if err = rows.Scan(&current, &t, &p, &c, &n); err != nil {
				break
			}
			if current {
				total = t
				prompt = p
				completion = c
				calls = n
			} else {
				previous = t
				previousCalls = n
			}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			d.Coverage.Usage = unavailable("调用统计暂不可用")
		} else {
			d.Usage.TotalTokens = &total
			d.Usage.PromptTokens = &prompt
			d.Usage.CompletionTokens = &completion
			d.Usage.InvocationCount = &calls
			d.Usage.PreviousTokens = &previous
			currentCalls = calls
		}
	}
	if d.Coverage.Usage.Status == "ready" {
		for i := range d.UsageHistory {
			zero := int64(0)
			calls := int64(0)
			d.UsageHistory[i].Tokens = &zero
			d.UsageHistory[i].Calls = &calls
		}
		rows, err = s.sess.SQL().QueryContext(ctx, `SELECT `+sqlBucket(d.Range, "created_at")+`,SUM(total_tokens),COUNT(*) FROM (`+scopedInv+`) scoped GROUP BY 1`, id, w.Start, w.End)
		if err != nil {
			d.Coverage.Usage = Coverage{Status: "partial", Note: "调用历史暂不可用"}
			for i := range d.UsageHistory {
				d.UsageHistory[i].Tokens = nil
				d.UsageHistory[i].Calls = nil
			}
		} else {
			for rows.Next() {
				var at time.Time
				var tokens, calls int64
				if err = rows.Scan(&at, &tokens, &calls); err != nil {
					break
				}
				for i := range d.UsageHistory {
					if d.UsageHistory[i].At.Equal(at) {
						d.UsageHistory[i].Tokens = &tokens
						d.UsageHistory[i].Calls = &calls
						break
					}
				}
			}
			if err == nil {
				err = rows.Err()
			}
			rows.Close()
			if err != nil {
				d.Coverage.Usage = Coverage{Status: "partial", Note: "调用历史读取不完整"}
				// A prefix cannot establish empty buckets; retain period totals only.
				for i := range d.UsageHistory {
					d.UsageHistory[i].Tokens = nil
					d.UsageHistory[i].Calls = nil
				}
			}
		}
		rows, err = s.sess.SQL().QueryContext(ctx, `SELECT instance_id,SUM(total_tokens) FROM (`+scopedInv+`) scoped GROUP BY 1 ORDER BY 1`, id, w.Start, w.End)
		if err == nil {
			for rows.Next() {
				a := AgentUsage{Costs: map[string]float64{}}
				if err = rows.Scan(&a.InstanceID, &a.Tokens); err != nil {
					break
				}
				d.AgentUsage = append(d.AgentUsage, a)
			}
			if err == nil {
				err = rows.Err()
			}
			rows.Close()
		}
		if err != nil {
			d.Coverage.Usage = Coverage{Status: "partial", Note: "Agent 调用分布暂不可用"}
		}
	}
	// Price completeness is evaluated across all currencies, never by converting them.
	complete := [2]bool{}
	var covered [2]int64
	var unknown [2]int64
	rows, err = s.sess.SQL().QueryContext(ctx, costFacts+`SELECT created_at>=?,COUNT(DISTINCT CASE WHEN known THEN id END),SUM(NOT known) FROM facts WHERE created_at<? OR created_at>=? GROUP BY 1`, id, w.PreviousStart, w.End, id, w.Start, w.PreviousEnd, w.Start)
	if err == nil {
		for rows.Next() {
			var current bool
			var n, bad int64
			if err = rows.Scan(&current, &n, &bad); err != nil {
				break
			}
			idx := 0
			if current {
				idx = 1
			}
			covered[idx] = n
			unknown[idx] = bad
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
	}
	if err != nil {
		d.Coverage.Costs = unavailable("已记录费用暂不可用")
		return
	}
	complete[0] = d.Usage.PreviousTokens != nil && covered[0] == previousCalls && unknown[0] == 0
	complete[1] = d.Usage.InvocationCount != nil && covered[1] == currentCalls && unknown[1] == 0
	type currencyTotals struct {
		current, previous sql.NullFloat64
		calls             int64
		estimated         bool
	}
	currencies := map[string]*currencyTotals{}
	rows, err = s.sess.SQL().QueryContext(ctx, costFacts+`SELECT created_at>=?,currency,SUM(amount),SUM(known),MAX(known AND actual_cost IS NULL) FROM facts WHERE created_at<? OR created_at>=? GROUP BY 1,2 ORDER BY 2`, id, w.PreviousStart, w.End, id, w.Start, w.PreviousEnd, w.Start)
	if err != nil {
		d.Coverage.Costs = unavailable("已记录费用暂不可用")
		return
	}
	for rows.Next() {
		var current bool
		var currency string
		var amount sql.NullFloat64
		var n int64
		var estimated bool
		if err = rows.Scan(&current, &currency, &amount, &n, &estimated); err != nil {
			break
		}
		c := currencies[currency]
		if c == nil {
			c = &currencyTotals{}
			currencies[currency] = c
		}
		if current {
			c.current = amount
			c.calls = n
			c.estimated = estimated
		} else {
			c.previous = amount
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		d.Coverage.Costs = unavailable("费用记录读取不完整")
		return
	}
	names := []string{}
	for name := range currencies {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		c := currencies[name]
		item := CurrencyUsage{Currency: name, Estimated: c.estimated, Complete: complete[1], RecordedCalls: c.calls}
		if c.current.Valid {
			v := c.current.Float64
			item.Amount = &v
		} else if complete[1] {
			zero := float64(0)
			item.Amount = &zero
		}
		if complete[0] && complete[1] {
			v := float64(0)
			if c.previous.Valid {
				v = c.previous.Float64
			}
			item.PreviousAmount = &v
		}
		d.Usage.Currencies = append(d.Usage.Currencies, item)
	}
	if !complete[1] {
		d.Coverage.Costs = Coverage{Status: "partial", Note: "仅含已记录费用，部分调用缺少价格或费用记录"}
	}
	rows, err = s.sess.SQL().QueryContext(ctx, costFacts+`SELECT `+sqlBucket(d.Range, "created_at")+`,instance_id,currency,SUM(amount) FROM facts GROUP BY 1,2,3`, id, w.Start, w.End, id)
	if err != nil {
		d.Coverage.Costs = Coverage{Status: "partial", Note: "费用历史暂不可用"}
		return
	}
	defer rows.Close()
	for rows.Next() {
		var at time.Time
		var instance int
		var currency string
		var amount sql.NullFloat64
		if err = rows.Scan(&at, &instance, &currency, &amount); err != nil {
			break
		}
		for i := range d.UsageHistory {
			p := &d.UsageHistory[i]
			if p.At.Equal(at) {
				if amount.Valid {
					if p.Costs[currency] == nil {
						zero := float64(0)
						p.Costs[currency] = &zero
					}
					*p.Costs[currency] += amount.Float64
				} else if _, ok := p.Costs[currency]; !ok {
					p.Costs[currency] = nil
				}
				break
			}
		}
		if amount.Valid {
			found := false
			for i := range d.AgentUsage {
				if d.AgentUsage[i].InstanceID == instance {
					d.AgentUsage[i].Costs[currency] += amount.Float64
					found = true
					break
				}
			}
			if !found {
				d.AgentUsage = append(d.AgentUsage, AgentUsage{InstanceID: instance, Costs: map[string]float64{currency: amount.Float64}})
			}
		}
	}
	if err != nil || rows.Err() != nil {
		d.Coverage.Costs = Coverage{Status: "partial", Note: "费用历史读取不完整"}
	}
}
func sqlBucket(period, field string) string {
	if period == "today" {
		return "CAST(DATE_FORMAT(" + field + ",'%Y-%m-%d %H:00:00') AS DATETIME)"
	}
	return "CAST(DATE(" + field + " + INTERVAL 8 HOUR) AS DATETIME) - INTERVAL 8 HOUR"
}

func (s *Store) activity(ctx context.Context, id int, now time.Time, d *Dashboard) {
	since := dayStart(now).AddDate(0, 0, -364)
	available := 0
	rows, err := s.sess.SQL().QueryContext(ctx, `SELECT DATE_FORMAT(created_at+INTERVAL 8 HOUR,'%Y-%m-%d'),COUNT(*),SUM(total_tokens) FROM (`+scopedInv+`) scoped GROUP BY 1`, id, since, now)
	if err == nil {
		for rows.Next() {
			var date string
			var n, tokens int64
			if err = rows.Scan(&date, &n, &tokens); err != nil {
				break
			}
			for i := range d.Activity {
				if d.Activity[i].Date == date {
					d.Activity[i].Calls = n
					d.Activity[i].Tokens = tokens
					d.Activity[i].Events += n
					break
				}
			}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err == nil {
			available++
		}
	}
	rows, err = s.sess.SQL().QueryContext(ctx, `SELECT DATE_FORMAT(tt.finished_at+INTERVAL 8 HOUR,'%Y-%m-%d'),COUNT(*) FROM team_tasks tt JOIN teams t ON t.id=tt.team_id JOIN team_members m ON m.id=tt.target_member_id AND m.team_id=t.id AND m.user_id=t.user_id WHERE t.user_id=? AND t.resource_scope='cm' AND tt.status IN ('succeeded','failed') AND tt.finished_at>=? AND tt.finished_at<? GROUP BY 1`, id, since, now)
	if err == nil {
		for rows.Next() {
			var date string
			var n int64
			if err = rows.Scan(&date, &n); err != nil {
				break
			}
			for i := range d.Activity {
				if d.Activity[i].Date == date {
					d.Activity[i].Tasks = n
					d.Activity[i].Events += n
					break
				}
			}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err == nil {
			available++
		}
	}
	d.Coverage.Activity = ready()
	if available < 2 {
		d.Coverage.Activity = Coverage{Status: "partial", Note: "年度活动记录不完整"}
	}
	if available == 0 {
		d.Coverage.Activity = unavailable("年度活动暂不可用")
	}
	for i := range d.Activity {
		d.Activity[i].Observed = available == 2
	}
}

// Physical deletion removes the only durable CM/northbound scope discriminator.
// Retained user attribution alone cannot safely reclassify a system owner's old
// northbound calls as personal CM activity. Report the known subtotal explicitly.
func (s *Store) attributionCoverage(ctx context.Context, id int, now time.Time, d *Dashboard) {
	rows, err := s.sess.SQL().QueryContext(ctx, `SELECT DATE_FORMAT(v.created_at+INTERVAL 8 HOUR,'%Y-%m-%d'),
 MAX(v.created_at>=? AND (v.created_at<? OR v.created_at>=?)),MAX(v.created_at>=?)
 FROM model_invocations v LEFT JOIN instances i ON i.id=v.instance_id
 WHERE v.user_id=? AND v.instance_id IS NOT NULL AND (i.id IS NULL OR i.user_id<>v.user_id)
 AND v.status<>'blocked' AND v.created_at>=? AND v.created_at<? GROUP BY 1`, d.Window.PreviousStart, d.Window.PreviousEnd, d.Window.Start, d.Window.Start, id, dayStart(now).AddDate(0, 0, -364), now)
	comparisonUnknown, currentUnknown, activityUnknown := false, false, false
	if err == nil {
		for rows.Next() {
			var date string
			var comparison, current bool
			if err = rows.Scan(&date, &comparison, &current); err != nil {
				break
			}
			comparisonUnknown = comparisonUnknown || comparison
			currentUnknown = currentUnknown || current
			activityUnknown = true
			for i := range d.Activity {
				if d.Activity[i].Date == date {
					d.Activity[i].Observed = false
					break
				}
			}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
	}
	if err != nil {
		comparisonUnknown = true
		currentUnknown = true
		activityUnknown = true
		for i := range d.Activity {
			d.Activity[i].Observed = false
		}
	}
	applyUsageAttributionCoverage(d, comparisonUnknown, currentUnknown)
	if activityUnknown && d.Coverage.Activity.Status != "unavailable" {
		d.Coverage.Activity = Coverage{Status: "partial", Note: attributionNote}
	}
}

const attributionNote = "部分历史记录无法验证资源归属，未计入统计"

func (s *Store) usageAttributionCoverage(ctx context.Context, id int, d *Dashboard) {
	w := d.Window
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT MAX(v.created_at>=? AND (v.created_at<? OR v.created_at>=?)),MAX(v.created_at>=?)
 FROM model_invocations v LEFT JOIN instances i ON i.id=v.instance_id
 WHERE v.user_id=? AND v.instance_id IS NOT NULL AND (i.id IS NULL OR i.user_id<>v.user_id)
 AND v.status<>'blocked' AND v.created_at>=? AND v.created_at<?`, w.PreviousStart, w.PreviousEnd, w.Start, w.Start, id, w.PreviousStart, w.End)
	var comparison, current sql.NullBool
	if err == nil {
		err = row.Scan(&comparison, &current)
	}
	applyUsageAttributionCoverage(d, err != nil || comparison.Bool, err != nil || current.Bool)
}

func applyUsageAttributionCoverage(d *Dashboard, comparisonUnknown, currentUnknown bool) {
	if comparisonUnknown {
		d.Usage.PreviousTokens = nil
		for i := range d.Usage.Currencies {
			d.Usage.Currencies[i].PreviousAmount = nil
		}
		if d.Coverage.Usage.Status != "unavailable" {
			d.Coverage.Usage = Coverage{Status: "partial", Note: attributionNote}
		}
		if d.Coverage.Costs.Status != "unavailable" {
			d.Coverage.Costs = Coverage{Status: "partial", Note: attributionNote}
		}
	}
	if currentUnknown {
		for i := range d.Usage.Currencies {
			d.Usage.Currencies[i].Complete = false
		}
	}
}
