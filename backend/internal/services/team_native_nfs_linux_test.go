//go:build linux

package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestTeamNativeNFSIdentityPreservesPrivateInstructions(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires disposable credential fixture")
	}
	root, err := os.MkdirTemp("/srv", "cm-team-native-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	if err = os.Chown(root, 10001, 10001); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(root, 0711); err != nil {
		t.Fatal(err)
	}
	grant := teamNativeRequest{Root: root, RuntimeType: "openclaw", UserID: 1, InstanceID: 2, Action: "grant_instance"}
	if ended, err := runTeamNativeHelper(context.Background(), grant); err != nil || !ended {
		t.Fatalf("grant: %t %v", ended, err)
	}
	request := grant
	request.Action = "identity"
	request.Identity = map[string]string{teamAgentsFileName: "managed role", teamSoulFileName: "managed soul"}
	request.Context = map[string]string{teamConfigFileName: `{"team_id":7}`}
	if ended, err := runTeamNativeHelper(context.Background(), request); err != nil || !ended {
		t.Fatalf("identity: %t %v", ended, err)
	}
	prompt := filepath.Join(root, "openclaw", "user-1", "instance-2", "home", ".openclaw", "workspace")
	target := filepath.Join(prompt, teamAgentsFileName)
	if err = os.WriteFile(target, []byte("private user instructions\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chown(target, 200002, 200002); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(prompt, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if ended, err := runTeamNativeHelper(context.Background(), request); err != nil || !ended {
			t.Fatalf("overlay: %t %v", ended, err)
		}
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "private user instructions") || strings.Count(string(data), teamManagedOverlayStart) != 1 {
		t.Fatal("private overlay was replaced or duplicated")
	}
	info, _ := os.Stat(target)
	if info.Mode().Perm() != 0600 || info.Sys().(*syscall.Stat_t).Uid != 200002 {
		t.Fatal("private prompt ownership/mode changed")
	}
	info, _ = os.Stat(prompt)
	if info.Mode().Perm() != 0700 {
		t.Fatal("private prompt directory permissions widened")
	}
}
