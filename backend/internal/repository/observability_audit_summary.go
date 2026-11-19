package repository

import (
	"clawreef/internal/historymaintenance"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// All source writers take the same per-trace lock before updating facts. This
// serializes concurrent invocation/event/fee projections without a global lock.
func maintainObservationSummary(ctx context.Context, tx db.Session, table string, value interface{}) error {
	trace := ""
	var sourceID int
	switch v := value.(type) {
	case *models.ModelInvocation:
		trace = v.TraceID
		sourceID = v.ID
	case *models.AuditEvent:
		trace = v.TraceID
		sourceID = v.ID
	case *models.CostRecord:
		trace = v.TraceID
		sourceID = v.ID
	case *models.ChatMessageRecord:
		if err := historymaintenance.WriteLogBatch(ctx, monthlySQL{tx.SQL()}, table, []any{v.ID}); err != nil {
			return err
		}
		return historymaintenance.ClearLogPending(ctx, monthlySQL{tx.SQL()}, table, v.ID)
	default:
		return nil
	}
	if sourceID > 0 {
		if err := historymaintenance.WriteLogBatch(ctx, monthlySQL{tx.SQL()}, table, []any{sourceID}); err != nil {
			return err
		}
	}
	if trace != "" {
		if err := lockAuditTraces(ctx, tx, []string{trace}); err != nil {
			return err
		}
	}
	if err := maintainInvocationSummary(ctx, tx, table, value); err != nil {
		return err
	}
	switch v := value.(type) {
	case *models.CostRecord:
		if err := maintainCostSummary(ctx, tx, v); err != nil {
			return err
		}
		if err := clearObservationPending(ctx, tx, "cost_records", v.ID); err != nil {
			return err
		}
	case *models.ModelInvocation:
		if err := reconcileInvocationCosts(ctx, tx, v.ID); err != nil {
			return err
		}
		if err := clearObservationPending(ctx, tx, "model_invocations", v.ID); err != nil {
			return err
		}
	}
	if trace == "" {
		if v, ok := value.(*models.AuditEvent); ok {
			_, err := tx.SQL().ExecContext(ctx, `DELETE FROM observation_audit_pending WHERE id=?`, v.ID)
			return err
		}
		return nil
	}
	if v, ok := value.(*models.AuditEvent); ok {
		var sourceTime struct {
			CreatedAt time.Time `db:"created_at"`
		}
		if err := tx.Collection("audit_events").Find(db.Cond{"id": v.ID}).Select("created_at").One(&sourceTime); err == nil {
			v.CreatedAt = sourceTime.CreatedAt
		} else if err != db.ErrNoMoreRows {
			return err
		}
		if _, err := tx.SQL().ExecContext(ctx, `INSERT IGNORE INTO observation_audit_event_summaries(id,trace_id,request_id,session_id,user_id,instance_id,event_type,message,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, v.ID, v.TraceID, v.RequestID, v.SessionID, v.UserID, v.InstanceID, v.EventType, v.Message, v.CreatedAt); err != nil {
			return err
		}
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM observation_audit_event_summaries WHERE id=? AND trace_id=? AND request_id<=>? AND session_id<=>? AND user_id<=>? AND instance_id<=>? AND event_type=? AND BINARY message=BINARY ? AND created_at=?`, v.ID, v.TraceID, v.RequestID, v.SessionID, v.UserID, v.InstanceID, v.EventType, v.Message, v.CreatedAt)
		if err != nil {
			return err
		}
		var matching int
		if err = row.Scan(&matching); err != nil {
			return err
		}
		if matching != 1 {
			return fmt.Errorf("audit %d summary identity/content mismatch", v.ID)
		}
	}
	if err := refreshAuditTraces(ctx, tx, []string{trace}); err != nil {
		return err
	}
	if v, ok := value.(*models.AuditEvent); ok {
		_, err := tx.SQL().ExecContext(ctx, `DELETE FROM observation_audit_pending WHERE id=?`, v.ID)
		return err
	}
	return nil
}

func lockAuditTraces(ctx context.Context, tx db.Session, traces []string) error {
	if len(traces) == 0 {
		return nil
	}
	sort.Strings(traces)
	args := make([]interface{}, len(traces))
	for i, trace := range traces {
		args[i] = trace
	}
	_, err := tx.SQL().ExecContext(ctx, `INSERT INTO observation_audit_trace_reports(trace_id,message) VALUES`+strings.TrimSuffix(strings.Repeat("(?,''),", len(traces)), ",")+` ON DUPLICATE KEY UPDATE trace_id=VALUES(trace_id)`, args...)
	return err
}

// The UNION covers legacy rows not yet visited by the invocation backfill.
// Both branches are restricted to the affected traces and deduplicated by ID;
// ordinary list/count queries never execute this projection.
func refreshAuditTraces(ctx context.Context, tx db.Session, traces []string) error {
	if len(traces) == 0 {
		return nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(traces)), ",")
	args := make([]interface{}, 0, len(traces)*3)
	for range 3 {
		for _, v := range traces {
			args = append(args, v)
		}
	}
	columns := `id,trace_id,request_id,session_id,user_id,instance_id,requested_model,actual_provider_model,provider_type,status,prompt_tokens,completion_tokens,total_tokens,latency_ms,ttft_ms,generation_ms,tokens_per_second,attempt_no,error_message,created_at,completed_at`
	_, err := tx.SQL().ExecContext(ctx, `INSERT INTO observation_audit_trace_reports (`+columns+`,sort_at,event_type,message)
 WITH invocation_rows AS (
 SELECT `+columns+` FROM observation_invocation_summaries WHERE trace_id IN (`+marks+`)
 UNION ALL SELECT `+columns+` FROM model_invocations m WHERE trace_id IN (`+marks+`) AND NOT EXISTS(SELECT 1 FROM observation_invocation_summaries s WHERE s.id=m.id)
 ), invocation_groups AS (
 SELECT trace_id,MIN(id) first_id,MAX(id) last_id,MIN(created_at) created_at,MAX(completed_at) completed_at,SUM(prompt_tokens) prompt_tokens,SUM(completion_tokens) completion_tokens,SUM(total_tokens) total_tokens,SUM(latency_ms) latency_ms FROM invocation_rows GROUP BY trace_id
 ), event_groups AS (
 SELECT trace_id,MAX(id) last_id FROM observation_audit_event_summaries WHERE trace_id IN (`+marks+`) GROUP BY trace_id
 ), fallback_costs AS (
 SELECT c.trace_id,MAX(c.id) last_id,SUM(c.prompt_tokens) prompt_tokens,SUM(c.completion_tokens) completion_tokens,SUM(c.total_tokens) total_tokens FROM event_groups g JOIN cost_records c ON c.trace_id=g.trace_id GROUP BY c.trace_id
 )
 SELECT m.id,m.trace_id,f.request_id,m.session_id,m.user_id,m.instance_id,m.requested_model,m.actual_provider_model,m.provider_type,
 CASE WHEN m.status IN ('completed','blocked','failed') THEN m.status ELSE 'pending' END,
 g.prompt_tokens,g.completion_tokens,g.total_tokens,g.latency_ms,m.ttft_ms,m.generation_ms,m.tokens_per_second,m.attempt_no,m.error_message,g.created_at,g.completed_at,g.created_at,'',''
 FROM invocation_groups g JOIN invocation_rows m ON m.id=g.last_id JOIN invocation_rows f ON f.id=g.first_id
 UNION ALL
 SELECT e.id,e.trace_id,COALESCE(e.request_id,''),e.session_id,COALESCE(e.user_id,c.user_id),COALESCE(e.instance_id,c.instance_id),'Auto',COALESCE(NULLIF(c.model_name,''),e.event_type),'gateway',
 CASE WHEN LOCATE('completed',e.event_type)>0 THEN 'completed' WHEN LOCATE('blocked',e.event_type)>0 THEN 'blocked' WHEN LOCATE('failed',e.event_type)>0 THEN 'failed' ELSE 'pending' END,
 COALESCE(fc.prompt_tokens,0),COALESCE(fc.completion_tokens,0),COALESCE(fc.total_tokens,0),NULL,NULL,NULL,NULL,0,NULL,e.created_at,NULL,e.created_at,e.event_type,e.message
 FROM event_groups g JOIN observation_audit_event_summaries e ON e.id=g.last_id
 LEFT JOIN fallback_costs fc ON fc.trace_id=e.trace_id LEFT JOIN cost_records c ON c.id=fc.last_id
 WHERE NOT EXISTS(SELECT 1 FROM invocation_groups i WHERE i.trace_id=e.trace_id)
 ON DUPLICATE KEY UPDATE sequence_id=IF(id=0,VALUES(sequence_id),sequence_id),id=VALUES(id),request_id=VALUES(request_id),session_id=VALUES(session_id),user_id=VALUES(user_id),instance_id=VALUES(instance_id),requested_model=VALUES(requested_model),actual_provider_model=VALUES(actual_provider_model),provider_type=VALUES(provider_type),status=VALUES(status),prompt_tokens=VALUES(prompt_tokens),completion_tokens=VALUES(completion_tokens),total_tokens=VALUES(total_tokens),latency_ms=VALUES(latency_ms),ttft_ms=VALUES(ttft_ms),generation_ms=VALUES(generation_ms),tokens_per_second=VALUES(tokens_per_second),attempt_no=VALUES(attempt_no),error_message=VALUES(error_message),created_at=VALUES(created_at),completed_at=VALUES(completed_at),sort_at=COALESCE(sort_at,VALUES(sort_at)),event_type=VALUES(event_type),message=VALUES(message)`, args...)
	return err
}

