package historymaintenance

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
)

const MaxPageBytes = 8 << 20
const recordColumns = `kind,original_id,owner_id,instance_id,team_id,trace_id,session_id,created_at,month_table,object_ref,payload_sha256,payload_bytes,archived_at,readback_verified_at,restore_verified_at,restore_sha256,restore_receipt,source_deleted_at,metadata_json,summary_verified_at,summary_sha256`

type scanner interface{ Scan(...any) error }

func scanRecord(row scanner, extra ...any) (r Record, err error) {
	var restored, summary sql.NullTime
	var ref []byte
	args := []any{&r.Kind, &r.ID, &r.OwnerID, &r.InstanceID, &r.TeamID, &r.TraceID, &r.SessionID, &r.CreatedAt, &r.Month, &ref, &r.SHA256, &r.Bytes, &r.ArchivedAt, &r.ReadbackVerifiedAt, &restored, &r.RestoreSHA256, &r.RestoreReceipt, &r.SourceDeletedAt, &r.Metadata, &summary, &r.SummarySHA256}
	err = row.Scan(append(args, extra...)...)
	if summary.Valid {
		r.SummaryVerifiedAt = summary.Time
	}
	r.Reference = ref
	if restored.Valid {
		r.RestoreVerifiedAt = restored.Time
	}
	r.Status = "archived_source_retained"
	if r.SourceDeletedAt.Valid {
		r.Status = "archived"
	}
	return
}
func (s *Service) metadata(ctx context.Context, kind string, id, owner int64, admin bool) (Record, error) {
	if !supported(kind) || id <= 0 || (!admin && owner <= 0) {
		return Record{}, errors.New("authorized archive identity required")
	}
	q := `SELECT ` + recordColumns + ` FROM history_archive_records WHERE kind=? AND original_id=?`
	args := []any{kind, id}
	if !admin || owner > 0 {
		q += " AND owner_id=?"
		args = append(args, owner)
	}
	return scanRecord(s.DB.QueryRowContext(ctx, q, args...))
}
func (s *Service) readPayload(ctx context.Context, r Record) ([]byte, error) {
	if s.Store == nil || r.OwnerID <= 0 || r.Bytes <= 0 || r.Bytes > MaxPageBytes {
		return nil, errors.New("archive unavailable or exceeds page budget")
	}
	raw, err := s.Store.Read(ctx, Owner(r.OwnerID), r.Reference)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(raw)
	if int64(len(raw)) != r.Bytes || hex.EncodeToString(h[:]) != r.SHA256 || !json.Valid(raw) {
		return nil, errors.New("archive payload checksum mismatch")
	}
	return raw, nil
}
func (s *Service) Get(ctx context.Context, kind string, id, owner int64, admin bool) (Record, error) {
	if r, handled, err := s.MonthlyGet(ctx, kind, id, owner, admin); handled || err != nil {
		return r, err
	}
	r, err := s.metadata(ctx, kind, id, owner, admin)
	if err != nil {
		return r, err
	}
	r.Payload, err = s.readPayload(ctx, r)
	return r, err
}

type Query struct {
	Exact        bool
	Kind         string
	OwnerID      int64
	Admin        bool
	TraceID      string
	SessionID    string
	InstanceID   int64
	TeamID       int64
	Since, Until time.Time
	AfterID      int64
	Limit        int
}
type Page struct {
	Records []Record `json:"records"`
	NextID  int64    `json:"next_id,omitempty"`
	HasMore bool     `json:"has_more"`
	Bytes   int64    `json:"bytes"`
}

