package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/upper/db/v4"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

const schemaMigrationsTable = "schema_migrations"

func applyEmbeddedMigrations(session db.Session) error {
	// Startup precedes leader election. Serialize migrations across App replicas
	// using a dedicated connection; pooled GET_LOCK/RELEASE_LOCK is unsafe.
	driver, ok := session.Driver().(*sql.DB)
	if !ok {
		return fmt.Errorf("migration lock requires MySQL SQL driver")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	conn, err := driver.Conn(ctx)
	if err != nil {
		return fmt.Errorf("migration lock connection: %w", err)
	}
	defer conn.Close()
	var acquired sql.NullInt64
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(CONCAT('cm:migrate:',MD5(DATABASE())),30)").Scan(&acquired); err != nil || !acquired.Valid || acquired.Int64 != 1 {
		return fmt.Errorf("migration lock unavailable: %v", err)
	}
	defer func() {
		release, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_, _ = conn.ExecContext(release, "SELECT RELEASE_LOCK(CONCAT('cm:migrate:',MD5(DATABASE())))")
	}()
	return applyEmbeddedMigrationsLocked(conn)
}

func applyEmbeddedMigrationsLocked(conn *sql.Conn) error {
	optionalReady := true
	if err := ensureSchemaMigrationsTable(conn); err != nil {
		return err
	}

	applied, err := listAppliedMigrations(conn)
	if err != nil {
		return err
	}

	entries, err := embeddedMigrations.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to list embedded migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		if _, ok := applied[entry.Name()]; ok {
			continue
		}
		if entry.Name() == "063_unique_usernames.sql" {
			if err := checkUniqueUsernames(conn); err != nil {
				return fmt.Errorf("cannot apply migration %s: %w", entry.Name(), err)
			}
		}

		rawSQL, err := embeddedMigrations.ReadFile(path.Join("migrations", entry.Name()))
		if err != nil {
			return fmt.Errorf("failed to read embedded migration %s: %w", entry.Name(), err)
		}

		statements := splitSQLStatements(string(rawSQL))
		optionalFailed := false
		for idx, statement := range statements {
			statementCtx := context.Background()
			stop := func() {}
			if entry.Name() == "067_add_northbound_owner_records.sql" {
				statementCtx, stop = context.WithTimeout(statementCtx, 30*time.Second)
			}
			_, statementErr := conn.ExecContext(statementCtx, statement)
			stop()
			if err := statementErr; err != nil {
				if entry.Name() == "067_add_northbound_owner_records.sql" {
					log.Printf("Optional owner analytics migration deferred: %T", err)
					optionalFailed = true
					break
				}
				return fmt.Errorf("failed to execute migration %s statement %d: %w", entry.Name(), idx+1, err)
			}
		}
		if optionalFailed {
			optionalReady = false
			continue
		}
		if entry.Name() == "067_add_northbound_owner_records.sql" {
			if err := verifyOwnerAnalytics(conn); err != nil {
				optionalReady = false
				log.Printf("Optional analytics schema incompatible: %T", err)
				continue
			}
		}

		if _, err := conn.ExecContext(context.Background(),
			fmt.Sprintf("INSERT INTO %s (filename, applied_at) VALUES (?, ?)", schemaMigrationsTable),
			entry.Name(),
			time.Now().UTC(),
		); err != nil {
			if entry.Name() == "067_add_northbound_owner_records.sql" {
				optionalReady = false
				log.Printf("Optional analytics migration record unavailable: %T", err)
				continue
			}
			return fmt.Errorf("failed to record applied migration %s: %w", entry.Name(), err)
		}

		log.Printf("Applied database migration %s", entry.Name())
	}
	if err := verifyOwnerAnalytics(conn); err != nil {
		ownerAnalyticsAvailable.Store(false)
		log.Printf("Owner analytics disabled; schema validation unavailable: %T", err)
	} else {
		ownerAnalyticsAvailable.Store(optionalReady)
	}
	return nil
}

