package historymaintenance

import (
	"clawreef/internal/backupgate"
	"clawreef/internal/config"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-sql-driver/mysql"
	"regexp"
	"strings"
	"time"
)

// DDL implicitly commits in MySQL and must never use the gated DML pool.
// This separate connection is bound to the same database; each trusted CREATE
// LIKE holds the independent work-unit lease until the DDL actually returns.
func (s *Service) ConfigureMonthlyDDL(ctx context.Context, cfg config.DatabaseConfig) (func(), error) {
	if s.Gate == nil {
		return nil, errors.New("monthly DDL requires independent database and backup boundary")
	}
	c := mysql.NewConfig()
	c.Net = "tcp"
	c.Addr = fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	c.User = cfg.User
	c.Passwd = cfg.Password
	c.DBName = cfg.Database
	c.ParseTime = true
	c.Timeout = 5 * time.Second
	c.ReadTimeout = 10 * time.Second
	c.WriteTimeout = 10 * time.Second
	db, err := sql.Open("mysql", c.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err = backupgate.RequireSameDatabase(ctx, s.DB, db); err != nil {
		db.Close()
		return nil, err
	}
	s.monthlyDDL = db
	return func() { db.Close() }, nil
}

var monthlyPattern = regexp.MustCompile(`^([a-z_]+)_([0-9]{6})$`)

func ParseMonthly(name string) (kind, month string, ok bool) {
	m := monthlyPattern.FindStringSubmatch(name)
	if len(m) != 3 || !supported(m[1]) {
		return "", "", false
	}
	at, err := time.Parse("200601", m[2])
	if err != nil || at.Year() < 2000 || at.Year() > 2199 {
		return "", "", false
	}
	return m[1], m[2], true
}

type Queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// SchemaFingerprint ignores the physical table name; columns and indexes must
// match the registered v1 template exactly. No arbitrary SQL is catalogued.
func SchemaFingerprint(ctx context.Context, q Queryer, table string) (string, error) {
	if table != "history_month_template" {
		if _, _, ok := ParseMonthly(table); !ok {
			return "", errors.New("unregistered monthly name")
		}
	}
	var records [][]string
	for _, query := range []string{`SELECT COLUMN_NAME,COLUMN_TYPE,IS_NULLABLE,COALESCE(COLUMN_DEFAULT,'<NULL>'),EXTRA FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? ORDER BY ORDINAL_POSITION`, `SELECT INDEX_NAME,CAST(SEQ_IN_INDEX AS CHAR),COLUMN_NAME,CAST(NON_UNIQUE AS CHAR),COALESCE(CAST(SUB_PART AS CHAR),'') FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? ORDER BY INDEX_NAME,SEQ_IN_INDEX`} {
		rows, err := q.QueryContext(ctx, query, table)
		if err != nil {
			return "", err
		}
		for rows.Next() {
			row := make([]string, 5)
			if err = rows.Scan(&row[0], &row[1], &row[2], &row[3], &row[4]); err != nil {
				rows.Close()
				return "", err
			}
			records = append(records, row)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return "", err
		}
	}
	if len(records) == 0 {
		return "", errors.New("monthly schema missing")
	}
	raw, _ := json.Marshal(records)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:]), nil
}

