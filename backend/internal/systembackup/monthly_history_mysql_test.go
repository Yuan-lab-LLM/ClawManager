package systembackup

import (
	"bytes"
	"clawreef/internal/historymaintenance"
	"clawreef/internal/systembackup/team"
	"context"
	"database/sql"
	"github.com/go-sql-driver/mysql"
	"os"
	"strings"
	"testing"
	"time"
)

func historyDDL(t *testing.T, db *sql.DB) {
	t.Helper()
	raw, err := os.ReadFile("../db/migrations/114_add_history_maintenance.sql")
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			lines = append(lines, line)
		}
	}
	for _, q := range strings.Split(strings.Join(lines, "\n"), ";") {
		if strings.TrimSpace(q) != "" {
			if _, err = db.Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	}
}
func TestMonthlyCaptureRejectsUnregisteredAndFreezesCatalog(t *testing.T) {
	ctx := context.Background()
	db := fixtureDatabase(t)
	historyDDL(t, db)
	if _, err := db.Exec("CREATE TABLE model_invocations_202001 LIKE history_month_template"); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverTables(ctx, db); err == nil {
		t.Fatal("rogue monthly table admitted without catalog")
	}
	db.Exec("DROP TABLE model_invocations_202001")
	svc := historymaintenance.New(db, nil, nil)
	if err := svc.PrepareMonths(ctx, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	names, err := DiscoverTables(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) < 18 {
		t.Fatal("future months missing")
	}
	month := "model_invocations_202611"
	db.Exec("INSERT INTO "+month+"(original_id,owner_id,created_at,trace_id,instance_id,object_ref,payload_sha256,payload_bytes) VALUES(7,12,'2026-11-02','trace',3,'{}',?,2)", strings.Repeat("a", 64))
	var archive bytes.Buffer
	if _, err = ExportMySQL(ctx, db, &archive, []string{month, "history_month_catalog", "history_month_template"}); err != nil {
		t.Fatal(err)
	}
	// A different database/capture cannot alter this frozen snapshot's policies.
	other := fixtureDatabase(t)
	historyDDL(t, other)
	if _, err = DiscoverTables(ctx, other); err != nil {
		t.Fatal(err)
	}
	if _, err = ReadSQLSnapshot(bytes.NewReader(archive.Bytes()), nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("ALTER TABLE " + month + " ADD COLUMN executable_token TEXT"); err != nil {
		t.Fatal(err)
	}
	if _, err = DiscoverTables(ctx, db); err == nil {
		t.Fatal("changed monthly schema admitted")
	}
	if _, err = ReadSQLSnapshot(bytes.NewReader(archive.Bytes()), nil, nil); err != nil {
		t.Fatal("frozen archive depends on live schema", err)
	}
	targetName, _ := RandomID("cm_restore_")
	if _, err = db.Exec("CREATE DATABASE `" + targetName + "`"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec("DROP DATABASE `" + targetName + "`") })
	cfg := mysql.Config{User: "root", Net: "tcp", Addr: "127.0.0.1:" + os.Getenv("TEST_SYSTEM_BACKUP_MYSQL_PORT"), DBName: targetName, ParseTime: true, AllowNativePasswords: true}
	target, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { target.Close() })
	historyDDL(t, target)
	if err = prepareMonthlyRestoreSchema(ctx, target, bytes.NewReader(archive.Bytes())); err != nil {
		t.Fatal(err)
	}
	if _, err = RestoreMySQL(ctx, target, "source", targetName, bytes.NewReader(archive.Bytes()), nil); err != nil {
		t.Fatal(err)
	}
	var restored int
	if err = target.QueryRow("SELECT COUNT(*) FROM " + month + " WHERE original_id=7 AND owner_id=12").Scan(&restored); err != nil || restored != 1 {
		t.Fatalf("stable ID restore %d %v", restored, err)
	}
	var normalized bytes.Buffer
	if _, err = NormalizeSQLSnapshot(ctx, bytes.NewReader(archive.Bytes()), &normalized, time.Now().UTC(), func(SQLTable, team.Change) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err = ReadSQLSnapshot(bytes.NewReader(normalized.Bytes()), nil, nil); err != nil {
		t.Fatal("normalization lost frozen monthly manifest", err)
	}
}

func TestRestoreInvalidatesMaintenanceAndProjectionAuthority(t *testing.T) {
	for _, tc := range []struct{ table, column, before, after string }{{"team_projection_watermarks", "baseline_verified", "1", "0"}, {"history_replay_closures", "recovery_closed", "1", "0"}, {"history_month_catalog", "state", "active", "prepared"}, {"history_archive_records", "restore_receipt", "source-cluster-proof", ""}} {
		table := SQLTable{Name: tc.table, Columns: []SQLColumn{{Name: tc.column, Type: "varchar(255)", Nullable: "NO"}}, PrimaryKey: []string{tc.column}}
		change, err := NormalizeDataRow(table, team.Row{tc.column: team.Value(tc.before)}, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		if string(change.After[tc.column].Data) != tc.after {
			t.Fatalf("%s.%s retained stale authority", tc.table, tc.column)
		}
	}
}
