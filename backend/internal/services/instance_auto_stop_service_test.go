package services

import (
	"clawreef/internal/models"
	"os"
	"path/filepath"
	"testing"
)

func TestIdleRestartRequiresOriginalHomeAndRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	p := RuntimeWorkspacePathWithRoot(root, "openclaw", 7, 11)
	i := &models.Instance{ID: 11, UserID: 7, WorkspacePath: &p}
	if err := verifyOriginalIdleWorkspace(root, "openclaw", i); err == nil {
		t.Fatal("missing original directory accepted")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("verification created an empty workspace")
	}
	if err := os.MkdirAll(filepath.Join(p, "home"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := verifyOriginalIdleWorkspace(root, "openclaw", i); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), p); err != nil {
		t.Fatal(err)
	}
	if err := verifyOriginalIdleWorkspace(root, "openclaw", i); err == nil {
		t.Fatal("workspace symlink changed identity")
	}
}