// Group and join using the database collation, exactly as the unique index does.
// Include each conflicting row separately so long groups cannot be truncated.
func checkUniqueUsernames(conn *sql.Conn) error {
	rows, err := conn.QueryContext(context.Background(), `
		SELECT u.username, u.id, u.auth_provider
		FROM users AS u
		JOIN (SELECT username FROM users GROUP BY username HAVING COUNT(*) > 1) AS duplicates
		  ON duplicates.username = u.username
		ORDER BY u.username, u.id
	`)
	if err != nil {
		return fmt.Errorf("failed to check duplicate usernames: %w", err)
	}
	defer rows.Close()
	var conflicts []string
	for rows.Next() {
		var username, provider string
		var id int
		if err := rows.Scan(&username, &id, &provider); err != nil {
			return fmt.Errorf("failed to read duplicate username: %w", err)
		}
		conflicts = append(conflicts, fmt.Sprintf("username=%q id=%d auth_provider=%q", username, id, provider))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to read duplicate usernames: %w", err)
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("duplicate usernames must be resolved by an administrator before retrying the upgrade: %s", strings.Join(conflicts, "; "))
	}
	return nil
}

func ensureSchemaMigrationsTable(conn *sql.Conn) error {
	_, err := conn.ExecContext(context.Background(), fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id INT AUTO_INCREMENT PRIMARY KEY,
			filename VARCHAR(255) NOT NULL,
			applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE KEY uk_schema_migrations_filename (filename)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`, schemaMigrationsTable))
	if err != nil {
		return fmt.Errorf("failed to ensure schema migrations table: %w", err)
	}
	return nil
}

func listAppliedMigrations(conn *sql.Conn) (map[string]struct{}, error) {
	rows, err := conn.QueryContext(context.Background(), fmt.Sprintf("SELECT filename FROM %s", schemaMigrationsTable))
	if err != nil {
		return nil, fmt.Errorf("failed to query applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]struct{})
	for rows.Next() {
		var filename string
		if err := rows.Scan(&filename); err != nil {
			return nil, fmt.Errorf("failed to scan applied migration: %w", err)
		}
		applied[filename] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate applied migrations: %w", err)
	}

	return applied, nil
}

func splitSQLStatements(input string) []string {
	statements := make([]string, 0)
	var current strings.Builder

	inSingleQuote := false
	inDoubleQuote := false
	inBacktick := false
	inLineComment := false
	inBlockComment := false

	for i := 0; i < len(input); i++ {
		ch := input[i]
		var next byte
		if i+1 < len(input) {
			next = input[i+1]
		}

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
			}
			continue
		}

		if inBlockComment {
			if ch == '*' && next == '/' {
				inBlockComment = false
				i++
			}
			continue
		}

		if inSingleQuote {
			current.WriteByte(ch)
			if ch == '\\' && next != 0 {
				i++
				current.WriteByte(next)
				continue
			}
			if ch == '\'' {
				if next == '\'' {
					i++
					current.WriteByte(next)
					continue
				}
				inSingleQuote = false
			}
			continue
		}

		if inDoubleQuote {
			current.WriteByte(ch)
			if ch == '\\' && next != 0 {
				i++
				current.WriteByte(next)
				continue
			}
			if ch == '"' {
				if next == '"' {
					i++
					current.WriteByte(next)
					continue
				}
				inDoubleQuote = false
			}
			continue
		}

		if inBacktick {
			current.WriteByte(ch)
			if ch == '`' {
				inBacktick = false
			}
			continue
		}

		if ch == '-' && next == '-' {
			var afterNext byte
			if i+2 < len(input) {
				afterNext = input[i+2]
			}
			if afterNext == 0 || afterNext == ' ' || afterNext == '\t' || afterNext == '\r' || afterNext == '\n' {
				inLineComment = true
				i++
				continue
			}
		}

		if ch == '/' && next == '*' {
			inBlockComment = true
			i++
			continue
		}

		switch ch {
		case '\'':
			inSingleQuote = true
			current.WriteByte(ch)
		case '"':
			inDoubleQuote = true
			current.WriteByte(ch)
		case '`':
			inBacktick = true
			current.WriteByte(ch)
		case ';':
			statement := strings.TrimSpace(current.String())
			if statement != "" {
				statements = append(statements, statement)
			}
			current.Reset()
		default:
			current.WriteByte(ch)
		}
	}

	if statement := strings.TrimSpace(current.String()); statement != "" {
		statements = append(statements, statement)
	}

	return statements
}
