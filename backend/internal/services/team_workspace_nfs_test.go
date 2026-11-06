//go:build linux

package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNFSTeamPublicFileOperationsUseNarrowCredentialService(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("trusted credential helper requires root")
	}
	base, err := os.MkdirTemp("/tmp", "cm-team-public-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	os.Chmod(base, 0755)
	s := NewTeamService(&teamTextOwnerRepo{}, nil, WithTeamRuntimeWorkspaceRoot(base)).(*teamService)
	root := s.teamRuntimeSharedPathFor(9, 17)
	if err = os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	os.Chown(root, 10001, 1000)
	os.Chmod(root, 0770|os.ModeSetgid)
	p := filepath.Join(root, "private.txt")
	os.WriteFile(p, []byte("private"), 0600)
	os.Chown(p, 200012, 1000)
	writes := &workspaceWriteFake{}
	ConfigureTeamWorkspaceNFSIO(s, "nfs-rootless-v1", base, writes, nfsTeamGuard{member: WorkspaceFileScope{InstanceID: 12, UserID: 9, RuntimeGeneration: 4}})
	if _, err = s.PreviewWorkspaceFile(context.Background(), 9, 17, "private.txt"); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("private member content exposed: %v", err)
	}
	if err = s.CreateWorkspaceFolder(t.Context(), 9, 17, TeamWorkspaceFolderRequest{Name: "folder"}); err != nil {
		t.Fatal(err)
	}
	scope, _, err := s.teamTextScope(t.Context(), 9, 17, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.workspaceText.Upload(t.Context(), scope, "folder", "shared.txt", strings.NewReader("shared"), 6); err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewWorkspaceFile(t.Context(), 9, 17, "folder/shared.txt")
	if err != nil || preview.Content != "shared" {
		t.Fatal(preview, err)
	}
	data, err := s.DownloadWorkspaceFile(t.Context(), 9, 17, "folder")
	if err != nil || data.Name != "folder.zip" || len(data.Data) == 0 {
		t.Fatal(data, err)
	}
	if err = s.RenameWorkspaceEntry(t.Context(), 9, 17, TeamWorkspaceRenameRequest{Path: "folder/shared.txt", NewName: "moved.txt"}); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListWorkspaceFiles(t.Context(), 9, 17, "folder")
	if err != nil || len(list.Entries) != 1 || list.Entries[0].Name != "moved.txt" {
		t.Fatal(list, err)
	}
	if _, err = s.ListWorkspaceFiles(t.Context(), 8, 17, ""); err == nil {
		t.Fatal("foreign owner allowed")
	}
	if err = s.DeleteWorkspaceEntry(t.Context(), 9, 17, "folder"); err != nil {
		t.Fatal(err)
	}
	if writes.begins < 4 || writes.completes != writes.begins {
		t.Fatalf("Team CRUD bypassed durable write fencing: %+v", writes)
	}
}
