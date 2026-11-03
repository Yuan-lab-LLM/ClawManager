package systembackup

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// TeamDataConfig is operator-owned bootstrap. Browser requests cannot supply
// credentials, database names, endpoints or filesystem paths.
type TeamDataConfig struct {
	Disabled                      bool   `json:"disabled,omitempty"`
	MinimumRecoveryPoints         int    `json:"minimum_recovery_points,omitempty"`
	InstallationID                string `json:"installation_id"`
	Namespace                     string `json:"namespace"`
	NamespaceUID                  string `json:"namespace_uid"`
	SourceRoot                    string `json:"source_root"`
	RuntimeRoot                   string `json:"runtime_root"`
	StageRoot                     string `json:"stage_root"`
	RestoreRoot                   string `json:"restore_root"`
	RedisURLFile                  string `json:"redis_url_file"`
	ObjectEndpoint                string `json:"object_endpoint"`
	ObjectSecure                  bool   `json:"object_secure"`
	ObjectCredentialsFile         string `json:"object_credentials_file"`
	VerifierObjectCredentialsFile string `json:"verifier_object_credentials_file"`
	RedisImage                    string `json:"restore_redis_image"`
	SourceBucket                  string `json:"source_bucket"`
	BackupBucket                  string `json:"backup_bucket"`
	AdminDSNFile                  string `json:"restore_admin_dsn_file"`
	EncryptionKeyFile             string `json:"encryption_key_file"`
	SigningKeyFile                string `json:"signing_key_file"`
	KeyVersion                    string `json:"key_version"`
	MaxBytes                      int64  `json:"max_bytes"`
	MaxItems                      int    `json:"max_items"`
	DeadlineSeconds               int    `json:"deadline_seconds"`
}

type TeamDataJob struct {
	ID              string          `json:"public_id"`
	Operation       string          `json:"operation"`
	SourceID        string          `json:"source_backup_id,omitempty"`
	Status          string          `json:"status"`
	Phase           string          `json:"phase"`
	Error           string          `json:"error_code,omitempty"`
	CancelRequested bool            `json:"cancel_requested"`
	CreatedAt       time.Time       `json:"created_at"`
	StartedAt       *time.Time      `json:"started_at,omitempty"`
	FinishedAt      *time.Time      `json:"finished_at,omitempty"`
	Report          json.RawMessage `json:"report,omitempty"`
	ConfigHash      string          `json:"-"`
	Owner           string          `json:"-"`
}

type TeamDataReport struct {
	BackupID               string                    `json:"backup_id"`
	Purpose                string                    `json:"purpose"`
	Consistency            string                    `json:"consistency"`
	Storage                string                    `json:"storage"`
	CaptureStartedAt       time.Time                 `json:"capture_started_at"`
	CaptureFinishedAt      time.Time                 `json:"capture_finished_at"`
	Parts                  []TeamDataPart            `json:"parts,omitempty"`
	IntegrityVerified      bool                      `json:"integrity_verified"`
	Restore                *DataRestoreReport        `json:"restore,omitempty"`
	Verification           *RestoredDataVerification `json:"verification,omitempty"`
	WriteCredentialRevoked bool                      `json:"write_credential_revoked,omitempty"`
	SourceUnaffected       bool                      `json:"source_untouched"`
	RuntimeDependencyLinks int                       `json:"runtime_dependency_links,omitempty"`
}
type TeamDataPart struct {
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type TeamDataService struct {
	recoveryOnly    bool
	db              *sql.DB
	admin           *sql.DB
	adminConfig     *mysql.Config
	cfg             TeamDataConfig
	configHash      string
	owner           string
	objects         *minio.Client
	verifierObjects *minio.Client
	store           ImmutableObjectStore
	wrapper         AESKeyWrapper
	signer          ed25519.PrivateKey
	kube            dynamic.Interface
	wg              sync.WaitGroup
}

func readTeamDataFile(path string, max int64) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, errors.New("not_ready")
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() > max {
		return nil, errors.New("validation_failed")
	}
	return os.ReadFile(path)
}

func NewTeamDataService(ctx context.Context, db *sql.DB, path string) (*TeamDataService, error) {
	return newTeamDataService(ctx, db, path, false)
}

