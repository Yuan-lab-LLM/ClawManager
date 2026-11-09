package systembackup

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func teamDataJobFixture(t *testing.T) *TeamDataService {
	t.Helper()
	db := fixtureDatabase(t)
	raw, e := os.ReadFile("../db/migrations/093_add_team_data_backup_jobs.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(string(raw)); e != nil {
		t.Fatal(e)
	}
	raw, e = os.ReadFile("../db/migrations/094_add_team_data_backup_management_operations.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, statement := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(statement) != "" {
			if _, e = db.Exec(statement); e != nil {
				t.Fatal(e)
			}
		}
	}
	raw, e = os.ReadFile("../db/migrations/095_add_team_data_provisioning_receipts.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(string(raw)); e != nil {
		t.Fatal(e)
	}
	return &TeamDataService{db: db, configHash: strings.Repeat("a", 64), owner: "test-worker"}
}

func TestTeamDataWorkerStartsWithInstallationMutex(t *testing.T) {
	s := teamDataJobFixture(t)
	s.cfg.InstallationID = "fixture-installation"
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	err := s.runOwned(ctx)
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatal("worker did not start and wait without panicking", err)
	}
}

func TestTeamDataRedisDoesNotCaptureGlobalRuntimeEventStream(t *testing.T) {
	c := redisFixture(t, "TEST_SYSTEM_BACKUP_REDIS_PORT")
	ctx := context.Background()
	id, _ := RandomID("test_")
	keys := []string{"claw:team:123:backup-test:" + id, "backup-global-runtime:" + id}
	for _, key := range keys {
		if _, e := c.command(ctx, []byte("SET"), []byte(key), []byte("retained history")); e != nil {
			t.Fatal(e)
		}
	}
	t.Cleanup(func() {
		for _, key := range keys {
			_, _ = c.command(context.Background(), []byte("DEL"), []byte(key))
		}
	})
	var archive bytes.Buffer
	b := RedisBudget{MaxItems: 100, MaxBytes: 1 << 20, MaxInventoryBytes: 1 << 20}
	if e := CaptureTeamDataRedisLogical(ctx, c, &archive, b); e != nil {
		t.Fatal(e)
	}
	var names []string
	if e := ReadRedisSnapshot(bytes.NewReader(archive.Bytes()), b, func(r RedisRecord) error { names = append(names, string(r.Key)); return nil }); e != nil {
		t.Fatal(e)
	}
	if len(names) != 1 || names[0] != keys[0] {
		t.Fatal("Team backup included unrelated runtime stream/cache", names)
	}
}

func TestTeamDataJobsIdempotencyBusyAndPendingCancel(t *testing.T) {
	s := teamDataJobFixture(t)
	ctx := context.Background()
	j, e := s.Create(ctx, 1, "same-request-0001", "backup", "")
	if e != nil {
		t.Fatal(e)
	}
	same, e := s.Create(ctx, 1, "same-request-0001", "backup", "")
	if e != nil || same.ID != j.ID {
		t.Fatal("retry created another backup", e)
	}
	if _, e = s.Create(ctx, 1, "other-request-0002", "backup", ""); e == nil || e.Error() != "conflict_active_job" {
		t.Fatal("parallel capture admitted", e)
	}
	if e = s.Cancel(ctx, j.ID); e != nil {
		t.Fatal(e)
	}
	j, e = s.Get(ctx, j.ID)
	if e != nil || j.Status != "canceled" || j.StartedAt != nil || j.FinishedAt == nil {
		t.Fatal("pending cancellation started work or lost finish time", e, j.Status)
	}
	if _, e = s.Create(ctx, 1, "same-request-0001", "restore", j.ID); e == nil || e.Error() != "idempotency_conflict" {
		t.Fatal("idempotency binding changed", e)
	}
	if _, e = s.Create(ctx, 1, "bad-restore-0003", "restore", j.ID); e == nil {
		t.Fatal("restored an incomplete backup")
	}
	if _, e = s.Create(ctx, 1, "next-backup-0004", "backup", ""); e != nil {
		t.Fatal("canceled pending job retained active slot", e)
	}
}

