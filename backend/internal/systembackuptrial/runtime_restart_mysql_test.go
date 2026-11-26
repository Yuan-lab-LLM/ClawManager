//go:build linux

package systembackuptrial

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawreef/internal/config"
	appdb "clawreef/internal/db"

	"github.com/go-sql-driver/mysql"
)

// The two phases run beside separate disposable MySQL servers. They share only
// the artifact directory and key, and the source can be removed between phases.
func TestTrialRuntimeRestartMySQL(t *testing.T) {
	phase := os.Getenv("TRIAL_RUNTIME_MYSQL_TEST_PHASE")
	if phase != "source" && phase != "target" {
		t.Skip("requires the disposable Linux trial runtime fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	password, err := os.ReadFile(os.Getenv("TRIAL_RUNTIME_MYSQL_TEST_PASSWORD_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(password)
	const targetDatabase = "cm_drill_trial_restart"
	database := targetDatabase
	if phase == "source" {
		database = "trial_restart_source"
	}
	ownerDSN := mysql.NewConfig()
	ownerDSN.User, ownerDSN.Passwd = "root", strings.TrimSpace(string(password))
	ownerDSN.Net, ownerDSN.Addr = "tcp", "127.0.0.1:3306"
	server, err := sql.Open("mysql", ownerDSN.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	var uuid string
	if err := server.QueryRowContext(ctx, "SELECT @@server_uuid").Scan(&uuid); err != nil {
		t.Fatal(err)
	}
	execSQL := func(db *sql.DB, statement string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	execSQL(server, "CREATE DATABASE `"+database+"`")
	ownerDSN.DBName = database
	owner, err := sql.Open("mysql", ownerDSN.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	catalog, err := appdb.EmbeddedMigrationCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if err := appdb.ApplyPinnedMigrations(ctx, owner, appdb.MigrationTarget{
		ServerUUID: uuid, Database: database, CatalogSHA256: catalog.Hash, PrepareTrialRuntime: phase == "target",
	}); err != nil {
		t.Fatal(err)
	}
	const fixturePassword = "synthetic-Trial-Restart!234"
	execSQL(server, "SET GLOBAL partial_revokes = ON")
	account := func(setting string, grants ...string) {
		t.Helper()
		user := "trial_" + strings.ToLower(setting)
		identity := "'" + user + "'@'%'"
		execSQL(server, "CREATE USER "+identity+" IDENTIFIED BY '"+fixturePassword+"'")
		for _, grant := range grants {
			execSQL(server, "GRANT "+grant+" TO "+identity)
		}
		dsn := *ownerDSN
		dsn.User, dsn.Passwd = user, fixturePassword
		file := filepath.Join(t.TempDir(), "dsn")
		if err := os.WriteFile(file, []byte(dsn.FormatDSN()), 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("SYSTEM_BACKUP_TRIAL_"+setting+"_DSN_FILE", file)
	}
	service := New(os.Getenv)
	if phase == "source" {
		var grants []string
		for _, table := range []string{"users", "user_quotas", "instances", "openclaw_injection_snapshots", "instance_config_revisions", "instance_desired_state"} {
			grants = append(grants, "SELECT ON `"+database+"`.`"+table+"`")
		}
		account("SOURCE", grants...)
		t.Setenv("SYSTEM_BACKUP_TRIAL_SOURCE_UUID", uuid)
		execSQL(owner, "INSERT INTO users (id, username, email, password_hash, role) VALUES (20, 'trial_owner', 'trial@example.invalid', 'fixture-hash', 'user')")
		execSQL(owner, "INSERT INTO user_quotas (user_id) VALUES (20)")
		execSQL(owner, "INSERT INTO instances (id, user_id, name, type, status, cpu_cores, memory_gb, disk_gb, os_type, os_version) VALUES (202, 20, 'trial-restored', 'openclaw', 'running', 1, 2, 4, 'ubuntu', '24.04')")
		backup := Task{ID: newID()}
		if err := service.backup(ctx, &backup); err != nil || !backup.Readback || backup.Counts["instances"] != 1 {
			t.Fatalf("source backup at %s: %v", backup.Stage, err)
		}
		t.Logf("source produced independently verified backup %s", backup.ID)
		return
	}
	account("RESTORE", "SELECT, INSERT, UPDATE, DELETE ON `"+database+"`.*")
	account("RESTORE_READ", "SELECT ON `"+database+"`.*")
	account("RUNTIME", "SELECT, INSERT, UPDATE, DELETE ON `"+database+"`.*")
	t.Setenv("SYSTEM_BACKUP_TRIAL_RESTORE_UUID", uuid)
	t.Setenv("SYSTEM_BACKUP_TRIAL_RESTORE_ISOLATED", "true")
	const namespace, class = "cm-drill-restart", "isolated"
	t.Setenv("SYSTEM_BACKUP_TRIAL_RESTORE_NAMESPACE", namespace)
	t.Setenv("SYSTEM_BACKUP_TRIAL_RESTORE_STORAGE_CLASS", class)
	files, err := filepath.Glob(filepath.Join(service.value("READ_DIR"), "*.receipt"))
	if err != nil || len(files) != 1 {
		t.Fatalf("expected one transferred artifact: %v", err)
	}
	id := strings.TrimSuffix(filepath.Base(files[0]), ".receipt")
	restore := Task{ID: newID(), BackupID: id}
	if err := service.restore(ctx, &restore); err != nil || !restore.Readback {
		t.Fatalf("target restore at %s: %v", restore.Stage, err)
	}
	cfg := &config.Config{}
	cfg.Database.Database = targetDatabase
	cfg.Database.MaxOpenConns = 3
	cfg.Kubernetes.Common.Namespace, cfg.Runtime.Namespace = namespace, namespace
	cfg.Kubernetes.Common.StorageClass = class
	cfg.Runtime.WorkspaceRoot = os.Getenv("TRIAL_RUNTIME_MYSQL_TEST_WORKSPACE")
	marker := filepath.Join(cfg.Runtime.WorkspaceRoot, runtimeAdmissionFile)
	start := func(use func(*sql.DB)) error {
		session, err := service.InitializeRuntime(ctx, cfg, id)
		if err != nil {
			return err
		}
		if use != nil {
			use(session.Driver().(*sql.DB))
		}
		return appdb.Close()
	}
	execSQL(owner, "UPDATE instances SET name='wrong-value', updated_at=updated_at WHERE id=202")
	if err := start(nil); err == nil {
		t.Fatal("unverified first boot admitted a changed instance")
	}
	if _, err := os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("first boot published a marker before verification")
	}
	execSQL(owner, "UPDATE instances SET name='trial-restored', updated_at=updated_at WHERE id=202")
	if err := start(nil); err != nil {
		t.Fatalf("verified first boot: %v", err)
	}
	// An interruption after committing only the workspace proof can be retried
	// while the target still matches the artifact.
	execSQL(owner, "DELETE FROM trial_runtime_admission WHERE id=1")
	if err := start(func(runtime *sql.DB) {
		execSQL(runtime, "UPDATE instances SET status='running', name='changed-after-admission' WHERE id=202")
	}); err != nil {
		t.Fatalf("completed an interrupted admission: %v", err)
	}
	if err := start(func(runtime *sql.DB) {
		var status, name string
		if err := runtime.QueryRowContext(ctx, "SELECT status, name FROM instances WHERE id=202").Scan(&status, &name); err != nil ||
			status != "running" || name != "changed-after-admission" {
			t.Fatalf("restarted session lost business writes: %q, %q, %v", status, name, err)
		}
	}); err != nil {
		t.Fatalf("restart after business writes: %v", err)
	}
	if err := os.Rename(marker, marker+".hidden"); err != nil {
		t.Fatal(err)
	}
	if err := start(nil); !errors.Is(err, errRuntimeAdmission) {
		t.Fatalf("restart with missing workspace marker = %v", err)
	}
	if err := os.Rename(marker+".hidden", marker); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SYSTEM_BACKUP_TRIAL_RESTORE_NAMESPACE", "cm-drill-wrong-target")
	cfg.Runtime.Namespace = "cm-drill-wrong-target"
	cfg.Kubernetes.Common.Namespace = "cm-drill-wrong-target"
	if err := start(nil); !errors.Is(err, errRuntimeAdmission) {
		t.Fatalf("restart with changed namespace = %v", err)
	}
	t.Setenv("SYSTEM_BACKUP_TRIAL_RESTORE_NAMESPACE", namespace)
	cfg.Runtime.Namespace, cfg.Kubernetes.Common.Namespace = namespace, namespace
	execSQL(owner, "DELETE FROM trial_runtime_admission WHERE id=1")
	if err := start(nil); err == nil {
		t.Fatal("changed target was admitted without the database proof")
	}
	t.Log("first boot verified, business writes survived restart, and missing or mismatched proofs failed closed")
}
