//go:build linux

package workspace

import (
	"archive/tar"
	"encoding/base64"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

func archiveMetadata(root *os.Root, name string, layout Layout) (map[string]string, error) {
	// Only the opt-in Wine layout preserves user.DOSATTRIB. ACLs, capabilities
	// and all other xattrs remain unsupported, never silently lost. The coordinator
	// must fence source renames/mount changes throughout this operation.
	n, e := unix.Llistxattr(filepath.Join(root.Name(), name), nil)
	if e != nil && e != unix.ENOTSUP {
		return nil, ErrArchive
	}
	attrs := map[string]string{}
	if n > 0 {
		if !layout.wine() || n != len(wineDOSAttribute)+1 {
			return nil, ErrArchive
		}
		list := make([]byte, n)
		got, e := unix.Llistxattr(filepath.Join(root.Name(), name), list)
		if e != nil || got != n || string(list) != wineDOSAttribute+"\x00" {
			return nil, ErrArchive
		}
		data := make([]byte, maxWineAttribute+1)
		got, e = unix.Lgetxattr(filepath.Join(root.Name(), name), wineDOSAttribute, data)
		if e != nil || got == 0 || got > maxWineAttribute {
			return nil, ErrArchive
		}
		attrs[wineDOSPAX] = base64.StdEncoding.EncodeToString(data[:got])
	}
	info, e := root.Lstat(name)
	if e != nil {
		return nil, ErrArchive
	}
	base, e := root.Stat(".")
	if e != nil {
		return nil, ErrArchive
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	baseStat, baseOK := base.Sys().(*syscall.Stat_t)
	if !ok || !baseOK || stat.Dev != baseStat.Dev {
		return nil, ErrArchive
	}
	special := info.Mode() & (os.ModeSetuid | os.ModeSetgid | os.ModeSticky)
	if special != 0 && !(info.IsDir() && special == os.ModeSetgid) {
		return nil, ErrArchive
	}
	if len(attrs) > 0 && !info.IsDir() && !info.Mode().IsRegular() {
		return nil, ErrArchive
	}
	return attrs, nil
}

func restoreMetadata(root *os.Root, h *tar.Header, layout Layout) error {
	encoded, ok := h.PAXRecords[wineDOSPAX]
	if !ok {
		return nil
	}
	if !layout.wine() || !layout.pax(h) {
		return ErrArchive
	}
	info, e := root.Lstat(h.Name)
	if e != nil || (!info.Mode().IsRegular() && !info.IsDir()) {
		return ErrArchive
	}
	data, e := base64.StdEncoding.DecodeString(encoded)
	if e != nil {
		return ErrArchive
	}
	// All writes are to the exclusive quarantine's real files/directories.
	// No arbitrary PAX xattr, ACL or capability name is accepted.
	if unix.Lsetxattr(filepath.Join(root.Name(), h.Name), wineDOSAttribute, data, 0) != nil {
		return ErrArchive
	}
	return nil
}
