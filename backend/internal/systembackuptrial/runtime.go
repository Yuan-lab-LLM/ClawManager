package systembackuptrial

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"clawreef/internal/config"
	"clawreef/internal/db"
	"clawreef/internal/middleware"
	"clawreef/internal/repository"
	domain "clawreef/internal/systembackuptrial/records"

	"github.com/gin-gonic/gin"
	upper "github.com/upper/db/v4"
)

// RegisterRuntimeIdentity is only called after InitializeRuntime admits the
// sample deployment. The site runner checks this identity before any start.
func RegisterRuntimeIdentity(api *gin.RouterGroup, users repository.UserRepository, backupID, namespace string) {
	api.GET("/trial-runtime", middleware.Auth(), middleware.SetUserInfo(users), func(c *gin.Context) {
		user := middleware.GetCurrentUser(c)
		if user == nil || !user.IsActive {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{"backup_id": backupID, "namespace": namespace})
	})
}

// InitializeRuntime verifies the first isolated start against the sealed trial.
// Later starts independently check the admission on the same target and volume.
// The deployment owner maintains namespace, credential and network isolation.
func (s *Service) InitializeRuntime(ctx context.Context, cfg *config.Config, backupID string) (upper.Session, error) {
	if cfg == nil || !taskID.MatchString(backupID) || !strings.HasPrefix(cfg.Database.Database, "cm_drill_trial_") ||
		cfg.Auth.Enterprise.Enabled || cfg.Northbound.Enabled || cfg.IEISystem.Enabled ||
		db.SQLGateScope() != "" ||
		s.value("RESTORE_ISOLATED") != "true" {
		return nil, domain.ErrIsolation
	}
	for _, flag := range []string{
		"SYSTEM_BACKUP_READ_SESSIONS_ENABLED", "SYSTEM_BACKUP_READ_LIVE_ENABLED",
		"SYSTEM_BACKUP_CONFIG_MUTATION_ENABLED", "SYSTEM_BACKUP_V2_EXECUTION_ENABLED",
	} {
		if value := strings.TrimSpace(s.env(flag)); value != "" && value != "false" {
			return nil, domain.ErrIsolation
		}
	}
	placement, err := prepareRuntimePlacement(cfg, s.value("RESTORE_NAMESPACE"), s.value("RESTORE_STORAGE_CLASS"))
	if err != nil {
		return nil, err
	}
	readerRoot, err := s.readerRoot()
	if err != nil {
		return nil, err
	}
	defer readerRoot.Close()
	if readOnly, err := restoreMountReadOnly(readerRoot); err != nil || !readOnly {
		return nil, errConfig
	}
	store := storage{reader: readerRoot}
	key, err := s.key()
	if err != nil {
		return nil, err
	}
	defer clear(key)
	a, err := store.metadata(backupID)
	if err != nil {
		return nil, errReadback
	}
	bundle, err := store.snapshot(ctx, backupID, a, key)
	if err != nil {
		return nil, err
	}
	defer clear(bundle.hermes)
	defer clear(bundle.workspace)
	snapshot, m := bundle.snapshot, bundle.manifest
	uuid := s.value("RESTORE_UUID")
	if uuid == "" || uuid == m.SourceUUID {
		return nil, domain.ErrIsolation
	}
	reader, readDatabase, err := s.database("RESTORE_READ")
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	if readDatabase != cfg.Database.Database {
		return nil, domain.ErrIsolation
	}
	target := domain.DrillTarget{Database: readDatabase, ServerUUID: uuid, CheckIsolation: func(ctx context.Context) error {
		return ctx.Err()
	}}
	if err := domain.CheckTrialTargetReader(ctx, reader, target); err != nil {
		return nil, err
	}
	workspace := cfg.Runtime.WorkspaceRoot
	if !filepath.IsAbs(workspace) || filepath.Clean(workspace) != workspace || filepath.Dir(workspace) == workspace {
		return nil, errRuntimeAdmission
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return nil, errRuntimeAdmission
	}
	defer root.Close()
	if m.Hermes != nil && s.value("HERMES_HISTORY_TARGET_ROOT") != workspace {
		return nil, errHistory
	}
	if m.Workspace != nil && s.value("WORKSPACE_FILES_TARGET_ROOT") != workspace {
		return nil, errWorkspaceFiles
	}
	pool, runtimeDatabase, err := s.database("RUNTIME")
	if err != nil {
		return nil, err
	}
	adopted := false
	defer func() {
		if !adopted {
			_ = pool.Close()
		}
	}()
	if runtimeDatabase != readDatabase {
		return nil, domain.ErrIsolation
	}
	if err := domain.CheckTrialRuntimeWriter(ctx, pool, target); err != nil {
		return nil, err
	}
	pool.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	pool.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	pool.SetConnMaxLifetime(cfg.Database.ConnMaxLifetime)
	pool.SetConnMaxIdleTime(cfg.Database.ConnMaxIdleTime)
	proof := runtimeAdmissionProof(key, backupID, a.Receipt.SHA256, runtimeDatabase, uuid, placement.Namespace, placement.StorageClass, workspace)
	session, err := db.InitializeVerifiedTrialRuntime(ctx, pool, runtimeDatabase, uuid, func() error {
		return admitTrialRuntime(ctx, reader, pool, root, proof, func() error {
			if err := domain.VerifyRestoredTrial(ctx, reader, target, snapshot, placement); err != nil {
				return err
			}
			if m.Hermes != nil {
				history, err := s.historyTarget()
				if err != nil {
					return err
				}
				if err := history.run(ctx, "verify", *m.Hermes, bundle.hermes); err != nil {
					return err
				}
			}
			if m.Workspace != nil {
				return verifyWorkspaceFiles(ctx, root, *m.Workspace, bundle.workspace)
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	adopted = true
	return session, nil
}

func prepareRuntimePlacement(cfg *config.Config, namespace, storageClass string) (domain.Placement, error) {
	if !strings.HasPrefix(namespace, "cm-drill-") || storageClass == "" ||
		cfg.Kubernetes.Common.Namespace != namespace || cfg.Runtime.Namespace != namespace ||
		cfg.GetStorageClass() != storageClass || cfg.GetInstanceStorageClass() != storageClass ||
		cfg.GetWorkspaceStorageClass() != storageClass {
		return domain.Placement{}, domain.ErrIsolation
	}
	// The control-plane leader starts the runtime scheduler. Its default
	// namespace is the ordinary installation, even when Runtime.Namespace
	// points at a drill. Keep both leases inside the admitted sample namespace.
	cfg.LeaderElection.Namespace = namespace
	return domain.Placement{Namespace: namespace, StorageClass: storageClass}, nil
}
