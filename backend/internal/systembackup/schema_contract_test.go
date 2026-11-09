package systembackup

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"testing"

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
	session, e := appdb.Initialize(config.DatabaseConfig{Host: "127.0.0.1", Port: port, User: "root", Database: name})
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
	if _, e = db.Exec("ALTER TABLE team_tasks ADD COLUMN new_executable_token TEXT NULL"); e != nil {
		t.Fatal(e)
	}
	if e = ValidateSchema(context.Background(), db); e == nil {
		t.Fatal("unknown field did not stop capture")
	}
}
