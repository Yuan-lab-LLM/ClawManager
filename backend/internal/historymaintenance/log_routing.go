package historymaintenance

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var ErrMonthlyRouteIncomplete = errors.New("monthly history route incomplete; run bounded catchup or explicitly roll back")

type LogWriter interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func LogKind(kind string) bool {
	return kind == "model_invocations" || kind == "audit_events" || kind == "chat_messages" || kind == "risk_hits" || kind == "cost_records"
}
func LogTimeColumn(kind string) string {
	if kind == "cost_records" {
		return "recorded_at"
	}
	return "created_at"
}
func logQueue(kind string) string {
	switch kind {
	case "model_invocations":
		return "observation_invocation_pending"
	case "cost_records":
		return "observation_cost_pending"
	case "chat_messages":
		return "observation_chat_pending"
	case "risk_hits":
		return "observation_risk_pending"
	}
	return "observation_audit_pending"
}

// Dual writes share the caller's existing source/WAL projection transaction.
// Fixed sources retain public-ID allocation and the explicit rollback copy.
// Registered month rows carry actual serving payloads; no new global payload table.
func WriteLogBatch(ctx context.Context, w LogWriter, kind string, ids []any) error {
	processed, err := WriteLogPrefix(ctx, w, kind, ids)
	if err == nil && len(processed) != len(ids) {
		return errors.New("monthly write requires a smaller byte-bounded batch")
	}
	return err
}

