package systembackup

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFileArchiveRejectsTrailingPayload(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "result.txt"), []byte("partial result"), 0600); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	budget := FileBudget{MaxBytes: 1 << 20, MaxEntries: 100}
	if _, err := CaptureFiles(context.Background(), &b, []FileRoot{{ID: "team", Path: source}}, budget); err != nil {
		t.Fatal(err)
	}
	b.WriteString("hidden trailing data")
	if _, err := ExtractFiles(context.Background(), &b, t.TempDir(), budget); err == nil {
		t.Fatal("trailing payload accepted")
	}
}

func TestFilesPreserveHiddenPartialArtifacts(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, ".state"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"partial.bin", ".state/active-assignment.json"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte{0, 255, 128, 10}, 0600); err != nil {
			t.Fatal(err)
		}
	}
	budget := FileBudget{MaxBytes: 1 << 20, MaxEntries: 100}
	var archive bytes.Buffer
	before, err := CaptureFiles(context.Background(), &archive, []FileRoot{{ID: "shared", Path: source}}, budget)
	if err != nil {
		t.Fatal(err)
	}
	after, err := ExtractFiles(context.Background(), bytes.NewReader(archive.Bytes()), target, budget)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("file inventory differs\n%+v\n%+v", before, after)
	}
	if count, e := VerifyFiles(context.Background(), bytes.NewReader(archive.Bytes()), target, budget); e != nil || count != len(before) {
		t.Fatal("restored readback failed", count, e)
	}
	file := filepath.Join(target, "roots/shared/partial.bin")
	info, e := os.Stat(file)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(file, []byte{9, 9, 9, 9}, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Chtimes(file, info.ModTime(), info.ModTime()); e != nil {
		t.Fatal(e)
	}
	if _, e = VerifyFiles(context.Background(), bytes.NewReader(archive.Bytes()), target, budget); e == nil {
		t.Fatal("same-size same-time file corruption accepted")
	}
	if _, err = ExtractFiles(context.Background(), bytes.NewReader(archive.Bytes()), target, budget); err == nil {
		t.Fatal("overwrote existing files")
	}
}
func TestFilesRejectEscapeDuplicatesAndBudgets(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "roots/shared/../../escape", `roots\shared\escape`, "roots/c:/escape"} {
		t.Run(name, func(t *testing.T) {
			var archive bytes.Buffer
			tw := tar.NewWriter(&archive)
			_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: 0})
			_ = tw.Close()
			if _, err := ExtractFiles(context.Background(), &archive, t.TempDir(), FileBudget{100, 100}); err == nil {
				t.Fatal("unsafe name accepted")
			}
		})
	}
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	for i := 0; i < 2; i++ {
		_ = tw.WriteHeader(&tar.Header{Name: "roots/shared/a", Mode: 0600, Size: 0})
	}
	_ = tw.Close()
	if _, err := ExtractFiles(context.Background(), &archive, t.TempDir(), FileBudget{100, 100}); err == nil {
		t.Fatal("duplicate accepted")
	}
	source := t.TempDir()
	_ = os.WriteFile(filepath.Join(source, "big"), make([]byte, 101), 0600)
	if _, err := CaptureFiles(context.Background(), &bytes.Buffer{}, []FileRoot{{"shared", source}}, FileBudget{100, 100}); err == nil {
		t.Fatal("budget ignored")
	}
}