// NewTeamDataRecoveryService uses a recovery-side control DB and namespace.
// It never queries source inventory, mounts source workspaces or starts workers.
func NewTeamDataRecoveryService(ctx context.Context, db *sql.DB, path string) (*TeamDataService, error) {
	return newTeamDataService(ctx, db, path, true)
}
func newTeamDataService(ctx context.Context, db *sql.DB, path string, recovery bool) (*TeamDataService, error) {
	if db == nil {
		return nil, errors.New("not_ready")
	}
	raw, e := readTeamDataFile(path, 16384)
	if e != nil {
		return nil, e
	}
	var c TeamDataConfig
	if DecodeStrictJSON(raw, &c) != nil {
		return nil, errors.New("validation_failed")
	}
	if c.MinimumRecoveryPoints < 0 || c.MinimumRecoveryPoints > 100 || !refPattern.MatchString(c.InstallationID) || c.Namespace == "" || c.NamespaceUID == "" || !refPattern.MatchString(c.KeyVersion) || !imageDigestPattern.MatchString(c.RedisImage) || c.SourceBucket == "" || c.SourceBucket == c.BackupBucket || c.BackupBucket == "" || c.MaxBytes < 1<<20 || c.MaxBytes > 128<<30 || c.MaxItems < 1 || c.MaxItems > 5000000 || c.DeadlineSeconds < 60 || c.DeadlineSeconds > 10800 {
		return nil, errors.New("validation_failed")
	}
	for _, root := range []string{c.SourceRoot, c.RuntimeRoot, c.StageRoot, c.RestoreRoot} {
		if !filepath.IsAbs(root) || filepath.Clean(root) != root || root == string(filepath.Separator) {
			return nil, errors.New("validation_failed")
		}
	}
	for _, a := range []string{c.StageRoot, c.RestoreRoot} {
		for _, b := range []string{c.SourceRoot, c.RuntimeRoot} {
			if pathWithin(a, b) || pathWithin(b, a) {
				return nil, errors.New("restore target overlaps source")
			}
		}
		if e = os.MkdirAll(a, 0700); e != nil {
			return nil, errors.New("not_ready")
		}
		if st, err := os.Lstat(a); err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("not_ready")
		}
	}
	if pathWithin(c.StageRoot, c.RestoreRoot) || pathWithin(c.RestoreRoot, c.StageRoot) {
		return nil, errors.New("validation_failed")
	}
	key, e := readTeamDataFile(c.EncryptionKeyFile, 32)
	if e != nil || len(key) != 32 {
		return nil, errors.New("encryption_key_unavailable")
	}
	signer, e := readTeamDataFile(c.SigningKeyFile, ed25519.PrivateKeySize)
	if e != nil || len(signer) != ed25519.PrivateKeySize {
		clear(key)
		return nil, errors.New("signing_key_unavailable")
	}
	creds, e := readTeamDataFile(c.ObjectCredentialsFile, 16384)
	if e != nil {
		return nil, e
	}
	defer clear(creds)
	var objectCreds struct {
		AccessKey string `json:"access_key"`
		SecretKey string `json:"secret_key"`
	}
	if DecodeStrictJSON(creds, &objectCreds) != nil || objectCreds.AccessKey == "" || objectCreds.SecretKey == "" {
		return nil, errors.New("validation_failed")
	}
	objects, e := minio.New(c.ObjectEndpoint, &minio.Options{Secure: c.ObjectSecure, Creds: credentials.NewStaticV4(objectCreds.AccessKey, objectCreds.SecretKey, ""), Transport: http.DefaultTransport})
	if e != nil {
		return nil, errors.New("artifact_unavailable")
	}
	readerRaw, e := readTeamDataFile(c.VerifierObjectCredentialsFile, 16384)
	if e != nil {
		return nil, e
	}
	defer clear(readerRaw)
	var readerCreds struct {
		AccessKey string `json:"access_key"`
		SecretKey string `json:"secret_key"`
	}
	if DecodeStrictJSON(readerRaw, &readerCreds) != nil || readerCreds.AccessKey == "" || readerCreds.SecretKey == "" || readerCreds.AccessKey == objectCreds.AccessKey {
		return nil, errors.New("validation_failed")
	}
	readerObjects, e := minio.New(c.ObjectEndpoint, &minio.Options{Secure: c.ObjectSecure, Creds: credentials.NewStaticV4(readerCreds.AccessKey, readerCreds.SecretKey, ""), Transport: http.DefaultTransport})
	if e != nil {
		return nil, errors.New("artifact_unavailable")
	}
	dsn, e := readTeamDataFile(c.AdminDSNFile, 16384)
	if e != nil {
		return nil, e
	}
	defer clear(dsn)
	adminConfig, e := mysql.ParseDSN(strings.TrimSpace(string(dsn)))
	if e != nil || adminConfig.MultiStatements || adminConfig.AllowAllFiles {
		return nil, errors.New("validation_failed")
	}
	adminConfig.ParseTime = true
	adminConfig.Timeout = 5 * time.Second
	adminConfig.ReadTimeout = 30 * time.Second
	adminConfig.WriteTimeout = 30 * time.Second
	admin, e := sql.Open("mysql", adminConfig.FormatDSN())
	if e != nil {
		return nil, errors.New("not_ready")
	}
	admin.SetMaxOpenConns(4)
	if e = admin.PingContext(ctx); e != nil {
		admin.Close()
		return nil, errors.New("not_ready")
	}
	var sourceServer, adminServer string
	if e = db.QueryRowContext(ctx, "SELECT @@server_uuid").Scan(&sourceServer); e == nil {
		e = admin.QueryRowContext(ctx, "SELECT @@server_uuid").Scan(&adminServer)
	}
	if e != nil || sourceServer != adminServer {
		admin.Close()
		return nil, errors.New("restore administrator is outside this installation")
	}
	if namespace := strings.TrimSpace(os.Getenv("POD_NAMESPACE")); namespace != "" && namespace != c.Namespace {
		admin.Close()
		return nil, errors.New("restore namespace is outside this installation")
	}
	identityConfig := c
	identityConfig.MinimumRecoveryPoints = 0
	identityConfig.Disabled = false
	configHash, hashErr := RequestHash(identityConfig)
	if hashErr != nil {
		admin.Close()
		return nil, hashErr
	}
	owner, _ := RandomID("online_")
	if recovery {
		owner, _ = RandomID("recovery_")
	}
	rc, e := rest.InClusterConfig()
	if e != nil {
		admin.Close()
		return nil, errors.New("not_ready")
	}
	rc.Timeout = 30 * time.Second
	kube, e := dynamic.NewForConfig(rc)
	if e != nil {
		admin.Close()
		return nil, errors.New("not_ready")
	}
	s := &TeamDataService{db: db, admin: admin, adminConfig: adminConfig, cfg: c, configHash: configHash, owner: owner, objects: objects, verifierObjects: readerObjects, store: ImmutableObjectStore{Client: objects, Bucket: c.BackupBucket, Root: "team-data"}, wrapper: AESKeyWrapper{Reference: SecretRef{Provider: "kubernetes", Name: "team-data-backup-bootstrap", Version: c.KeyVersion, Purpose: "encryption"}, Key: key}, signer: ed25519.PrivateKey(signer), kube: kube}
	s.recoveryOnly = recovery
	// Bucket provisioning is explicit to this configured, separate target.
	exists, e := objects.BucketExists(ctx, c.BackupBucket)
	if e != nil {
		s.Close()
		return nil, errors.New("artifact_unavailable")
	}
	if !exists {
		if e = objects.MakeBucket(ctx, c.BackupBucket, minio.MakeBucketOptions{}); e != nil {
			s.Close()
			return nil, errors.New("artifact_unavailable")
		}
		if e = objects.SetBucketVersioning(ctx, c.BackupBucket, minio.BucketVersioningConfiguration{Status: "Enabled"}); e != nil {
			s.Close()
			return nil, errors.New("artifact_unavailable")
		}
	}
	if e = s.store.ProbeRead(ctx); e != nil {
		s.Close()
		return nil, e
	}
	return s, nil
}

