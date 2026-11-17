package repository

import (
	"context"
	"github.com/upper/db/v4"
	"strings"
	"time"
)

var costOverviewDimensions = []struct{ scope, expression string }{
	{"day", "''"}, {"instance", "COALESCE(CAST(cr.instance_id AS CHAR),'')"},
	{"model", "cr.model_name"}, {"user", "COALESCE(CAST(cr.user_id AS CHAR),'')"},
}

// Receipt insertion and all four ledger rollups share one SQL transaction.
func materializeCostOverview(ctx context.Context, tx db.Session, where string, args ...interface{}) error {
	for _, dimension := range costOverviewDimensions {
		query := `INSERT INTO observation_cost_overview_daily(scope,dimension_id,day_utc,currency,record_count,prompt_tokens,completion_tokens,total_tokens,estimated_cost,internal_cost)
 SELECT ?,` + dimension.expression + `,DATE(cr.recorded_at),` + normalizedCostCurrencySQL + `,COUNT(*),SUM(cr.prompt_tokens),SUM(cr.completion_tokens),SUM(cr.total_tokens),SUM(cr.estimated_cost),SUM(cr.internal_cost) FROM cost_records cr WHERE ` + where + ` GROUP BY ` + dimension.expression + `,DATE(cr.recorded_at),` + normalizedCostCurrencySQL + ` ORDER BY ` + dimension.expression + `,DATE(cr.recorded_at),` + normalizedCostCurrencySQL + `
 ON DUPLICATE KEY UPDATE record_count=record_count+VALUES(record_count),prompt_tokens=prompt_tokens+VALUES(prompt_tokens),completion_tokens=completion_tokens+VALUES(completion_tokens),total_tokens=total_tokens+VALUES(total_tokens),estimated_cost=estimated_cost+VALUES(estimated_cost),internal_cost=internal_cost+VALUES(internal_cost)`
		if _, e := tx.SQL().ExecContext(ctx, query, append([]interface{}{dimension.scope}, args...)...); e != nil {
			return e
		}
	}
	return nil
}

// Complete UTC days read persisted sums. Fractional boundary days read the
// immutable ledger. Subtract post-cursor additions to preserve the first-page
// snapshot even when later deliveries carry old recorded_at timestamps.
func costOverviewRollupFrom(scope string, since, until time.Time, maxID int, options ...bool) (string, []interface{}) {
	currentSnapshot := len(options) > 0 && options[0]
	collapseDays := len(options) > 1 && options[1]
	low := since.UTC().Truncate(24 * time.Hour)
	if low.Before(since) {
		low = low.Add(24 * time.Hour)
	}
	high := until.UTC().Truncate(24 * time.Hour)
	dimension := "''"
	for _, v := range costOverviewDimensions {
		if v.scope == scope {
			dimension = v.expression
		}
	}
	sums := `record_count,prompt_tokens,completion_tokens,total_tokens,estimated_cost,internal_cost`
	parts := `SELECT dimension_id,day_utc,currency,` + sums + ` FROM observation_cost_overview_daily WHERE scope=? AND day_utc>=? AND day_utc<?`
	args := []interface{}{scope, low, high}
	if !since.Equal(low) || !until.Equal(high) {
		parts += ` UNION ALL SELECT ` + dimension + `,DATE(cr.recorded_at),` + normalizedCostCurrencySQL + `,COUNT(*),SUM(cr.prompt_tokens),SUM(cr.completion_tokens),SUM(cr.total_tokens),SUM(cr.estimated_cost),SUM(cr.internal_cost) FROM cost_records cr WHERE cr.recorded_at>=? AND cr.recorded_at<? AND cr.id<=? AND (cr.recorded_at<? OR cr.recorded_at>=?) GROUP BY ` + dimension + `,DATE(cr.recorded_at),` + normalizedCostCurrencySQL
		args = append(args, since, until, maxID, low, high)
	}
	if !currentSnapshot {
		parts += ` UNION ALL SELECT ` + dimension + `,DATE(cr.recorded_at),` + normalizedCostCurrencySQL + `,-COUNT(*),-SUM(cr.prompt_tokens),-SUM(cr.completion_tokens),-SUM(cr.total_tokens),-SUM(cr.estimated_cost),-SUM(cr.internal_cost) FROM cost_records cr WHERE cr.id>? AND cr.recorded_at>=? AND cr.recorded_at<? GROUP BY ` + dimension + `,DATE(cr.recorded_at),` + normalizedCostCurrencySQL
		args = append(args, maxID, low, high)
	}
	user, instance, model := "NULL", "NULL", "''"
	switch scope {
	case "user":
		user = "CAST(NULLIF(dimension_id,'') AS UNSIGNED)"
	case "instance":
		instance = "CAST(NULLIF(dimension_id,'') AS UNSIGNED)"
	case "model":
		model = "dimension_id"
	}
	totals := []string{}
	for _, column := range strings.Split(sums, ",") {
		totals = append(totals, "SUM("+column+") "+column)
	}
	day, group := "day_utc", "dimension_id,day_utc,currency"
	if collapseDays {
		day, group = "MIN(day_utc)", "dimension_id,currency"
	}
	projection, groupBy := strings.Join(totals, ","), " GROUP BY "+group+" HAVING SUM(record_count)>0"
	if currentSnapshot && !collapseDays {
		projection, groupBy = sums, ""
	}
	from := ` FROM (SELECT ` + user + ` user_id,` + instance + ` instance_id,` + model + ` model_name,` + day + ` recorded_at,currency,` + projection + ` FROM (` + parts + `) parts` + groupBy + `) cr LEFT JOIN users u ON u.id=cr.user_id LEFT JOIN instances i ON i.id=cr.instance_id WHERE 1=1`
	return from, args
}
