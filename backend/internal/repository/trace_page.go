package repository

import (
	"clawreef/internal/historymaintenance"
	"clawreef/internal/models"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/upper/db/v4"
	"strings"
	"time"
	"unicode/utf8"
)

const TracePageBytes = 8 << 20

var traceKinds = []string{"model_invocations", "audit_events", "cost_records", "risk_hits", "chat_messages"}

type TraceRecord struct {
	Kind string          `json:"kind"`
	ID   int             `json:"id"`
	Data json.RawMessage `json:"data"`
}
type TracePageResult struct {
	Records    []TraceRecord `json:"records"`
	NextCursor string        `json:"next_cursor,omitempty"`
}
type traceCursor struct {
	Trace string    `json:"trace"`
	Max   []int     `json:"max"`
	At    time.Time `json:"at"`
	Kind  string    `json:"kind"`
	ID    int       `json:"id"`
}

func traceColumns(kind string) []string {
	common := []string{"id", "trace_id", "session_id", "request_id", "user_id", "instance_id"}
	switch kind {
	case "model_invocations":
		return append(common, "provider_type", "requested_model", "actual_provider_model", "status", "prompt_tokens", "completion_tokens", "total_tokens", "latency_ms", "ttft_ms", "generation_ms", "tokens_per_second", "attempt_no", "is_streaming", "created_at", "completed_at")
	case "audit_events":
		return append(common, "event_type", "severity", "created_at", "invocation_id")
	case "cost_records":
		return append(common, "provider_type", "model_name", "currency", "prompt_tokens", "completion_tokens", "total_tokens", "estimated_cost", "internal_cost", "recorded_at", "invocation_id")
	case "risk_hits":
		return append(common, "rule_id", "rule_name", "severity", "action", "created_at", "invocation_id")
	case "chat_messages":
		return append(common, "role", "sequence_no", "created_at", "invocation_id")
	}
	return nil
}
func traceTimeColumn(kind string) string {
	if kind == "cost_records" {
		return "recorded_at"
	}
	return "created_at"
}

