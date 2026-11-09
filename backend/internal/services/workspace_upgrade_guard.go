package services

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

// Reads retain their existing behavior. Writes hold the SAME lifecycle lock as
// upgrade reservation until all filesystem/remote-exec work has returned.
// This protects platform entrypoints, not arbitrary external workspace writers.
type guardedWorkspaceFileService struct {
	WorkspaceFileService
	instances repository.InstanceRepository
	remote    bool
}

func WithWorkspaceUpgradeGuard(files WorkspaceFileService, instances repository.InstanceRepository, remote bool) WorkspaceFileService {
	return &guardedWorkspaceFileService{WorkspaceFileService: files, instances: instances, remote: remote}
}

func (s *guardedWorkspaceFileService) acquire(ctx context.Context, scope WorkspaceFileScope) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.WorkspaceFileService == nil || s.instances == nil {
		return nil, fmt.Errorf("workspace write guard is not configured")
	}
	if _, ok := s.instances.(instanceLifecycleLocker); !ok {
		return nil, fmt.Errorf("workspace write guard requires persistent lifecycle locking")
	}
	if scope.InstanceID <= 0 || scope.UserID <= 0 || scope.RuntimeGeneration < 0 {
		return nil, models.ErrRuntimeUpgradeConflict
	}
	release, err := acquireInstanceLifecycleOperation(ctx, s.instances, scope.InstanceID)
	if err != nil {
		return nil, err
	}
	instance, err := s.instances.GetByID(scope.InstanceID)
	if err != nil {
		release()
		return nil, err
	}
	valid := instance != nil && instance.ID == scope.InstanceID && instance.UserID == scope.UserID && instance.RuntimeGeneration == scope.RuntimeGeneration && instance.Status != "deleting"
	if valid {
		remote := strings.EqualFold(strings.TrimSpace(instance.InstanceMode), InstanceModePro) || strings.EqualFold(strings.TrimSpace(instance.RuntimeType), RuntimeBackendDesktop)
		valid = remote == s.remote
		if s.remote {
			valid = valid && scope.WorkspacePath == runtimeWorkspaceBaseDir
		} else {
			valid = valid && instance.WorkspacePath != nil && strings.TrimSpace(*instance.WorkspacePath) != "" && strings.TrimSpace(scope.WorkspacePath) != "" && filepath.Clean(*instance.WorkspacePath) == filepath.Clean(scope.WorkspacePath)
		}
	}
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	if !valid {
		release()
		return nil, models.ErrRuntimeUpgradeConflict
	}
	return release, nil
}

func (s *guardedWorkspaceFileService) Upload(ctx context.Context, scope WorkspaceFileScope, dir, name string, r io.Reader, size int64) (*WorkspaceEntry, error) {
	release, err := s.acquire(ctx, scope)
	if err != nil {
		return nil, err
	}
	defer release()
	return s.WorkspaceFileService.Upload(ctx, scope, dir, name, r, size)
}
func (s *guardedWorkspaceFileService) Mkdir(ctx context.Context, scope WorkspaceFileScope, path string) (*WorkspaceEntry, error) {
	release, err := s.acquire(ctx, scope)
	if err != nil {
		return nil, err
	}
	defer release()
	return s.WorkspaceFileService.Mkdir(ctx, scope, path)
}
func (s *guardedWorkspaceFileService) Rename(ctx context.Context, scope WorkspaceFileScope, oldPath, newPath string) (*WorkspaceEntry, error) {
	release, err := s.acquire(ctx, scope)
	if err != nil {
		return nil, err
	}
	defer release()
	return s.WorkspaceFileService.Rename(ctx, scope, oldPath, newPath)
}
func (s *guardedWorkspaceFileService) Delete(ctx context.Context, scope WorkspaceFileScope, path string) error {
	release, err := s.acquire(ctx, scope)
	if err != nil {
		return err
	}
	defer release()
	return s.WorkspaceFileService.Delete(ctx, scope, path)
}
