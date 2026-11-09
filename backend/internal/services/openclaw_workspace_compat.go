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
	root, err := openOpenClawCompatibilityRoot(workspacePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	openClawHome := filepath.Join("home", ".openclaw")
	npmRoot := filepath.Join(openClawHome, "npm")
	projectsRoot := filepath.Join(npmRoot, "projects")
	globalRoot := filepath.Join(npmRoot, "node_modules")

	projects, err := readOpenClawCompatibilityDir(root, projectsRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read OpenClaw npm projects: %w", err)
	}

	globalInfo, err := root.Lstat(globalRoot)
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
		if err := collectOpenClawProjectPackages(root, projectModules, packages); err != nil {
			return fmt.Errorf("inspect OpenClaw npm project %q: %w", project.Name(), err)
		}
	}
	if len(packages) == 0 {
		return nil
	}
	if err := root.MkdirAll(globalRoot, 0o755); err != nil {
		return fmt.Errorf("create OpenClaw global node_modules: %w", err)
	}
	if globalRootMissing {
		if err := chownOpenClawCompatibilityPath(root, globalRoot, uid, gid, false); err != nil {
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
		exists, err := openClawCompatibilityPathExists(root, linkPath)
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
		if err := createOpenClawCompatibilityLink(root, linkPath, targets[0], globalRoot, uid, gid); err != nil {
			return fmt.Errorf("link OpenClaw plugin %q: %w", packageName, err)
		}
	}
	return nil
}

func collectOpenClawProjectPackages(root *os.Root, nodeModulesRoot string, packages map[string][]string) error {
	entries, err := readOpenClawCompatibilityDir(root, nodeModulesRoot)
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
			scopedEntries, err := readOpenClawCompatibilityDir(root, scopeRoot)
			if err != nil {
				return err
			}
			for _, scopedEntry := range scopedEntries {
				if strings.HasPrefix(scopedEntry.Name(), ".") {
					continue
				}
				packageName := name + "/" + scopedEntry.Name()
				packagePath := filepath.Join(scopeRoot, scopedEntry.Name())
				plugin, err := isOpenClawPluginPackage(root, packagePath)
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
		plugin, err := isOpenClawPluginPackage(root, packagePath)
		if err != nil {
			return err
		}
		if plugin {
			packages[name] = append(packages[name], packagePath)
		}
	}
	return nil
}

func isOpenClawPluginPackage(root *os.Root, packagePath string) (bool, error) {
	info, err := root.Lstat(packagePath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect OpenClaw plugin package %q: %w", packagePath, err)
	}
	// Project SDK dependencies may link into the immutable image. Only real
	// package directories are candidates for new global compatibility links.
	if !info.IsDir() {
		return false, nil
	}
	manifestPath := filepath.Join(packagePath, "openclaw.plugin.json")
	info, err = root.Stat(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect OpenClaw plugin manifest %q: %w", manifestPath, err)
	}
	return !info.IsDir(), nil
}

func openClawCompatibilityPathExists(root *os.Root, path string) (bool, error) {
	_, err := root.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("inspect OpenClaw compatibility path %q: %w", path, err)
}

func createOpenClawCompatibilityLink(root *os.Root, linkPath, targetPath, globalRoot string, uid, gid int) error {
	linkParent := filepath.Dir(linkPath)
	if linkParent != globalRoot {
		parentInfo, err := root.Lstat(linkParent)
		if err == nil && parentInfo.Mode()&os.ModeSymlink != 0 {
			// Preserve an existing whole-scope link rather than writing through it.
			return nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parentMissing := errors.Is(err, os.ErrNotExist)
		if err := root.MkdirAll(linkParent, 0o755); err != nil {
			return err
		}
		if parentMissing {
			if err := chownOpenClawCompatibilityPath(root, linkParent, uid, gid, false); err != nil {
				return err
			}
		}
	}
	relativeTarget, err := filepath.Rel(linkParent, targetPath)
	if err != nil {
		return err
	}
	if err := root.Symlink(relativeTarget, linkPath); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return err
	}
	return chownOpenClawCompatibilityPath(root, linkPath, uid, gid, true)
}

