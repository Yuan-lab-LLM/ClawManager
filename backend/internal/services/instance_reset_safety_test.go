package services

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"clawreef/internal/models"
)

// Factory reset may erase the exact workspace/PVC, but it must not broaden into
// deleting the instance record, account namespace or unrelated resources.
func TestResetUsesNarrowFactoryResetPaths(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "instance_service.go", nil, 0)
	if err != nil {
		t.Fatalf("parse instance_service.go: %v", err)
	}
	var resetBody *ast.BlockStmt
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Recv != nil && function.Name.Name == "Reset" {
			resetBody = function.Body
			break
		}
	}
	if resetBody == nil {
		t.Fatal("Reset method not found")
	}
	forbidden := map[string]bool{"DeleteAllInstanceResources": true, "CleanupInstance": true}
	required := map[string]bool{
		"resetV2Workspace": false, "DeletePVCByName": false,
		"resetInstanceRuntimeData": false, "prepareFreshRuntimeCredentials": false,
	}
	ast.Inspect(resetBody, func(node ast.Node) bool {
		switch expression := node.(type) {
		case *ast.SelectorExpr:
			if forbidden[expression.Sel.Name] {
				t.Errorf("Reset must not call broad cleanup method %s", expression.Sel.Name)
			}
			if _, ok := required[expression.Sel.Name]; ok {
				required[expression.Sel.Name] = true
			}
		case *ast.Ident:
			if forbidden[expression.Name] {
				t.Errorf("Reset must not enter broad cleanup path %s", expression.Name)
			}
			if _, ok := required[expression.Name]; ok {
				required[expression.Name] = true
			}
		}
		return true
	})
	for name, found := range required {
		if !found {
			t.Errorf("Reset does not use required factory-reset path %s", name)
		}
	}
}

func TestResetV2WorkspaceErasesOnlyExpectedInstanceDirectory(t *testing.T) {
	root := t.TempDir()
	service := &instanceService{workspaceRoot: root}
	workspace := RuntimeWorkspacePathWithRoot(root, "openclaw", 42, 77)
	if err := os.MkdirAll(filepath.Join(workspace, "home", "project"), 0750); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(workspace, "home", "project", "user-data.txt")
	if err := os.WriteFile(marker, []byte("must be erased"), 0640); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0640); err != nil {
		t.Fatal(err)
	}
	instance := &models.Instance{ID: 77, UserID: 42, WorkspacePath: &workspace}
	if err := service.resetV2Workspace(instance, "openclaw"); err != nil {
		t.Fatalf("resetV2Workspace returned error: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("workspace marker still exists, err=%v", err)
	}
	if info, err := os.Stat(workspace); err != nil || !info.IsDir() {
		t.Fatalf("fresh workspace was not recreated, info=%v err=%v", info, err)
	}
	if body, err := os.ReadFile(outside); err != nil || string(body) != "keep" {
		t.Fatalf("outside file was changed, body=%q err=%v", string(body), err)
	}
}

func TestResetV2WorkspaceRejectsUnexpectedPath(t *testing.T) {
	root := t.TempDir()
	service := &instanceService{workspaceRoot: root}
	unsafe := root
	instance := &models.Instance{ID: 77, UserID: 42, WorkspacePath: &unsafe}
	if err := service.resetV2Workspace(instance, "openclaw"); err == nil {
		t.Fatal("expected workspace root reset to be rejected")
	}
}
