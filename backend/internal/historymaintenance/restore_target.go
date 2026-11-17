package historymaintenance

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

// OpenRestoreTarget opens a separate mounted credential, never the business
// pool. The account must be unable to read the source archive registry.
func (s *Service) OpenRestoreTarget(ctx context.Context, path string) (*sql.DB, error) {
	if path == "" {
		return nil, errors.New("CLAWMANAGER_HISTORY_RESTORE_DSN_FILE must name the dedicated isolated restore credential")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("history restore credential unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16384 || info.Mode().Perm()&0007 != 0 {
		return nil, errors.New("history restore credential must be a bounded private regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(f, 16385))
	if err != nil || len(raw) > 16384 {
		return nil, errors.New("history restore credential invalid")
	}
	defer clear(raw)
	cfg, err := mysql.ParseDSN(strings.TrimSpace(string(raw)))
	if err != nil || !isolatedRestoreName.MatchString(cfg.DBName) || cfg.User == "" || cfg.User == "root" || cfg.MultiStatements || cfg.AllowAllFiles {
		return nil, errors.New("dedicated isolated history restore credential required")
	}
	cfg.ParseTime = true
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 10 * time.Second
	cfg.WriteTimeout = 10 * time.Second
	target, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, errors.New("history restore connection unavailable")
	}
	target.SetMaxOpenConns(2)
	target.SetMaxIdleConns(1)
	if err = s.ValidateRestoreTarget(ctx, target); err != nil {
		target.Close()
		return nil, err
	}
	return target, nil
}
func (s *Service) ValidateRestoreTarget(ctx context.Context, target *sql.DB) error {
	var sourceDB, sourceUser, sourceServer, targetDB, targetUser, targetServer string
	if err := s.DB.QueryRowContext(ctx, "SELECT DATABASE(),CURRENT_USER(),@@server_uuid").Scan(&sourceDB, &sourceUser, &sourceServer); err != nil {
		return err
	}
	if err := target.QueryRowContext(ctx, "SELECT DATABASE(),CURRENT_USER(),@@server_uuid").Scan(&targetDB, &targetUser, &targetServer); err != nil {
		return err
	}
	if !isolatedRestoreName.MatchString(targetDB) || (sourceDB == targetDB && sourceServer == targetServer) || sourceUser == targetUser {
		return errors.New("history restore credential/schema isolation required")
	}
	// Probe only metadata with LIMIT0. An unexpected error is not evidence of
	// denial. On an independent server the source schema may be absent instead.
	rows, err := target.QueryContext(ctx, "SELECT original_id FROM `"+strings.ReplaceAll(sourceDB, "`", "``")+"`.history_archive_records LIMIT 0")
	if err == nil {
		rows.Close()
		return errors.New("history restore credential can access the business source")
	}
	var denied *mysql.MySQLError
	if !errors.As(err, &denied) || (denied.Number != 1142 && denied.Number != 1044 && !(sourceServer != targetServer && (denied.Number == 1049 || denied.Number == 1146))) {
		return errors.New("history restore source access isolation unproven")
	}
	var executable int
	if err := target.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE())+(SELECT COUNT(*) FROM information_schema.EVENTS WHERE EVENT_SCHEMA=DATABASE())+(SELECT COUNT(*) FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA=DATABASE())`).Scan(&executable); err != nil {
		return err
	}
	if executable != 0 {
		return errors.New("isolated restore target has executable schema objects")
	}
	var unknown int
	if err = target.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME<>'history_restore_rows'`).Scan(&unknown); err != nil {
		return err
	}
	if unknown != 0 {
		return errors.New("isolated history restore schema contains unknown objects")
	}
	return nil
}
