package historymaintenance

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrBackupPaused = errors.New("history maintenance paused by backup gate")

type Result struct {
	Processed int  `json:"processed"`
	Archived  int  `json:"archived"`
	Deleted   int  `json:"deleted"`
	LeaseBusy bool `json:"lease_busy"`
}

func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		_, _ = s.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Service) Tick(parent context.Context) (result Result, err error) {
	if s.DB == nil {
		return result, errors.New("history database required")
	}
	ctx, cancel := context.WithTimeout(parent, BatchDuration)
	defer cancel()
	if s.Gate != nil {
		release, e := s.Gate(ctx)
		if e != nil {
			return result, e
		}
		if release == nil {
			return result, ErrBackupPaused
		}
		defer release()
	}
	_, err = s.DB.ExecContext(ctx, `INSERT IGNORE INTO history_maintenance_leases(name,holder,lease_until) VALUES('history','',UTC_TIMESTAMP(6))`)
	if err != nil {
		return result, err
	}
	claimed, e := s.DB.ExecContext(ctx, `UPDATE history_maintenance_leases SET holder=?,lease_until=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 45 SECOND),last_started_at=UTC_TIMESTAMP(6),last_error='' WHERE name='history' AND lease_until<=UTC_TIMESTAMP(6)`, s.holder)
	if e != nil {
		return result, e
	}
	n, e := claimed.RowsAffected()
	if e != nil {
		return result, e
	}
	if n == 0 {
		result.LeaseBusy = true
		return result, nil
	}
	defer func() {
		finish, c := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
		defer c()
		msg := ""
		if err != nil {
			msg = err.Error()
			if len(msg) > 1000 {
				msg = msg[:1000]
			}
		}
		_, finishErr := s.DB.ExecContext(finish, `UPDATE history_maintenance_leases SET lease_until=UTC_TIMESTAMP(6),last_finished_at=UTC_TIMESTAMP(6),processed_rows=?,last_error=? WHERE name='history' AND holder=?`, result.Processed, msg, s.holder)
		if err == nil {
			err = finishErr
		}
	}()
	if err = s.PrepareMonths(ctx, time.Now().UTC()); err != nil {
		return result, err
	}
	for _, kind := range []string{"chat_messages", "risk_hits"} {
		if result.Processed == BatchLimit {
			break
		}
		n, e := s.CatchupLogs(ctx, kind, BatchLimit-result.Processed)
		if e != nil {
			return result, e
		}
		result.Processed += n
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -7)
	res, e := s.DB.ExecContext(ctx, `DELETE FROM instance_model_activity_requests WHERE finished_at<? AND completion_reason='completed' AND lease_expires_at IS NOT NULL AND lease_expires_at<UTC_TIMESTAMP(6) ORDER BY finished_at,request_id LIMIT ?`, cutoff, BatchLimit-result.Processed)
	if e != nil {
		return result, e
	}
	n, e = res.RowsAffected()
	if e != nil {
		return result, e
	}
	result.Processed += int(n)
	result.Deleted += int(n)
	if result.Processed < BatchLimit {
		n, e = s.pruneOutbox(ctx, cutoff, BatchLimit-result.Processed)
		if e != nil {
			return result, e
		}
		result.Processed += int(n)
		result.Deleted += int(n)
	}
	if result.Processed < BatchLimit {
		n, e = s.pruneCancelledOutbox(ctx, cutoff, BatchLimit-result.Processed)
		if e != nil {
			return result, e
		}
		result.Processed += int(n)
		result.Deleted += int(n)
	}
	if s.Store == nil {
		return result, nil
	}
	for _, kind := range Kinds {
		if result.Processed >= BatchLimit {
			break
		}
		if e = ctx.Err(); e != nil {
			return result, e
		}
		records, e := s.candidates(ctx, kind, time.Now().UTC().AddDate(0, 0, -90), BatchLimit-result.Processed)
		if e != nil {
			return result, e
		}
		for _, r := range records {
			if e = ctx.Err(); e != nil {
				return result, e
			}
			result.Processed++
			sealed, e := s.verifyArchive(ctx, r)
			if e != nil {
				return result, e
			}
			if e = s.save(ctx, sealed); e != nil {
				return result, e
			}
			result.Archived++
			if _, e = s.DB.ExecContext(ctx, `UPDATE history_maintenance_cursors SET after_id=?,updated_at=UTC_TIMESTAMP(6) WHERE kind=?`, r.ID, kind); e != nil {
				return result, e
			}
			deleted, e := s.deleteVerified(ctx, sealed)
			if e != nil {
				return result, e
			}
			if deleted {
				result.Deleted++
			}
		}
	}
	return result, nil
}
func (s *Service) pruneOutbox(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT o.id,o.team_id,o.destination,o.message_id,o.source_event_id,o.payload_json,o.delivered_at,COALESCE(rc.source_window_sha256,c.proof_sha256) FROM team_event_outbox o JOIN teams t ON t.id=o.team_id LEFT JOIN history_replay_closures c ON c.team_id=o.team_id LEFT JOIN team_outbox_receipt_closures rc ON rc.outbox_id=o.id AND rc.team_id=o.team_id LEFT JOIN team_tasks task ON task.id=rc.task_id AND task.team_id=o.team_id WHERE o.status='delivered' AND o.delivered_at<? AND ((t.status='deleted' AND o.created_at<c.closed_before AND c.replay_closed=TRUE AND c.recovery_closed=TRUE AND c.idempotency_durable=TRUE AND LENGTH(c.proof_sha256)=64) OR (rc.redis_reclaimed_at IS NOT NULL AND o.created_at<rc.closed_before AND o.delivered_at<rc.closed_before AND LENGTH(rc.source_window_sha256)=64 AND task.status IN ('completed','succeeded','failed','cancelled') AND task.finished_at IS NOT NULL AND task.finished_at<rc.closed_before AND NOT EXISTS(SELECT 1 FROM team_work_items w WHERE w.root_task_id=task.id AND (w.finished_at IS NULL OR w.status NOT IN ('completed','succeeded','failed','cancelled','stale'))) AND NOT EXISTS(SELECT 1 FROM team_workflow_phases p WHERE p.root_task_id=task.id AND (p.completed_at IS NULL OR p.status NOT IN ('completed','failed','cancelled','skipped'))) AND NOT EXISTS(SELECT 1 FROM team_event_outbox retry WHERE retry.team_id=o.team_id AND retry.destination LIKE 'internal:%' AND retry.status<>'delivered'))) ORDER BY o.id LIMIT ? FOR UPDATE`, cutoff, limit)
	if err != nil {
		return 0, err
	}
	type item struct {
		id, team                                     int64
		destination, message, source, payload, proof string
		at                                           time.Time
	}
	var items []item
	for rows.Next() {
		var i item
		if err = rows.Scan(&i.id, &i.team, &i.destination, &i.message, &i.source, &i.payload, &i.at, &i.proof); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, i := range items {
		proof, proofErr := hex.DecodeString(i.proof)
		if proofErr != nil || len(proof) != 32 {
			return 0, errors.New("invalid durable replay closure proof")
		}
		hash := sha256.Sum256([]byte(i.payload))
		digest := hex.EncodeToString(hash[:])
		_, err = tx.ExecContext(ctx, `INSERT IGNORE INTO history_outbox_receipts(team_id,destination,message_id,source_event_id,payload_sha256,delivered_at,closure_sha256) VALUES(?,?,?,?,?,?,?)`, i.team, i.destination, i.message, i.source, digest, i.at, i.proof)
		if err != nil {
			return 0, err
		}
		var existing, source string
		if err = tx.QueryRowContext(ctx, `SELECT payload_sha256,source_event_id FROM history_outbox_receipts WHERE team_id=? AND destination=? AND message_id=?`, i.team, i.destination, i.message).Scan(&existing, &source); err != nil {
			return 0, err
		}
		if existing != digest || source != i.source {
			return 0, errors.New("outbox receipt identity conflict")
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM team_event_outbox WHERE id=? AND status='delivered'`, i.id); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return int64(len(items)), nil
}