func pathWithin(path, root string) bool {
	rel, e := filepath.Rel(root, path)
	return e == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
func (s *TeamDataService) Close() {
	s.wg.Wait()
	if s.admin != nil {
		s.admin.Close()
	}
	clear(s.wrapper.Key)
	clear(s.signer)
}
func instanceRootID(id int64) string { return "instance_" + strconv.FormatInt(id, 10) }
func (s *TeamDataService) Overview(ctx context.Context) (map[string]any, error) {
	var current, historical int
	if e := s.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(status<>'deleted'),0),COALESCE(SUM(status='deleted'),0) FROM teams").Scan(&current, &historical); e != nil {
		return nil, e
	}
	return map[string]any{"configured": true, "disabled": s.cfg.Disabled, "storage": s.cfg.BackupBucket, "consistency": "best_effort", "max_bytes": s.cfg.MaxBytes, "scope": "team_data", "excluded": "global_ai_gateway_chat_metering_risk_logs", "restore_mode": "isolated_data_copy", "current_teams": current, "historical_deleted_teams": historical}, nil
}

const teamDataColumns = "public_id,operation,COALESCE(source_backup_id,''),status,phase,error_code,cancel_requested,created_at,started_at,finished_at,report_json,config_hash,COALESCE(owner_id,'')"

