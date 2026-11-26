package systembackuptrial

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

type restorePreflight struct {
	BackupID         string                `json:"backup_id"`
	Readback         bool                  `json:"readback_verified"`
	CiphertextSHA256 string                `json:"ciphertext_sha256,omitempty"`
	Missing          string                `json:"missing,omitempty"`
	Counts           map[string]int        `json:"counts,omitempty"`
	Hermes           *historyReceipt       `json:"hermes_history,omitempty"`
	Files            *workspaceFileReceipt `json:"workspace_files,omitempty"`
	WorkspaceDir     *workspaceSelection   `json:"workspace_directory,omitempty"`
}

func (s *Service) handleRestorePreflight(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id := c.Param("id")
	if !taskID.MatchString(id) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_backup_id"})
		return
	}
	if missing := s.environment(c.Request.Context(), false).missing("target"); missing != "" {
		c.JSON(http.StatusOK, restorePreflight{BackupID: id, Missing: missing})
		return
	}
	s.mu.Lock()
	if s.checking || (s.task != nil && s.task.Status == "running") {
		s.mu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"code": "trial_task_running"})
		return
	}
	s.checking = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.checking = false
		s.mu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()
	c.JSON(http.StatusOK, s.checkRestoreArtifact(ctx, id))
}

func (s *Service) readRestoreArtifact(ctx context.Context, id string) (restoredArtifact, error) {
	key, err := s.key()
	if err != nil {
		return restoredArtifact{}, err
	}
	defer clear(key)
	root, err := s.readerRoot()
	if err != nil {
		return restoredArtifact{}, err
	}
	defer root.Close()
	store := storage{reader: root}
	a, err := store.metadata(id)
	if err != nil {
		return restoredArtifact{}, err
	}
	return store.snapshot(ctx, id, a, key)
}

type restoreContentTargets struct {
	history   historyTarget
	workspace *os.Root
}

func (t restoreContentTargets) close() {
	if t.workspace != nil {
		_ = t.workspace.Close()
	}
}

func (s *Service) restoreContentTargets(ctx context.Context, bundle restoredArtifact) (restoreContentTargets, error) {
	var target restoreContentTargets
	m := bundle.manifest
	if m.Hermes != nil {
		worker, err := s.historyTarget()
		if err != nil {
			return target, err
		}
		probe, cancel := context.WithTimeout(ctx, 10*time.Second)
		err = worker.ready(probe)
		cancel()
		if err != nil {
			return target, err
		}
		target.history = worker
	}
	if m.Workspace != nil {
		mount, err := s.openWorkspaceTarget()
		if err != nil {
			return target, err
		}
		if err := checkWorkspaceDestination(ctx, mount, *m.Workspace, bundle.workspace); err != nil {
			mount.Close()
			return target, err
		}
		target.workspace = mount
	}
	return target, nil
}

func (s *Service) checkRestoreArtifact(ctx context.Context, id string) restorePreflight {
	result := restorePreflight{BackupID: id}
	uuid := s.value("RESTORE_UUID")
	if s.value("RESTORE_ISOLATED") != "true" || uuid == "" {
		result.Missing = "restore_isolation"
		return result
	}
	bundle, err := s.readRestoreArtifact(ctx, id)
	if err != nil {
		switch {
		case errors.Is(err, errConfig):
			result.Missing = "restore_artifact_configuration"
		case errors.Is(err, os.ErrNotExist):
			result.Missing = "restore_artifact_missing"
		default:
			result.Missing = "restore_artifact_readback"
		}
		return result
	}
	defer clear(bundle.hermes)
	defer clear(bundle.workspace)
	m := bundle.manifest
	if uuid == m.SourceUUID {
		result.Missing = "restore_isolation"
		return result
	}
	result.Readback, result.Counts = true, m.Counts
	result.CiphertextSHA256 = bundle.ciphertextSHA256
	if m.Hermes != nil {
		result.Hermes = &m.Hermes.historyReceipt
	}
	if m.Workspace != nil {
		result.Files = &m.Workspace.workspaceFileReceipt
		result.WorkspaceDir = &m.Workspace.workspaceSelection
	}
	target, err := s.restoreContentTargets(ctx, bundle)
	if err != nil {
		result.Missing = category(err)
		return result
	}
	defer target.close()
	if m.Hermes != nil {
		if err := target.history.run(ctx, "preflight", *m.Hermes, bundle.hermes); err != nil {
			result.Missing = "hermes_history"
		}
	}
	return result
}
