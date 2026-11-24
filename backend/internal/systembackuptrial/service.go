// Package systembackuptrial owns the opt-in trial workflow. It does not use
// formal backup admission, task storage or recovery point registration.
package systembackuptrial

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	staging "clawreef/internal/systembackuptrial/envelope"
	domain "clawreef/internal/systembackuptrial/records"

	"github.com/go-sql-driver/mysql"
)

const maxEncoded = 64 << 20
const maxTrialPlaintext = maxEncoded + maxHermesHistory + maxWorkspaceArchive + 4096

var limits = domain.Limits{MaxRows: 100000, MaxBytes: maxEncoded}
var taskID = regexp.MustCompile(`^[0-9a-f]{32}$`)
var ciphertextDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)
var errConfig = errors.New("trial configuration unavailable")
var errReadback = errors.New("trial readback mismatch")
var errArtifactChanged = errors.New("trial ciphertext differs from the checked backup")

type Task struct {
	ID                       string                `json:"id"`
	BackupID                 string                `json:"backup_id,omitempty"`
	Status                   string                `json:"status"`
	Stage                    string                `json:"stage"`
	Error                    string                `json:"error,omitempty"`
	Readback                 bool                  `json:"readback_verified"`
	BackupMode               string                `json:"backup_mode,omitempty"`
	CiphertextSHA256         string                `json:"ciphertext_sha256,omitempty"`
	Counts                   map[string]int        `json:"counts,omitempty"`
	Hermes                   *historyReceipt       `json:"hermes_history,omitempty"`
	Workspace                *workspaceFileReceipt `json:"workspace_files,omitempty"`
	expectedCiphertextSHA256 string
	selection                *backupSelection
}

type Readiness struct {
	Missing         []string                `json:"missing"`
	RestoreMissing  []string                `json:"restore_missing"`
	Environment     *trialEnvironmentStatus `json:"environment,omitempty"`
	ImportAvailable bool                    `json:"import_available,omitempty"`
	Details         *readinessDetails       `json:"details,omitempty"`
}

type Service struct {
	env             func(string) string
	environmentJobs func(string) (environmentJobs, error)
	mu              sync.Mutex
	task            *Task
	checking        bool
}

func New(env func(string) string) *Service { return &Service{env: env} }

func (s *Service) value(name string) string {
	return strings.TrimSpace(s.env("SYSTEM_BACKUP_TRIAL_" + name))
}

func readSetting(file string, limit int64) ([]byte, error) {
	if !filepath.IsAbs(file) {
		return nil, errConfig
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, errConfig
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errConfig
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errConfig
	}
	return data, nil
}

func (s *Service) database(credential string) (*sql.DB, string, error) {
	cfg, err := s.databaseConfig(credential)
	if err != nil {
		return nil, "", err
	}
	db, err := openTrialMySQL(cfg)
	if err != nil {
		return nil, "", err
	}
	if credential != "RUNTIME" {
		db.SetMaxOpenConns(2)
	}
	return db, cfg.DBName, nil
}

func (s *Service) databaseConfig(credential string) (*mysql.Config, error) {
	raw, err := readSetting(s.value(credential+"_DSN_FILE"), 8192)
	if err != nil {
		return nil, err
	}
	defer clear(raw)
	cfg, err := mysql.ParseDSN(string(bytes.TrimSpace(raw)))
	if err != nil || cfg.DBName == "" || cfg.User == "" || cfg.Passwd == "" {
		return nil, errConfig
	}
	// Use verified TLS on remote TCP. Local development can use loopback
	// and a Unix socket with filesystem permissions.
	if cfg.Net == "tcp" {
		host, _, addrErr := net.SplitHostPort(cfg.Addr)
		if addrErr != nil {
			return nil, errConfig
		}
		local := net.ParseIP(host).IsLoopback()
		if !local && cfg.TLSConfig != "true" {
			return nil, errConfig
		}
	} else if cfg.Net != "unix" {
		return nil, errConfig
	}
	cfg.MultiStatements = false
	cfg.AllowAllFiles = false
	cfg.ParseTime = credential == "RUNTIME"
	if cfg.Params == nil {
		cfg.Params = make(map[string]string)
	}
	cfg.Params["time_zone"] = "'+00:00'"
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 30 * time.Second
	cfg.WriteTimeout = 30 * time.Second
	return cfg, nil
}

