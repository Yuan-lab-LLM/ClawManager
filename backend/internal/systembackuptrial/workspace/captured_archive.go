package workspace

import (
	"archive/tar"
	"path"
)

// CapturedArchiveValidator applies the source/restore layout's SAME entry and
// PAX policies without extracting files or depending on the reader host OS.
// Reading Linux metadata does not enable capture/restore execution on Windows.
type CapturedArchiveValidator struct {
	layout  Layout
	seen    map[string]byte
	parents map[string]bool
}

func NewCapturedArchiveValidator(layout Layout) (*CapturedArchiveValidator, error) {
	if !layout.Known() {
		return nil, ErrArchive
	}
	return &CapturedArchiveValidator{layout: layout, seen: map[string]byte{}, parents: map[string]bool{}}, nil
}

func (v *CapturedArchiveValidator) Observe(h *tar.Header) error {
	if v == nil || h == nil || !v.layout.entry(h) || !v.layout.pax(h) || h.Size < 0 || h.Uid < 0 || h.Gid < 0 || h.Mode < 0 || h.Mode&^07777 != 0 || h.Devmajor != 0 || h.Devminor != 0 || len(h.Xattrs) != 0 {
		return ErrArchive
	}
	special := h.Mode & 07000
	if special != 0 && !(h.Typeflag == tar.TypeDir && special == 02000) {
		return ErrArchive
	}
	if _, exists := v.seen[h.Name]; exists {
		return ErrArchive
	}
	switch h.Typeflag {
	case tar.TypeReg, tar.TypeRegA:
		if h.Linkname != "" {
			return ErrArchive
		}
	case tar.TypeDir:
		if h.Size != 0 || h.Linkname != "" {
			return ErrArchive
		}
	case tar.TypeSymlink:
		if h.Size != 0 {
			return ErrArchive
		}
	default:
		return ErrArchive
	}
	if h.Name == "." && h.Typeflag != tar.TypeDir {
		return ErrArchive
	}
	if h.Typeflag != tar.TypeDir && v.parents[h.Name] {
		return ErrArchive
	}
	for parent := path.Dir(h.Name); parent != "."; parent = path.Dir(parent) {
		if kind, exists := v.seen[parent]; exists && kind != tar.TypeDir {
			return ErrArchive
		}
		v.parents[parent] = true
	}
	v.seen[h.Name] = h.Typeflag
	return nil
}

func (v *CapturedArchiveValidator) Finish() error {
	if v == nil {
		return ErrArchive
	}
	if v.layout.wine() {
		prefix := v.layout.winePath(WinePrefix)
		_, present := v.seen[prefix]
		_, c := v.seen[v.layout.winePath(wineC)]
		_, z := v.seen[v.layout.winePath(wineZ)]
		if c != z || present != (c && z) {
			return ErrArchive
		}
		if present {
			for _, directory := range []string{".local", ".local/share", WinePrefix, WinePrefix + "/drive_c", WinePrefix + "/dosdevices"} {
				if v.seen[v.layout.winePath(directory)] != tar.TypeDir {
					return ErrArchive
				}
			}
			if v.layout == WorkBuddyInstanceWineV1 && v.seen["home"] != tar.TypeDir {
				return ErrArchive
			}
			for _, file := range []string{"system.reg", "user.reg", "userdef.reg"} {
				kind, exists := v.seen[prefix+"/"+file]
				if !exists || (kind != tar.TypeReg && kind != tar.TypeRegA) {
					return ErrArchive
				}
			}
		}
	}
	if v.layout == DeepSeekHarnessV1 {
		for _, directory := range []string{".dsh", ".dsh/profiles", deepSeekModules} {
			if kind, exists := v.seen[directory]; exists && kind != tar.TypeDir {
				return ErrArchive
			}
		}
	}
	return nil
}
