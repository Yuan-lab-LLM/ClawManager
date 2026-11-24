package systembackuptrial

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawreef/internal/config"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	staging "clawreef/internal/systembackuptrial/envelope"
	domain "clawreef/internal/systembackuptrial/records"
	"clawreef/internal/utils"

	"github.com/gin-gonic/gin"
	mysqldriver "github.com/go-sql-driver/mysql"
	mysqladapter "github.com/upper/db/v4/adapter/mysql"
	"golang.org/x/crypto/bcrypt"
)

type trialTestUsers struct{ repository.UserRepository }

func (trialTestUsers) GetByID(id int) (*models.User, error) {
	role := "user"
	if id != 2 {
		role = "admin"
	}
	return &models.User{ID: id, Role: role, IsActive: id != 3}, nil
}

func TestTrialAdminAdmissionWithoutFormalConfiguration(t *testing.T) {
	const secret = "trial-test-jwt-secret-only"
	t.Setenv("JWT_SECRET", secret)
	r := gin.New()
	New(func(string) string { return "" }).Register(r.Group("/api/v1"), trialTestUsers{})
	for _, tc := range []struct {
		name string
		path string
		user int
		want int
	}{
		{"anonymous", "/backups", 0, http.StatusUnauthorized},
		{"ordinary user", "/backups", 2, http.StatusForbidden},
		{"inactive admin", "/backups", 3, http.StatusForbidden},
		{"admin and missing trial material", "/backups", 1, http.StatusServiceUnavailable},
		{"anonymous preview", "/backups/" + strings.Repeat("a", 32) + "/preflight", 0, http.StatusUnauthorized},
		{"ordinary user preview", "/backups/" + strings.Repeat("a", 32) + "/preflight", 2, http.StatusForbidden},
		{"invalid backup ID", "/backups/not-a-backup/preflight", 1, http.StatusBadRequest},
		{"admin preview without target", "/backups/" + strings.Repeat("a", 32) + "/preflight", 1, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system-backup/trial"+tc.path, nil)
			if tc.user != 0 {
				token, err := utils.GenerateToken(utils.TokenClaims{UserID: tc.user, TokenType: "access"}, secret, time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Authorization", "Bearer "+token)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			if tc.want == http.StatusServiceUnavailable && !strings.Contains(rec.Body.String(), "source_credential") {
				t.Fatal("trial readiness did not report independently missing source credential")
			}
			if tc.want == http.StatusOK && (rec.Header().Get("Cache-Control") != "no-store" || !strings.Contains(rec.Body.String(), "restore_isolation")) {
				t.Fatal("preflight should report missing target isolation without caching the result")
			}
		})
	}
}

