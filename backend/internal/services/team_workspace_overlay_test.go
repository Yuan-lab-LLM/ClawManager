package services

import (
	"os"
	"path/filepath"
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
	if _, err := os.Stat(filepath.Join(workspace, teamAgentsFileName)); !os.IsNotExist(err) {
		t.Fatalf("OpenClaw Team AGENTS.md must not be written to the unused workspace root: %v", err)
	}
}
