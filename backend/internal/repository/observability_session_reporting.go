package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
)

type SessionUsageReportItem struct {
	SessionID                                                    string
	Title                                                        *string
	PromptTokens, CompletionTokens, TotalTokens, InvocationCount int
	EstimatedCost                                                float64
	Currency                                                     string
	FirstSeenAt, LastSeenAt                                      time.Time
}

type SessionUsageReportSummary struct {
	InvocationCount                                           int64
	PromptTokens, CompletionTokens, TotalTokens, SessionCount int
	EstimatedCost                                             float64
	Currency                                                  string
	FallbackSessionCount, RecentFallbackAuditCount            int
	Latest                                                    *models.ModelInvocation
}

type SessionUsageReport struct {
	NextCursor string
	Summary    SessionUsageReportSummary
	Items      []SessionUsageReportItem
	Total      int
}

type AdminSessionUsageReportItem struct {
	InstanceID                 int
	InstanceName, InstanceType string
	UserID                     int
	Summary                    SessionUsageReportSummary
}

type AdminSessionUsageReport struct {
	NextCursor string
	Summary    SessionUsageReportSummary
	Items      []AdminSessionUsageReportItem
	Total      int
}

// Reporting reads durable narrow facts and daily rollups; payload retention cannot change totals.
func sessionUsageReportSQL(instanceID int, search string, filter SessionUsageFilter, exactSession ...string) (string, []interface{}) {
	query := `WITH scope AS (SELECT id,name,type,user_id FROM instances WHERE `
	args := []interface{}{}
	if instanceID != 0 {
		query += "id = ?"
		args = append(args, instanceID)
	} else {
		query += `type IN ('openclaw','hermes','opencode','workbuddy','deepseek-harness') AND status IN ('running','creating','stopped','error')`
		if search = strings.ToLower(strings.TrimSpace(search)); search != "" {
			query += ` AND (LOCATE(?,LOWER(name))>0 OR LOCATE(?,CAST(id AS CHAR))>0 OR LOCATE(?,LOWER(type))>0)`
			args = append(args, search, search, search)
		}
	}
	query += `), invocation_rows AS (SELECT mi.id,mi.instance_id,mi.session_id,mi.prompt_tokens,mi.completion_tokens,mi.total_tokens,mi.created_at,
 mi.requested_model,mi.actual_provider_model,mi.status,mi.latency_ms,mi.ttft_ms,mi.trace_id,mi.request_id,mi.is_streaming,mi.attempt_no
 FROM observation_invocation_summaries mi JOIN scope ON scope.id=mi.instance_id
 WHERE mi.session_id IS NOT NULL AND mi.session_id!='' AND mi.status != ?`
	args = append(args, models.ModelInvocationStatusBlocked)
	if len(exactSession) > 0 {
		query += " AND mi.session_id = ?"
		args = append(args, exactSession[0])
	}
	query, args = appendTimeFilter(query, args, filter, "mi.created_at")
	tokenSQL, tokenArgs := sessionDailyTokenSQL(filter, exactSession...)
	args = append(args, tokenArgs...)
	costSQL, costArgs := sessionCostSummarySQL(filter)
	args = append(args, costArgs...)
	query += `), tokens AS (` + tokenSQL + `), eligible_costs AS (` + costSQL + `),
 cost_groups AS (SELECT cr.instance_id,cr.session_id,SUM(cr.estimated_cost) estimated_cost,
 MAX(COALESCE(NULLIF(UPPER(TRIM(cr.currency)),''),'USD')) currency,
 COUNT(DISTINCT COALESCE(NULLIF(UPPER(TRIM(cr.currency)),''),'USD')) currency_count
 FROM eligible_costs cr GROUP BY cr.instance_id,cr.session_id), sessions AS (
 SELECT t.*,cs.title,COALESCE(c.estimated_cost,0) estimated_cost,c.currency,COALESCE(c.currency_count,0) currency_count
 FROM tokens t LEFT JOIN cost_groups c ON c.instance_id=t.instance_id AND c.session_id=t.session_id
 LEFT JOIN chat_sessions cs ON cs.instance_id=t.instance_id AND cs.session_id=t.session_id),
 summaries AS (SELECT scope.id instance_id,scope.name,scope.type,scope.user_id,
 COALESCE(SUM(s.prompt_tokens),0) prompt_tokens,COALESCE(SUM(s.completion_tokens),0) completion_tokens,COALESCE(SUM(s.total_tokens),0) total_tokens,
 COALESCE(SUM(s.estimated_cost),0) estimated_cost,COUNT(s.session_id) session_count,COALESCE(SUM(s.invocation_count),0) invocation_count,COALESCE(MAX(s.currency),'USD') currency,
 GREATEST(COALESCE(MAX(s.currency_count),0),COUNT(DISTINCT s.currency)) currency_count,
 COALESCE(SUM(LEFT(TRIM(s.session_id),5) = BINARY 'sess_'),0) fallback_count
 FROM scope LEFT JOIN sessions s ON s.instance_id=scope.id GROUP BY scope.id,scope.name,scope.type,scope.user_id),
 latest_ranked AS (SELECT invocation_rows.*,ROW_NUMBER() OVER(PARTITION BY instance_id ORDER BY created_at DESC,session_id DESC,id DESC) rn FROM invocation_rows),
 fallback_audits AS (SELECT ae.instance_id,COUNT(*) fallback_audit_count FROM audit_events ae JOIN scope ON scope.id=ae.instance_id
 WHERE ae.event_type='gateway.session.fallback' AND ae.created_at>=? GROUP BY ae.instance_id)`
	since := time.Now().UTC().Add(-24 * time.Hour)
	if filter.Since != nil && filter.Since.After(since) {
		since = *filter.Since
	}
	args = append(args, since)
	return query, args
}

