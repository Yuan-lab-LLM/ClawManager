package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
)

func hostPathWorkspaceScanEnabled() bool {
	client := k8s.GetClient()
	return client != nil && client.HostPathFallbackEnabled
}

func instancePersistentHostPath(userID, instanceID int) (string, bool) {
	if !hostPathWorkspaceScanEnabled() || userID <= 0 || instanceID <= 0 {
		return "", false
	}
	hostPathPrefix := "/data/clawreef"
	if client := k8s.GetClient(); client != nil && strings.TrimSpace(client.HostPathPrefix) != "" {
		hostPathPrefix = strings.TrimSpace(client.HostPathPrefix)
	}
	return filepath.Join(hostPathPrefix, fmt.Sprintf("user-%d", userID), fmt.Sprintf("instance-%d", instanceID)), true
}

func proDesktopWorkspaceScanEligible(instance *models.Instance) bool {
	if instance == nil || isLiteRuntimeInstance(instance) {
		return false
	}
	if v2Type, ok := v2RuntimeTypeForInstance(instance); ok && strings.TrimSpace(v2Type) != "" {
		return false
	}
	return supportsManagedRuntimeIntegration(instance.Type)
}

func EnsureInstanceWorkspacePathForServerScan(ctx context.Context, repo repository.InstanceRepository, instance *models.Instance) error {
	if repo == nil || instance == nil {
		return nil
	}
	if instance.WorkspacePath != nil && strings.TrimSpace(*instance.WorkspacePath) != "" {
		return nil
	}
	if !proDesktopWorkspaceScanEligible(instance) {
		return nil
	}
	hostPath, ok := instancePersistentHostPath(instance.UserID, instance.ID)
	if !ok {
		return nil
	}
	if _, err := os.Stat(hostPath); err != nil {
		return nil
	}
	if err := repo.SetWorkspacePath(ctx, instance.ID, hostPath); err != nil {
		return fmt.Errorf("failed to persist pro desktop workspace path: %w", err)
	}
	instance.WorkspacePath = &hostPath
	return nil
}
