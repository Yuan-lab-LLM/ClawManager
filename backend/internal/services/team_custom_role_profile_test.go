package services

import (
	"encoding/json"
	"strings"
	"testing"

	"clawreef/internal/models"
)

type compiledTeamAgentProfilePayload struct {
	Items []struct {
		Content struct {
			Config struct {
				ProfileKey         string   `json:"profileKey"`
				BaseProfileKey     string   `json:"baseProfileKey"`
				SystemPrompt       string   `json:"systemPrompt"`
				CollaborationRules []string `json:"collaborationRules"`
				OutputContract     []string `json:"outputContract"`
			} `json:"config"`
		} `json:"content"`
	} `json:"items"`
}

func TestPlanTeamMembersCompilesCustomRoleProfileIntoExistingIdentityFlow(t *testing.T) {
	description := "Owns evidence-backed market research."
	plans, err := planTeamMembers("research-team", []CreateTeamMemberRequest{
		{
			MemberID: "leader", Name: "Leader", Role: "leader", IsLeader: true,
			RuntimeType: "openclaw", Mode: InstanceModeLite,
			RoleProfile: &TeamMemberRoleProfileRequest{
				DisplayName: "Research Leader", RoleHint: "leader", Summary: "Coordinates the research team.",
				Mission: "Decompose the goal and synthesize the final answer.",
			},
		},
		{
			MemberID: "market-researcher", Name: "Market Researcher", Role: "researcher",
			RuntimeType: "openclaw", Mode: InstanceModeLite, Description: &description,
			RoleProfile: &TeamMemberRoleProfileRequest{
				ProfileKey: "custom.team-template.7.market-researcher", DisplayName: "Market Researcher",
				RoleHint: "market-researcher", Summary: description, Mission: "Research the market with traceable evidence.",
				Responsibilities: []string{"Compare competitors", "Cite primary sources"},
				Boundaries:       []string{"Do not implement product code"},
				Deliverables:     []string{"Competitive analysis"},
			},
		},
	})
	if err != nil {
		t.Fatalf("planTeamMembers returned error: %v", err)
	}
	if got := plans[1].EffectiveRole; got != "market-researcher" {
		t.Fatalf("EffectiveRole = %q, want market-researcher", got)
	}
	if got := plans[1].ProfileKey; got != "custom.team-template.7.market-researcher" {
		t.Fatalf("ProfileKey = %q", got)
	}
	leaderPrompt := plans[0].Request.EnvironmentOverrides["CLAWMANAGER_RUNTIME_SYSTEM_PROMPT"]
	for _, expected := range []string{
		"You are the Team Leader and orchestration controller.",
		"Decompose the goal and synthesize the final answer.",
	} {
		if !strings.Contains(leaderPrompt, expected) {
			t.Fatalf("planned custom Leader prompt missing %q:\n%s", expected, leaderPrompt)
		}
	}
	leaderSoul := buildTeamMemberSoulMarkdown(plans[0], teamCommunicationModeLeaderMediated)
	for _, expected := range []string{
		"You are the Team Leader and orchestration controller.",
		"Decompose the goal and synthesize the final answer.",
		"This is a strict hub-and-spoke workflow isolated from worker-direct flow.",
	} {
		if !strings.Contains(leaderSoul, expected) {
			t.Fatalf("custom Leader SOUL.md missing %q:\n%s", expected, leaderSoul)
		}
	}
	leaderAgents := buildTeamMemberAgentsMarkdown(&models.Team{
		ID: 7, CommunicationMode: teamCommunicationModeLeaderMediated,
	}, plans[0])
	for _, expected := range []string{"team_complete_task once", "Leader Team Context Preflight", "read ./team.json and ./team-introduction.md"} {
		if !strings.Contains(leaderAgents, expected) {
			t.Fatalf("custom Leader AGENTS.md missing %q:\n%s", expected, leaderAgents)
		}
	}
	soul := buildTeamMemberSoulMarkdown(plans[1], teamCommunicationModeLeaderMediated)
	for _, expected := range []string{"Research the market with traceable evidence.", "Compare competitors", "Do not implement product code", "Competitive analysis"} {
		if !strings.Contains(soul, expected) {
			t.Fatalf("SOUL.md missing %q:\n%s", expected, soul)
		}
	}
	if !strings.Contains(plans[1].Request.EnvironmentOverrides["CLAWMANAGER_HERMES_SYSTEM_PROMPT"], "Compare competitors") {
		t.Fatal("Hermes compatibility system prompt did not receive the custom role")
	}
	if description := plannedTeamMemberDescription(plans[1]); !strings.Contains(description, "Compare competitors") || !strings.Contains(description, "Do not implement product code") {
		t.Fatalf("Leader-facing member description did not receive the complete generated role: %s", description)
	}
	roster, err := buildTeamRosterConfig(&models.Team{
		ID: 7, CommunicationMode: teamCommunicationModeLeaderMediated, SharedMountPath: "/team",
	}, plans)
	if err != nil {
		t.Fatalf("buildTeamRosterConfig returned error: %v", err)
	}
	for _, expected := range []string{"custom.team-template.7.market-researcher", "Compare competitors", "Do not implement product code", "Competitive analysis"} {
		if !strings.Contains(roster, expected) {
			t.Fatalf("team.json roster missing %q:\n%s", expected, roster)
		}
	}
}