// TracePage merges hot/cold metadata before applying one 200-record budget.
// Bodies are deliberately a separate authorized API, never silently truncated.
func (r *modelInvocationRepository) TracePage(ctx context.Context, trace, encoded string, limit int) (*TracePageResult, error) {
	if trace == "" || len(trace) > 100 {
		return nil, fmt.Errorf("invalid trace")
	}
	if limit < 1 || limit > 200 {
		limit = 200
	}
	cursor := traceCursor{Trace: trace, Max: make([]int, len(traceKinds))}
	if encoded != "" {
		raw, e := base64.RawURLEncoding.DecodeString(encoded)
		if e != nil || len(raw) > 4096 {
			return nil, fmt.Errorf("invalid trace cursor")
		}
		if e = json.Unmarshal(raw, &cursor); e != nil || cursor.Trace != trace || len(cursor.Max) != len(traceKinds) {
			return nil, fmt.Errorf("invalid trace cursor scope")
		}
	}
	result := &TracePageResult{Records: []TraceRecord{}}
	err := r.sess.TxContext(ctx, func(tx db.Session) error {
		months := map[string][]string{}
		monthSet := map[string]bool{}
		for _, kind := range traceKinds {
			names, e := historymaintenance.ActiveLogMonths(ctx, monthlySQL{tx.SQL()}, kind, time.Time{}, time.Time{}, trace)
			if e != nil {
				return e
			}
			months[kind] = names
			for _, name := range names {
				_, month, _ := historymaintenance.ParseMonthly(name)
				monthSet[month] = true
			}
		}
		if len(monthSet) > 4 {
			return fmt.Errorf("trace spans over 4 months; use a bounded history window or archive export")
		}
		if encoded == "" {
			for i, kind := range traceKinds {
				q := `SELECT COALESCE(MAX(id),0) FROM (SELECT id FROM ` + kind + ` WHERE trace_id=?`
				args := []interface{}{trace}
				if kind != "cost_records" {
					q += ` UNION ALL SELECT original_id FROM history_archive_records WHERE kind=? AND trace_id=?`
					args = append(args, kind, trace)
				}
				for _, name := range months[kind] {
					q += " UNION ALL SELECT original_id FROM `" + name + "` WHERE trace_id=?"
					args = append(args, trace)
				}
				q += `) ids`
				row, e := tx.SQL().QueryRowContext(ctx, q, args...)
				if e != nil {
					return e
				}
				if e = row.Scan(&cursor.Max[i]); e != nil {
					return e
				}
			}
		}
		parts := []string{}
		args := []interface{}{}
		for i, kind := range traceKinds {
			col := traceTimeColumn(kind)
			pairs := []string{}
			for _, field := range traceColumns(kind) {
				if field == "is_streaming" {
					pairs = append(pairs, "'is_streaming',IF(h.is_streaming,CAST('true' AS JSON),CAST('false' AS JSON))")
				} else {
					pairs = append(pairs, "'"+field+"',h."+field)
				}
			}
			// MySQL JSON timestamps are normalized to RFC3339 for the public models.
			for j, pair := range pairs {
				for _, field := range []string{"created_at", "recorded_at", "completed_at"} {
					if pair == "'"+field+"',h."+field {
						pairs[j] = "'" + field + "',DATE_FORMAT(h." + field + ",'%Y-%m-%dT%H:%i:%s.%fZ')"
					}
				}
			}
			hot := `SELECT '` + kind + `' kind,h.id,h.` + col + ` at,JSON_OBJECT(` + strings.Join(pairs, ",") + `) data FROM ` + kind + ` h WHERE h.trace_id=? AND h.id<=?`
			if historymaintenance.LogKind(kind) {
				hot += ` AND NOT EXISTS(SELECT 1 FROM history_month_catalog c WHERE c.kind='` + kind + `' AND c.month_utc=DATE_FORMAT(h.` + historymaintenance.LogTimeColumn(kind) + `,'%Y%m') AND c.read_route='monthly')`
			}
			parts = append(parts, hot)
			args = append(args, trace, cursor.Max[i])
			if kind != "cost_records" {
				parts = append(parts, `SELECT '`+kind+`',a.original_id,a.created_at,JSON_MERGE_PATCH(a.metadata_json,JSON_OBJECT('id',a.original_id,'trace_id',a.trace_id,'session_id',a.session_id,'user_id',a.owner_id,'instance_id',a.instance_id,'created_at',DATE_FORMAT(a.created_at,'%Y-%m-%dT%H:%i:%s.%fZ'))) FROM history_archive_records a WHERE a.kind=? AND a.trace_id=? AND a.original_id<=? AND NOT EXISTS(SELECT 1 FROM history_month_catalog c WHERE c.table_name=a.month_table AND c.read_route='monthly') AND NOT EXISTS(SELECT 1 FROM `+kind+` h WHERE h.id=a.original_id)`)
				args = append(args, kind, trace, cursor.Max[i])
			}
			for _, name := range months[kind] {
				parts = append(parts, `SELECT '`+kind+`',a.original_id,a.created_at,JSON_MERGE_PATCH(a.metadata_json,JSON_OBJECT('id',a.original_id,'trace_id',a.trace_id,'session_id',a.session_id,'user_id',a.owner_id,'instance_id',a.instance_id,'created_at',DATE_FORMAT(a.created_at,'%Y-%m-%dT%H:%i:%s.%fZ'))) FROM `+"`"+name+"`"+` a WHERE a.trace_id=? AND a.original_id<=?`)
				args = append(args, trace, cursor.Max[i])
			}
		}
		q := `SELECT kind,id,at,data FROM (` + strings.Join(parts, " UNION ALL ") + `) merged`
		if encoded != "" {
			q += ` WHERE (at,kind,id)>(?,?,?)`
			args = append(args, cursor.At, cursor.Kind, cursor.ID)
		}
		q += ` ORDER BY at,kind,id LIMIT ?`
		args = append(args, limit+1)
		rows, e := tx.SQL().QueryContext(ctx, q, args...)
		if e != nil {
			return e
		}
		defer rows.Close()
		size := 1024
		more := false
		for rows.Next() {
			var item TraceRecord
			var at time.Time
			if e = rows.Scan(&item.Kind, &item.ID, &at, &item.Data); e != nil {
				return e
			}
			if len(result.Records) == limit || size+len(item.Data)+256 > TracePageBytes {
				more = true
				break
			}
			result.Records = append(result.Records, item)
			size += len(item.Data) + 256
			cursor.At = at
			cursor.Kind = item.Kind
			cursor.ID = item.ID
		}
		if e = rows.Err(); e != nil {
			return e
		}
		if more {
			if len(result.Records) == 0 {
				return fmt.Errorf("trace metadata exceeds page budget")
			}
			raw, _ := json.Marshal(cursor)
			result.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, err
}

type TraceBodyChunk struct {
	Data       string `json:"data"`
	NextOffset int    `json:"next_offset"`
	TotalBytes int    `json:"total_bytes"`
}

func (r *modelInvocationRepository) TraceBody(ctx context.Context, trace, kind string, id, offset int) (*TraceBodyChunk, error) {
	if traceColumns(kind) == nil || id < 1 || offset < 0 {
		return nil, fmt.Errorf("invalid trace body request")
	}
	var value interface{}
	switch kind {
	case "model_invocations":
		value = &models.ModelInvocation{}
	case "audit_events":
		value = &models.AuditEvent{}
	case "cost_records":
		value = &models.CostRecord{}
	case "risk_hits":
		value = &models.RiskHit{}
	case "chat_messages":
		value = &models.ChatMessageRecord{}
	}
	var e error
	monthly := false
	if reader := historyReader(r.sess); reader != nil {
		record, handled, err := reader.MonthlyGet(ctx, kind, int64(id), 0, true)
		if err != nil {
			return nil, err
		}
		if handled {
			monthly = true
			if record.TraceID != trace {
				return nil, sql.ErrNoRows
			}
			e = json.Unmarshal(record.Payload, value)
		}
	}
	if !monthly {
		e = r.sess.WithContext(ctx).Collection(kind).Find(db.Cond{"id": id, "trace_id": trace}).One(value)
	}
	if e == db.ErrNoMoreRows && kind != "cost_records" {
		reader := historyReader(r.sess)
		if reader == nil {
			return nil, sql.ErrNoRows
		}
		record, err := reader.Get(ctx, kind, int64(id), 0, true)
		if err != nil {
			return nil, err
		}
		var scope struct {
			Trace string `json:"trace_id"`
		}
		if err = json.Unmarshal(record.Payload, &scope); err != nil || scope.Trace != trace {
			return nil, sql.ErrNoRows
		}
		e = json.Unmarshal(record.Payload, value)
	}
	if e != nil {
		return nil, e
	}
	if e = hydrateObservation(ctx, value); e != nil {
		return nil, e
	}
	raw, e := json.Marshal(value)
	if e != nil {
		return nil, e
	}
	if offset > len(raw) {
		return nil, errors.New("body offset out of range")
	}
	end := offset + (1 << 20)
	if end > len(raw) {
		end = len(raw)
	}
	for end < len(raw) && !utf8.RuneStart(raw[end]) {
		end--
	}
	if offset < len(raw) && !utf8.RuneStart(raw[offset]) {
		return nil, errors.New("body offset must be a UTF-8 boundary")
	}
	next := 0
	if end < len(raw) {
		next = end
	}
	return &TraceBodyChunk{Data: string(raw[offset:end]), NextOffset: next, TotalBytes: len(raw)}, nil
}
