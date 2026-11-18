package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

var isolatedRestoreDatabase = regexp.MustCompile(`^cm_restore_[a-f0-9]{32}$`)

// PrepareIsolatedRestoreSchema installs this binary's trusted schema into a
// newly-created EMPTY drill database. It never executes source-provided DDL,
// seeds login accounts/configuration, starts repositories, or clears data.
// Resource ownership and network isolation are the controller's prerequisites.
func PrepareIsolatedRestoreSchema(ctx context.Context, database *sql.DB) error {
	conn, e := database.Conn(ctx)
	if e != nil {
		return e
	}
	defer conn.Close()
	var name string
	var count int
	if e = conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&name); e != nil {
		return e
	}
	if !isolatedRestoreDatabase.MatchString(name) {
		return errors.New("restore schema requires an isolated database")
	}
	if e = conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE()").Scan(&count); e != nil {
		return e
	}
	if count != 0 {
		return errors.New("restore schema refuses a nonempty database")
	}
	entries, e := embeddedMigrations.ReadDir("migrations")
	if e != nil {
		return e
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		raw, e := embeddedMigrations.ReadFile(path.Join("migrations", entry.Name()))
		if e != nil {
			return e
		}
		for i, statement := range splitSQLStatements(string(raw)) {
			upper := strings.ToUpper(strings.TrimSpace(statement))
			// The migration parser already removes comments. Seed DML is intentionally
			// excluded; updates/backfills on this empty schema cannot create records.
			if strings.HasPrefix(upper, "INSERT ") || strings.HasPrefix(upper, "REPLACE ") {
				continue
			}
			if _, e = conn.ExecContext(ctx, statement); e != nil {
				return fmt.Errorf("restore schema migration %s statement %d: %w", entry.Name(), i+1, e)
			}
		}
	}
	return nil
}
