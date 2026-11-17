package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"clawreef/internal/config"
	appdb "clawreef/internal/db"
	"github.com/go-sql-driver/mysql"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// This test executes the real built CLI and the operator credential generator
// against marker-guarded disposable SQL and an independently named bucket.
func TestHistoryCLIActualBackfillRestoreCredentialAndBackupPause(t *testing.T) {
	port := os.Getenv("TEST_DATABASE_MYSQL_PORT")
	objectPort := os.Getenv("TEST_SYSTEM_BACKUP_MINIO_PORT")
	if port == "" || objectPort == "" {
		t.Skip("disposable MySQL/MinIO fixtures required")
	}
	root, err := sql.Open("mysql", "root@tcp(127.0.0.1:"+port+")/mysql?parseTime=true&multiStatements=true")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	var marker int
	if err = root.QueryRow("SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME='cm_database_test_marker'").Scan(&marker); err != nil || marker != 1 {
		t.Fatal("disposable marker required")
	}
	random := make([]byte, 16)
	if _, err = rand.Read(random); err != nil {
		t.Fatal(err)
	}
	suffix := hex.EncodeToString(random)
	name := "cm_cli_test_" + suffix
	account := "cmcli_" + suffix[:20]
	password := suffix
	if _, err = root.Exec("CREATE DATABASE `" + name + "`;CREATE USER '" + account + "'@'%' IDENTIFIED BY '" + password + "';GRANT ALL ON `" + name + "`.* TO '" + account + "'@'%'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Exec("DROP DATABASE `" + name + "`;DROP USER '" + account + "'@'%'") })
	p, _ := strconv.Atoi(port)
	sess, err := appdb.Initialize(config.DatabaseConfig{Host: "127.0.0.1", Port: p, User: account, Password: password, Database: name})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	pool := sess.Driver().(*sql.DB)
	for _, q := range []string{
		`INSERT INTO system_backup_gate(scope,phase) VALUES('installation','idle')`,
		`INSERT INTO system_backup_gate_admissions(scope) VALUES('installation')`,
		`INSERT INTO model_invocations(trace_id,request_id,user_id,instance_id,provider_type,requested_model,actual_provider_model,traffic_class,status,created_at,completed_at,request_payload) VALUES('cli-trace','cli-one',12,3,'test','test','test','test','completed','2026-01-02','2026-01-02','{"private":"complete bytes"}'),('cli-trace-two','cli-two',12,3,'test','test','test','test','completed','2026-01-03','2026-01-03','{"private":"second body"}')`,
	} {
		if _, err = pool.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	bootstrap := filepath.Join(dir, "restore")
	restoreUser := "cmproof_" + suffix[:20]
	prep := exec.Command("python3", "../../../deployments/k8s/observability/prepare-history-restore.py", "--output", bootstrap, "--host", "127.0.0.1", "--port", port, "--user", restoreUser, "--tls", "false")
	if out, err := prep.CombinedOutput(); err != nil {
		t.Fatalf("credential generation failed: %v %s", err, out)
	}
	generated, err := os.ReadFile(filepath.Join(bootstrap, "bootstrap.sql"))
	if err != nil {
		t.Fatal(err)
	}
	dsn, err := os.ReadFile(filepath.Join(bootstrap, "dsn.private"))
	if err != nil {
		t.Fatal(err)
	}
	targetConfig, err := mysql.ParseDSN(strings.TrimSpace(string(dsn)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = root.Exec(string(generated)); err != nil {
		t.Fatal("generated grants failed", err)
	}
	t.Cleanup(func() { root.Exec("DROP DATABASE `" + targetConfig.DBName + "`;DROP USER '" + restoreUser + "'@'%'") })
	target, err := sql.Open("mysql", targetConfig.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if _, err = target.Query("SELECT original_id FROM `" + name + "`.history_archive_records LIMIT 0"); err == nil {
		t.Fatal("generated account can read source")
	}
	if _, err = target.Exec("CREATE TABLE credential_scope_probe(id INT)"); err != nil {
		t.Fatal("generated CREATE grant missing", err)
	}
	if _, err = target.Exec("DROP TABLE credential_scope_probe"); err == nil {
		t.Fatal("generated account unexpectedly has DROP privilege")
	}
	if _, err = root.Exec("DROP TABLE `" + targetConfig.DBName + "`.credential_scope_probe"); err != nil {
		t.Fatal(err)
	}
	objects, err := minio.New("127.0.0.1:"+objectPort, &minio.Options{Creds: credentials.NewStaticV4(os.Getenv("TEST_SYSTEM_BACKUP_MINIO_USER"), os.Getenv("TEST_SYSTEM_BACKUP_MINIO_PASSWORD"), ""), Secure: false})
	if err != nil {
		t.Fatal(err)
	}
	bucket := "cm-cli-test-" + suffix
	if err = objects.MakeBucket(context.Background(), bucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for o := range objects.ListObjects(context.Background(), bucket, minio.ListObjectsOptions{Recursive: true}) {
			objects.RemoveObject(context.Background(), bucket, o.Key, minio.RemoveObjectOptions{})
		}
		objects.RemoveBucket(context.Background(), bucket)
	})
	executable := filepath.Join(dir, "history-maintenance")
	build := exec.Command(filepath.Join(runtime.GOROOT(), "bin/go"), "build", "-o", executable, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v %s", err, out)
	}
	controlFile := filepath.Join(dir, "control.dsn")
	controlAccount := "cmgate_" + suffix[:20]
	if _, err = root.Exec("CREATE USER '" + controlAccount + "'@'%' IDENTIFIED BY '" + password + "';GRANT SELECT ON `" + name + "`.system_backup_gate TO '" + controlAccount + "'@'%';GRANT SELECT ON `" + name + "`.system_backup_gate_admissions TO '" + controlAccount + "'@'%';GRANT SELECT,INSERT,UPDATE ON `" + name + "`.system_backup_mutation_leases TO '" + controlAccount + "'@'%'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Exec("DROP USER '" + controlAccount + "'@'%'") })
	controlDSN := controlAccount + ":" + password + "@tcp(127.0.0.1:" + port + ")/" + name + "?parseTime=true"
	controlPool, err := sql.Open("mysql", controlDSN)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = controlPool.Exec("CREATE TABLE forbidden_ddl(id INT)"); err == nil {
		controlPool.Close()
		t.Fatal("control credential unexpectedly has CREATE")
	}
	controlPool.Close()
	if err = os.WriteFile(controlFile, []byte(controlDSN), 0600); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "DB_HOST=127.0.0.1", "DB_PORT="+port, "DB_USER="+account, "DB_PASSWORD="+password, "DB_NAME="+name, "OBJECT_STORAGE_ENDPOINT=127.0.0.1:"+objectPort, "OBJECT_STORAGE_ACCESS_KEY="+os.Getenv("TEST_SYSTEM_BACKUP_MINIO_USER"), "OBJECT_STORAGE_SECRET_KEY="+os.Getenv("TEST_SYSTEM_BACKUP_MINIO_PASSWORD"), "OBJECT_STORAGE_BUCKET="+bucket, "OBJECT_STORAGE_BASE_PATH=cli/native", "CLAWMANAGER_OBSERVABILITY_DIR="+filepath.Join(dir, "wal"), "CLAWMANAGER_OBSERVABILITY_KEY="+base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{8}, 32)), "CLAWMANAGER_HISTORY_RESTORE_DSN_FILE="+filepath.Join(bootstrap, "dsn.private"), "SYSTEM_BACKUP_SQL_GATE_SCOPE=installation", "SYSTEM_BACKUP_CONTROL_DSN_FILE="+controlFile)
	cli := func(wantSuccess bool, args ...string) string {
		t.Helper()
		cmd := exec.Command(executable, args...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if (err == nil) != wantSuccess {
			t.Fatalf("CLI %v expected success=%v: %v %s", args, wantSuccess, err, out)
		}
		return string(out)
	}
	backfill := []string{"-mode", "backfill", "-kind", "model_invocations", "-since", "2026-01-01T00:00:00Z", "-until", "2026-02-01T00:00:00Z", "-limit", "1"}
	if out := cli(true, backfill...); !strings.Contains(out, `"processed":1`) {
		t.Fatal("first bounded batch missing", out)
	}
	if _, err = pool.Exec("UPDATE system_backup_gate SET phase='held' WHERE scope='installation'"); err != nil {
		t.Fatal(err)
	}
	cli(false, append(backfill, "-after", "1")...)
	var n int
	if err = pool.QueryRow("SELECT COUNT(*) FROM history_archive_records").Scan(&n); err != nil || n != 1 {
		t.Fatal("backup gate allowed archive mutation", n, err)
	}
	if _, err = pool.Exec("UPDATE system_backup_gate SET phase='idle' WHERE scope='installation'"); err != nil {
		t.Fatal(err)
	}
	if out := cli(true, append(backfill, "-after", "1")...); !strings.Contains(out, `"last_id":2`) {
		t.Fatal("resume did not keep stable cursor", out)
	}
	cli(true, "-mode", "prove", "-limit", "1")
	cli(true, "-mode", "prove", "-limit", "1")
	if err = pool.QueryRow("SELECT COUNT(*) FROM history_archive_records WHERE restore_verified_at IS NOT NULL").Scan(&n); err != nil || n != 2 {
		t.Fatal("actual isolated SQL restore proof missing", n, err)
	}
	cli(true, "-mode", "verify", "-month", "model_invocations_202601")
	cli(true, "-mode", "rollback", "-month", "model_invocations_202601")
	if _, err = pool.Exec("UPDATE history_archive_records SET source_deleted_at=UTC_TIMESTAMP(6) WHERE original_id=1"); err != nil {
		t.Fatal(err)
	}
	cli(false, "-mode", "rollback", "-month", "model_invocations_202601")
	if err = pool.QueryRow("SELECT COUNT(*) FROM system_backup_mutation_leases WHERE released_at IS NULL").Scan(&n); err != nil || n != 0 {
		t.Fatal("CLI left active backup blockers", n, err)
	}
	t.Log(fmt.Sprintf("actual CLI: two bounded backfills, gate pause/resume, two isolated proofs, verify, rollback, post-deletion rollback refusal; generated grants source-denied and DROP-denied; independent control credential CREATE-denied"))
}
