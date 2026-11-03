//go:build !windows

package services

import (
	"context"
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
func lockWorkspaceEditor(ctx context.Context, root *os.Root) (func(), error) {
	f, err := root.OpenFile(".clawmanager-editor-lock", os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
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
