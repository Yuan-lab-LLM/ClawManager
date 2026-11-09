package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWorkspaceTextEditPreservesAndRejectsConflicts(t *testing.T) {
	root := t.TempDir()
	scope := workspaceTestScope(root)
	p := filepath.Join(root, "config.yaml")
	original := "\ufeffname: 中文\r\n"
	if err := os.WriteFile(p, []byte(original), 0750); err != nil {
		t.Fatal(err)
	}
	s := NewWorkspaceFileService(nil).(WorkspaceTextService)
	doc, err := s.ReadText(context.Background(), scope, "config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Text != original || doc.Version == "" {
		t.Fatal("lost original bytes or version")
	}
	updated, err := s.SaveText(context.Background(), scope, "config.yaml", original+"ok: true\r\n", doc.Version)
	if err != nil || updated.Version == doc.Version {
		t.Fatalf("save: %v", err)
	}
	if _, err = s.SaveText(context.Background(), scope, "config.yaml", "stale", doc.Version); !errors.Is(err, ErrWorkspaceTextConflict) {
		t.Fatalf("expected conflict: %v", err)
	}
	data, _ := os.ReadFile(p)
	if string(data) != updated.Text {
		t.Fatal("conflict overwrote file")
	}
}

func TestWorkspaceTextValidation(t *testing.T) {
	root := t.TempDir()
	s := NewWorkspaceFileService(nil).(WorkspaceTextService)
	scope := workspaceTestScope(root)
	cases := map[string][]byte{"bad.txt": {0, 1}, "invalid.md": {255}, "state.db": []byte("text"), "huge.txt": []byte(strings.Repeat("a", 1024*1024+1))}
	for name, data := range cases {
		if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ReadText(context.Background(), scope, name); err == nil {
			t.Errorf("accepted %s", name)
		}
	}
	for _, name := range []string{".env", ".env.local", "Dockerfile", "config.toml", "test.sh", "empty.txt"} {
		os.WriteFile(filepath.Join(root, name), nil, 0600)
		if _, err := s.ReadText(context.Background(), scope, name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := s.ReadText(context.Background(), scope, "../outside.txt"); err == nil {
		t.Fatal("accepted traversal")
	}
}

func TestWorkspaceTextConcurrentSave(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("base"), 0600)
	s := NewWorkspaceFileService(nil).(WorkspaceTextService)
	scope := workspaceTestScope(root)
	doc, _ := s.ReadText(context.Background(), scope, "a.txt")
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, v := range []string{"one", "two"} {
		wg.Add(1)
		go func(value string) {
			defer wg.Done()
			_, err := s.SaveText(context.Background(), scope, "a.txt", value, doc.Version)
			results <- err
		}(v)
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrWorkspaceTextConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}
