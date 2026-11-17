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
	PromptTokens, CompletionTokens, TotalTokens, SessionCount int
	EstimatedCost                                             float64
	Currency                                                  string
	FallbackSessionCount, RecentFallbackAuditCount            int
	Latest                                                    *models.ModelInvocation
}

type SessionUsageReport struct {
	Summary SessionUsageReportSummary
	Items   []SessionUsageReportItem
	Total   int
}

type AdminSessionUsageReportItem struct {
	InstanceID                 int
	InstanceName, InstanceType string
	UserID                     int
	Summary                    SessionUsageReportSummary
}

type AdminSessionUsageReport struct {
	Summary SessionUsageReportSummary
	Items   []AdminSessionUsageReportItem
	Total   int
}

// ponytail: SQL aggregation still scans the selected history. Persisted summaries
// and cursor paging are the next capacity step; only the current page reaches Go.
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
 FROM model_invocations mi JOIN scope ON scope.id=mi.instance_id
 WHERE mi.session_id IS NOT NULL AND mi.session_id!='' AND mi.status != ?`
	args = append(args, models.ModelInvocationStatusBlocked)
	if len(exactSession) > 0 {
		query += " AND mi.session_id = ?"
		args = append(args, exactSession[0])
	}
	query, args = appendTimeFilter(query, args, filter, "mi.created_at")
	query += `), tokens AS (SELECT instance_id,session_id,SUM(prompt_tokens) prompt_tokens,SUM(completion_tokens) completion_tokens,SUM(total_tokens) total_tokens,COUNT(*) invocation_count,MIN(created_at) first_seen_at,MAX(created_at) last_seen_at FROM invocation_rows GROUP BY instance_id,session_id),
 cost_groups AS (SELECT cr.instance_id,cr.session_id,SUM(cr.estimated_cost) estimated_cost,
 MAX(COALESCE(NULLIF(UPPER(TRIM(cr.currency)),''),'USD')) currency,
 COUNT(DISTINCT COALESCE(NULLIF(UPPER(TRIM(cr.currency)),''),'USD')) currency_count
 FROM cost_records cr JOIN tokens t ON t.instance_id=cr.instance_id AND t.session_id=cr.session_id WHERE 1=1`
	query, args = appendEligibleCostInvocationFilter(query, args, filter)
	query += ` GROUP BY cr.instance_id,cr.session_id), sessions AS (
 SELECT t.*,cs.title,COALESCE(c.estimated_cost,0) estimated_cost,c.currency,COALESCE(c.currency_count,0) currency_count
 FROM tokens t LEFT JOIN cost_groups c ON c.instance_id=t.instance_id AND c.session_id=t.session_id
 LEFT JOIN chat_sessions cs ON cs.instance_id=t.instance_id AND cs.session_id=t.session_id),
 summaries AS (SELECT scope.id instance_id,scope.name,scope.type,scope.user_id,
 COALESCE(SUM(s.prompt_tokens),0) prompt_tokens,COALESCE(SUM(s.completion_tokens),0) completion_tokens,COALESCE(SUM(s.total_tokens),0) total_tokens,
 COALESCE(SUM(s.estimated_cost),0) estimated_cost,COUNT(s.session_id) session_count,COALESCE(MAX(s.currency),'USD') currency,
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
	return driver.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
}

const sessionSummaryColumns = `su.prompt_tokens,su.completion_tokens,su.total_tokens,su.estimated_cost,su.session_count,su.currency,su.currency_count,su.fallback_count,COALESCE(fa.fallback_audit_count,0),
 li.id,li.requested_model,li.actual_provider_model,li.status,li.created_at,li.latency_ms,li.ttft_ms,li.trace_id,li.request_id,li.is_streaming,li.attempt_no`
const sessionSummaryJoins = ` FROM summaries su LEFT JOIN latest_ranked li ON li.instance_id=su.instance_id AND li.rn=1 LEFT JOIN fallback_audits fa ON fa.instance_id=su.instance_id`

type rowScanner interface{ Scan(...interface{}) error }

func scanSessionReportSummary(row rowScanner, summary *SessionUsageReportSummary, prefix ...interface{}) error {
	var currencyCount int
	var id, latency, ttft, attempt sql.NullInt64
	var requested, actual, status, trace, request sql.NullString
	var created sql.NullTime
	var streaming sql.NullBool
	dest := append(prefix, &summary.PromptTokens, &summary.CompletionTokens, &summary.TotalTokens, &summary.EstimatedCost, &summary.SessionCount, &summary.Currency, &currencyCount, &summary.FallbackSessionCount, &summary.RecentFallbackAuditCount,
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
	if err = tx.QueryRowContext(ctx, cte+" SELECT COUNT(*) FROM sessions"+where, args...).Scan(&result.Total); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, cte+` SELECT session_id,title,prompt_tokens,completion_tokens,total_tokens,invocation_count,estimated_cost,COALESCE(currency,'USD'),first_seen_at,last_seen_at FROM sessions`+where+` ORDER BY last_seen_at DESC,session_id DESC LIMIT ? OFFSET ?`, append(args, limit, (page-1)*limit)...)
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
	err = tx.QueryRowContext(ctx, cte+` SELECT COUNT(*),COALESCE(SUM(prompt_tokens),0),COALESCE(SUM(completion_tokens),0),COALESCE(SUM(total_tokens),0),COALESCE(SUM(estimated_cost),0),COALESCE(SUM(session_count),0),COALESCE(MAX(CASE WHEN currency_count>0 THEN currency END),'USD'),GREATEST(COALESCE(MAX(currency_count),0),COUNT(DISTINCT CASE WHEN currency_count>0 THEN currency END)) FROM summaries`, args...).Scan(&result.Total, &result.Summary.PromptTokens, &result.Summary.CompletionTokens, &result.Summary.TotalTokens, &result.Summary.EstimatedCost, &result.Summary.SessionCount, &result.Summary.Currency, &currencies)
	if err != nil {
		return nil, err
	}
	if currencies > 1 {
		return nil, ErrMixedCostCurrencies
	}
	rows, err := tx.QueryContext(ctx, cte+" SELECT su.instance_id,su.name,su.type,su.user_id,"+sessionSummaryColumns+sessionSummaryJoins+` ORDER BY su.total_tokens DESC,su.instance_id DESC LIMIT ? OFFSET ?`, append(args, limit, (page-1)*limit)...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item AdminSessionUsageReportItem
		if err = scanSessionReportSummary(rows, &item.Summary, &item.InstanceID, &item.InstanceName, &item.InstanceType, &item.UserID); err != nil {
			rows.Close()
			return nil, err
		}
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
	for i := range result.Items {
		if err = hydrateSessionReportRetry(ctx, tx, &result.Items[i].Summary, filter); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// A single matching status event is hydrated for each displayed instance only.
// This preserves stream retry hints without loading an entire reused trace.
func hydrateSessionReportRetry(ctx context.Context, tx *sql.Tx, summary *SessionUsageReportSummary, filter SessionUsageFilter) error {
	latest := summary.Latest
	if filter.Until != nil || latest == nil || latest.Status != models.ModelInvocationStatusFailed || !latest.IsStreaming || latest.RequestID == "" {
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
