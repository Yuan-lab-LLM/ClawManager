package systembackup

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotVerificationRejectsAuthenticatedButUnusablePartFormats(t *testing.T) {
	ctx := context.Background()
	b := CaptureBudget{MaxBytes: 1 << 20, MaxItems: 100}
	for _, name := range RequiredParts {
		if err := ValidateSnapshotPart(ctx, name, bytes.NewReader([]byte("authenticated private task data, not a valid snapshot")), b); err == nil {
			t.Fatal("opaque payload accepted as restorable", name)
		}
	}
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, ".partial.txt"), []byte("preserved partial artifact"), 0600); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if _, err := CaptureFiles(ctx, &archive, []FileRoot{{ID: "team", Path: source}}, FileBudget{MaxBytes: b.MaxBytes, MaxEntries: b.MaxItems}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSnapshotPart(ctx, "workspace", bytes.NewReader(archive.Bytes()), b); err != nil {
		t.Fatal(err)
	}
	archive.WriteString("hidden trailing bytes")
	if err := ValidateSnapshotPart(ctx, "workspace", bytes.NewReader(archive.Bytes()), b); err == nil {
		t.Fatal("trailing archive payload accepted")
	}
}

func TestFileSnapshotValidationRejectsEscapesAndParentLinksInEitherOrder(t *testing.T) {
	ctx := context.Background()
	budget := FileBudget{MaxBytes: 1 << 20, MaxEntries: 100}
	for _, headers := range [][]*tar.Header{
		{{Name: "../other", Typeflag: tar.TypeReg, Mode: 0600}},
		{{Name: "roots/team/link", Linkname: "../../../other", Typeflag: tar.TypeSymlink, Mode: 0777}},
		{{Name: "roots/team/a", Linkname: "b", Typeflag: tar.TypeSymlink, Mode: 0777}, {Name: "roots/team/a/child", Typeflag: tar.TypeReg, Mode: 0600}},
		{{Name: "roots/team/a/child", Typeflag: tar.TypeReg, Mode: 0600}, {Name: "roots/team/a", Linkname: "b", Typeflag: tar.TypeSymlink, Mode: 0777}},
	} {
		var archive bytes.Buffer
		tw := tar.NewWriter(&archive)
		for _, header := range headers {
			if err := tw.WriteHeader(header); err != nil {
				t.Fatal(err)
			}
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := ValidateFileSnapshot(ctx, bytes.NewReader(archive.Bytes()), budget); err == nil {
			t.Fatal("unsafe archive accepted")
		}
	}
}
