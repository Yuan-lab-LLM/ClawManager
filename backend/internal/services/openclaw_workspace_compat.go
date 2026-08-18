package services

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// ensureOpenClawPluginLayoutCompatibility bridges the project-local npm layout
// used by newer OpenClaw releases to the global npm layout used by older
// releases. Existing global packages are authoritative and are never replaced.
func ensureOpenClawPluginLayoutCompatibility(workspacePath string, uid, gid int) error {
	openClawHome := filepath.Join(workspacePath, "home", ".openclaw")
	npmRoot := filepath.Join(openClawHome, "npm")
	projectsRoot := filepath.Join(npmRoot, "projects")
	globalRoot := filepath.Join(npmRoot, "node_modules")

	projects, err := os.ReadDir(projectsRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read OpenClaw npm projects: %w", err)
	}

	globalInfo, err := os.Lstat(globalRoot)
	globalRootMissing := errors.Is(err, os.ErrNotExist)
	if err == nil {
		if globalInfo.Mode()&os.ModeSymlink != 0 {
			// A whole-directory compatibility link is already configured.
			return nil
		}
		if !globalInfo.IsDir() {
			return fmt.Errorf("OpenClaw global node_modules is not a directory")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect OpenClaw global node_modules: %w", err)
	}

	packages := map[string][]string{}
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		projectModules := filepath.Join(projectsRoot, project.Name(), "node_modules")
		if err := collectOpenClawProjectPackages(projectModules, packages); err != nil {
			return fmt.Errorf("inspect OpenClaw npm project %q: %w", project.Name(), err)
		}
	}
	if len(packages) == 0 {
		return nil
	}
	if err := os.MkdirAll(globalRoot, 0o755); err != nil {
		return fmt.Errorf("create OpenClaw global node_modules: %w", err)
	}
	if globalRootMissing {
		if err := chownOpenClawCompatibilityPath(globalRoot, uid, gid, false); err != nil {
			return fmt.Errorf("set OpenClaw global node_modules ownership: %w", err)
		}
	}

	packageNames := make([]string, 0, len(packages))
	for packageName := range packages {
		packageNames = append(packageNames, packageName)
	}
	sort.Strings(packageNames)
	for _, packageName := range packageNames {
		linkPath := filepath.Join(globalRoot, filepath.FromSlash(packageName))
		exists, err := openClawCompatibilityPathExists(linkPath)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		targets := packages[packageName]
		if len(targets) != 1 {
			return fmt.Errorf("OpenClaw plugin %q exists in multiple npm projects; cannot choose a rollback-compatible target", packageName)
		}
		if err := createOpenClawCompatibilityLink(linkPath, targets[0], globalRoot, uid, gid); err != nil {
			return fmt.Errorf("link OpenClaw plugin %q: %w", packageName, err)
		}
	}
	return nil
}

func collectOpenClawProjectPackages(nodeModulesRoot string, packages map[string][]string) error {
	entries, err := os.ReadDir(nodeModulesRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if strings.HasPrefix(name, "@") && entry.IsDir() {
			scopeRoot := filepath.Join(nodeModulesRoot, name)
			scopedEntries, err := os.ReadDir(scopeRoot)
			if err != nil {
				return err
			}
			for _, scopedEntry := range scopedEntries {
				if strings.HasPrefix(scopedEntry.Name(), ".") {
					continue
				}
				packageName := name + "/" + scopedEntry.Name()
				packagePath := filepath.Join(scopeRoot, scopedEntry.Name())
				plugin, err := isOpenClawPluginPackage(packagePath)
				if err != nil {
					return err
				}
				if plugin {
					packages[packageName] = append(packages[packageName], packagePath)
				}
			}
			continue
		}
		packagePath := filepath.Join(nodeModulesRoot, name)
		plugin, err := isOpenClawPluginPackage(packagePath)
		if err != nil {
			return err
		}
		if plugin {
			packages[name] = append(packages[name], packagePath)
		}
	}
	return nil
}

func isOpenClawPluginPackage(packagePath string) (bool, error) {
	manifestPath := filepath.Join(packagePath, "openclaw.plugin.json")
	info, err := os.Stat(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect OpenClaw plugin manifest %q: %w", manifestPath, err)
	}
	return !info.IsDir(), nil
}

func openClawCompatibilityPathExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("inspect OpenClaw compatibility path %q: %w", path, err)
}

