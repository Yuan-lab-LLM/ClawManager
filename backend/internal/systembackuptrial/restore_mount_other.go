//go:build !linux

package systembackuptrial

import "os"

func restoreMountReadOnly(root *os.Root) (bool, error) {
	return probeReadOnlyReader(root)
}
