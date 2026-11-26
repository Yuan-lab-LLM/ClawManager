//go:build linux

package systembackuptrial

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"clawreef/internal/config"
	appdb "clawreef/internal/db"
	domain "clawreef/internal/systembackuptrial/records"
	"github.com/go-sql-driver/mysql"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

// Use separate disposable MySQL servers on loopback ports 3306 and 3307,
// read-only mount aliases, and an in-memory Kubernetes resource store.
func TestTrialPrepareMySQLAndRestore(t *testing.T) {
	passwordFile := os.Getenv("TRIAL_PREPARE_MYSQL_PASSWORD_FILE")
	if passwordFile == "" {
		t.Skip("requires two disposable MySQL servers and trial mounts")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	password, err := os.ReadFile(passwordFile)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(password)
	catalog, err := appdb.EmbeddedMigrationCatalog()
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	execSQL := func(db *sql.DB, text string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, text); err != nil {
			t.Fatal(err)
		}
	}
	write := func(name string, content []byte) string {
		t.Helper()
		p := filepath.Join(work, name)
		if err := os.WriteFile(p, content, 0600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	server := func(port int, database string) (*sql.DB, string, string) {
		t.Helper()
		cfg := mysql.NewConfig()
		cfg.User = "root"
		cfg.Passwd = strings.TrimSpace(string(password))
		cfg.Addr = "127.0.0.1:" + strconv.Itoa(port)
		cfg.Net = "tcp"
		pool, err := openTrialMySQL(cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		var uuid string
		if err := pool.QueryRowContext(ctx, "SELECT @@server_uuid").Scan(&uuid); err != nil {
			t.Fatal(err)
		}
		execSQL(pool, "CREATE DATABASE `"+database+"`")
		cfg.DBName = database
		owner, err := openTrialMySQL(cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = owner.Close() })
		return owner, uuid, write(database+".dsn", []byte(cfg.FormatDSN()))
	}
	sourceDB, sourceUUID, sourceOwner := server(3306, "trial_prepare_source")
	targetDB, targetUUID, targetOwner := server(3307, "cm_drill_trial_prepare")
	if sourceUUID == targetUUID {
		t.Fatal("test requires independent MySQL servers")
	}
	if err := appdb.ApplyPinnedMigrations(ctx, sourceDB, appdb.MigrationTarget{ServerUUID: sourceUUID, Database: "trial_prepare_source", CatalogSHA256: catalog.Hash}); err != nil {
		t.Fatal(err)
	}
	execSQL(sourceDB, `INSERT INTO users (id, username, email, password_hash, role) VALUES (20, 'trial_user', 'trial@example.invalid', 'fixture-hash', 'user')`)
	execSQL(sourceDB, `INSERT INTO user_quotas (user_id) VALUES (20)`)
	execSQL(sourceDB, `INSERT INTO instances (id, user_id, name, type, status, cpu_cores, memory_gb, disk_gb, os_type, os_version) VALUES (202,20,'restore-me','openclaw','running',1,2,4,'ubuntu','24.04')`)

	_, profile, _ := trialEnvironmentFixture(t)
	profile.Target.RuntimeSecret = "target-runtime"
	objects := []runtime.Object{}
	for _, p := range []trialPlacement{profile.Source, profile.Target} {
		for _, name := range []string{p.KeySecret, p.SourceReadSecret, p.RestoreWriteSecret, p.RestoreReadSecret, p.RuntimeSecret} {
			if name != "" {
				objects = append(objects, &core.Secret{ObjectMeta: meta.ObjectMeta{
					Name: name, Namespace: p.Namespace, UID: types.UID("fixture-" + name),
					Annotations: map[string]string{trialMaterialOwner: p.Namespace},
				}, Type: core.SecretTypeOpaque})
			}
		}
		for _, name := range []string{p.ArtifactClaim, p.WorkspaceClaim} {
			if name != "" {
				objects = append(objects, &core.PersistentVolumeClaim{ObjectMeta: meta.ObjectMeta{Name: name, Namespace: p.Namespace},
					Spec: core.PersistentVolumeClaimSpec{VolumeName: p.Namespace + "-" + name}, Status: core.PersistentVolumeClaimStatus{Phase: core.ClaimBound}})
			}
		}
	}
	resources := fake.NewSimpleClientset(objects...).CoreV1()
	secret := func(ns, name, field string) []byte {
		t.Helper()
		value, err := resources.Secrets(ns).Get(ctx, name, meta.GetOptions{})
		if err != nil || len(value.Data[field]) == 0 {
			t.Fatalf("missing prepared %s in %s", field, name)
		}
		return value.Data[field]
	}
	settings := map[string]string{
		"SOURCE_UUID": sourceUUID, "RESTORE_UUID": targetUUID, "RESTORE_ISOLATED": "true", "PREPARE_ACCOUNT_HOST": "%",
		"PREPARE_MIGRATION_CATALOG_SHA256": catalog.Hash,
		"PREPARE_DATABASE":                 "trial_prepare_source", "PREPARE_OWNER_DSN_FILE": sourceOwner,
		"WRITE_DIR": os.Getenv("TRIAL_PREPARE_EXPORT"), "READ_DIR": os.Getenv("TRIAL_PREPARE_EXPORT_READ"),
	}
	svc := New(func(name string) string { return settings[strings.TrimPrefix(name, "SYSTEM_BACKUP_TRIAL_")] })
	profile.Side = "source"
	if err := svc.prepareEnvironment(ctx, profile, resources); err != nil {
		t.Fatalf("prepare source: %v", err)
	}
	key := secret(profile.Source.Namespace, profile.Source.KeySecret, "key")
	sourceRead := secret(profile.Source.Namespace, profile.Source.SourceReadSecret, "dsn")
	if err := svc.prepareEnvironment(ctx, profile, resources); err != nil || !bytes.Equal(key, secret(profile.Source.Namespace, profile.Source.KeySecret, "key")) ||
		!bytes.Equal(sourceRead, secret(profile.Source.Namespace, profile.Source.SourceReadSecret, "dsn")) {
		t.Fatalf("repeated source preparation changed materials: %v", err)
	}
	settings["KEY_FILE"] = write("source-key", key)
	settings["SOURCE_DSN_FILE"] = write("source-reader", sourceRead)
	backup := Task{ID: newID()}
	if err := svc.backup(ctx, &backup); err != nil || !backup.Readback || backup.Counts["instances"] != 1 {
		t.Fatalf("backup prepared source at %s: %v", backup.Stage, err)
	}

	profile.Side = "target"
	settings["PREPARE_DATABASE"] = "cm_drill_trial_prepare"
	settings["PREPARE_OWNER_DSN_FILE"] = targetOwner
	settings["RESTORE_NAMESPACE"], settings["RESTORE_STORAGE_CLASS"] = profile.Target.Namespace, profile.Target.InstanceStorageClass
	settings["PREPARE_WORKSPACE_DIR"] = os.Getenv("TRIAL_PREPARE_WORKSPACE")
	settings["WRITE_DIR"] = ""
	settings["READ_DIR"] = os.Getenv("TRIAL_PREPARE_IMPORT")
	if err := svc.prepareEnvironment(ctx, profile, resources); err == nil || !strings.Contains(err.Error(), "read-only import mount") {
		t.Fatalf("prepared a writable import mount: %v", err)
	}
	settings["READ_DIR"] = os.Getenv("TRIAL_PREPARE_IMPORT_READ")
	oldTargetUUID := settings["RESTORE_UUID"]
	settings["RESTORE_UUID"] = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	if err := svc.prepareEnvironment(ctx, profile, resources); err == nil || !strings.Contains(err.Error(), "SQL identity mismatch") {
		t.Fatalf("prepared a database outside the registered UUID: %v", err)
	}
	settings["RESTORE_UUID"] = oldTargetUUID
	if err := svc.prepareEnvironment(ctx, profile, resources); err != nil {
		t.Fatalf("prepare target: %v", err)
	}
	targetKey := secret(profile.Target.Namespace, profile.Target.KeySecret, "key")
	if !bytes.Equal(key, targetKey) {
		t.Fatal("target key differs from source")
	}
	settings["KEY_FILE"] = write("target-key", targetKey)
	for label, name := range map[string]string{"RESTORE": profile.Target.RestoreWriteSecret, "RESTORE_READ": profile.Target.RestoreReadSecret, "RUNTIME": profile.Target.RuntimeSecret} {
		settings[label+"_DSN_FILE"] = write(label, secret(profile.Target.Namespace, name, "dsn"))
	}
	keys := resources.Secrets(profile.Source.Namespace)
	sourceKey, err := keys.Get(ctx, profile.Source.KeySecret, meta.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	sourceKey.Data = nil
	if _, err := keys.Update(ctx, sourceKey, meta.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	settings["SOURCE_DSN_FILE"] = ""
	if err := svc.prepareEnvironment(ctx, profile, resources); err != nil {
		t.Fatalf("prepare target retry without source material: %v", err)
	}
	for label, name := range map[string]string{"RESTORE": profile.Target.RestoreWriteSecret, "RESTORE_READ": profile.Target.RestoreReadSecret, "RUNTIME": profile.Target.RuntimeSecret} {
		before, err := os.ReadFile(settings[label+"_DSN_FILE"])
		if err != nil || !bytes.Equal(before, secret(profile.Target.Namespace, name, "dsn")) {
			t.Fatalf("target %s credential changed on retry", label)
		}
	}
	var tables int
	if err := targetDB.QueryRowContext(ctx, "SELECT count(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='trial_runtime_admission'").Scan(&tables); err != nil || tables != 1 {
		t.Fatalf("target migration did not prepare runtime: %v", err)
	}
	for _, suffix := range []string{".trial", ".receipt"} {
		name := backup.ID + suffix
		data, err := os.ReadFile(filepath.Join(os.Getenv("TRIAL_PREPARE_EXPORT_READ"), name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(os.Getenv("TRIAL_PREPARE_IMPORT"), name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	task := Task{ID: newID(), BackupID: backup.ID}
	if err := svc.restore(ctx, &task); err != nil || !task.Readback || task.Counts["instances"] != 1 {
		t.Fatalf("restore prepared target at %s: %v", task.Stage, err)
	}
	reader, name, err := svc.database("RESTORE_READ")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if err := domain.CheckTrialTargetReader(ctx, reader, domain.DrillTarget{Database: name, ServerUUID: targetUUID, CheckIsolation: func(context.Context) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := reader.QueryRowContext(ctx, "SELECT status FROM instances WHERE id=202 AND user_id=20").Scan(&status); err != nil || status != "stopped" {
		t.Fatal("restored instance did not preserve its owner and stopped state")
	}
	if _, err := reader.ExecContext(ctx, "UPDATE instances SET name='forbidden' WHERE id=202"); err == nil {
		t.Fatal("verification account could change a restored row")
	}
	cfg := &config.Config{}
	cfg.Database.Database = name
	cfg.Database.MaxOpenConns = 2
	cfg.Kubernetes.Common.Namespace, cfg.Runtime.Namespace = profile.Target.Namespace, profile.Target.Namespace
	cfg.Kubernetes.Common.StorageClass = profile.Target.InstanceStorageClass
	cfg.Runtime.WorkspaceRoot = settings["PREPARE_WORKSPACE_DIR"]
	if _, err := svc.InitializeRuntime(ctx, cfg, backup.ID); err != nil {
		t.Fatalf("runtime could not start with prepared materials: %v", err)
	}
	if err := appdb.Close(); err != nil {
		t.Fatal(err)
	}
	profile.Side = "source"
	if _, err := ensureTrialKey(ctx, profile, resources); err == nil {
		t.Fatal("missing material behind a committed key binding was replaced")
	}
	t.Logf("Prepared both database identities and restored artifact %s using generated dedicated accounts", backup.ID)
}