// ReconcileAuditReports resumes after interruption. Each transaction owns at most
// 500 source IDs; the caller may loop for a maximum of 30 seconds. Completion is
// a serving cutover gate, never an excuse to return a partial count.
func ReconcileAuditReports(ctx context.Context, sess db.Session, limit int) (int, bool, error) {
	if limit < 1 || limit > 500 {
		limit = 500
	}
	done, ready := 0, false
	err := sess.WithContext(ctx).Tx(func(tx db.Session) error {
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT source,after_id FROM observation_audit_backfill WHERE complete=FALSE ORDER BY source LIMIT 1 FOR UPDATE`)
		if err != nil {
			return err
		}
		var source string
		var after int
		if err = row.Scan(&source, &after); err != nil {
			if strings.Contains(err.Error(), "no rows") {
				done, err = reconcilePendingAuditEvents(ctx, tx, limit)
				ready = done == 0 && err == nil
				return err
			}
			return err
		}
		table, id, where := source, "id", ""
		switch source {
		case "archive_invocations":
			table, id, where = "history_archive_records", "original_id", " AND kind='model_invocations'"
		case "archive_audits":
			table, id, where = "history_archive_records", "original_id", " AND kind='audit_events'"
		case "model_invocations", "observation_invocation_summaries", "audit_events":
		default:
			return fmt.Errorf("invalid audit backfill source %q", source)
		}
		startAfter := after
		bytesColumn := "0"
		if table == "history_archive_records" {
			bytesColumn = "payload_bytes"
		}
		query := `SELECT ` + id + `,trace_id,` + bytesColumn + ` FROM ` + table + ` WHERE ` + id + `>?` + where + ` ORDER BY ` + id + ` LIMIT ?`
		rows, err := tx.SQL().QueryContext(ctx, query, after, limit)
		if err != nil {
			return err
		}
		traces := map[string]bool{}
		idTraces := map[int]string{}
		ids := []int{}
		var selectedBytes int64
		byteLimited := false
		for rows.Next() {
			var n int
			var trace string
			var bytes int64
			if err = rows.Scan(&n, &trace, &bytes); err != nil {
				rows.Close()
				return err
			}
			if table == "history_archive_records" {
				if bytes <= 0 || bytes > historymaintenance.MaxPageBytes {
					rows.Close()
					return fmt.Errorf("archive row exceeds bounded audit backfill")
				}
				if selectedBytes+bytes > historymaintenance.MaxPageBytes {
					byteLimited = true
					break
				}
				selectedBytes += bytes
			}
			ids = append(ids, n)
			idTraces[n] = trace
			if trace != "" {
				traces[trace] = true
			}
			after = n
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		selectedCount := len(ids)
		if source == "audit_events" && len(ids) > 0 {
			args := make([]any, len(ids))
			for i, id := range ids {
				args[i] = id
			}
			processed, e := historymaintenance.WriteLogPrefix(ctx, monthlySQL{tx.SQL()}, source, args)
			if e != nil {
				return e
			}
			ids = ids[:len(processed)]
			after = startAfter
			traces = map[string]bool{}
			for _, id := range ids {
				after = id
				if trace := idTraces[id]; trace != "" {
					traces[trace] = true
				}
			}
		}
		list := make([]string, 0, len(traces))
		for trace := range traces {
			list = append(list, trace)
		}
		if err = lockAuditTraces(ctx, tx, list); err != nil {
			return err
		}
		if len(ids) > 0 {
			first := ids[0]
			if source == "audit_events" {
				_, err = tx.SQL().ExecContext(ctx, `INSERT IGNORE INTO observation_audit_event_summaries(id,trace_id,request_id,session_id,user_id,instance_id,event_type,message,created_at) SELECT id,trace_id,request_id,session_id,user_id,instance_id,event_type,message,created_at FROM audit_events WHERE id>=? AND id<=?`, first, after)
			} else if table == "history_archive_records" {
				err = backfillArchivedAuditFacts(ctx, tx, where, first, after)
			}
			if err != nil {
				return err
			}
			if err = refreshAuditTraces(ctx, tx, list); err != nil {
				return err
			}
			if source == "audit_events" {
				args := make([]interface{}, len(ids))
				for i, id := range ids {
					args[i] = id
				}
				if _, err = tx.SQL().ExecContext(ctx, `DELETE FROM observation_audit_pending WHERE id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`)`, args...); err != nil {
					return err
				}
			}
		}
		done = len(ids)
		_, err = tx.SQL().ExecContext(ctx, `UPDATE observation_audit_backfill SET after_id=?,complete=?,updated_at=UTC_TIMESTAMP(6) WHERE source=?`, after, !byteLimited && selectedCount < limit && len(ids) == selectedCount, source)
		return err
	})
	return done, ready, err
}

