//go:build !windows

package services

import (
	"context"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"syscall"
	"time"
)

const workspaceTextOpenFlags = unix.O_NOFOLLOW | unix.O_NONBLOCK

func workspaceHasMultipleLinks(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Nlink != 1
}
func preserveWorkspaceTextOwner(file *os.File, info os.FileInfo) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ErrWorkspaceTextUnsupported
	}
	return file.Chown(int(st.Uid), int(st.Gid))
}
func lockWorkspaceEditor(ctx context.Context, root *os.Root, mode os.FileMode) (func(), error) {
	f, err := root.OpenFile(".clawmanager-editor-lock", os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, mode)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || workspaceHasMultipleLinks(st) {
		f.Close()
		return nil, ErrWorkspaceTextUnsupported
	}
	for {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if err != unix.EWOULDBLOCK && err != unix.EAGAIN {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); f.Close() }, nil
}

// Open with the caller's credentials before selecting any owner identity.
func inspectWorkspaceUpload(root *os.Root, name string) (os.FileInfo, error) {
	info, err := root.Lstat(name)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || workspaceHasMultipleLinks(info) {
		return nil, ErrWorkspaceTextUnsupported
	}
	file, err := root.OpenFile(name, os.O_RDWR|workspaceTextOpenFlags, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, actual) || workspaceHasMultipleLinks(actual) {
		return nil, ErrWorkspaceTextConflict
	}
	return actual, nil
}
func workspaceUploadIdentity(info os.FileInfo) string {
	if info == nil {
		return "absent"
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "invalid"
	}
	return fmt.Sprintf("%d:%d:%d:%d:%d:%d:%d", st.Dev, st.Ino, st.Uid, st.Gid, info.Mode(), info.Size(), info.ModTime().UnixNano())
}

// The NFS credential helper may read only its own private inodes. Explicit Team
// scopes retain their separate shared grant and POSIX permission checks.
func validateWorkspacePrivateInode(scope WorkspaceFileScope, info os.FileInfo) error {
	if !scope.credentialScoped || scope.TeamID > 0 || scope.InstanceID <= 0 {
		return nil
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != RuntimeLinuxID(scope.InstanceID) {
		return os.ErrPermission
	}
	return nil
}