// Bulk callers acknowledge and advance only the returned ordered prefix. A
// collection of individually valid rows always makes bounded forward progress.
func WriteLogPrefix(ctx context.Context, w LogWriter, kind string, ids []any) ([]any, error) {
	if !LogKind(kind) || len(ids) == 0 {
		return ids, nil
	}
	if len(ids) > BatchLimit {
		return nil, errors.New("monthly write batch exceeds 500")
	}
	var prior int64
	for _, id := range ids {
		if integer(id) <= prior {
			return nil, errors.New("ordered positive monthly IDs required")
		}
		prior = integer(id)
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	rows, err := w.QueryContext(ctx, "SELECT id,DATE_FORMAT("+LogTimeColumn(kind)+",'%Y%m'),@@transaction_isolation FROM `"+kind+"` WHERE id IN ("+marks+") ORDER BY id", ids...)
	if err != nil {
		return nil, err
	}
	sourceMonths := map[int64]string{}
	touchedSet := map[string]bool{}
	for rows.Next() {
		var id int64
		var month, isolation string
		if err = rows.Scan(&id, &month, &isolation); err != nil {
			rows.Close()
			return nil, err
		}
		if isolation != "REPEATABLE-READ" {
			rows.Close()
			return nil, errors.New("monthly source writes require REPEATABLE-READ transactions")
		}
		sourceMonths[id] = month
		touchedSet[month] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var touched []string
	for month := range touchedSet {
		touched = append(touched, month)
	}
	sort.Strings(touched)
	months := map[string]string{}
	for _, month := range touched {
		// The missing-key gap matters: first registration must wait for source
		// transactions that already decided to use the fixed route. Shared
		// ownership is compatible with ordinary writers; serving never locks.
		lockRows, e := w.QueryContext(ctx, `SELECT table_name FROM history_month_catalog FORCE INDEX(uk_history_kind_month) WHERE kind=? AND month_utc=? FOR SHARE`, kind, month)
		if e != nil {
			return nil, e
		}
		if lockRows.Next() {
			var name string
			e = lockRows.Scan(&name)
			if k, m, ok := ParseMonthly(name); e != nil || !ok || k != kind || m != month {
				lockRows.Close()
				return nil, errors.New("invalid registered log month")
			}
			months[month] = name
		}
		e = lockRows.Err()
		lockRows.Close()
		if e != nil {
			return nil, e
		}
	}
	var registered []any
	for _, id := range ids {
		if _, ok := months[sourceMonths[integer(id)]]; ok {
			registered = append(registered, id)
		}
	}
	if len(registered) == 0 {
		return ids, nil
	}
	marks = strings.TrimSuffix(strings.Repeat("?,", len(registered)), ",")
	// Current locking reads follow catalog ownership, so a concurrent body
	// migration cannot commit a newer row and then be overwritten by this batch.
	rows, err = w.QueryContext(ctx, "SELECT h.*,h.user_id archive_owner_id FROM `"+kind+"` h WHERE h.id IN ("+marks+") ORDER BY h.id FOR SHARE", registered...)
	if err != nil {
		return nil, err
	}
	records, err := scanSources(rows, kind)
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, r := range records {
		name, ok := months[r.CreatedAt.UTC().Format("200601")]
		if !ok {
			continue
		}
		r.Month = name
		if err = writeLogRecord(ctx, w, r); err != nil {
			return nil, err
		}
	}
	if len(records) > 0 && len(records) < len(registered) {
		last := records[len(records)-1].ID
		for i, id := range ids {
			if integer(id) > last {
				return ids[:i], nil
			}
		}
	}
	return ids, nil
}
func writeLogRecord(ctx context.Context, w LogWriter, r Record) error {
	h := sha256.Sum256(r.Payload)
	digest := hex.EncodeToString(h[:])
	// JSON source values are typed before either representation is produced.
	_, err := w.ExecContext(ctx, "INSERT INTO `"+r.Month+"`(original_id,owner_id,created_at,trace_id,instance_id,object_ref,payload_sha256,payload_bytes,session_id,team_id,metadata_json,payload_json) VALUES(?,?,?,?,?,CAST('null' AS JSON),?,?,?,?,?,?) ON DUPLICATE KEY UPDATE owner_id=VALUES(owner_id),created_at=VALUES(created_at),trace_id=VALUES(trace_id),instance_id=VALUES(instance_id),session_id=VALUES(session_id),team_id=VALUES(team_id),metadata_json=VALUES(metadata_json),payload_json=VALUES(payload_json),payload_sha256=VALUES(payload_sha256),payload_bytes=VALUES(payload_bytes)", r.ID, r.OwnerID, r.CreatedAt, r.TraceID, r.InstanceID, digest, len(r.Payload), r.SessionID, r.TeamID, string(r.Metadata), string(r.Payload))
	if err != nil {
		return err
	}
	_, err = w.ExecContext(ctx, `INSERT INTO history_log_locations(kind,original_id,month_table) VALUES(?,?,?) ON DUPLICATE KEY UPDATE month_table=VALUES(month_table)`, r.Kind, r.ID, r.Month)
	return err
}

// BackfillLogs migrates online rows as well as terminal history. It never removes
// fixed sources. A transactional trigger queue separately covers old writers
// whose lower allocated IDs commit after this batch's source snapshot.
func (s *Service) BackfillLogs(ctx context.Context, kind string, since, until time.Time, after int64, limit int) (int64, int, error) {
	if !LogKind(kind) || limit < 1 || limit > BatchLimit || !until.After(since) || until.Sub(since) > 93*24*time.Hour {
		return after, 0, errors.New("bounded log backfill required")
	}
	ctx, cancel := context.WithTimeout(ctx, BatchDuration)
	defer cancel()
	if s.Gate != nil {
		release, err := s.Gate(ctx)
		if err != nil {
			return after, 0, err
		}
		if release == nil {
			return after, 0, ErrBackupPaused
		}
		defer release()
	}
	month := time.Date(since.UTC().Year(), since.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	for month.Before(until) {
		if err := s.prepareMonth(ctx, kind, month); err != nil {
			return after, 0, err
		}
		month = month.AddDate(0, 1, 0)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return after, 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT id FROM `"+kind+"` WHERE "+LogTimeColumn(kind)+">=? AND "+LogTimeColumn(kind)+"<? AND id>? ORDER BY id LIMIT ?", since.UTC(), until.UTC(), after, limit)
	if err != nil {
		return after, 0, err
	}
	var ids []any
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return after, 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return after, 0, err
	}
	ids, err = WriteLogPrefix(ctx, tx, kind, ids)
	if err != nil {
		return after, 0, err
	}
	if len(ids) > 0 {
		after = integer(ids[len(ids)-1])
	}
	return after, len(ids), tx.Commit()
}

// VerifyLogMonth checks <=500 IDs in both directions against the current source
// and persisted payload. Activation is a separate command; incomplete or late
// legacy work prevents serving instead of silently falling back to the old copy.
func (s *Service) VerifyLogMonth(ctx context.Context, name string, after int64) (int64, bool, error) {
	kind, month, ok := ParseMonthly(name)
	if !ok || !LogKind(kind) {
		return after, false, errors.New("registered log month required")
	}
	if err := ValidateMonthly(ctx, s.DB, name); err != nil {
		return after, false, err
	}
	start, _ := time.Parse("200601", month)
	end := start.AddDate(0, 1, 0)
	ctx, cancel := context.WithTimeout(ctx, BatchDuration)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return after, false, err
	}
	defer tx.Rollback()
	var stored int64
	if err = tx.QueryRowContext(ctx, `SELECT route_after_id FROM history_month_catalog WHERE table_name=? FOR UPDATE`, name).Scan(&stored); err != nil {
		return after, false, err
	}
	if stored != after {
		return after, false, errors.New("log verification cursor mismatch")
	}
	var pending bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM `"+logQueue(kind)+"` p JOIN `"+kind+"` h ON h.id=p.id WHERE h."+LogTimeColumn(kind)+">=? AND h."+LogTimeColumn(kind)+"<?)", start, end).Scan(&pending); err != nil {
		return after, false, err
	}
	if pending {
		return after, false, ErrMonthlyRouteIncomplete
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM ((SELECT id FROM `"+kind+"` WHERE "+LogTimeColumn(kind)+">=? AND "+LogTimeColumn(kind)+"<? AND id>? ORDER BY id LIMIT 500) UNION (SELECT original_id id FROM `"+name+"` WHERE original_id>? ORDER BY original_id LIMIT 500)) ids ORDER BY id LIMIT 500", start, end, after, after)
	if err != nil {
		return after, false, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return after, false, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return after, false, err
	}
	for _, id := range ids {
		rows, err = tx.QueryContext(ctx, "SELECT h.*,h.user_id archive_owner_id FROM `"+kind+"` h WHERE id=?", id)
		if err != nil {
			return after, false, err
		}
		source, readErr := scanSources(rows, kind)
		rows.Close()
		if readErr != nil {
			return after, false, readErr
		}
		r, readErr := readMonthlyRecord(ctx, tx, name, kind, id, 0, true)
		if readErr != nil {
			return after, false, readErr
		}
		if len(source) == 0 {
			var deleted bool
			if err = tx.QueryRowContext(ctx, `SELECT source_deleted_at IS NOT NULL FROM history_archive_records WHERE kind=? AND original_id=?`, kind, id).Scan(&deleted); err != nil || !deleted {
				return after, false, ErrMonthlyRouteIncomplete
			}
			if _, err = s.readPayload(ctx, r); err != nil {
				return after, false, err
			}
		} else {
			h := sha256.Sum256(source[0].Payload)
			if hex.EncodeToString(h[:]) != r.SHA256 {
				return after, false, errors.New("monthly log content differs from source")
			}
		}
		after = id
	}
	complete := len(ids) < BatchLimit
	if _, err = tx.ExecContext(ctx, `UPDATE history_month_catalog SET route_after_id=?,route_verified=? WHERE table_name=?`, after, complete, name); err != nil {
		return after, false, err
	}
	return after, complete, tx.Commit()
}
func (s *Service) CutoverLogMonth(ctx context.Context, name string) error {
	kind, month, ok := ParseMonthly(name)
	if !ok || !LogKind(kind) {
		return errors.New("registered log month required")
	}
	if err := ValidateMonthly(ctx, s.DB, name); err != nil {
		return err
	}
	start, _ := time.Parse("200601", month)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var verified bool
	if err = tx.QueryRowContext(ctx, `SELECT route_verified FROM history_month_catalog WHERE table_name=? FOR UPDATE`, name).Scan(&verified); err != nil {
		return err
	}
	if !verified {
		return ErrMonthlyRouteIncomplete
	}
	var pending bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM `"+logQueue(kind)+"` p JOIN `"+kind+"` h ON h.id=p.id WHERE h."+LogTimeColumn(kind)+">=? AND h."+LogTimeColumn(kind)+"<?)", start, start.AddDate(0, 1, 0)).Scan(&pending); err != nil {
		return err
	}
	if pending {
		return ErrMonthlyRouteIncomplete
	}
	if _, err = tx.ExecContext(ctx, `UPDATE history_month_catalog SET read_route='monthly',state=IF(state='rolled_back','prepared',state) WHERE table_name=?`, name); err != nil {
		return err
	}
	return tx.Commit()
}