func openTrialMySQL(cfg *mysql.Config) (*sql.DB, error) {
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, errConfig
	}
	return sql.OpenDB(connector), nil
}

func (s *Service) key() ([]byte, error) {
	raw, err := readSetting(s.value("KEY_FILE"), 128)
	if err != nil {
		return nil, err
	}
	defer clear(raw)
	key, err := hex.DecodeString(string(bytes.TrimSpace(raw)))
	if err != nil || len(key) != 32 {
		clear(key)
		return nil, errConfig
	}
	return key, nil
}

type storage struct{ writer, reader *os.Root }

func (s *Service) readerRoot() (*os.Root, error) {
	path := s.value("READ_DIR")
	if !filepath.IsAbs(path) {
		return nil, errConfig
	}
	reader, err := os.OpenRoot(path)
	if err != nil {
		return nil, errConfig
	}
	return reader, nil
}

func (s *Service) storage() (storage, error) {
	w, r := s.value("WRITE_DIR"), s.value("READ_DIR")
	if !filepath.IsAbs(w) || !filepath.IsAbs(r) || filepath.Clean(w) == filepath.Clean(r) {
		return storage{}, errConfig
	}
	writer, err := os.OpenRoot(w)
	if err != nil {
		return storage{}, errConfig
	}
	reader, err := s.readerRoot()
	if err != nil {
		writer.Close()
		return storage{}, errConfig
	}
	return storage{writer, reader}, nil
}

func (s storage) close() {
	s.reader.Close()
	s.writer.Close()
}

func newID() string {
	var raw [16]byte
	rand.Read(raw[:])
	return hex.EncodeToString(raw[:])
}

func removeOwned(root *os.Root, name string) error {
	// Windows requires clearing the read-only attribute before deleting a
	// file sealed by staging.
	if err := root.Chmod(name, 0600); err != nil {
		return err
	}
	return root.Remove(name)
}

func binding(id string) staging.Binding {
	scope := sha256.Sum256([]byte("clawmanager.system-backup.trial.v1"))
	return staging.Binding{BackupID: id, Attempt: "1", Path: id + ".trial", EnvelopeID: id, CaptureContextHash: hex.EncodeToString(scope[:])}
}

func taskKey(key []byte, id string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("clawmanager.system-backup.trial.v1:" + id))
	return mac.Sum(nil)
}

func (s storage) probe(ctx context.Context, master []byte) (err error) {
	id := newID()
	key := taskKey(master, id)
	defer clear(key)
	b := binding(id)
	challenge := []byte(newID())
	receipt, err := staging.SealFile(ctx, s.writer, b.Path, key, b, func(w io.Writer) error {
		_, err := w.Write(challenge)
		return err
	})
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, removeOwned(s.writer, receipt.Name))
	}()
	if readOnly, err := probeReadOnlyReader(s.reader); err != nil || !readOnly {
		return errors.Join(errConfig, err)
	}
	var got bytes.Buffer
	if err := s.open(ctx, receipt, key, b, &got, 128); err != nil || !bytes.Equal(got.Bytes(), challenge) {
		return errReadback
	}
	return nil
}

// A unique directory distinguishes a denial from a successful write without
// touching artifacts. Remove it immediately if the reader can write.
func probeReadOnlyReader(reader *os.Root) (bool, error) {
	marker := newID() + ".write-probe"
	if err := reader.Mkdir(marker, 0700); err != nil {
		if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EROFS) {
			return true, nil
		}
		return false, err
	}
	return false, reader.Remove(marker)
}

func (s storage) open(ctx context.Context, receipt staging.Receipt, key []byte, b staging.Binding, dst io.Writer, limit int64) error {
	if receipt.Name != b.Path || receipt.Size < 1 || receipt.Size > limit+(1<<20) {
		return errReadback
	}
	f, err := staging.OpenFile(ctx, s.reader, receipt)
	if err != nil {
		return err
	}
	defer f.Close()
	return staging.Open(ctx, f, dst, key, b, limit)
}

