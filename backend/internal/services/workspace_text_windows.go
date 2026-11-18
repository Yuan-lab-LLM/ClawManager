//go:build windows

package services

import (
	"context"
	"os"
	"sync"
)

const workspaceTextOpenFlags = 0

// Development fallback; production images run Linux and use cross-process flock.
var workspaceEditorMutex sync.Mutex

func workspaceHasMultipleLinks(info os.FileInfo) bool                  { return false }
func preserveWorkspaceTextOwner(file *os.File, info os.FileInfo) error { return nil }
func lockWorkspaceEditor(ctx context.Context, root *os.Root, _ os.FileMode) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	workspaceEditorMutex.Lock()
	return workspaceEditorMutex.Unlock, nil
}

func inspectWorkspaceUpload(root *os.Root, name string) (os.FileInfo, error) {
	return nil, os.ErrPermission
}
func workspaceUploadIdentity(info os.FileInfo) string { return "unsupported" }

func validateWorkspacePrivateInode(WorkspaceFileScope, os.FileInfo) error { return nil }
