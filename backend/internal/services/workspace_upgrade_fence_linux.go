//go:build linux

package services

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Same permanent NFS lock inode as Runtime migration/rebuild commands. Never
// unlink it: another Pod may already hold an FD for this inode.
func (s *RuntimeUpgradeService) lockWorkspaceOperations(ids []int) (func(), error) {
	dir := filepath.Join(s.workspaceRoot, ".clawmanager-upgrades", "operations")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	var files []*os.File
	release := func() {
		for i := len(files) - 1; i >= 0; i-- {
			_ = files[i].Close()
		}
	}
	for _, id := range normalizedPositiveIDs(ids) {
		name := filepath.Join(dir, fmt.Sprintf("instance-%d.lock", id))
		fd, err := syscall.Open(name, syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
		if err != nil {
			release()
			return nil, err
		}
		f := os.NewFile(uintptr(fd), name)
		if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			f.Close()
			release()
			return nil, fmt.Errorf("%w: #%d", ErrWorkspaceUpgradeBusy, id)
		}
		files = append(files, f)
	}
	return release, nil
}
