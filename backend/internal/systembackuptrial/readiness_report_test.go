package systembackuptrial

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func reportService(t *testing.T, side string) (*Service, map[string]string) {
	t.Helper()
	_, profile, path := trialEnvironmentFixture(t)
	profile.Side, profile.Job = side, nil
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	settings := map[string]string{
		"SYSTEM_BACKUP_TRIAL_ENVIRONMENT_PROFILE_FILE": path,
		"SYSTEM_BACKUP_TRIAL_RESTORE_NAMESPACE":        profile.Target.Namespace,
		"SYSTEM_BACKUP_TRIAL_RESTORE_STORAGE_CLASS":    profile.Target.InstanceStorageClass,
	}
	return New(func(key string) string { return settings[key] }), settings
}

func reportFixtureFile(t *testing.T, name string, contents []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, contents, 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func reportCheck(t *testing.T, report Readiness, id, state string) readinessCheck {
	t.Helper()
	if report.Details != nil {
		for _, check := range report.Details.Checks {
			if check.ID == id {
				if check.State != state {
					t.Fatalf("check %s = %s (%s), want %s", id, check.State, check.Reason, state)
				}
				return check
			}
		}
	}
	t.Fatalf("check %s missing", id)
	return readinessCheck{}
}

func TestTrialReadinessReportOnlyIncludesVerificationMetadata(t *testing.T) {
	service, settings := reportService(t, "source")
	key := strings.Repeat("9a", 32)
	rawKey, _ := hex.DecodeString(key)
	settings["SYSTEM_BACKUP_TRIAL_KEY_FILE"] = reportFixtureFile(t, "synthetic-key", []byte(key))
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "report-fixture"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &private.PublicKey, private)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	settings["SSL_CERT_FILE"] = reportFixtureFile(t, "ca-bundle.crt", pemBytes)
	report := service.readiness(context.Background())
	if !slices.Contains(report.Missing, "source_credential") || !slices.Contains(report.Missing, "storage_mounts") {
		t.Fatalf("original admission status changed: %v", report.Missing)
	}
	reportCheck(t, report, "preparation", "not_configured")
	reportCheck(t, report, "source_credential", "failed")
	reportCheck(t, report, "source_read", "skipped")
	reportCheck(t, report, "source_storage_roundtrip", "skipped")
	reportCheck(t, report, "source_workspace", "not_configured")
	if got := reportCheck(t, report, "database_ca", "passed"); got.Reference != "ca-bundle.crt" || got.Fingerprint != sourceMaterialSHA256(pemBytes) {
		t.Fatal("CA verification metadata differs")
	}
	if got := reportCheck(t, report, "encryption_key", "passed"); got.Fingerprint != sourceMaterialSHA256(rawKey) {
		t.Fatal("key fingerprint differs")
	}
	encoded, err := json.Marshal(report)
	if err != nil || strings.Contains(string(encoded), key) || strings.Contains(string(encoded), "BEGIN CERTIFICATE") ||
		strings.Contains(string(encoded), settings["SSL_CERT_FILE"]) {
		t.Fatal("report included test credential, certificate body or absolute path")
	}
}

func TestTrialTargetReadinessReportsSkippedLiveChecks(t *testing.T) {
	service, settings := reportService(t, "target")
	settings["SYSTEM_BACKUP_TRIAL_RESTORE_ISOLATED"] = "true"
	settings["SYSTEM_BACKUP_TRIAL_RESTORE_UUID"] = "target-fixture"
	settings["SYSTEM_BACKUP_TRIAL_READ_DIR"] = t.TempDir()
	report := service.readiness(context.Background())
	reportCheck(t, report, "target_isolation", "passed")
	reportCheck(t, report, "target_writer_credential", "failed")
	reportCheck(t, report, "target_archive", "skipped")
	reportCheck(t, report, "target_reader", "skipped")
	reportCheck(t, report, "target_importer", "not_configured")
}

