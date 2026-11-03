package systembackup

import "os"

func stageDirectoryPermissions(root *os.Root, name string) error { return root.Chmod(name, 0750) }
