//go:build linux

package services

import (
	"context"
	"golang.org/x/sys/unix"
	"io"
	"math"
	"os"
)

// Only called inside the credential-scoped, killable helper. Directory file
// descriptors and O_NOFOLLOW prevent even an in-root symlink from being walked.
func measureWorkspaceUsageTree(ctx context.Context, path string, limit int) (int64, error) {
	if limit <= 0 || limit > workspaceUsageEntryLimit {
		return 0, errWorkspaceUsageUnavailable
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return 0, err
	}
	root := os.NewFile(uintptr(fd), "workspace-usage")
	defer root.Close()
	remaining := limit
	var walk func(*os.File, int) (int64, error)
	walk = func(dir *os.File, depth int) (int64, error) {
		if depth > 128 {
			return 0, errWorkspaceUsageUnavailable
		}
		var total int64
		for {
			if err := ctx.Err(); err != nil {
				return 0, err
			}
			entries, readErr := dir.ReadDir(128)
			if readErr != nil && readErr != io.EOF {
				return 0, readErr
			}
			for _, entry := range entries {
				remaining--
				if remaining < 0 {
					return 0, errWorkspaceUsageUnavailable
				}
				if err := ctx.Err(); err != nil {
					return 0, err
				}
				var st unix.Stat_t
				if err := unix.Fstatat(int(dir.Fd()), entry.Name(), &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
					return 0, err
				}
				var size int64
				switch st.Mode & unix.S_IFMT {
				case unix.S_IFREG:
					size = st.Size
				case unix.S_IFDIR:
					childFD, err := unix.Openat(int(dir.Fd()), entry.Name(), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
					if err != nil {
						return 0, err
					}
					child := os.NewFile(uintptr(childFD), "workspace-usage-directory")
					var actual unix.Stat_t
					err = unix.Fstat(childFD, &actual)
					if err == nil && (st.Dev != actual.Dev || st.Ino != actual.Ino) {
						err = errWorkspaceUsageUnavailable
					}
					if err == nil {
						size, err = walk(child, depth+1)
					}
					child.Close()
					if err != nil {
						return 0, err
					}
				}
				if size < 0 || size > math.MaxInt64-total {
					return 0, errWorkspaceUsageUnavailable
				}
				total += size
			}
			if readErr == io.EOF {
				return total, nil
			}
		}
	}
	return walk(root, 0)
}
