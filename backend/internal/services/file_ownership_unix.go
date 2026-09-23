//go:build !windows

package services

import "os"

func setFileOwnership(path string, uid, gid int) error {
	return os.Chown(path, uid, gid)
}
