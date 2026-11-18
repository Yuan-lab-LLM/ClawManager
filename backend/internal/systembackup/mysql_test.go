package systembackup

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"clawreef/internal/systembackup/team"
	"github.com/go-sql-driver/mysql"
)

func TestSQLSnapshotRejectsTruncationControlTablesAndCycles(t *testing.T) {
	for _, input := range []string{"", `{"kind":"mysql_rows_v1"}` + "\n", `{"kind":"mysql_rows_v1"}` + "\n" + `{"kind":"table","table":{"name":"schema_migrations","columns":[{"name":"filename"}]}}` + "\n"} {
		if _, err := ReadSQLSnapshot(strings.NewReader(input), nil, nil); err == nil {
			t.Fatal("invalid SQL payload accepted")
		}
	}
	if _, err := DependencyOrder([]string{"a", "b"}, map[string][]string{"a": {"b"}, "b": {"a"}}); err == nil {
		t.Fatal("dependency cycle accepted")
	}
	order, err := DependencyOrder([]string{"team_tasks", "teams", "users"}, map[string][]string{"teams": {"users"}, "team_tasks": {"teams"}})
	if err != nil || !reflect.DeepEqual(order, []string{"users", "teams", "team_tasks"}) {
		t.Fatal(order, err)
	}
}

func TestMySQLCaptureRestorePreservesBytesAndInterruptState(t *testing.T) {
	port := os.Getenv("TEST_SYSTEM_BACKUP_MYSQL_PORT")
	if port == "" {
		t.Skip("requires disposable localhost MySQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	rootConfig := mysql.Config{User: "root", Net: "tcp", Addr: "127.0.0.1:" + port, DBName: "mysql", AllowNativePasswords: true}
	root, err := sql.Open("mysql", rootConfig.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	var random [16]byte
	_, _ = rand.Read(random[:])
	suffix := hex.EncodeToString(random[:])
	sourceName := "cm_capture_" + suffix
	targetName := "cm_restore_" + suffix
	for _, name := range []string{sourceName, targetName} {
		if _, err = root.ExecContext(ctx, "CREATE DATABASE `"+name+"`"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = root.Exec("DROP DATABASE `" + name + "`") })
	}
	// Keep the root connection alive until cleanup has removed only these two
	// freshly-created databases, never an existing installation database.
	open := func(name string) *sql.DB {
		c := rootConfig
		c.DBName = name
		d, e := sql.Open("mysql", c.FormatDSN())
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { d.Close() })
		return d
	}
	source, target := open(sourceName), open(targetName)
	ddl := "CREATE TABLE team_tasks (id BIGINT PRIMARY KEY,status VARCHAR(30) NOT NULL,workflow_state VARCHAR(40) NOT NULL,ledger_version BIGINT NOT NULL,result_json LONGBLOB,error_message TEXT NULL,finished_at DATETIME(6) NULL,empty_text VARCHAR(10) NOT NULL) ENGINE=InnoDB"
	for _, d := range []*sql.DB{source, target} {
		if _, err = d.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	payload := []byte{0, 255, 128, 10, 13}
	if _, err = source.ExecContext(ctx, "INSERT INTO team_tasks VALUES (?,?,?,?,?,?,NULL,'')", 7, "running", "executing", int64(9007199254740993), payload, "keep diagnostic"); err != nil {
		t.Fatal(err)
	}
	var captured bytes.Buffer
	before, err := ExportMySQL(ctx, source, &captured, []string{"team_tasks"})
	if err != nil {
		t.Fatal(err)
	}
	var changes []team.Change
	_, err = RestoreMySQL(ctx, target, sourceName, targetName, bytes.NewReader(captured.Bytes()), func(table SQLTable, row team.Row) (team.Row, error) {
		c, e := team.Normalize(table.Name, row, time.Now().UTC())
		if e == nil {
			changes = append(changes, c)
		}
		return c.After, e
	})
	if err != nil {
		t.Fatal(err)
	}
	var status, diagnostic string
	var actual []byte
	var ledger int64
	if err = target.QueryRowContext(ctx, "SELECT status,result_json,error_message,ledger_version FROM team_tasks WHERE id=7").Scan(&status, &actual, &diagnostic, &ledger); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || !bytes.Equal(payload, actual) || diagnostic != "keep diagnostic" || ledger != 9007199254740993 || len(changes) != 1 {
		t.Fatal("lossy normalization")
	}
	var sourceAfter bytes.Buffer
	after, err := ExportMySQL(ctx, source, &sourceAfter, []string{"team_tasks"})
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("source changed")
	}
	if _, err = RestoreMySQL(ctx, target, sourceName, targetName, bytes.NewReader(captured.Bytes()), nil); err == nil {
		t.Fatal("overwrote populated target")
	}
	if _, err = ExportMySQL(ctx, source, &bytes.Buffer{}, []string{"system_backup_tasks"}); err == nil {
		t.Fatal("exported control table")
	}
}