type artifact struct {
	Receipt staging.Receipt `json:"receipt"`
}

type manifest struct {
	SourceUUID string             `json:"source_uuid"`
	SHA256     string             `json:"snapshot_sha256"`
	Counts     map[string]int     `json:"counts"`
	Hermes     *historyManifest   `json:"hermes_history,omitempty"`
	Workspace  *workspaceManifest `json:"workspace_files,omitempty"`
}

func (s storage) metadata(id string) (artifact, error) {
	out, _, err := s.metadataBytes(id)
	return out, err
}

func (s storage) metadataBytes(id string) (artifact, []byte, error) {
	var out artifact
	f, err := s.reader.Open(id + ".receipt")
	if err != nil {
		return out, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 2048 {
		return out, nil, errReadback
	}
	raw, err := io.ReadAll(io.LimitReader(f, 2049))
	if err != nil || len(raw) > 2048 {
		return out, nil, errReadback
	}
	err = json.Unmarshal(raw, &out)
	return out, raw, err
}

type restoredArtifact struct {
	snapshot         domain.Snapshot
	manifest         manifest
	hermes           []byte
	workspace        []byte
	ciphertextSHA256 string
}

func readArtifactSection(r io.Reader, length, limit int) ([]byte, error) {
	if length <= 0 || length > limit {
		return nil, errReadback
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		clear(data)
		return nil, errReadback
	}
	return data, nil
}

func (s storage) snapshot(ctx context.Context, id string, a artifact, master []byte) (restoredArtifact, error) {
	key := taskKey(master, id)
	defer clear(key)
	var plain bytes.Buffer
	defer func() { clear(plain.Bytes()) }()
	if err := s.open(ctx, a.Receipt, key, binding(id), &plain, maxTrialPlaintext); err != nil {
		return restoredArtifact{}, err
	}
	reader := bufio.NewReader(bytes.NewReader(plain.Bytes()))
	line, err := reader.ReadSlice('\n')
	var m manifest
	if err != nil || json.Unmarshal(line, &m) != nil || m.SourceUUID == "" {
		return restoredArtifact{}, errReadback
	}
	var history, files []byte
	verified := false
	defer func() {
		if !verified {
			clear(history)
			clear(files)
		}
	}()
	if m.Hermes != nil {
		history, err = readArtifactSection(reader, m.Hermes.Bytes, maxHermesHistory)
		if err != nil {
			return restoredArtifact{}, err
		}
	}
	if m.Workspace != nil {
		files, err = readArtifactSection(reader, m.Workspace.ArchiveBytes, maxWorkspaceArchive)
		if err != nil {
			return restoredArtifact{}, err
		}
	}
	snapshot, err := domain.Decode(reader, limits)
	if err != nil {
		return restoredArtifact{}, err
	}
	digest, err := domain.Digest(snapshot)
	if err != nil || digest != m.SHA256 || !maps.Equal(counts(snapshot), m.Counts) {
		return restoredArtifact{}, errReadback
	}
	if err := domain.ValidateTrial(snapshot); err != nil {
		return restoredArtifact{}, err
	}
	if m.Hermes != nil {
		actual, err := describeHistory(history, snapshot)
		if err != nil || actual != *m.Hermes {
			return restoredArtifact{}, errReadback
		}
	}
	if m.Workspace != nil {
		actual, err := describeWorkspaceFiles(files)
		scope := selectedRuntime(snapshot, m.Workspace.workspaceSelection)
		if err != nil || actual != m.Workspace.workspaceFileReceipt || scope == "" || scope != m.Workspace.Runtime {
			return restoredArtifact{}, errReadback
		}
	}
	verified = true
	return restoredArtifact{snapshot: snapshot, manifest: m, hermes: history, workspace: files, ciphertextSHA256: a.Receipt.SHA256}, nil
}

func (s *Service) restoreSettings() []string {
	return s.reportRestoreSettings(nil)
}

func (s *Service) reportRestoreSettings(details *readinessDetails) []string {
	var missing []string
	key, err := s.key()
	details.key(key, err)
	if err != nil {
		missing = append(missing, "encryption_key")
	}
	clear(key)
	reader, err := s.readerRoot()
	if err != nil {
		missing = append(missing, "restore_artifact_mount")
		details.result("target_archive", "restore_artifact_mount")
	} else {
		reader.Close()
	}
	if s.value("RESTORE_ISOLATED") != "true" ||
		s.value("RESTORE_UUID") == "" || s.value("RESTORE_UUID") == s.value("SOURCE_UUID") {
		missing = append(missing, "restore_isolation")
		details.result("target_isolation", "restore_isolation")
	} else {
		details.result("target_isolation", "")
	}
	for _, account := range []struct{ setting, check, missing string }{
		{"RESTORE", "target_writer_credential", "restore_credential"},
		{"RESTORE_READ", "target_reader_credential", "restore_read_credential"},
	} {
		db, database, err := s.database(account.setting)
		if err != nil {
			missing = append(missing, account.missing)
			details.result(account.check, account.missing)
			continue
		}
		db.Close()
		if !strings.HasPrefix(database, "cm_drill_") {
			missing = append(missing, "restore_database_name")
			details.result(account.check, "restore_database_name")
		} else {
			details.result(account.check, "")
		}
	}
	if !strings.HasPrefix(s.value("RESTORE_NAMESPACE"), "cm-drill-") ||
		s.value("RESTORE_STORAGE_CLASS") == "" {
		missing = append(missing, "restore_placement")
		details.result("target_placement", "restore_placement")
	} else {
		details.result("target_placement", "")
	}
	return missing
}

func (s *Service) readiness(ctx context.Context) Readiness {
	environment := s.environment(ctx, false)
	result := Readiness{Environment: &environment}
	if environment.Profile != nil {
		result.Details = newReadinessDetails(environment.Profile.Side)
		if environment.State == "registered" {
			result.Details.set("preparation", "not_configured", "", "", "")
		} else {
			result.Details.result("preparation", environment.missing(environment.Profile.Side))
		}
	}
	if missing := environment.missing("source"); missing != "" {
		result.Missing = []string{missing}
	} else {
		result.Missing = s.reportBackupReadiness(ctx, result.Details)
	}
	if missing := environment.missing("target"); missing != "" {
		result.RestoreMissing = []string{missing}
	} else {
		result.RestoreMissing = s.reportRestoreReadiness(ctx, result.Details)
	}
	result.ImportAvailable = environment.Profile != nil && environment.Profile.Side == "target" &&
		len(result.RestoreMissing) == 0 && s.importSocketReady()
	if result.Details != nil && environment.missing(environment.Profile.Side) == "" {
		s.addReadinessDetails(ctx, environment.Profile, result.Details)
	}
	return result
}

func (s *Service) backupReadiness(ctx context.Context) []string {
	return s.reportBackupReadiness(ctx, nil)
}

func (s *Service) reportBackupReadiness(ctx context.Context, details *readinessDetails) []string {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var missing []string
	source, database, err := s.database("SOURCE")
	if err != nil {
		missing = append(missing, "source_credential")
		details.result("source_credential", "source_credential")
	} else {
		defer source.Close()
		details.result("source_credential", "")
		if err := domain.CheckTrialSource(ctx, source, database, s.value("SOURCE_UUID")); err != nil {
			reason := "source_" + category(err)
			missing = append(missing, reason)
			details.result("source_read", reason)
		} else {
			details.result("source_read", "")
		}
	}
	key, err := s.key()
	details.key(key, err)
	if err != nil {
		missing = append(missing, "encryption_key")
	}
	defer clear(key)
	store, err := s.storage()
	if err != nil {
		missing = append(missing, "storage_mounts")
		details.result("source_storage_mounts", "storage_mounts")
	} else {
		defer store.close()
		details.result("source_storage_mounts", "")
		if len(key) == 32 {
			if err := store.probe(ctx, key); err != nil {
				missing = append(missing, "storage_write_readback")
				details.result("source_storage_roundtrip", "storage_write_readback")
			} else {
				details.result("source_storage_roundtrip", "")
			}
		}
	}
	return missing
}

func (s *Service) restoreReadiness(ctx context.Context) []string {
	return s.reportRestoreReadiness(ctx, nil)
}

func (s *Service) reportRestoreReadiness(ctx context.Context, details *readinessDetails) []string {
	if missing := s.reportRestoreSettings(details); len(missing) != 0 {
		return missing
	}
	reader, err := s.readerRoot()
	if err != nil {
		details.result("target_archive", "restore_artifact_mount")
		return []string{"restore_artifact_mount"}
	}
	defer reader.Close()
	var missing []string
	if readOnly, err := restoreMountReadOnly(reader); err != nil {
		missing = append(missing, "restore_artifact_mount")
		details.result("target_archive", "restore_artifact_mount")
	} else if !readOnly {
		missing = append(missing, "restore_artifact_writable")
		details.result("target_archive", "restore_artifact_writable")
	} else {
		details.result("target_archive", "")
	}
	return append(missing, s.reportRestoreDatabase(ctx, details)...)
}

func (s *Service) restoreDatabaseReadiness(ctx context.Context) []string {
	return s.reportRestoreDatabase(ctx, nil)
}

func (s *Service) reportRestoreDatabase(ctx context.Context, details *readinessDetails) []string {
	writer, database, err := s.database("RESTORE")
	if err != nil {
		details.result("target_writer_credential", "restore_credential")
		return []string{"restore_credential"}
	}
	defer writer.Close()
	reader, readDatabase, err := s.database("RESTORE_READ")
	if err != nil {
		details.result("target_reader_credential", "restore_read_credential")
		return []string{"restore_read_credential"}
	}
	defer reader.Close()
	if database != readDatabase {
		details.result("target_accounts", "restore_database_mismatch")
		return []string{"restore_database_mismatch"}
	}

	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	target := domain.DrillTarget{Database: database, ServerUUID: s.value("RESTORE_UUID"), CheckIsolation: func(ctx context.Context) error {
		return ctx.Err()
	}}
	var missing []string
	writerAccount, err := domain.CheckTrialTargetAccount(ctx, writer, target, false)
	if err != nil {
		reason := "restore_writer_" + category(err)
		missing = append(missing, reason)
		details.result("target_writer", reason)
	} else {
		details.result("target_writer", "")
	}
	readerAccount, err := domain.CheckTrialTargetAccount(ctx, reader, target, true)
	if err != nil {
		reason := "restore_reader_" + category(err)
		missing = append(missing, reason)
		details.result("target_reader", reason)
	} else {
		details.result("target_reader", "")
	}
	if writerAccount != "" && readerAccount != "" {
		if writerAccount == readerAccount {
			missing = append(missing, "restore_accounts_shared")
			details.result("target_accounts", "restore_accounts_shared")
		} else {
			details.result("target_accounts", "")
		}
	}
	if len(missing) == 0 {
		if err := domain.CheckTrialRestoreSchema(ctx, reader); err != nil {
			reason := "restore_target_" + category(err)
			missing = append(missing, reason)
			details.result("target_schema", reason)
		} else {
			details.result("target_schema", "")
		}
	}
	if details != nil && writerAccount != "" && ctx.Err() == nil {
		if err := domain.CheckTrialRuntimeWriter(ctx, writer, target); err != nil {
			details.result("target_writer_grants", "restore_writer_grants")
		} else {
			details.result("target_writer_grants", "")
		}
	}
	return missing
}

func category(err error) string {
	switch {
	case errors.Is(err, domain.ErrTrialScope):
		return "backup_scope_changed"
	case errors.Is(err, errArtifactChanged):
		return "restore_artifact_changed"
	case errors.Is(err, errWorkspaceInstanceMissing):
		return "restore_workspace_instance"
	case errors.Is(err, errWorkspaceFiles):
		return "workspace_files"
	case errors.Is(err, errHistory):
		return "hermes_history"
	case errors.Is(err, domain.ErrSchema):
		return "schema"
	case errors.Is(err, domain.ErrIsolation):
		return "isolation"
	case errors.Is(err, domain.ErrNotEmpty):
		return "target_not_empty"
	case errors.Is(err, domain.ErrData):
		return "data"
	case errors.Is(err, domain.ErrCapacity):
		return "capacity"
	case errors.Is(err, domain.ErrDatabase):
		return "database"
	case errors.Is(err, domain.ErrNormalization):
		return "placement"
	default:
		return "failed"
	}
}