func scanTeamDataJob(row interface{ Scan(...any) error }) (j TeamDataJob, e error) {
	var report []byte
	e = row.Scan(&j.ID, &j.Operation, &j.SourceID, &j.Status, &j.Phase, &j.Error, &j.CancelRequested, &j.CreatedAt, &j.StartedAt, &j.FinishedAt, &report, &j.ConfigHash, &j.Owner)
	j.Report = json.RawMessage(report)
	if errors.Is(e, sql.ErrNoRows) {
		e = errors.New("not_found")
	}
	return
}
func (s *TeamDataService) Get(ctx context.Context, id string) (TeamDataJob, error) {
	if !publicIDPattern.MatchString(id) {
		return TeamDataJob{}, errors.New("not_found")
	}
	return scanTeamDataJob(s.db.QueryRowContext(ctx, "SELECT "+teamDataColumns+" FROM system_team_data_backup_jobs WHERE public_id=?", id))
}
func (s *TeamDataService) List(ctx context.Context) ([]TeamDataJob, error) {
	rows, e := s.db.QueryContext(ctx, "SELECT "+teamDataColumns+" FROM system_team_data_backup_jobs ORDER BY created_at DESC LIMIT 50")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []TeamDataJob{}
	for rows.Next() {
		j, e := scanTeamDataJob(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *TeamDataService) Create(ctx context.Context, actor int, key, operation, source string) (TeamDataJob, error) {
	if s.cfg.Disabled && operation != "cleanup" {
		return TeamDataJob{}, errors.New("feature_disabled")
	}
	if s.recoveryOnly && operation == "backup" {
		return TeamDataJob{}, errors.New("recovery_only")
	}
	if actor < 1 || !idemPattern.MatchString(key) || (operation != "backup" && operation != "restore" && operation != "cleanup" && operation != "verify" && operation != "delete") || (operation == "backup" && source != "") || ((operation == "restore" || operation == "verify" || operation == "delete") && (!publicIDPattern.MatchString(source) || !strings.HasPrefix(source, "sbk_"))) || (operation == "cleanup" && (!publicIDPattern.MatchString(source) || !strings.HasPrefix(source, "sdr_"))) {
		return TeamDataJob{}, errors.New("validation_failed")
	}
	hash, _ := RequestHash(map[string]string{"operation": operation, "source_backup_id": source})
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return TeamDataJob{}, e
	}
	defer tx.Rollback()
	var id, oldHash string
	e = tx.QueryRowContext(ctx, "SELECT public_id,request_hash FROM system_team_data_backup_jobs WHERE actor_id=? AND idempotency_key=? FOR UPDATE", actor, key).Scan(&id, &oldHash)
	if e == nil {
		if oldHash != hash {
			return TeamDataJob{}, errors.New("idempotency_conflict")
		}
		tx.Rollback()
		return s.Get(ctx, id)
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return TeamDataJob{}, e
	}
	if operation == "restore" || operation == "verify" {
		j, e := s.Get(ctx, source)
		if e != nil || j.Status != "succeeded" || j.Operation != "backup" || j.ConfigHash != s.configHash {
			state, _, catalogErr := s.catalog().Load(ctx)
			a, ok := state.Artifacts[source]
			if catalogErr != nil || !ok || a.Deleted || a.Deleting {
				return TeamDataJob{}, errors.New("artifact_unavailable")
			}
		}
		if s.store.Client != nil {
			state, _, e := s.catalog().Load(ctx)
			if e != nil {
				return TeamDataJob{}, e
			}
			if a, ok := state.Artifacts[source]; ok && (a.Deleted || a.Deleting) {
				return TeamDataJob{}, errors.New("backup_deleting")
			}
		}
	}
	if operation == "delete" {
		state, _, e := s.catalog().Load(ctx)
		if e != nil {
			return TeamDataJob{}, e
		}
		a, ok := state.Artifacts[source]
		if !ok || !a.Deleting || a.Deleted || a.DeleteActor != actor {
			return TeamDataJob{}, errors.New("confirmation_invalid")
		}
	}
	if operation == "cleanup" {
		j, e := s.Get(ctx, source)
		if e != nil || (j.Operation != "restore" && j.Operation != "verify") || j.ConfigHash != s.configHash || (j.Status != "succeeded" && j.Status != "failed" && j.Status != "canceled") {
			return TeamDataJob{}, errors.New("state_conflict")
		}
	}
	prefix := "sbk_"
	if operation != "backup" {
		prefix = "sdr_"
	}
	id, e = RandomID(prefix)
	if e != nil {
		return TeamDataJob{}, e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO system_team_data_backup_jobs(public_id,operation,source_backup_id,actor_id,idempotency_key,request_hash,config_hash) VALUES(?,?,NULLIF(?,''),?,?,?,?)", id, operation, source, actor, key, hash, s.configHash)
	if e != nil {
		var duplicate *mysql.MySQLError
		if errors.As(e, &duplicate) && duplicate.Number == 1062 {
			return TeamDataJob{}, errors.New("conflict_active_job")
		}
		return TeamDataJob{}, e
	}
	// Reserve the storage-side reference before accepting a queued restore or
	// verification. The same CAS state is used by independent recovery tools.
	if operation == "restore" || operation == "verify" {
		a, e := OpenPublishedArtifact(ctx, s.store, s.cfg.InstallationID, source, s.catalog().PublicKey, s.cfg.KeyVersion)
		if e != nil {
			return TeamDataJob{}, e
		}
		if e = s.catalog().Import(ctx, a, actor, false); e != nil {
			return TeamDataJob{}, e
		}
		if e = s.catalog().Acquire(ctx, source, id, "job:"+id); e != nil {
			return TeamDataJob{}, e
		}
		// A COMMIT error may have committed. Retain this reference on an
		// unknown DB result rather than permitting an unsafe concurrent delete.
	}
	if e = tx.Commit(); e != nil {
		return TeamDataJob{}, e
	}
	return s.Get(ctx, id)
}

func (s *TeamDataService) Cancel(ctx context.Context, id string) error {
	before, e := s.Get(ctx, id)
	if e != nil {
		return e
	}
	r, e := s.db.ExecContext(ctx, "UPDATE system_team_data_backup_jobs SET cancel_requested=1,finished_at=CASE WHEN status='pending' THEN UTC_TIMESTAMP(6) ELSE finished_at END,status=CASE WHEN status='pending' THEN 'canceled' ELSE 'canceling' END,updated_at=UTC_TIMESTAMP(6) WHERE public_id=? AND status IN ('pending','running','canceling')", id)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return errors.New("state_conflict")
	}
	if before.Status == "pending" && (before.Operation == "restore" || before.Operation == "verify") {
		after, e := s.Get(ctx, id)
		if e != nil {
			return e
		}
		if after.Status == "canceled" {
			return s.catalog().Release(ctx, before.SourceID, before.ID, "job:"+before.ID)
		}
	}
	return nil
}

func (s *TeamDataService) phase(ctx context.Context, j TeamDataJob, phase string) error {
	r, e := s.db.ExecContext(ctx, "UPDATE system_team_data_backup_jobs SET phase=?,updated_at=UTC_TIMESTAMP(6) WHERE public_id=? AND status='running' AND cancel_requested=0 AND owner_id=?", phase, j.ID, s.owner)
	return requireRow(r, e)
}

func (s *TeamDataService) redis(ctx context.Context) (*RedisClient, error) {
	raw, e := readTeamDataFile(s.cfg.RedisURLFile, 16384)
	if e != nil {
		return nil, e
	}
	defer clear(raw)
	u, e := url.Parse(strings.TrimSpace(string(raw)))
	if e != nil || u.Scheme != "redis" || u.Hostname() == "" {
		return nil, errors.New("validation_failed")
	}
	password := ""
	username := ""
	if u.User != nil {
		username = u.User.Username()
		password, _ = u.User.Password()
	}
	database := 0
	if u.Path != "" && u.Path != "/0" && u.Path != "/" {
		return nil, errors.New("validation_failed")
	}
	return ConnectRedis(ctx, RedisOptions{Address: u.Host, Username: username, Password: password, Database: database, MaxReplyBytes: 64 << 20})
}

func (s *TeamDataService) captureSources(ctx context.Context, j TeamDataJob) (CaptureSources, error) {
	r, e := s.redis(ctx)
	if e != nil {
		return CaptureSources{}, e
	}
	source := CaptureSources{MySQL: s.db, Redis: r, Objects: s.objects, ObjectBuckets: []string{s.cfg.SourceBucket}, Namespace: s.cfg.Namespace, NamespaceUID: s.cfg.NamespaceUID, DeploymentClient: s.kube, SecretClient: s.kube, TeamDataOnly: true, FileAliases: map[string]string{s.cfg.RuntimeRoot: s.cfg.SourceRoot}}
	// Shared Team task data is canonical here, including historical Teams.
	shared := filepath.Join(s.cfg.SourceRoot, "teams")
	if st, e := os.Stat(shared); e != nil || !st.IsDir() {
		r.Close()
		return CaptureSources{}, errors.New("required_part_failed")
	}
	source.Workspace = []FileRoot{{ID: "team_shared", Path: shared}}
	rows, e := s.db.QueryContext(ctx, "SELECT DISTINCT i.id,i.workspace_path FROM instances i JOIN team_members m ON m.instance_id=i.id WHERE m.status<>'deleted' AND i.workspace_path IS NOT NULL ORDER BY i.id")
	if e != nil {
		r.Close()
		return CaptureSources{}, e
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var logical string
		if e = rows.Scan(&id, &logical); e != nil {
			r.Close()
			return CaptureSources{}, e
		}
		logical = filepath.Clean(logical)
		if !pathWithin(logical, s.cfg.RuntimeRoot) || logical == s.cfg.RuntimeRoot {
			r.Close()
			return CaptureSources{}, errors.New("source workspace outside configured root")
		}
		rel, _ := filepath.Rel(s.cfg.RuntimeRoot, logical)
		physical := filepath.Join(s.cfg.SourceRoot, rel)
		if st, e := os.Stat(physical); e != nil || !st.IsDir() {
			r.Close()
			return CaptureSources{}, errors.New("required_part_failed")
		}
		source.Workspace = append(source.Workspace, FileRoot{ID: instanceRootID(id), Path: physical})
	}
	if e = rows.Err(); e != nil {
		r.Close()
		return CaptureSources{}, e
	}
	source.Progress = func(part string) { _ = s.phase(ctx, j, "capture_"+part) }
	return source, nil
}

func (s *TeamDataService) backup(ctx context.Context, j TeamDataJob) (report TeamDataReport, err error) {
	report = TeamDataReport{BackupID: j.ID, Purpose: "team_data", Consistency: "best_effort", Storage: s.cfg.BackupBucket + "/team-data/" + s.cfg.InstallationID + "/" + j.ID, SourceUnaffected: true, CaptureStartedAt: time.Now().UTC()}
	stage := filepath.Join(s.cfg.StageRoot, j.ID)
	if e := os.Mkdir(stage, 0700); e != nil {
		return report, errors.New("fresh encrypted staging required")
	}
	defer func() {
		root, e := os.OpenRoot(s.cfg.StageRoot)
		if e == nil {
			defer root.Close()
			e = root.RemoveAll(j.ID)
		}
		if e != nil && err == nil {
			err = errors.New("staging_cleanup_failed")
		}
	}()
	source, e := s.captureSources(ctx, j)
	if e != nil {
		return report, e
	}
	defer source.Redis.Close()
	source.MetadataOnlyFiles = func(n int) { report.RuntimeDependencyLinks = n }
	manifest, e := CaptureFunctionalBundle(ctx, stage, s.cfg.InstallationID, j.ID, source, CaptureBudget{s.cfg.MaxBytes, s.cfg.MaxItems}, s.wrapper, s.signer, s.cfg.KeyVersion)
	if e != nil {
		return report, e
	}
	report.CaptureFinishedAt = time.Now().UTC()
	if e = s.phase(ctx, j, "publishing"); e != nil {
		return report, e
	}
	artifact, e := PublishFunctionalArtifact(ctx, s.store, stage, manifest, s.signer, s.cfg.KeyVersion, s.cfg.MaxBytes)
	if e != nil {
		return report, e
	}
	if e = s.phase(ctx, j, "verifying_backup"); e != nil {
		return report, e
	}
	if e = ValidatePublishedSnapshot(ctx, s.store, artifact, s.wrapper, CaptureBudget{s.cfg.MaxBytes, s.cfg.MaxItems}); e != nil {
		return report, e
	}
	for _, p := range manifest.Payload.Parts {
		report.Parts = append(report.Parts, TeamDataPart{Name: p.Name, Bytes: p.CiphertextBytes, SHA256: p.CiphertextSHA256})
	}
	report.IntegrityVerified = true
	if e = s.catalog().Import(ctx, artifact, 0, true); e != nil {
		return report, e
	}
	return report, nil
}
