//go:build windows

package systembackup

import "os"

// Windows is a development host, not a supported Kubernetes capture runner.
func syncDirectory(root *os.Root) error { return nil }
