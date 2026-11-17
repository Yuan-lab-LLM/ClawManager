package repository

import (
	"clawreef/internal/models"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/upper/db/v4"
	"strings"
	"time"
)

type CostOverviewItem struct {
	Record                 models.CostRecord
	Username, InstanceName string
}
type CostOverviewGroup struct {
	Label  string
	Totals models.CostRecord
}
type CostOverviewDay struct {
	Label, Day string
	Totals     models.CostRecord
}
type CostOverviewReport struct {
	Totals                     models.CostRecord
	Total                      int
	Items                      []CostOverviewItem
	Users, Instances, Models   []CostOverviewGroup
	Daily, ModelDays, UserDays []CostOverviewDay
	NextCursor                 string
}
type costCursor struct {
	Search           string
	Since, Until, At time.Time
	MaxID, ID        int
}

const costSums = `COALESCE(SUM(cr.prompt_tokens),0),COALESCE(SUM(cr.completion_tokens),0),COALESCE(SUM(cr.total_tokens),0),COALESCE(SUM(cr.estimated_cost),0),COALESCE(SUM(cr.internal_cost),0)`
const costUserLabel = `COALESCE(NULLIF(u.username,''),CONCAT('User #',cr.user_id),'Unknown')`
const costInstanceLabel = `COALESCE(NULLIF(i.name,''),CONCAT('Instance #',cr.instance_id),'Unassigned')`

func costDest(v *models.CostRecord) []interface{} {
	return []interface{}{&v.PromptTokens, &v.CompletionTokens, &v.TotalTokens, &v.EstimatedCost, &v.InternalCost}
}