func TestTeamDataExportPreservesTeamResultsWithoutGlobalGatewayRows(t *testing.T) {
	db := fixtureDatabase(t)
	ctx := context.Background()
	for _, q := range []string{"CREATE TABLE team_tasks(id BIGINT PRIMARY KEY,status VARCHAR(30),result_json BLOB) ENGINE=InnoDB", "CREATE TABLE model_invocations(id BIGINT PRIMARY KEY,payload BLOB) ENGINE=InnoDB", "INSERT INTO team_tasks VALUES(1,'running',X'00FF70617274')", "INSERT INTO model_invocations VALUES(1,'unrelated global metering log')"} {
		if _, e := db.ExecContext(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	var archive bytes.Buffer
	result, e := ExportTeamDataMySQL(ctx, db, &archive, []string{"team_tasks", "model_invocations"})
	if e != nil {
		t.Fatal(e)
	}
	if result.Tables["team_tasks"] != 1 || result.Tables["model_invocations"] != 0 {
		t.Fatal("wrong data scope", result.Tables)
	}
	readback, e := ReadSQLSnapshot(bytes.NewReader(archive.Bytes()), nil, nil)
	if e != nil || readback.Tables["team_tasks"] != 1 {
		t.Fatal("archive unreadable", e)
	}
	if bytes.Contains(archive.Bytes(), []byte("unrelated global")) {
		t.Fatal("global gateway payload captured")
	}
	archive.Reset()
	full, e := ExportMySQL(ctx, db, &archive, []string{"model_invocations"})
	if e != nil || full.Tables["model_invocations"] != 1 {
		t.Fatal("existing full-system export scope changed", e)
	}
}

func TestTeamDataCaptureRecognizesHistoricalRuntimeSchema(t *testing.T) {
	db := fullSchemaDB(t)
	ctx := context.Background()
	for _, statement := range []string{
		"ALTER TABLE instances ADD COLUMN workbuddy_storage_identity VARCHAR(512) NULL",
		"CREATE TABLE workbuddy_browser_grants(id BIGINT PRIMARY KEY) ENGINE=InnoDB",
	} {
		if _, e := db.ExecContext(ctx, statement); e != nil {
			t.Fatal(e)
		}
	}
	// This is the real online preflight that failed before reading any data.
	if e := ValidateTeamDataSchema(ctx, db); e != nil {
		t.Fatal("online capture rejected existing runtime metadata", e)
	}
	if e := ValidateSchema(ctx, db); e == nil {
		t.Fatal("strict full-system contract was widened")
	}
	tables, e := snapshotBusinessTables(ctx, db, true)
	if e != nil {
		t.Fatal(e)
	}
	var archive bytes.Buffer
	if _, e = ExportTeamDataMySQL(ctx, db, &archive, tables); e != nil {
		t.Fatal(e)
	}
	target, name := isolatedSchemaDB(t, db)
	if e = alignTeamDataRestoreSchema(ctx, target, bytes.NewReader(archive.Bytes())); e != nil {
		t.Fatal(e)
	}
	if _, e = RestoreMySQL(ctx, target, "", name, bytes.NewReader(archive.Bytes()), nil); e != nil {
		t.Fatal(e)
	}
	if _, e = VerifyMySQL(ctx, target, bytes.NewReader(archive.Bytes()), 10000); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("ALTER TABLE team_tasks ADD COLUMN unexpected_execution_token TEXT NULL"); e != nil {
		t.Fatal(e)
	}
	if e = ValidateTeamDataSchema(ctx, db); e == nil {
		t.Fatal("unreviewed Team field was silently accepted")
	}
}

func TestTeamDataFileAliasesPreserveSharedLinksWithoutWideningNormalCapture(t *testing.T) {
	physical, logical, target := t.TempDir(), t.TempDir(), t.TempDir()
	shared, instance := filepath.Join(physical, "shared"), filepath.Join(physical, "instance")
	for _, dir := range []string{shared, instance} {
		if e := os.Mkdir(dir, 0700); e != nil {
			t.Fatal(e)
		}
	}
	if e := os.WriteFile(filepath.Join(shared, "partial.bin"), []byte("partial artifact"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(filepath.Join(logical, "shared"), filepath.Join(instance, "team")); e != nil {
		t.Skip("symlinks unavailable on this host")
	}
	roots := []FileRoot{{ID: "shared", Path: shared}, {ID: "instance", Path: instance}}
	var archive bytes.Buffer
	if _, e := CaptureFiles(context.Background(), &archive, roots, FileBudget{1 << 20, 100}); e == nil {
		t.Fatal("normal capture accepted an external logical alias")
	}
	archive.Reset()
	if _, e := captureFiles(context.Background(), &archive, roots, FileBudget{1 << 20, 100}, map[string]string{logical: physical}); e != nil {
		t.Fatal(e)
	}
	if _, e := ExtractFiles(context.Background(), bytes.NewReader(archive.Bytes()), target, FileBudget{1 << 20, 100}); e != nil {
		t.Fatal(e)
	}
	got, e := os.ReadFile(filepath.Join(target, "roots", "instance", "team", "partial.bin"))
	if e != nil || string(got) != "partial artifact" {
		t.Fatal("shared artifact link lost", e)
	}
}

func TestTeamDataPreservesImageDependencyLinkAsInertMetadata(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX Runtime image symlinks are tested in the owned Linux fixture")
	}
	root, target := t.TempDir(), t.TempDir()
	dir := filepath.Join(root, "home", ".openclaw", "npm", "node_modules", "@example")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	original := "/defaults/.openclaw/npm/projects/package/node_modules/@example/plugin"
	link := filepath.Join(dir, "plugin")
	if e := os.Symlink(original, link); e != nil {
		t.Skip("symlinks unavailable")
	}
	if e := os.Symlink("/usr/local/lib/node_modules/openclaw", filepath.Join(dir, "..", "openclaw")); e != nil {
		t.Fatal(e)
	}
	skills := filepath.Join(root, "home", ".openclaw", "plugin-skills")
	if e := os.MkdirAll(skills, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink("/usr/local/lib/node_modules/openclaw/dist/extensions/browser/skills/browser-automation", filepath.Join(skills, "browser-automation")); e != nil {
		t.Fatal(e)
	}
	payload := []byte("partial task artifact\x00\xff")
	if e := os.WriteFile(filepath.Join(root, "result.bin"), payload, 0600); e != nil {
		t.Fatal(e)
	}
	var archive bytes.Buffer
	roots := []FileRoot{{ID: "instance_543", Path: root}}
	budget := FileBudget{1 << 20, 100}
	if _, e := captureFilesWithPolicy(context.Background(), &archive, roots, budget, nil, true); e != nil {
		t.Fatal("image dependency link blocked Team data", e)
	}
	if _, e := ExtractFiles(context.Background(), bytes.NewReader(archive.Bytes()), target, budget); e != nil {
		t.Fatal(e)
	}
	restored := filepath.Join(target, "roots", "instance_543", "home", ".openclaw", "npm", "node_modules", "@example", "plugin")
	info, e := os.Lstat(restored)
	if e != nil || !info.Mode().IsRegular() {
		t.Fatal("external image link became active", e)
	}
	raw, e := os.ReadFile(restored)
	if e != nil {
		t.Fatal(e)
	}
	var descriptor map[string]any
	if json.Unmarshal(raw, &descriptor) != nil || descriptor["original_link"] != original {
		t.Fatal("original link metadata lost")
	}
	got, e := os.ReadFile(filepath.Join(target, "roots", "instance_543", "result.bin"))
	if e != nil || !bytes.Equal(got, payload) {
		t.Fatal("task artifact changed", e)
	}
	if _, e = VerifyFiles(context.Background(), bytes.NewReader(archive.Bytes()), target, budget); e != nil {
		t.Fatal(e)
	}
	if _, e = CaptureFiles(context.Background(), &bytes.Buffer{}, roots, budget); e == nil {
		t.Fatal("normal capture accepted external dependency link")
	}
	if e = os.Symlink("/etc/passwd", filepath.Join(root, "task-artifact-link")); e != nil {
		t.Fatal(e)
	}
	if _, e = captureFilesWithPolicy(context.Background(), &bytes.Buffer{}, roots, budget, nil, true); e == nil {
		t.Fatal("unrelated external task link was silently omitted")
	}
}

func TestTeamBackupPreservesSharedDirectoryGroupInheritance(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX setgid directory tested in owned Linux fixture")
	}
	source, target := t.TempDir(), t.TempDir()
	shared := filepath.Join(source, "shared")
	if e := os.Mkdir(shared, 0775); e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(shared, os.ModeSetgid|0775); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(shared, "partial.txt"), []byte("saved result"), 0664); e != nil {
		t.Fatal(e)
	}
	var archive bytes.Buffer
	b := FileBudget{1 << 20, 100}
	if _, e := CaptureFiles(context.Background(), &archive, []FileRoot{{ID: "team_shared", Path: source}}, b); e != nil {
		t.Fatal(e)
	}
	if e := ValidateFileSnapshot(context.Background(), bytes.NewReader(archive.Bytes()), b); e != nil {
		t.Fatal("normal Team shared directory rejected", e)
	}
	if _, e := ExtractFiles(context.Background(), bytes.NewReader(archive.Bytes()), target, b); e != nil {
		t.Fatal(e)
	}
	if _, e := VerifyFiles(context.Background(), bytes.NewReader(archive.Bytes()), target, b); e != nil {
		t.Fatal("shared directory mode or data changed", e)
	}
	for _, mode := range []int64{04755, 02755} {
		var unsafe bytes.Buffer
		tw := tar.NewWriter(&unsafe)
		if e := tw.WriteHeader(&tar.Header{Name: "roots/team_shared/privileged-program", Typeflag: tar.TypeReg, Mode: mode}); e != nil {
			t.Fatal(e)
		}
		if e := tw.Close(); e != nil {
			t.Fatal(e)
		}
		if e := ValidateFileSnapshot(context.Background(), bytes.NewReader(unsafe.Bytes()), b); e == nil {
			t.Fatal("privileged file mode accepted", mode)
		}
	}
}
