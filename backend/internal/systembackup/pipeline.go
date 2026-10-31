package systembackup

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"errors"
	"io"
	"os"
	"time"

	"github.com/minio/minio-go/v7"
	"k8s.io/client-go/dynamic"
)

type CaptureSources struct {
	MySQL                   *sql.DB
	Redis                   *RedisClient
	Objects                 *minio.Client
	ObjectBuckets           []string
	Workspace               []FileRoot
	DeploymentClient        dynamic.Interface
	SecretClient            dynamic.Interface
	Namespace, NamespaceUID string
	Deployments, Secrets    []KubernetesSelection
	TeamDataOnly            bool
	Progress                func(string)
	FileAliases             map[string]string
	MetadataOnlyFiles       func(int)
}
type CaptureBudget struct {
	MaxBytes int64
	MaxItems int
}

// CaptureFunctionalBundle connects all six real data adapters. It deliberately
// cannot issue an acceptance artifact: full writer quiescence, checkpoint,
// identity and permission proofs belong to the independent acceptance runner.
// Every staged file is encrypted; a failed part leaves no valid signed bundle.
func CaptureFunctionalBundle(ctx context.Context, stage, origin, id string, source CaptureSources, b CaptureBudget, wrapper DataKeyWrapper, signing ed25519.PrivateKey, signVersion string) (SignedManifest, error) {
	var zero SignedManifest
	if !validPartIdentity(origin, id, "mysql") || b.MaxBytes < 1<<20 || b.MaxItems < 1 || source.MySQL == nil || source.Redis == nil || source.Objects == nil || source.DeploymentClient == nil || source.SecretClient == nil || len(source.ObjectBuckets) == 0 || len(source.Workspace) == 0 {
		return zero, errors.New("incomplete six-part capture configuration")
	}
	entries, e := os.ReadDir(stage)
	if e != nil || len(entries) != 0 {
		return zero, errors.New("fresh encrypted staging required")
	}
	validate := ValidateSchema
	if source.TeamDataOnly {
		validate = ValidateTeamDataSchema
	}
	if e = validate(ctx, source.MySQL); e != nil {
		return zero, e
	}
	tables, e := snapshotBusinessTables(ctx, source.MySQL, source.TeamDataOnly)
	if e != nil {
		return zero, e
	}
	m := Manifest{Schema: "system-backup.v1", Contract: ContractVersion, BackupID: id, OriginInstallationID: origin, Purpose: "functional_test", RegistryHash: RegistryHash(), CapturedAt: time.Now().UTC(), Consistency: "best_effort"}
	if source.TeamDataOnly {
		m.Purpose = "team_data"
		m.ExcludedSQLTables = teamDataExcludedTables()
	}
	adapters := captureAdapters(source, b, tables)
	remaining := b.MaxBytes
	for _, adapter := range adapters {
		if source.Progress != nil {
			source.Progress(adapter.name)
		}
		if remaining < 1 {
			return zero, errors.New("capacity_exceeded")
		}
		part, e := CapturePart(ctx, stage, origin, id, adapter.name, remaining, wrapper, adapter.capture)
		if e != nil {
			return zero, e
		}
		remaining -= part.CiphertextBytes
		if remaining < 0 {
			return zero, errors.New("capacity_exceeded")
		}
		m.Parts = append(m.Parts, part)
	}
	return SignManifest(m, signing, signVersion)
}

// ConsumePublishedPart propagates authentication/EOF failures through the pipe
// before a transactional consumer can commit. Early consumer failure closes
// the producer and waits for it, avoiding orphaned goroutines and plaintext.
func ConsumePublishedPart(ctx context.Context, store ImmutableObjectStore, artifact PublishedArtifact, name string, wrapper DataKeyWrapper, maxBytes int64, consume func(io.Reader) error) error {
	if consume == nil {
		return errors.New("part consumer required")
	}
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() {
		e := OpenPublishedPart(ctx, store, artifact, name, wrapper, writer, maxBytes)
		writer.CloseWithError(e)
		done <- e
	}()
	e := consume(reader)
	reader.CloseWithError(e)
	producerError := <-done
	if e != nil {
		return e
	}
	return producerError
}

type captureAdapter struct {
	name    string
	capture func(context.Context, io.Writer) error
}

func captureAdapters(source CaptureSources, b CaptureBudget, tables []string) []captureAdapter {
	objectBudget := ObjectBudget{b.MaxBytes, b.MaxItems}
	return []captureAdapter{
		{"mysql", func(ctx context.Context, w io.Writer) error {
			if source.TeamDataOnly {
				_, e := ExportTeamDataMySQL(ctx, source.MySQL, w, tables)
				return e
			}
			_, e := ExportMySQL(ctx, source.MySQL, w, tables)
			return e
		}},
		{"redis", func(ctx context.Context, w io.Writer) error {
			if source.TeamDataOnly {
				return CaptureTeamDataRedisLogical(ctx, source.Redis, w, RedisBudget{MaxBytes: b.MaxBytes, MaxItems: b.MaxItems, MaxInventoryBytes: min(b.MaxBytes/8, 64<<20)})
			}
			return CaptureRedisLogical(ctx, source.Redis, w, RedisBudget{MaxBytes: b.MaxBytes, MaxItems: b.MaxItems, MaxInventoryBytes: min(b.MaxBytes/8, 64<<20)})
		}},
		{"object_storage", func(ctx context.Context, w io.Writer) error {
			if source.TeamDataOnly {
				return CaptureTeamDataObjects(ctx, source.Objects, w, source.ObjectBuckets, objectBudget)
			}
			return CaptureObjects(ctx, source.Objects, w, source.ObjectBuckets, objectBudget)
		}},
		{"workspace", func(ctx context.Context, w io.Writer) error {
			records, e := captureFilesWithPolicy(ctx, w, source.Workspace, FileBudget{b.MaxBytes, b.MaxItems}, source.FileAliases, source.TeamDataOnly)
			if e == nil && source.MetadataOnlyFiles != nil {
				count := 0
				for _, r := range records {
					if r.MetadataOnly {
						count++
					}
				}
				source.MetadataOnlyFiles(count)
			}
			return e
		}},
		{"deployment_resources", func(ctx context.Context, w io.Writer) error {
			return CaptureKubernetesResources(ctx, source.DeploymentClient, w, source.Namespace, source.NamespaceUID, "deployment_resources", source.Deployments, objectBudget)
		}},
		{"secret_bundle", func(ctx context.Context, w io.Writer) error {
			return CaptureKubernetesResources(ctx, source.SecretClient, w, source.Namespace, source.NamespaceUID, "secret_bundle", source.Secrets, objectBudget)
		}},
	}
}
