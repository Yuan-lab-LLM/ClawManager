package systembackup

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"clawreef/internal/systembackup/team"
	"github.com/go-sql-driver/mysql"
)

type liveBackupWriter struct {
	bytes.Buffer
	beforeTaskRead func() error
	updated        bool
}

func (w *liveBackupWriter) Write(p []byte) (int, error) {
	if !w.updated && bytes.Contains(p, []byte(`"name":"team_tasks"`)) {
		if err := w.beforeTaskRead(); err != nil {
			return 0, err
		}
		w.updated = true
	}
	return w.Buffer.Write(p)
}

func TestLiveTeamBackupKeepsSnapshotWhileSourceTaskCompletes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	source := fixtureDatabase(t)
	targetName, err := RandomID("cm_restore_")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = source.ExecContext(ctx, "CREATE DATABASE `"+targetName+"`"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := source.Exec("DROP DATABASE `" + targetName + "`"); err != nil {
			t.Error(err)
		}
	})
	config := mysql.Config{User: "root", Net: "tcp", Addr: "127.0.0.1:" + os.Getenv("TEST_SYSTEM_BACKUP_MYSQL_PORT"), DBName: targetName, AllowNativePasswords: true}
	target, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { target.Close() })
	for _, statement := range []string{
		"CREATE TABLE users (id BIGINT PRIMARY KEY) ENGINE=InnoDB",
		"CREATE TABLE team_tasks (id BIGINT PRIMARY KEY,status VARCHAR(32),workflow_state VARCHAR(40),result_json LONGBLOB) ENGINE=InnoDB",
	} {
		if _, err := source.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
		if _, err := target.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := source.ExecContext(ctx, "INSERT INTO users VALUES(1)"); err != nil {
		t.Fatal(err)
	}
	partial := []byte{0, 255, 10, 'p', 'a', 'r', 't'}
	if _, err := source.ExecContext(ctx, "INSERT INTO team_tasks VALUES(1,'running','executing',?)", partial); err != nil {
		t.Fatal(err)
	}
	archive := &liveBackupWriter{beforeTaskRead: func() error {
		_, err := source.ExecContext(ctx, "UPDATE team_tasks SET status='succeeded',workflow_state='completed',result_json='new final result' WHERE id=1")
		return err
	}}
	if _, err := ExportMySQL(ctx, source, archive, []string{"users", "team_tasks"}); err != nil {
		t.Fatal(err)
	}
	if !archive.updated {
		t.Fatal("source task did not progress during capture")
	}
	var captured team.Row
	if _, err := ReadSQLSnapshot(bytes.NewReader(archive.Bytes()), nil, func(table SQLTable, row team.Row) error {
		if table.Name == "team_tasks" {
			captured = team.Clone(row)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if string(captured["status"].Data) != "running" || !bytes.Equal(captured["result_json"].Data, partial) {
		t.Fatal("live backup mixed the later source state into the SQL snapshot")
	}
	var originalState string
	var sourceDB, targetDB string
	if err := source.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&sourceDB); err != nil {
		t.Fatal(err)
	}
	if err := target.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&targetDB); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreMySQL(ctx, target, sourceDB, targetDB, bytes.NewReader(archive.Bytes()), func(table SQLTable, row team.Row) (team.Row, error) {
		if table.Name != "team_tasks" {
			return row, nil
		}
		originalState = string(row["status"].Data)
		change, err := team.Normalize(table.Name, row, time.Now())
		return change.After, err
	}); err != nil {
		t.Fatal(err)
	}
	var status, workflow string
	var result []byte
	if err := target.QueryRowContext(ctx, "SELECT status,workflow_state,result_json FROM team_tasks WHERE id=1").Scan(&status, &workflow, &result); err != nil {
		t.Fatal(err)
	}
	if originalState != "running" || status != "failed" || workflow != "failed" || !bytes.Equal(result, partial) {
		t.Fatal("restored interrupted state or partial result lost")
	}
	if err := source.QueryRowContext(ctx, "SELECT status,result_json FROM team_tasks WHERE id=1").Scan(&status, &result); err != nil {
		t.Fatal(err)
	}
	if status != "succeeded" || strings.TrimSpace(string(result)) != "new final result" {
		t.Fatal("backup/restore changed the live source task")
	}
}
