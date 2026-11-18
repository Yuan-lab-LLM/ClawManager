//go:build linux

package services

import (
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"syscall"
)

func nfsBrowserParent(root string, scope WorkspaceFileScope) string {
	return filepath.Join(root, "browsers", fmt.Sprintf("user-%d", scope.UserID), fmt.Sprintf("instance-%d", scope.InstanceID))
}

func grantNFSBrowserParent(request workspaceNFSRequest) error {
	validation := request
	validation.Scope.WorkspacePath = request.Root
	if err := validateWorkspaceNFSHelperPath(validation); err != nil {
		return err
	}
	root, err := os.OpenRoot(request.Root)
	if err != nil {
		return err
	}
	defer func() { root.Close() }()
	info, err := root.Stat(".")
	if err != nil {
		return err
	}
	if info.Sys().(*syscall.Stat_t).Uid != 10001 {
		return os.ErrPermission
	}
	parts := []string{"browsers", fmt.Sprintf("user-%d", request.Scope.UserID)}
	if request.Action == "browser_grant" {
		parts = append(parts, fmt.Sprintf("instance-%d", request.Scope.InstanceID))
	}
	for index, part := range parts {
		mode, gid := os.FileMode(0711), 10001
		if request.Action == "browser_grant" && index == len(parts)-1 {
			mode, gid = 0771, request.GID
		}
		if request.Action == "browser_grant" && index < len(parts)-1 {
			if _, err := root.Lstat(part); err != nil {
				return err
			}
		}
		child, created, err := nativeTeamChild(root, part, mode)
		if err != nil {
			return err
		}
		if created {
			directory, err := child.Open(".")
			if err == nil {
				err = directory.Chmod(mode)
				directory.Close()
			}
			if err != nil {
				child.Close()
				return err
			}
		}
		info, err = child.Stat(".")
		if err != nil {
			child.Close()
			return err
		}
		stat := info.Sys().(*syscall.Stat_t)
		if stat.Uid != 10001 || int(stat.Gid) != gid || info.Mode().Perm() != mode {
			child.Close()
			return os.ErrPermission
		}
		root.Close()
		root = child
	}
	return nil
}

