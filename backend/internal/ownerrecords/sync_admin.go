package ownerrecords

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/upper/db/v4"
	"strconv"
	"strings"
	"time"
)

type SyncQuery struct {
	Kind, Status, Owner string
	Instance, Before    uint64
	Limit, Days         int
}

var syncTypes = map[string]bool{}

func init() {
	for _, k := range strings.Fields("sync_batch sync_event sync_action summary_batch history_schedule recent_schedule history_scan report_dirty report_preview_job usage_dirty source_bootstrap source_change_cursor source_repair_cursor source_window_dirty native_reader_guard analytics_epoch owner_snapshot growth_profile xp_event xp_ledger login_day question_v4 recent_question daily_usage source_usage source_session report_preview report_head calendar_version") {
		syncTypes[k] = true
	}
}
func validateSyncQuery(q SyncQuery) (SyncQuery, error) {
	if !syncTypes[q.Kind] || q.Limit < 1 || q.Limit > 100 || len(q.Owner) > 128 || len(q.Status) > 24 || q.Days < 0 || q.Days > 30 {
		return q, fmt.Errorf("无效的记录类型或查询范围")
	}
	return q, nil
}

type SyncRecord struct {
	ID        uint64          `json:"id"`
	Kind      string          `json:"kind"`
	Owner     string          `json:"owner"`
	Instance  uint64          `json:"instance"`
	Runtime   string          `json:"runtime"`
	Status    string          `json:"status"`
	At        string          `json:"at"`
	Updated   string          `json:"updated"`
	Next      string          `json:"next"`
	Since     string          `json:"since"`
	Version   uint64          `json:"version"`
	Reason    string          `json:"reason"`
	Retryable bool            `json:"retryable"`
	Current   bool            `json:"current"`
	Details   json.RawMessage `json:"details,omitempty"`
}
type SyncPage struct {
	Records []SyncRecord `json:"records"`
	Next    uint64       `json:"next_cursor"`
	AsOf    time.Time    `json:"as_of"`
	Limited bool         `json:"limited"`
}
type SyncOverview struct {
	State     string `json:"state"`
	Checked   string `json:"checked_at"`
	Published string `json:"published_at"`
}

func (s *Store) SyncOverview(ctx context.Context) (SyncOverview, error) {
	v := SyncOverview{State: "unrecorded"}
	row, e := s.Session.SQL().QueryRowContext(ctx, `SELECT status,COALESCE(DATE_FORMAT(occurred_at,'%Y-%m-%dT%H:%i:%s.%fZ'),''),COALESCE(DATE_FORMAT(interval_end,'%Y-%m-%dT%H:%i:%s.%fZ'),'') FROM northbound_owner_records WHERE record_type='summary_batch' AND record_key=?`, Key("summary-batch", "v4"))
	if e != nil {
		return v, e
	}
	e = row.Scan(&v.State, &v.Checked, &v.Published)
	if errors.Is(e, sql.ErrNoRows) {
		return v, nil
	}
	return v, e
}

