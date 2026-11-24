//go:build linux

package systembackuptrial

import (
	"os"

	"golang.org/x/sys/unix"
)

func inheritWorkspaceDirectory(parent, stage *os.Root, name string) error {
	directory, err := parent.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	var info unix.Stat_t
	if err := unix.Fstat(int(directory.Fd()), &info); err != nil {
		return err
	}
	if err := stage.Chown(name, int(info.Uid), int(info.Gid)); err != nil {
		return err
	}
	mode := os.FileMode(info.Mode & 0777)
	if info.Mode&unix.S_ISGID != 0 {
		mode |= os.ModeSetgid
	}
	return stage.Chmod(name, mode)
}

func publishWorkspaceFiles(root *os.Root, candidate, target string) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	fd := int(directory.Fd())
	if err := unix.Renameat2(fd, candidate, fd, target, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	return directory.Sync()
}