func openNFSBrowserParent(request workspaceNFSRequest) (*os.Root, error) {
	path := nfsBrowserParent(request.Root, request.Scope)
	validation := request
	validation.Scope.WorkspacePath = path
	if err := validateWorkspaceNFSHelperPath(validation); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	info, err := root.Stat(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	stat := info.Sys().(*syscall.Stat_t)
	if stat.Uid != 10001 || int(stat.Gid) != request.GID || info.Mode().Perm() != 0771 {
		root.Close()
		return nil, os.ErrPermission
	}
	return root, nil
}

func migrateNFSBrowserProfile(request workspaceNFSRequest) error {
	target, err := openNFSBrowserParent(request)
	if err != nil {
		return err
	}
	defer target.Close()
	workspace, err := os.OpenRoot(request.Scope.WorkspacePath)
	if err != nil {
		return err
	}
	defer workspace.Close()
	source, err := workspace.OpenRoot("home/.openclaw/browser-worker")
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if source != nil {
		defer source.Close()
	}
	// Persistent browser state and user downloads each move atomically. Other
	// legacy root-init/cache files stay untouched in the original private home.
	for _, names := range [][2]string{{"chromium", "profile"}, {"Downloads", "downloads"}} {
		if err = migrateNFSBrowserDirectory(source, target, names[0], names[1], request.UID); err != nil {
			return err
		}
	}
	return nil
}

func migrateNFSBrowserDirectory(source, target *os.Root, oldName, newName string, uid int) error {
	var previous os.FileInfo
	var err error
	if source != nil {
		previous, err = source.Lstat(oldName)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if previous != nil && (!previous.IsDir() || previous.Mode()&os.ModeSymlink != 0 || previous.Sys().(*syscall.Stat_t).Uid != uint32(uid)) {
			return os.ErrPermission
		}
	}
	current, statErr := target.Lstat(newName)
	if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}
	if current != nil {
		if !current.IsDir() || current.Mode()&os.ModeSymlink != 0 || current.Sys().(*syscall.Stat_t).Uid != uint32(uid) {
			return os.ErrPermission
		}
		if previous != nil {
			return ErrWorkspaceEntryExists
		}
		audited, err := openAuditedNFSBrowserDirectory(target, newName, current, uid)
		if err != nil {
			return err
		}
		defer audited.Close()
		actual, err := target.Lstat(newName)
		if err != nil {
			return err
		}
		if !os.SameFile(current, actual) {
			return ErrWorkspacePathEscape
		}
		return nil
	}
	if previous == nil {
		return target.Mkdir(newName, 0700)
	}
	// The source entry cannot be safely renamed through a foreign-writable
	// parent. Both the parent and audited leaf remain descriptor-anchored.
	parentInfo, err := source.Stat(".")
	if err != nil {
		return err
	}
	if err = checkNFSBrowserPrivateInode(parentInfo, uid); err != nil {
		return err
	}
	audited, err := openAuditedNFSBrowserDirectory(source, oldName, previous, uid)
	if err != nil {
		return err
	}
	defer audited.Close()
	oldDirectory, err := source.Open(".")
	if err != nil {
		return err
	}
	defer oldDirectory.Close()
	newDirectory, err := target.Open(".")
	if err != nil {
		return err
	}
	defer newDirectory.Close()
	// NFSv4.1 does not implement RENAME_NOREPLACE. The caller holds the same
	// permanent per-instance operation fence across grant+migration, and business
	// mounts expose only native/profile leaves, never this management parent.
	// Hence another authorized writer cannot create the checked-absent destination.
	actual, err := source.Lstat(oldName)
	if err != nil {
		return err
	}
	if !os.SameFile(previous, actual) {
		return ErrWorkspacePathEscape
	}
	if err = unix.Renameat(int(oldDirectory.Fd()), oldName, int(newDirectory.Fd()), newName); err != nil {
		return err
	}
	moved, err := target.Lstat(newName)
	if err != nil {
		return err
	}
	if !os.SameFile(previous, moved) || !moved.IsDir() || moved.Mode()&os.ModeSymlink != 0 {
		return ErrWorkspacePathEscape
	}
	return nil
}

// Browser mounts bypass RuntimeWorkspace, so profile/download admission needs
// its own read-only inode audit under the existing writer/operation gates.
// A path is never authority for another UID's inode, even with one link/0644.
func checkNFSBrowserPrivateInode(info os.FileInfo, uid int) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(uid) {
		return os.ErrPermission
	}
	// Chromium owns normal singleton symlinks into its local container /tmp.
	// Check their inode owner without following or treating mode0777 as a grant.
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	if info.Mode().Perm()&0002 != 0 || info.Mode().Perm()&0020 != 0 && stat.Gid != uint32(uid) {
		return os.ErrPermission
	}
	return nil
}
func openAuditedNFSBrowserDirectory(parent *os.Root, name string, expected os.FileInfo, uid int) (*os.Root, error) {
	root, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	actual, err := root.Stat(".")
	if err == nil && !os.SameFile(expected, actual) {
		err = ErrWorkspacePathEscape
	}
	if err == nil {
		err = auditNFSBrowserPrivateTree(root, uid)
	}
	if err != nil {
		root.Close()
		return nil, err
	}
	return root, nil
}
func auditNFSBrowserPrivateTree(root *os.Root, uid int) error {
	info, err := root.Stat(".")
	if err != nil {
		return err
	}
	if err = checkNFSBrowserPrivateInode(info, uid); err != nil {
		return err
	}
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	entries, err := directory.ReadDir(-1)
	directory.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		// O_PATH pins metadata without reading foreign data or blocking on a FIFO.
		file, err := root.OpenFile(entry.Name(), unix.O_PATH|unix.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		actual, err := file.Stat()
		file.Close()
		if err != nil {
			return err
		}
		if err = checkNFSBrowserPrivateInode(actual, uid); err != nil {
			return err
		}
		if actual.IsDir() {
			child, err := openAuditedNFSBrowserDirectory(root, entry.Name(), actual, uid)
			if err != nil {
				return err
			}
			child.Close()
		}
	}
	return nil
}
