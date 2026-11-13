package systembackup

import (
	"bytes"
	"clawreef/internal/systembackup/team"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"clawreef/internal/config"
	appdb "clawreef/internal/db"
)

func fullSchemaDB(t *testing.T) *sql.DB {
	t.Helper()
	d := fixtureDatabase(t)
	var name string
	if e := d.QueryRow("SELECT DATABASE()").Scan(&name); e != nil {
		t.Fatal(e)
	}
	port, e := strconv.Atoi(os.Getenv("TEST_SYSTEM_BACKUP_MYSQL_PORT"))
	if e != nil {
		t.Fatal(e)
	}
	session, e := appdb.Initialize(config.DatabaseConfig{Host: "127.0.0.1", Port: port, User: "root", Password: os.Getenv("TEST_SYSTEM_BACKUP_MYSQL_PASSWORD"), Database: name})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { session.Close() })
	return d
}

func TestSchemaContractMatchesFreshMigrations(t *testing.T) {
	db := fullSchemaDB(t)
	actual, e := DiscoverSchema(context.Background(), db)
	if e != nil {
		t.Fatal(e)
	}
	if os.Getenv("UPDATE_SYSTEM_BACKUP_SCHEMA") == "1" {
		raw, e := json.MarshalIndent(actual, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile("schema.json", append(raw, '\n'), 0644); e != nil {
			t.Fatal(e)
		}
		return
	}
	expected, e := BuiltinSchema()
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Fatal("schema changed: review backup field policies and regenerate schema contract")
	}
	if e = ValidateSchema(context.Background(), db); e != nil {
		t.Fatal(e)
	}
	// A real migrated observation row must be exported and preserved as history.
	if _, e = db.Exec(`INSERT INTO workspace_dashboard_observations VALUES(7,'2026-11-12 00:00:00','2026-11-12 00:00:01.123456','{"total":2,"running":1,"offline":1,"eligible_slots":null}')`); e != nil {
		t.Fatal(e)
	}
	var archive bytes.Buffer
	result, e := ExportMySQL(context.Background(), db, &archive, []string{"workspace_dashboard_observations"})
	if e != nil || result.Tables["workspace_dashboard_observations"] != 1 {
		t.Fatal("observation history not captured", result, e)
	}
	restored := 0
	if _, e = ReadSQLSnapshot(bytes.NewReader(archive.Bytes()), nil, func(table SQLTable, row team.Row) error {
		change, err := NormalizeDataRow(table, row, time.Now().UTC())
		if err != nil {
			return err
		}
		if change.Reason != "preserved" || change.Quarantined || !reflect.DeepEqual(change.After, row) {
			t.Error("observation history changed on data-only normalization")
		}
		restored++
		return nil
	}); e != nil || restored != 1 {
		t.Fatal("observation archive lost rows", restored, e)
	}
	if _, e = db.Exec("ALTER TABLE workspace_dashboard_observations ADD COLUMN new_executable_token TEXT NULL"); e != nil {
		t.Fatal(e)
	}
	if e = ValidateSchema(context.Background(), db); e == nil {
		t.Fatal("unknown observation field did not stop capture")
	}
	if _, e = db.Exec("ALTER TABLE workspace_dashboard_observations DROP COLUMN new_executable_token"); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("ALTER TABLE team_tasks ADD COLUMN new_executable_token TEXT NULL"); e != nil {
		t.Fatal(e)
	}
	if e = ValidateSchema(context.Background(), db); e == nil {
		t.Fatal("unknown field did not stop capture")
	}
}

func TestWorkspaceObservationEmbeddedSchema(t *testing.T) {
	tables, err := BuiltinSchema()
	if err != nil {
		t.Fatal(err)
	}
	expected := SQLTable{Name: "workspace_dashboard_observations", Columns: []SQLColumn{{Name: "user_id", Type: "int", Nullable: "NO"}, {Name: "bucket_start", Type: "datetime", Nullable: "NO"}, {Name: "observed_at", Type: "datetime(6)", Nullable: "NO"}, {Name: "snapshot_json", Type: "json", Nullable: "NO"}}, PrimaryKey: []string{"user_id", "bucket_start"}}
	for _, table := range tables {
		if table.Name == expected.Name {
			if !reflect.DeepEqual(table, expected) {
				t.Fatalf("observation contract changed: %+v", table)
			}
			return
		}
	}
	t.Fatal("observation history missing from strict embedded business schema")
}
