//go:build !windows

package systembackup

import "os"

func restoreFileOwner(root *os.Root, r FileRecord) error { return root.Lchown(r.Path, r.UID, r.GID) }
