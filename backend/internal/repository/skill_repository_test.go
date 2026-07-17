package repository

import "testing"

func TestWorkspaceDeleteTargetsSkillKeyHermesNestedPath(t *testing.T) {
	path := "home/.hermes/skills/productivity/my-skill"
	if !workspaceDeleteTargetsSkillKey(path, "my-skill") {
		t.Fatalf("expected nested hermes skill path to match")
	}
}

func TestWorkspaceDeleteTargetsSkillKeyOpenClawFlatPath(t *testing.T) {
	path := "home/.openclaw/workspace/skills/paper-ranker"
	if !workspaceDeleteTargetsSkillKey(path, "paper-ranker") {
		t.Fatalf("expected openclaw flat skill path to match")
	}
}

func TestWorkspaceDeleteTargetsSkillKeyRejectsMismatchedLeaf(t *testing.T) {
	path := "home/.hermes/skills/productivity/other-skill"
	if workspaceDeleteTargetsSkillKey(path, "my-skill") {
		t.Fatalf("expected mismatched leaf to be rejected")
	}
}
