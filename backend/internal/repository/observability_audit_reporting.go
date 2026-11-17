package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// ListAuditReport filters the complete trace set before paging. Bodies are never
// selected for a list; event-only traces remain visible if invocation logging failed.
func (r *modelInvocationRepository) ListAuditReport(page, limit int, search, status, model string) ([]models.ModelInvocation, int, error) {
	if page < 1 || limit < 1 || limit > 100 || page > int(^uint(0)>>1)/limit {
		return nil, 0, fmt.Errorf("invalid audit page")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cte := auditReportCTE
	where := " FROM report_rows r LEFT JOIN users u ON u.id=r.user_id WHERE 1=1"
	args := []interface{}{}
	if status = strings.ToLower(strings.TrimSpace(status)); status != "" {
		where += " AND r.status=?"
		args = append(args, status)
	}
	if model = strings.ToLower(strings.TrimSpace(model)); model != "" {
		where += " AND (LOCATE(?,LOWER(r.requested_model))>0 OR LOCATE(?,LOWER(r.actual_provider_model))>0)"
		args = append(args, model, model)
	}
	if search = strings.ToLower(strings.TrimSpace(search)); search != "" {
		where += " AND (LOCATE(?,LOWER(CONCAT_WS(CHAR(0),r.trace_id,r.request_id,r.requested_model,r.actual_provider_model,r.provider_type,u.username,r.event_type,r.message)))>0)"
		args = append(args, search)
	}
	items := make([]models.ModelInvocation, 0, limit)
	total := 0
	err := r.sess.TxContext(ctx, func(tx db.Session) error {
		row, err := tx.SQL().QueryRowContext(ctx, cte+" SELECT COUNT(*)"+where, args...)
		if err != nil {
			return err
		}
		if err := row.Scan(&total); err != nil {
			return err
		}
		pageArgs := append(append([]interface{}{}, args...), limit, (page-1)*limit)
		rows, err := tx.SQL().QueryContext(ctx, cte+` SELECT r.id,r.trace_id,r.request_id,r.session_id,r.user_id,r.instance_id,
r.requested_model,r.actual_provider_model,r.provider_type,r.status,
r.prompt_tokens,r.completion_tokens,r.total_tokens,r.latency_ms,r.ttft_ms,r.generation_ms,
r.tokens_per_second,r.attempt_no,r.error_message,r.created_at,r.completed_at`+where+" ORDER BY r.created_at DESC,r.trace_id DESC LIMIT ? OFFSET ?", pageArgs...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v models.ModelInvocation
			if err := rows.Scan(&v.ID, &v.TraceID, &v.RequestID, &v.SessionID, &v.UserID, &v.InstanceID,
				&v.RequestedModel, &v.ActualProviderModel, &v.ProviderType, &v.Status,
				&v.PromptTokens, &v.CompletionTokens, &v.TotalTokens, &v.LatencyMs, &v.TTFTMs, &v.GenerationMs,
				&v.TokensPerSecond, &v.AttemptNo, &v.ErrorMessage, &v.CreatedAt, &v.CompletedAt); err != nil {
				return err
			}
			items = append(items, v)
		}
		return rows.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, 0, fmt.Errorf("query audit report: %w", err)
	}
	return items, total, nil
}

// Aggregate each source independently before joining. This prevents calls and
// costs under a reused trace from multiplying one another.
const auditReportCTE = `WITH invocation_groups AS (
 SELECT trace_id,MIN(id) first_id,MAX(id) last_id,MIN(created_at) created_at,
 MAX(completed_at) completed_at,SUM(prompt_tokens) prompt_tokens,
 SUM(completion_tokens) completion_tokens,SUM(total_tokens) total_tokens,SUM(latency_ms) latency_ms
 FROM model_invocations WHERE trace_id<>'' GROUP BY trace_id
), event_groups AS (
 SELECT e.trace_id,MAX(e.id) last_id FROM audit_events e
 WHERE e.trace_id<>'' AND NOT EXISTS(SELECT 1 FROM model_invocations m WHERE m.trace_id=e.trace_id)
 GROUP BY e.trace_id
), fallback_costs AS (
 SELECT c.trace_id,MAX(c.id) last_id,SUM(c.prompt_tokens) prompt_tokens,
 SUM(c.completion_tokens) completion_tokens,SUM(c.total_tokens) total_tokens
 FROM cost_records c JOIN event_groups g ON g.trace_id=c.trace_id GROUP BY c.trace_id
), report_rows AS (
 SELECT m.id,m.trace_id,f.request_id,m.session_id,m.user_id,m.instance_id,
 m.requested_model,m.actual_provider_model,m.provider_type,
 CASE WHEN m.status IN ('completed','blocked','failed') THEN m.status ELSE 'pending' END status,
 g.prompt_tokens,g.completion_tokens,g.total_tokens,g.latency_ms,m.ttft_ms,m.generation_ms,
 m.tokens_per_second,m.attempt_no,m.error_message,g.created_at,g.completed_at,
 '' event_type,'' message
 FROM invocation_groups g JOIN model_invocations m ON m.id=g.last_id JOIN model_invocations f ON f.id=g.first_id
 UNION ALL
 SELECT e.id,e.trace_id,COALESCE(e.request_id,''),e.session_id,COALESCE(e.user_id,c.user_id),COALESCE(e.instance_id,c.instance_id),
 'Auto',COALESCE(NULLIF(c.model_name,''),e.event_type),'gateway',
 CASE WHEN LOCATE('completed',e.event_type)>0 THEN 'completed'
 WHEN LOCATE('blocked',e.event_type)>0 THEN 'blocked' WHEN LOCATE('failed',e.event_type)>0 THEN 'failed' ELSE 'pending' END,
 COALESCE(fc.prompt_tokens,0),COALESCE(fc.completion_tokens,0),COALESCE(fc.total_tokens,0),
 NULL,NULL,NULL,NULL,0,NULL,e.created_at,NULL,e.event_type,e.message
 FROM event_groups g JOIN audit_events e ON e.id=g.last_id
 LEFT JOIN fallback_costs fc ON fc.trace_id=e.trace_id LEFT JOIN cost_records c ON c.id=fc.last_id
)`
