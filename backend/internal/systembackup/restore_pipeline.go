package systembackup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"clawreef/internal/systembackup/team"
	"github.com/minio/minio-go/v7"
)

// DataRestoreTarget contains only disaster-side dependencies. Provisioning and
// network isolation are external prerequisites; this never starts an app or
// applies historical Kubernetes resources. Every target must be disposable.
type DataRestoreTarget struct {
	MySQL, Verifier *sql.DB
	Database        string
	Redis           *RedisClient
	RedisVerifier   *RedisClient
	Objects         *minio.Client
	BucketMapping   map[string]string
	Directory       string
	Control         Store
}

type DataRestoreReport struct {
	RunID                string              `json:"run_id"`
	BackupID             string              `json:"backup_id"`
	OriginInstallationID string              `json:"origin_installation_id"`
	Status               string              `json:"status"`
	Phase                string              `json:"phase"`
	Failure              string              `json:"failure,omitempty"`
	StartedAt            time.Time           `json:"started_at"`
	FinishedAt           *time.Time          `json:"finished_at,omitempty"`
	Team                 TeamRecoverySummary `json:"team"`
	SQL                  SQLResult           `json:"sql"`
	FileCount            int                 `json:"file_count"`
	VerifiedFileCount    int                 `json:"verified_file_count"`
	VerifiedObjectCount  int                 `json:"verified_object_count"`
	VerifiedRedisCount   int                 `json:"verified_redis_count"`
	HistoryCounts        map[string]int      `json:"history_counts"`
	HistoryParts         []ArtifactPart      `json:"history_parts"`
	AcceptanceEligible   bool                `json:"acceptance_eligible"`
}

