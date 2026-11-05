package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"context"
	"fmt"
	"path/filepath"

	"clawreef/internal/services/k8s"
)

func WithRuntimeSandboxProfile(profile string) InstanceServiceOption {
	return func(s *instanceService) { s.runtimeSandboxProfile = profile }
}

func (s *instanceService) allocateRuntimeWorkspace(root, runtimeType string, userID, instanceID int) (string, error) {
	if s.runtimeSandboxProfile == "" {
		return ensureRuntimeWorkspaceDirectories(root, runtimeType, userID, instanceID)
	}
	if s.runtimeSandboxProfile != k8s.RuntimeNFSRootlessProfile {
		return "", fmt.Errorf("unknown workspace sandbox profile")
	}
	if _, ok := NormalizeV2RuntimeType(runtimeType); !ok || userID <= 0 || instanceID <= 0 || !filepath.IsAbs(root) || filepath.Clean(root) == "/" {
		return "", fmt.Errorf("invalid NFS instance workspace identity")
	}
	// Reserve the logical instance path only. The Runtime management identity
	// provisions and validates the real directory before any native operation.
	return RuntimeWorkspacePathWithRoot(filepath.Clean(root), runtimeType, userID, instanceID), nil
}

func WithWorkspaceWriteRepository(writes repository.WorkspaceWriteRepository) InstanceServiceOption {
	return func(s *instanceService) { s.workspaceWrites = writes }
}

func (s *instanceService) verifyOriginalWorkspace(ctx context.Context, runtimeType string, instance *models.Instance) error {
	if s.runtimeSandboxProfile == k8s.RuntimeNFSRootlessProfile {
		return ProbeNFSInstanceWorkspace(ctx, s.runtimeWorkspaceRoot(), runtimeType, instance)
	}
	return verifyOriginalIdleWorkspace(s.runtimeWorkspaceRoot(), runtimeType, instance)
}