func readMonthlyRecord(ctx context.Context, q Queryer, name, kind string, id, owner int64, admin bool) (Record, error) {
	r := Record{Kind: kind, ID: id, Month: name, Status: "monthly"}
	var payload, ref []byte
	query := "SELECT owner_id,instance_id,team_id,trace_id,session_id,created_at,object_ref,payload_sha256,payload_bytes,metadata_json,payload_json FROM `" + name + "` WHERE original_id=?"
	args := []any{id}
	if !admin || owner > 0 {
		query += " AND owner_id=?"
		args = append(args, owner)
	}
	err := q.QueryRowContext(ctx, query, args...).Scan(&r.OwnerID, &r.InstanceID, &r.TeamID, &r.TraceID, &r.SessionID, &r.CreatedAt, &ref, &r.SHA256, &r.Bytes, &r.Metadata, &payload)
	r.Reference = ref
	if err != nil {
		return r, err
	}
	if len(payload) > 0 {
		err = decodeMonthlyPayload(&r, payload)
	}
	return r, err
}

// MonthlyGet returns handled=true whenever this identity belongs to an active
// route, including missing/corrupt data: callers must never mask it by fallback.
func (s *Service) MonthlyGet(ctx context.Context, kind string, id, owner int64, admin bool) (r Record, handled bool, err error) {
	if !LogKind(kind) {
		return r, false, nil
	}
	if id <= 0 || (!admin && owner <= 0) {
		return r, true, errors.New("authorized monthly identity required")
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return r, false, err
	}
	defer tx.Rollback()
	return s.monthlyGetSnapshot(ctx, tx, kind, id, owner, admin)
}

