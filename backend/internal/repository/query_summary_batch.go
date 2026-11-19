package repository

import (
	"clawreef/internal/models"
	"context"
	"fmt"
	"github.com/upper/db/v4"
	"sort"
	"strings"
	"time"
)

// One source batch is at most 500 rows. Dedup, rollups, trace projection and
// cursor advancement share the caller transaction; no partially rebuilt totals.
func materializeInvocationBatch(ctx context.Context, tx db.Session, items []models.ModelInvocation) error {
	if len(items) == 0 {
		return nil
	}
	if len(items) > 500 {
		return fmt.Errorf("summary batch exceeds 500")
	}
	traceSet := map[string]bool{}
	ids := make([]interface{}, 0, len(items))
	for _, v := range items {
		if v.TraceID != "" {
			traceSet[v.TraceID] = true
		}
		ids = append(ids, v.ID)
	}
	traces := make([]string, 0, len(traceSet))
	for trace := range traceSet {
		traces = append(traces, trace)
	}
	if e := lockAuditTraces(ctx, tx, traces); e != nil {
		return e
	}
	days := make([]time.Time, 0, len(items))
	for _, v := range items {
		days = append(days, v.CreatedAt)
	}
	if e := lockSummaryDays(ctx, tx, false, days...); e != nil {
		return e
	}
	rows, e := tx.SQL().QueryContext(ctx, `SELECT id FROM observation_invocation_summaries WHERE id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`)`, ids...)
	if e != nil {
		return e
	}
	seen := map[int]bool{}
	for rows.Next() {
		var id int
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		seen[id] = true
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	fresh := make([]models.ModelInvocation, 0, len(items))
	args := []interface{}{}
	for _, v := range items {
		if seen[v.ID] {
			continue
		}
		fresh = append(fresh, v)
		args = append(args, v.ID, v.EventUID, v.InstanceID, v.UserID, v.SessionID, v.TraceID, v.RequestID, v.RequestedModel, v.ActualProviderModel, v.ProviderType, v.Status, v.PromptTokens, v.CompletionTokens, v.TotalTokens, v.LatencyMs, v.TTFTMs, v.GenerationMs, v.TokensPerSecond, v.IsStreaming, v.AttemptNo, v.CreatedAt, v.CompletedAt, v.ErrorMessage)
	}
	if len(fresh) > 0 {
		result, err := tx.SQL().ExecContext(ctx, `INSERT IGNORE INTO observation_invocation_summaries(id,event_uid,instance_id,user_id,session_id,trace_id,request_id,requested_model,actual_provider_model,provider_type,status,prompt_tokens,completion_tokens,total_tokens,latency_ms,ttft_ms,generation_ms,tokens_per_second,is_streaming,attempt_no,created_at,completed_at,error_message) VALUES `+strings.TrimSuffix(strings.Repeat("("+strings.TrimSuffix(strings.Repeat("?,", 23), ",")+"),", len(fresh)), ","), args...)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != int64(len(fresh)) {
			return fmt.Errorf("summary batch raced with archival; retry without advancing cursor")
		}
		type key struct {
			scope          string
			instance       int
			dimension, day string
		}
		type value struct {
			prompt, completion, total, count int
			first, last                      time.Time
		}
		groups := map[key]value{}
		for _, v := range fresh {
			if v.Status == models.ModelInvocationStatusBlocked {
				continue
			}
			instance := 0
			if v.InstanceID != nil {
				instance = *v.InstanceID
			}
			scopes := [][2]string{{"day", ""}, {"instance", ""}, {"trace", v.TraceID}}
			if v.SessionID != nil && *v.SessionID != "" {
				scopes = append(scopes, [2]string{"session", *v.SessionID})
			}
			for _, scope := range scopes {
				k := key{scope[0], instance, scope[1], v.CreatedAt.UTC().Format("2006-01-02")}
				g := groups[k]
				g.prompt += v.PromptTokens
				g.completion += v.CompletionTokens
				g.total += v.TotalTokens
				g.count++
				if g.first.IsZero() || v.CreatedAt.Before(g.first) {
					g.first = v.CreatedAt
				}
				if v.CreatedAt.After(g.last) {
					g.last = v.CreatedAt
				}
				groups[k] = g
			}
		}
		keys := make([]key, 0, len(groups))
		for k := range groups {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			a, b := keys[i], keys[j]
			if a.scope != b.scope {
				return a.scope < b.scope
			}
			if a.instance != b.instance {
				return a.instance < b.instance
			}
			if a.dimension != b.dimension {
				return a.dimension < b.dimension
			}
			return a.day < b.day
		})
		if len(keys) > 0 {
			args = nil
			for _, k := range keys {
				g := groups[k]
				args = append(args, k.scope, k.instance, k.dimension, k.day, g.prompt, g.completion, g.total, g.count, g.first, g.last)
			}
			_, e = tx.SQL().ExecContext(ctx, `INSERT INTO observation_daily_summaries(scope,instance_id,dimension_id,day_utc,prompt_tokens,completion_tokens,total_tokens,invocation_count,first_seen_at,last_seen_at) VALUES `+strings.TrimSuffix(strings.Repeat("(?,?,?,?,?,?,?,?,?,?),", len(keys)), ",")+` ON DUPLICATE KEY UPDATE prompt_tokens=prompt_tokens+VALUES(prompt_tokens),completion_tokens=completion_tokens+VALUES(completion_tokens),total_tokens=total_tokens+VALUES(total_tokens),invocation_count=invocation_count+VALUES(invocation_count),first_seen_at=LEAST(first_seen_at,VALUES(first_seen_at)),last_seen_at=GREATEST(last_seen_at,VALUES(last_seen_at))`, args...)
			if e != nil {
				return e
			}
		}
	}
	return refreshAuditTraces(ctx, tx, traces)
}