func TestCompileCustomLeaderRoleProfileComposesImmutableOrchestratorBase(t *testing.T) {
	const duplicateBaseRule = "Default to leader-mediated collaboration: user tasks enter through the Leader, then fan out to members."
	member, err := compileTeamMemberRoleProfile(CreateTeamMemberRequest{
		MemberID: "leader", Name: "Chief Editor", Role: "leader",
		RuntimeType: "openclaw", Mode: InstanceModeLite,
		// Keep IsLeader false to cover old/API clients that identify the Leader by
		// the canonical role. planTeamMembers recognizes the same shape.
		RoleProfile: &TeamMemberRoleProfileRequest{
			ProfileKey:  "custom.team-template.9.leader",
			DisplayName: "Chief Editor",
			RoleHint:    "leader",
			Summary:     "Owns the daily briefing.",
			Mission:     "Publish an evidence-backed daily briefing.",
			Responsibilities: []string{
				"Assign research beats to the right workers",
				"Reconcile all worker findings before publication",
			},
			Deliverables:       []string{"final_answer", "Daily briefing"},
			AcceptanceCriteria: []string{"Every claim is traceable"},
			CollaborationNotes: []string{duplicateBaseRule, "Resolve conflicting sources with the assigned reviewers"},
		},
	})
	if err != nil {
		t.Fatalf("compileTeamMemberRoleProfile returned error: %v", err)
	}

	systemPrompt := member.EnvironmentOverrides["CLAWMANAGER_RUNTIME_SYSTEM_PROMPT"]
	for _, expected := range []string{
		"You are the Team Leader and orchestration controller.",
		"Prefer coordination over doing all work yourself.",
		"Publish an evidence-backed daily briefing.",
		"Assign research beats to the right workers",
		"Do not mark work complete until member outputs have been reconciled",
		"Expected output contract:",
		"open_risks",
		"Daily briefing",
	} {
		if !strings.Contains(systemPrompt, expected) {
			t.Fatalf("custom Leader system prompt missing %q:\n%s", expected, systemPrompt)
		}
	}
	if count := strings.Count(systemPrompt, duplicateBaseRule); count != 1 {
		t.Fatalf("base collaboration rule appears %d times, want exactly once:\n%s", count, systemPrompt)
	}
	for _, key := range []string{
		"CLAWMANAGER_OPENCLAW_AGENTS_JSON",
		"CLAWMANAGER_HERMES_AGENTS_JSON",
		"CLAWMANAGER_HERMES_SYSTEM_PROMPT",
		"CLAWMANAGER_AGENT_SYSTEM_PROMPT",
		"HERMES_SYSTEM_PROMPT",
	} {
		value := member.EnvironmentOverrides[key]
		if value == "" {
			t.Fatalf("custom Leader compatibility alias %s is empty", key)
		}
	}
	if got := member.EnvironmentOverrides["CLAWMANAGER_HERMES_SYSTEM_PROMPT"]; got != systemPrompt {
		t.Fatal("Hermes and Runtime custom Leader system prompts diverged")
	}

	var payload compiledTeamAgentProfilePayload
	if err := json.Unmarshal([]byte(member.EnvironmentOverrides["CLAWMANAGER_RUNTIME_AGENTS_JSON"]), &payload); err != nil {
		t.Fatalf("failed to decode compiled custom Leader profile: %v", err)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("compiled profile item count = %d, want 1", len(payload.Items))
	}
	config := payload.Items[0].Content.Config
	if config.ProfileKey != "custom.team-template.9.leader" {
		t.Fatalf("custom profile key = %q", config.ProfileKey)
	}
	if config.BaseProfileKey != "agency.agents-orchestrator" {
		t.Fatalf("base profile key = %q, want agency.agents-orchestrator", config.BaseProfileKey)
	}
	for _, expected := range []string{"task_breakdown", "assignments", "member_results", "verification", "final_answer", "open_risks", "Daily briefing", "Every claim is traceable"} {
		if countString(config.OutputContract, expected) != 1 {
			t.Fatalf("output contract must contain %q exactly once: %#v", expected, config.OutputContract)
		}
	}
	if countString(config.CollaborationRules, duplicateBaseRule) != 1 {
		t.Fatalf("collaboration rules did not deduplicate the base rule: %#v", config.CollaborationRules)
	}
}

