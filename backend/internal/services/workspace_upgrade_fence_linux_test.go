//go:build linux

package services

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceUpgradeFenceUsesPermanentNFSInode(t *testing.T) {
	root := t.TempDir()
	a := &RuntimeUpgradeService{workspaceRoot: root}
	b := &RuntimeUpgradeService{workspaceRoot: root}
	release, err := a.lockWorkspaceOperations([]int{481})
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(root, ".clawmanager-upgrades", "operations", "instance-481.lock")
	before, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if next, err := b.lockWorkspaceOperations([]int{481}); err == nil {
		next()
		t.Fatal("concurrent upgrade/editor lock admitted")
	} else if !errors.Is(err, ErrWorkspaceUpgradeBusy) {
		t.Fatal(err)
	}
	release()
	next, err := b.lockWorkspaceOperations([]int{481})
	if err != nil {
		t.Fatal(err)
	}
	defer next()
	after, err := os.Stat(name)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("operation lock inode was replaced")
	}
}