func (s *Service) List(ctx context.Context, q Query) ([]Record, error) {
	page, err := s.ListPage(ctx, q)
	return page.Records, err
}
func (s *Service) ListPage(ctx context.Context, q Query) (Page, error) {
	page := Page{Records: []Record{}}
	if !supported(q.Kind) || (!q.Admin && q.OwnerID <= 0) || q.Limit < 1 || q.Limit > 200 || q.AfterID < 0 || (!q.Exact && (!q.Until.After(q.Since) || q.Until.Sub(q.Since) > 90*24*time.Hour)) || (q.Exact && q.TraceID == "" && q.SessionID == "") {
		return page, errors.New("archive query requires explicit owner/admin,kind,1..200rows and<=90days")
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	months, err := ActiveLogMonths(ctx, tx, q.Kind, q.Since, q.Until, q.TraceID, q.SessionID)
	if err != nil {
		return page, err
	}
	parts := []string{`SELECT ` + recordColumns + `,FALSE inline_payload,NULL inline_bytes FROM history_archive_records a WHERE NOT EXISTS(SELECT 1 FROM history_month_catalog c WHERE c.table_name=a.month_table AND c.read_route='monthly')`}
	for _, name := range months {
		parts = append(parts, `SELECT '`+q.Kind+`',original_id,owner_id,instance_id,team_id,trace_id,session_id,created_at,'`+name+`',object_ref,payload_sha256,payload_bytes,created_at,created_at,NULL,'','',NULL,metadata_json,NULL,'',payload_json IS NOT NULL,OCTET_LENGTH(payload_json) FROM `+"`"+name+"`")
	}
	sqlQuery := `SELECT ` + recordColumns + `,inline_payload,inline_bytes FROM (` + strings.Join(parts, " UNION ALL ") + `) routed WHERE kind=? AND original_id>?`
	args := []any{q.Kind, q.AfterID}
	if !q.Exact || !q.Since.IsZero() {
		sqlQuery += " AND created_at>=?"
		args = append(args, q.Since.UTC())
	}
	if !q.Exact || !q.Until.IsZero() {
		sqlQuery += " AND created_at<?"
		args = append(args, q.Until.UTC())
	}
	if !q.Admin || q.OwnerID > 0 {
		sqlQuery += " AND owner_id=?"
		args = append(args, q.OwnerID)
	}
	for _, filter := range []struct {
		name    string
		value   any
		present bool
	}{{"trace_id", q.TraceID, q.TraceID != ""}, {"session_id", q.SessionID, q.SessionID != ""}, {"instance_id", q.InstanceID, q.InstanceID > 0}, {"team_id", q.TeamID, q.TeamID > 0}} {
		if filter.present {
			sqlQuery += " AND " + filter.name + "=?"
			args = append(args, filter.value)
		}
	}
	sqlQuery += " ORDER BY original_id LIMIT ?"
	args = append(args, q.Limit+1)
	rows, err := tx.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return page, err
	}
	type candidate struct {
		record Record
		inline bool
	}
	var records []candidate
	var selectedBytes int64
	for rows.Next() {
		var inline bool
		var inlineBytes sql.NullInt64
		r, e := scanRecord(rows, &inline, &inlineBytes)
		if e != nil {
			rows.Close()
			return page, e
		}
		if len(records) >= q.Limit || (len(records) > 0 && r.Bytes > MaxPageBytes-selectedBytes) {
			page.HasMore = true
			break
		}
		if r.Bytes <= 0 || r.Bytes > MaxPageBytes || (inline && (!inlineBytes.Valid || inlineBytes.Int64 != r.Bytes)) {
			rows.Close()
			return page, errors.New("history payload length invalid or exceeds page budget")
		}
		selectedBytes += r.Bytes
		records = append(records, candidate{r, inline})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	for _, candidate := range records {
		r := candidate.record
		var raw []byte
		var e error
		if candidate.inline {
			e = tx.QueryRowContext(ctx, "SELECT payload_json FROM `"+r.Month+"` WHERE original_id=?", r.ID).Scan(&raw)
			if e == nil && (int64(len(raw)) != r.Bytes || int64(len(raw)) > MaxPageBytes-page.Bytes) {
				e = errors.New("monthly payload exceeds remaining page budget")
			}
			if e == nil {
				e = decodeMonthlyPayload(&r, raw)
				raw = r.Payload
				r.Status = "monthly"
			}
		} else {
			raw, e = s.readPayload(ctx, r)
		}
		if e != nil {
			return page, e
		}
		r.Payload = raw
		page.Records = append(page.Records, r)
		page.Bytes += int64(len(raw))
		page.NextID = r.ID
	}
	return page, nil
}

var isolatedRestoreName = regexp.MustCompile(`^cm_history_restore_[a-f0-9]{32}$`)

// VerifyRestore writes the authenticated archive to an explicitly provisioned
// isolated MySQL schema, then reads and verifies the persisted bytes. A marker
// or caller assertion alone never grants permission to remove source rows.
func (s *Service) VerifyRestore(ctx context.Context, kind string, id int64, target *sql.DB, receipt string) error {
	ctx, cancel := context.WithTimeout(ctx, BatchDuration)
	defer cancel()
	if target == nil || receipt == "" || len(receipt) > 255 {
		return errors.New("isolated target and receipt required")
	}
	if s.Gate != nil {
		release, err := s.Gate(ctx)
		if err != nil {
			return err
		}
		if release == nil {
			return ErrBackupPaused
		}
		defer release()
	}
	var sourceName, targetName string
	if err := s.DB.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&sourceName); err != nil {
		return err
	}
	if err := target.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&targetName); err != nil {
		return err
	}
	if sourceName == targetName || !isolatedRestoreName.MatchString(targetName) {
		return errors.New("isolated archive restore target required")
	}
	var unknown int
	if err := target.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME<>'history_restore_rows'`).Scan(&unknown); err != nil {
		return err
	}
	if unknown != 0 {
		return errors.New("isolated restore schema contains unknown objects")
	}
	r, err := s.metadata(ctx, kind, id, 0, true)
	if err != nil {
		return err
	}
	raw, err := s.readPayload(ctx, r)
	if err != nil {
		return err
	}
	detail := raw
	if prover, ok := s.Store.(interface {
		RestoreDetail(context.Context, string, []byte) ([]byte, error)
	}); ok {
		detail, err = prover.RestoreDetail(ctx, r.Kind, raw)
		if err != nil {
			return err
		}
	} else if hasBodyReferences(raw) {
		return errors.New("isolated restore must authenticate and materialize nested body bytes")
	}
	if !json.Valid(detail) || len(detail) > MaxPageBytes {
		return errors.New("restored detail exceeds8MiB proof batch; original retained")
	}
	detailHash := sha256.Sum256(detail)
	detailDigest := hex.EncodeToString(detailHash[:])
	if _, err = target.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS history_restore_rows(kind VARCHAR(32) NOT NULL,original_id BIGINT NOT NULL,owner_id BIGINT NOT NULL,payload LONGBLOB NOT NULL,payload_sha256 CHAR(64) NOT NULL,detail_payload LONGBLOB NOT NULL,detail_sha256 CHAR(64) NOT NULL,PRIMARY KEY(kind,original_id)) ENGINE=InnoDB`); err != nil {
		return err
	}
	if _, err = target.ExecContext(ctx, `INSERT INTO history_restore_rows VALUES(?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE payload=VALUES(payload),payload_sha256=VALUES(payload_sha256),owner_id=VALUES(owner_id),detail_payload=VALUES(detail_payload),detail_sha256=VALUES(detail_sha256)`, r.Kind, r.ID, r.OwnerID, raw, r.SHA256, detail, detailDigest); err != nil {
		return err
	}
	var restored, restoredDetail []byte
	var owner int64
	var digest, restoredDetailDigest string
	if err = target.QueryRowContext(ctx, `SELECT owner_id,payload,payload_sha256,detail_payload,detail_sha256 FROM history_restore_rows WHERE kind=? AND original_id=?`, r.Kind, r.ID).Scan(&owner, &restored, &digest, &restoredDetail, &restoredDetailDigest); err != nil {
		return err
	}
	h := sha256.Sum256(restored)
	detailCheck := sha256.Sum256(restoredDetail)
	if restoredDetailDigest != detailDigest || hex.EncodeToString(detailCheck[:]) != detailDigest || string(restoredDetail) != string(detail) {
		return errors.New("isolated detail/body restore checksum mismatch")
	}
	if owner != r.OwnerID || digest != r.SHA256 || hex.EncodeToString(h[:]) != r.SHA256 || string(restored) != string(raw) {
		return errors.New("isolated restore checksum mismatch")
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE history_archive_records SET restore_verified_at=UTC_TIMESTAMP(6),restore_sha256=?,restore_receipt=? WHERE kind=? AND original_id=? AND payload_sha256=?`, r.SHA256, receipt, r.Kind, r.ID, r.SHA256)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("archive changed during isolated restore")
	}
	return nil
}

func hasBodyReferences(raw []byte) bool {
	var fields map[string]any
	if json.Unmarshal(raw, &fields) != nil {
		return true
	}
	for name, value := range fields {
		if strings.HasSuffix(name, "_body_ref") && value != nil && value != "" && value != "null" {
			return true
		}
	}
	return false
}
