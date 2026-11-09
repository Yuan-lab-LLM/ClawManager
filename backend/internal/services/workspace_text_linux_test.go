//go:build linux

package services

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type localPythonWorkspace struct{ root string }

func (e localPythonWorkspace) exec(ctx context.Context, _, _ int, command []string, stdin io.Reader, stdout, stderr io.Writer) error {
	args := append([]string{}, command[1:]...)
	args[len(args)-1] = e.root
	cmd := exec.CommandContext(ctx, command[0], args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
func TestRuntimeWorkspaceTextRealPython(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 required")
	}
	root := t.TempDir()
	p := filepath.Join(root, "run.sh")
	os.WriteFile(p, []byte("#!/bin/sh\necho old\n"), 0750)
	s := &runtimeWorkspaceFileService{executor: localPythonWorkspace{root}}
	scope := workspaceTestScope(root)
	old, err := s.ReadText(context.Background(), scope, "run.sh")
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.SaveText(context.Background(), scope, "run.sh", "#!/bin/sh\necho new\n", old.Version)
	if err != nil {
		t.Fatal(err)
	}
	if next.Version == old.Version {
		t.Fatal("version unchanged")
	}
	if _, err := s.SaveText(context.Background(), scope, "run.sh", "stale", old.Version); !errors.Is(err, ErrWorkspaceTextConflict) {
		t.Fatalf("conflict: %v", err)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0750 {
		t.Fatal("lost executable permission")
	}
	for _, svc := range []WorkspaceTextService{s, NewWorkspaceFileService(nil).(WorkspaceTextService)} {
		os.Symlink(p, filepath.Join(root, "link.txt"))
		if _, err := svc.ReadText(context.Background(), scope, "link.txt"); err == nil {
			t.Fatal("accepted symlink")
		}
	}
	os.Link(p, filepath.Join(root, "hard.sh"))
	if _, err := s.ReadText(context.Background(), scope, "hard.sh"); err == nil {
		t.Fatal("accepted hard link")
	}
}
