package systembackup

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"clawreef/internal/systembackup/team"
	"github.com/minio/minio-go/v7"
)

// RestoreVerificationIntent must come from the controller's frozen restore
// intent/evidence, not from an editable target report. It never grants formal
// acceptance or proves that restore write credentials have been destroyed.
type RestoreVerificationIntent struct {
	RunID        string         `json:"run_id"`
	NormalizedAt time.Time      `json:"normalized_at"`
	HistoryParts []ArtifactPart `json:"history_parts"`
}
type RestoredDataReader struct {
	MySQL         *sql.DB
	Database      string
	Redis         *RedisClient
	Objects       *minio.Client
	BucketMapping map[string]string
	Directory     string
}
type RestoredDataVerification struct {
	RunID                string              `json:"run_id"`
	BackupID             string              `json:"backup_id"`
	SQL                  SQLResult           `json:"sql"`
	Team                 TeamRecoverySummary `json:"team"`
	FileCount            int                 `json:"file_count"`
	ObjectCount          int                 `json:"object_count"`
	RedisCount           int                 `json:"redis_count"`
	HistoryPartsVerified int                 `json:"history_parts_verified"`
	IntegrityVerified    bool                `json:"integrity_verified"`
	AcceptanceEligible   bool                `json:"acceptance_eligible"`
}

// VerifyRestoredData is a separate, read-only verification pass. Its input has
// no source SQL/Kubernetes identity, restore writer or control-database handle.
// It derives expected rows from the authenticated archive, not target hashes.
func VerifyRestoredData(ctx context.Context, store ImmutableObjectStore, artifact PublishedArtifact, wrapper DataKeyWrapper, intent RestoreVerificationIntent, target RestoredDataReader, budget CaptureBudget) (out RestoredDataVerification, err error) {
	out = RestoredDataVerification{RunID: intent.RunID, BackupID: artifact.Manifest.Payload.BackupID}
	if !publicIDPattern.MatchString(intent.RunID) || len(intent.RunID) != 36 || intent.RunID[:4] != "sdr_" || intent.NormalizedAt.IsZero() || !restoreDatabasePattern.MatchString(target.Database) || target.MySQL == nil || target.Redis == nil || target.Objects == nil || wrapper == nil || budget.MaxBytes < 1<<20 || budget.MaxItems < 1 || len(intent.HistoryParts) != 2 {
		return out, errors.New("invalid restored-data verification intent")
	}
	if err = RequireReadOnlyVerifier(ctx, target.MySQL, target.Database); err != nil {
		return out, err
	}
	if err = VerifyPublishedArtifact(ctx, store, artifact, wrapper, budget.MaxBytes); err != nil {
		return out, err
	}
	root, e := os.OpenRoot(target.Directory)
	if e != nil {
		return out, e
	}
	defer root.Close()
	owner, e := root.Open(".restore-owner")
	if e != nil {
		return out, e
	}
	raw, e := io.ReadAll(io.LimitReader(owner, 128))
	owner.Close()
	if e != nil || string(raw) != intent.RunID {
		return out, errors.New("restore target ownership mismatch")
	}
	consume := func(name string, fn func(io.Reader) error) error {
		return ConsumePublishedPart(ctx, store, artifact, name, wrapper, budget.MaxBytes, fn)
	}
	var collector teamReportCollector
	err = consume("mysql", func(r io.Reader) error {
		pr, pw := io.Pipe()
		done := make(chan error, 1)
		go func() {
			_, e := NormalizeSQLSnapshot(ctx, r, pw, intent.NormalizedAt, func(_ SQLTable, change team.Change) error { collector.record(change); return nil })
			pw.CloseWithError(e)
			done <- e
		}()
		var e error
		out.SQL, e = VerifyMySQL(ctx, target.MySQL, pr, int64(budget.MaxItems))
		pr.CloseWithError(e)
		producer := <-done
		if e != nil {
			return e
		}
		return producer
	})
	if err != nil {
		return out, err
	}
	if err = requireBusinessSQLInventory(out.SQL); err != nil {
		return out, err
	}
	out.Team = collector.summary(true)
	err = consume("workspace", func(r io.Reader) error {
		var e error
		out.FileCount, e = VerifyFiles(ctx, r, filepath.Join(target.Directory, "workspace"), FileBudget{budget.MaxBytes, budget.MaxItems})
		return e
	})
	if err != nil {
		return out, err
	}
	err = consume("redis", func(r io.Reader) error {
		var e error
		out.RedisCount, e = VerifyRedisHistory(ctx, target.Redis, r, RedisBudget{MaxBytes: budget.MaxBytes, MaxItems: budget.MaxItems, MaxInventoryBytes: min(budget.MaxBytes/8, 64<<20)}, intent.RunID)
		return e
	})
	if err != nil {
		return out, err
	}
	err = consume("object_storage", func(r io.Reader) error {
		var e error
		out.ObjectCount, e = VerifyObjects(ctx, target.Objects, r, ObjectBudget{budget.MaxBytes, budget.MaxItems}, target.BucketMapping)
		return e
	})
	if err != nil {
		return out, err
	}
	seen := map[string]bool{}
	for _, part := range intent.HistoryParts {
		if (part.Name != "deployment_resources" && part.Name != "secret_bundle") || seen[part.Name] {
			return out, errors.New("invalid history verification inventory")
		}
		seen[part.Name] = true
		matched := false
		for _, original := range artifact.Manifest.Payload.Parts {
			if original.Name == part.Name && original.PlaintextBytes == part.PlaintextBytes && original.PlaintextSHA256 == part.PlaintextSHA256 {
				matched = true
			}
		}
		if !matched {
			return out, errors.New("history differs from signed source")
		}
		file, e := root.Open("encrypted-history/" + part.Name + ".enc")
		if e != nil {
			return out, e
		}
		e = OpenPart(ctx, file, io.Discard, artifact.Manifest.Payload, part, wrapper, budget.MaxBytes)
		file.Close()
		if e != nil {
			return out, e
		}
		out.HistoryPartsVerified++
	}
	out.IntegrityVerified = true
	return out, nil
}