// ValidateMonthly verifies a frozen discovery candidate against this database's
// catalog and physical schema. It does not mutate any process-global registry.
func ValidateMonthly(ctx context.Context, q Queryer, table string) error {
	kind, month, ok := ParseMonthly(table)
	if !ok {
		return errors.New("invalid monthly table")
	}
	var storedKind, storedMonth, digest, state string
	var version int
	if err := q.QueryRowContext(ctx, `SELECT kind,month_utc,schema_version,schema_sha256,state FROM history_month_catalog WHERE table_name=?`, table).Scan(&storedKind, &storedMonth, &version, &digest, &state); err != nil {
		return fmt.Errorf("monthly catalog registration required: %w", err)
	}
	if storedKind != kind || storedMonth != month || version != SchemaVersion || (state != "prepared" && state != "active" && state != "verifying" && state != "rolled_back") {
		return errors.New("monthly catalog identity invalid")
	}
	actual, err := SchemaFingerprint(ctx, q, table)
	if err != nil {
		return err
	}
	template, err := SchemaFingerprint(ctx, q, "history_month_template")
	if err != nil {
		return err
	}
	if actual != digest || digest != template {
		return errors.New("monthly schema fingerprint mismatch")
	}
	return nil
}
func (s *Service) PrepareMonths(ctx context.Context, now time.Time) error {
	month := time.Date(now.UTC().Year(), now.UTC().Month(), 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i <= 2; i++ {
		for _, kind := range MonthlyKinds {
			if err := s.prepareMonth(ctx, kind, month.AddDate(0, i, 0)); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Service) prepareMonth(ctx context.Context, kind string, at time.Time) error {
	name, err := MonthlyTable(kind, at)
	if err != nil {
		return err
	}
	if _, _, ok := ParseMonthly(name); !ok {
		return errors.New("month outside supported years")
	}
	ddl := s.DB
	if s.monthlyDDL != nil {
		release, e := s.Gate(ctx)
		if e != nil {
			return e
		}
		if release == nil {
			return ErrBackupPaused
		}
		defer release()
		ddl = s.monthlyDDL
	}
	if _, err = ddl.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS `"+name+"` LIKE history_month_template"); err != nil {
		return err
	}
	digest, err := SchemaFingerprint(ctx, s.DB, name)
	if err != nil {
		return err
	}
	template, err := SchemaFingerprint(ctx, s.DB, "history_month_template")
	if err != nil {
		return err
	}
	if digest != template {
		return errors.New("existing monthly table differs from template")
	}
	_, err = s.DB.ExecContext(ctx, `INSERT IGNORE INTO history_month_catalog(table_name,kind,month_utc,schema_version,schema_sha256,created_at) VALUES(?,?,?,?,?,UTC_TIMESTAMP(6))`, name, kind, at.UTC().Format("200601"), SchemaVersion, digest)
	if err != nil {
		return err
	}
	return ValidateMonthly(ctx, s.DB, name)
}

// Backfill archives only eligible rows, returns a stable original-ID cursor,
// and never removes the source. Repeated batches are digest-idempotent.
func (s *Service) Backfill(ctx context.Context, kind string, since, until time.Time, after int64, limit int) (last int64, processed int, err error) {
	ctx, cancel := context.WithTimeout(ctx, BatchDuration)
	defer cancel()
	if s.Gate != nil {
		release, e := s.Gate(ctx)
		if e != nil {
			return after, 0, e
		}
		if release == nil {
			return after, 0, ErrBackupPaused
		}
		defer release()
	}
	if limit < 1 || limit > BatchLimit || !until.After(since) || until.Sub(since) > 93*24*time.Hour {
		return after, 0, errors.New("backfill requires1..500rows and<=93days")
	}
	base, err := sourceSQL(kind)
	if err != nil {
		return after, 0, err
	}
	rows, err := s.DB.QueryContext(ctx, base+" AND h.created_at>=? AND h.created_at<? AND h.id>? ORDER BY h.id LIMIT ?", since.UTC(), until.UTC(), after, limit)
	if err != nil {
		return after, 0, err
	}
	records, err := scanSources(rows, kind)
	rows.Close()
	if err != nil {
		return after, 0, err
	}
	last = after
	for _, r := range records {
		sealed, e := s.verifyArchive(ctx, r)
		if e != nil {
			return last, processed, e
		}
		if e = s.save(ctx, sealed); e != nil {
			return last, processed, e
		}
		last = r.ID
		processed++
	}
	return
}

// VerifyMonth is bounded and read-only until the final catalog state update.
// The caller resumes with the returned cursor; a complete verified batch is
// required for cutover. The count/hash recheck prevents concurrent inserts from
// being silently omitted by the validation cursor.
func (s *Service) VerifyMonth(ctx context.Context, name string, after int64) (last int64, complete bool, err error) {
	return s.verifyMonth(ctx, name, after, BatchLimit)
}
func (s *Service) verifyMonth(ctx context.Context, name string, after int64, limit int) (last int64, complete bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, BatchDuration)
	defer cancel()
	if err = ValidateMonthly(ctx, s.DB, name); err != nil {
		return after, false, err
	}
	kind, _, _ := ParseMonthly(name)
	var startedRevision int64
	if err = s.DB.QueryRowContext(ctx, `SELECT revision FROM history_month_catalog WHERE table_name=?`, name).Scan(&startedRevision); err != nil {
		return after, false, err
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT original_id FROM ((SELECT original_id FROM history_archive_records WHERE month_table=? AND original_id>? ORDER BY original_id LIMIT ?) UNION (SELECT original_id FROM `"+name+"` WHERE payload_json IS NULL AND original_id>? ORDER BY original_id LIMIT ?)) ids ORDER BY original_id LIMIT ?", name, after, limit, after, limit, limit)
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
	last = after
	for _, id := range ids {
		r, e := s.metadata(ctx, kind, id, 0, true)
		if e != nil {
			return last, false, e
		}
		if r.Month != name {
			return last, false, errors.New("monthly locator mismatch")
		}
		var digest, trace string
		var owner, instance, bytes int64
		var created time.Time
		var reference []byte
		if e = s.DB.QueryRowContext(ctx, "SELECT owner_id,created_at,trace_id,instance_id,object_ref,payload_sha256,payload_bytes FROM `"+name+"` WHERE original_id=?", id).Scan(&owner, &created, &trace, &instance, &reference, &digest, &bytes); e != nil || owner != r.OwnerID || instance != r.InstanceID || trace != r.TraceID || !created.Equal(r.CreatedAt) || digest != r.SHA256 || bytes != r.Bytes || string(reference) != string(r.Reference) {
			return last, false, errors.New("monthly locator/readback mismatch")
		}

		if _, e = s.readPayload(ctx, r); e != nil {
			return last, false, e
		}
		last = id
	}
	// A trusted persisted verification cursor prevents callers skipping rows.
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return last, false, err
	}
	defer tx.Rollback()
	var cursor, revision int64
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT verify_after_id,state,revision FROM history_month_catalog WHERE table_name=? FOR UPDATE`, name).Scan(&cursor, &state, &revision); err != nil {
		return last, false, err
	}
	if revision != startedRevision {
		return last, false, errors.New("monthly archive changed during verification; restart")
	}
	if state == "rolled_back" {
		return last, false, errors.New("month rolled back; explicit backfill/cutover required")
	}
	if after != cursor {
		return last, false, errors.New("monthly verification cursor mismatch; restart from persisted cursor")
	}
	complete = len(ids) < limit
	state = "verifying"
	if complete {
		state = "active"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE history_month_catalog SET state=?,verify_after_id=?,verified_at=IF(?='active',UTC_TIMESTAMP(6),NULL) WHERE table_name=?`, state, last, state, name); err != nil {
		return last, false, err
	}
	return last, complete, tx.Commit()
}
func (s *Service) RollbackMonth(ctx context.Context, name string) error {
	if _, _, ok := ParseMonthly(name); !ok {
		return errors.New("invalid monthly name")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// All catalog writers take this row before archive evidence. A rollback's
	// source check and state change therefore cannot straddle source deletion.
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT state FROM history_month_catalog WHERE table_name=? FOR UPDATE`, name).Scan(&state); err != nil {
		return err
	}
	var deleted int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM history_archive_records WHERE month_table=? AND source_deleted_at IS NOT NULL`, name).Scan(&deleted); err != nil {
		return err
	}
	if deleted != 0 {
		return errors.New("rollback requires restoring deleted sources first")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE history_month_catalog SET state='rolled_back',read_route='legacy',route_verified=FALSE,route_after_id=0,verified_at=NULL,verify_after_id=0 WHERE table_name=?`, name); err != nil {
		return err
	}
	return tx.Commit()
}
func MonthlyKind(name string) string {
	kind, _, _ := ParseMonthly(name)
	return strings.TrimSpace(kind)
}
