//go:build !windows

package systembackup

import "os"

func stageDirectoryPermissions(root *os.Root, name string) error {
	if err := root.Chown(name, 65532, 65532); err != nil {
		return err
	}
	return root.Chmod(name, 0750)
}