// RestorePublishedData is a one-shot, data-only restore. It authenticates every
// source part before target writes, journals original states outside the target
// DB, normalizes interrupted operations, and checks SQL through a separate
// read-only account. A failure leaves owned partial targets for inspection; it
// never deletes resources or retries into a partially populated target.
func RestorePublishedData(ctx context.Context, store ImmutableObjectStore, artifact PublishedArtifact, wrapper DataKeyWrapper, runID string, target DataRestoreTarget, budget CaptureBudget) (report DataRestoreReport, err error) {
	report = DataRestoreReport{RunID: runID, BackupID: artifact.Manifest.Payload.BackupID, OriginInstallationID: artifact.Manifest.Payload.OriginInstallationID, Status: "failed", Phase: "preflight", StartedAt: time.Now().UTC(), HistoryCounts: map[string]int{}, HistoryParts: []ArtifactPart{}}
	if !publicIDPattern.MatchString(runID) || len(runID) != 36 || runID[:4] != "sdr_" || !restoreDatabasePattern.MatchString(target.Database) || target.MySQL == nil || target.Verifier == nil || target.MySQL == target.Verifier || target.Redis == nil || target.Objects == nil || target.Control.DB == nil || target.Control.DB == target.MySQL || len(target.BucketMapping) == 0 || budget.MaxBytes < 1<<20 || budget.MaxItems < 1 || wrapper == nil {
		return report, errors.New("invalid data restore configuration")
	}
	// The directory is an externally provisioned, empty, private target. Refuse
	// symlinks and reserve it once, so a retry cannot overwrite a previous run.
	stat, e := os.Lstat(target.Directory)
	if e != nil || !stat.IsDir() || stat.Mode()&os.ModeSymlink != 0 {
		return report, errors.New("fresh restore directory required")
	}
	entries, e := os.ReadDir(target.Directory)
	if e != nil || len(entries) != 0 {
		return report, errors.New("fresh restore directory required")
	}
	owner, e := os.OpenFile(filepath.Join(target.Directory, ".restore-owner"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return report, e
	}
	_, e = owner.WriteString(runID)
	closeErr := owner.Close()
	if e != nil {
		return report, e
	}
	if closeErr != nil {
		return report, closeErr
	}
	defer func() {
		at := time.Now().UTC()
		report.FinishedAt = &at
		if err != nil {
			report.Status = "failed"
			report.Failure = PublicError(err)
		}
		f, e := os.OpenFile(filepath.Join(target.Directory, "restore-report.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e == nil {
			e = json.NewEncoder(f).Encode(report)
			if syncErr := f.Sync(); e == nil {
				e = syncErr
			}
			if closeErr := f.Close(); e == nil {
				e = closeErr
			}
		}
		if err == nil && e != nil {
			report.Status = "failed"
			report.Failure = "evidence_incomplete"
			err = errors.New("restore report persistence failed")
		}
	}()
	var actualDB, verifierDB, restoreUser, verifierUser, restoreServer, verifierServer, controlDB, controlServer string
	if e = target.MySQL.QueryRowContext(ctx, "SELECT DATABASE(),CURRENT_USER(),@@server_uuid").Scan(&actualDB, &restoreUser, &restoreServer); e != nil {
		return report, e
	}
	if e = target.Verifier.QueryRowContext(ctx, "SELECT DATABASE(),CURRENT_USER(),@@server_uuid").Scan(&verifierDB, &verifierUser, &verifierServer); e != nil {
		return report, e
	}
	if e = target.Control.DB.QueryRowContext(ctx, "SELECT DATABASE(),@@server_uuid").Scan(&controlDB, &controlServer); e != nil {
		return report, e
	}
	if actualDB != target.Database || verifierDB != actualDB || restoreServer != verifierServer || restoreUser == verifierUser || (controlDB == actualDB && controlServer == restoreServer) {
		return report, errors.New("restore target or credential isolation mismatch")
	}
	if e = RequireReadOnlyVerifier(ctx, target.Verifier, target.Database); e != nil {
		return report, e
	}
	validate := ValidateSchema
	if artifact.Manifest.Payload.Purpose == "team_data" {
		validate = ValidateTeamDataSchema
	}
	if e = validate(ctx, target.MySQL); e != nil {
		return report, e
	}
	if e = VerifyPublishedArtifact(ctx, store, artifact, wrapper, budget.MaxBytes); e != nil {
		return report, e
	}
	targetHash, e := legacyJSONHash(map[string]string{"server": restoreServer, "database": actualDB, "run": runID})
	if e != nil {
		return report, e
	}
	at := report.StartedAt
	evidence := func(table SQLTable, change team.Change) error {
		if !change.Quarantined {
			return nil
		}
		_, e := target.Control.RecordRestoreEvidence(ctx, runID, report.BackupID, targetHash, table, change, wrapper)
		return e
	}
	consume := func(name string, fn func(io.Reader) error) error {
		return ConsumePublishedPart(ctx, store, artifact, name, wrapper, budget.MaxBytes, fn)
	}
	// Authenticate complete SQL coverage before any target business write.
	if e = consume("mysql", ValidateBusinessSQLSnapshot); e != nil {
		return report, e
	}
	var teamReport teamReportCollector
	report.Phase = "mysql"
	if e = consume("mysql", func(r io.Reader) error {
		_, e := RestoreMySQL(ctx, target.MySQL, "", actualDB, r, func(table SQLTable, row team.Row) (team.Row, error) {
			change, e := NormalizeDataRow(table, row, at)
			if e != nil {
				return nil, e
			}
			teamReport.record(change)
			if e = evidence(table, change); e != nil {
				return nil, e
			}
			return change.After, nil
		})
		return e
	}); e != nil {
		return report, e
	}
	report.Team = teamReport.summary(false)
	report.Phase = "mysql_verification"
	// Stream normalized expectations into the independent SELECT-only verifier;
	// no plaintext snapshot file and no whole-database memory buffer are needed.
	if e = consume("mysql", func(r io.Reader) error {
		pr, pw := io.Pipe()
		done := make(chan error, 1)
		go func() { _, e := NormalizeSQLSnapshot(ctx, r, pw, at, evidence); pw.CloseWithError(e); done <- e }()
		result, e := VerifyMySQL(ctx, target.Verifier, pr, int64(budget.MaxItems))
		pr.CloseWithError(e)
		producerErr := <-done
		if e != nil {
			return e
		}
		if producerErr != nil {
			return producerErr
		}
		report.SQL = result
		return nil
	}); e != nil {
		return report, e
	}
	report.Team.SQLReadbackVerified = true
	report.Phase = "workspace"
	workspace := filepath.Join(target.Directory, "workspace")
	if e = os.Mkdir(workspace, 0700); e != nil {
		return report, e
	}
	if e = consume("workspace", func(r io.Reader) error {
		files, e := ExtractFiles(ctx, r, workspace, FileBudget{budget.MaxBytes, budget.MaxItems})
		report.FileCount = len(files)
		return e
	}); e != nil {
		return report, e
	}
	report.Phase = "workspace_verification"
	if e = consume("workspace", func(r io.Reader) error {
		var e error
		report.VerifiedFileCount, e = VerifyFiles(ctx, r, workspace, FileBudget{budget.MaxBytes, budget.MaxItems})
		return e
	}); e != nil {
		return report, e
	}
	report.Phase = "redis_history"
	if e = consume("redis", func(r io.Reader) error {
		return RestoreRedisHistory(ctx, target.Redis, r, RedisBudget{MaxBytes: budget.MaxBytes, MaxItems: budget.MaxItems, MaxInventoryBytes: min(budget.MaxBytes/8, 64<<20)}, runID)
	}); e != nil {
		return report, e
	}
	report.Phase = "redis_verification"
	redisVerifier := target.RedisVerifier
	if redisVerifier == nil {
		redisVerifier = target.Redis
	}
	if e = consume("redis", func(r io.Reader) error {
		var e error
		report.VerifiedRedisCount, e = VerifyRedisHistory(ctx, redisVerifier, r, RedisBudget{MaxBytes: budget.MaxBytes, MaxItems: budget.MaxItems, MaxInventoryBytes: min(budget.MaxBytes/8, 64<<20)}, runID)
		return e
	}); e != nil {
		return report, e
	}
	report.Phase = "objects"
	if e = consume("object_storage", func(r io.Reader) error {
		return RestoreObjects(ctx, target.Objects, r, ObjectBudget{budget.MaxBytes, budget.MaxItems}, target.BucketMapping)
	}); e != nil {
		return report, e
	}
	report.Phase = "object_verification"
	if e = consume("object_storage", func(r io.Reader) error {
		var e error
		report.VerifiedObjectCount, e = VerifyObjects(ctx, target.Objects, r, ObjectBudget{budget.MaxBytes, budget.MaxItems}, target.BucketMapping)
		return e
	}); e != nil {
		return report, e
	}
	history := filepath.Join(target.Directory, "encrypted-history")
	if e = os.Mkdir(history, 0700); e != nil {
		return report, e
	}
	for _, name := range []string{"deployment_resources", "secret_bundle"} {
		report.Phase = name
		// Keep portable encrypted history, never apply source objects or leave
		// plaintext service-account tokens in a filesystem report.
		part, e := CapturePart(ctx, history, artifact.Manifest.Payload.OriginInstallationID, report.BackupID, name, budget.MaxBytes, wrapper, func(ctx context.Context, w io.Writer) error {
			return consume(name, func(r io.Reader) error {
				return ReadKubernetesHistory(ctx, io.TeeReader(r, w), name, ObjectBudget{budget.MaxBytes, budget.MaxItems}, func(_ KubernetesSelection, _ json.RawMessage) error { report.HistoryCounts[name]++; return nil })
			})
		})
		if e != nil {
			return report, e
		}
		report.HistoryParts = append(report.HistoryParts, part)
	}
	report.Phase = "complete"
	report.Status = "succeeded"
	return report, nil
}