func (r *modelInvocationRepository) sessionReportTransaction(ctx context.Context) (*sql.Tx, error) {
	driver, ok := r.sess.Driver().(*sql.DB)
	if !ok {
		return nil, fmt.Errorf("session reporting requires SQL database driver")
	}
	tx, err := driver.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	var target int64
	var ready bool
	if err = tx.QueryRowContext(ctx, `SELECT SUM(target_id),MIN(ready) AND NOT EXISTS(SELECT 1 FROM observation_invocation_pending) AND NOT EXISTS(SELECT 1 FROM observation_cost_pending) FROM observation_summary_state WHERE id IN(1,2)`).Scan(&target, &ready); err != nil {
		tx.Rollback()
		return nil, err
	}
	if !ready {
		var processed int64
		var pending int64
		_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM observation_cost_facts WHERE resolved=FALSE`).Scan(&pending)
		_ = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(after_id),0) FROM history_maintenance_cursors WHERE kind IN('query_summary','query_cost_summary')`).Scan(&processed)
		tx.Rollback()
		return nil, &SummaryRebuildingError{Processed: processed, Target: target, PendingCosts: pending}
	}
	return tx, nil
}

const sessionSummaryColumns = `su.prompt_tokens,su.completion_tokens,su.total_tokens,su.estimated_cost,su.session_count,su.invocation_count,su.currency,su.currency_count,su.fallback_count,COALESCE(fa.fallback_audit_count,0),
 li.id,li.requested_model,li.actual_provider_model,li.status,li.created_at,li.latency_ms,li.ttft_ms,li.trace_id,li.request_id,li.is_streaming,li.attempt_no`
const sessionSummaryJoins = ` FROM summaries su LEFT JOIN latest_ranked li ON li.instance_id=su.instance_id AND li.rn=1 LEFT JOIN fallback_audits fa ON fa.instance_id=su.instance_id`

type rowScanner interface{ Scan(...interface{}) error }

func scanSessionReportSummary(row rowScanner, summary *SessionUsageReportSummary, prefix ...interface{}) error {
	var currencyCount int
	var id, latency, ttft, attempt sql.NullInt64
	var requested, actual, status, trace, request sql.NullString
	var created sql.NullTime
	var streaming sql.NullBool
	dest := append(prefix, &summary.PromptTokens, &summary.CompletionTokens, &summary.TotalTokens, &summary.EstimatedCost, &summary.SessionCount, &summary.InvocationCount, &summary.Currency, &currencyCount, &summary.FallbackSessionCount, &summary.RecentFallbackAuditCount,
		&id, &requested, &actual, &status, &created, &latency, &ttft, &trace, &request, &streaming, &attempt)
	if err := row.Scan(dest...); err != nil {
		return err
	}
	if currencyCount > 1 {
		return ErrMixedCostCurrencies
	}
	if id.Valid {
		summary.Latest = &models.ModelInvocation{ID: int(id.Int64), RequestedModel: requested.String, ActualProviderModel: actual.String, Status: status.String, CreatedAt: created.Time, TraceID: trace.String, RequestID: request.String, IsStreaming: streaming.Bool, AttemptNo: int(attempt.Int64)}
		if latency.Valid {
			n := int(latency.Int64)
			summary.Latest.LatencyMs = &n
		}
		if ttft.Valid {
			n := int(ttft.Int64)
			summary.Latest.TTFTMs = &n
		}
	}
	return nil
}

