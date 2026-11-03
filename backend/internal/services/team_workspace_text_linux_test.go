//go:build linux

package services

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Two independent processes share the real on-disk lock, not the Windows mutex.
func TestTeamEditorProcessHelper(t *testing.T) {
	root := os.Getenv("TEAM_EDITOR_HELPER_ROOT")
	if root == "" {
		return
	}
	svc := NewWorkspaceFileService(nil).(WorkspaceTextService)
	scope := WorkspaceFileScope{TeamID: 17, UserID: 9, WorkspacePath: root}
	doc, err := svc.ReadText(context.Background(), scope, "a.md")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("READY")
	if _, err = io.ReadFull(os.Stdin, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	_, err = svc.SaveText(context.Background(), scope, "a.md", os.Getenv("TEAM_EDITOR_HELPER_TEXT"), doc.Version)
	if err == nil {
		fmt.Println("SAVED")
	} else if errors.Is(err, ErrWorkspaceTextConflict) {
		fmt.Println("CONFLICT")
	} else {
		t.Fatal(err)
	}
}
func TestTeamEditorMultiProcessAndLinkSafety(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("base"), 0640); err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	type process struct {
		cmd *exec.Cmd
		in  io.WriteCloser
		out *bufio.Reader
	}
	var workers []process
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, text := range []string{"one", "two"} {
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestTeamEditorProcessHelper$")
		cmd.Env = append(os.Environ(), "TEAM_EDITOR_HELPER_ROOT="+root, "TEAM_EDITOR_HELPER_TEXT="+text)
		in, _ := cmd.StdinPipe()
		out, _ := cmd.StdoutPipe()
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		workers = append(workers, process{cmd, in, bufio.NewReader(out)})
	}
	for _, w := range workers {
		line, _ := w.out.ReadString('\n')
		if line != "READY\n" {
			t.Fatal(line)
		}
	}
	for _, w := range workers {
		_, _ = io.WriteString(w.in, "x")
		_ = w.in.Close()
	}
	saved, conflict := 0, 0
	for _, w := range workers {
		data, _ := io.ReadAll(w.out)
		if strings.Contains(string(data), "SAVED") {
			saved++
		}
		if strings.Contains(string(data), "CONFLICT") {
			conflict++
		}
		if err := w.cmd.Wait(); err != nil {
			t.Fatal(err, string(data))
		}
	}
	if saved != 1 || conflict != 1 {
		t.Fatal(saved, conflict)
	}
	svc := NewTeamService(&teamTextOwnerRepo{}, nil, WithTeamRuntimeWorkspaceRoot(t.TempDir())).(*teamService)
	teamRoot := svc.teamRuntimeSharedPathFor(9, 17)
	_ = os.MkdirAll(teamRoot, 0755)
	_ = os.Symlink(filepath.Join(root, "a.md"), filepath.Join(teamRoot, "link.md"))
	_ = os.Link(filepath.Join(root, "a.md"), filepath.Join(teamRoot, "hard.md"))
	_ = os.Symlink(root, filepath.Join(teamRoot, "linked"))
	for _, name := range []string{"link.md", "hard.md", "linked/a.md"} {
		if _, err := svc.ReadWorkspaceText(ctx, 9, 17, name); !errors.Is(err, ErrWorkspaceTextUnsupported) {
			t.Fatal(name, err)
		}
	}
	info, err := os.Stat(filepath.Join(root, "a.md"))
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatal(info, err)
	}
}
