package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

var ErrInvalidAuditQuery = errors.New("invalid audit query")

type AuditReportQuery struct {
	Limit                         int
	Search, Status, Model, Cursor string
	Since, Until                  *time.Time
}
type AuditReportPage struct {
	Items           []models.ModelInvocation
	TruncatedErrors map[int]bool
	Total           int
	NextCursor      string
	Since, Until    time.Time
}
type auditReportCursor struct {
	Version      int
	Filter       string
	At           time.Time
	Trace        string
	Fence        int64
	Since, Until time.Time
}

func auditFilterKey(q AuditReportQuery) string {
	b, _ := json.Marshal([]interface{}{q.Search, q.Status, q.Model, q.Limit})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func normalizeAuditReportQuery(q AuditReportQuery) (AuditReportQuery, auditReportCursor, error) {
	q.Search = strings.ToLower(strings.TrimSpace(q.Search))
	q.Status = strings.ToLower(strings.TrimSpace(q.Status))
	q.Model = strings.ToLower(strings.TrimSpace(q.Model))
	c := auditReportCursor{Version: 1, Filter: auditFilterKey(q)}
	bad := func() (AuditReportQuery, auditReportCursor, error) { return q, c, ErrInvalidAuditQuery }
	if q.Limit < 1 || q.Limit > 100 || len(q.Search) > 1000 || len(q.Model) > 255 || strings.ContainsRune(q.Search, 0) {
		return bad()
	}
	switch q.Status {
	case "", "completed", "blocked", "failed", "pending":
	default:
		return bad()
	}
	if q.Cursor != "" {
		if len(q.Cursor) > 4096 {
			return bad()
		}
		b, e := base64.RawURLEncoding.DecodeString(q.Cursor)
		if e != nil {
			return bad()
		}
		if e = json.Unmarshal(b, &c); e != nil || c.Version != 1 || c.Filter != auditFilterKey(q) || c.Trace == "" || c.At.IsZero() || c.Fence < 1 {
			return bad()
		}
		if q.Since != nil && !q.Since.Equal(c.Since) || q.Until != nil && !q.Until.Equal(c.Until) {
			return bad()
		}
	} else {
		c.Until = time.Now().UTC()
		if q.Until != nil {
			c.Until = q.Until.UTC()
		}
		c.Since = c.Until.Add(-30 * 24 * time.Hour)
		if q.Since != nil {
			c.Since = q.Since.UTC()
		}
	}
	if c.Since.IsZero() || c.Until.IsZero() || !c.Until.After(c.Since) || c.Until.Sub(c.Since) > 90*24*time.Hour {
		return bad()
	}
	return q, c, nil
}

// ListAuditReportPage reads one persisted row per trace, including cold and
// event-only traces. sort_at is immutable after first materialization so late
// events cannot move a trace across a cursor. sequence_id fences new traces.
func (r *modelInvocationRepository) ListAuditReportPage(ctx context.Context, q AuditReportQuery) (*AuditReportPage, error) {
	q, c, err := normalizeAuditReportQuery(q)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	result := &AuditReportPage{Items: []models.ModelInvocation{}, TruncatedErrors: map[int]bool{}, Since: c.Since, Until: c.Until}
	err = r.sess.TxContext(ctx, func(tx db.Session) error {
		row, e := tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(NOT complete),0)+(SELECT EXISTS(SELECT 1 FROM observation_audit_pending))+(SELECT EXISTS(SELECT 1 FROM observation_invocation_pending))+(SELECT EXISTS(SELECT 1 FROM observation_cost_pending)),COALESCE(SUM(LEAST(after_id,target_id)),0),COALESCE(SUM(target_id),0) FROM observation_audit_backfill`)
		if e != nil {
			return e
		}
		var sources, pending int
		var processed, target int64
		if e = row.Scan(&sources, &pending, &processed, &target); e != nil {
			return e
		}
		if pending > 0 || sources != 5 {
			return &SummaryRebuildingError{Processed: processed, Target: target}
		}
		if q.Cursor == "" {
			row, e = tx.SQL().QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence_id),0) FROM observation_audit_trace_reports`)
			if e != nil {
				return e
			}
			if e = row.Scan(&c.Fence); e != nil {
				return e
			}
		}
		from := ` FROM observation_audit_trace_reports r`
		where := ` WHERE r.id>0 AND r.sort_at>=? AND r.sort_at<? AND r.sequence_id<=?`
		args := []interface{}{c.Since, c.Until, c.Fence}
		if q.Status != "" {
			where += ` AND r.status=?`
			args = append(args, q.Status)
		}
		if q.Model != "" {
			where += ` AND (LOCATE(?,LOWER(r.requested_model))>0 OR LOCATE(?,LOWER(r.actual_provider_model))>0)`
			args = append(args, q.Model, q.Model)
		}
		countQuery := `SELECT COUNT(*)` + from + where
		countArgs := args
		if q.Search != "" {
			clauses := []string{}
			for _, field := range []string{"r.requested_model", "r.actual_provider_model", "u.username", "r.trace_id", "r.request_id", "r.provider_type", "r.event_type", "r.message"} {
				clauses = append(clauses, `LOCATE(?,LOWER(COALESCE(`+field+`,'')))>0`)
			}
			// Model matches use a covering range scan. The disjoint remainder
			// rejects those same rows by index condition before fetching message
			// or joining the current username. Both counts retain this RR fence.
			modelMatch := `(` + strings.Join(clauses[:2], ` OR `) + `)`
			countFrom := from + ` FORCE INDEX(idx_audit_report_model_search)`
			countQuery = `SELECT SUM(n) FROM (SELECT COUNT(*) n` + countFrom + where + ` AND ` + modelMatch +
				` UNION ALL SELECT COUNT(*) n` + countFrom + ` LEFT JOIN users u ON u.id=r.user_id` + where +
				` AND NOT ` + modelMatch + ` AND (` + strings.Join(clauses[2:], ` OR `) + `)) audit_counts`
			countArgs = append(append([]interface{}{}, args...), q.Search, q.Search)
			countArgs = append(countArgs, args...)
			for range clauses {
				countArgs = append(countArgs, q.Search)
				args = append(args, q.Search)
			}
			from += ` LEFT JOIN users u ON u.id=r.user_id`
			where += ` AND (` + strings.Join(clauses, ` OR `) + `)`
		}
		row, e = tx.SQL().QueryRowContext(ctx, countQuery, countArgs...)
		if e != nil {
			return e
		}
		if e = row.Scan(&result.Total); e != nil {
			return e
		}
		if q.Cursor != "" {
			where += ` AND (r.sort_at<? OR (r.sort_at=? AND r.trace_id<?))`
			args = append(args, c.At, c.At, c.Trace)
		}
		args = append(args, q.Limit+1)
		rows, e := tx.SQL().QueryContext(ctx, `SELECT r.id,r.trace_id,r.request_id,r.session_id,r.user_id,r.instance_id,r.requested_model,r.actual_provider_model,r.provider_type,r.status,r.prompt_tokens,r.completion_tokens,r.total_tokens,r.latency_ms,r.ttft_ms,r.generation_ms,r.tokens_per_second,r.attempt_no,LEFT(r.error_message,1000),r.created_at,r.completed_at,r.sort_at,COALESCE(CHAR_LENGTH(r.error_message)>1000,FALSE)`+from+where+` ORDER BY r.sort_at DESC,r.trace_id DESC LIMIT ?`, args...)
		if e != nil {
			return e
		}
		defer rows.Close()
		var lastAt time.Time
		for rows.Next() {
			var v models.ModelInvocation
			var at time.Time
			var truncated bool
			if e = rows.Scan(&v.ID, &v.TraceID, &v.RequestID, &v.SessionID, &v.UserID, &v.InstanceID, &v.RequestedModel, &v.ActualProviderModel, &v.ProviderType, &v.Status, &v.PromptTokens, &v.CompletionTokens, &v.TotalTokens, &v.LatencyMs, &v.TTFTMs, &v.GenerationMs, &v.TokensPerSecond, &v.AttemptNo, &v.ErrorMessage, &v.CreatedAt, &v.CompletedAt, &at, &truncated); e != nil {
				return e
			}
			if len(result.Items) == q.Limit {
				c.At = lastAt
				c.Trace = result.Items[len(result.Items)-1].TraceID
				b, _ := json.Marshal(c)
				result.NextCursor = base64.RawURLEncoding.EncodeToString(b)
				break
			}
			result.Items = append(result.Items, v)
			if truncated {
				result.TruncatedErrors[v.ID] = true
			}
			lastAt = at
		}
		return rows.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Compatibility for in-process callers; HTTP serving uses the explicit cursor
// method above. Walking older pages remains keyset-based, without OFFSET.
func (r *modelInvocationRepository) ListAuditReport(page, limit int, search, status, model string) ([]models.ModelInvocation, int, error) {
	if page < 1 || page > 10000 {
		return nil, 0, fmt.Errorf("invalid audit page")
	}
	q := AuditReportQuery{Limit: limit, Search: search, Status: status, Model: model}
	for i := 1; i <= page; i++ {
		p, e := r.ListAuditReportPage(context.Background(), q)
		if e != nil {
			return nil, 0, e
		}
		if i == page || p.NextCursor == "" {
			if i < page {
				return []models.ModelInvocation{}, p.Total, nil
			}
			return p.Items, p.Total, nil
		}
		q.Cursor = p.NextCursor
	}
	return nil, 0, nil
}