func (r *modelInvocationRepository) ListSessionUsageReport(instanceID, page, limit int, search string, filter SessionUsageFilter) (*SessionUsageReport, error) {
	if instanceID < 1 || page < 1 || limit < 1 || limit > 100 || page > int(^uint(0)>>1)/limit {
		return nil, fmt.Errorf("invalid session report page or instance")
	}
	cursor, cursorErr := decodeSessionCursor(filter.Cursor, instanceID, search, filter)
	if cursorErr != nil {
		return nil, cursorErr
	}
	if filter.Cursor != "" {
		filter.Since, filter.Until = cursor.Since, cursor.Until
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	tx, err := r.sessionReportTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	cte, args := sessionUsageReportSQL(instanceID, "", filter)
	result := &SessionUsageReport{Items: []SessionUsageReportItem{}, Summary: SessionUsageReportSummary{Currency: "USD"}}
	err = scanSessionReportSummary(tx.QueryRowContext(ctx, cte+" SELECT "+sessionSummaryColumns+sessionSummaryJoins, args...), &result.Summary)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("session summary: %w", err)
	}
	where := ""
	search = strings.ToLower(strings.TrimSpace(search))
	if search != "" {
		where = ` WHERE LOCATE(?,LOWER(TRIM(session_id)))>0 OR LOCATE(?,LOWER(COALESCE(title,'')))>0`
		args = append(args, search, search)
	}
	var membership string
	if err = tx.QueryRowContext(ctx, cte+" SELECT COUNT(*),"+sessionMembershipSQL("JSON_ARRAY(session_id,title)")+" FROM sessions"+where, args...).Scan(&result.Total, &membership); err != nil {
		return nil, err
	}
	if err = checkSessionPopulation(&cursor, filter.Cursor, result.Summary, fmt.Sprintf("%d/%s", result.Total, membership)); err != nil {
		return nil, err
	}
	pageArgs := append([]interface{}{}, args...)
	pageWhere := where
	offset := (page - 1) * limit
	if filter.Cursor != "" {
		if pageWhere == "" {
			pageWhere = " WHERE "
		} else {
			pageWhere = " WHERE (" + strings.TrimPrefix(pageWhere, " WHERE ") + ") AND "
		}
		pageWhere += "(last_seen_at,session_id)<(?,?)"
		pageArgs = append(pageArgs, cursor.At, cursor.Session)
		offset = 0
	}
	rows, err := tx.QueryContext(ctx, cte+` SELECT session_id,title,prompt_tokens,completion_tokens,total_tokens,invocation_count,estimated_cost,COALESCE(currency,'USD'),first_seen_at,last_seen_at FROM sessions`+pageWhere+` ORDER BY last_seen_at DESC,session_id DESC LIMIT ? OFFSET ?`, append(pageArgs, limit+1, offset)...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item SessionUsageReportItem
		if err = rows.Scan(&item.SessionID, &item.Title, &item.PromptTokens, &item.CompletionTokens, &item.TotalTokens, &item.InvocationCount, &item.EstimatedCost, &item.Currency, &item.FirstSeenAt, &item.LastSeenAt); err != nil {
			rows.Close()
			return nil, err
		}
		result.Items = append(result.Items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if result.Summary.Latest != nil {
		n := instanceID
		result.Summary.Latest.InstanceID = &n
	}
	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		last := result.Items[limit-1]
		cursor.At = last.LastSeenAt
		cursor.Session = last.SessionID
		result.NextCursor = encodeSessionCursor(cursor)
	}
	if err = hydrateSessionReportRetry(ctx, tx, &result.Summary, filter); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *modelInvocationRepository) ListAdminSessionUsageReport(page, limit int, search string, filter SessionUsageFilter) (*AdminSessionUsageReport, error) {
	if page < 1 || limit < 1 || limit > 100 || page > int(^uint(0)>>1)/limit {
		return nil, fmt.Errorf("invalid admin session report page")
	}
	cursor, cursorErr := decodeSessionCursor(filter.Cursor, 0, search, filter)
	if cursorErr != nil {
		return nil, cursorErr
	}
	if filter.Cursor != "" {
		filter.Since, filter.Until = cursor.Since, cursor.Until
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	tx, err := r.sessionReportTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	cte, args := sessionUsageReportSQL(0, search, filter)
	result := &AdminSessionUsageReport{Items: []AdminSessionUsageReportItem{}, Summary: SessionUsageReportSummary{Currency: "USD"}}
	var currencies int
	var membership string
	pageArgs := append([]interface{}{}, args...)
	where := ""
	offset := (page - 1) * limit
	if filter.Cursor != "" {
		where = " WHERE (su.total_tokens,su.instance_id)<(?,?)"
		pageArgs = append(pageArgs, cursor.Tokens, cursor.ID)
		offset = 0
	}
	pageQuery, pageArgs := adminSessionPageSQL(cte, pageArgs, where, limit+1, offset, filter)
	rows, err := tx.QueryContext(ctx, pageQuery, pageArgs...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item AdminSessionUsageReportItem
		var instanceID, userID sql.NullInt64
		var name, kind sql.NullString
		if err = scanSessionReportSummary(rows, &item.Summary, &result.Total, &result.Summary.PromptTokens, &result.Summary.CompletionTokens, &result.Summary.TotalTokens, &result.Summary.EstimatedCost, &result.Summary.SessionCount, &result.Summary.InvocationCount, &result.Summary.Currency, &currencies, &membership, &instanceID, &name, &kind, &userID); err != nil {
			rows.Close()
			return nil, err
		}
		if !instanceID.Valid {
			continue
		}
		item.InstanceID, item.InstanceName, item.InstanceType, item.UserID = int(instanceID.Int64), name.String, kind.String, int(userID.Int64)
		if item.Summary.Latest != nil {
			n := item.InstanceID
			item.Summary.Latest.InstanceID = &n
		}
		result.Items = append(result.Items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if currencies > 1 {
		return nil, ErrMixedCostCurrencies
	}
	if err = checkSessionPopulation(&cursor, filter.Cursor, result.Summary, fmt.Sprintf("%d/%s", result.Total, membership)); err != nil {
		return nil, err
	}
	for i := range result.Items {
		if err = hydrateSessionReportRetry(ctx, tx, &result.Items[i].Summary, filter); err != nil {
			return nil, err
		}
	}
	if len(result.Items) > limit {
		result.Items = result.Items[:limit]
		last := result.Items[limit-1]
		cursor.Tokens = last.Summary.TotalTokens
		cursor.ID = last.InstanceID
		result.NextCursor = encodeSessionCursor(cursor)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func adminSessionPageSQL(cte string, pageArgs []interface{}, where string, limit, offset int, filter SessionUsageFilter) (string, []interface{}) {
	// One statement shares the materialized rollups between the global total and
	// page selection; separate statements repeated the entire aggregation twice.
	pageCTE := cte + `,report_totals AS (SELECT COUNT(*) instance_count,COALESCE(SUM(prompt_tokens),0) prompt_tokens,COALESCE(SUM(completion_tokens),0) completion_tokens,COALESCE(SUM(total_tokens),0) total_tokens,COALESCE(SUM(estimated_cost),0) estimated_cost,COALESCE(SUM(session_count),0) session_count,COALESCE(SUM(invocation_count),0) invocation_count,COALESCE(MAX(CASE WHEN currency_count>0 THEN currency END),'USD') currency,GREATEST(COALESCE(MAX(currency_count),0),COUNT(DISTINCT CASE WHEN currency_count>0 THEN currency END)) currency_count,` + sessionMembershipSQL("JSON_ARRAY(instance_id,name,type,user_id)") + ` membership FROM summaries),page_summaries AS (SELECT su.* FROM summaries su` + where + ` ORDER BY su.total_tokens DESC,su.instance_id DESC LIMIT ? OFFSET ?)`
	// Only the displayed instances need a latest invocation. Ranking the entire
	// window here made a 20-row page sort hundreds of thousands of facts.
	pageJoins := ` FROM report_totals totals LEFT JOIN page_summaries su ON TRUE LEFT JOIN observation_invocation_summaries li ON li.id=(SELECT latest.id FROM observation_invocation_summaries latest WHERE latest.instance_id=su.instance_id AND latest.session_id IS NOT NULL AND latest.session_id!='' AND latest.status<>'blocked'`
	latestArgs := []interface{}{}
	pageJoins, latestArgs = appendTimeFilter(pageJoins, latestArgs, filter, "latest.created_at")
	pageJoins += ` ORDER BY latest.created_at DESC,latest.session_id DESC,latest.id DESC LIMIT 1) LEFT JOIN fallback_audits fa ON fa.instance_id=su.instance_id`
	pageArgs = append(pageArgs, limit, offset)
	pageArgs = append(pageArgs, latestArgs...)
	columns := sessionSummaryColumns
	for _, column := range []string{"prompt_tokens", "completion_tokens", "total_tokens", "estimated_cost", "session_count", "invocation_count", "currency_count", "fallback_count"} {
		columns = strings.ReplaceAll(columns, "su."+column, "COALESCE(su."+column+",0)")
	}
	columns = strings.ReplaceAll(columns, "su.currency,", "COALESCE(su.currency,'USD'),")
	return pageCTE + " SELECT totals.*,su.instance_id,su.name,su.type,su.user_id," + columns + pageJoins + ` ORDER BY su.total_tokens DESC,su.instance_id DESC`, pageArgs
}

// A single matching status event is hydrated for each displayed instance only.
// This preserves stream retry hints without loading an entire reused trace.
func hydrateSessionReportRetry(ctx context.Context, tx *sql.Tx, summary *SessionUsageReportSummary, filter SessionUsageFilter) error {
	latest := summary.Latest
	if (filter.Until != nil && (time.Since(*filter.Until) > time.Minute || time.Until(*filter.Until) > time.Minute)) || latest == nil || latest.Status != models.ModelInvocationStatusFailed || !latest.IsStreaming || latest.RequestID == "" {
		return nil
	}
	var eventType, nextModel string
	var created time.Time
	err := tx.QueryRowContext(ctx, `SELECT event_type,created_at,
 CASE WHEN JSON_VALID(details) THEN COALESCE(JSON_UNQUOTE(JSON_EXTRACT(details,'$.next_model')),'') ELSE '' END
 FROM audit_events WHERE trace_id=? AND request_id=? AND instance_id=? AND created_at>=?
 AND (event_type='gateway.request.stream_failed' OR (event_type='gateway.deployment.failover'
 AND CASE WHEN JSON_VALID(details) THEN JSON_TYPE(JSON_EXTRACT(details,'$.failed_attempt'))='INTEGER' AND JSON_EXTRACT(details,'$.failed_attempt')=? AND JSON_TYPE(JSON_EXTRACT(details,'$.next_model'))='STRING' AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(details,'$.next_model')),'')!='' ELSE FALSE END))
 ORDER BY (event_type='gateway.request.stream_failed') DESC,id DESC LIMIT 1`, latest.TraceID, latest.RequestID, *latest.InstanceID, latest.CreatedAt.Truncate(time.Second), latest.AttemptNo).Scan(&eventType, &created, &nextModel)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if eventType == "gateway.request.stream_failed" {
		latest.Status = "request_failed"
		return nil
	}
	if time.Since(created) > 10*time.Minute {
		latest.Status = "retry_unknown"
		return nil
	}
	latest.Status = "retrying"
	latest.ActualProviderModel = nextModel
	latest.CreatedAt = created
	latest.LatencyMs = nil
	latest.TTFTMs = nil
	return nil
}

// GetSessionUsageReportItem filters the invocation source before aggregation, so
// a detail request never materializes neighboring sessions in Go or in the CTE.
func (r *modelInvocationRepository) GetSessionUsageReportItem(instanceID int, sessionID string, filter SessionUsageFilter) (*SessionUsageReportItem, error) {
	if instanceID < 1 {
		return nil, fmt.Errorf("invalid session report instance")
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	tx, err := r.sessionReportTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	cte, args := sessionUsageReportSQL(instanceID, "", filter, sessionID)
	var item SessionUsageReportItem
	var currencyCount int
	err = tx.QueryRowContext(ctx, cte+` SELECT session_id,title,prompt_tokens,completion_tokens,total_tokens,invocation_count,estimated_cost,COALESCE(currency,'USD'),first_seen_at,last_seen_at,currency_count FROM sessions`, args...).Scan(&item.SessionID, &item.Title, &item.PromptTokens, &item.CompletionTokens, &item.TotalTokens, &item.InvocationCount, &item.EstimatedCost, &item.Currency, &item.FirstSeenAt, &item.LastSeenAt, &currencyCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if currencyCount > 1 {
		return nil, ErrMixedCostCurrencies
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &item, nil
}
