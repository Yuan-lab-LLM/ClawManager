package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"clawreef/internal/migrationcatalog"
)

type MigrationTarget struct {
	ServerUUID, Database, CatalogSHA256 string
	PrepareTrialRuntime                 bool
}

var migrationDatabaseName = regexp.MustCompile("^[a-z][a-z0-9_]{0,63}$")

func EmbeddedMigrationCatalog() (migrationcatalog.Catalog, error) {
	return migrationcatalog.Compute(embeddedMigrations, "migrations")
}

// ApplyPinnedMigrations runs the image's embedded migrations against the
// deployment's exact database and catalog. Trial targets receive an inert
// schema and deployment defaults only.
func ApplyPinnedMigrations(ctx context.Context, pool *sql.DB, target MigrationTarget) error {
	catalog, err := EmbeddedMigrationCatalog()
	trial := strings.HasPrefix(target.Database, "cm_drill_trial_")
	if err != nil || pool == nil || !migrationDatabaseName.MatchString(target.Database) || target.ServerUUID == "" ||
		target.CatalogSHA256 != catalog.Hash || (target.PrepareTrialRuntime && !trial) {
		return errors.New("trial migration pins are incomplete or mismatched")
	}
	conn, err := pool.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var database, uuid string
	if conn.QueryRowContext(ctx, "SELECT DATABASE(), @@server_uuid").Scan(&database, &uuid) != nil ||
		database != target.Database || uuid != target.ServerUUID {
		return errors.New("trial migration database identity mismatch")
	}
	var locked sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(CONCAT('cm:migrate:',MD5(DATABASE())),30)").Scan(&locked); err != nil || !locked.Valid || locked.Int64 != 1 {
		return errors.New("trial migration lock unavailable")
	}
	defer func() {
		release, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(release, "SELECT RELEASE_LOCK(CONCAT('cm:migrate:',MD5(DATABASE())))")
	}()
	if !trial {
		return applyEmbeddedMigrationsLocked(conn)
	}
	if err := applyTrialTargetMigrations(ctx, conn, catalog.Filenames); err != nil {
		return err
	}
	if !target.PrepareTrialRuntime {
		return nil
	}
	_, err = conn.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS trial_runtime_admission (id TINYINT UNSIGNED NOT NULL PRIMARY KEY CHECK (id = 1), proof BINARY(32) NOT NULL) ENGINE=InnoDB")
	return err
}

func applyTrialTargetMigrations(ctx context.Context, conn *sql.Conn, filenames []string) error {
	if err := ensureSchemaMigrationsTable(conn); err != nil {
		return err
	}
	applied, err := listAppliedMigrations(conn)
	if err != nil {
		return err
	}
	for _, filename := range filenames {
		if _, exists := applied[filename]; exists {
			continue
		}
		raw, err := embeddedMigrations.ReadFile(path.Join("migrations", filename))
		if err != nil {
			return err
		}
		for index, statement := range splitSQLStatements(string(raw)) {
			if !trialMigrationStatement(statement) {
				continue
			}
			if _, err := conn.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("trial migration %s statement %d: %w", filename, index+1, err)
			}
		}
		if _, err := conn.ExecContext(ctx, "INSERT INTO schema_migrations (filename, applied_at) VALUES (?, ?)", filename, time.Now().UTC()); err != nil {
			return err
		}
	}
	return nil
}

func trialMigrationStatement(statement string) bool {
	upper := strings.ToUpper(strings.TrimSpace(statement))
	if strings.HasPrefix(upper, "CREATE TRIGGER ") || strings.HasPrefix(upper, "CREATE EVENT ") ||
		strings.HasPrefix(upper, "CREATE VIEW ") {
		return false
	}
	for _, prefix := range []string{"INSERT INTO ", "INSERT IGNORE INTO ", "REPLACE INTO "} {
		if !strings.HasPrefix(upper, prefix) {
			continue
		}
		table := strings.FieldsFunc(upper[len(prefix):], func(r rune) bool { return r == ' ' || r == '(' || r == '\r' || r == '\n' || r == '\t' })
		if len(table) == 0 {
			return false
		}
		switch strings.Trim(table[0], "`") {
		case "SYSTEM_IMAGE_SETTINGS", "SKILL_HUB_TAGS", "INSTANCE_LIFECYCLE_SCHEDULER", "NORTHBOUND_ADMIN_SETTINGS":
			return true
		}
		return false
	}
	return true
}