const terminalInvocation = `i.completed_at IS NOT NULL AND i.status IN ('completed','failed','blocked','cancelled')`

func eligibility(kind string) (string, string, error) {
	switch kind {
	case "model_invocations":
		return "h.user_id", `h.user_id>0 AND h.completed_at IS NOT NULL AND h.status IN ('completed','failed','blocked','cancelled')`, nil
	case "audit_events", "chat_messages", "risk_hits":
		return "h.user_id", `h.user_id>0 AND NOT EXISTS(SELECT 1 FROM model_invocations i WHERE i.user_id=h.user_id AND i.trace_id=h.trace_id AND NOT (` + terminalInvocation + `))`, nil
	case "team_events":
		return "t.user_id", `t.user_id>0 AND t.status='deleted' AND EXISTS(SELECT 1 FROM history_replay_closures c WHERE c.team_id=h.team_id AND c.closed_before>h.created_at AND c.replay_closed=TRUE AND c.recovery_closed=TRUE AND c.idempotency_durable=TRUE AND LENGTH(c.proof_sha256)=64) AND (h.task_id IS NULL OR EXISTS(SELECT 1 FROM team_tasks task WHERE task.id=h.task_id AND task.finished_at IS NOT NULL AND task.status IN ('completed','succeeded','failed','cancelled')))`, nil
	case "team_tasks":
		return "t.user_id", `t.user_id>0 AND t.status='deleted' AND h.finished_at IS NOT NULL AND h.status IN ('completed','succeeded','failed','cancelled') AND EXISTS(SELECT 1 FROM history_replay_closures c WHERE c.team_id=h.team_id AND c.closed_before>h.created_at AND c.replay_closed=TRUE AND c.recovery_closed=TRUE AND c.idempotency_durable=TRUE AND LENGTH(c.proof_sha256)=64) AND NOT EXISTS(SELECT 1 FROM team_events e WHERE e.task_id=h.id) AND NOT EXISTS(SELECT 1 FROM team_work_items w WHERE w.root_task_id=h.id) AND NOT EXISTS(SELECT 1 FROM team_workflow_phases p WHERE p.root_task_id=h.id)`, nil
	}
	return "", "", errors.New("unsupported history kind")
}
func sourceSQL(kind string) (string, error) {
	owner, predicate, err := eligibility(kind)
	if err != nil {
		return "", err
	}
	join := ""
	if strings.HasPrefix(kind, "team_") {
		join = " JOIN teams t ON t.id=h.team_id"
	}
	return "SELECT h.*," + owner + " AS archive_owner_id FROM `" + kind + "` h" + join + " WHERE " + predicate, nil
}
func (s *Service) candidates(ctx context.Context, kind string, cutoff time.Time, limit int) ([]Record, error) {
	_, err := s.DB.ExecContext(ctx, `INSERT IGNORE INTO history_maintenance_cursors(kind,after_id,updated_at) VALUES(?,0,UTC_TIMESTAMP(6))`, kind)
	if err != nil {
		return nil, err
	}
	var after int64
	if err = s.DB.QueryRowContext(ctx, `SELECT after_id FROM history_maintenance_cursors WHERE kind=?`, kind).Scan(&after); err != nil {
		return nil, err
	}
	base, err := sourceSQL(kind)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, base+" AND h.created_at<? AND h.id>? ORDER BY h.id LIMIT ?", cutoff, after, limit)
	if err != nil {
		return nil, err
	}
	records, err := scanSources(rows, kind)
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 && after > 0 {
		_, err = s.DB.ExecContext(ctx, `UPDATE history_maintenance_cursors SET after_id=0,updated_at=UTC_TIMESTAMP(6) WHERE kind=?`, kind)
	}
	return records, err
}
func scanSources(rows *sql.Rows, kind string, requireAll ...bool) ([]Record, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	var out []Record
	var totalBytes int
	for rows.Next() {
		values := make([]any, len(cols))
		ptr := make([]any, len(cols))
		for i := range ptr {
			ptr[i] = &values[i]
		}
		if err = rows.Scan(ptr...); err != nil {
			return nil, err
		}
		m := map[string]any{}
		for i, v := range values {
			if b, ok := v.([]byte); ok {
				v = string(b)
				if types[i].DatabaseTypeName() == "DECIMAL" || types[i].DatabaseTypeName() == "NEWDECIMAL" {
					v = json.Number(string(b))
				}
			}
			m[cols[i]] = v
		}
		// Normalize the SQL representation once, before metadata and payload are
		// derived. MySQL sends BOOLEAN as int64 and DECIMAL as bytes.
		if kind == "model_invocations" {
			if n, ok := m["is_streaming"].(int64); ok {
				m["is_streaming"] = n != 0
			}
		}
		r := Record{Kind: kind, ID: integer(m["id"]), OwnerID: integer(m["archive_owner_id"]), InstanceID: integer(m["instance_id"]), TeamID: integer(m["team_id"]), TraceID: str(m["trace_id"]), SessionID: str(m["session_id"])}
		delete(m, "archive_owner_id")
		if at, ok := m[LogTimeColumn(kind)].(time.Time); ok {
			r.CreatedAt = at.UTC()
		} else {
			return nil, errors.New("history requires parsed created_at")
		}
		r.Metadata, err = summaryMetadata(m)
		if err != nil {
			return nil, err
		}
		r.Payload, err = json.Marshal(m)
		if err != nil {
			return nil, err
		}
		if len(r.Payload) > 8<<20 {
			if len(out) > 0 && !(len(requireAll) > 0 && requireAll[0]) {
				break
			}
			return nil, errors.New("history row exceeds8MiB; source retained")
		}
		if totalBytes+len(r.Payload) > MaxPageBytes && len(out) > 0 {
			if len(requireAll) > 0 && requireAll[0] {
				return nil, errors.New("monthly batch exceeds8MiB; source/queue retained")
			}
			break
		}
		totalBytes += len(r.Payload)
		out = append(out, r)
	}
	return out, rows.Err()
}
func integer(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case uint64:
		return int64(n)
	case string:
		var parsed int64
		fmt.Sscan(n, &parsed)
		return parsed
	}
	return 0
}
func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func (s *Service) save(ctx context.Context, r Record) error {
	if err := s.prepareMonth(ctx, r.Kind, r.CreatedAt); err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if r.TeamID > 0 {
		var team int64
		if err = tx.QueryRowContext(ctx, `SELECT id FROM teams WHERE id=? FOR UPDATE`, r.TeamID).Scan(&team); err != nil {
			return err
		}
	}
	// Team -> month catalog -> archive evidence is shared with source deletion.
	var catalogState string
	if err = tx.QueryRowContext(ctx, `SELECT state FROM history_month_catalog WHERE table_name=? FOR UPDATE`, r.Month).Scan(&catalogState); err != nil {
		return err
	}
	// Archive bytes were authenticated before entering SQL. Recheck the source
	// under the shared catalog -> source lock order before changing an active
	// month, otherwise an old snapshot could overwrite a newer body migration.
	base, e := sourceSQL(r.Kind)
	if e != nil {
		return e
	}
	rows, e := tx.QueryContext(ctx, base+" AND h.id=? FOR UPDATE", r.ID)
	if e != nil {
		return e
	}
	current, e := scanSources(rows, r.Kind)
	rows.Close()
	if e != nil {
		return e
	}
	if len(current) != 1 || string(current[0].Payload) != string(r.Payload) {
		return errors.New("source changed before archive commit; retry without replacing monthly payload")
	}
	if err = preserveIdentities(ctx, tx, r); err != nil {
		return err
	}
	var oldDigest string
	var oldOwner, oldInstance, oldTeam int64
	var oldTrace, oldSession string
	var oldCreated time.Time
	oldErr := tx.QueryRowContext(ctx, `SELECT payload_sha256,owner_id,instance_id,team_id,trace_id,session_id,created_at FROM history_archive_records WHERE kind=? AND original_id=? FOR UPDATE`, r.Kind, r.ID).Scan(&oldDigest, &oldOwner, &oldInstance, &oldTeam, &oldTrace, &oldSession, &oldCreated)
	if oldErr == nil && (oldOwner != r.OwnerID || oldInstance != r.InstanceID || oldTeam != r.TeamID || oldTrace != r.TraceID || oldSession != r.SessionID || !oldCreated.Equal(r.CreatedAt)) {
		return errors.New("archived stable identity cannot be reassigned")
	}

	if oldErr != nil && !errors.Is(oldErr, sql.ErrNoRows) {
		return oldErr
	}
	if oldDigest != r.SHA256 {
		if _, err = tx.ExecContext(ctx, `UPDATE history_month_catalog SET state=IF(state='rolled_back',state,'prepared'),verified_at=NULL,verify_after_id=0,revision=revision+1 WHERE table_name=?`, r.Month); err != nil {
			return err
		}
	}
	// Identical retries retain restore proof. A source mutation invalidates it.
	_, err = tx.ExecContext(ctx, `INSERT INTO history_archive_records(kind,original_id,owner_id,instance_id,team_id,trace_id,session_id,created_at,month_table,object_ref,payload_sha256,payload_bytes,metadata_json,archived_at,readback_verified_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE summary_verified_at=IF(payload_sha256=VALUES(payload_sha256),summary_verified_at,NULL),summary_sha256=IF(payload_sha256=VALUES(payload_sha256),summary_sha256,''),metadata_json=VALUES(metadata_json),restore_verified_at=IF(payload_sha256=VALUES(payload_sha256),restore_verified_at,NULL),restore_sha256=IF(payload_sha256=VALUES(payload_sha256),restore_sha256,''),restore_receipt=IF(payload_sha256=VALUES(payload_sha256),restore_receipt,''),object_ref=VALUES(object_ref),payload_bytes=VALUES(payload_bytes),readback_verified_at=VALUES(readback_verified_at),payload_sha256=VALUES(payload_sha256)`, r.Kind, r.ID, r.OwnerID, r.InstanceID, r.TeamID, r.TraceID, r.SessionID, r.CreatedAt, r.Month, []byte(r.Reference), r.SHA256, r.Bytes, []byte(r.Metadata), r.ArchivedAt, r.ReadbackVerifiedAt)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO `"+r.Month+"`(original_id,owner_id,created_at,trace_id,instance_id,object_ref,payload_sha256,payload_bytes,session_id,team_id,metadata_json) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE object_ref=VALUES(object_ref),payload_sha256=VALUES(payload_sha256),payload_bytes=VALUES(payload_bytes),metadata_json=VALUES(metadata_json),payload_json=NULL", r.ID, r.OwnerID, r.CreatedAt, r.TraceID, r.InstanceID, []byte(r.Reference), r.SHA256, r.Bytes, r.SessionID, r.TeamID, []byte(r.Metadata))
	if err != nil {
		return err
	}
	if LogKind(r.Kind) {
		if _, err = tx.ExecContext(ctx, `INSERT INTO history_log_locations(kind,original_id,month_table) VALUES(?,?,?) ON DUPLICATE KEY UPDATE month_table=VALUES(month_table)`, r.Kind, r.ID, r.Month); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Service) deleteVerified(ctx context.Context, r Record) (bool, error) {
	sealed, err := s.metadata(ctx, r.Kind, r.ID, r.OwnerID, false)
	if err != nil {
		return false, err
	}
	if !sealed.Removable() {
		return false, nil
	}
	raw, err := s.readPayload(ctx, sealed)
	if err != nil {
		return false, err
	}
	if string(raw) != string(r.Payload) {
		return false, errors.New("archive changed before deletion")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if r.TeamID > 0 {
		var team int64
		if err = tx.QueryRowContext(ctx, `SELECT id FROM teams WHERE id=? FOR UPDATE`, r.TeamID).Scan(&team); err != nil {
			return false, err
		}
	}
	var monthState string
	if err = tx.QueryRowContext(ctx, `SELECT state FROM history_month_catalog WHERE table_name=? FOR UPDATE`, sealed.Month).Scan(&monthState); err != nil {
		return false, err
	}
	if monthState != "active" {
		return false, nil
	}
	base, err := sourceSQL(r.Kind)
	if err != nil {
		return false, err
	}
	rows, err := tx.QueryContext(ctx, base+" AND h.id=? AND h.created_at<? FOR UPDATE", r.ID, time.Now().UTC().AddDate(0, 0, -90))
	if err != nil {
		return false, err
	}
	actual, err := scanSources(rows, r.Kind)
	rows.Close()
	if err != nil {
		return false, err
	}
	if len(actual) != 1 || string(actual[0].Payload) != string(raw) {
		return false, nil
	}
	// Lock the evidence row too: restore proof/source hash cannot change under us.
	var digest, restore, receipt, summaryDigest string
	var verified, summaryAt sql.NullTime
	if err = tx.QueryRowContext(ctx, `SELECT payload_sha256,restore_sha256,restore_receipt,restore_verified_at,summary_verified_at,summary_sha256 FROM history_archive_records WHERE kind=? AND original_id=? FOR UPDATE`, r.Kind, r.ID).Scan(&digest, &restore, &receipt, &verified, &summaryAt, &summaryDigest); err != nil {
		return false, err
	}
	if digest != r.SHA256 || restore != digest || !verified.Valid || receipt == "" || !summaryAt.Valid || summaryDigest != digest {
		return false, nil
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM `"+r.Kind+"` WHERE id=?", r.ID); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE history_archive_records SET source_deleted_at=UTC_TIMESTAMP(6) WHERE kind=? AND original_id=?`, r.Kind, r.ID); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM history_log_locations WHERE kind=? AND original_id=?`, r.Kind, r.ID); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func summaryMetadata(row map[string]any) ([]byte, error) {
	out := map[string]any{}
	for _, key := range []string{"status", "request_id", "is_streaming", "attempt_no", "model_id", "provider_type", "requested_model", "actual_provider_model", "prompt_tokens", "completion_tokens", "total_tokens", "cached_tokens", "reasoning_tokens", "latency_ms", "ttft_ms", "generation_ms", "tokens_per_second", "completed_at", "event_type", "severity", "role", "sequence_no", "invocation_id", "error_message", "message", "rule_id", "rule_name", "action", "recorded_at", "model_name", "currency", "estimated_cost", "internal_cost"} {
		if value, ok := row[key]; ok {
			out[key] = value
		}
	}
	return json.Marshal(out)
}
