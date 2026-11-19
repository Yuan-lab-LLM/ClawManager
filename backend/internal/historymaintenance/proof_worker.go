package historymaintenance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ProvePending performs actual isolated restore/readback, not an operator-set
// boolean. A crash can leave at most a bounded restore batch; the next pass
// removes only rows whose matching verified source receipt already exists.
func (s *Service) ProvePending(parent context.Context, target *sql.DB, limit int) (int, error) {
	if target == nil || limit < 1 || limit > BatchLimit {
		return 0, errors.New("isolated restore target and1..500rows required")
	}
	ctx, cancel := context.WithTimeout(parent, BatchDuration)
	defer cancel()
	var name, server string
	if err := target.QueryRowContext(ctx, "SELECT DATABASE(),@@server_uuid").Scan(&name, &server); err != nil {
		return 0, err
	}
	if !isolatedRestoreName.MatchString(name) {
		return 0, errors.New("invalid isolated proof target")
	}
	if err := s.cleanupProofRows(ctx, target, limit); err != nil {
		return 0, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT kind,original_id,payload_sha256,payload_bytes FROM history_archive_records WHERE source_deleted_at IS NULL AND (restore_verified_at IS NULL OR restore_sha256<>payload_sha256) ORDER BY kind,original_id LIMIT ?`, limit)
	if err != nil {
		return 0, err
	}
	type pending struct {
		kind, digest string
		id, bytes    int64
	}
	var work []pending
	var total int64
	for rows.Next() {
		var p pending
		if err = rows.Scan(&p.kind, &p.id, &p.digest, &p.bytes); err != nil {
			rows.Close()
			return 0, err
		}
		if total+p.bytes > MaxPageBytes && len(work) > 0 {
			break
		}
		total += p.bytes
		work = append(work, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, p := range work {
		receipt := fmt.Sprintf("mysql:%s/%s/%s/%d/%s", server, name, p.kind, p.id, p.digest)
		if err = s.VerifyRestore(ctx, p.kind, p.id, target, receipt); err != nil {
			return n, err
		}
		n++
		if _, err = target.ExecContext(ctx, `DELETE FROM history_restore_rows WHERE kind=? AND original_id=? AND payload_sha256=?`, p.kind, p.id, p.digest); err != nil {
			return n, err
		}
	}
	return n, nil
}
func (s *Service) cleanupProofRows(ctx context.Context, target *sql.DB, limit int) error {
	var exists int
	if err := target.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='history_restore_rows'`).Scan(&exists); err != nil || exists == 0 {
		return err
	}
	rows, err := target.QueryContext(ctx, `SELECT kind,original_id,payload_sha256 FROM history_restore_rows ORDER BY kind,original_id LIMIT ?`, limit)
	if err != nil {
		return err
	}
	type item struct {
		kind, digest string
		id           int64
	}
	var work []item
	for rows.Next() {
		var p item
		if err = rows.Scan(&p.kind, &p.id, &p.digest); err != nil {
			rows.Close()
			return err
		}
		work = append(work, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, p := range work {
		var count int
		if err = s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM history_archive_records WHERE kind=? AND original_id=? AND restore_verified_at IS NOT NULL AND restore_sha256=? AND payload_sha256=?`, p.kind, p.id, p.digest, p.digest).Scan(&count); err != nil {
			return err
		}
		if count == 1 {
			if _, err = target.ExecContext(ctx, `DELETE FROM history_restore_rows WHERE kind=? AND original_id=? AND payload_sha256=?`, p.kind, p.id, p.digest); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Service) CutoverPending(parent context.Context, limit int) (int, error) {
	if limit < 1 || limit > BatchLimit {
		return 0, errors.New("cutover requires1..500rows")
	}
	ctx, cancel := context.WithTimeout(parent, BatchDuration)
	defer cancel()
	if s.Gate != nil {
		release, err := s.Gate(ctx)
		if err != nil {
			return 0, err
		}
		if release == nil {
			return 0, ErrBackupPaused
		}
		defer release()
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT table_name,verify_after_id FROM history_month_catalog WHERE state IN ('prepared','verifying') ORDER BY month_utc,kind LIMIT 500`)
	if err != nil {
		return 0, err
	}
	type month struct {
		name  string
		after int64
	}
	var months []month
	for rows.Next() {
		var m month
		if err = rows.Scan(&m.name, &m.after); err != nil {
			rows.Close()
			return 0, err
		}
		months = append(months, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	total := 0
	for _, m := range months {
		if total >= limit {
			break
		}
		last, _, err := s.verifyMonth(ctx, m.name, m.after, limit-total)
		if err != nil {
			return total, err
		}
		var n int
		if err = s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM `"+m.name+"` WHERE original_id>? AND original_id<=?", m.after, last).Scan(&n); err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}
func (s *Service) RunWithRestore(ctx context.Context, target *sql.DB, report func(error)) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	run := func() {
		if _, err := s.Tick(ctx); err != nil && report != nil {
			report(err)
		}
		if target != nil {
			if _, err := s.ProvePending(ctx, target, BatchLimit); err != nil && report != nil {
				report(err)
			}
		}
		if _, err := s.CutoverPending(ctx, BatchLimit); err != nil && report != nil {
			report(err)
		}
	}
	for {
		run()
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
