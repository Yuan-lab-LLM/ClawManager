package envelope

import "os"

// SyncDirectory persists directory entries on supported capture platforms.
// Windows remains a local functional fixture platform (see sync_windows.go).
func SyncDirectory(root *os.Root) error { return syncDirectory(root) }
