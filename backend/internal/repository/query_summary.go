package repository

import (
	"clawreef/internal/historymaintenance"
	"clawreef/internal/models"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/upper/db/v4"
	"sort"
	"strings"
	"time"
)

// maintainObservationSummary shares the source insert transaction. Immutable
// invocation identity is the dedup boundary, including replay after archival.
func maintainInvocationSummary(ctx context.Context, tx db.Session, table string, value interface{}) error {
	if table != "model_invocations" {
		return nil
	}
	v, ok := value.(*models.ModelInvocation)
	if !ok {
		return fmt.Errorf("invalid invocation summary")
	}
	// Source TIMESTAMP precision is authoritative. Using Go nanoseconds here
	// would later reject the same archived event after SQL rounded its timestamp.
	var sourceTime struct {
		CreatedAt       time.Time  `db:"created_at"`
		CompletedAt     *time.Time `db:"completed_at"`
		TokensPerSecond *float64   `db:"tokens_per_second"`
	}
	if e := tx.Collection("model_invocations").Find(db.Cond{"id": v.ID}).Select("created_at", "completed_at", "tokens_per_second").One(&sourceTime); e == nil {
		v.CreatedAt = sourceTime.CreatedAt
		v.CompletedAt = sourceTime.CompletedAt
		v.TokensPerSecond = sourceTime.TokensPerSecond
	} else if e != db.ErrNoMoreRows {
		return e
	}
	if err := lockSummaryDays(ctx, tx, false, v.CreatedAt); err != nil {
		return err
	}
	result, err := tx.SQL().ExecContext(ctx, `INSERT IGNORE INTO observation_invocation_summaries
 (id,event_uid,instance_id,user_id,session_id,trace_id,request_id,requested_model,actual_provider_model,provider_type,status,prompt_tokens,completion_tokens,total_tokens,latency_ms,ttft_ms,generation_ms,tokens_per_second,is_streaming,attempt_no,created_at,completed_at,error_message)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, v.ID, v.EventUID, v.InstanceID, v.UserID, v.SessionID, v.TraceID, v.RequestID, v.RequestedModel, v.ActualProviderModel, v.ProviderType, v.Status, v.PromptTokens, v.CompletionTokens, v.TotalTokens, v.LatencyMs, v.TTFTMs, v.GenerationMs, v.TokensPerSecond, v.IsStreaming, v.AttemptNo, v.CreatedAt, v.CompletedAt, v.ErrorMessage)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		row, e := tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM observation_invocation_summaries WHERE id=? AND trace_id=? AND status=? AND prompt_tokens=? AND completion_tokens=? AND total_tokens=? AND COALESCE(instance_id,0)=COALESCE(?,0) AND COALESCE(session_id,'')=COALESCE(?,'') AND created_at=? AND error_message<=>? AND tokens_per_second<=>? AND is_streaming=? AND request_id=? AND requested_model=? AND actual_provider_model=? AND provider_type=? AND attempt_no=? AND latency_ms<=>? AND ttft_ms<=>? AND generation_ms<=>? AND completed_at<=>?`, v.ID, v.TraceID, v.Status, v.PromptTokens, v.CompletionTokens, v.TotalTokens, v.InstanceID, v.SessionID, v.CreatedAt, v.ErrorMessage, v.TokensPerSecond, v.IsStreaming, v.RequestID, v.RequestedModel, v.ActualProviderModel, v.ProviderType, v.AttemptNo, v.LatencyMs, v.TTFTMs, v.GenerationMs, v.CompletedAt)
		if e != nil {
			return e
		}
		var matching int
		if e = row.Scan(&matching); e != nil {
			return e
		}
		if matching != 1 {
			return fmt.Errorf("invocation %d summary identity/content mismatch", v.ID)
		}
		return nil
	}
	if v.Status == models.ModelInvocationStatusBlocked {
		return nil
	}
	instance := 0
	if v.InstanceID != nil {
		instance = *v.InstanceID
	}
	session := ""
	if v.SessionID != nil {
		session = *v.SessionID
	}
	scopes := [][2]string{{"instance", ""}, {"day", ""}, {"trace", v.TraceID}}
	if session != "" {
		scopes = append(scopes, [2]string{"session", session})
	}
	sort.Slice(scopes, func(i, j int) bool { return scopes[i][0] < scopes[j][0] })
	for _, scope := range scopes {
		_, err = tx.SQL().ExecContext(ctx, `INSERT INTO observation_daily_summaries(scope,instance_id,dimension_id,day_utc,prompt_tokens,completion_tokens,total_tokens,invocation_count,first_seen_at,last_seen_at)
 VALUES(?,?,?,?,?,?,?,1,?,?) ON DUPLICATE KEY UPDATE prompt_tokens=prompt_tokens+VALUES(prompt_tokens),completion_tokens=completion_tokens+VALUES(completion_tokens),total_tokens=total_tokens+VALUES(total_tokens),invocation_count=invocation_count+1,first_seen_at=LEAST(first_seen_at,VALUES(first_seen_at)),last_seen_at=GREATEST(last_seen_at,VALUES(last_seen_at))`, scope[0], instance, scope[1], v.CreatedAt.UTC().Format("2006-01-02"), v.PromptTokens, v.CompletionTokens, v.TotalTokens, v.CreatedAt, v.CreatedAt)
		if err != nil {
			return err
		}
	}
	return nil
}

