package systembackup

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	appdb "clawreef/internal/db"
	"clawreef/internal/systembackup/team"
	"github.com/go-sql-driver/mysql"
)

func isolatedSchemaDB(t *testing.T, admin *sql.DB) (*sql.DB, string) {
	t.Helper()
	name, e := RandomID("cm_restore_")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, e := admin.Exec("DROP DATABASE `" + name + "`"); e != nil {
			t.Error(e)
		}
	})
	c := mysql.Config{User: "root", Net: "tcp", Addr: "127.0.0.1:" + os.Getenv("TEST_SYSTEM_BACKUP_MYSQL_PORT"), DBName: name, AllowNativePasswords: true, ParseTime: true}
	d, e := sql.Open("mysql", c.FormatDSN())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { d.Close() })
	if e = appdb.PrepareIsolatedRestoreSchema(context.Background(), d); e != nil {
		t.Fatal(e)
	}
	return d, name
}

func TestFullSchemaTeamRestorePreservesScopeHistoryAndInterruptedResults(t *testing.T) {
	source := fullSchemaDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	statements := []string{
		`INSERT INTO users(id,username,email,password_hash,role) VALUES(101,'provider-a','a@test.invalid','not-a-password','user'),(102,'provider-b','b@test.invalid','not-a-password','user')`,
		`INSERT INTO teams(id,user_id,name,status,resource_scope,owner,redis_url_secret_name) VALUES(1001,101,'cm','running','cm',NULL,'source-secret'),(1002,101,'owner-x','running','northbound','owner-x','source-secret'),(1003,101,'owner-y','creating','northbound','owner-y','source-secret'),(1004,102,'owner-x-other-provider','running','northbound','owner-x','source-secret')`,
		`INSERT INTO team_members(id,team_id,user_id,member_key,display_name,role,status) VALUES(2001,1001,101,'leader','Leader','leader','busy'),(2002,1002,101,'leader','Leader','leader','idle'),(2003,1003,101,'worker','Worker','worker','creating'),(2004,1004,102,'leader','Leader','leader','busy')`,
		`INSERT INTO team_tasks(id,team_id,target_member_id,message_id,status,payload_json,result_json,workflow_state) VALUES(3001,1001,2001,'cm-1','running','{"task":"保存数据"}','{"partial":"已生成结果"}','executing'),(3002,1002,2002,'owner-1','succeeded','{}','{"answer":"完成结果"}','completed'),(3003,1004,2004,'other-provider-1','waiting','{}','{"partial":"等待人工"}','awaiting_phase_results')`,
		`UPDATE team_members SET current_task_id=3001,availability='busy',runtime_task_id='3001' WHERE id=2001`,
		`INSERT INTO team_events(team_id,member_id,task_id,event_type,payload_json) VALUES(1001,2001,3001,'assistant_message','{"text":"中断前的完整消息"}')`,
		`INSERT INTO owner_team_provision_steps(team_id,step_key,operation_id,status) VALUES(1003,'secret','half-created-operation','done'),(1003,'member','half-created-operation','started')`,
		`INSERT INTO northbound_operations(operation_id,user_id,session_id,operation_type,idempotency_key_hash,request_hash,request_payload,status,available_at,attempt_count,created_at,updated_at,owner,team_id,owner_request_key) VALUES('half-created-operation',101,'historical-session','owner_team_teams_create',REPEAT('a',64),REPEAT('b',64),'{"owner":"owner-y","team_id":1003}','processing',UTC_TIMESTAMP(),1,UTC_TIMESTAMP(),UTC_TIMESTAMP(),'owner-y',1003,REPEAT('c',64))`,
	}
	for _, statement := range statements {
		if _, e := source.ExecContext(ctx, statement); e != nil {
			t.Fatal(e)
		}
	}
	if e := ValidateSchema(ctx, source); e != nil {
		t.Fatal(e)
	}
	tables, e := SnapshotBusinessTables(ctx, source)
	if e != nil {
		t.Fatal(e)
	}
	var original bytes.Buffer
	capture, e := ExportMySQL(ctx, source, &original, tables)
	if e != nil {
		t.Fatal(e)
	}
	var normalized bytes.Buffer
	var evidence []team.Change
	at := time.Date(2026, 10, 23, 14, 0, 0, 123456000, time.UTC)
	normalizedResult, e := NormalizeSQLSnapshot(ctx, bytes.NewReader(original.Bytes()), &normalized, at, func(_ SQLTable, c team.Change) error {
		if c.Quarantined {
			evidence = append(evidence, c)
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	if len(capture.Tables) != 75 || len(normalizedResult.Tables) != 75 || len(evidence) < 10 {
		t.Fatal("incomplete full capture/normalization")
	}
	target, targetName := isolatedSchemaDB(t, source)
	var sourceName string
	source.QueryRow("SELECT DATABASE()").Scan(&sourceName)
	if _, e = RestoreMySQL(ctx, target, sourceName, targetName, bytes.NewReader(normalized.Bytes()), nil); e != nil {
		t.Fatal(e)
	}
	verifier := selectOnlyVerifier(t, source, targetName)
	if _, e = VerifyMySQL(ctx, verifier, bytes.NewReader(normalized.Bytes()), 100000); e != nil {
		t.Fatal(e)
	}
	var status, scope, owner, result string
	var provider int
	if e = target.QueryRow("SELECT status,result_json FROM team_tasks WHERE id=3001").Scan(&status, &result); e != nil || status != "failed" || !strings.Contains(result, "已生成结果") {
		t.Fatal(status, result, e)
	}
	if e = target.QueryRow("SELECT status,result_json FROM team_tasks WHERE id=3002").Scan(&status, &result); e != nil || status != "succeeded" || !strings.Contains(result, "完成结果") {
		t.Fatal(status, result, e)
	}
	if e = target.QueryRow("SELECT user_id,resource_scope,owner FROM teams WHERE id=1004").Scan(&provider, &scope, &owner); e != nil || provider != 102 || scope != "northbound" || owner != "owner-x" {
		t.Fatal(provider, scope, owner, e)
	}
	if e = target.QueryRow("SELECT status FROM owner_team_provision_steps WHERE team_id=1003 AND step_key='secret'").Scan(&status); e != nil || status != "done" {
		t.Fatal("lost completed step", status, e)
	}
	if e = target.QueryRow("SELECT status FROM owner_team_provision_steps WHERE team_id=1003 AND step_key='member'").Scan(&status); e != nil || status != "failed" {
		t.Fatal("live unfinished step", status, e)
	}
	if e = source.QueryRow("SELECT status FROM team_tasks WHERE id=3001").Scan(&status); e != nil || status != "running" {
		t.Fatal("source mutated", status, e)
	}
	var controls int
	if e = target.QueryRow("SELECT COUNT(*) FROM system_backup_tasks").Scan(&controls); e != nil || controls != 0 {
		t.Fatal("source control plane replayed", controls, e)
	}
	// A populated target is never reused implicitly, even after a failed retry.
	if e = appdb.PrepareIsolatedRestoreSchema(ctx, target); e == nil {
		t.Fatal("schema bootstrap accepted existing database")
	}
}

func selectOnlyVerifier(t *testing.T, admin *sql.DB, database string) *sql.DB {
	v, _ := selectOnlyVerifierWithDSN(t, admin, database)
	return v
}
func selectOnlyVerifierWithDSN(t *testing.T, admin *sql.DB, database string) (*sql.DB, string) {
	t.Helper()
	user, e := RandomID("sbv_")
	if e != nil {
		t.Fatal(e)
	}
	user = user[:28]
	var secret [32]byte
	if _, e = rand.Read(secret[:]); e != nil {
		t.Fatal(e)
	}
	password := hex.EncodeToString(secret[:])
	clear(secret[:])
	if _, e = admin.Exec("CREATE USER '" + user + "'@'%' IDENTIFIED BY '" + password + "'"); e != nil {
		t.Fatal("create isolated verifier account:", e)
	}
	t.Cleanup(func() {
		if _, e := admin.Exec("DROP USER '" + user + "'@'%'"); e != nil {
			t.Error(e)
		}
	})
	if _, e = admin.Exec("GRANT SELECT ON `" + database + "`.* TO '" + user + "'@'%'"); e != nil {
		t.Fatal(e)
	}
	c := mysql.Config{User: user, Passwd: password, Net: "tcp", Addr: "127.0.0.1:" + os.Getenv("TEST_SYSTEM_BACKUP_MYSQL_PORT"), DBName: database, AllowNativePasswords: true, ParseTime: true}
	v, e := sql.Open("mysql", c.FormatDSN())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { v.Close() })
	for _, statement := range []string{"UPDATE team_tasks SET status=status LIMIT 0", "CREATE TABLE backup_forbidden (id INT)", "DELETE FROM team_events WHERE 1=0"} {
		_, e = v.Exec(statement)
		var serverError *mysql.MySQLError
		if !errors.As(e, &serverError) || (serverError.Number != 1142 && serverError.Number != 1143 && serverError.Number != 1227) {
			t.Fatal("verifier write privilege was not denied", e)
		}
	}
	return v, c.FormatDSN()
}
