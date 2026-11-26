package workspace

import (
	"archive/tar"
	"encoding/base64"
	"os"
	"path"
	"regexp"
	"runtime"
	"strings"
)

// Layout is explicitly selected by the release policy, never inferred from
// archive contents. The empty layout retains the portable archive contract.
type Layout string

const WorkBuddyWineV1 Layout = "workbuddy-lite-wine.v1"

// The UID identity contract registers the instance root, with HOME under home/.
const WorkBuddyInstanceWineV1 Layout = "workbuddy-lite-instance-wine.v1"
const DeepSeekHarnessV1 Layout = "deepseek-harness-node-modules.v1"
const deepSeekModules = ".dsh/profiles/node_modules"
const deepSeekImage = "/usr/local/lib/node_modules/@deepseek-ai/dsh"

var npmPackage = regexp.MustCompile(`^(?:@[a-z0-9][a-z0-9._-]*/)?[a-z0-9][a-z0-9._-]*$`)

const WinePrefix = ".local/share/workbuddy-editor-sdk-wine"
const wineC = WinePrefix + "/dosdevices/c:"
const wineZ = WinePrefix + "/dosdevices/z:"
const wineDOSAttribute = "user.DOSATTRIB"
const wineDOSPAX = "CLAWMANAGER.wine.dosattrib"
const maxWineAttribute = 4096

func (l Layout) pax(h *tar.Header) bool {
	for key, value := range h.PAXRecords {
		switch key {
		case "path", "linkpath", "mtime", "atime", "ctime", "size":
		case wineDOSPAX:
			if !l.wine() || (h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA && h.Typeflag != tar.TypeDir) {
				return false
			}
			if len(value) > base64.StdEncoding.EncodedLen(maxWineAttribute) {
				return false
			}
			data, e := base64.StdEncoding.DecodeString(value)
			if e != nil || len(data) == 0 || len(data) > maxWineAttribute || base64.StdEncoding.EncodeToString(data) != value {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (l Layout) wine() bool { return l == WorkBuddyWineV1 || l == WorkBuddyInstanceWineV1 }
func (l Layout) winePath(p string) string {
	if l == WorkBuddyInstanceWineV1 {
		return "home/" + p
	}
	return p
}
func (l Layout) Known() bool      { return l == "" || l.wine() || l == DeepSeekHarnessV1 }
func (l Layout) executable() bool { return l.Known() && (l == "" || runtime.GOOS == "linux") }
func (l Layout) name(s string) bool {
	return validName(s) || (l.wine() && (s == l.winePath(wineC) || s == l.winePath(wineZ)))
}
func (l Layout) entry(h *tar.Header) bool {
	if !l.name(h.Name) {
		return false
	}
	if l.wine() {
		switch h.Name {
		case l.winePath(wineC):
			return h.Typeflag == tar.TypeSymlink && h.Linkname == "../drive_c" && h.Size == 0
		case l.winePath(wineZ):
			return h.Typeflag == tar.TypeSymlink && h.Linkname == "/" && h.Size == 0
		}
	}
	if l == DeepSeekHarnessV1 && h.Typeflag == tar.TypeSymlink && path.IsAbs(h.Linkname) {
		return h.Size == 0 && deepSeekLink(h.Name, h.Linkname)
	}
	return h.Typeflag != tar.TypeSymlink || validLink(h.Name, h.Linkname)
}

// These are image-owned package links observed in rc.6, not instance data.
// Preserve link text only; never traverse them or accept arbitrary destinations.
// RuntimePolicy separately binds this opt-in layout to an immutable image.
func deepSeekLink(name, target string) bool {
	if !strings.HasPrefix(name, deepSeekModules+"/") {
		return false
	}
	pkg := strings.TrimPrefix(name, deepSeekModules+"/")
	if !npmPackage.MatchString(pkg) {
		return false
	}
	expected := deepSeekImage + "/node_modules/" + pkg
	switch pkg {
	case "@deepseek-ai/dsh":
		expected = deepSeekImage
	case "react-dom", "@tanstack/react-virtual":
		expected = deepSeekImage + "/node_modules/@deepseek-ai/dsh-client-ui-trajectory/node_modules/" + pkg
	}
	return target == expected
}

func (l Layout) deepSeekData(root *os.Root) error {
	if l != DeepSeekHarnessV1 {
		return nil
	}
	if _, e := root.Lstat(".dsh"); os.IsNotExist(e) {
		return nil
	}
	for _, name := range []string{".dsh", ".dsh/profiles", deepSeekModules} {
		info, e := root.Lstat(name)
		if os.IsNotExist(e) {
			return nil
		} // A not-yet-initialized profile is valid.
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrArchive
		}
	}
	return nil
}

// No prefix is valid for a not-yet-initialized instance. An existing prefix
// must be complete and must not redirect through a substituted ancestor.
func (l Layout) wineData(root *os.Root) (bool, error) {
	if !l.wine() {
		return false, nil
	}
	if l == WorkBuddyInstanceWineV1 {
		i, e := root.Lstat("home")
		if e != nil || !i.IsDir() || i.Mode()&os.ModeSymlink != 0 {
			return false, ErrArchive
		}
	}
	if _, e := root.Lstat(l.winePath(WinePrefix)); os.IsNotExist(e) {
		return false, nil
	} else if e != nil {
		return false, ErrArchive
	}
	for _, name := range []string{".", ".local", ".local/share", WinePrefix, WinePrefix + "/drive_c", WinePrefix + "/dosdevices"} {
		info, e := root.Lstat(l.winePath(name))
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, ErrArchive
		}
	}
	for _, name := range []string{"system.reg", "user.reg", "userdef.reg"} {
		info, e := root.Lstat(l.winePath(WinePrefix + "/" + name))
		if e != nil || !info.Mode().IsRegular() {
			return false, ErrArchive
		}
	}
	return true, nil
}
func (l Layout) source(root *os.Root) error {
	if e := l.deepSeekData(root); e != nil {
		return e
	}
	present, e := l.wineData(root)
	if e != nil || !present {
		return e
	}
	for name, target := range map[string]string{wineC: "../drive_c", wineZ: "/"} {
		link, e := root.Readlink(l.winePath(name))
		if e != nil || link != target {
			return ErrArchive
		}
	}
	return nil
}