func backfillArchivedAuditFacts(ctx context.Context, tx db.Session, kindWhere string, first, last int) error {
	rows, err := tx.SQL().QueryContext(ctx, `SELECT kind,original_id,owner_id,instance_id,session_id,trace_id,created_at,metadata_json FROM history_archive_records WHERE original_id>=? AND original_id<=?`+kindWhere, first, last)
	if err != nil {
		return err
	}
	type record struct {
		kind, session, trace, metadata string
		id, owner, instance            int
		at                             time.Time
	}
	records := []record{}
	for rows.Next() {
		var v record
		if err = rows.Scan(&v.kind, &v.id, &v.owner, &v.instance, &v.session, &v.trace, &v.at, &v.metadata); err != nil {
			rows.Close()
			return err
		}
		records = append(records, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, v := range records {
		if v.kind == "model_invocations" {
			var m models.ModelInvocation
			if err = json.Unmarshal([]byte(v.metadata), &m); err != nil {
				return err
			}
			m.ID = v.id
			m.TraceID = v.trace
			m.UserID = &v.owner
			m.InstanceID = &v.instance
			m.SessionID = &v.session
			m.CreatedAt = v.at
			if err = maintainInvocationSummary(ctx, tx, v.kind, &m); err != nil {
				return err
			}
		} else {
			var a models.AuditEvent
			if err = json.Unmarshal([]byte(v.metadata), &a); err != nil {
				return err
			}
			_, err = tx.SQL().ExecContext(ctx, `INSERT IGNORE INTO observation_audit_event_summaries(id,trace_id,request_id,session_id,user_id,instance_id,event_type,message,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, v.id, v.trace, a.RequestID, v.session, v.owner, v.instance, a.EventType, a.Message, v.at)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// No ID watermark: committed queue entries remain eligible even if a lower-ID
// legacy transaction commits after the initial source catchup completed.
func reconcilePendingAuditEvents(ctx context.Context, tx db.Session, limit int) (int, error) {
	rows, err := tx.SQL().QueryContext(ctx, `SELECT id FROM observation_audit_pending ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return 0, err
	}
	var ids []interface{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	ids, err = historymaintenance.WriteLogPrefix(ctx, monthlySQL{tx.SQL()}, "audit_events", ids)
	if err != nil {
		return 0, err
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	var events []models.AuditEvent
	if err = tx.Collection("audit_events").Find(db.Cond{"id IN": ids}).Select("id", "trace_id", "request_id", "session_id", "user_id", "instance_id", "event_type", "message", "created_at").All(&events); err != nil {
		return 0, err
	}
	for i := range events {
		if err = maintainObservationSummary(ctx, tx, "audit_events", &events[i]); err != nil {
			return 0, err
		}
	}
	_, err = tx.SQL().ExecContext(ctx, `DELETE FROM observation_audit_pending WHERE id IN (`+marks+`)`, ids...)
	return len(ids), err
}
