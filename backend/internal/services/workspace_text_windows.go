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
func lockWorkspaceEditor(ctx context.Context, root *os.Root) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	workspaceEditorMutex.Lock()
	return workspaceEditorMutex.Unlock, nil
}
