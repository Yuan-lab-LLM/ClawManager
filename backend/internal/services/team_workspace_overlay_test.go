package services

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"clawreef/internal/models"
)

func TestWriteManagedTeamWorkspaceOverlayPreservesOpenClawDefaultsAndReplacesOldOverlay(t *testing.T) {
	path := filepath.Join(t.TempDir(), teamAgentsFileName)
	if err := os.WriteFile(path, []byte("# Default workspace rules\n\nKeep this content.\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeManagedTeamWorkspaceOverlay(path, "# First Team\nmember_id=developer"); err != nil {
		t.Fatal(err)
	}
	if err := writeManagedTeamWorkspaceOverlay(path, "# Updated Team\nmember_id=reviewer"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, expected := range []string{"# Default workspace rules", "Keep this content.", "# Updated Team", "member_id=reviewer"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("overlay result missing %q: %s", expected, got)
		}
	}
	if strings.Contains(got, "# First Team") || strings.Count(got, teamManagedOverlayStart) != 1 {
		t.Fatalf("overlay should replace exactly one prior managed block: %s", got)
	}
}

func TestRepairLitePromptWorkspaceOwnershipUsesInstanceUIDWithoutRecursing(t *testing.T) {
	workspace := t.TempDir()
	promptWorkspace := filepath.Join(workspace, "home", ".openclaw", "workspace")
	nestedDir := filepath.Join(promptWorkspace, "skills")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{teamAgentsFileName, teamSoulFileName, teamConfigFileName, "TOOLS.md"} {
		if err := os.WriteFile(filepath.Join(promptWorkspace, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	unmanagedFile := filepath.Join(promptWorkspace, "USER.md")
	if err := os.WriteFile(unmanagedFile, []byte("preserve user ownership"), 0o644); err != nil {
		t.Fatal(err)
	}
	nestedFile := filepath.Join(nestedDir, "user-skill.md")
	if err := os.WriteFile(nestedFile, []byte("preserve"), 0o644); err != nil {
		t.Fatal(err)
	}

	original := chownLitePromptWorkspacePath
	t.Cleanup(func() { chownLitePromptWorkspacePath = original })
	type call struct {
		path     string
		uid, gid int
	}
	var calls []call
	chownLitePromptWorkspacePath = func(path string, uid, gid int) error {
		calls = append(calls, call{path: filepath.Clean(path), uid: uid, gid: gid})
		return nil
	}

	instance := &models.Instance{ID: 77, Type: "openclaw", InstanceMode: InstanceModeLite, WorkspacePath: &workspace}
	if err := repairLitePromptWorkspaceOwnership(instance); err != nil {
		t.Fatalf("repairLitePromptWorkspaceOwnership() error = %v", err)
	}
	sort.Slice(calls, func(i, j int) bool { return calls[i].path < calls[j].path })
	for _, got := range calls {
		if got.uid != RuntimeLinuxID(77) || got.gid != teamSharedGID {
			t.Fatalf("chown %s used %d:%d, want %d:%d", got.path, got.uid, got.gid, RuntimeLinuxID(77), teamSharedGID)
		}
		if got.path == nestedDir || got.path == nestedFile || got.path == unmanagedFile {
			t.Fatalf("ownership repair must not change user-managed prompt workspace paths")
		}
	}
	if len(calls) != 5 {
		t.Fatalf("ownership repair calls = %#v, want prompt root plus four immediate prompt files", calls)
	}
}

func TestRepairLitePromptWorkspaceOwnershipKeepsReadableRootSquashedFilesUsable(t *testing.T) {
	workspace := t.TempDir()
	promptWorkspace := filepath.Join(workspace, "home", ".openclaw", "workspace")
	if err := os.MkdirAll(promptWorkspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptWorkspace, "TOOLS.md"), []byte("tools"), 0o644); err != nil {
		t.Fatal(err)
	}
	original := chownLitePromptWorkspacePath
	t.Cleanup(func() { chownLitePromptWorkspacePath = original })
	chownLitePromptWorkspacePath = func(string, int, int) error {
		return os.ErrPermission
	}
	instance := &models.Instance{ID: 96, Type: "openclaw", InstanceMode: InstanceModeLite, WorkspacePath: &workspace}
	if err := repairLitePromptWorkspaceOwnership(instance); err != nil {
		t.Fatalf("readable root-squashed prompt files must remain compatible: %v", err)
	}
}

func TestPrepareHermesLitePromptRootsOwnsWorkerParentAndPromptRoots(t *testing.T) {
	workspace := t.TempDir()
	instance := &models.Instance{
		ID:            117,
		Type:          "hermes",
		InstanceMode:  InstanceModeLite,
		WorkspacePath: &workspace,
	}
	original := chownLitePromptWorkspacePath
	t.Cleanup(func() { chownLitePromptWorkspacePath = original })
	type call struct {
		path     string
		uid, gid int
	}
	var calls []call
	chownLitePromptWorkspacePath = func(path string, uid, gid int) error {
		calls = append(calls, call{path: filepath.Clean(path), uid: uid, gid: gid})
		return nil
	}

	roots, err := prepareHermesLitePromptRoots(instance)
	if err != nil {
		t.Fatalf("prepareHermesLitePromptRoots() error = %v", err)
	}
	workerRoot := filepath.Join(workspace, "home", ".clawmanager-team-worker")
	expectedRoots := []string{
		filepath.Join(workspace, "home", ".hermes"),
		filepath.Join(workerRoot, ".hermes"),
	}
	if len(roots) != len(expectedRoots) {
		t.Fatalf("prompt roots = %#v, want %#v", roots, expectedRoots)
	}
	for index := range expectedRoots {
		if filepath.Clean(roots[index]) != filepath.Clean(expectedRoots[index]) {
			t.Fatalf("prompt root %d = %q, want %q", index, roots[index], expectedRoots[index])
		}
	}
	expectedOwned := map[string]bool{
		filepath.Clean(workerRoot):       true,
		filepath.Clean(expectedRoots[0]): true,
		filepath.Clean(expectedRoots[1]): true,
	}
	for _, got := range calls {
		if !expectedOwned[got.path] {
			t.Fatalf("unexpected managed ownership repair: %#v", got)
		}
		delete(expectedOwned, got.path)
		if got.uid != RuntimeLinuxID(instance.ID) || got.gid != teamSharedGID {
			t.Fatalf("chown %s used %d:%d, want %d:%d", got.path, got.uid, got.gid, RuntimeLinuxID(instance.ID), teamSharedGID)
		}
		info, statErr := os.Lstat(got.path)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() ||
			(runtime.GOOS != "windows" && info.Mode().Perm() != 0o750) {
			t.Fatalf("managed directory %s has unsafe mode %v", got.path, info.Mode())
		}
	}
	if len(expectedOwned) != 0 {
		t.Fatalf("managed directories were not assigned to the runtime: %#v", expectedOwned)
	}
}

func TestPrepareHermesLitePromptRootsRejectsUnrepairableWorkerRoot(t *testing.T) {
	workspace := t.TempDir()
	instance := &models.Instance{ID: 118, Type: "hermes", InstanceMode: InstanceModeLite, WorkspacePath: &workspace}
	original := chownLitePromptWorkspacePath
	t.Cleanup(func() { chownLitePromptWorkspacePath = original })
	chownLitePromptWorkspacePath = func(path string, _, _ int) error {
		if filepath.Clean(path) == filepath.Join(workspace, "home", ".clawmanager-team-worker") {
			return os.ErrPermission
		}
		return nil
	}

	if _, err := prepareHermesLitePromptRoots(instance); err == nil || !strings.Contains(err.Error(), "set runtime owner") {
		t.Fatalf("prepareHermesLitePromptRoots() error = %v, want ownership failure", err)
	}
}

func TestPrepareHermesLitePromptRootsRejectsSymlinkedWorkerRoot(t *testing.T) {
	workspace := t.TempDir()
	home := filepath.Join(workspace, "home")
	if err := os.MkdirAll(home, 0o750); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	workerRoot := filepath.Join(home, ".clawmanager-team-worker")
	if err := os.Symlink(outside, workerRoot); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	instance := &models.Instance{ID: 119, Type: "hermes", InstanceMode: InstanceModeLite, WorkspacePath: &workspace}
	original := chownLitePromptWorkspacePath
	t.Cleanup(func() { chownLitePromptWorkspacePath = original })
	chownLitePromptWorkspacePath = func(string, int, int) error { return nil }

	if _, err := prepareHermesLitePromptRoots(instance); err == nil || !strings.Contains(err.Error(), "real directory") {
		t.Fatalf("prepareHermesLitePromptRoots() error = %v, want symlink rejection", err)
	}
}

func TestWriteLiteOpenClawTeamIdentityFilesUseInjectedWorkspace(t *testing.T) {
	workspace := t.TempDir()
	plans, err := planTeamMembers("team", []CreateTeamMemberRequest{{MemberID: "leader", Role: "leader"}})
	if err != nil {
		t.Fatal(err)
	}
	team := &models.Team{ID: 77, CommunicationMode: teamCommunicationModeLeaderMediated, SharedMountPath: "/team"}
	instance := &models.Instance{Type: "openclaw", InstanceMode: InstanceModeLite, WorkspacePath: &workspace}
	actualAgents := filepath.Join(workspace, "home", ".openclaw", "workspace", teamAgentsFileName)
	if err := os.MkdirAll(filepath.Dir(actualAgents), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(actualAgents, []byte("# OpenClaw default\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := (&teamService{}).writeLiteTeamMemberIdentityFiles(instance, team, plans[0], `{"members":[{"memberId":"leader"}]}`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{teamAgentsFileName, teamSoulFileName} {
		data, readErr := os.ReadFile(filepath.Join(workspace, "home", ".openclaw", "workspace", name))
		if readErr != nil || !strings.Contains(string(data), teamManagedOverlayStart) {
			t.Fatalf("injected %s invalid: data=%q err=%v", name, string(data), readErr)
		}
		if name == teamSoulFileName && !strings.Contains(string(data), "Member ID: leader") {
			t.Fatalf("injected SOUL.md missing member identity: %s", string(data))
		}
	}
	agents, err := os.ReadFile(actualAgents)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"## Leader Team Context Preflight",
		"./team.json",
		"./team-introduction.md",
		"$CLAWMANAGER_TEAM_SHARED_DIR/team.json",
	} {
		if !strings.Contains(string(agents), expected) {
			t.Fatalf("Leader AGENTS.md missing %q: %s", expected, string(agents))
		}
	}
	roster, err := os.ReadFile(filepath.Join(workspace, "home", ".openclaw", "workspace", teamConfigFileName))
	if err != nil || !strings.Contains(string(roster), `"memberId":"leader"`) {
		t.Fatalf("injected team.json invalid: data=%q err=%v", string(roster), err)
	}
	if _, err := os.Stat(filepath.Join(workspace, teamAgentsFileName)); !os.IsNotExist(err) {
		t.Fatalf("OpenClaw Team AGENTS.md must not be written to the unused workspace root: %v", err)
	}
}