func chownOpenClawCompatibilityPath(root *os.Root, path string, uid, gid int, symlink bool) error {
	if runtime.GOOS == "windows" || uid <= 0 || gid <= 0 {
		return nil
	}
	var err error
	if symlink {
		err = root.Lchown(path, uid, gid)
	} else {
		file, openErr := root.Open(path)
		if openErr != nil {
			return openErr
		}
		defer file.Close()
		err = file.Chown(uid, gid)
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
	root, err := openOpenClawCompatibilityRoot(workspacePath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer root.Close()
	openClawHome := filepath.Join("home", ".openclaw")
	legacyTasksRoot := filepath.Join(openClawHome, "tasks")
	fileNames := []string{"runs.sqlite", "runs.sqlite-wal", "runs.sqlite-shm"}
	existing := make([]string, 0, len(fileNames))
	for _, fileName := range fileNames {
		path := filepath.Join(legacyTasksRoot, fileName)
		if _, err := root.Lstat(path); err == nil {
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
	quarantinePath, err := nextAvailableQuarantinePath(root, quarantineRoot, baseName)
	if err != nil {
		return false, err
	}

	moved := make([]string, 0, len(existing))
	for _, fileName := range existing {
		source := filepath.Join(legacyTasksRoot, fileName)
		destination := filepath.Join(quarantinePath, fileName)
		if err := root.Rename(source, destination); err != nil {
			var rollbackErrs []error
			for index := len(moved) - 1; index >= 0; index-- {
				movedName := moved[index]
				if rollbackErr := root.Rename(filepath.Join(quarantinePath, movedName), filepath.Join(legacyTasksRoot, movedName)); rollbackErr != nil {
					rollbackErrs = append(rollbackErrs, rollbackErr)
				}
			}
			_ = root.Remove(quarantinePath)
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

// Atomically reserve a fresh directory: never accept an attacker-created link
// between an existence check and the first database rename.
func nextAvailableQuarantinePath(root *os.Root, directory, baseName string) (string, error) {
	if err := root.MkdirAll(directory, 0o750); err != nil {
		return "", err
	}
	for suffix := 0; suffix < 1000; suffix++ {
		name := baseName
		if suffix > 0 {
			name += "-" + strconv.Itoa(suffix)
		}
		candidate := filepath.Join(directory, name)
		if err := root.Mkdir(candidate, 0o750); err == nil {
			return candidate, nil
		} else if !errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("create quarantine directory: %w", err)
		}
	}
	return "", fmt.Errorf("too many quarantine directories for generation")
}

func readOpenClawCompatibilityDir(root *os.Root, path string) ([]os.DirEntry, error) {
	file, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return file.ReadDir(-1)
}

// Pin every component from the filesystem root, including the trusted pool root
// and its user/instance descendants. A tenant cannot redirect the initial root
// through a symlink, or swap one between inspection and descriptor acquisition.
// Arbitrary workspace roots use the same rule and must have canonical ancestors.
func openOpenClawCompatibilityRoot(path string) (*os.Root, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("OpenClaw workspace must be absolute")
	}
	path = filepath.Clean(path)
	base := filepath.VolumeName(path) + string(filepath.Separator)
	root, err := os.OpenRoot(base)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(base, path)
	if err != nil {
		root.Close()
		return nil, err
	}
	if rel == "." {
		root.Close()
		return nil, fmt.Errorf("filesystem root cannot be an instance workspace")
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		before, err := root.Lstat(part)
		if err != nil {
			root.Close()
			return nil, err
		}
		if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil, fmt.Errorf("OpenClaw workspace requires real directory ancestors")
		}
		next, err := root.OpenRoot(part)
		if err != nil {
			root.Close()
			return nil, err
		}
		after, err := next.Stat(".")
		root.Close()
		if err != nil || !os.SameFile(before, after) {
			next.Close()
			return nil, fmt.Errorf("OpenClaw workspace changed during open")
		}
		root = next
	}
	return root, nil
}