func (s *Service) monthlyGetSnapshot(ctx context.Context, tx *sql.Tx, kind string, id, owner int64, admin bool) (r Record, handled bool, err error) {
	var name, route string
	err = tx.QueryRowContext(ctx, `SELECT c.table_name,c.read_route FROM history_log_locations l JOIN history_month_catalog c ON c.table_name=l.month_table WHERE l.kind=? AND l.original_id=?`, kind, id).Scan(&name, &route)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, "SELECT c.table_name,c.read_route FROM `"+kind+"` h JOIN history_month_catalog c ON c.kind=? AND c.month_utc=DATE_FORMAT(h."+LogTimeColumn(kind)+",'%Y%m') WHERE h.id=?", kind, id).Scan(&name, &route)
	}
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `SELECT c.table_name,c.read_route FROM history_archive_records a JOIN history_month_catalog c ON c.table_name=a.month_table WHERE a.kind=? AND a.original_id=?`, kind, id).Scan(&name, &route)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, err
	}
	if route != "monthly" {
		return r, false, nil
	}
	if err = checkMonthlyPending(ctx, tx, kind, name); err != nil {
		return r, true, err
	}
	r, err = readMonthlyRecord(ctx, tx, name, kind, id, owner, admin)
	if err != nil {
		return r, true, err
	}
	if len(r.Payload) == 0 {
		r.Payload, err = s.readPayload(ctx, r)
	}
	return r, true, err
}

