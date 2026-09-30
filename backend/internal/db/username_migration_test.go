package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/upper/db/v4/adapter/mysql"
)

// Exercise the actual migration runner through database/sql without installing
// a test driver or connecting to an operator's database.
func TestUsernameMigrationBlocksDuplicatesAndCanBeRetried(t *testing.T) {
	state := &usernameMigrationDB{duplicates: [][]driver.Value{
		{"Alice", int64(7), "local"}, {"alice", int64(9), "ldap"},
	}}
	sqlDB := sql.OpenDB(state)
	session, err := mysql.New(sqlDB)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	err = applyEmbeddedMigrations(session)
	if err == nil {
		t.Fatal("duplicates must block migration")
	}
	for _, detail := range []string{"063_unique_usernames.sql", "Alice", "alice", "id=7", "id=9", "local", "ldap"} {
		if !strings.Contains(err.Error(), detail) {
			t.Fatalf("missing conflict detail %q: %v", detail, err)
		}
	}
	if state.prepared || state.applied {
		t.Fatal("conflicting upgrade must not execute or record migration")
	}
	state.duplicates = nil // Administrator resolves conflicts outside the migration.
	if err := applyEmbeddedMigrations(session); err != nil {
		t.Fatal(err)
	}
	if !state.prepared || !state.applied {
		t.Fatal("resolved upgrade must execute and record migration")
	}
	queries := state.conflictQueries
	if err := applyEmbeddedMigrations(session); err != nil {
		t.Fatal(err)
	}
	if state.conflictQueries != queries {
		t.Fatal("already applied migration must be skipped")
	}
}

func TestUsernameMigrationFailsClosedOnInspectionError(t *testing.T) {
	state := &usernameMigrationDB{queryError: errors.New("inspection unavailable")}
	session, err := mysql.New(sql.OpenDB(state))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := applyEmbeddedMigrations(session); err == nil || !strings.Contains(err.Error(), "inspection unavailable") {
		t.Fatalf("inspection result: %v", err)
	}
	if state.prepared || state.applied {
		t.Fatal("inspection failure must prevent migration")
	}
}

func TestUsernameMigrationPreservesDataAndCollation(t *testing.T) {
	raw, err := embeddedMigrations.ReadFile("migrations/063_unique_usernames.sql")
	if err != nil {
		t.Fatal(err)
	}
	statements := splitSQLStatements(string(raw))
	if len(statements) != 4 {
		t.Fatalf("statement count = %d", len(statements))
	}
	if !strings.Contains(statements[0], "ADD UNIQUE KEY uk_users_username (username)") || !strings.Contains(statements[0], "information_schema.STATISTICS") {
		t.Fatal("migration must add the global index idempotently")
	}
	for _, statement := range statements {
		for _, forbidden := range []string{"DELETE FROM", "UPDATE USERS", "DROP TABLE", "COLLATE", "MODIFY COLUMN"} {
			if strings.Contains(strings.ToUpper(statement), forbidden) {
				t.Fatalf("migration changes account data or collation: %s", statement)
			}
		}
	}
}

type usernameMigrationDB struct {
	duplicates        [][]driver.Value
	queryError        error
	prepared, applied bool
	conflictQueries   int
}

func (d *usernameMigrationDB) Connect(context.Context) (driver.Conn, error) {
	return &usernameMigrationConn{d}, nil
}
func (d *usernameMigrationDB) Driver() driver.Driver { return usernameMigrationDriver{} }

type usernameMigrationDriver struct{}

func (usernameMigrationDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use connector")
}

type usernameMigrationConn struct{ state *usernameMigrationDB }

func (c *usernameMigrationConn) Close() error { return nil }
func (c *usernameMigrationConn) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected transaction")
}
func (c *usernameMigrationConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared driver statement")
}
func (c *usernameMigrationConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if strings.Contains(query, "ADD UNIQUE KEY uk_users_username") {
		c.state.prepared = true
	}
	if strings.Contains(query, "INSERT INTO schema_migrations") {
		c.state.applied = true
	}
	return driver.RowsAffected(1), nil
}
func (c *usernameMigrationConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	query = strings.Join(strings.Fields(query), " ")
	if query == "SELECT DATABASE() AS name" {
		return &usernameMigrationRows{columns: []string{"name"}, values: [][]driver.Value{{"username_migration_test"}}}, nil
	}
	if strings.Contains(query, "SELECT filename FROM schema_migrations") {
		entries, err := embeddedMigrations.ReadDir("migrations")
		if err != nil {
			return nil, err
		}
		rows := &usernameMigrationRows{columns: []string{"filename"}}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") && (entry.Name() != "063_unique_usernames.sql" || c.state.applied) {
				rows.values = append(rows.values, []driver.Value{entry.Name()})
			}
		}
		return rows, nil
	}
	if strings.Contains(query, "HAVING COUNT(*) > 1") {
		c.state.conflictQueries++
		if c.state.queryError != nil {
			return nil, c.state.queryError
		}
		return &usernameMigrationRows{columns: []string{"username", "id", "auth_provider"}, values: c.state.duplicates}, nil
	}
	return nil, errors.New("unexpected query: " + query)
}

type usernameMigrationRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *usernameMigrationRows) Columns() []string { return r.columns }
func (r *usernameMigrationRows) Close() error      { return nil }
func (r *usernameMigrationRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}
