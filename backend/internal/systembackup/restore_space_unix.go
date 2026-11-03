//go:build linux || darwin || freebsd || openbsd || netbsd

package systembackup

import "golang.org/x/sys/unix"

func availableRestoreBytes(path string) (uint64, error) {
	var stat unix.Statfs_t
	if e := unix.Statfs(path, &stat); e != nil {
		return 0, e
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}
