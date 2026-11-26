//go:build !linux

package workspace

import (
	"archive/tar"
	"os"
)

// Non-Linux execution is for isolated fixture tests, not supported capture jobs.
func archiveMetadata(root *os.Root, name string, layout Layout) (map[string]string, error) {
	i, e := root.Lstat(name)
	if e != nil || i.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return nil, ErrArchive
	}
	return nil, nil
}

func restoreMetadata(_ *os.Root, h *tar.Header, _ Layout) error {
	if _, ok := h.PAXRecords[wineDOSPAX]; ok {
		return ErrArchive
	}
	return nil
}
