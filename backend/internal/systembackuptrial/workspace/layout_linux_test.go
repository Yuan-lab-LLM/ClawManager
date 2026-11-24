//go:build linux

package workspace

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func wineFixture(t *testing.T) (*os.Root, []byte) {
	t.Helper()
	r, e := os.OpenRoot(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { r.Close() })
	for _, dir := range []string{WinePrefix + "/dosdevices", WinePrefix + "/drive_c/users/test/Documents"} {
		if e = r.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
	}
	for _, file := range []string{WinePrefix + "/system.reg", WinePrefix + "/user.reg", WinePrefix + "/userdef.reg", WinePrefix + "/drive_c/users/test/Documents/report.docx"} {
		if e = r.WriteFile(file, []byte("synthetic exact bytes"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e = r.Symlink("../drive_c", wineC); e != nil {
		t.Fatal(e)
	}
	if e = r.Symlink("/", wineZ); e != nil {
		t.Fatal(e)
	}
	var b bytes.Buffer
	if _, e = ArchiveWithLayout(context.Background(), r, &b, Limits{100, 1 << 20}, WorkBuddyWineV1); e != nil {
		t.Fatal(e)
	}
	return r, b.Bytes()
}
func TestWineRoundTripKeepsMappingAndDoesNotTraverseRoot(t *testing.T) {
	source, raw := wineFixture(t)
	if _, e := Archive(context.Background(), source, io.Discard, Limits{100, 1 << 20}); !errors.Is(e, ErrArchive) {
		t.Fatal("generic layout accepted Wine")
	}
	target, e := os.OpenRoot(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer target.Close()
	if _, e = RestoreWithLayout(context.Background(), target, bytes.NewReader(raw), Limits{100, 1 << 20}, true, WorkBuddyWineV1); e != nil {
		t.Fatal(e)
	}
	for name, want := range map[string]string{wineC: "../drive_c", wineZ: "/"} {
		got, e := target.Readlink(name)
		if e != nil || got != want {
			t.Fatal(name, got, e)
		}
	}
	if b, e := target.ReadFile(WinePrefix + "/drive_c/users/test/Documents/report.docx"); e != nil || string(b) != "synthetic exact bytes" {
		t.Fatal(e)
	}
	if _, e = target.ReadFile(wineZ + "/etc/passwd"); e == nil {
		t.Fatal("os.Root followed external root")
	}
	before, _ := source.Stat(WinePrefix + "/dosdevices")
	after, _ := target.Stat(WinePrefix + "/dosdevices")
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("dosdevices mtime lost")
	}
	var second bytes.Buffer
	if _, e = ArchiveWithLayout(context.Background(), target, &second, Limits{100, 1 << 20}, WorkBuddyWineV1); e != nil {
		t.Fatal("restored tree not recapturable", e)
	}
}

func TestWineRestoreRejectsUnexpectedMappingsAndTraversal(t *testing.T) {
	_, raw := wineFixture(t)
	for _, attack := range []string{"z-target", "c-target", "z-regular", "other-drive", "drive-descendant", "other-absolute", "missing-c", "missing-z", "missing-registry", "missing-drive-c", "link-prefix", "root-traversal", "unknown-layout", "generic"} {
		t.Run(attack, func(t *testing.T) {
			var out bytes.Buffer
			tw := tar.NewWriter(&out)
			tr := tar.NewReader(bytes.NewReader(raw))
			for {
				h, e := tr.Next()
				if e == io.EOF {
					break
				}
				if e != nil {
					t.Fatal(e)
				}
				if (attack == "missing-c" && h.Name == wineC) || (attack == "missing-z" && h.Name == wineZ) || (attack == "missing-registry" && h.Name == WinePrefix+"/system.reg") || (attack == "missing-drive-c" && (h.Name == WinePrefix+"/drive_c" || strings.HasPrefix(h.Name, WinePrefix+"/drive_c/"))) {
					continue
				}
				if h.Name == wineZ {
					switch attack {
					case "z-target":
						h.Linkname = "/etc"
					case "z-regular":
						h.Typeflag = tar.TypeReg
						h.Linkname = ""
					case "other-drive":
						h.Name = WinePrefix + "/dosdevices/d:"
					case "drive-descendant":
						h.Name = wineZ + "/outside"
					case "other-absolute":
						h.Name = "unexpected-link"
					case "root-traversal":
						h.Name = "../outside"
					}
				}
				if h.Name == wineC && attack == "c-target" {
					h.Linkname = "../../.."
				}
				if h.Name == WinePrefix && attack == "link-prefix" {
					h.Typeflag = tar.TypeSymlink
					h.Linkname = "other"
				}
				if e = tw.WriteHeader(h); e != nil {
					t.Fatal(e)
				}
				if _, e = io.Copy(tw, tr); e != nil {
					t.Fatal(e)
				}
			}
			if e := tw.Close(); e != nil {
				t.Fatal(e)
			}
			root, e := os.OpenRoot(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			defer root.Close()
			layout := WorkBuddyWineV1
			if attack == "unknown-layout" {
				layout = "wine-guessed.v2"
			}
			if attack == "generic" {
				layout = ""
			}
			if _, e = RestoreWithLayout(context.Background(), root, &out, Limits{100, 1 << 20}, true, layout); e == nil {
				t.Fatal("unsafe layout restored")
			}
		})
	}
}

func TestWineDOSAttributesRoundTripAndUnknownMetadataRejected(t *testing.T) {
	source, _ := wineFixture(t)
	value := []byte{0, 1, 2, 255, 128}
	for _, name := range []string{WinePrefix + "/user.reg", WinePrefix + "/drive_c"} {
		if e := unix.Lsetxattr(filepath.Join(source.Name(), name), wineDOSAttribute, value, 0); e != nil {
			t.Fatal(e)
		}
	}
	var raw bytes.Buffer
	if _, e := ArchiveWithLayout(context.Background(), source, &raw, Limits{100, 1 << 20}, WorkBuddyWineV1); e != nil {
		t.Fatal(e)
	}
	target, e := os.OpenRoot(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer target.Close()
	if _, e = RestoreWithLayout(context.Background(), target, &raw, Limits{100, 1 << 20}, true, WorkBuddyWineV1); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{WinePrefix + "/user.reg", WinePrefix + "/drive_c"} {
		got := make([]byte, 64)
		n, e := unix.Lgetxattr(filepath.Join(target.Name(), name), wineDOSAttribute, got)
		if e != nil || !bytes.Equal(got[:n], value) {
			t.Fatal("DOS attributes lost", e)
		}
	}
	if e = unix.Lsetxattr(filepath.Join(source.Name(), WinePrefix+"/user.reg"), "user.unknown", []byte("unsupported"), 0); e != nil {
		t.Fatal(e)
	}
	if _, e = ArchiveWithLayout(context.Background(), source, io.Discard, Limits{100, 1 << 20}, WorkBuddyWineV1); e == nil {
		t.Fatal("unknown xattr silently omitted")
	}
	for _, kind := range []string{"generic", "unknown-key", "invalid-base64", "too-large", "link-xattr"} {
		t.Run(kind, func(t *testing.T) {
			h := &tar.Header{Name: "data", Typeflag: tar.TypeReg, Mode: 0600, PAXRecords: map[string]string{wineDOSPAX: base64.StdEncoding.EncodeToString(value)}, Format: tar.FormatPAX}
			layout := WorkBuddyWineV1
			switch kind {
			case "generic":
				layout = ""
			case "unknown-key":
				h.PAXRecords = map[string]string{"SCHILY.xattr.security.capability": "payload"}
			case "invalid-base64":
				h.PAXRecords[wineDOSPAX] = "!!!"
			case "too-large":
				h.PAXRecords[wineDOSPAX] = base64.StdEncoding.EncodeToString(make([]byte, maxWineAttribute+1))
			case "link-xattr":
				h.Typeflag = tar.TypeSymlink
				h.Linkname = "other"
			}
			var buf bytes.Buffer
			tw := tar.NewWriter(&buf)
			if e := tw.WriteHeader(h); e != nil {
				t.Fatal(e)
			}
			tw.Close()
			r, e := os.OpenRoot(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			defer r.Close()
			if _, e = RestoreWithLayout(context.Background(), r, &buf, Limits{100, 1 << 20}, true, layout); e == nil {
				t.Fatal("unsafe xattr accepted")
			}
		})
	}
}

func TestWineCaptureRejectsSubstitutionAndExtraLinks(t *testing.T) {
	for _, attack := range []string{"root-map", "extra-drive", "other-absolute", "registry-symlink"} {
		t.Run(attack, func(t *testing.T) {
			r, _ := wineFixture(t)
			switch attack {
			case "root-map":
				r.Remove(wineZ)
				r.Symlink("/etc", wineZ)
			case "extra-drive":
				r.Symlink("../drive_c", WinePrefix+"/dosdevices/d:")
			case "other-absolute":
				r.Symlink("/", "other")
			case "registry-symlink":
				r.Remove(WinePrefix + "/user.reg")
				r.Symlink("system.reg", WinePrefix+"/user.reg")
			}
			if _, e := ArchiveWithLayout(context.Background(), r, io.Discard, Limits{100, 1 << 20}, WorkBuddyWineV1); e == nil {
				t.Fatal("unsafe source accepted")
			}
		})
	}
}