// LogDetails keeps route selection, ID candidates and payload reads in one
// read-only snapshot. A concurrent cutover/rollback cannot omit a source row or
// cause a payload to be read from a different route than its candidate query.
func (s *Service) LogDetails(ctx context.Context, kind, trace, session string, owner int64, limit int, recent bool) ([]Record, error) {
	if !LogKind(kind) || limit < 1 || limit > 200 || (!recent && trace == "" && session == "") {
		return nil, errors.New("bounded log detail query required")
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	months, err := ActiveLogMonths(ctx, tx, kind, time.Time{}, time.Time{}, trace, session)
	if err != nil {
		return nil, err
	}
	parts := []string{"SELECT id," + LogTimeColumn(kind) + " created_at,user_id owner_id,trace_id,session_id FROM `" + kind + "` h WHERE NOT EXISTS(SELECT 1 FROM history_month_catalog c WHERE c.kind='" + kind + "' AND c.month_utc=DATE_FORMAT(h." + LogTimeColumn(kind) + ",'%Y%m') AND c.read_route='monthly')",
		`SELECT original_id id,created_at,owner_id,trace_id,session_id FROM history_archive_records a WHERE kind='` + kind + `' AND NOT EXISTS(SELECT 1 FROM history_month_catalog c WHERE c.table_name=a.month_table AND c.read_route='monthly')`}
	for _, name := range months {
		parts = append(parts, "SELECT original_id id,created_at,owner_id,trace_id,session_id FROM `"+name+"`")
	}
	query := "SELECT DISTINCT id,created_at FROM (" + strings.Join(parts, " UNION ALL ") + ") candidates WHERE 1=1"
	args := []any{}
	for _, f := range []struct {
		column  string
		value   any
		include bool
	}{{"trace_id", trace, trace != ""}, {"session_id", session, session != ""}, {"owner_id", owner, owner > 0}} {
		if f.include {
			query += " AND " + f.column + "=?"
			args = append(args, f.value)
		}
	}
	query += " ORDER BY id"
	if recent {
		query = strings.TrimSuffix(query, " ORDER BY id") + " ORDER BY created_at DESC,id DESC"
	}
	query += " LIMIT ?"
	args = append(args, limit+1)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		var at time.Time
		if err = rows.Scan(&id, &at); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) > limit {
		if !recent {
			return nil, errors.New("history exceeds synchronous detail limits; use bounded cursor/export")
		}
		ids = ids[:limit]
	}
	out := make([]Record, 0, len(ids))
	size := 0
	for _, id := range ids {
		r, handled, e := s.monthlyGetSnapshot(ctx, tx, kind, id, owner, true)
		if e != nil {
			return nil, e
		}
		if !handled {
			q := "SELECT h.*,h.user_id archive_owner_id FROM `" + kind + "` h WHERE id=?"
			a := []any{id}
			if owner > 0 {
				q += " AND user_id=?"
				a = append(a, owner)
			}
			rows, e = tx.QueryContext(ctx, q, a...)
			if e != nil {
				return nil, e
			}
			fixed, readErr := scanSources(rows, kind, true)
			rows.Close()
			if readErr != nil {
				return nil, readErr
			}
			if len(fixed) > 0 {
				r = fixed[0]
			} else {
				q = `SELECT ` + recordColumns + ` FROM history_archive_records WHERE kind=? AND original_id=?`
				a = []any{kind, id}
				if owner > 0 {
					q += " AND owner_id=?"
					a = append(a, owner)
				}
				r, e = scanRecord(tx.QueryRowContext(ctx, q, a...))
				if e != nil {
					return nil, e
				}
				r.Payload, e = s.readPayload(ctx, r)
				if e != nil {
					return nil, e
				}
			}
		}
		size += len(r.Payload)
		if size > MaxPageBytes {
			return nil, errors.New("history exceeds8MiB; use bounded cursor/export")
		}
		out = append(out, r)
	}
	return out, nil
}
func checkMonthlyPending(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, kind, name string) error {
	_, month, ok := ParseMonthly(name)
	if !ok {
		return errors.New("invalid month")
	}
	start, _ := time.Parse("200601", month)
	rows, err := q.QueryContext(ctx, "SELECT EXISTS(SELECT 1 FROM `"+logQueue(kind)+"` p JOIN `"+kind+"` h ON h.id=p.id WHERE h."+LogTimeColumn(kind)+">=? AND h."+LogTimeColumn(kind)+"<?)", start, start.AddDate(0, 1, 0))
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		return errors.New("monthly readiness unavailable")
	}
	var pending bool
	if err = rows.Scan(&pending); err != nil {
		return err
	}
	if pending {
		return ErrMonthlyRouteIncomplete
	}
	return rows.Err()
}

