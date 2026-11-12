//go:build linux

package ownerrecords

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func lockOwnedOpenClawHistoryFixture(root string) (func(), error) {
	name := filepath.Join(root, ".clawmanager-writer-fences", "openclaw-user-1-instance-17.lock")
	fd, err := syscall.Open(name, syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !stat.Mode().IsRegular() || stat.Mode().Perm() != 0600 {
		f.Close()
		return nil, fmt.Errorf("unsafe owned fixture fence")
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return func() { _ = f.Close() }, nil
}
