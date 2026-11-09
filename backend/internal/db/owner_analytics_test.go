package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/upper/db/v4/adapter/mysql"
)

func TestOptionalAnalyticsMigrationMySQL(t *testing.T) {
	host := os.Getenv("NB_KEYS_TEST_MYSQL")
	if host == "" {
		t.Skip("local disposable MySQL not configured")
	}
	if !strings.HasPrefix(host, "127.0.0.1:") {
		t.Fatal("localhost required")
	}
	url := mysql.ConnectionURL{Host: host, User: "root", Password: os.Getenv("NB_KEYS_TEST_PASSWORD"), Database: "mysql", Options: map[string]string{"parseTime": "true"}}
	root, err := mysql.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	name := fmt.Sprintf("owner_schema_test_%d", time.Now().UnixNano())
	if _, err = root.SQL().Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	defer root.SQL().Exec("DROP DATABASE " + name)
	url.Database = name
	session, err := mysql.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	driver, ok := session.Driver().(*sql.DB)
	if !ok {
		t.Fatal("expected dedicated SQL connection support")
	}
	driver.SetMaxOpenConns(1)
	conn, err := driver.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = ensureSchemaMigrationsTable(conn); err != nil {
		t.Fatal(err)
	}
	// This fixture tests the optional migration, not the application's core schema.
	entries, err := embeddedMigrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") || entry.Name() == "067_add_northbound_owner_records.sql" {
			continue
		}
		if _, err = conn.ExecContext(context.Background(), "INSERT INTO schema_migrations(filename) VALUES (?)", entry.Name()); err != nil {
			t.Fatal(err)
		}
	}
	conn.Close()
	other, err := mysql.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	other.Driver().(*sql.DB).SetMaxOpenConns(1)
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); errors <- applyEmbeddedMigrations(session) }()
	go func() { defer wg.Done(); errors <- applyEmbeddedMigrations(other) }()
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if !OwnerAnalyticsAvailable() {
		t.Fatal("fresh optional table not available")
	}
	// Deliberately incompatible existing table: never drop/recreate or mark success.
	if _, err = session.SQL().Exec("DELETE FROM schema_migrations WHERE filename='067_add_northbound_owner_records.sql'"); err != nil {
		t.Fatal(err)
	}
	if _, err = session.SQL().Exec("RENAME TABLE northbound_owner_records TO retained_owner_records"); err != nil {
		t.Fatal(err)
	}
	if _, err = session.SQL().Exec("CREATE TABLE northbound_owner_records(sentinel INT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	if _, err = session.SQL().Exec("INSERT INTO northbound_owner_records VALUES(42)"); err != nil {
		t.Fatal(err)
	}
	if err = applyEmbeddedMigrations(session); err != nil {
		t.Fatal("optional schema must not prevent core startup", err)
	}
	if OwnerAnalyticsAvailable() {
		t.Fatal("incompatible schema exposed to requests")
	}
	row, err := session.SQL().QueryRow("SELECT sentinel FROM northbound_owner_records")
	if err != nil {
		t.Fatal(err)
	}
	var sentinel int
	if err = row.Scan(&sentinel); err != nil || sentinel != 42 {
		t.Fatal("existing data changed", err)
	}
	row, err = session.SQL().QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE filename='067_add_northbound_owner_records.sql'")
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = row.Scan(&count); err != nil || count != 0 {
		t.Fatal("failed migration marked complete", count, err)
	}
}