// CostOverviewReport counts immutable ledger rows once, and bounds every result
// projection. Snapshot max ID excludes delayed/backdated writes across pages.
func (r *costRecordRepository) CostOverviewReport(page, limit int, search string, filter SessionUsageFilter) (*CostOverviewReport, error) {
	if page < 1 || limit < 1 || limit > 100 || page > int(^uint(0)>>1)/limit {
		return nil, fmt.Errorf("invalid cost page")
	}
	now := time.Now().UTC()
	until := now
	if filter.Until != nil {
		until = *filter.Until
	}
	since := until.Add(-30 * 24 * time.Hour)
	if filter.Since != nil {
		since = *filter.Since
	}
	if !until.After(since) || until.Sub(since) > 90*24*time.Hour {
		return nil, fmt.Errorf("cost query requires a window of at most 90 days")
	}
	search = strings.ToLower(strings.TrimSpace(search))
	cursor := costCursor{Search: search, Since: since, Until: until}
	if filter.Cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(filter.Cursor)
		if e != nil || len(raw) > 4096 {
			return nil, fmt.Errorf("invalid cost cursor")
		}
		if e = json.Unmarshal(raw, &cursor); e != nil || cursor.Search != search || !cursor.Until.After(cursor.Since) || cursor.Until.Sub(cursor.Since) > 90*24*time.Hour || cursor.MaxID < 1 {
			return nil, fmt.Errorf("invalid cost cursor scope")
		}
		since, until = cursor.Since, cursor.Until
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result := &CostOverviewReport{Items: []CostOverviewItem{}, Users: []CostOverviewGroup{}, Instances: []CostOverviewGroup{}, Models: []CostOverviewGroup{}, Daily: []CostOverviewDay{}, ModelDays: []CostOverviewDay{}, UserDays: []CostOverviewDay{}}
	err := r.sess.TxContext(ctx, func(tx db.Session) error {
		if filter.Cursor == "" {
			row, e := tx.SQL().QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM cost_records`)
			if e != nil {
				return e
			}
			if e = row.Scan(&cursor.MaxID); e != nil {
				return e
			}
		}
		from := ` FROM cost_records cr LEFT JOIN users u ON u.id=cr.user_id LEFT JOIN instances i ON i.id=cr.instance_id WHERE cr.recorded_at>=? AND cr.recorded_at<? AND cr.id<=?`
		args := []interface{}{since, until, cursor.MaxID}
		if search != "" {
			from += ` AND LOCATE(?,LOWER(CONCAT_WS(CHAR(0),cr.trace_id,cr.model_name,cr.provider_type,u.username,i.name)))>0`
			args = append(args, search)
		}
		reportFrom := func(scope string, collapseDays bool) (string, []interface{}) {
			if search != "" {
				return from, append([]interface{}{}, args...)
			}
			return costOverviewRollupFrom(scope, since, until, cursor.MaxID, filter.Cursor == "", collapseDays)
		}
		var target, processed int64
		var pending bool
		row, e := tx.SQL().QueryRowContext(ctx, `SELECT target_id,COALESCE((SELECT after_id FROM history_maintenance_cursors WHERE kind='query_cost_summary'),0),EXISTS(SELECT 1 FROM observation_cost_pending) FROM observation_summary_state WHERE id=2`)
		if e != nil {
			return e
		}
		if e = row.Scan(&target, &processed, &pending); e != nil {
			return e
		}
		if processed < target || pending {
			return &SummaryRebuildingError{Processed: processed, Target: target}
		}
		totalFrom, totalArgs := reportFrom("day", true)
		countSQL := "COALESCE(SUM(cr.record_count),0)"
		if search != "" {
			countSQL = "COUNT(*)"
		}
		row, e = tx.SQL().QueryRowContext(ctx, `SELECT `+countSQL+`,`+costSums+`,COALESCE(MAX(`+normalizedCostCurrencySQL+`),'USD'),COUNT(DISTINCT `+normalizedCostCurrencySQL+`)`+totalFrom, totalArgs...)
		if e != nil {
			return e
		}
		var currencies int
		dest := append([]interface{}{&result.Total}, costDest(&result.Totals)...)
		dest = append(dest, &result.Totals.Currency, &currencies)
		if e = row.Scan(dest...); e != nil {
			return e
		}
		if currencies > 1 {
			return ErrMixedCostCurrencies
		}
		for _, spec := range []struct {
			label, scope string
			out          *[]CostOverviewGroup
		}{{costUserLabel, "user", &result.Users}, {costInstanceLabel, "instance", &result.Instances}, {"cr.model_name", "model", &result.Models}} {
			groupFrom, groupArgs := reportFrom(spec.scope, true)
			rows, err := tx.SQL().QueryContext(ctx, `SELECT `+spec.label+` label,`+costSums+groupFrom+` GROUP BY label ORDER BY SUM(cr.estimated_cost) DESC,SUM(cr.total_tokens) DESC,label LIMIT 6`, groupArgs...)
			if err != nil {
				return err
			}
			for rows.Next() {
				var v CostOverviewGroup
				if err = rows.Scan(append([]interface{}{&v.Label}, costDest(&v.Totals)...)...); err != nil {
					rows.Close()
					return err
				}
				*spec.out = append(*spec.out, v)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
		}
		for _, spec := range []struct {
			label, scope string
			groups       []CostOverviewGroup
			out          *[]CostOverviewDay
		}{{"''", "day", nil, &result.Daily}, {"cr.model_name", "model", result.Models, &result.ModelDays}, {costUserLabel, "user", result.Users, &result.UserDays}} {
			queryFrom, dayArgs := reportFrom(spec.scope, false)
			if spec.groups != nil {
				if len(spec.groups) == 0 {
					continue
				}
				queryFrom += " AND " + spec.label + " IN (" + strings.TrimSuffix(strings.Repeat("?,", min(len(spec.groups), 5)), ",") + ")"
				for _, g := range spec.groups[:min(len(spec.groups), 5)] {
					dayArgs = append(dayArgs, g.Label)
				}
			}
			rows, err := tx.SQL().QueryContext(ctx, `SELECT `+spec.label+` label,DATE_FORMAT(cr.recorded_at,'%Y-%m-%d') day,`+costSums+queryFrom+` GROUP BY label,day ORDER BY day,label`, dayArgs...)
			if err != nil {
				return err
			}
			for rows.Next() {
				var v CostOverviewDay
				if err = rows.Scan(append([]interface{}{&v.Label, &v.Day}, costDest(&v.Totals)...)...); err != nil {
					rows.Close()
					return err
				}
				*spec.out = append(*spec.out, v)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
		}
		pageFrom := from
		pageArgs := append([]interface{}{}, args...)
		offset := (page - 1) * limit
		if filter.Cursor != "" {
			pageFrom += ` AND (cr.recorded_at,cr.id)<(?,?)`
			pageArgs = append(pageArgs, cursor.At, cursor.ID)
			offset = 0
		}
		rows, e := tx.SQL().QueryContext(ctx, `SELECT cr.id,cr.trace_id,cr.user_id,COALESCE(u.username,''),cr.instance_id,COALESCE(i.name,''),cr.model_name,cr.provider_type,cr.prompt_tokens,cr.completion_tokens,cr.total_tokens,cr.estimated_cost,cr.internal_cost,`+normalizedCostCurrencySQL+`,cr.recorded_at`+pageFrom+` ORDER BY cr.recorded_at DESC,cr.id DESC LIMIT ? OFFSET ?`, append(pageArgs, limit+1, offset)...)
		if e != nil {
			return e
		}
		for rows.Next() {
			var v CostOverviewItem
			cr := &v.Record
			if e = rows.Scan(&cr.ID, &cr.TraceID, &cr.UserID, &v.Username, &cr.InstanceID, &v.InstanceName, &cr.ModelName, &cr.ProviderType, &cr.PromptTokens, &cr.CompletionTokens, &cr.TotalTokens, &cr.EstimatedCost, &cr.InternalCost, &cr.Currency, &cr.RecordedAt); e != nil {
				rows.Close()
				return e
			}
			result.Items = append(result.Items, v)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if len(result.Items) > limit {
			result.Items = result.Items[:limit]
			last := result.Items[limit-1].Record
			cursor.At = last.RecordedAt
			cursor.ID = last.ID
			raw, _ := json.Marshal(cursor)
			result.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, err
}