func TestCompileCustomWorkerRoleProfileDoesNotInheritOrchestratorBase(t *testing.T) {
	member, err := compileTeamMemberRoleProfile(CreateTeamMemberRequest{
		MemberID: "writer", Name: "Writer", Role: "writer",
		RuntimeType: "openclaw", Mode: InstanceModeLite,
		RoleProfile: &TeamMemberRoleProfileRequest{
			ProfileKey: "custom.team-template.9.writer", DisplayName: "Writer",
			Mission: "Draft the assigned briefing section.",
		},
	})
	if err != nil {
		t.Fatalf("compileTeamMemberRoleProfile returned error: %v", err)
	}
	if prompt := member.EnvironmentOverrides["CLAWMANAGER_RUNTIME_SYSTEM_PROMPT"]; strings.Contains(prompt, "Team Leader and orchestration controller") {
		t.Fatalf("custom Worker inherited Leader behavior:\n%s", prompt)
	}
	var payload compiledTeamAgentProfilePayload
	if err := json.Unmarshal([]byte(member.EnvironmentOverrides["CLAWMANAGER_RUNTIME_AGENTS_JSON"]), &payload); err != nil {
		t.Fatalf("failed to decode compiled custom Worker profile: %v", err)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("compiled profile item count = %d, want 1", len(payload.Items))
	}
	if base := payload.Items[0].Content.Config.BaseProfileKey; base != "" {
		t.Fatalf("custom Worker base profile key = %q, want empty", base)
	}
}

func TestCompileMemberWithoutCustomRoleProfileLeavesFixedProfileUntouched(t *testing.T) {
	overrides := map[string]string{
		"CLAWMANAGER_RUNTIME_SYSTEM_PROMPT": "fixed-template-leader-prompt",
		"CLAWMANAGER_RUNTIME_AGENTS_JSON":   `{"schemaVersion":1,"items":[]}`,
		"USER_DEFINED_VALUE":                "preserve-me",
	}
	member, err := compileTeamMemberRoleProfile(CreateTeamMemberRequest{
		MemberID: "leader", Name: "Leader", Role: "leader", IsLeader: true,
		RuntimeType: "openclaw", Mode: InstanceModeLite, EnvironmentOverrides: overrides,
	})
	if err != nil {
		t.Fatalf("compileTeamMemberRoleProfile returned error: %v", err)
	}
	if len(member.EnvironmentOverrides) != len(overrides) {
		t.Fatalf("fixed profile overrides changed size: %#v", member.EnvironmentOverrides)
	}
	for key, expected := range overrides {
		if got := member.EnvironmentOverrides[key]; got != expected {
			t.Fatalf("fixed profile override %s = %q, want %q", key, got, expected)
		}
	}
}

func countString(values []string, expected string) int {
	count := 0
	for _, value := range values {
		if value == expected {
			count++
		}
	}
	return count
}