func TestTrialRestoreAdmissionWithOnlyTargetConfiguration(t *testing.T) {
	root := t.TempDir()
	reader := filepath.Join(root, "imported")
	if err := os.Mkdir(reader, 0700); err != nil {
		t.Fatal(err)
	}
	settings := map[string]string{
		"READ_DIR": reader, "RESTORE_ISOLATED": "true", "RESTORE_UUID": "target-uuid",
		"RESTORE_NAMESPACE": "cm-drill-fixture", "RESTORE_STORAGE_CLASS": "target-class",
	}
	for setting, contents := range map[string]string{
		"KEY_FILE":              strings.Repeat("ab", 32),
		"RESTORE_DSN_FILE":      "writer:test@tcp(127.0.0.1:1)/cm_drill_trial_fixture",
		"RESTORE_READ_DSN_FILE": "reader:test@tcp(127.0.0.1:1)/cm_drill_trial_fixture",
	} {
		name := filepath.Join(root, setting)
		if err := os.WriteFile(name, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		settings[setting] = name
	}
	service := New(func(name string) string { return settings[strings.TrimPrefix(name, "SYSTEM_BACKUP_TRIAL_")] })
	ready := service.readiness(context.Background())
	if got := strings.Join(ready.RestoreMissing, ","); len(ready.Missing) == 0 || got != "restore_artifact_writable,restore_writer_database,restore_reader_database" {
		t.Fatalf("expected the writable import and both unreachable credentials to be reported, got %+v", ready)
	}
	if got := service.restoreSettings(); len(got) != 0 {
		t.Fatalf("expected valid restore-only settings, got %v", got)
	}
	task := Task{ID: newID(), BackupID: newID()}
	if err := service.restore(context.Background(), &task); !errors.Is(err, os.ErrNotExist) || task.Stage != "readback" {
		t.Fatalf("expected independent artifact lookup on the target, got stage=%q err=%v", task.Stage, err)
	}
	delete(settings, "READ_DIR")
	if got := service.restoreSettings(); len(got) != 1 || got[0] != "restore_artifact_mount" {
		t.Fatalf("missing target artifact mount did not block restore: %v", got)
	}
}

func TestTrialRuntimeRejectsFormalSchemaAndMismatchedPlacement(t *testing.T) {
	t.Setenv("SYSTEM_BACKUP_SQL_GATE_SCOPE", "")
	values := map[string]string{
		"SYSTEM_BACKUP_TRIAL_RESTORE_ISOLATED":      "true",
		"SYSTEM_BACKUP_TRIAL_RESTORE_NAMESPACE":     "cm-drill-fixture",
		"SYSTEM_BACKUP_TRIAL_RESTORE_STORAGE_CLASS": "isolated",
	}
	service := New(func(name string) string { return values[name] })
	cfg := &config.Config{}
	cfg.Database.Database = "cm_drill_formal"
	cfg.Kubernetes.Common.Namespace = "cm-drill-fixture"
	cfg.Kubernetes.Common.StorageClass = "isolated"
	cfg.Runtime.Namespace = "cm-drill-fixture"
	id := strings.Repeat("a", 32)
	if _, err := service.InitializeRuntime(context.Background(), cfg, id); !errors.Is(err, domain.ErrIsolation) {
		t.Fatalf("formal drill reached trial runtime credential access: %v", err)
	}
	cfg.Database.Database = "cm_drill_trial_fixture"
	cfg.Runtime.Namespace = "default"
	if _, err := service.InitializeRuntime(context.Background(), cfg, id); !errors.Is(err, domain.ErrIsolation) {
		t.Fatalf("mismatched placement reached trial runtime credential access: %v", err)
	}
}

func TestTrialRuntimeIdentityRequiresActiveUser(t *testing.T) {
	const secret = "trial-runtime-identity-test-only"
	t.Setenv("JWT_SECRET", secret)
	router := gin.New()
	backupID := strings.Repeat("a", 32)
	RegisterRuntimeIdentity(router.Group("/api/v1"), trialTestUsers{}, backupID, "cm-drill-fixture")
	for _, tc := range []struct{ user, want int }{
		{0, http.StatusUnauthorized},
		{2, http.StatusOK},
		{3, http.StatusForbidden},
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/trial-runtime", nil)
		if tc.user != 0 {
			token, err := utils.GenerateToken(utils.TokenClaims{UserID: tc.user, TokenType: "access"}, secret, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("user %d: status %d, want %d", tc.user, rec.Code, tc.want)
		}
		if tc.want == http.StatusOK &&
			(rec.Header().Get("Cache-Control") != "no-store" ||
				!strings.Contains(rec.Body.String(), backupID) || !strings.Contains(rec.Body.String(), "cm-drill-fixture")) {
			t.Fatal("trial identity response missing the admitted backup or namespace")
		}
	}
}

func TestTrialRuntimePinsLeaderLeaseToDrillNamespace(t *testing.T) {
	cfg := &config.Config{}
	cfg.Kubernetes.Common.Namespace = "cm-drill-fixture"
	cfg.Kubernetes.Common.StorageClass = "isolated"
	cfg.Runtime.Namespace = "cm-drill-fixture"
	cfg.LeaderElection.Enabled = true
	cfg.LeaderElection.Namespace = "clawmanager-system"
	placement, err := prepareRuntimePlacement(cfg, "cm-drill-fixture", "isolated")
	if err != nil {
		t.Fatal(err)
	}
	if placement.Namespace != "cm-drill-fixture" || cfg.LeaderElection.Namespace != placement.Namespace {
		t.Fatal("the sample scheduler would acquire a lease outside the drill namespace")
	}
}

func TestTrialStorageReopensAndChecksCiphertextAndRejectsWritableReadPath(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	writer, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	reader, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	store := storage{writer, reader}
	master := bytes.Repeat([]byte{1}, 32)
	if err := store.probe(ctx, master); err == nil {
		t.Fatal("writable read path admitted")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("readiness probe did not remove its files")
	}

	id := newID()
	b := binding(id)
	key := taskKey(master, id)
	receipt, err := staging.SealFile(ctx, writer, b.Path, key, b, func(w io.Writer) error {
		_, err := io.WriteString(w, "trial configuration fixture")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	defer removeOwned(writer, receipt.Name)
	var dst bytes.Buffer
	if err := store.open(ctx, receipt, key, b, &dst, 128); err != nil || dst.String() != "trial configuration fixture" {
		t.Fatalf("independent reopen failed: %v", err)
	}
	if err := store.open(ctx, receipt, taskKey(master, newID()), b, io.Discard, 128); err == nil {
		t.Fatal("accepted different task key")
	}
	if err := writer.Chmod(receipt.Name, 0600); err != nil {
		t.Fatal(err)
	}
	f, err := writer.OpenFile(receipt.Name, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteAt([]byte{0}, 0)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		t.Fatal("failed to alter test ciphertext")
	}
	if err := store.open(ctx, receipt, key, b, io.Discard, 128); err == nil {
		t.Fatal("accepted altered ciphertext")
	}
}

// TestTrialMySQLRoundTrip is opt-in. Its DSNs must point to a disposable source
// with the six trial tables and two fresh, disposable target schemas on a second
// MySQL server. The artifact is copied to a separate directory before either
// restore. It also runs on Windows without a read-only bind mount.
func TestTrialMySQLRoundTrip(t *testing.T) {
	if os.Getenv("CLAWMANAGER_TRIAL_MYSQL_TEST") != "1" {
		t.Skip("requires disposable MySQL source and targets")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	service := New(os.Getenv)
	source, database, err := service.database("SOURCE")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if err := domain.CheckTrialSource(ctx, source, database, service.value("SOURCE_UUID")); err != nil {
		t.Fatalf("source readiness: %v", err)
	}
	backup := Task{ID: newID()}
	if err := service.backup(ctx, &backup); err != nil {
		t.Fatalf("backup %s: %v", backup.Stage, err)
	}
	if !backup.Readback || backup.BackupID != backup.ID ||
		backup.Counts["users"] != 2 || backup.Counts["instances"] != 2 ||
		backup.Counts["openclaw_injection_snapshots"] != 1 || backup.Counts["instance_config_revisions"] != 1 {
		t.Fatalf("unexpected verified sample counts: %v", backup.Counts)
	}
	t.Logf("backup %s encrypted and independently read back, counts=%v", backup.ID, backup.Counts)
	imported := t.TempDir()
	for _, suffix := range []string{".trial", ".receipt"} {
		name := backup.ID + suffix
		artifact, err := os.ReadFile(filepath.Join(service.value("READ_DIR"), name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(imported, name), artifact, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("SYSTEM_BACKUP_TRIAL_READ_DIR", imported)
	t.Setenv("SYSTEM_BACKUP_TRIAL_WRITE_DIR", "")
	t.Setenv("SYSTEM_BACKUP_TRIAL_SOURCE_DSN_FILE", "")
	t.Setenv("SYSTEM_BACKUP_TRIAL_SOURCE_UUID", "")
	for _, target := range []string{"first", "second"} {
		if target == "second" {
			t.Setenv("SYSTEM_BACKUP_TRIAL_RESTORE_DSN_FILE", os.Getenv("TRIAL_TEST_REPLAY_DSN_FILE"))
			t.Setenv("SYSTEM_BACKUP_TRIAL_RESTORE_READ_DSN_FILE", os.Getenv("TRIAL_TEST_REPLAY_READ_DSN_FILE"))
		}
		if missing := service.restoreDatabaseReadiness(ctx); len(missing) != 0 {
			t.Fatalf("%s target readiness: %v", target, missing)
		}
		restore := Task{ID: newID(), BackupID: backup.ID}
		if err := service.restore(ctx, &restore); err != nil {
			t.Fatalf("%s restore %s: %v", target, restore.Stage, err)
		}
		if !restore.Readback {
			t.Fatal("restore did not reopen the encrypted artifact")
		}
		t.Logf("%s fresh target: verified IDs, ownership, specs and startup configuration through target SELECT-only credential", target)
		verifyTrialUserAccess(t, service)
	}
}

func verifyTrialUserAccess(t *testing.T, service *Service) {
	t.Helper()
	raw, err := readSetting(service.value("RESTORE_READ_DSN_FILE"), 8192)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	cfg, err := mysqldriver.ParseDSN(string(bytes.TrimSpace(raw)))
	if err != nil {
		t.Fatal("invalid fixture target DSN")
	}
	cfg.ParseTime = true
	connector, err := mysqldriver.NewConnector(cfg)
	if err != nil {
		t.Fatal("fixture target connector failed")
	}
	pool := sql.OpenDB(connector)
	defer pool.Close()
	session, err := mysqladapter.New(pool)
	if err != nil {
		t.Fatal("fixture target session failed")
	}
	user, err := repository.NewUserRepository(session).GetByID(20)
	if err != nil || user == nil || !user.IsActive || user.Role != "user" ||
		bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("trial-local-fixture")) != nil {
		t.Fatal("restored user's test credential or role did not match")
	}
	visible, err := repository.NewInstanceRepository(session).GetByUserID(user.ID, 0, 100)
	if err != nil || len(visible) != 1 || visible[0].ID != 202 || visible[0].UserID != user.ID {
		t.Fatal("restored ordinary user's scoped instance query did not match")
	}
	t.Log("restored test password and ordinary user's instance visibility verified through application repositories")
}