func createOpenClawCompatibilityLink(linkPath, targetPath, globalRoot string, uid, gid int) error {
	linkParent := filepath.Dir(linkPath)
	if linkParent != globalRoot {
		parentInfo, err := os.Lstat(linkParent)
		if err == nil && parentInfo.Mode()&os.ModeSymlink != 0 {
			// Preserve an existing whole-scope link rather than writing through it.
			return nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parentMissing := errors.Is(err, os.ErrNotExist)
		if err := os.MkdirAll(linkParent, 0o755); err != nil {
			return err
		}
		if parentMissing {
			if err := chownOpenClawCompatibilityPath(linkParent, uid, gid, false); err != nil {
				return err
			}
		}
	}
	relativeTarget, err := filepath.Rel(linkParent, targetPath)
	if err != nil {
		return err
	}
	if err := os.Symlink(relativeTarget, linkPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	}
	return chownOpenClawCompatibilityPath(linkPath, uid, gid, true)
}

func chownOpenClawCompatibilityPath(path string, uid, gid int, symlink bool) error {
	if runtime.GOOS == "windows" || uid <= 0 || gid <= 0 {
		return nil
	}
	var err error
	if symlink {
		err = os.Lchown(path, uid, gid)
	} else {
		err = os.Chown(path, uid, gid)
	}
	// NFS root-squash can reject chown even though the link is readable and the
	// parent directory is usable. Ownership is therefore best-effort only.
	if errors.Is(err, os.ErrPermission) {
		return nil
	}
	return err
}

// quarantineCorruptLegacyOpenClawTaskState isolates only the optional 5.4 task
// history database after the runtime has explicitly reported SQLite corruption.
// The files stay inside the instance workspace so an operator can recover them.
func quarantineCorruptLegacyOpenClawTaskState(workspacePath string, generation int, runtimeError *string) (bool, error) {
	if strings.TrimSpace(workspacePath) == "" || runtimeError == nil || !isSQLiteCorruptionError(*runtimeError) {
		return false, nil
	}
	openClawHome := filepath.Join(workspacePath, "home", ".openclaw")
	legacyTasksRoot := filepath.Join(openClawHome, "tasks")
	fileNames := []string{"runs.sqlite", "runs.sqlite-wal", "runs.sqlite-shm"}
	existing := make([]string, 0, len(fileNames))
	for _, fileName := range fileNames {
		path := filepath.Join(legacyTasksRoot, fileName)
		if _, err := os.Lstat(path); err == nil {
			existing = append(existing, fileName)
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("inspect legacy task database %q: %w", path, err)
		}
	}
	if len(existing) == 0 {
		return false, nil
	}

	quarantineRoot := filepath.Join(openClawHome, "quarantine")
	baseName := "legacy-tasks-generation-" + strconv.Itoa(generation)
	quarantinePath, err := nextAvailableQuarantinePath(quarantineRoot, baseName)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(quarantinePath, 0o750); err != nil {
		return false, fmt.Errorf("create quarantine directory: %w", err)
	}

	moved := make([]string, 0, len(existing))
	for _, fileName := range existing {
		source := filepath.Join(legacyTasksRoot, fileName)
		destination := filepath.Join(quarantinePath, fileName)
		if err := os.Rename(source, destination); err != nil {
			var rollbackErrs []error
			for index := len(moved) - 1; index >= 0; index-- {
				movedName := moved[index]
				if rollbackErr := os.Rename(filepath.Join(quarantinePath, movedName), filepath.Join(legacyTasksRoot, movedName)); rollbackErr != nil {
					rollbackErrs = append(rollbackErrs, rollbackErr)
				}
			}
			_ = os.Remove(quarantinePath)
			return false, errors.Join(append([]error{fmt.Errorf("move %s to quarantine: %w", fileName, err)}, rollbackErrs...)...)
		}
		moved = append(moved, fileName)
	}
	return true, nil
}

func isSQLiteCorruptionError(message string) bool {
	normalized := strings.ToLower(message)
	for _, marker := range []string{
		"database disk image is malformed",
		"database is corrupt",
		"database corruption",
		"file is not a database",
		"malformed database schema",
		"sqlite_corrupt",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func nextAvailableQuarantinePath(root, baseName string) (string, error) {
	for suffix := 0; suffix < 1000; suffix++ {
		name := baseName
		if suffix > 0 {
			name += "-" + strconv.Itoa(suffix)
		}
		candidate := filepath.Join(root, name)
		_, err := os.Lstat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
		if err != nil {
			return "", fmt.Errorf("inspect quarantine path: %w", err)
		}
	}
	return "", fmt.Errorf("too many quarantine directories for generation")
}
