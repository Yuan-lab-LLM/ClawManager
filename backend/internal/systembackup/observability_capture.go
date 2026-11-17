package systembackup

import (
	"context"
	"errors"
	"os"
)

// A migrated installation has an independently durable WAL even when SQL has
// not yet received its events. SQL row counts cannot prove that WAL is empty.
func validateObservationCaptureSources(ctx context.Context, source CaptureSources, includeSecrets bool) error {
	if source.TeamDataOnly {
		return nil
	}
	var exists int
	if err := source.MySQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name='observability_admissions'`).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	return validateObservationCaptureCoverage(source, includeSecrets)
}
func validateObservationCaptureCoverage(source CaptureSources, includeSecrets bool) error {
	found := false
	for _, root := range source.Workspace {
		if root.ID == "observability" {
			info, err := os.Lstat(root.Path)
			if err != nil {
				return err
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return errors.New("observability backup root must be a real journal directory")
			}
			found = true
		}
	}
	if !found {
		return errors.New("full backup requires the dedicated observability journal PVC as FileRoot observability")
	}
	if includeSecrets {
		for _, selection := range source.Secrets {
			if selection.Resource == "secrets" && selection.Name == "clawmanager-observability-key-v1" && selection.UID != "" {
				return nil
			}
		}
		return errors.New("full backup requires the immutable observability master key in the encrypted secret bundle")
	}
	return nil
}