func TestTrialImporterReadinessChecksWriteAndCleanup(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		path   string
		status int
	}{
		{root, http.StatusNoContent},
		{os.Getenv("CLAWMANAGER_TRIAL_REPORT_READONLY_MOUNT"), http.StatusServiceUnavailable},
	} {
		if tc.path == "" {
			continue
		}
		mount, err := os.OpenRoot(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadDir(tc.path)
		if err != nil {
			mount.Close()
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		artifactImporter(mount, trialEnvironment{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		mount.Close()
		after, err := os.ReadDir(tc.path)
		if recorder.Code != tc.status || err != nil || len(before) != len(after) || recorder.Body.Len() != 0 {
			t.Fatalf("import probe status or cleanup differs: %d, %v", recorder.Code, err)
		}
	}
}

func TestTrialTargetReadinessReportMySQL(t *testing.T) {
	writer := os.Getenv("CLAWMANAGER_TRIAL_REPORT_WRITER_DSN")
	if runtime.GOOS != "linux" || writer == "" {
		t.Skip("requires isolated MySQL and archive mounts")
	}
	service, settings := reportService(t, "target")
	settings["SYSTEM_BACKUP_TRIAL_RESTORE_ISOLATED"] = "true"
	settings["SYSTEM_BACKUP_TRIAL_RESTORE_UUID"] = os.Getenv("CLAWMANAGER_TRIAL_REPORT_TARGET_UUID")
	settings["SYSTEM_BACKUP_TRIAL_READ_DIR"] = os.Getenv("CLAWMANAGER_TRIAL_REPORT_READONLY_MOUNT")
	settings["SYSTEM_BACKUP_TRIAL_RESTORE_DSN_FILE"] = reportFixtureFile(t, "writer", []byte(writer))
	reader := os.Getenv("CLAWMANAGER_TRIAL_REPORT_READER_DSN")
	settings["SYSTEM_BACKUP_TRIAL_RESTORE_READ_DSN_FILE"] = reportFixtureFile(t, "reader", []byte(reader))
	settings["SYSTEM_BACKUP_TRIAL_KEY_FILE"] = reportFixtureFile(t, "key", []byte(strings.Repeat("5c", 32)))
	settings["SYSTEM_BACKUP_TRIAL_WORKSPACE_FILES_TARGET_ROOT"] = t.TempDir()
	importDir := t.TempDir()
	importRoot, err := os.OpenRoot(importDir)
	if err != nil {
		t.Fatal(err)
	}
	defer importRoot.Close()
	socket := filepath.Join(t.TempDir(), "import.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: artifactImporter(importRoot, trialEnvironment{})}
	defer server.Close()
	go server.Serve(listener)
	settings["SYSTEM_BACKUP_TRIAL_IMPORT_SOCKET"] = socket
	report := service.readiness(context.Background())
	if len(report.RestoreMissing) != 0 {
		t.Fatalf("target fixture readiness: %v", report.RestoreMissing)
	}
	for _, id := range []string{"target_writer", "target_writer_grants", "target_reader", "target_accounts", "target_archive", "target_schema", "target_workspace", "target_importer"} {
		reportCheck(t, report, id, "passed")
	}
	reduced := os.Getenv("CLAWMANAGER_TRIAL_REPORT_NONWRITER_DSN")
	settings["SYSTEM_BACKUP_TRIAL_RESTORE_DSN_FILE"] = reportFixtureFile(t, "nonwriter", []byte(reduced))
	report = service.readiness(context.Background())
	reportCheck(t, report, "target_writer_grants", "failed")
	reportCheck(t, report, "target_reader", "passed")
	if len(report.RestoreMissing) != 0 {
		t.Fatal("report-only grant check changed legacy admission")
	}
	if entries, err := os.ReadDir(importDir); err != nil || len(entries) != 0 {
		t.Fatal("importer left a readiness probe on the archive volume")
	}
}

func TestTrialSourceReadinessReportMySQL(t *testing.T) {
	source := os.Getenv("CLAWMANAGER_TRIAL_REPORT_SOURCE_DSN")
	if runtime.GOOS != "linux" || source == "" {
		t.Skip("requires isolated MySQL and source archive mounts")
	}
	service, settings := reportService(t, "source")
	settings["SYSTEM_BACKUP_TRIAL_SOURCE_UUID"] = os.Getenv("CLAWMANAGER_TRIAL_REPORT_TARGET_UUID")
	settings["SYSTEM_BACKUP_TRIAL_SOURCE_DSN_FILE"] = reportFixtureFile(t, "source", []byte(source))
	settings["SYSTEM_BACKUP_TRIAL_KEY_FILE"] = reportFixtureFile(t, "key", []byte(strings.Repeat("5c", 32)))
	settings["SYSTEM_BACKUP_TRIAL_WRITE_DIR"] = os.Getenv("CLAWMANAGER_TRIAL_REPORT_WRITE_MOUNT")
	settings["SYSTEM_BACKUP_TRIAL_READ_DIR"] = os.Getenv("CLAWMANAGER_TRIAL_REPORT_READONLY_MOUNT")
	report := service.readiness(context.Background())
	if len(report.Missing) != 0 {
		t.Fatalf("source fixture readiness: %v", report.Missing)
	}
	for _, id := range []string{"source_credential", "source_read", "source_storage_mounts", "source_storage_roundtrip", "encryption_key"} {
		reportCheck(t, report, id, "passed")
	}
	if entries, err := os.ReadDir(settings["SYSTEM_BACKUP_TRIAL_WRITE_DIR"]); err != nil || len(entries) != 0 {
		t.Fatal("source left a readiness probe on the archive volume")
	}
}
