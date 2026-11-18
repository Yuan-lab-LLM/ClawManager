//go:build linux

package services

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestNFSPersistedForeignInodeFileReaders(t *testing.T) {
	root := os.Getenv("CM_INODE_FIXTURE")
	if root == "" || os.Geteuid() != 0 {
		t.Skip("requires dedicated root-squash fixture")
	}
	for _, kind := range []string{"hardlink", "singlelink", "own-hardlink"} {
		t.Run(kind, func(t *testing.T) {
			scope := WorkspaceFileScope{UserID: 3, InstanceID: 12, RuntimeGeneration: 1, WorkspacePath: filepath.Join(root, kind, "openclaw", "user-3", "instance-12")}
			s := NewWorkspaceFileService(&recordingWorkspaceFileAuditRepo{}).(*workspaceFileService)
			ConfigureWorkspaceNFSIO(s, "nfs-rootless-v1", filepath.Join(root, kind), &workspaceWriteFake{})
			for _, action := range []string{"download", "read_text", "preview", "archive"} {
				var err error
				switch action {
				case "read_text":
					_, err = s.ReadText(context.Background(), scope, "foreign.txt")
				case "preview":
					_, err = s.Preview(context.Background(), scope, "foreign.txt")
				default:
					name := "foreign.txt"
					if action == "archive" {
						name = "folder"
					}
					var file io.ReadCloser
					file, _, _, err = s.OpenDownload(context.Background(), scope, name)
					if file != nil {
						file.Close()
					}
				}
				// Text editing already deliberately excludes all hardlinks.
				valid := kind == "own-hardlink" && action != "read_text"
				if valid && err != nil {
					t.Errorf("%s own hardlink rejected: %v", action, err)
				}
				if !valid && err == nil {
					t.Errorf("%s consumed persisted foreign inode", action)
				}
			}
		})
	}
}

func TestNFSPersistedForeignPrivateNativeOverlay(t *testing.T) {
	root := os.Getenv("CM_INODE_FIXTURE")
	if root == "" || os.Geteuid() != 0 {
		t.Skip("requires dedicated root-squash fixture")
	}
	for _, kind := range []string{"hardlink", "singlelink"} {
		request := teamNativeRequest{Root: filepath.Join(root, kind), RuntimeType: "openclaw", UserID: 3, InstanceID: 12, Generation: 1, Action: "identity", Identity: map[string]string{teamAgentsFileName: "managed role"}}
		if _, err := runTeamNativeHelper(context.Background(), request); err == nil {
			t.Errorf("%s private native overlay consumed foreign inode", kind)
		}
	}
}
