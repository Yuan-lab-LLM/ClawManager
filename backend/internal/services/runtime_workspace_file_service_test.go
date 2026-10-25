package services

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

type runtimeWorkspaceDownloadExecutor struct {
	archive []byte
}

func (e *runtimeWorkspaceDownloadExecutor) exec(_ context.Context, _, _ int, command []string, _ io.Reader, stdout, _ io.Writer) error {
	if len(command) < 3 {
		return errors.New("unexpected runtime workspace command")
	}
	switch command[2] {
	case runtimeWorkspaceStatEntryScript:
		_, err := io.WriteString(stdout, `{"name":"docs","path":"docs","is_dir":true,"size":0,"modified_at":"2026-09-02T00:00:00Z"}`)
		return err
	case runtimeWorkspaceStreamDirectoryScript:
		_, err := io.Copy(stdout, bytes.NewReader(e.archive))
		return err
	default:
		return errors.New("unexpected runtime workspace script")
	}
}

func TestRuntimeWorkspaceFileServiceDownloadsDirectoryArchiveAndCleansTemporaryFile(t *testing.T) {
	want := []byte("zip-payload")
	service := &runtimeWorkspaceFileService{
		executor: &runtimeWorkspaceDownloadExecutor{archive: want},
	}
	download, filename, size, err := service.OpenDownload(context.Background(), WorkspaceFileScope{
		InstanceID:    12,
		UserID:        34,
		WorkspacePath: "/config",
	}, "docs")
	if err != nil {
		t.Fatalf("OpenDownload directory returned error: %v", err)
	}
	temporary, ok := download.(*workspaceTemporaryDownload)
	if !ok {
		t.Fatalf("directory download type = %T, want temporary download", download)
	}
	temporaryPath := temporary.path
	data, err := io.ReadAll(download)
	if err != nil {
		t.Fatalf("read runtime directory archive: %v", err)
	}
	if err := download.Close(); err != nil {
		t.Fatalf("close runtime directory archive: %v", err)
	}
	if _, err := os.Stat(temporaryPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary runtime archive remains after close: %v", err)
	}
	if filename != "docs.zip" || size != int64(len(want)) || !bytes.Equal(data, want) {
		t.Fatalf("runtime directory download filename=%q size=%d data=%q", filename, size, data)
	}
	for _, required := range []string{
		"zipfile.ZipFile(sys.stdout.buffer",
		"followlinks=False",
		"not os.path.islink",
		"not is_subpath(base, real)",
	} {
		if !strings.Contains(runtimeWorkspaceStreamDirectoryScript, required) {
			t.Fatalf("runtime directory script missing safety clause %q", required)
		}
	}
}