func syncRetryable(kind, state string) bool {
	return (kind == "history_schedule" || kind == "recent_schedule") && (state == "retry" || state == "restricted") || (kind == "report_dirty" || kind == "report_preview_job") && state == "pending"
}
func safeSyncReason(reason string) string {
	if len(reason) == 10 && strings.HasPrefix(reason, "mysql_") {
		if code, err := strconv.Atoi(reason[6:]); err == nil && code >= 1000 && code <= 9999 {
			return reason
		}
	}
	switch reason {
	case "", "budget", "budget_yield", "reader_busy", "instance_changed", "permission", "missing", "restricted", "invalid_format", "appending", "read_or_store", "report_budget", "report_read_or_store":
		return reason
	default:
		return "read_or_store"
	}
}
func (s *Store) SyncRecords(ctx context.Context, q SyncQuery, detailID uint64) (SyncPage, error) {
	result := SyncPage{Records: []SyncRecord{}, AsOf: time.Now().UTC()}
	q, e := validateSyncQuery(q)
	if e != nil {
		return result, e
	}
	// Use the existing (record_type,status,id) index. Auto-increment gaps from
	// UPSERTs must never hide long-lived schedules. At most 32 indexed lanes,
	// 200 IDs per lane, 200 merged candidates, no large JSON or table count.
	inner := `SELECT id FROM northbound_owner_records WHERE record_type=? AND id=?`
	args := []interface{}{q.Kind, detailID}
	if detailID > 0 {
		inner += " LIMIT 1"
	} else {
		statuses := []string{q.Status}
		if q.Status == "" {
			statuses = nil
			rows, err := s.Session.SQL().QueryContext(ctx, `SELECT DISTINCT status FROM northbound_owner_records FORCE INDEX(idx_owner_record_pending) WHERE record_type=? LIMIT 33`, q.Kind)
			if err != nil {
				return result, err
			}
			for rows.Next() {
				var status string
				if err = rows.Scan(&status); err != nil {
					rows.Close()
					return result, err
				}
				statuses = append(statuses, status)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return result, err
			}
			if len(statuses) > 32 {
				return result, fmt.Errorf("状态类别较多，请指定状态查询")
			}
		}
		if len(statuses) == 0 {
			return result, nil
		}
		lanes := []string{}
		args = nil
		for _, status := range statuses {
			lane := `SELECT id FROM northbound_owner_records FORCE INDEX(idx_owner_record_pending) WHERE record_type=? AND status=?`
			args = append(args, q.Kind, status)
			if q.Before > 0 {
				lane += " AND id<?"
				args = append(args, q.Before)
			}
			lanes = append(lanes, "("+lane+" ORDER BY id DESC LIMIT 200)")
		}
		inner = "SELECT id FROM (" + strings.Join(lanes, " UNION ALL ") + ") candidates ORDER BY id DESC LIMIT 200"
	}
	rows, e := s.Session.SQL().QueryContext(ctx, `SELECT r.id,COALESCE(r.owner_normalized,''),COALESCE(r.instance_id,0),COALESCE(r.runtime_type,''),r.status,COALESCE(DATE_FORMAT(r.occurred_at,'%Y-%m-%dT%H:%i:%s.%fZ'),''),DATE_FORMAT(r.updated_at,'%Y-%m-%dT%H:%i:%s.%fZ'),COALESCE(DATE_FORMAT(r.expires_at,'%Y-%m-%dT%H:%i:%s.%fZ'),''),COALESCE(DATE_FORMAT(r.interval_start,'%Y-%m-%dT%H:%i:%s.%fZ'),''),r.version,COALESCE(JSON_UNQUOTE(JSON_EXTRACT(r.payload,'$.reason')),''),
 CASE WHEN r.record_type IN ('sync_batch','sync_event','sync_action') THEN r.payload ELSE JSON_OBJECT('tokens',r.total_tokens,'questions',r.question_count,'sessions',r.session_count,'xp',r.xp_value,'date',r.business_date,'period',r.period_type,'period_start',r.period_start,'failures',JSON_EXTRACT(r.payload,'$.failures'),'retry_at',JSON_EXTRACT(r.payload,'$.retry_at')) END,
 CASE WHEN r.record_type IN ('history_schedule','recent_schedule') THEN EXISTS(SELECT 1 FROM instances i WHERE i.id=r.instance_id AND i.user_id=r.scope_id AND LOWER(TRIM(i.owner))=r.owner_normalized AND i.status='running' AND i.instance_mode='lite' AND r.source_key=SHA2(CONCAT(COALESCE(i.workspace_path,''),'|',COALESCE(i.access_token,'')),256)) ELSE TRUE END
 FROM northbound_owner_records r JOIN (`+inner+`) picked ON r.id=picked.id ORDER BY r.id DESC`, args...)
	if e != nil {
		return result, e
	}
	defer rows.Close()
	scanned := 0
	for rows.Next() {
		r := SyncRecord{Kind: q.Kind}
		var raw []byte
		if e = rows.Scan(&r.ID, &r.Owner, &r.Instance, &r.Runtime, &r.Status, &r.At, &r.Updated, &r.Next, &r.Since, &r.Version, &r.Reason, &raw, &r.Current); e != nil {
			return result, e
		}
		scanned++
		result.Next = r.ID
		if q.Status != "" && r.Status != q.Status || q.Owner != "" && !strings.Contains(strings.ToLower(r.Owner), strings.ToLower(q.Owner)) || q.Instance > 0 && r.Instance != q.Instance {
			continue
		}
		if q.Days > 0 {
			at, _ := time.Parse(time.RFC3339Nano, r.At)
			if at.Before(result.AsOf.AddDate(0, 0, -q.Days)) {
				continue
			}
		}
		r.Reason = safeSyncReason(r.Reason)
		r.Retryable = r.Current && syncRetryable(r.Kind, r.Status)
		if r.Kind == "report_dirty" || r.Kind == "report_preview_job" {
			var p struct {
				Failures int `json:"failures"`
			}
			_ = json.Unmarshal(raw, &p)
			r.Retryable = r.Retryable && p.Failures > 0
		}
		if detailID > 0 && len(raw) <= 32768 {
			r.Details = raw
		}
		result.Records = append(result.Records, r)
		if len(result.Records) == q.Limit {
			result.Limited = true
			return result, nil
		}
	}
	if e = rows.Err(); e != nil {
		return result, e
	}
	result.Limited = scanned == 200
	if scanned < 200 {
		result.Next = 0
	}
	if detailID > 0 {
		result.Next = 0
		result.Limited = false
	}
	return result, nil
}

