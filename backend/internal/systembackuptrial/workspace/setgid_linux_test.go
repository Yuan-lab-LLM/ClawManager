//go:build linux

package workspace

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"syscall"
	"testing"
)

func TestSharedDirectorySetgidCaptureRestorePreservesOwnershipAndMode(t *testing.T) {
	source, e := os.OpenRoot(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer source.Close()
	if e = source.Chmod(".", os.ModeSetgid|0750); e != nil {
		t.Fatal(e)
	}
	if e = source.Mkdir("shared", 0750); e != nil {
		t.Fatal(e)
	}
	if e = source.Chmod("shared", 0750|os.ModeSetgid); e != nil {
		t.Fatal(e)
	}
	if e = source.WriteFile("shared/result.md", []byte("preserved"), 0640); e != nil {
		t.Fatal(e)
	}
	var archive bytes.Buffer
	limits := Limits{MaxFiles: 10, MaxBytes: 1024}
	if _, e = Archive(context.Background(), source, &archive, limits); e != nil {
		t.Fatal(e)
	}
	destination, e := os.OpenRoot(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer destination.Close()
	if _, e = Restore(context.Background(), destination, bytes.NewReader(archive.Bytes()), limits, true); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{".", "shared"} {
		want, _ := source.Stat(name)
		got, e := destination.Stat(name)
		if e != nil || got.Mode() != want.Mode() {
			t.Fatal("directory mode was changed", name, e)
		}
		originalOwner := want.Sys().(*syscall.Stat_t)
		restoredOwner := got.Sys().(*syscall.Stat_t)
		if originalOwner.Uid != restoredOwner.Uid || originalOwner.Gid != restoredOwner.Gid {
			t.Fatal("directory ownership was changed", name)
		}
	}
	raw, e := destination.ReadFile("shared/result.md")
	if e != nil || string(raw) != "preserved" {
		t.Fatal("contents changed", e)
	}
	unowned, e := os.OpenRoot(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer unowned.Close()
	if _, e = Restore(context.Background(), unowned, bytes.NewReader(archive.Bytes()), limits, false); !errors.Is(e, ErrArchive) {
		t.Fatal("setgid restored without preserving its owner", e)
	}
}
func TestSharedDirectorySupportStillRejectsPrivilegedFileModes(t *testing.T) {
	for _, mode := range []int64{04755, 02755, 01777} {
		var archive bytes.Buffer
		tw := tar.NewWriter(&archive)
		if e := tw.WriteHeader(&tar.Header{Name: "payload", Mode: mode, Size: 1, Typeflag: tar.TypeReg}); e != nil {
			t.Fatal(e)
		}
		io.WriteString(tw, "x")
		tw.Close()
		root, e := os.OpenRoot(t.TempDir())
		if e != nil {
			t.Fatal(e)
		}
		_, e = Restore(context.Background(), root, bytes.NewReader(archive.Bytes()), Limits{MaxFiles: 5, MaxBytes: 1024}, true)
		root.Close()
		if !errors.Is(e, ErrArchive) {
			t.Fatal("privileged file mode accepted", mode, e)
		}
	}
}