func decodeMonthlyPayload(r *Record, payload []byte) error {
	// LONGTEXT preserves the canonical bytes produced by scanSources. Verify
	// them directly instead of allocating a decoded and re-encoded body copy.
	h := sha256.Sum256(payload)
	if int64(len(payload)) != r.Bytes || hex.EncodeToString(h[:]) != r.SHA256 || !json.Valid(payload) {
		return errors.New("monthly payload checksum mismatch")
	}
	r.Payload = payload
	return nil
}

// ActiveLogMonths is bounded by the caller's time window. Trace callers without
// a window get an explicit limit error instead of an unbounded SQL UNION.
func ActiveLogMonths(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, kind string, since, until time.Time, scope ...string) ([]string, error) {
	if !LogKind(kind) {
		return nil, nil
	}
	query := `SELECT table_name FROM history_month_catalog WHERE kind=? AND read_route='monthly'`
	args := []any{kind}
	if !since.IsZero() {
		query += " AND month_utc>=?"
		args = append(args, since.UTC().Format("200601"))
	}
	if !until.IsZero() {
		query += " AND month_utc<=?"
		args = append(args, until.UTC().Add(-time.Nanosecond).Format("200601"))
	}
	for i, column := range []string{"trace_id", "session_id"} {
		if i >= len(scope) || scope[i] == "" {
			continue
		}
		query += ` AND month_utc IN (SELECT DATE_FORMAT(` + LogTimeColumn(kind) + `,'%Y%m') FROM ` + "`" + kind + "`" + ` WHERE ` + column + `=? UNION SELECT DATE_FORMAT(created_at,'%Y%m') FROM history_archive_records WHERE kind=? AND ` + column + `=?)`
		args = append(args, scope[i], kind, scope[i])
	}
	query += " ORDER BY month_utc LIMIT 5"
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var names []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		if k, _, ok := ParseMonthly(name); !ok || k != kind {
			rows.Close()
			return nil, errors.New("invalid monthly route")
		}
		names = append(names, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(names) > 4 {
		return nil, fmt.Errorf("monthly history spans over 4 months; use a bounded time window or archive export")
	}
	for _, name := range names {
		if err = checkMonthlyPending(ctx, q, kind, name); err != nil {
			return nil, err
		}
	}
	return names, nil
}

// Chat/risk have no accounting projection. Their old-writer receipts are
// drained by the same bounded maintenance worker that prepares log months.
func (s *Service) CatchupLogs(ctx context.Context, kind string, limit int) (int, error) {
	if kind != "chat_messages" && kind != "risk_hits" {
		return 0, errors.New("chat/risk catchup kind required")
	}
	if limit < 1 || limit > BatchLimit {
		return 0, errors.New("catchup limit 1..500 required")
	}
	ctx, cancel := context.WithTimeout(ctx, BatchDuration)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT id FROM `"+logQueue(kind)+"` ORDER BY id LIMIT ?", limit)
	if err != nil {
		return 0, err
	}
	var ids []any
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
	if err != nil {
		return 0, err
	}
	ids, err = WriteLogPrefix(ctx, tx, kind, ids)
	if err != nil {
		return 0, err
	}
	if err = ClearLogPending(ctx, tx, kind, ids...); err != nil {
		return 0, err
	}
	return len(ids), tx.Commit()
}
func ClearLogPending(ctx context.Context, w LogWriter, kind string, ids ...any) error {
	if len(ids) == 0 {
		return nil
	}
	if !LogKind(kind) {
		return errors.New("unknown log kind")
	}
	_, err := w.ExecContext(ctx, "DELETE FROM `"+logQueue(kind)+"` WHERE id IN ("+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+")", ids...)
	return err
}

// Acquire before mutating an existing source row. Retention takes the same
// catalog -> source order, while readers remain ordinary snapshot readers.
func LockLogMonth(ctx context.Context, w LogWriter, kind string, at time.Time) error {
	if !LogKind(kind) {
		return nil
	}
	rows, err := w.QueryContext(ctx, `SELECT table_name FROM history_month_catalog WHERE kind=? AND month_utc=? FOR SHARE`, kind, at.UTC().Format("200601"))
	if err != nil {
		return err
	}
	for rows.Next() {
	}
	err = rows.Err()
	rows.Close()
	return err
}