// ReconcileObservationSummaries advances a durable watermark in the same
// transaction as each batch. Archive proofs never precede summary commitment.
func ReconcileObservationSummaries(ctx context.Context, sess db.Session, limit int) (int, error) {
	if limit < 1 || limit > 500 {
		limit = 500
	}
	done := 0
	err := sess.WithContext(ctx).Tx(func(tx db.Session) error {
		_, err := tx.SQL().ExecContext(ctx, `INSERT IGNORE INTO history_maintenance_cursors(kind,after_id,updated_at) VALUES('query_summary',0,UTC_TIMESTAMP(6))`)
		if err != nil {
			return err
		}
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT after_id FROM history_maintenance_cursors WHERE kind='query_summary' FOR UPDATE`)
		if err != nil {
			return err
		}
		var after int
		if err = row.Scan(&after); err != nil {
			return err
		}
		var items []models.ModelInvocation
		ids, err := observationCatchupIDs(ctx, tx, "model_invocations", after, limit)
		if err != nil {
			return err
		}
		ids, err = historymaintenance.WriteLogPrefix(ctx, monthlySQL{tx.SQL()}, "model_invocations", ids)
		if err != nil {
			return err
		}
		if len(ids) > 0 {
			err = tx.Collection("model_invocations").Find(db.Cond{"id IN": ids}).Select("id", "instance_id", "user_id", "session_id", "trace_id", "request_id", "requested_model", "actual_provider_model", "provider_type", "status", "prompt_tokens", "completion_tokens", "total_tokens", "latency_ms", "ttft_ms", "generation_ms", "tokens_per_second", "is_streaming", "attempt_no", "created_at", "completed_at", "error_message").OrderBy("id").Limit(limit).All(&items)
		}
		if err != nil {
			return err
		}
		if err = materializeInvocationBatch(ctx, tx, items); err != nil {
			return err
		}
		if len(items) > 0 {
			after = max(after, items[len(items)-1].ID)
			done += len(items)
		}
		if err = clearObservationPending(ctx, tx, "model_invocations", ids...); err != nil {
			return err
		}
		_, err = tx.SQL().ExecContext(ctx, `UPDATE history_maintenance_cursors SET after_id=?,updated_at=UTC_TIMESTAMP(6) WHERE kind='query_summary'`, after)
		if err != nil {
			return err
		}
		_, err = tx.SQL().ExecContext(ctx, `UPDATE observation_summary_state SET ready=TRUE WHERE id=1 AND target_id<=?`, after)
		return err
	})
	if err != nil {
		return done, err
	}
	// A different cursor is unnecessary: the proof is the durable archive cursor.
	err = sess.WithContext(ctx).Tx(func(tx db.Session) error {
		rows, err := tx.SQL().QueryContext(ctx, `SELECT kind,original_id,owner_id,instance_id,session_id,trace_id,created_at,payload_sha256,metadata_json,payload_bytes FROM history_archive_records WHERE summary_verified_at IS NULL ORDER BY kind,original_id LIMIT ? FOR UPDATE`, limit)
		if err != nil {
			return err
		}
		type archived struct {
			kind, session, trace, digest, metadata string
			id, owner, instance                    int
			created                                time.Time
		}
		var records []archived
		var bytes int64
		for rows.Next() {
			var a archived
			var size int64
			if err = rows.Scan(&a.kind, &a.id, &a.owner, &a.instance, &a.session, &a.trace, &a.created, &a.digest, &a.metadata, &size); err != nil {
				rows.Close()
				return err
			}
			if size <= 0 || size > historymaintenance.MaxPageBytes {
				rows.Close()
				return errors.New("archive row exceeds summary batch budget")
			}
			if bytes+size > historymaintenance.MaxPageBytes {
				break
			}
			bytes += size
			records = append(records, a)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, a := range records {
			var owner, instance *int
			var session *string
			if a.owner != 0 {
				owner = &a.owner
			}
			if a.instance != 0 {
				instance = &a.instance
			}
			if a.session != "" {
				session = &a.session
			}
			if a.kind == "model_invocations" {
				var v models.ModelInvocation
				if err = json.Unmarshal([]byte(a.metadata), &v); err != nil {
					return err
				}
				v.ID = a.id
				v.UserID = owner
				v.InstanceID = instance
				v.SessionID = session
				v.TraceID = a.trace
				v.CreatedAt = a.created
				if err = maintainObservationSummary(ctx, tx, a.kind, &v); err != nil {
					return err
				}
			} else if a.kind == "audit_events" {
				var v models.AuditEvent
				if err = json.Unmarshal([]byte(a.metadata), &v); err != nil {
					return err
				}
				v.ID, v.UserID, v.InstanceID, v.SessionID, v.TraceID, v.CreatedAt = a.id, owner, instance, session, a.trace, a.created
				if err = maintainObservationSummary(ctx, tx, a.kind, &v); err != nil {
					return err
				}
			}
			_, err = tx.SQL().ExecContext(ctx, `INSERT INTO observation_archive_receipts(kind,original_id,payload_sha256,metadata_json) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE payload_sha256=VALUES(payload_sha256),metadata_json=VALUES(metadata_json)`, a.kind, a.id, a.digest, a.metadata)
			if err != nil {
				return err
			}
			_, err = tx.SQL().ExecContext(ctx, `UPDATE history_archive_records SET summary_verified_at=UTC_TIMESTAMP(6),summary_sha256=? WHERE kind=? AND original_id=? AND payload_sha256=?`, a.digest, a.kind, a.id, a.digest)
			if err != nil {
				return err
			}
			done++
		}
		return nil
	})
	return done, err
}

// RebuildObservationDay reconciles one UTC day from durable deduplicated facts.
// The per-day transaction and ordinary backup-fenced session preserve readers.
func RebuildObservationDay(ctx context.Context, sess db.Session, day time.Time) error {
	start := day.UTC().Truncate(24 * time.Hour)
	return sess.WithContext(ctx).Tx(func(tx db.Session) error { return rebuildObservationDay(ctx, tx, start) })
}

func rebuildObservationDay(ctx context.Context, tx db.Session, start time.Time) error {
	if err := lockSummaryDays(ctx, tx, true, start); err != nil {
		return err
	}
	end := start.Add(24 * time.Hour)
	if _, err := tx.SQL().ExecContext(ctx, `DELETE FROM observation_daily_summaries WHERE day_utc=?`, start); err != nil {
		return err
	}
	for _, scope := range []string{"session", "instance", "day", "trace"} {
		dimension := "''"
		extra := ""
		if scope == "session" {
			dimension = "session_id"
			extra = " AND session_id IS NOT NULL AND session_id<>''"
		}
		if scope == "trace" {
			dimension = "trace_id"
		}
		_, err := tx.SQL().ExecContext(ctx, `INSERT INTO observation_daily_summaries SELECT ?,COALESCE(instance_id,0),`+dimension+`,DATE(created_at),SUM(prompt_tokens),SUM(completion_tokens),SUM(total_tokens),COUNT(*),MIN(created_at),MAX(created_at) FROM observation_invocation_summaries WHERE created_at>=? AND created_at<? AND status<>'blocked'`+extra+` GROUP BY COALESCE(instance_id,0),`+dimension+`,DATE(created_at)`, scope, start, end)
		if err != nil {
			return err
		}
	}
	return rebuildCostDay(ctx, tx, start, end)
}

func sessionDailyTokenSQL(filter SessionUsageFilter, exactSession ...string) (string, []interface{}) {
	q := `SELECT d.instance_id,d.dimension_id session_id,SUM(d.prompt_tokens) prompt_tokens,SUM(d.completion_tokens) completion_tokens,SUM(d.total_tokens) total_tokens,SUM(d.invocation_count) invocation_count,MIN(d.first_seen_at) first_seen_at,MAX(d.last_seen_at) last_seen_at FROM observation_daily_summaries d JOIN scope ON scope.id=d.instance_id WHERE d.scope='session'`
	args := []interface{}{}
	if len(exactSession) > 0 {
		q += " AND d.dimension_id=?"
		args = append(args, exactSession[0])
	}
	var lower, upper *time.Time
	if filter.Since != nil {
		v := filter.Since.UTC().Truncate(24 * time.Hour)
		if v.Before(*filter.Since) {
			v = v.Add(24 * time.Hour)
		}
		if !v.Equal(*filter.Since) {
			lower = &v
		}
		q += " AND d.day_utc>=?"
		args = append(args, v)
	}
	if filter.Until != nil {
		v := filter.Until.UTC().Truncate(24 * time.Hour)
		if !v.Equal(*filter.Until) {
			upper = &v
		}
		q += " AND d.day_utc<?"
		args = append(args, v)
	}
	q += " GROUP BY d.instance_id,d.dimension_id"
	if lower == nil && upper == nil {
		return q, args
	}
	boundary := `SELECT mi.instance_id,mi.session_id,SUM(mi.prompt_tokens) prompt_tokens,SUM(mi.completion_tokens) completion_tokens,SUM(mi.total_tokens) total_tokens,COUNT(*) invocation_count,MIN(mi.created_at) first_seen_at,MAX(mi.created_at) last_seen_at FROM observation_invocation_summaries mi JOIN scope ON scope.id=mi.instance_id WHERE mi.status<>'blocked' AND mi.session_id IS NOT NULL AND mi.session_id<>''`
	if len(exactSession) > 0 {
		boundary += " AND mi.session_id=?"
		args = append(args, exactSession[0])
	}
	boundary, args = appendTimeFilter(boundary, args, filter, "mi.created_at")
	boundary += " AND ("
	if lower != nil {
		boundary += "mi.created_at<?"
		args = append(args, *lower)
	}
	if upper != nil {
		if lower != nil {
			boundary += " OR "
		}
		boundary += "mi.created_at>=?"
		args = append(args, *upper)
	}
	boundary += ") GROUP BY mi.instance_id,mi.session_id"
	return `SELECT instance_id,session_id,SUM(prompt_tokens) prompt_tokens,SUM(completion_tokens) completion_tokens,SUM(total_tokens) total_tokens,SUM(invocation_count) invocation_count,MIN(first_seen_at) first_seen_at,MAX(last_seen_at) last_seen_at FROM (` + q + ` UNION ALL ` + boundary + `) day_parts GROUP BY instance_id,session_id`, args
}

type sessionPageCursor struct {
	Population   string    `json:"population"`
	Instance     int       `json:"instance"`
	Search       string    `json:"search"`
	At           time.Time `json:"at"`
	Session      string    `json:"session"`
	Tokens       int       `json:"tokens"`
	ID           int       `json:"id"`
	Since, Until *time.Time
}

var ErrSessionCursorStale = errors.New("Usage changed during pagination. Refresh to load the latest results and restart pagination or export.")

// Facts are append-only and their count is monotonic, including zero-token and
// lower-ID transactions committed after page one. Compare the aggregate receipt
// in the same repeatable-read view as each page; a time bound alone is not a
// snapshot. Cold-source movement leaves this receipt unchanged. A changed
// population fails explicitly rather than silently skipping reordered entries.
func checkSessionPopulation(c *sessionPageCursor, raw string, s SessionUsageReportSummary, scope string) error {
	population := fmt.Sprintf("%s/%d/%d/%d/%d/%d/%.8f/%s", scope, s.InvocationCount, s.SessionCount, s.PromptTokens, s.CompletionTokens, s.TotalTokens, s.EstimatedCost, s.Currency)
	if raw != "" && c.Population != population {
		return ErrSessionCursorStale
	}
	c.Population = population
	return nil
}

// A fixed-size, order-independent SHA-256 membership receipt also detects title
// and instance metadata edits changing search membership. Unlike GROUP_CONCAT,
// this is not truncated by MySQL's session string-aggregation limit.
func sessionMembershipSQL(value string) string {
	parts := []string{}
	for _, offset := range []int{1, 17, 33, 49} {
		parts = append(parts, fmt.Sprintf("LPAD(HEX(BIT_XOR(CAST(CONV(SUBSTRING(SHA2(%s,256),%d,16),16,10) AS UNSIGNED))),16,'0')", value, offset))
	}
	return "CONCAT(" + strings.Join(parts, ",") + ")"
}

func decodeSessionCursor(raw string, instance int, search string, filter SessionUsageFilter) (sessionPageCursor, error) {
	c := sessionPageCursor{Instance: instance, Search: search, Since: filter.Since, Until: filter.Until}
	if raw == "" {
		return c, nil
	}
	b, e := base64.RawURLEncoding.DecodeString(raw)
	if e != nil || len(b) > 4096 {
		return c, fmt.Errorf("invalid cursor")
	}
	e = json.Unmarshal(b, &c)
	if e != nil || c.Instance != instance || c.Search != search {
		return c, fmt.Errorf("invalid cursor scope")
	}
	if (filter.Since != nil && c.Since == nil) || (filter.Until != nil && c.Until == nil) {
		return c, fmt.Errorf("cursor requires query window")
	}
	if filter.Since != nil && !filter.Since.Equal(*c.Since) || filter.Until != nil && !filter.Until.Equal(*c.Until) {
		return c, fmt.Errorf("cursor query window changed")
	}
	if c.Since != nil && c.Until != nil && (!c.Until.After(*c.Since) || c.Until.Sub(*c.Since) > 90*24*time.Hour) {
		return c, fmt.Errorf("cursor query window must be at most 90 days")
	}
	return c, nil
}
func encodeSessionCursor(c sessionPageCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

// ReconcileObservationDay continuously audits the retained 365-day summary
// horizon one UTC day per call. The cursor and rebuilt rollups commit together.
func ReconcileObservationDay(ctx context.Context, sess db.Session) error {
	today := time.Now().UTC().Truncate(24*time.Hour).Unix() / 86400
	return sess.WithContext(ctx).Tx(func(tx db.Session) error {
		if _, e := tx.SQL().ExecContext(ctx, `INSERT IGNORE INTO history_maintenance_cursors(kind,after_id,updated_at)VALUES('query_rebuild_day',?,UTC_TIMESTAMP(6))`, today); e != nil {
			return e
		}
		row, e := tx.SQL().QueryRowContext(ctx, `SELECT after_id FROM history_maintenance_cursors WHERE kind='query_rebuild_day' FOR UPDATE`)
		if e != nil {
			return e
		}
		var day int64
		if e = row.Scan(&day); e != nil {
			return e
		}
		if day < today-365 || day > today {
			day = today
		}
		if e = rebuildObservationDay(ctx, tx, time.Unix(day*86400, 0).UTC()); e != nil {
			return e
		}
		_, e = tx.SQL().ExecContext(ctx, `UPDATE history_maintenance_cursors SET after_id=?,updated_at=UTC_TIMESTAMP(6) WHERE kind='query_rebuild_day'`, day-1)
		return e
	})
}

type SummaryRebuildingError struct{ Processed, Target, PendingCosts int64 }

func (e *SummaryRebuildingError) Error() string {
	if e.PendingCosts > 0 {
		return fmt.Sprintf("observation summaries await %d unresolved invocation dependencies (%d/%d records processed)", e.PendingCosts, e.Processed, e.Target)
	}
	return fmt.Sprintf("observation summaries rebuilding (%d/%d); retry shortly", e.Processed, e.Target)
}