var ErrSyncConflict = errors.New("记录已变化、尚未到期或不允许重试，请刷新后检查")

// No direct scan, no cursor reset, and no change to failure counters. Mutation
// and audit commit together. Current instance identity is checked again in SQL.
func (s *Store) RetrySync(ctx context.Context, id uint64, updated string, actor int, now time.Time) error {
	return s.Session.TxContext(ctx, func(tx db.Session) error {
		row, e := tx.SQL().QueryRowContext(ctx, `SELECT record_type,status,record_key,DATE_FORMAT(updated_at,'%Y-%m-%dT%H:%i:%s.%fZ'),COALESCE(JSON_EXTRACT(payload,'$.retry_requested_at'),0) FROM northbound_owner_records WHERE id=? FOR UPDATE`, id)
		if e != nil {
			return e
		}
		var kind, state, key, current string
		var previous int64
		if e = row.Scan(&kind, &state, &key, &current, &previous); e != nil {
			return e
		}
		if !syncRetryable(kind, state) || updated != current || previous > now.Add(-10*time.Minute).Unix() {
			return ErrSyncConflict
		}
		var affected int64
		if kind == "history_schedule" || kind == "recent_schedule" {
			res, e := tx.SQL().ExecContext(ctx, `UPDATE northbound_owner_records q JOIN instances i ON i.id=q.instance_id AND i.user_id=q.scope_id AND LOWER(TRIM(i.owner))=q.owner_normalized AND q.source_key=SHA2(CONCAT(COALESCE(i.workspace_path,''),'|',COALESCE(i.access_token,'')),256) SET q.expires_at=?,q.payload=JSON_SET(COALESCE(q.payload,JSON_OBJECT()),'$.retry_requested_at',?) WHERE q.id=? AND i.status='running' AND i.instance_mode='lite'`, nextSummaryBatch(now).UTC(), now.Unix(), id)
			if e != nil {
				return e
			}
			affected, e = res.RowsAffected()
			if e != nil {
				return e
			}
		} else {
			res, e := tx.SQL().ExecContext(ctx, `UPDATE northbound_owner_records SET payload=JSON_SET(COALESCE(payload,JSON_OBJECT()),'$.retry_at',?,'$.retry_requested_at',?) WHERE id=? AND expires_at<=? AND COALESCE(JSON_EXTRACT(payload,'$.failures'),0)>0`, nextSummaryBatch(now).Unix(), now.Unix(), id, now.UTC())
			if e != nil {
				return e
			}
			affected, e = res.RowsAffected()
			if e != nil {
				return e
			}
		}
		if affected != 1 {
			return ErrSyncConflict
		}
		body, _ := json.Marshal(map[string]interface{}{"record_id": id, "kind": kind, "actor": actor, "action": "retry_next_batch"})
		_, e = tx.SQL().ExecContext(ctx, `INSERT INTO northbound_owner_records(record_type,record_key,scope_id,status,occurred_at,payload) VALUES('sync_action',?,0,'queued',?,?)`, Key("sync-retry", key, updated), now.UTC(), string(body))
		return e
	}, nil)
}
