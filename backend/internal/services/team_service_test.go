package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestTeamMemberEnvUsesSecretBackedRedisAndToken(t *testing.T) {
	t.Setenv("CLAWMANAGER_TEAM_MANAGER_BASE_URL", "http://manager.example")

	service := &teamService{}
	env := service.teamMemberEnv(&models.Team{
		ID:              12,
		SharedMountPath: "/team",
	}, plannedTeamMember{
		MemberKey: "leader",
		Role:      "lead",
	})

	if env["CLAWMANAGER_TEAM_ID"] != "12" {
		t.Fatalf("expected Team id env, got %q", env["CLAWMANAGER_TEAM_ID"])
	}
	if env["CLAWMANAGER_TEAM_MEMBER_ID"] != "leader" {
		t.Fatalf("expected member id env, got %q", env["CLAWMANAGER_TEAM_MEMBER_ID"])
	}
	if env["CLAWMANAGER_TEAM_ROLE"] != "lead" {
		t.Fatalf("expected Team role env, got %q", env["CLAWMANAGER_TEAM_ROLE"])
	}
	if env["CLAWMANAGER_TEAM_INBOX_KEY"] != "claw:team:12:inbox:leader" {
		t.Fatalf("unexpected inbox key: %q", env["CLAWMANAGER_TEAM_INBOX_KEY"])
	}
	if env["CLAWMANAGER_TEAM_EVENTS_KEY"] != "claw:team:12:events" {
		t.Fatalf("unexpected events key: %q", env["CLAWMANAGER_TEAM_EVENTS_KEY"])
	}
	if env["CLAWMANAGER_TEAM_MANAGER_URL"] != "http://manager.example" {
		t.Fatalf("unexpected manager url: %q", env["CLAWMANAGER_TEAM_MANAGER_URL"])
	}
	if env["CLAWMANAGER_TEAM_CONFIG_PATH"] != "/etc/clawmanager/team/team.json" {
		t.Fatalf("unexpected Team config path: %q", env["CLAWMANAGER_TEAM_CONFIG_PATH"])
	}
	if env["CLAWMANAGER_TEAM_SHARED_UID"] != "1000" || env["CLAWMANAGER_TEAM_SHARED_GID"] != "1000" || env["CLAWMANAGER_TEAM_UMASK"] != "0002" {
		t.Fatalf("expected Team shared permission env, got %#v", env)
	}
	if env["PUID"] != "1000" || env["PGID"] != "1000" || env["UMASK"] != "0002" {
		t.Fatalf("expected runtime shared permission env, got %#v", env)
	}
	if env["CLAWMANAGER_TEAM_AUTORUN"] != "true" || env["CLAWMANAGER_TEAM_CONSUMER_GROUP"] != "team-members" {
		t.Fatalf("expected Team autorun and consumer group env, got %#v", env)
	}
	for key := range env {
		if strings.Contains(key, "REDIS_URL") || strings.Contains(key, "TOKEN") {
			t.Fatalf("sensitive Team env %s must come from Secret, not plain env", key)
		}
	}
}

func TestTeamMemberEnvInjectsRoleGuidance(t *testing.T) {
	t.Setenv("CLAWMANAGER_TEAM_MANAGER_BASE_URL", "http://manager.example")
	description := "Senior Developer: implements scoped changes and reports verification."

	service := &teamService{}
	env := service.teamMemberEnv(&models.Team{
		ID:              12,
		SharedMountPath: "/team",
	}, plannedTeamMember{
		MemberKey:   "worker",
		DisplayName: "team-worker",
		Role:        "senior-developer",
		Request: CreateTeamMemberRequest{
			Description: &description,
		},
	})

	if env["CLAWMANAGER_TEAM_ROLE"] != "senior-developer" {
		t.Fatalf("expected specific Team role env, got %q", env["CLAWMANAGER_TEAM_ROLE"])
	}
	if env["CLAWMANAGER_TEAM_MEMBER_DESCRIPTION"] != description {
		t.Fatalf("expected description env, got %q", env["CLAWMANAGER_TEAM_MEMBER_DESCRIPTION"])
	}
	if !strings.Contains(env["CLAWMANAGER_TEAM_SYSTEM_PROMPT"], "senior-developer") {
		t.Fatalf("expected Team system prompt to include role, got %q", env["CLAWMANAGER_TEAM_SYSTEM_PROMPT"])
	}
	if env["HERMES_AGENT_HELP_GUIDANCE"] != env["CLAWMANAGER_TEAM_SYSTEM_PROMPT"] {
		t.Fatalf("expected Hermes guidance alias to match Team system prompt")
	}
	for _, expected := range []string{"exact CLAWMANAGER_TEAM_SHARED_DIR", "team/... is invalid", "/team/<relative-path>"} {
		if !strings.Contains(env["CLAWMANAGER_TEAM_SYSTEM_PROMPT"], expected) {
			t.Fatalf("expected shared workspace guidance %q, got %q", expected, env["CLAWMANAGER_TEAM_SYSTEM_PROMPT"])
		}
	}
}

func TestCleanTeamWorkspacePathTreatsTeamPrefixAsLogicalAlias(t *testing.T) {
	cases := map[string]string{
		"/team/results/report.md":  "results/report.md",
		"team/results/report.md":   "results/report.md",
		"./team/results/report.md": "results/report.md",
		"/team":                    "",
		"team":                     "",
		"results/report.md":        "results/report.md",
	}

	for raw, expected := range cases {
		cleaned, err := cleanTeamWorkspacePath(raw)
		if err != nil {
			t.Fatalf("cleanTeamWorkspacePath(%q) returned error: %v", raw, err)
		}
		if cleaned != expected {
			t.Fatalf("cleanTeamWorkspacePath(%q) = %q, want %q", raw, cleaned, expected)
		}
	}
}

func TestBuildTeamMemberInstanceRequestUsesSharedPermissionDefaults(t *testing.T) {
	service := &teamService{}
	pvcName := "clawreef-team-7-shared"
	secretName := "clawreef-team-7-bus"
	req := service.buildTeamMemberInstanceRequest(&models.Team{
		ID:                  7,
		Name:                "Delivery",
		SharedPVCName:       &pvcName,
		SharedMountPath:     "/team",
		TeamTokenSecretName: &secretName,
	}, plannedTeamMember{
		MemberKey:   "delivery-lead",
		DisplayName: "delivery lead",
		Role:        "leader",
		RuntimeType: "openclaw",
	})

	if req.Team == nil {
		t.Fatalf("expected Team instance config")
	}
	if req.Team.SharedUID != 1000 || req.Team.SharedGID != 1000 || req.Team.SharedUmask != "0002" {
		t.Fatalf("unexpected Team shared permission config: %#v", req.Team)
	}
	if req.Team.ConfigMountPath != "/etc/clawmanager/team" {
		t.Fatalf("unexpected Team config mount path: %q", req.Team.ConfigMountPath)
	}
	if req.Team.Environment["CLAWMANAGER_TEAM_CONFIG_PATH"] != "/etc/clawmanager/team/team.json" {
		t.Fatalf("unexpected Team config env: %#v", req.Team.Environment)
	}
}

func TestBuildTeamMemberInstanceRequestMountsHermesSoul(t *testing.T) {
	service := &teamService{}
	pvcName := "clawreef-team-7-shared"
	secretName := "clawreef-team-7-bus"
	req := service.buildTeamMemberInstanceRequest(&models.Team{
		ID:                  7,
		Name:                "Delivery",
		SharedPVCName:       &pvcName,
		SharedMountPath:     "/team",
		TeamTokenSecretName: &secretName,
	}, plannedTeamMember{
		MemberKey:   "worker",
		DisplayName: "team worker",
		Role:        "senior-developer",
		RuntimeType: "hermes",
	})

	if req.Team == nil {
		t.Fatalf("expected Team instance config")
	}
	if req.Team.PersonaConfigKey != "hermes-soul-worker.md" {
		t.Fatalf("expected Hermes persona config key, got %q", req.Team.PersonaConfigKey)
	}
}

func TestBuildTeamMemberInstanceRequestSupportsLiteMode(t *testing.T) {
	service := &teamService{runtimeWorkspaceRoot: "/workspaces"}
	team := &models.Team{
		UserID:          1,
		ID:              8,
		Name:            "Lite Team",
		SharedMountPath: "/team",
	}
	memberPlan := plannedTeamMember{
		Request: CreateTeamMemberRequest{
			Mode:         "Lite",
			InstanceMode: "Lite",
		},
		MemberKey:    "lite-worker",
		DisplayName:  "lite worker",
		Role:         "developer",
		RuntimeType:  "openclaw",
		InstanceMode: InstanceModeLite,
	}
	req := service.buildTeamMemberInstanceRequest(team, memberPlan)

	if req.Mode != InstanceModeLite || req.InstanceMode != InstanceModeLite {
		t.Fatalf("expected Team member instance request to preserve lite mode, got mode=%q instance_mode=%q", req.Mode, req.InstanceMode)
	}
	if req.RuntimeType != RuntimeBackendGateway {
		t.Fatalf("expected lite Team member to target gateway runtime, got %q", req.RuntimeType)
	}

	rosterJSON := `{"teamId":"8","members":[{"memberId":"lite-worker"}]}`
	liteReq := service.buildTeamMemberInstanceRequestWithSecrets(team, memberPlan, &teamRuntimeSecrets{
		RedisURL: "redis://team-redis:6379/0",
		Token:    "team_test_token",
	}, rosterJSON)
	if liteReq.EnvironmentOverrides[teamRedisURLSecretKey] != "redis://team-redis:6379/0" ||
		liteReq.EnvironmentOverrides[teamTokenSecretKey] != "team_test_token" {
		t.Fatalf("expected Lite Team runtime secrets in gateway env overrides, got %#v", liteReq.EnvironmentOverrides)
	}
	if liteReq.EnvironmentOverrides["CLAWMANAGER_TEAM_CONFIG_JSON"] != rosterJSON {
		t.Fatalf("Lite Team roster JSON should preserve upstream logical sharedDir contract, got %#v", liteReq.EnvironmentOverrides)
	}
}

func TestBuildTeamMemberInstanceRequestPointsLiteSharedDirAtRuntimeWorkspace(t *testing.T) {
	service := &teamService{runtimeWorkspaceRoot: "/workspaces"}
	team := &models.Team{
		UserID:          1,
		ID:              28,
		Name:            "Mixed Team",
		SharedMountPath: "/team",
	}
	memberPlan := plannedTeamMember{
		MemberKey:    "backend",
		DisplayName:  "backend",
		Role:         "developer",
		RuntimeType:  "openclaw",
		InstanceMode: InstanceModeLite,
	}

	req := service.buildTeamMemberInstanceRequestWithSecrets(team, memberPlan, &teamRuntimeSecrets{
		RedisURL: "redis://team-redis:6379/0",
		Token:    "team_test_token",
	}, `{"sharedDir":"/team"}`)

	wantSharedDir := "/workspaces/teams/user-1/team-28-shared"
	if req.EnvironmentOverrides["CLAWMANAGER_TEAM_SHARED_DIR"] != wantSharedDir {
		t.Fatalf("expected Lite shared dir %q, got %#v", wantSharedDir, req.EnvironmentOverrides)
	}
	if req.EnvironmentOverrides["CLAWMANAGER_TEAM_CONFIG_JSON"] != `{"sharedDir":"/team"}` {
		t.Fatalf("Lite roster JSON should preserve logical /team sharedDir, got %s", req.EnvironmentOverrides["CLAWMANAGER_TEAM_CONFIG_JSON"])
	}
	if req.Team.SharedMountPath != "/team" {
		t.Fatalf("Pro Team mount path should remain /team, got %q", req.Team.SharedMountPath)
	}
}

func TestOpenClawConfigPlanForTeamMemberFiltersOnlyWorkers(t *testing.T) {
	originalPlan := &OpenClawConfigPlan{
		Mode:        OpenClawConfigPlanModeManual,
		ResourceIDs: []int{10, 20},
	}
	filteredPlan := &OpenClawConfigPlan{
		Mode:        OpenClawConfigPlanModeManual,
		ResourceIDs: []int{20},
	}
	planner := &teamOpenClawConfigPlannerStub{nextPlan: filteredPlan}
	service := &teamService{openClawConfigPlanner: planner}

	leaderPlan, err := service.openClawConfigPlanForTeamMember(7, plannedTeamMember{
		IsLeader: true,
		Request:  CreateTeamMemberRequest{OpenClawConfigPlan: originalPlan},
	})
	if err != nil {
		t.Fatalf("leader plan returned error: %v", err)
	}
	if leaderPlan != originalPlan {
		t.Fatalf("expected leader to keep original OpenClaw plan")
	}
	if planner.calls != 0 {
		t.Fatalf("expected leader plan to skip filtering, got %d calls", planner.calls)
	}

	workerPlan, err := service.openClawConfigPlanForTeamMember(7, plannedTeamMember{
		IsLeader: false,
		Request:  CreateTeamMemberRequest{OpenClawConfigPlan: originalPlan},
	})
	if err != nil {
		t.Fatalf("worker plan returned error: %v", err)
	}
	if workerPlan != filteredPlan {
		t.Fatalf("expected worker to use filtered OpenClaw plan")
	}
	if planner.calls != 1 || planner.userID != 7 || planner.plan != originalPlan {
		t.Fatalf("unexpected planner call: %#v", planner)
	}
}

func TestNewRedisBusParsesURLWithoutNetwork(t *testing.T) {
	bus, err := newRedisBus("redis://:pass@redis.example:6380/3")
	if err != nil {
		t.Fatalf("newRedisBus returned error: %v", err)
	}
	if bus.address != "redis.example:6380" || bus.password != "pass" || bus.db != 3 || bus.useTLS {
		t.Fatalf("unexpected redis bus config: %#v", bus)
	}
}

func TestDefaultTeamRedisURLUsesClusterServiceFallback(t *testing.T) {
	t.Setenv("CLAWMANAGER_TEAM_REDIS_URL", "")
	t.Setenv("TEAM_REDIS_URL", "")
	t.Setenv("REDIS_URL", "")
	t.Setenv("CLAWMANAGER_SYSTEM_NAMESPACE", "")
	t.Setenv("K8S_NAMESPACE", "clawmanager")
	t.Setenv("CLAWMANAGER_TEAM_REDIS_SERVICE_NAME", "")
	t.Setenv("CLAWMANAGER_TEAM_REDIS_SERVICE", "")
	t.Setenv("CLAWMANAGER_TEAM_REDIS_SERVICE_PORT", "")
	t.Setenv("CLAWMANAGER_TEAM_REDIS_PORT", "")
	t.Setenv("CLAWMANAGER_TEAM_REDIS_DB", "")
	t.Setenv("TEAM_REDIS_DB", "")

	got := defaultTeamRedisURL()
	want := "redis://clawmanager-team-redis.clawmanager-system.svc.cluster.local:6379/0"
	if got != want {
		t.Fatalf("expected default Team redis URL %q, got %q", want, got)
	}
}

func TestProjectTeamTaskRuntimeStateUsesExplicitCompletionSignals(t *testing.T) {
	now := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	resultJSON := `{"status":"done","resultMarkdown":"finished"}`
	task := &models.TeamTask{Status: models.TeamTaskStatusFailed}

	projection := projectTeamTaskRuntimeState(task, map[string]interface{}{
		"status":             "done",
		"resultMarkdown":     "finished",
		"explicitCompletion": true,
	}, "completion", &resultJSON, now)

	if !projection.changed || projection.status != models.TeamTaskStatusSucceeded {
		t.Fatalf("expected succeeded projection, got %#v", projection)
	}
	if task.Status != models.TeamTaskStatusSucceeded || task.FinishedAt == nil || task.ResultJSON == nil {
		t.Fatalf("expected task to be completed with result, got %#v", task)
	}
}

func TestProjectTeamTaskRuntimeStateDoesNotLetLateFailureOverrideSuccess(t *testing.T) {
	now := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	task := &models.TeamTask{Status: models.TeamTaskStatusSucceeded}

	projection := projectTeamTaskRuntimeState(task, map[string]interface{}{
		"status": "failed",
		"error":  "late failure",
	}, "task_failed", nil, now)

	if projection.changed || task.Status != models.TeamTaskStatusSucceeded {
		t.Fatalf("late failure must not override success, projection=%#v task=%#v", projection, task)
	}
}

func TestProjectTeamTaskRuntimeStateDoesNotTreatPlainReplyAsCompletion(t *testing.T) {
	now := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	task := &models.TeamTask{Status: models.TeamTaskStatusRunning}

	projection := projectTeamTaskRuntimeState(task, map[string]interface{}{
		"message": "worker is preparing the result and will report back soon",
	}, "reply", nil, now)

	if projection.changed || task.Status != models.TeamTaskStatusRunning || task.FinishedAt != nil {
		t.Fatalf("plain reply must not complete task, projection=%#v task=%#v", projection, task)
	}
}

func TestProjectTeamTaskRuntimeStateDoesNotDowngradeTerminalTaskToRunning(t *testing.T) {
	now := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	task := &models.TeamTask{Status: models.TeamTaskStatusSucceeded}

	projection := projectTeamTaskRuntimeState(task, map[string]interface{}{
		"progress": 30,
	}, "task_started", nil, now)

	if projection.changed || task.Status != models.TeamTaskStatusSucceeded || task.StartedAt != nil {
		t.Fatalf("running signal must not downgrade terminal task, projection=%#v task=%#v", projection, task)
	}
}

func TestPlanTeamMembersRequiresExactlyOneLeader(t *testing.T) {
	_, err := planTeamMembers("team", []CreateTeamMemberRequest{
		{MemberID: "worker", Role: "developer"},
	})
	if err == nil || !strings.Contains(err.Error(), "exactly one leader") {
		t.Fatalf("expected exactly one leader validation error, got %v", err)
	}

	plans, err := planTeamMembers("team", []CreateTeamMemberRequest{
		{MemberID: "lead", Role: "team leader"},
		{MemberID: "worker", Role: "developer"},
	})
	if err != nil {
		t.Fatalf("planTeamMembers returned error: %v", err)
	}
	if len(plans) != 2 || !plans[0].IsLeader || plans[0].Role != "leader" {
		t.Fatalf("expected first member to be normalized as leader, got %#v", plans)
	}
	if plans[1].RuntimeType != "openclaw" {
		t.Fatalf("expected default runtime type openclaw, got %#v", plans[1])
	}
}

func TestPlanTeamMembersSupportsHermesRuntime(t *testing.T) {
	plans, err := planTeamMembers("team", []CreateTeamMemberRequest{
		{MemberID: "lead", Role: "leader"},
		{MemberID: "hermes-writer", Role: "writer", RuntimeType: "Hermes", InstanceMode: "Pro"},
	})
	if err != nil {
		t.Fatalf("planTeamMembers returned error: %v", err)
	}
	if plans[1].RuntimeType != "hermes" {
		t.Fatalf("expected Hermes runtime to be normalized, got %#v", plans[1])
	}
	if plans[1].InstanceMode != InstanceModePro {
		t.Fatalf("expected Pro instance mode to be normalized, got %#v", plans[1])
	}

	_, err = planTeamMembers("team", []CreateTeamMemberRequest{
		{MemberID: "lead", Role: "leader"},
		{MemberID: "worker", Role: "developer", RuntimeType: "ubuntu"},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported team member runtime type") {
		t.Fatalf("expected unsupported runtime validation error, got %v", err)
	}

	_, err = planTeamMembers("team", []CreateTeamMemberRequest{
		{MemberID: "lead", Role: "leader"},
		{MemberID: "worker", Role: "developer", Mode: "mini"},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported team member instance mode") {
		t.Fatalf("expected unsupported instance mode validation error, got %v", err)
	}
}

func TestTeamMemberInstanceNameUsesTeamIDAndMemberKey(t *testing.T) {
	name := teamMemberInstanceName("Software Engineering Team", 42, "code-reviewer")
	if name != "software-engineering-team-42-code-reviewer" {
		t.Fatalf("unexpected Team member instance name: %q", name)
	}

	longName := teamMemberInstanceName("very-long-software-engineering-platform-team", 12345, "extremely-long-code-reviewer-member-key")
	if len(longName) > 50 {
		t.Fatalf("expected instance name to stay within 50 chars, got %d: %q", len(longName), longName)
	}
	if !strings.Contains(longName, "-12345-") {
		t.Fatalf("expected instance name to include Team ID, got %q", longName)
	}
}

func TestBuildTeamRosterConfigOmitsSecrets(t *testing.T) {
	description := "reviews implementation and validates results"
	plans, err := planTeamMembers("team", []CreateTeamMemberRequest{
		{MemberID: "leader", Role: "leader"},
		{MemberID: "worker", Role: "developer", Description: &description},
	})
	if err != nil {
		t.Fatalf("planTeamMembers returned error: %v", err)
	}
	roster, err := buildTeamRosterConfig(&models.Team{
		ID:                9,
		CommunicationMode: "leader_mediated",
		SharedMountPath:   "/team",
	}, plans)
	if err != nil {
		t.Fatalf("buildTeamRosterConfig returned error: %v", err)
	}
	for _, forbidden := range []string{"REDIS_URL", "TOKEN", "OPENAI_API_KEY", "secret"} {
		if strings.Contains(roster, forbidden) {
			t.Fatalf("roster must not contain sensitive value marker %q: %s", forbidden, roster)
		}
	}
	if !strings.Contains(roster, `"leaderMemberId":"leader"`) || !strings.Contains(roster, `"eventsKey":"claw:team:9:events"`) {
		t.Fatalf("roster missing expected leader or redis keys: %s", roster)
	}
	if !strings.Contains(roster, description) {
		t.Fatalf("roster missing member description: %s", roster)
	}
	if !strings.Contains(roster, `"runtimeType":"openclaw"`) {
		t.Fatalf("roster missing member runtime type: %s", roster)
	}
	if !strings.Contains(roster, `"instanceMode":"lite"`) {
		t.Fatalf("roster missing member instance mode: %s", roster)
	}
	if !strings.Contains(roster, `"communicationMode":"leader_mediated"`) || !strings.Contains(roster, `"allowPeerToPeer":false`) {
		t.Fatalf("roster missing leader-mediated collaboration policy: %s", roster)
	}
}

func TestPlanTeamMembersUsesProfileEffectiveRole(t *testing.T) {
	profileEnv := map[string]string{
		"CLAWMANAGER_AGENT_PERSONA_JSON": `{"profileKey":"agency.senior-developer","name":"Senior Developer","displayName":"Senior Developer","roleHint":"senior-developer","summary":"Implements scoped engineering tasks.","systemPrompt":"You are a senior implementation specialist."}`,
	}
	plans, err := planTeamMembers("team", []CreateTeamMemberRequest{
		{MemberID: "leader", Role: "leader"},
		{MemberID: "worker", Role: "developer", EnvironmentOverrides: profileEnv},
	})
	if err != nil {
		t.Fatalf("planTeamMembers returned error: %v", err)
	}
	worker := plans[1]
	if worker.Role != "senior-developer" || worker.EffectiveRole != "senior-developer" {
		t.Fatalf("expected profile effective role to override generic role, got role=%q effective=%q", worker.Role, worker.EffectiveRole)
	}
	if worker.ProfileKey != "agency.senior-developer" || worker.ProfileName != "Senior Developer" {
		t.Fatalf("expected profile metadata, got key=%q name=%q", worker.ProfileKey, worker.ProfileName)
	}

	roster, err := buildTeamRosterConfig(&models.Team{
		ID:                19,
		CommunicationMode: teamCommunicationModeLeaderMediated,
		SharedMountPath:   "/team",
	}, plans)
	if err != nil {
		t.Fatalf("buildTeamRosterConfig returned error: %v", err)
	}
	for _, expected := range []string{
		`"role":"senior-developer"`,
		`"effectiveRole":"senior-developer"`,
		`"profileKey":"agency.senior-developer"`,
		`"profileName":"Senior Developer"`,
	} {
		if !strings.Contains(roster, expected) {
			t.Fatalf("roster missing %q: %s", expected, roster)
		}
	}
}

func TestTeamMemberEnvIncludesPeerAssistedPolicy(t *testing.T) {
	description := "Developer: implements scoped tasks."
	plan, err := planTeamMembers("team", []CreateTeamMemberRequest{
		{MemberID: "leader", Role: "leader"},
		{MemberID: "worker", Role: "developer", Description: &description},
	})
	if err != nil {
		t.Fatalf("planTeamMembers returned error: %v", err)
	}
	env := (&teamService{}).teamMemberEnv(&models.Team{
		ID:                12,
		CommunicationMode: teamCommunicationModePeerAssisted,
		SharedMountPath:   "/team",
	}, plan[1])
	if env["CLAWMANAGER_TEAM_COMMUNICATION_MODE"] != teamCommunicationModePeerAssisted {
		t.Fatalf("expected peer_assisted env, got %#v", env)
	}
	if !strings.Contains(env["CLAWMANAGER_TEAM_COLLABORATION_POLICY_JSON"], `"allowPeerToPeer":true`) {
		t.Fatalf("expected peer-to-peer policy env, got %#v", env["CLAWMANAGER_TEAM_COLLABORATION_POLICY_JSON"])
	}
	if !strings.Contains(env["CLAWMANAGER_TEAM_SYSTEM_PROMPT"], "Collaboration mode: peer_assisted") {
		t.Fatalf("expected peer-assisted guidance in system prompt: %s", env["CLAWMANAGER_TEAM_SYSTEM_PROMPT"])
	}
	if !strings.Contains(env["CLAWMANAGER_TEAM_SYSTEM_PROMPT"], "Direct handoff is mandatory") {
		t.Fatalf("expected mandatory direct handoff guidance in system prompt: %s", env["CLAWMANAGER_TEAM_SYSTEM_PROMPT"])
	}
	if env["GATEWAY_ALLOW_ALL_USERS"] != "true" {
		t.Fatalf("expected Hermes teammate messages to be allowed, got %#v", env["GATEWAY_ALLOW_ALL_USERS"])
	}
}

func TestAppendTeamTaskCompletionInstructionSeparatesCollaborationModes(t *testing.T) {
	leaderMediated := appendTeamTaskCompletionInstruction("Do the work.", teamCommunicationModeLeaderMediated, "")
	if !strings.Contains(leaderMediated, "Leader-mediated mode") || strings.Contains(leaderMediated, "Worker-direct mode") {
		t.Fatalf("leader-mediated completion contract mixed modes: %s", leaderMediated)
	}
	for _, expected := range []string{
		"strict hub-and-spoke workflow",
		"answer self-contained control-plane or simple tasks directly",
		"wait for the assigned workers' actual results",
		"Do not hand off directly to another Worker",
		"Only the Leader may finalize the root task",
	} {
		if !strings.Contains(leaderMediated, expected) {
			t.Fatalf("leader-mediated completion contract missing %q: %s", expected, leaderMediated)
		}
	}

	peerAssisted := appendTeamTaskCompletionInstruction("Do the work.", teamCommunicationModePeerAssisted, "")
	for _, expected := range []string{
		"Worker-direct mode",
		"MUST hand off to that exact member",
		"required, not optional",
		"fallback only",
	} {
		if !strings.Contains(peerAssisted, expected) {
			t.Fatalf("peer-assisted completion contract missing %q: %s", expected, peerAssisted)
		}
	}
	if strings.Contains(peerAssisted, "Leader-mediated mode") {
		t.Fatalf("peer-assisted completion contract should not include leader-mediated flow: %s", peerAssisted)
	}
}

func TestTeamMemberEnvKeepsLeaderMediatedFlowIsolated(t *testing.T) {
	description := "Developer: implements assigned work and reports to the Leader."
	plans, err := planTeamMembers("team", []CreateTeamMemberRequest{
		{MemberID: "leader", Role: "leader"},
		{MemberID: "worker", Role: "developer", Description: &description},
	})
	if err != nil {
		t.Fatalf("planTeamMembers returned error: %v", err)
	}
	team := &models.Team{
		ID:                14,
		CommunicationMode: teamCommunicationModeLeaderMediated,
		SharedMountPath:   "/team",
	}
	for _, plan := range plans {
		env := (&teamService{}).teamMemberEnv(team, plan)
		if env["CLAWMANAGER_TEAM_COMMUNICATION_MODE"] != teamCommunicationModeLeaderMediated {
			t.Fatalf("expected leader-mediated env for %s: %#v", plan.MemberKey, env)
		}
		if !strings.Contains(env["CLAWMANAGER_TEAM_COLLABORATION_POLICY_JSON"], `"allowPeerToPeer":false`) {
			t.Fatalf("leader-mediated policy allowed peer-to-peer for %s: %s", plan.MemberKey, env["CLAWMANAGER_TEAM_COLLABORATION_POLICY_JSON"])
		}
		guidance := env["CLAWMANAGER_TEAM_SYSTEM_PROMPT"]
		for _, expected := range []string{"strict hub-and-spoke workflow", "Workers must not hand off directly to other workers", "only the Leader may finalize"} {
			if !strings.Contains(guidance, expected) {
				t.Fatalf("leader-mediated guidance for %s missing %q: %s", plan.MemberKey, expected, guidance)
			}
		}
		if strings.Contains(guidance, "Direct handoff is mandatory") {
			t.Fatalf("leader-mediated guidance leaked worker-direct rules for %s: %s", plan.MemberKey, guidance)
		}
	}
}

func TestAppendTeamTaskCompletionInstructionUsesLeaderOnlyBootstrapContract(t *testing.T) {
	bootstrap := appendTeamTaskCompletionInstruction(
		"Introduce the current Team.",
		teamCommunicationModeLeaderMediated,
		initialLeaderTaskIntent,
	)
	for _, expected := range []string{
		"Bootstrap completion contract",
		"assigned only to the Leader",
		"Do not delegate it",
		"Complete this bootstrap in the current turn",
		"metadata.teamConfigJson",
		"/team/team.json from the shared workspace",
		"optional system fallbacks",
	} {
		if !strings.Contains(bootstrap, expected) {
			t.Fatalf("bootstrap contract missing %q: %s", expected, bootstrap)
		}
	}
	for _, forbidden := range []string{"For multi-member Teams", "workers report deliverables back to the Leader", "Never look for /team/team.json"} {
		if strings.Contains(bootstrap, forbidden) {
			t.Fatalf("bootstrap contract must not include normal collaboration rule %q: %s", forbidden, bootstrap)
		}
	}
}

func TestBuildTeamMemberSoulMarkdownIncludesProfileGuidance(t *testing.T) {
	description := "Senior Developer: owns implementation and verification."
	member := plannedTeamMember{
		MemberKey:   "worker",
		DisplayName: "team-worker",
		Role:        "senior-developer",
		RuntimeType: "hermes",
		Request: CreateTeamMemberRequest{
			Description: &description,
		},
	}

	soul := buildTeamMemberSoulMarkdown(member, teamCommunicationModePeerAssisted)
	for _, expected := range []string{
		"# team-worker",
		"Member ID: worker",
		"Role: senior-developer",
		description,
		"Collaboration mode: peer_assisted",
		"If asked about your role",
		"team/... is invalid",
		"Report shared artifact links as /team/<relative-path>",
	} {
		if !strings.Contains(soul, expected) {
			t.Fatalf("SOUL.md missing %q: %s", expected, soul)
		}
	}
}

func TestWriteLiteTeamMemberIdentityFiles(t *testing.T) {
	workspace := t.TempDir()
	profileEnv := map[string]string{
		"CLAWMANAGER_AGENT_PERSONA_JSON": `{"profileKey":"agency.senior-developer","name":"Senior Developer","roleHint":"senior-developer","summary":"Implements scoped engineering tasks.","systemPrompt":"You are a senior implementation specialist."}`,
	}
	plans, err := planTeamMembers("team", []CreateTeamMemberRequest{
		{MemberID: "leader", Role: "leader"},
		{MemberID: "worker", Role: "developer", RuntimeType: "hermes", Mode: InstanceModeLite, InstanceMode: InstanceModeLite, EnvironmentOverrides: profileEnv},
	})
	if err != nil {
		t.Fatalf("planTeamMembers returned error: %v", err)
	}
	member := plans[1]
	team := &models.Team{
		UserID:            1,
		ID:                31,
		CommunicationMode: teamCommunicationModeLeaderMediated,
		SharedMountPath:   "/team",
	}
	roster, err := buildTeamRosterConfig(team, plans)
	if err != nil {
		t.Fatalf("buildTeamRosterConfig returned error: %v", err)
	}
	instance := &models.Instance{
		Type:          "hermes",
		RuntimeType:   RuntimeBackendGateway,
		InstanceMode:  InstanceModeLite,
		WorkspacePath: &workspace,
	}

	if err := (&teamService{}).writeLiteTeamMemberIdentityFiles(instance, team, member, roster); err != nil {
		t.Fatalf("writeLiteTeamMemberIdentityFiles returned error: %v", err)
	}
	for _, name := range []string{teamAgentsFileName, teamSoulFileName, teamConfigFileName, filepath.Join(".hermes", teamSoulFileName)} {
		if _, err := os.Stat(filepath.Join(workspace, name)); err != nil {
			t.Fatalf("expected Lite identity file %s: %v", name, err)
		}
	}
	soulBytes, err := os.ReadFile(filepath.Join(workspace, teamSoulFileName))
	if err != nil {
		t.Fatalf("failed to read SOUL.md: %v", err)
	}
	soul := string(soulBytes)
	for _, expected := range []string{
		"Effective role: senior-developer",
		"Profile key: agency.senior-developer",
		"Profile name: Senior Developer",
		"If team.json contains effectiveRole/profileName",
	} {
		if !strings.Contains(soul, expected) {
			t.Fatalf("SOUL.md missing %q: %s", expected, soul)
		}
	}
	agentsBytes, err := os.ReadFile(filepath.Join(workspace, teamAgentsFileName))
	if err != nil {
		t.Fatalf("failed to read AGENTS.md: %v", err)
	}
	if !strings.Contains(string(agentsBytes), "SOUL.md as the member-specific identity") {
		t.Fatalf("AGENTS.md missing identity source guidance: %s", string(agentsBytes))
	}
	rosterBytes, err := os.ReadFile(filepath.Join(workspace, teamConfigFileName))
	if err != nil {
		t.Fatalf("failed to read team.json: %v", err)
	}
	if !strings.Contains(string(rosterBytes), `"effectiveRole":"senior-developer"`) {
		t.Fatalf("team.json missing effective role: %s", string(rosterBytes))
	}
}

func TestBuildInitialLeaderTaskPayloadDescribesRosterAndTeamSend(t *testing.T) {
	payload := buildInitialLeaderTaskPayload("Software Engineering Team")

	if payload["intent"] != initialLeaderTaskIntent {
		t.Fatalf("unexpected bootstrap intent: %#v", payload)
	}
	if payload["title"] == "" {
		t.Fatalf("expected bootstrap task title: %#v", payload)
	}
	if payload["executionMode"] != "leader_control_plane_snapshot" || payload["requiresDelegation"] != false {
		t.Fatalf("expected leader-only bootstrap execution metadata: %#v", payload)
	}
	if payload["anchorEligible"] != false {
		t.Fatalf("bootstrap task must not become a user question anchor: %#v", payload)
	}
	prompt, ok := payload["prompt"].(string)
	if !ok {
		t.Fatalf("expected prompt string: %#v", payload)
	}
	for _, expected := range []string{
		"team Software Engineering Team",
		"Redis Team\u6210\u5458\u6784\u6210",
		"\u8fd0\u884c\u72b6\u6001\u4e0e\u6280\u672f\u80fd\u529b\u8fb9\u754c",
		"\u534f\u4f5c\u4e0e\u901a\u4fe1\u673a\u5236(team_send)",
		"\u4efb\u52a1\u6d41\u8f6c\u65b9\u5f0f",
		"\u6d88\u606f\u540c\u6b65\u65b9\u5f0f",
		"\u4e0a\u4e0b\u6587\u5171\u4eab\u65b9\u5f0f",
		"\u53ef\u8c03\u7528\u7684\u65b9\u6cd5\u3001\u5de5\u5177\u4e0e\u64cd\u4f5c\u80fd\u529b",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("bootstrap prompt missing %q: %s", expected, prompt)
		}
	}
}

func TestBuildTeamTaskEnvelopeIncludesCompletionContract(t *testing.T) {
	task := &models.TeamTask{ID: 67}
	payload := map[string]interface{}{
		"intent":         "team_bootstrap_introduction",
		"title":          "Introduce the team",
		"prompt":         "Generate the team report.",
		"teamConfigJson": `{"sharedDir":"/team"}`,
	}

	envelope := buildTeamTaskEnvelope(31, "leader", task, "team-31-bootstrap-introduction", payload, nil, time.Unix(123, 0).UTC())

	if envelope["replyTo"] != "clawmanager" {
		t.Fatalf("expected replyTo clawmanager, got %#v", envelope["replyTo"])
	}
	if envelope["requiresCompletion"] != true {
		t.Fatalf("expected requiresCompletion=true, got %#v", envelope["requiresCompletion"])
	}
	if envelope["completionTool"] != "team_complete_task" {
		t.Fatalf("expected completion tool team_complete_task, got %#v", envelope["completionTool"])
	}
	monitorPolicy, ok := envelope["monitorPolicy"].(map[string]interface{})
	if !ok || monitorPolicy["enabled"] != true || monitorPolicy["visibleToChat"] != true {
		t.Fatalf("expected visible monitor policy in envelope, got %#v", envelope["monitorPolicy"])
	}
	if monitorPolicy["heartbeatEverySec"] != 30 || monitorPolicy["visibleHeartbeatEverySec"] != 180 {
		t.Fatalf("expected 30s internal heartbeat and 180s chat digest policy, got %#v", monitorPolicy)
	}
	resultSink, ok := envelope["resultSink"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected resultSink map, got %#v", envelope["resultSink"])
	}
	if resultSink["type"] != "redis_stream" || resultSink["eventsKey"] != "claw:team:31:events" {
		t.Fatalf("unexpected resultSink: %#v", resultSink)
	}
	if resultSink["successEvent"] != "completion_proposed" || resultSink["failureEvent"] != "task_failed" {
		t.Fatalf("unexpected resultSink events: %#v", resultSink)
	}
	if envelope["teamConfigJson"] != `{"sharedDir":"/team"}` {
		t.Fatalf("expected teamConfigJson in envelope, got %#v", envelope["teamConfigJson"])
	}
	prompt, ok := envelope["prompt"].(string)
	if !ok {
		t.Fatalf("expected prompt string, got %#v", envelope["prompt"])
	}
	for _, expected := range []string{"Generate the team report.", "team_complete_task", "resultMarkdown", "task_completed"} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("completion prompt missing %q: %s", expected, prompt)
		}
	}
}

func TestBuildTeamTaskEnvelopeCarriesLocaleAndMemberWorkspace(t *testing.T) {
	task := &models.TeamTask{ID: 88}
	payload := map[string]interface{}{
		"prompt":         "请实现一个计算器应用",
		"responseLocale": "zh-CN",
		"workspaceContract": map[string]interface{}{
			"physicalSharedDir": "/workspaces/teams/user-1/team-54-shared",
			"taskRef":           "team-54-task-88",
		},
	}
	envelope := buildTeamTaskEnvelope(54, "ui-designer", task, "root-message", payload, map[string]string{}, time.Unix(123, 0).UTC())
	if envelope["responseLocale"] != "zh-CN" {
		t.Fatalf("response locale was not propagated: %#v", envelope)
	}
	shared, ok := envelope["sharedWorkspace"].(map[string]interface{})
	if !ok || shared["physicalPath"] != "/workspaces/teams/user-1/team-54-shared" || shared["memberArtifactPhysicalRoot"] != "/workspaces/teams/user-1/team-54-shared/artifacts/team-54-task-88/members/ui-designer" {
		t.Fatalf("member shared workspace was not resolved: %#v", envelope["sharedWorkspace"])
	}
	prompt, _ := envelope["prompt"].(string)
	if !strings.Contains(prompt, "use zh-CN") || !strings.Contains(prompt, "team_artifact_write") {
		t.Fatalf("runtime prompt is missing locale/artifact guidance: %s", prompt)
	}
}

func TestCompleteInitialLeaderTaskFromSnapshotWritesReportAndCompletion(t *testing.T) {
	workspaceRoot := t.TempDir()
	team := &models.Team{
		ID:                49,
		UserID:            1,
		Name:              "delivery-team",
		CommunicationMode: teamCommunicationModeLeaderMediated,
		SharedMountPath:   "/team",
	}
	task := &models.TeamTask{
		ID:             91,
		TeamID:         team.ID,
		TargetMemberID: 700,
		MessageID:      "team-49-bootstrap-introduction",
		Status:         models.TeamTaskStatusPending,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	leader := &models.TeamMember{ID: 700, TeamID: team.ID, MemberKey: "delivery-lead", DisplayName: "Delivery Lead", Role: "leader", Status: models.TeamMemberStatusBusy, Availability: models.TeamMemberAvailabilityBusy}
	developer := &models.TeamMember{ID: 701, TeamID: team.ID, MemberKey: "developer", DisplayName: "Developer", Role: "developer", RuntimeType: "openclaw", InstanceMode: "lite", Status: models.TeamMemberStatusIdle, Availability: models.TeamMemberAvailabilityIdle}
	repo := &teamRepositoryStub{
		membersByKey: map[string]*models.TeamMember{
			"delivery-lead": leader,
			"developer":     developer,
		},
	}
	service := &teamService{repo: repo, runtimeWorkspaceRoot: workspaceRoot}
	payload := map[string]interface{}{
		"intent": initialLeaderTaskIntent,
		"workspaceContract": map[string]interface{}{
			"sharedDir":         "/team",
			"physicalSharedDir": filepath.Join(workspaceRoot, "teams", "user-1", "team-49-shared"),
		},
	}

	result, err := service.completeInitialLeaderTaskFromSnapshot(1, team, task, leader, payload)
	if err != nil {
		t.Fatalf("completeInitialLeaderTaskFromSnapshot returned error: %v", err)
	}
	if result.Status != models.TeamTaskStatusSucceeded || result.FinishedAt == nil {
		t.Fatalf("expected backend bootstrap task to finish, got %#v", result.TeamTask)
	}
	reportPath := filepath.Join(workspaceRoot, "teams", "user-1", "team-49-shared", "results", "team-49-task-91", "team-introduction.md")
	reportBytes, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("expected bootstrap report to be written: %v", err)
	}
	report := string(reportBytes)
	if !strings.Contains(report, "Developer") || !strings.Contains(report, "Redis Streams") {
		t.Fatalf("bootstrap report missing member or mechanism detail: %s", report)
	}
	if repo.updatedTask == nil || repo.updatedTask.Status != models.TeamTaskStatusSucceeded || repo.updatedTask.ResultJSON == nil {
		t.Fatalf("expected task result to be persisted, got %#v", repo.updatedTask)
	}
	if repo.updatedMember == nil || repo.updatedMember.MemberKey != "delivery-lead" || repo.updatedMember.Status != models.TeamMemberStatusIdle {
		t.Fatalf("expected leader member to become idle, got %#v", repo.updatedMember)
	}
	if len(repo.createdEvents) != 1 || repo.createdEvents[0].EventType != "task_completed" {
		t.Fatalf("expected one backend task_completed event, got %#v", repo.createdEvents)
	}
	stored := teamEventPayloadMap(repo.createdEvents[0])
	if eventString(stored, "completionSource") != "clawmanager_backend" || eventBool(stored, "backendGenerated") != true {
		t.Fatalf("expected backend completion markers, got %#v", stored)
	}
}

func TestProjectTeamEventDoesNotTreatPlainFinalReplyAsTaskCompleted(t *testing.T) {
	taskID := 67
	messageID := "team-31-bootstrap-introduction"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	member := &models.TeamMember{
		ID:            120,
		TeamID:        31,
		MemberKey:     "leader",
		Status:        models.TeamMemberStatusBusy,
		CurrentTaskID: &taskID,
		Availability:  models.TeamMemberAvailabilityBusy,
	}
	repo := &teamRepositoryStub{
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": member},
	}
	service := &teamService{repo: repo}
	payload := map[string]interface{}{
		"event":          "reply",
		"messageId":      messageID,
		"memberId":       "leader",
		"taskId":         "team-31-task-67",
		"final":          true,
		"summary":        "Team report ready",
		"resultMarkdown": "Full report",
		"text":           "Full report",
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	err = service.projectTeamEvent(&models.Team{ID: 31}, nil, redisStreamMessage{
		ID:     "1781171178655-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	})
	if err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}

	if repo.updatedTask != nil {
		t.Fatalf("plain final reply must not mark task succeeded, got %#v", repo.updatedTask)
	}
	if repo.updatedMember == nil || repo.updatedMember.Status == models.TeamMemberStatusIdle && repo.updatedMember.Progress == 100 {
		t.Fatalf("plain final reply must not mark member completed, got %#v", repo.updatedMember)
	}
	if len(repo.createdEvents) != 1 || repo.createdEvents[0].EventType != "reply" {
		t.Fatalf("expected stored event type reply, got %#v", repo.createdEvents)
	}
}

func TestProjectTeamEventKeepsSubstantialDirectTargetReplyNonTerminal(t *testing.T) {
	taskID := 68
	messageID := "team-31-bootstrap-introduction"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusDispatched,
		UpdatedAt:      time.Now().UTC(),
	}
	member := &models.TeamMember{
		ID:            120,
		TeamID:        31,
		MemberKey:     "leader",
		Status:        models.TeamMemberStatusBusy,
		CurrentTaskID: &taskID,
		Availability:  models.TeamMemberAvailabilityBusy,
	}
	repo := &teamRepositoryStub{
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": member},
	}
	service := &teamService{repo: repo}
	payload := map[string]interface{}{
		"event":     "reply",
		"messageId": messageID,
		"memberId":  "leader",
		"taskId":    "team-31-task-68",
		"summary":   "Team introduction ready",
		"text": strings.Join([]string{
			"# Team report",
			"The team has two members. Leader coordinates planning, handoff, verification, and final synthesis.",
			"Worker handles scoped implementation tasks, reports concrete outputs, and keeps changes practical.",
		}, "\n"),
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	err = service.projectTeamEvent(&models.Team{ID: 31}, nil, redisStreamMessage{
		ID:     "1781171178656-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	})
	if err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}

	if repo.updatedTask != nil && (repo.updatedTask.Status == models.TeamTaskStatusSucceeded || repo.updatedTask.FinishedAt != nil) {
		t.Fatalf("substantial reply without an explicit completion tool must stay non-terminal, got %#v", repo.updatedTask)
	}
	if repo.updatedMember != nil && repo.updatedMember.Progress == 100 {
		t.Fatalf("substantial reply without explicit completion must not complete the member, got %#v", repo.updatedMember)
	}
	if len(repo.createdEvents) != 1 || repo.createdEvents[0].EventType != "reply" {
		t.Fatalf("expected stored event type reply, got %#v", repo.createdEvents)
	}
	var stored map[string]interface{}
	if err := json.Unmarshal([]byte(*repo.createdEvents[0].PayloadJSON), &stored); err != nil {
		t.Fatalf("decode stored event payload: %v", err)
	}
	step, ok := stored["collaborationStep"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected collaboration step in stored event, got %#v", stored)
	}
	if got := step["summary"]; got != "Team introduction ready" {
		t.Fatalf("expected collaboration step summary to stay compact, got %#v", got)
	}
	if got, _ := step["content"].(string); !strings.Contains(got, "# Team report") {
		t.Fatalf("expected collaboration step content to preserve full reply text, got %#v", got)
	}
}

func TestProjectTeamEventDoesNotTreatDelegationReplyAsTaskCompleted(t *testing.T) {
	taskID := 69
	messageID := "team-31-task-69"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusDispatched,
		UpdatedAt:      time.Now().UTC(),
	}
	member := &models.TeamMember{
		ID:            120,
		TeamID:        31,
		MemberKey:     "leader",
		Status:        models.TeamMemberStatusBusy,
		CurrentTaskID: &taskID,
		Availability:  models.TeamMemberAvailabilityBusy,
	}
	repo := &teamRepositoryStub{
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": member},
	}
	service := &teamService{repo: repo}
	payload := map[string]interface{}{
		"event":     "reply",
		"messageId": messageID,
		"memberId":  "leader",
		"taskId":    "team-31-task-69",
		"final":     true,
		"text":      "Assigned to worker and waiting for worker to finish the report.",
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	err = service.projectTeamEvent(&models.Team{ID: 31}, nil, redisStreamMessage{
		ID:     "1781171178657-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	})
	if err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}

	if repo.updatedTask == nil || repo.updatedTask.Status == models.TeamTaskStatusSucceeded || repo.updatedTask.FinishedAt != nil {
		t.Fatalf("delegation reply should touch but not complete task, got %#v", repo.updatedTask)
	}
	if len(repo.createdEvents) != 1 || repo.createdEvents[0].EventType != "reply" {
		t.Fatalf("expected stored event type reply, got %#v", repo.createdEvents)
	}
}

func TestProjectTeamEventDoesNotTreatProcessOnlyTaskCompletedAsRootCompletion(t *testing.T) {
	taskID := 70
	messageID := "team-31-bootstrap-introduction"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	member := &models.TeamMember{
		ID:            120,
		TeamID:        31,
		MemberKey:     "leader",
		Status:        models.TeamMemberStatusBusy,
		CurrentTaskID: &taskID,
		Availability:  models.TeamMemberAvailabilityBusy,
	}
	repo := &teamRepositoryStub{
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": member},
	}
	service := &teamService{repo: repo}
	payload := map[string]interface{}{
		"event":     "task_completed",
		"messageId": messageID,
		"memberId":  "leader",
		"taskId":    "team-31-task-70",
		"status":    "succeeded",
		"collaborationStep": map[string]interface{}{
			"type":    "progress",
			"summary": "Good, I have the team configuration.",
			"content": "Good, I have the team configuration. Now let me write the comprehensive report to the shared workspace and then finalize.",
		},
		"toolCall": map[string]interface{}{
			"name": teamTaskCompletionTool,
		},
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	err = service.projectTeamEvent(&models.Team{ID: 31}, nil, redisStreamMessage{
		ID:     "1781171178661-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	})
	if err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}

	if repo.updatedTask == nil || repo.updatedTask.Status == models.TeamTaskStatusSucceeded || repo.updatedTask.FinishedAt != nil {
		t.Fatalf("process-only completion wrapper must not complete task, got %#v", repo.updatedTask)
	}
	if len(repo.createdEvents) != 1 || repo.createdEvents[0].EventType != "reply" {
		t.Fatalf("expected process-only completion wrapper to be stored as reply, got %#v", repo.createdEvents)
	}
	var stored map[string]interface{}
	if err := json.Unmarshal([]byte(*repo.createdEvents[0].PayloadJSON), &stored); err != nil {
		t.Fatalf("decode stored event payload: %v", err)
	}
	if stored["nonAuthoritativeCompletion"] != true {
		t.Fatalf("expected nonAuthoritativeCompletion marker, got %#v", stored)
	}
}

func TestProjectTeamEventLeaderDispatchCompletionDoesNotCloseRootTask(t *testing.T) {
	taskID := 169
	messageID := "team-31-task-169"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC().Add(-10 * time.Minute),
	}
	member := &models.TeamMember{
		ID:            120,
		TeamID:        31,
		MemberKey:     "leader",
		Role:          "leader",
		Status:        models.TeamMemberStatusBusy,
		CurrentTaskID: &taskID,
		Availability:  models.TeamMemberAvailabilityBusy,
	}
	repo := &teamRepositoryStub{
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": member},
	}
	service := &teamService{repo: repo}
	payload := map[string]interface{}{
		"event":     "task_completed",
		"messageId": messageID,
		"memberId":  "leader",
		"taskId":    "team-31-task-169",
		"status":    "succeeded",
		"summary":   "Dispatched to designer.",
		"resultMarkdown": strings.Join([]string{
			"[ASSIGNMENT] Self introduction",
			"",
			"designer, the user wants you to write a short self introduction.",
			"",
			"Please include your role identity, capability boundary, and working style.",
			"After finishing, write the content into the shared workspace and report back to me.",
			"",
			"Shared directory: $CLAWMANAGER_TEAM_SHARED_DIR/results/team-31-task-169/",
			"Canonical path: /team/results/team-31-task-169/intro-designer.md",
		}, "\n"),
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	if err := service.projectTeamEvent(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID:     "1781171178669-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}

	if repo.updatedTask == nil {
		t.Fatalf("expected root task to be touched so stale detection sees active delegation")
	}
	if repo.updatedTask.Status == models.TeamTaskStatusSucceeded || repo.updatedTask.FinishedAt != nil || repo.updatedTask.ResultJSON != nil {
		t.Fatalf("leader dispatch must not complete root task, got %#v", repo.updatedTask)
	}
	if repo.updatedMember == nil || repo.updatedMember.Status != models.TeamMemberStatusBusy || repo.updatedMember.Progress == 100 {
		t.Fatalf("leader dispatch must keep leader/root task active, got %#v", repo.updatedMember)
	}
	if len(repo.createdEvents) != 1 || repo.createdEvents[0].EventType != "reply" {
		t.Fatalf("expected dispatch stored as reply, got %#v", repo.createdEvents)
	}
	var stored map[string]interface{}
	if err := json.Unmarshal([]byte(*repo.createdEvents[0].PayloadJSON), &stored); err != nil {
		t.Fatalf("decode stored payload: %v", err)
	}
	if stored["leaderDispatchOnly"] != true || stored["rootTaskTerminal"] != false {
		t.Fatalf("expected leader dispatch marker without root terminal, got %#v", stored)
	}
	step := stored["collaborationStep"].(map[string]interface{})
	if step["type"] != "assignment" || step["status"] != models.TeamTaskStatusDispatched {
		t.Fatalf("expected assignment collaboration step, got %#v", step)
	}
	if got, _ := step["content"].(string); !strings.Contains(got, "designer, the user wants you to write a short self introduction") {
		t.Fatalf("expected assignment content preserved, got %#v", got)
	}
}

func TestProjectTeamEventLeaderPlanningDoesNotCreateLeaderAssignmentLane(t *testing.T) {
	taskID := 169
	messageID := "team-31-task-169"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	leader := &models.TeamMember{
		ID:            120,
		TeamID:        31,
		MemberKey:     "leader",
		Role:          "leader",
		Status:        models.TeamMemberStatusBusy,
		CurrentTaskID: &taskID,
		Availability:  models.TeamMemberAvailabilityBusy,
	}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": leader},
	}
	service := &teamService{repo: repo}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":              "reply",
		"memberId":           "leader",
		"messageId":          messageID,
		"rootTaskId":         messageID,
		"status":             "dispatched",
		"summary":            "Planning: decomposing the task into assignments",
		"leaderDispatchOnly": true,
		"collaborationStep": map[string]interface{}{
			"type":       "assignment",
			"status":     "dispatched",
			"actor":      "leader",
			"target":     "leader",
			"rootTaskId": messageID,
			"content":    "Planning: decomposing the task into assignments",
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := service.projectTeamEvent(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID:     "1781171178657-1",
		Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}
	if len(repo.workItems) != 0 {
		t.Fatalf("leader planning/dispatch-to-self must not create Kanban work items, got %#v", repo.workItems)
	}
}

func TestProjectTeamEventDoesNotTreatNonTargetReplyAsTaskCompleted(t *testing.T) {
	taskID := 70
	messageID := "team-31-task-70"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusDispatched,
		UpdatedAt:      time.Now().UTC(),
	}
	worker := &models.TeamMember{
		ID:            121,
		TeamID:        31,
		MemberKey:     "worker",
		Status:        models.TeamMemberStatusBusy,
		CurrentTaskID: &taskID,
		Availability:  models.TeamMemberAvailabilityBusy,
	}
	repo := &teamRepositoryStub{
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"worker": worker},
	}
	service := &teamService{repo: repo}
	payload := map[string]interface{}{
		"event":     "reply",
		"messageId": messageID,
		"memberId":  "worker",
		"taskId":    "team-31-task-70",
		"summary":   "Worker report ready",
		"text":      "# Worker report\nThis is a detailed result, but it belongs to a member that is not the target of the parent task.",
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	err = service.projectTeamEvent(&models.Team{ID: 31}, nil, redisStreamMessage{
		ID:     "1781171178658-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	})
	if err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}

	if repo.updatedTask != nil && (repo.updatedTask.Status == models.TeamTaskStatusSucceeded || repo.updatedTask.FinishedAt != nil) {
		t.Fatalf("non-target reply must not mark task succeeded, got %#v", repo.updatedTask)
	}
	if len(repo.createdEvents) != 2 || repo.createdEvents[0].EventType != "reply" || repo.createdEvents[1].EventType != "member_result_confirmed" {
		t.Fatalf("expected stored event type reply, got %#v", repo.createdEvents)
	}
}

func TestProjectTeamEventDowngradesLiteDispatchWrapperFailure(t *testing.T) {
	taskID := 71
	messageID := "team-31-task-71"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 121,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	worker := &models.TeamMember{
		ID:            121,
		TeamID:        31,
		MemberKey:     "worker",
		Status:        models.TeamMemberStatusBusy,
		CurrentTaskID: &taskID,
		Availability:  models.TeamMemberAvailabilityBusy,
	}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"worker": worker},
	}
	service := &teamService{repo: repo}
	payload := map[string]interface{}{
		"event":        "message_failed",
		"memberId":     "worker",
		"availability": "blocked",
		"reason":       "dispatch finished without reply/completion",
		"text":         "Redis Team task failed",
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	err = service.projectTeamEvent(&models.Team{ID: 31}, nil, redisStreamMessage{
		ID:     "1781171178659-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	})
	if err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}

	if repo.updatedTask != nil {
		t.Fatalf("lite wrapper dispatch failure must not fail the task, got %#v", repo.updatedTask)
	}
	if repo.updatedMember == nil || repo.updatedMember.Availability == models.TeamMemberAvailabilityBlocked {
		t.Fatalf("lite wrapper dispatch failure must not block the member, got %#v", repo.updatedMember)
	}
	if len(repo.createdEvents) != 1 || repo.createdEvents[0].EventType != "message_warning" {
		t.Fatalf("expected warning event, got %#v", repo.createdEvents)
	}
}

func TestProjectTeamEventDoesNotTreatSuccessfulFailedWrapperAsCompletion(t *testing.T) {
	taskID := 74
	messageID := "team-31-task-74"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 121,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	worker := &models.TeamMember{
		ID:            121,
		TeamID:        31,
		MemberKey:     "worker",
		Status:        models.TeamMemberStatusBusy,
		CurrentTaskID: &taskID,
		Availability:  models.TeamMemberAvailabilityBusy,
	}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"worker": worker},
	}
	service := &teamService{repo: repo}
	payload := map[string]interface{}{
		"event":          "task_failed",
		"memberId":       "worker",
		"messageId":      messageID,
		"status":         "succeeded",
		"resultMarkdown": "Delivered correct result.",
		"summary":        "Finished successfully despite wrapper event type.",
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	err = service.projectTeamEvent(&models.Team{ID: 31}, nil, redisStreamMessage{
		ID:     "1781171178660-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	})
	if err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}

	if repo.updatedTask != nil && repo.updatedTask.Status == models.TeamTaskStatusSucceeded {
		t.Fatalf("a contradictory task_failed wrapper must not complete the task, got %#v", repo.updatedTask)
	}
	if repo.updatedMember == nil || repo.updatedMember.Availability == models.TeamMemberAvailabilityBlocked {
		t.Fatalf("successful task_failed wrapper must not block member, got %#v", repo.updatedMember)
	}
	if len(repo.createdEvents) != 1 {
		t.Fatalf("expected one stored event, got %#v", repo.createdEvents)
	}
	var stored map[string]interface{}
	if err := json.Unmarshal([]byte(*repo.createdEvents[0].PayloadJSON), &stored); err != nil {
		t.Fatalf("decode stored payload: %v", err)
	}
	step, ok := stored["collaborationStep"].(map[string]interface{})
	if !ok || step["type"] == "result" || step["status"] == models.TeamTaskStatusSucceeded {
		t.Fatalf("expected contradictory wrapper to remain non-terminal, got %#v", stored)
	}
}

func TestProjectTeamEventAssociatesLeaderPeerHandoffWithRootTask(t *testing.T) {
	taskID := 73
	messageID := "team-31-task-73"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	leader := &models.TeamMember{
		ID:            120,
		TeamID:        31,
		MemberKey:     "leader",
		Role:          "leader",
		Status:        models.TeamMemberStatusBusy,
		CurrentTaskID: &taskID,
		Availability:  models.TeamMemberAvailabilityBusy,
	}
	worker := &models.TeamMember{
		ID:           121,
		TeamID:       31,
		MemberKey:    "worker",
		Role:         "developer",
		Status:       models.TeamMemberStatusIdle,
		Availability: models.TeamMemberAvailabilityIdle,
	}
	repo := &teamRepositoryStub{
		tasksByID:    map[int]*models.TeamTask{taskID: task},
		membersByKey: map[string]*models.TeamMember{"leader": leader, "worker": worker},
	}
	service := &teamService{repo: repo}
	payload := map[string]interface{}{
		"event":     "outbound",
		"from":      "leader",
		"to":        "worker",
		"messageId": "worker-task-1",
		"title":     "Research requirement",
		"text":      "Please research the user segment and return evidence.",
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	err = service.projectTeamEvent(&models.Team{ID: 31, CommunicationMode: teamCommunicationModePeerAssisted}, nil, redisStreamMessage{
		ID:     "1781171178661-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	})
	if err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}

	if repo.updatedTask != nil && repo.updatedTask.Status == models.TeamTaskStatusSucceeded {
		t.Fatalf("leader handoff must not complete root task, got %#v", repo.updatedTask)
	}
	if len(repo.createdEvents) != 1 || repo.createdEvents[0].TaskID == nil || *repo.createdEvents[0].TaskID != taskID {
		t.Fatalf("expected handoff event linked to root task, got %#v", repo.createdEvents)
	}
	var stored map[string]interface{}
	if err := json.Unmarshal([]byte(*repo.createdEvents[0].PayloadJSON), &stored); err != nil {
		t.Fatalf("decode stored payload: %v", err)
	}
	step, ok := stored["collaborationStep"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected collaborationStep in payload: %#v", stored)
	}
	if step["type"] != "assignment" || step["status"] != models.TeamTaskStatusDispatched || step["target"] != "worker" {
		t.Fatalf("unexpected collaboration step: %#v", step)
	}
	if step["rootTaskId"] != "team-31-task-73" || step["rootMessageId"] != messageID {
		t.Fatalf("expected root task context, got %#v", step)
	}
}

func TestProjectTeamEventDoesNotCompleteRootTaskFromPeerMemberTerminal(t *testing.T) {
	taskID := 75
	messageID := "team-31-task-75"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	leader := &models.TeamMember{
		ID:           120,
		TeamID:       31,
		MemberKey:    "leader",
		Role:         "leader",
		Status:       models.TeamMemberStatusBusy,
		Availability: models.TeamMemberAvailabilityBusy,
	}
	workerTaskID := taskID
	worker := &models.TeamMember{
		ID:            121,
		TeamID:        31,
		MemberKey:     "worker",
		Role:          "developer",
		Status:        models.TeamMemberStatusBusy,
		CurrentTaskID: &workerTaskID,
		Availability:  models.TeamMemberAvailabilityBusy,
	}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": leader, "worker": worker},
	}
	service := &teamService{repo: repo}
	payload := map[string]interface{}{
		"event":              "task_completed",
		"memberId":           "worker",
		"messageId":          messageID,
		"taskId":             messageID,
		"status":             "succeeded",
		"summary":            "Worker delivery ready",
		"resultMarkdown":     "Worker completed the assigned research and produced a report for leader synthesis.",
		"explicitCompletion": true,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	err = service.projectTeamEvent(&models.Team{ID: 31, CommunicationMode: teamCommunicationModePeerAssisted}, nil, redisStreamMessage{
		ID:     "1781171178662-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	})
	if err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}

	if repo.updatedTask == nil || repo.updatedTask.Status == models.TeamTaskStatusSucceeded || repo.updatedTask.FinishedAt != nil {
		t.Fatalf("peer member terminal event should touch but not complete root task, got %#v", repo.updatedTask)
	}
	if repo.updatedMember == nil || repo.updatedMember.MemberKey != "worker" || repo.updatedMember.Status != models.TeamMemberStatusIdle || repo.updatedMember.Progress != 100 {
		t.Fatalf("expected peer member to be marked idle and complete, got %#v", repo.updatedMember)
	}
	if len(repo.createdEvents) != 1 || repo.createdEvents[0].TaskID == nil || *repo.createdEvents[0].TaskID != taskID {
		t.Fatalf("expected member terminal event linked to root task for visibility, got %#v", repo.createdEvents)
	}
	var stored map[string]interface{}
	if err := json.Unmarshal([]byte(*repo.createdEvents[0].PayloadJSON), &stored); err != nil {
		t.Fatalf("decode stored payload: %v", err)
	}
	if stored["memberTerminalOnly"] != true || stored["rootTaskTerminal"] != false {
		t.Fatalf("expected member terminal marker without root completion, got %#v", stored)
	}
}

func TestProjectTeamEventLeaderMediatedWorkerCompletionDoesNotCloseRootTask(t *testing.T) {
	taskID := 76
	messageID := "team-31-task-76"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	leader := &models.TeamMember{ID: 120, TeamID: 31, MemberKey: "leader", Role: "leader"}
	worker := &models.TeamMember{
		ID:            121,
		TeamID:        31,
		MemberKey:     "worker",
		Role:          "developer",
		Status:        models.TeamMemberStatusBusy,
		CurrentTaskID: &taskID,
		Availability:  models.TeamMemberAvailabilityBusy,
	}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": leader, "worker": worker},
	}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":          "task_completed",
		"memberId":       "worker",
		"messageId":      messageID,
		"taskId":         messageID,
		"status":         "succeeded",
		"summary":        "Worker delivery ready for Leader verification",
		"resultMarkdown": "The assigned work is complete with evidence and artifact paths.",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	service := &teamService{repo: repo}
	if err := service.projectTeamEvent(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID:     "1781171178663-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}
	if repo.updatedTask == nil || repo.updatedTask.Status == models.TeamTaskStatusSucceeded || repo.updatedTask.FinishedAt != nil {
		t.Fatalf("leader-mediated worker completion should touch but not close root task, got %#v", repo.updatedTask)
	}
	if repo.updatedMember == nil || repo.updatedMember.MemberKey != "worker" || repo.updatedMember.Status != models.TeamMemberStatusIdle || repo.updatedMember.Progress != 100 {
		t.Fatalf("worker delivery should complete only the worker lane, got %#v", repo.updatedMember)
	}
	if len(repo.createdEvents) != 2 || repo.createdEvents[0].TaskID == nil || *repo.createdEvents[0].TaskID != taskID || repo.createdEvents[1].EventType != "member_result_confirmed" {
		t.Fatalf("worker delivery must remain linked to the Leader root task, got %#v", repo.createdEvents)
	}
}

func TestProjectTeamEventLeaderMediatedWorkerReplyToLeaderIsAssignmentResult(t *testing.T) {
	taskID := 176
	messageID := "team-31-task-176"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	worker := &models.TeamMember{
		ID:            121,
		TeamID:        31,
		MemberKey:     "designer",
		Role:          "ui-ux-designer",
		Status:        models.TeamMemberStatusBusy,
		CurrentTaskID: &taskID,
		Availability:  models.TeamMemberAvailabilityBusy,
	}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"designer": worker},
	}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":      "outbound",
		"memberId":   "designer",
		"from":       "designer",
		"to":         "leader",
		"rootTaskId": messageID,
		"messageId":  "reply-designer-1",
		"text":       "1",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	service := &teamService{repo: repo}
	if err := service.projectTeamEvent(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID:     "1781171178676-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}
	if repo.updatedTask == nil || repo.updatedTask.Status == models.TeamTaskStatusSucceeded || repo.updatedTask.FinishedAt != nil {
		t.Fatalf("worker assignment result must not close Leader root task, got %#v", repo.updatedTask)
	}
	if repo.updatedMember == nil || repo.updatedMember.MemberKey != "designer" || repo.updatedMember.Status != models.TeamMemberStatusIdle || repo.updatedMember.Progress != 100 {
		t.Fatalf("worker reply should complete only the member lane, got %#v", repo.updatedMember)
	}
	var stored map[string]interface{}
	if err := json.Unmarshal([]byte(*repo.createdEvents[0].PayloadJSON), &stored); err != nil {
		t.Fatalf("decode stored payload: %v", err)
	}
	if stored["assignmentResultOnly"] != true || stored["rootTaskTerminal"] != false {
		t.Fatalf("expected assignment-result marker, got %#v", stored)
	}
	if stored["memberResultConfirmed"] != true || stored["normalizedResultSource"] != "legacy_normalized_reply" {
		t.Fatalf("expected normalized member result marker, got %#v", stored)
	}
	step := stored["collaborationStep"].(map[string]interface{})
	if step["type"] != "result" || step["status"] != models.TeamTaskStatusSucceeded || step["actor"] != "designer" {
		t.Fatalf("expected worker result collaboration step, got %#v", step)
	}
	if len(repo.createdEvents) != 2 || repo.createdEvents[1].EventType != "member_result_confirmed" {
		t.Fatalf("expected leader ledger notification after worker result, got %#v", repo.createdEvents)
	}
	var notification map[string]interface{}
	if err := json.Unmarshal([]byte(*repo.createdEvents[1].PayloadJSON), &notification); err != nil {
		t.Fatalf("decode leader notification: %v", err)
	}
	if notification["workId"] != "member-designer" || notification["to"] != "leader" || notification["memberResultConfirmed"] != true {
		t.Fatalf("expected structured leader notification, got %#v", notification)
	}
}

func TestProjectTeamEventLeaderMediatedIgnoresGenericWorkerCompletionSummary(t *testing.T) {
	taskID := 276
	messageID := "team-31-task-276"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	worker := &models.TeamMember{
		ID:            121,
		TeamID:        31,
		MemberKey:     "designer",
		Role:          "ui-ux-designer",
		Status:        models.TeamMemberStatusBusy,
		CurrentTaskID: &taskID,
		Availability:  models.TeamMemberAvailabilityBusy,
	}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"designer": worker},
	}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":     "task_completed",
		"memberId":  "designer",
		"from":      "designer",
		"to":        "leader",
		"taskId":    messageID,
		"messageId": "msg-worker-assignment",
		"status":    "succeeded",
		"summary":   "Redis Team task processing completed",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	service := &teamService{repo: repo}
	if err := service.projectTeamEvent(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID:     "1781171178678-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}
	if len(repo.createdEvents) != 1 {
		t.Fatalf("generic runtime completion summary must not create ledger notification, got %#v", repo.createdEvents)
	}
	if repo.createdEvents[0].EventType != "reply" {
		t.Fatalf("generic runtime completion should be downgraded to non-authoritative reply, got %#v", repo.createdEvents[0])
	}
	var stored map[string]interface{}
	if err := json.Unmarshal([]byte(*repo.createdEvents[0].PayloadJSON), &stored); err != nil {
		t.Fatalf("decode stored payload: %v", err)
	}
	if stored["assignmentResultOnly"] == true || stored["memberResultConfirmed"] == true {
		t.Fatalf("generic runtime completion must not be a member result, got %#v", stored)
	}
}

func TestProjectTeamEventLeaderMediatedRejectsWorkerSelfRoute(t *testing.T) {
	taskID := 177
	messageID := "team-31-task-177"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	architect := &models.TeamMember{
		ID:            122,
		TeamID:        31,
		MemberKey:     "architect",
		Role:          "architect",
		Status:        models.TeamMemberStatusBusy,
		CurrentTaskID: &taskID,
		Availability:  models.TeamMemberAvailabilityBusy,
	}
	repo := &teamRepositoryStub{
		tasksByID:    map[int]*models.TeamTask{taskID: task},
		membersByKey: map[string]*models.TeamMember{"architect": architect},
	}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":      "outbound",
		"memberId":   "architect",
		"from":       "architect",
		"to":         "architect",
		"rootTaskId": messageID,
		"messageId":  "self-loop",
		"text":       "42",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	service := &teamService{repo: repo}
	if err := service.projectTeamEvent(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID:     "1781171178677-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}
	if repo.updatedTask == nil || repo.updatedTask.Status == models.TeamTaskStatusSucceeded || repo.updatedTask.FinishedAt != nil {
		t.Fatalf("invalid worker route must not close root task, got %#v", repo.updatedTask)
	}
	if len(repo.createdEvents) != 1 || repo.createdEvents[0].EventType != "message_warning" {
		t.Fatalf("expected protocol warning event, got %#v", repo.createdEvents)
	}
	var stored map[string]interface{}
	if err := json.Unmarshal([]byte(*repo.createdEvents[0].PayloadJSON), &stored); err != nil {
		t.Fatalf("decode stored payload: %v", err)
	}
	if stored["leaderMediatedRouteViolation"] != true || stored["nonAuthoritative"] != true {
		t.Fatalf("expected route violation marker, got %#v", stored)
	}
	step := stored["collaborationStep"].(map[string]interface{})
	if step["type"] != "warning" {
		t.Fatalf("expected warning collaboration step, got %#v", step)
	}
}

func TestProjectTeamEventLeaderMediatedPrematureLeaderCompletionWaitsForAssignments(t *testing.T) {
	taskID := 178
	messageID := "team-31-task-178"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	leader := &models.TeamMember{
		ID:            120,
		TeamID:        31,
		MemberKey:     "leader",
		Role:          "leader",
		Status:        models.TeamMemberStatusBusy,
		CurrentTaskID: &taskID,
		Availability:  models.TeamMemberAvailabilityBusy,
	}
	assignmentPayload := `{"event":"reply","leaderDispatchOnly":true,"status":"dispatched","rootTaskId":"team-31-task-178","memberId":"leader","collaborationStep":{"type":"assignment","status":"dispatched","actor":"leader","target":"designer","rootTaskId":"team-31-task-178","content":"Please return a number."}}`
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": leader},
		createdEvents: []models.TeamEvent{{
			TeamID:      31,
			TaskID:      &taskID,
			EventType:   "reply",
			PayloadJSON: &assignmentPayload,
		}},
	}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":              "task_completed",
		"memberId":           "leader",
		"messageId":          messageID,
		"taskId":             messageID,
		"status":             "succeeded",
		"summary":            "Final result ready too early",
		"resultMarkdown":     "Designer result is pending.",
		"explicitCompletion": true,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	service := &teamService{repo: repo}
	if err := service.projectTeamEvent(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID:     "1781171178678-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}
	if repo.updatedTask == nil || repo.updatedTask.Status == models.TeamTaskStatusSucceeded || repo.updatedTask.FinishedAt != nil {
		t.Fatalf("premature Leader completion must keep root task open, got %#v", repo.updatedTask)
	}
	stored := map[string]interface{}{}
	if err := json.Unmarshal([]byte(*repo.createdEvents[len(repo.createdEvents)-1].PayloadJSON), &stored); err != nil {
		t.Fatalf("decode stored payload: %v", err)
	}
	if stored["completionDecision"] != teamCompletionDecisionDeferred || stored["completionDecisionReason"] != "pending_legacy_assignments" || stored["rootTaskTerminal"] != false {
		t.Fatalf("expected structured deferred completion, got %#v", stored)
	}
}

func TestProjectTeamEventLeaderMediatedLeaderCompletionAfterAssignmentResultsClosesRoot(t *testing.T) {
	taskID := 179
	messageID := "team-31-task-179"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	leader := &models.TeamMember{
		ID:            120,
		TeamID:        31,
		MemberKey:     "leader",
		Role:          "leader",
		Status:        models.TeamMemberStatusBusy,
		CurrentTaskID: &taskID,
		Availability:  models.TeamMemberAvailabilityBusy,
	}
	assignmentPayload := `{"event":"reply","leaderDispatchOnly":true,"status":"dispatched","rootTaskId":"team-31-task-179","memberId":"leader","collaborationStep":{"type":"assignment","status":"dispatched","actor":"leader","target":"designer","rootTaskId":"team-31-task-179","content":"Please return a number."}}`
	resultPayload := `{"event":"outbound","assignmentResultOnly":true,"status":"succeeded","rootTaskId":"team-31-task-179","memberId":"designer","from":"designer","to":"leader","text":"1","collaborationStep":{"type":"result","status":"succeeded","actor":"designer","target":"leader","rootTaskId":"team-31-task-179","content":"1"}}`
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": leader},
		createdEvents: []models.TeamEvent{
			{TeamID: 31, TaskID: &taskID, EventType: "reply", PayloadJSON: &assignmentPayload},
			{TeamID: 31, TaskID: &taskID, EventType: "outbound", PayloadJSON: &resultPayload},
		},
	}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":              "task_completed",
		"memberId":           "leader",
		"messageId":          messageID,
		"taskId":             messageID,
		"status":             "succeeded",
		"summary":            "Designer returned 1; final synthesis complete.",
		"resultMarkdown":     "Final result: designer=1.",
		"explicitCompletion": true,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	service := &teamService{repo: repo}
	if err := service.projectTeamEvent(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID:     "1781171178679-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}
	if repo.updatedTask == nil || repo.updatedTask.Status != models.TeamTaskStatusSucceeded || repo.updatedTask.FinishedAt == nil {
		t.Fatalf("Leader final synthesis after member results should close root task, got %#v", repo.updatedTask)
	}
	if repo.updatedTask.ResultJSON == nil || !strings.Contains(*repo.updatedTask.ResultJSON, "designer=1") {
		t.Fatalf("expected final synthesis result stored, got %#v", repo.updatedTask.ResultJSON)
	}
	if len(repo.outboxRows) != 1 || !strings.Contains(repo.outboxRows[0].Destination, "completion-acks") || !strings.Contains(repo.outboxRows[0].PayloadJSON, `"decision":"accepted"`) {
		t.Fatalf("accepted root completion must atomically persist its acknowledgement: %#v", repo.outboxRows)
	}
	acceptedPayload := teamEventPayloadMap(repo.createdEvents[len(repo.createdEvents)-1])
	if eventString(acceptedPayload, "chatKind") != "final_delivery" || eventString(acceptedPayload, "displayKey") != "root-final:179" || eventString(acceptedPayload, "resultMarkdown") == "" {
		t.Fatalf("accepted completion must expose one full final delivery event: %#v", acceptedPayload)
	}
}

func TestProjectTeamEventLeaderMediatedInterimLeaderCompletionDoesNotCloseAfterMemberResults(t *testing.T) {
	taskID := 183
	messageID := "team-31-task-183"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	leader := &models.TeamMember{ID: 120, TeamID: 31, MemberKey: "leader", Role: "leader", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	pm := &models.TeamMember{ID: 121, TeamID: 31, MemberKey: "pm", Role: "product-manager", Status: models.TeamMemberStatusIdle, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityIdle}
	designer := &models.TeamMember{ID: 122, TeamID: 31, MemberKey: "designer", Role: "designer", Status: models.TeamMemberStatusIdle, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityIdle}
	architect := &models.TeamMember{ID: 123, TeamID: 31, MemberKey: "architect", Role: "architect", Status: models.TeamMemberStatusIdle, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityIdle}
	now := time.Now().UTC()
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey: map[string]*models.TeamMember{
			"leader":    leader,
			"pm":        pm,
			"designer":  designer,
			"architect": architect,
		},
		workItems: []models.TeamWorkItem{
			{TeamID: 31, RootTaskID: taskID, WorkID: "member-pm", OwnerMemberID: &pm.ID, Status: models.TeamTaskStatusSucceeded, UpdatedAt: now},
			{TeamID: 31, RootTaskID: taskID, WorkID: "member-designer", OwnerMemberID: &designer.ID, Status: models.TeamTaskStatusSucceeded, UpdatedAt: now},
			{TeamID: 31, RootTaskID: taskID, WorkID: "member-architect", OwnerMemberID: &architect.ID, Status: models.TeamTaskStatusSucceeded, UpdatedAt: now},
		},
	}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":              "task_completed",
		"memberId":           "leader",
		"messageId":          messageID,
		"taskId":             messageID,
		"status":             "succeeded",
		"summary":            "PM result archived. Still waiting on Designer and Architect.",
		"resultMarkdown":     "PM result archived. Still waiting on Designer and Architect.",
		"explicitCompletion": true,
		"rootTaskTerminal":   true,
		"completionId":       "leader-fallback-msg-1",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	service := &teamService{repo: repo}
	if err := service.projectTeamEvent(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID:     "1781171178682-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}
	if repo.updatedTask == nil || repo.updatedTask.Status == models.TeamTaskStatusSucceeded || repo.updatedTask.FinishedAt != nil {
		t.Fatalf("interim Leader completion must keep root task open even after member results, got %#v", repo.updatedTask)
	}
	stored := map[string]interface{}{}
	if err := json.Unmarshal([]byte(*repo.createdEvents[len(repo.createdEvents)-1].PayloadJSON), &stored); err != nil {
		t.Fatalf("decode stored payload: %v", err)
	}
	if stored["completionDecision"] != teamCompletionDecisionNeedsConfirmation || stored["completionDecisionReason"] != "narrative_indicates_remaining_work" || stored["rootTaskTerminal"] != false {
		t.Fatalf("expected completion confirmation request, got %#v", stored)
	}
}

func TestEvaluateProtocolV3CompletionAllowsUnusedPlannedPhaseAfterWorkflowSeal(t *testing.T) {
	leaderID := 120
	workerID := 121
	task := &models.TeamTask{
		ID: 190, TeamID: 31, TargetMemberID: leaderID, Status: models.TeamTaskStatusRunning,
		WorkflowState: teamWorkflowStateAwaitingLeaderDecision, PlanVersion: 2, LedgerVersion: 7,
	}
	assignmentID := "research-pm"
	phaseID := "research"
	repo := &teamRepositoryStub{
		workItems: []models.TeamWorkItem{{
			TeamID: 31, RootTaskID: task.ID, WorkID: assignmentID, AssignmentID: &assignmentID,
			PhaseID: &phaseID, Revision: 1, RequiredForRoot: true, OwnerMemberID: &workerID,
			Status: models.TeamTaskStatusSucceeded,
		}},
		workflowPhases: []models.TeamWorkflowPhase{
			{TeamID: 31, RootTaskID: task.ID, PhaseID: "research", PlanVersion: 2, Status: teamPhaseStatusCompleted, RequiredForRoot: true},
			{TeamID: 31, RootTaskID: task.ID, PhaseID: "implementation", PlanVersion: 2, Status: teamPhaseStatusPlanned, RequiredForRoot: true},
		},
	}
	service := &teamService{repo: repo}
	payload := map[string]interface{}{
		"protocolVersion": 3, "completionId": "completion-190", "completionSource": teamTaskCompletionTool,
		"explicitCompletion": true, "rootTaskTerminal": true, "workflowFinal": true, "finalAnswerReady": true,
		"planVersion": 2, "ledgerVersion": 7, "status": "succeeded", "summary": "第一阶段完成",
		"resultMarkdown": "第一阶段结果已经汇总。",
	}
	evaluation, err := service.evaluateLeaderRootCompletion(
		&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, task,
		&models.TeamMember{ID: leaderID, TeamID: 31, MemberKey: "leader", Role: "leader"}, payload,
	)
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Decision != teamCompletionDecisionAccepted {
		t.Fatalf("an explicitly sealed workflow must ignore a planned phase with no dispatched work: %#v", evaluation)
	}
}

func TestReconcileTeamWorkflowLedgerRepairsCompletedAndUnusedPhases(t *testing.T) {
	now := time.Now().UTC()
	leaderID := 120
	workerID := 121
	assignmentID := "review-calculator"
	phaseID := "phase-2"
	task := &models.TeamTask{
		ID: 201, TeamID: 31, TargetMemberID: leaderID, Status: models.TeamTaskStatusRunning,
		WorkflowState: teamWorkflowStateAwaitingPhaseResults, PlanVersion: 1, LedgerVersion: 6,
		CurrentPhaseID: &phaseID,
	}
	repo := &teamRepositoryStub{
		workItems: []models.TeamWorkItem{{
			TeamID: 31, RootTaskID: task.ID, WorkID: assignmentID, AssignmentID: &assignmentID,
			PhaseID: &phaseID, OwnerMemberID: &workerID, RequiredForRoot: true,
			Revision: 1, Status: models.TeamTaskStatusSucceeded,
		}},
		workflowPhases: []models.TeamWorkflowPhase{
			{TeamID: 31, RootTaskID: task.ID, PhaseID: phaseID, PlanVersion: 1, Status: teamPhaseStatusAwaitingResults, RequiredForRoot: true},
			{TeamID: 31, RootTaskID: task.ID, PhaseID: "phase-3", PlanVersion: 1, Status: teamPhaseStatusPlanned, RequiredForRoot: true},
		},
	}
	service := &teamService{repo: repo}
	changed, err := service.reconcileTeamWorkflowLedger(task, true, now)
	if err != nil || !changed {
		t.Fatalf("expected workflow ledger repair, changed=%v err=%v", changed, err)
	}
	if repo.workflowPhases[0].Status != teamPhaseStatusCompleted || repo.workflowPhases[1].Status != teamPhaseStatusCancelled {
		t.Fatalf("expected completed current phase and cancelled unused planned phase, got %#v", repo.workflowPhases)
	}
	if task.WorkflowState != teamWorkflowStateSynthesizing || task.LedgerVersion != 7 || task.CurrentPhaseID != nil {
		t.Fatalf("unexpected reconciled task state: %#v", task)
	}
}

func TestMarkStructuredCompletionDeferredRemainsVisibleInChat(t *testing.T) {
	payload := map[string]interface{}{
		"completionId": "completion-201", "resultMarkdown": "# Final delivery\n\nFull report.",
	}
	markStructuredCompletionDecision("completion_proposed", payload, teamCompletionEvaluation{
		Decision: teamCompletionDecisionDeferred, Reason: "open_workflow_phases", LedgerVersion: 9,
	})
	if !eventBool(payload, "visibleToChat") || eventString(payload, "chatPolicy") != "warning" || eventString(payload, "chatKind") != "completion_deferred" {
		t.Fatalf("deferred completion must retain visible report and diagnostic: %#v", payload)
	}
}

func TestReconcileDeferredCompletionAcceptsAfterLedgerRepair(t *testing.T) {
	now := time.Now().UTC()
	taskID := 202
	leaderID := 120
	workerID := 121
	messageID := "team-31-task-202"
	assignmentID := "review-calculator"
	phaseID := "phase-2"
	task := &models.TeamTask{
		ID: taskID, TeamID: 31, TargetMemberID: leaderID, MessageID: messageID,
		Status: models.TeamTaskStatusRunning, WorkflowState: teamWorkflowStateAwaitingPhaseResults,
		PlanVersion: 1, LedgerVersion: 6, UpdatedAt: now,
	}
	leader := &models.TeamMember{ID: leaderID, TeamID: 31, MemberKey: "leader", Role: "leader", Status: models.TeamMemberStatusBusy}
	deferredPayload, err := json.Marshal(map[string]interface{}{
		"protocolVersion": 3, "event": "completion_deferred", "completionId": "completion-202",
		"attemptId": "attempt-202", "completionSource": teamTaskCompletionTool, "explicitCompletion": true,
		"rootTaskTerminal": false, "workflowFinal": true, "finalAnswerReady": true,
		"remainingActions": []string{}, "planVersion": 1, "ledgerVersion": 6,
		"summary":        "Calculator delivered and verified.",
		"resultMarkdown": "# Final delivery\n\nCalculator delivered and verified.",
	})
	if err != nil {
		t.Fatal(err)
	}
	eventID := "deferred-202"
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": leader},
		workItems: []models.TeamWorkItem{{
			TeamID: 31, RootTaskID: taskID, WorkID: assignmentID, AssignmentID: &assignmentID,
			PhaseID: &phaseID, OwnerMemberID: &workerID, RequiredForRoot: true,
			Revision: 1, Status: models.TeamTaskStatusSucceeded,
		}},
		workflowPhases: []models.TeamWorkflowPhase{
			{TeamID: 31, RootTaskID: taskID, PhaseID: phaseID, PlanVersion: 1, Status: teamPhaseStatusAwaitingResults, RequiredForRoot: true},
			{TeamID: 31, RootTaskID: taskID, PhaseID: "phase-3", PlanVersion: 1, Status: teamPhaseStatusPlanned, RequiredForRoot: true},
		},
		createdEvents: []models.TeamEvent{{
			TeamID: 31, TaskID: &taskID, MemberID: &leaderID, EventID: &eventID,
			EventType: "completion_deferred", PayloadJSON: stringPtr(string(deferredPayload)),
		}},
	}
	service := &teamService{repo: repo}
	reconciled, err := service.reconcileDeferredTeamCompletion(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, task, leader)
	if err != nil || !reconciled || task.Status != models.TeamTaskStatusSucceeded {
		t.Fatalf("expected deferred completion to self-heal without another Leader turn: reconciled=%v task=%#v err=%v", reconciled, task, err)
	}
	if repo.workflowPhases[0].Status != teamPhaseStatusCompleted || repo.workflowPhases[1].Status != teamPhaseStatusCancelled {
		t.Fatalf("expected repaired phase ledger before acceptance: %#v", repo.workflowPhases)
	}
}

func TestProjectProtocolV3DeferredCompletionPersistsAcknowledgementOutbox(t *testing.T) {
	now := time.Now().UTC()
	taskID := 193
	messageID := "team-31-task-193"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		WorkflowState:  teamWorkflowStateExecuting,
		PlanVersion:    1,
		LedgerVersion:  2,
		UpdatedAt:      now,
	}
	leader := &models.TeamMember{ID: 120, TeamID: 31, MemberKey: "leader", Role: "leader", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID}
	workerID := 121
	assignmentID := "implementation"
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": leader},
		workItems: []models.TeamWorkItem{{
			TeamID:          31,
			RootTaskID:      taskID,
			WorkID:          assignmentID,
			AssignmentID:    &assignmentID,
			OwnerMemberID:   &workerID,
			Status:          models.TeamTaskStatusRunning,
			RequiredForRoot: true,
			Revision:        1,
			UpdatedAt:       now,
		}},
	}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"protocolVersion":    3,
		"event":              "completion_proposed",
		"eventId":            "evt-deferred-193",
		"completionId":       "completion:31:team-31-task-193:leader",
		"attemptId":          "attempt-deferred-193",
		"completionSource":   teamTaskCompletionTool,
		"explicitCompletion": true,
		"rootTaskTerminal":   true,
		"memberId":           "leader",
		"messageId":          messageID,
		"taskId":             messageID,
		"rootTaskId":         messageID,
		"status":             "succeeded",
		"summary":            "最终报告已准备，但实现任务仍在执行。",
		"resultMarkdown":     "# 最终报告\n\n等待结构化账本允许后提交。",
		"workflowFinal":      true,
		"finalAnswerReady":   true,
		"remainingActions":   []string{},
		"planVersion":        1,
		"ledgerVersion":      2,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	service := &teamService{repo: repo}
	if err := service.projectTeamEvent(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID: "1781171178993-0", Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}
	if len(repo.createdEvents) != 1 || repo.createdEvents[0].EventType != "completion_deferred" {
		t.Fatalf("pending required assignment must defer root completion, got %#v", repo.createdEvents)
	}
	if len(repo.outboxRows) != 1 || !strings.Contains(repo.outboxRows[0].PayloadJSON, `"decision":"deferred"`) || !strings.Contains(repo.outboxRows[0].PayloadJSON, assignmentID) {
		t.Fatalf("deferred completion acknowledgement must be durable and diagnostic: %#v", repo.outboxRows)
	}
}

func TestEvaluateProtocolV3DynamicPhaseRequiresLeaderDecisionOrWorkflowSeal(t *testing.T) {
	leaderID := 120
	workerID := 121
	assignmentID := "collect-worker-input"
	phaseID := "collection"
	task := &models.TeamTask{ID: 191, TeamID: 31, TargetMemberID: leaderID, Status: models.TeamTaskStatusRunning, WorkflowState: teamWorkflowStateAwaitingLeaderDecision, PlanVersion: 1, LedgerVersion: 4}
	repo := &teamRepositoryStub{
		workItems:      []models.TeamWorkItem{{TeamID: 31, RootTaskID: task.ID, WorkID: assignmentID, AssignmentID: &assignmentID, PhaseID: &phaseID, Revision: 1, RequiredForRoot: true, OwnerMemberID: &workerID, Status: models.TeamTaskStatusSucceeded}},
		workflowPhases: []models.TeamWorkflowPhase{{TeamID: 31, RootTaskID: task.ID, PhaseID: phaseID, PlanVersion: 1, Status: teamPhaseStatusAwaitingLeaderDecision, RequiredForRoot: true, DecisionRequired: true}},
	}
	service := &teamService{repo: repo}
	payload := map[string]interface{}{
		"protocolVersion": 3, "completionId": "completion-191", "completionSource": teamTaskCompletionTool,
		"explicitCompletion": true, "rootTaskTerminal": true, "finalAnswerReady": true,
		"planVersion": 1, "ledgerVersion": 4, "status": "succeeded", "summary": "信息已收集", "resultMarkdown": "信息已收集。",
	}
	leader := &models.TeamMember{ID: leaderID, TeamID: 31, MemberKey: "leader", Role: "leader"}
	evaluation, err := service.evaluateLeaderRootCompletion(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, task, leader, payload)
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Decision != teamCompletionDecisionDeferred || evaluation.Reason != "workflow_not_sealed" {
		t.Fatalf("dynamic phase must wait for Leader decision: %#v", evaluation)
	}
	payload["workflowFinal"] = true
	evaluation, err = service.evaluateLeaderRootCompletion(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, task, leader, payload)
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Decision != teamCompletionDecisionAccepted {
		t.Fatalf("explicitly sealed dynamic workflow should be accepted: %#v", evaluation)
	}
}

func TestEvaluateProtocolV3CompletionDoesNotTreatFeatureWordAsInterim(t *testing.T) {
	if isInterimOrDelegationReplyText("计算器最终交付报告：重复 = 操作，所有功能均已验证。") {
		t.Fatal("legacy narrative helper must not treat an ordinary feature label as an interim reply")
	}
	task := &models.TeamTask{ID: 192, TeamID: 31, TargetMemberID: 120, Status: models.TeamTaskStatusRunning, WorkflowState: teamWorkflowStateSynthesizing, PlanVersion: 1, LedgerVersion: 1}
	service := &teamService{repo: &teamRepositoryStub{}}
	payload := map[string]interface{}{
		"protocolVersion": 3, "completionId": "completion-192", "completionSource": teamTaskCompletionTool,
		"explicitCompletion": true, "rootTaskTerminal": true, "workflowFinal": true, "finalAnswerReady": true,
		"planVersion": 1, "ledgerVersion": 1, "status": "succeeded", "summary": "计算器最终交付报告",
		"resultMarkdown": "功能清单：重复 = 操作。所有功能已验证。",
	}
	evaluation, err := service.evaluateLeaderRootCompletion(
		&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, task,
		&models.TeamMember{ID: 120, TeamID: 31, MemberKey: "leader", Role: "leader"}, payload,
	)
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Decision != teamCompletionDecisionAccepted || len(evaluation.StrongContradictions) != 0 {
		t.Fatalf("ordinary feature wording must not veto structured completion: %#v", evaluation)
	}
}

func TestEvaluateProtocolV3RequiresStructuredRiskWaiverForFailedRequiredWork(t *testing.T) {
	leaderID := 120
	workerID := 121
	assignmentID := "browser-validation"
	optionalAssignmentID := "nice-to-have-benchmark"
	phaseID := "validation"
	task := &models.TeamTask{ID: 195, TeamID: 31, TargetMemberID: leaderID, Status: models.TeamTaskStatusRunning, WorkflowState: teamWorkflowStateSynthesizing, PlanVersion: 1, LedgerVersion: 5}
	repo := &teamRepositoryStub{
		workItems: []models.TeamWorkItem{
			{ID: 1, TeamID: 31, RootTaskID: task.ID, WorkID: assignmentID, AssignmentID: &assignmentID, PhaseID: &phaseID, Revision: 1, RequiredForRoot: true, OwnerMemberID: &workerID, Status: models.TeamTaskStatusFailed},
			{ID: 2, TeamID: 31, RootTaskID: task.ID, WorkID: optionalAssignmentID, AssignmentID: &optionalAssignmentID, PhaseID: &phaseID, Revision: 1, RequiredForRoot: false, OwnerMemberID: &workerID, Status: models.TeamTaskStatusRunning},
		},
		workflowPhases: []models.TeamWorkflowPhase{{TeamID: 31, RootTaskID: task.ID, PhaseID: phaseID, PlanVersion: 1, Status: teamPhaseStatusAwaitingResults, RequiredForRoot: true}},
	}
	service := &teamService{repo: repo}
	payload := map[string]interface{}{
		"protocolVersion": 3, "completionId": "completion-195", "completionSource": teamTaskCompletionTool,
		"explicitCompletion": true, "rootTaskTerminal": true, "workflowFinal": true, "finalAnswerReady": true,
		"planVersion": 1, "ledgerVersion": 5, "status": "succeeded", "summary": "最终交付报告",
		"resultMarkdown": "浏览器验证失败，交付报告明确记录该风险。",
	}
	leader := &models.TeamMember{ID: leaderID, TeamID: 31, MemberKey: "leader", Role: "leader"}
	evaluation, err := service.evaluateLeaderRootCompletion(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, task, leader, payload)
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Decision != teamCompletionDecisionDeferred || !containsTeamString(evaluation.PendingAssignments, assignmentID) {
		t.Fatalf("failed required work without a waiver must block completion: %#v", evaluation)
	}
	payload["waivers"] = []interface{}{map[string]interface{}{"assignmentId": assignmentID, "reason": "测试环境不可用"}}
	evaluation, err = service.evaluateLeaderRootCompletion(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, task, leader, payload)
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Decision != teamCompletionDecisionDeferred {
		t.Fatalf("waiver without an accepted risk must not close the root task: %#v", evaluation)
	}
	payload["waivers"] = []interface{}{map[string]interface{}{
		"assignmentId": assignmentID,
		"reason":       "测试环境不可用",
		"risk":         "浏览器交互仍未验证，发布前必须人工复核",
	}}
	evaluation, err = service.evaluateLeaderRootCompletion(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, task, leader, payload)
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Decision != teamCompletionDecisionDeferred || !containsTeamString(evaluation.PendingAssignments, optionalAssignmentID+":skip_reason") {
		t.Fatalf("omitted optional work must have a structured skip reason: %#v", evaluation)
	}
	payload["skippedAssignments"] = []interface{}{map[string]interface{}{"assignmentId": optionalAssignmentID, "reason": "不影响核心交付，留待后续性能专项"}}
	evaluation, err = service.evaluateLeaderRootCompletion(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, task, leader, payload)
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Decision != teamCompletionDecisionAccepted || !containsTeamString(evaluation.WaivedAssignments, assignmentID) || !containsTeamString(evaluation.SkippedAssignments, optionalAssignmentID) {
		t.Fatalf("complete structured waiver and optional skip record should permit a recorded partial-risk delivery: %#v", evaluation)
	}
}

func TestProjectTeamWorkItemKeepsSequentialAssignmentsForSameMember(t *testing.T) {
	team := &models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}
	task := &models.TeamTask{ID: 193, TeamID: 31, TargetMemberID: 120, Status: models.TeamTaskStatusRunning}
	developer := &models.TeamMember{ID: 121, TeamID: 31, MemberKey: "developer", Role: "developer"}
	repo := &teamRepositoryStub{membersByKey: map[string]*models.TeamMember{"developer": developer}}
	service := &teamService{repo: repo}
	for index, assignmentID := range []string{"developer-research", "developer-implementation"} {
		phaseID := []string{"research", "implementation"}[index]
		payload := map[string]interface{}{
			"assignmentId": assignmentID, "workId": assignmentID, "phaseId": phaseID,
			"required": true, "revision": 1,
			"collaborationStep": map[string]interface{}{
				"type": "assignment", "status": "dispatched", "actor": "leader", "target": "developer",
				"workId": assignmentID, "phase": phaseID, "title": assignmentID,
			},
		}
		if err := service.projectTeamWorkItem(team, task, developer, "team_send", payload, &models.TeamEvent{CreatedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	if len(repo.workItems) != 2 || repo.workItems[0].WorkID == repo.workItems[1].WorkID {
		t.Fatalf("sequential assignments for one member must remain distinct: %#v", repo.workItems)
	}
}

func TestArtifactChangeInvalidatesMatchingReviewedWorkItem(t *testing.T) {
	taskID := 196
	leaderID := 120
	developerID := 121
	validatedRevision := 1
	assignmentID := "developer-implementation"
	artifactRefs := `["/team/artifacts/team-31-task-196/members/developer/app.js"]`
	task := &models.TeamTask{ID: taskID, TeamID: 31, TargetMemberID: leaderID, MessageID: "team-31-task-196", Status: models.TeamTaskStatusRunning, LedgerVersion: 3, UpdatedAt: time.Now().UTC()}
	leader := &models.TeamMember{ID: leaderID, TeamID: 31, MemberKey: "leader", Role: "leader", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{task.MessageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": leader},
		workItems: []models.TeamWorkItem{{
			ID: 1, TeamID: 31, RootTaskID: taskID, WorkID: assignmentID, AssignmentID: &assignmentID,
			OwnerMemberID: &developerID, Revision: 1, RequiredForRoot: true, ReviewRequired: true,
			ValidatedRevision: &validatedRevision, Status: models.TeamTaskStatusSucceeded, ArtifactRefsJSON: &artifactRefs,
		}},
	}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event": "artifact_changed", "eventKind": "artifact_changed", "eventId": "artifact-change-196",
		"memberId": "leader", "messageId": task.MessageID, "taskId": task.MessageID, "rootTaskId": task.MessageID,
		"assignmentId": "leader-final-synthesis", "artifactChanged": true,
		"artifactRefs": []string{"/team/artifacts/team-31-task-196/members/developer/app.js"},
		"status":       "running", "summary": "Leader updated the reviewed artifact.",
	})
	if err != nil {
		t.Fatal(err)
	}
	service := &teamService{repo: repo}
	if err := service.projectTeamEvent(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID: "1781171178996-0", Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatal(err)
	}
	for idx := range repo.workItems {
		if repo.workItems[idx].ID == 1 && repo.workItems[idx].ValidatedRevision != nil {
			t.Fatalf("modifying a reviewed artifact must invalidate the old review: %#v", repo.workItems[idx])
		}
	}
	if repo.updatedTask == nil || repo.updatedTask.LedgerVersion != 4 {
		t.Fatalf("review invalidation must advance the root ledger: %#v", repo.updatedTask)
	}
}

func TestTeamChatPolicyPreservesMeaningfulCheckProgress(t *testing.T) {
	payload := map[string]interface{}{
		"eventKind": "assignment_check_result", "checkId": "monitor:team-31-task-1:work-1:2",
		"assignmentId": "work-1", "rootTaskId": "team-31-task-1", "progress": 60,
		"summary": "已完成核心模块，正在进行集成验证。",
	}
	applyTeamChatPolicy("task_progress", payload, nil, &models.TeamMember{MemberKey: "developer"})
	if payload["chatPolicy"] != "visible" || payload["visibleToChat"] != true || payload["chatBusinessKind"] != "worker_progress" {
		t.Fatalf("meaningful check progress must remain visible: %#v", payload)
	}
	quiet := map[string]interface{}{
		"eventKind": "assignment_check_result", "checkId": "monitor:team-31-task-1:work-1:3",
		"assignmentId": "work-1", "rootTaskId": "team-31-task-1", "summary": "still running",
	}
	applyTeamChatPolicy("task_progress", quiet, nil, &models.TeamMember{MemberKey: "developer"})
	if quiet["chatPolicy"] != "digest" || quiet["visibleToChat"] != false {
		t.Fatalf("unchanged transport check should be digested: %#v", quiet)
	}
}

func TestTeamChatPolicyBusinessNarrativeOverridesLegacyHiddenPolicy(t *testing.T) {
	payload := map[string]interface{}{
		"eventKind": "worker_plan", "chatPolicy": "hidden", "visibleToChat": false,
		"memberId": "developer", "phaseId": "implementation",
		"text":       "I will implement the calculator UI and report the deliverable to the Leader.",
		"displayKey": "worker-plan:team-31-task-1:",
	}
	applyTeamChatPolicy("task_progress", payload, nil, &models.TeamMember{MemberKey: "developer"})
	if payload["chatPolicy"] != "visible" || payload["visibleToChat"] != true || eventString(payload, "displayKey") != "" {
		t.Fatalf("business narrative must override stale hidden policy and empty worker display key: %#v", payload)
	}
}

func TestTeamChatPolicyKeepsTransportAcknowledgementHidden(t *testing.T) {
	payload := map[string]interface{}{
		"eventKind": "assignment_heartbeat", "summary": "still running", "visibleToChat": true,
	}
	applyTeamChatPolicy("assignment_heartbeat", payload, nil, &models.TeamMember{MemberKey: "developer"})
	if payload["chatPolicy"] != "digest" || payload["visibleToChat"] != false {
		t.Fatalf("heartbeat without business content must remain digest-only: %#v", payload)
	}
}

func TestTeamEventPayloadsFilterOnlyHiddenTransportFacts(t *testing.T) {
	hiddenJSON := `{"event":"member_result_confirmed","chatPolicy":"hidden","visibleToChat":false}`
	progressJSON := `{"event":"task_progress","eventKind":"worker_progress","chatPolicy":"replaceable","visibleToChat":true,"summary":"正在实现核心模块"}`
	digestJSON := `{"event":"assignment_heartbeat","chatPolicy":"digest","visibleToChat":false,"summary":"仍在执行"}`
	events := []models.TeamEvent{
		{ID: 1, EventType: "member_result_confirmed", PayloadJSON: &hiddenJSON},
		{ID: 2, EventType: "task_progress", PayloadJSON: &progressJSON},
		{ID: 3, EventType: "assignment_heartbeat", PayloadJSON: &digestJSON},
	}
	payloads := teamEventPayloads(events)
	if len(payloads) != 2 || payloads[0].ID != 2 || payloads[1].ID != 3 {
		t.Fatalf("chat projection must preserve business progress and digests while removing hidden confirmation facts: %#v", payloads)
	}
}

func containsTeamString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func TestTeamEventPayloadsRestoreLegacyHiddenBusinessNarrative(t *testing.T) {
	workerPlanJSON := `{"event":"task_progress","eventKind":"worker_plan","chatPolicy":"hidden","visibleToChat":false,"text":"Worker plan: implement the core module and report to Leader."}`
	transportJSON := `{"event":"task_received","chatPolicy":"hidden","visibleToChat":false,"summary":"task_received"}`
	events := []models.TeamEvent{
		{ID: 31, EventType: "task_progress", PayloadJSON: &workerPlanJSON},
		{ID: 32, EventType: "task_received", PayloadJSON: &transportJSON},
	}
	payloads := teamEventPayloads(events)
	if len(payloads) != 1 || payloads[0].ID != 31 {
		t.Fatalf("legacy hidden business narrative must be returned while transport acknowledgement stays hidden: %#v", payloads)
	}
}

func TestProjectTeamEventAssignmentResultUsesActualMemberLane(t *testing.T) {
	taskID := 180
	messageID := "team-31-task-180"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	leader := &models.TeamMember{ID: 120, TeamID: 31, MemberKey: "leader", Role: "leader", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	developer := &models.TeamMember{ID: 121, TeamID: 31, MemberKey: "developer", Role: "senior-developer", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	reviewer := &models.TeamMember{ID: 122, TeamID: 31, MemberKey: "reviewer", Role: "qa-engineer", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": leader, "developer": developer, "reviewer": reviewer},
	}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":                "outbound",
		"assignmentResultOnly": true,
		"assignmentId":         "assignment-developer-001",
		"memberId":             "reviewer",
		"from":                 "reviewer",
		"to":                   "leader",
		"rootTaskId":           messageID,
		"status":               "succeeded",
		"text":                 "Reviewer delivered a valid result.",
		"collaborationStep": map[string]interface{}{
			"type":       "result",
			"status":     "succeeded",
			"actor":      "reviewer",
			"target":     "leader",
			"rootTaskId": messageID,
			"content":    "Reviewer delivered a valid result.",
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	service := &teamService{repo: repo}
	if err := service.projectTeamEvent(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID:     "1781171178680-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}
	if len(repo.workItems) != 1 {
		t.Fatalf("expected one work item, got %#v", repo.workItems)
	}
	if repo.workItems[0].WorkID != "assignment-developer-001" {
		t.Fatalf("assignment result must preserve the business assignment ID, got workID %q", repo.workItems[0].WorkID)
	}
	if repo.workItems[0].OwnerMemberID == nil || *repo.workItems[0].OwnerMemberID != reviewer.ID {
		t.Fatalf("expected reviewer owner, got %#v", repo.workItems[0].OwnerMemberID)
	}
}

func TestProjectTeamEventLeaderDispatchAndWorkerResultShareMemberLane(t *testing.T) {
	taskID := 181
	messageID := "team-31-task-181"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	leader := &models.TeamMember{ID: 120, TeamID: 31, MemberKey: "leader", Role: "leader", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	developer := &models.TeamMember{ID: 121, TeamID: 31, MemberKey: "developer", Role: "senior-developer", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": leader, "developer": developer},
	}
	service := &teamService{repo: repo}
	assignmentJSON, err := json.Marshal(map[string]interface{}{
		"event":              "reply",
		"leaderDispatchOnly": true,
		"assignmentId":       "assignment-developer-001",
		"memberId":           "leader",
		"rootTaskId":         messageID,
		"status":             "dispatched",
		"collaborationStep": map[string]interface{}{
			"type":       "assignment",
			"status":     "dispatched",
			"actor":      "leader",
			"target":     "developer",
			"rootTaskId": messageID,
			"content":    "Please answer with one flower.",
		},
	})
	if err != nil {
		t.Fatalf("marshal assignment: %v", err)
	}
	if err := service.projectTeamEvent(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID:     "1781171178681-0",
		Fields: map[string]string{"payload": string(assignmentJSON)},
	}); err != nil {
		t.Fatalf("project assignment: %v", err)
	}
	resultJSON, err := json.Marshal(map[string]interface{}{
		"event":                "outbound",
		"assignmentResultOnly": true,
		"assignmentId":         "runtime-random-assignment-id",
		"memberId":             "developer",
		"from":                 "developer",
		"to":                   "leader",
		"rootTaskId":           messageID,
		"status":               "succeeded",
		"text":                 "洋牡丹：受欢迎、魅力四射。",
		"collaborationStep": map[string]interface{}{
			"type":       "result",
			"status":     "succeeded",
			"actor":      "developer",
			"target":     "leader",
			"rootTaskId": messageID,
			"content":    "洋牡丹：受欢迎、魅力四射。",
		},
	})
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if err := service.projectTeamEvent(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID:     "1781171178681-1",
		Fields: map[string]string{"payload": string(resultJSON)},
	}); err != nil {
		t.Fatalf("project result: %v", err)
	}
	if len(repo.workItems) != 1 {
		t.Fatalf("dispatch and result should share one member lane, got %#v", repo.workItems)
	}
	item := repo.workItems[0]
	if item.WorkID != "assignment-developer-001" || item.Status != models.TeamTaskStatusSucceeded {
		t.Fatalf("expected succeeded canonical developer assignment, got %#v", item)
	}
	if item.ResultJSON == nil || !strings.Contains(*item.ResultJSON, "洋牡丹") {
		t.Fatalf("expected full worker result in lane payload, got %#v", item.ResultJSON)
	}
	if len(repo.createdEvents) < 3 || repo.createdEvents[len(repo.createdEvents)-1].EventType != "member_result_confirmed" {
		t.Fatalf("expected a structured leader notification for worker result, got %#v", repo.createdEvents)
	}
}

func TestProjectTeamEventLeaderMediatedWorkerProgressAndResultShareMemberLane(t *testing.T) {
	taskID := 184
	messageID := "team-31-task-184"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	leader := &models.TeamMember{ID: 120, TeamID: 31, MemberKey: "leader", Role: "leader", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	pm := &models.TeamMember{ID: 121, TeamID: 31, MemberKey: "pm", Role: "product-manager", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": leader, "pm": pm},
	}
	service := &teamService{repo: repo}
	progressJSON, err := json.Marshal(map[string]interface{}{
		"event":      "progress",
		"memberId":   "pm",
		"from":       "pm",
		"to":         "leader",
		"rootTaskId": messageID,
		"workId":     "w1-pm-flower",
		"status":     "running",
		"progress":   50,
		"text":       "Working on PM flower choice.",
		"collaborationStep": map[string]interface{}{
			"type":       "progress",
			"status":     "running",
			"actor":      "pm",
			"target":     "leader",
			"rootTaskId": messageID,
			"content":    "Working on PM flower choice.",
		},
	})
	if err != nil {
		t.Fatalf("marshal progress: %v", err)
	}
	if err := service.projectTeamEvent(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID:     "1781171178683-0",
		Fields: map[string]string{"payload": string(progressJSON)},
	}); err != nil {
		t.Fatalf("project progress: %v", err)
	}
	if len(repo.workItems) != 1 || repo.workItems[0].WorkID != "w1-pm-flower" || repo.workItems[0].Status != models.TeamTaskStatusRunning {
		t.Fatalf("expected running PM assignment after progress, got %#v", repo.workItems)
	}
	resultJSON, err := json.Marshal(map[string]interface{}{
		"event":                "outbound",
		"assignmentResultOnly": true,
		"assignmentId":         "runtime-random-assignment-id",
		"memberId":             "pm",
		"from":                 "pm",
		"to":                   "leader",
		"rootTaskId":           messageID,
		"status":               "succeeded",
		"text":                 "PM flower: Sunflower.",
		"collaborationStep": map[string]interface{}{
			"type":       "result",
			"status":     "succeeded",
			"actor":      "pm",
			"target":     "leader",
			"rootTaskId": messageID,
			"content":    "PM flower: Sunflower.",
		},
	})
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if err := service.projectTeamEvent(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID:     "1781171178683-1",
		Fields: map[string]string{"payload": string(resultJSON)},
	}); err != nil {
		t.Fatalf("project result: %v", err)
	}
	if len(repo.workItems) != 1 {
		t.Fatalf("progress and result should share one member lane, got %#v", repo.workItems)
	}
	item := repo.workItems[0]
	if item.WorkID != "w1-pm-flower" || item.Status != models.TeamTaskStatusSucceeded {
		t.Fatalf("expected succeeded PM assignment, got %#v", item)
	}
	if item.ResultJSON == nil || !strings.Contains(*item.ResultJSON, "Sunflower") {
		t.Fatalf("expected worker result stored in pm lane, got %#v", item.ResultJSON)
	}
}

func TestProjectTeamEventLeaderMediatedDuplicateWorkerResultDoesNotNotifyTwice(t *testing.T) {
	taskID := 185
	messageID := "team-31-task-185"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	leader := &models.TeamMember{ID: 120, TeamID: 31, MemberKey: "leader", Role: "leader", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	pm := &models.TeamMember{ID: 121, TeamID: 31, MemberKey: "pm", Role: "product-manager", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	confirmedPayload := `{"event":"member_result_confirmed","from":"pm","memberId":"pm","rootTaskId":"team-31-task-185","text":"PM flower: Sunflower."}`
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": leader, "pm": pm},
		createdEvents: []models.TeamEvent{{
			TeamID:      31,
			TaskID:      &taskID,
			EventType:   "member_result_confirmed",
			PayloadJSON: &confirmedPayload,
		}},
	}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":                "outbound",
		"assignmentResultOnly": true,
		"memberId":             "pm",
		"from":                 "pm",
		"to":                   "leader",
		"rootTaskId":           messageID,
		"status":               "succeeded",
		"text":                 "PM flower: Sunflower.",
		"collaborationStep": map[string]interface{}{
			"type":       "result",
			"status":     "succeeded",
			"actor":      "pm",
			"target":     "leader",
			"rootTaskId": messageID,
			"content":    "PM flower: Sunflower.",
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	service := &teamService{repo: repo}
	if err := service.projectTeamEvent(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID:     "1781171178684-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("project duplicate result: %v", err)
	}
	confirmations := 0
	for _, event := range repo.createdEvents {
		if event.EventType == "member_result_confirmed" {
			confirmations++
		}
	}
	if confirmations != 1 {
		t.Fatalf("duplicate worker result must not create another leader notification, got %d events: %#v", confirmations, repo.createdEvents)
	}
}

func TestProjectTeamEventLeaderMediatedChangedWorkerResultNotifiesAgain(t *testing.T) {
	taskID := 186
	messageID := "team-31-task-186"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	leader := &models.TeamMember{ID: 120, TeamID: 31, MemberKey: "leader", Role: "leader", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	reviewer := &models.TeamMember{ID: 121, TeamID: 31, MemberKey: "reviewer", Role: "qa-engineer", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	confirmedPayload := `{"event":"member_result_confirmed","from":"reviewer","memberId":"reviewer","rootTaskId":"team-31-task-186","assignmentId":"review-current","workId":"review-current","text":"Reviewer verdict: FAIL"}`
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": leader, "reviewer": reviewer},
		createdEvents: []models.TeamEvent{{
			TeamID:      31,
			TaskID:      &taskID,
			EventType:   "member_result_confirmed",
			PayloadJSON: &confirmedPayload,
		}},
	}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":                "outbound",
		"assignmentResultOnly": true,
		"assignmentId":         "review-current",
		"memberId":             "reviewer",
		"from":                 "reviewer",
		"to":                   "leader",
		"rootTaskId":           messageID,
		"status":               "succeeded",
		"text":                 "Reviewer verdict: PASS",
		"collaborationStep": map[string]interface{}{
			"type":       "result",
			"status":     "succeeded",
			"actor":      "reviewer",
			"target":     "leader",
			"rootTaskId": messageID,
			"content":    "Reviewer verdict: PASS",
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	service := &teamService{repo: repo}
	if err := service.projectTeamEvent(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID:     "1781171178685-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("project changed result: %v", err)
	}
	confirmations := 0
	for _, event := range repo.createdEvents {
		if event.EventType == "member_result_confirmed" {
			confirmations++
		}
	}
	if confirmations != 2 {
		t.Fatalf("changed worker result must create a fresh leader notification, got %d events: %#v", confirmations, repo.createdEvents)
	}
	if len(repo.outboxRows) != 1 {
		t.Fatalf("changed worker result must be delivered to leader once, got outbox %#v", repo.outboxRows)
	}
	if len(repo.workItems) != 1 || repo.workItems[0].ResultJSON == nil || !strings.Contains(*repo.workItems[0].ResultJSON, "PASS") {
		t.Fatalf("changed worker result must refresh current work item, got %#v", repo.workItems)
	}
}

func TestProjectTeamEventLeaderDispatchRefreshesTerminalCurrentLane(t *testing.T) {
	taskID := 187
	messageID := "team-31-task-187"
	now := time.Now().UTC()
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      now.Add(-time.Minute),
	}
	leader := &models.TeamMember{ID: 120, TeamID: 31, MemberKey: "leader", Role: "leader", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	reviewer := &models.TeamMember{ID: 121, TeamID: 31, MemberKey: "reviewer", Role: "qa-engineer", Status: models.TeamMemberStatusIdle, Availability: models.TeamMemberAvailabilityIdle}
	oldResult := `{"summary":"Reviewer verdict: FAIL"}`
	oldFinishedAt := now.Add(-2 * time.Minute)
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": leader, "reviewer": reviewer},
		workItems: []models.TeamWorkItem{{
			TeamID:        31,
			RootTaskID:    taskID,
			WorkID:        "review-current",
			AssignmentID:  stringPtr("review-current"),
			OwnerMemberID: &reviewer.ID,
			Title:         "reviewer delivers result",
			Status:        models.TeamTaskStatusFailed,
			ResultJSON:    &oldResult,
			FinishedAt:    &oldFinishedAt,
			UpdatedAt:     now.Add(-2 * time.Minute),
		}},
	}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":              "reply",
		"leaderDispatchOnly": true,
		"assignmentId":       "review-current",
		"memberId":           "leader",
		"from":               "leader",
		"to":                 "reviewer",
		"rootTaskId":         messageID,
		"status":             "dispatched",
		"text":               "Please re-check after the calculator fix.",
		"collaborationStep": map[string]interface{}{
			"type":       "assignment",
			"status":     "dispatched",
			"actor":      "leader",
			"target":     "reviewer",
			"rootTaskId": messageID,
			"content":    "Please re-check after the calculator fix.",
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	service := &teamService{repo: repo}
	if err := service.projectTeamEvent(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID:     "1781171178686-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("project redispatch: %v", err)
	}
	if len(repo.workItems) != 1 {
		t.Fatalf("redispatch should refresh one current lane, got %#v", repo.workItems)
	}
	item := repo.workItems[0]
	if item.Status != models.TeamTaskStatusDispatched || item.ResultJSON != nil || item.FinishedAt != nil {
		t.Fatalf("redispatch must reopen current lane and clear old terminal result, got %#v", item)
	}
}

func TestLeaderMediatedRootCompletionReadyCoalescesSameOwnerWorkItems(t *testing.T) {
	taskID := 182
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      "team-31-task-182",
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	leader := &models.TeamMember{ID: 120, TeamID: 31, MemberKey: "leader", Role: "leader", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	developer := &models.TeamMember{ID: 121, TeamID: 31, MemberKey: "developer", Role: "senior-developer", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	repo := &teamRepositoryStub{
		workItems: []models.TeamWorkItem{
			{TeamID: 31, RootTaskID: taskID, WorkID: "assignment-developer-legacy", OwnerMemberID: &developer.ID, Status: models.TeamTaskStatusDispatched, UpdatedAt: time.Now().UTC()},
			{TeamID: 31, RootTaskID: taskID, WorkID: "member-developer", OwnerMemberID: &developer.ID, Status: models.TeamTaskStatusSucceeded, UpdatedAt: time.Now().UTC()},
		},
	}
	service := &teamService{repo: repo}
	ready, err := service.leaderMediatedRootCompletionReady(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, task, leader)
	if err != nil {
		t.Fatalf("leaderMediatedRootCompletionReady returned error: %v", err)
	}
	if !ready {
		t.Fatalf("same-owner legacy dispatch card must not block root completion after member result")
	}
}

func TestProjectTeamEventAssociatesCurrentTaskReplyWithoutCompletingIt(t *testing.T) {
	taskID := 72
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 121,
		MessageID:      "team-31-task-72",
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	worker := &models.TeamMember{
		ID:            121,
		TeamID:        31,
		MemberKey:     "worker",
		Status:        models.TeamMemberStatusBusy,
		CurrentTaskID: &taskID,
		Availability:  models.TeamMemberAvailabilityBusy,
	}
	repo := &teamRepositoryStub{
		tasksByID:    map[int]*models.TeamTask{taskID: task},
		membersByKey: map[string]*models.TeamMember{"worker": worker},
	}
	service := &teamService{repo: repo}
	payload := map[string]interface{}{
		"event":    "reply",
		"memberId": "worker",
		"summary":  "Weather report ready",
		"text": strings.Join([]string{
			"# Melbourne weather",
			"Current conditions: partly cloudy, 16C, south wind 27 km/h, humidity 82%.",
			"Forecast: showers today, cooler tomorrow, rain on Saturday.",
		}, "\n"),
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	err = service.projectTeamEvent(&models.Team{ID: 31}, nil, redisStreamMessage{
		ID:     "1781171178660-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	})
	if err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}

	if repo.updatedTask != nil && (repo.updatedTask.Status == models.TeamTaskStatusSucceeded || repo.updatedTask.FinishedAt != nil) {
		t.Fatalf("current task reply must be associated but remain non-terminal, got %#v", repo.updatedTask)
	}
	if repo.updatedMember != nil && repo.updatedMember.Progress == 100 {
		t.Fatalf("reply without explicit completion must not complete worker state, got %#v", repo.updatedMember)
	}
	if len(repo.createdEvents) != 1 || repo.createdEvents[0].EventType != "reply" || repo.createdEvents[0].TaskID == nil || *repo.createdEvents[0].TaskID != taskID {
		t.Fatalf("expected reply event linked to current task, got %#v", repo.createdEvents)
	}
}

func TestProjectTeamEventTreatsExplicitCompletionToolReplyAsTaskCompleted(t *testing.T) {
	taskID := 67
	messageID := "team-31-bootstrap-introduction"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	member := &models.TeamMember{
		ID:            120,
		TeamID:        31,
		MemberKey:     "leader",
		Status:        models.TeamMemberStatusBusy,
		CurrentTaskID: &taskID,
		Availability:  models.TeamMemberAvailabilityBusy,
	}
	repo := &teamRepositoryStub{
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": member},
	}
	service := &teamService{repo: repo}
	payload := map[string]interface{}{
		"event":          "reply",
		"messageId":      messageID,
		"memberId":       "leader",
		"taskId":         "team-31-task-67",
		"final":          true,
		"summary":        "Team report ready",
		"resultMarkdown": "Full report",
		"text":           "Full report",
		"toolCall": map[string]interface{}{
			"name": teamTaskCompletionTool,
		},
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	err = service.projectTeamEvent(&models.Team{ID: 31}, nil, redisStreamMessage{
		ID:     "1781171178655-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	})
	if err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}

	if repo.updatedTask == nil || repo.updatedTask.Status != models.TeamTaskStatusSucceeded {
		t.Fatalf("expected explicit completion tool reply to mark task succeeded, got %#v", repo.updatedTask)
	}
	if repo.updatedTask.ResultJSON == nil || !strings.Contains(*repo.updatedTask.ResultJSON, "Full report") {
		t.Fatalf("expected reply payload to become task result, got %#v", repo.updatedTask.ResultJSON)
	}
	if repo.updatedMember == nil || repo.updatedMember.Status != models.TeamMemberStatusIdle || repo.updatedMember.Availability != models.TeamMemberAvailabilityIdle {
		t.Fatalf("expected member to become idle, got %#v", repo.updatedMember)
	}
	if len(repo.createdEvents) != 1 || repo.createdEvents[0].EventType != "task_completed" {
		t.Fatalf("expected stored event type task_completed, got %#v", repo.createdEvents)
	}
	var stored map[string]interface{}
	if err := json.Unmarshal([]byte(*repo.createdEvents[0].PayloadJSON), &stored); err != nil {
		t.Fatalf("decode stored event payload: %v", err)
	}
	step, ok := stored["collaborationStep"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected collaboration step in stored event, got %#v", stored)
	}
	if got := step["summary"]; got != "Team report ready" {
		t.Fatalf("expected collaboration step summary to stay compact, got %#v", got)
	}
	if got := step["content"]; got != "Full report" {
		t.Fatalf("expected collaboration step content to preserve full resultMarkdown, got %#v", got)
	}
}

func TestProjectTeamEventCompletionReportMentioningDispatchStillClosesTask(t *testing.T) {
	taskID := 69
	messageID := "team-31-bootstrap-introduction"
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	member := &models.TeamMember{
		ID:            120,
		TeamID:        31,
		MemberKey:     "leader",
		Status:        models.TeamMemberStatusBusy,
		CurrentTaskID: &taskID,
		Availability:  models.TeamMemberAvailabilityBusy,
	}
	repo := &teamRepositoryStub{
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": member},
	}
	service := &teamService{repo: repo}
	report := strings.Join([]string{
		"# Team 31 introduction",
		"",
		"## Collaboration",
		"Leader uses team_send for task dispatch to each member, then waits for worker evidence before final synthesis.",
		"PM, Designer, and Architect each receive scoped assignments and return concrete artifacts through the shared workspace.",
		"Task dispatch is part of this completed report, not a dispatch-only acknowledgement.",
	}, "\n")
	payload := map[string]interface{}{
		"event":              "task_completed",
		"messageId":          messageID,
		"memberId":           "leader",
		"taskId":             "team-31-task-69",
		"status":             "succeeded",
		"summary":            "Completed team introduction.",
		"resultMarkdown":     report,
		"explicitCompletion": true,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	err = service.projectTeamEvent(&models.Team{ID: 31}, nil, redisStreamMessage{
		ID:     "1781171178657-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	})
	if err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}

	if repo.updatedTask == nil || repo.updatedTask.Status != models.TeamTaskStatusSucceeded {
		t.Fatalf("expected completion report mentioning dispatch to close task, got %#v", repo.updatedTask)
	}
	if repo.updatedTask.ResultJSON == nil || !strings.Contains(*repo.updatedTask.ResultJSON, "Task dispatch is part of this completed report") {
		t.Fatalf("expected full report to be stored on task result, got %#v", repo.updatedTask.ResultJSON)
	}
	var stored map[string]interface{}
	if err := json.Unmarshal([]byte(*repo.createdEvents[0].PayloadJSON), &stored); err != nil {
		t.Fatalf("decode stored event payload: %v", err)
	}
	step := stored["collaborationStep"].(map[string]interface{})
	if got, _ := step["content"].(string); !strings.Contains(got, "Leader uses team_send") {
		t.Fatalf("expected collaboration step content to preserve report, got %#v", got)
	}
}

func TestProjectTeamEventProtocolV2BootstrapCompletionMentioningTeamSendClosesTask(t *testing.T) {
	teamID := 40
	taskID := 69
	messageID := "team-40-bootstrap-introduction"
	workspaceRoot := t.TempDir()
	resultDir := filepath.Join(workspaceRoot, "teams", "user-1", "team-40-shared", "results", "team-40-task-69")
	if err := os.MkdirAll(resultDir, 0o775); err != nil {
		t.Fatalf("create result dir: %v", err)
	}
	for _, name := range []string{"team-introduction.md", "result.md"} {
		if err := os.WriteFile(filepath.Join(resultDir, name), []byte("team introduction"), 0o664); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         teamID,
		TargetMemberID: 145,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	leader := &models.TeamMember{
		ID:            145,
		TeamID:        teamID,
		MemberKey:     "leader",
		Role:          "leader",
		Status:        models.TeamMemberStatusBusy,
		CurrentTaskID: &taskID,
		Availability:  models.TeamMemberAvailabilityBusy,
	}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": leader},
	}
	service := &teamService{repo: repo, runtimeWorkspaceRoot: workspaceRoot}
	report := strings.Join([]string{
		"# Product Discovery Team 23 - Bootstrap complete",
		"",
		"## Collaboration",
		"- Mode: Leader Mediated; all user work enters through the Leader.",
		"- Communication layer: Redis Streams with team_send, events, inbox, presence, and DLQ.",
		"- Worker pipeline: Worker completes, writes result, calls team_complete_task, then reports back.",
		"",
		"Detailed report: /team/results/team-40-task-69/team-introduction.md",
	}, "\n")
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"v":                   1,
		"protocolVersion":     2,
		"eventId":             "evt_team40_completion",
		"event":               "task_completed",
		"type":                "task_completed",
		"memberId":            "leader",
		"messageId":           messageID,
		"taskId":              "team-40-task-69",
		"rootTaskId":          "team-40-task-69",
		"rootMessageId":       messageID,
		"status":              "succeeded",
		"runtimeStatus":       "succeeded",
		"availability":        "idle",
		"completionId":        "completion:40:team-40-task-69:leader",
		"completionSource":    teamTaskCompletionTool,
		"explicitCompletion":  true,
		"summary":             "Bootstrap report completed.",
		"result":              report,
		"resultMarkdown":      report,
		"artifactRefs":        []interface{}{"/team/results/team-40-task-69/team-introduction.md", "/team/results/team-40-task-69/result.md"},
		"sourceMessageId":     messageID,
		"completionMessageId": "completion:40:team-40-task-69:leader",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	if err := service.projectTeamEvent(&models.Team{ID: teamID, UserID: 1, SharedMountPath: "/team", CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID:     "1783407850912-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}

	if repo.updatedTask == nil || repo.updatedTask.Status != models.TeamTaskStatusSucceeded || repo.updatedTask.FinishedAt == nil {
		t.Fatalf("protocol v2 completion report should close the root task, got %#v", repo.updatedTask)
	}
	if len(repo.createdEvents) != 1 || repo.createdEvents[0].EventType != "task_completed" {
		t.Fatalf("expected stored task_completed event, got %#v", repo.createdEvents)
	}
	stored := teamEventPayloadMap(repo.createdEvents[0])
	if eventBool(stored, "leaderDispatchOnly") {
		t.Fatalf("explicit completion must not be downgraded to leader dispatch, got %#v", stored)
	}
	step, ok := stored["collaborationStep"].(map[string]interface{})
	if !ok || step["type"] != "result" || step["status"] != models.TeamTaskStatusSucceeded {
		t.Fatalf("expected result collaboration step for explicit completion, got %#v", step)
	}
}

func TestActiveTeamMembersFiltersDeletedMembers(t *testing.T) {
	members := activeTeamMembers([]models.TeamMember{
		{MemberKey: "leader", Status: models.TeamMemberStatusIdle},
		{MemberKey: "old", Status: models.TeamMemberStatusDeleted},
		{MemberKey: "gone", Status: models.TeamMemberStatusDeleting},
	})
	if len(members) != 1 || members[0].MemberKey != "leader" {
		t.Fatalf("unexpected active members: %#v", members)
	}
}

func TestDeletedTeamNameReleasesUniqueName(t *testing.T) {
	name := deletedTeamName("DeepResearch", 42)
	if name != "DeepResearch__deleted_42" {
		t.Fatalf("unexpected deleted Team name: %q", name)
	}
	if again := deletedTeamName(name, 42); again != name {
		t.Fatalf("deleted Team name should be idempotent, got %q", again)
	}
}

func TestTeamTaskStaleTimeoutUsesEnvironment(t *testing.T) {
	t.Setenv("CLAWMANAGER_TEAM_TASK_STALE_SECONDS", "60")
	if got := teamTaskStaleTimeout(); got != time.Minute {
		t.Fatalf("expected one minute stale timeout, got %s", got)
	}

	t.Setenv("CLAWMANAGER_TEAM_TASK_STALE_SECONDS", "0")
	if got := teamTaskStaleTimeout(); got != 0 {
		t.Fatalf("expected disabled stale timeout, got %s", got)
	}
}

func TestApplyTeamMemberRuntimeProjectionSetsBlockedAvailability(t *testing.T) {
	member := &models.TeamMember{Availability: models.TeamMemberAvailabilityBusy}
	payload := map[string]interface{}{
		"availability":  "blocked",
		"lastSummary":   "Task failed: LLM request failed: network connection error.",
		"currentTaskId": "task_cb1062da-dff2-46ff-836f-86490583d944",
		"currentIntent": "weather_query_beijing",
	}

	applyTeamMemberRuntimeProjection(member, payload, "status")

	if member.Availability != models.TeamMemberAvailabilityBlocked {
		t.Fatalf("expected blocked availability, got %q", member.Availability)
	}
	if member.LastSummary == nil || !strings.Contains(*member.LastSummary, "LLM request failed") {
		t.Fatalf("expected last summary projection, got %#v", member.LastSummary)
	}
	if member.RuntimeTaskID == nil || *member.RuntimeTaskID != "task_cb1062da-dff2-46ff-836f-86490583d944" {
		t.Fatalf("expected runtime task id projection, got %#v", member.RuntimeTaskID)
	}
}

func TestApplyTeamMemberRuntimeProjectionClearsStaleBlockedOnCompletion(t *testing.T) {
	reason := "previous task failed"
	member := &models.TeamMember{
		Availability:  models.TeamMemberAvailabilityBlocked,
		BlockedReason: &reason,
	}
	payload := map[string]interface{}{
		"lastSummary":   "Redis Team task processing completed",
		"currentTaskId": "task_001",
	}

	applyTeamMemberRuntimeProjection(member, payload, "task_completed")

	if member.Availability != models.TeamMemberAvailabilityIdle {
		t.Fatalf("expected idle availability after task completion, got %q", member.Availability)
	}
	if member.BlockedReason != nil {
		t.Fatalf("expected stale blocked reason to be cleared, got %#v", *member.BlockedReason)
	}
}

func TestMergeMissingEventFieldsEnrichesOutboundPayload(t *testing.T) {
	base := map[string]interface{}{
		"event":     "outbound",
		"messageId": "msg_123",
		"from":      "leader",
		"to":        "worker",
	}
	extra := map[string]interface{}{
		"messageId": "msg_123",
		"title":     "Check date",
		"text":      "Check today's date and send the result back.",
		"metadata": map[string]interface{}{
			"prompt": "metadata prompt should also be available",
		},
	}

	merged := mergeMissingEventFields(base, extra)

	if merged["messageId"] != "msg_123" || merged["from"] != "leader" || merged["to"] != "worker" {
		t.Fatalf("base fields should be preserved, got %#v", merged)
	}
	if merged["title"] != "Check date" || merged["text"] == "" {
		t.Fatalf("expected outbound payload to be enriched with title/text, got %#v", merged)
	}
	if merged["prompt"] != "metadata prompt should also be available" {
		t.Fatalf("expected metadata prompt to be merged, got %#v", merged)
	}
	if !teamEventHasBody(merged) {
		t.Fatalf("expected enriched event to have displayable body: %#v", merged)
	}
}

func TestCollectTeamArtifactReferencesStopsAtMarkdownAndJSONDelimiters(t *testing.T) {
	payload := map[string]interface{}{
		"resultMarkdown": "Report: `/team/results/team-22-task-40/team-introduction-report.md`.",
		"raw":            `{"resultMarkdown":"/team/results/team-22-task-40/team-introduction-report.md\\n","artifactRefs":["/team/results/team-22-task-40/result.md"]}`,
		"directory":      "Detailed report directory: `/team/results/team-22-task-40/`.",
		"artifactRefs": []interface{}{
			"/team/results/team-22-task-40/team-introduction-report.md",
			"/team/results/team-22-task-40/result.md",
		},
	}

	got := collectTeamArtifactReferences(payload)
	want := map[string]bool{
		"/team/results/team-22-task-40/team-introduction-report.md": true,
		"/team/results/team-22-task-40/result.md":                   true,
	}
	if len(got) != len(want) {
		t.Fatalf("artifact refs = %#v, want exactly %#v", got, want)
	}
	for _, ref := range got {
		if !want[ref] {
			t.Fatalf("unexpected malformed artifact ref %q in %#v", ref, got)
		}
	}
}

func TestProjectTeamEventTreatsLegacyBootstrapReportAsCompletion(t *testing.T) {
	teamID := 31
	taskID := 67
	messageID := "team-31-bootstrap-introduction"
	workspaceRoot := t.TempDir()
	resultDir := filepath.Join(workspaceRoot, "teams", "user-1", "team-31-shared", "results", "team-31-task-67")
	if err := os.MkdirAll(resultDir, 0o775); err != nil {
		t.Fatalf("create result dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(resultDir, "team-introduction.md"), []byte("team introduction"), 0o664); err != nil {
		t.Fatalf("write result: %v", err)
	}
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         teamID,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	leader := &models.TeamMember{
		ID:            120,
		TeamID:        teamID,
		MemberKey:     "leader",
		Role:          "leader",
		Status:        models.TeamMemberStatusBusy,
		CurrentTaskID: &taskID,
		Availability:  models.TeamMemberAvailabilityBusy,
	}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": leader},
	}
	service := &teamService{repo: repo, runtimeWorkspaceRoot: workspaceRoot}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":     "reply",
		"memberId":  "leader",
		"messageId": messageID,
		"text": "Team 31 bootstrap introduction completed. The Team roster, runtime status, collaboration mode, " +
			"capability boundaries, Redis Streams team_send mechanism, shared workspace rules, and available methods " +
			"have been delivered. Detailed report: /team/results/team-31-task-67/team-introduction.md",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	if err := service.projectTeamEvent(&models.Team{ID: teamID, UserID: 1, SharedMountPath: "/team", CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID: "1781171178692-0", Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}
	if repo.updatedTask == nil || repo.updatedTask.Status != models.TeamTaskStatusSucceeded || repo.updatedTask.FinishedAt == nil {
		t.Fatalf("legacy bootstrap report should close the root task, got %#v", repo.updatedTask)
	}
	if len(repo.createdEvents) != 1 || repo.createdEvents[0].EventType != "task_completed" {
		t.Fatalf("expected one normalized task_completed event, got %#v", repo.createdEvents)
	}
	payload := teamEventPayloadMap(repo.createdEvents[0])
	if !eventBool(payload, "legacyCompletionCandidate") || eventString(payload, "completionSource") != "legacy_runtime_reply" {
		t.Fatalf("expected legacy completion markers, got %#v", payload)
	}
}

func TestProjectTeamEventTreatsServerBootstrapToolReplyAsCompletion(t *testing.T) {
	teamID := 30
	taskID := 55
	messageID := "team-30-bootstrap-introduction"
	workspaceRoot := t.TempDir()
	resultDir := filepath.Join(workspaceRoot, "teams", "user-1", "team-30-shared", "results", "team-30-task-55")
	if err := os.MkdirAll(resultDir, 0o775); err != nil {
		t.Fatalf("create result dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(resultDir, "team-introduction.md"), []byte("team introduction"), 0o664); err != nil {
		t.Fatalf("write result: %v", err)
	}
	task := &models.TeamTask{ID: taskID, TeamID: teamID, TargetMemberID: 120, MessageID: messageID, Status: models.TeamTaskStatusRunning, UpdatedAt: time.Now().UTC()}
	leader := &models.TeamMember{ID: 120, TeamID: teamID, MemberKey: "leader", Role: "leader", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	designer := &models.TeamMember{ID: 121, TeamID: teamID, MemberKey: "ui-designer", Role: "ui-ux-designer", Status: models.TeamMemberStatusIdle, Availability: models.TeamMemberAvailabilityIdle}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": leader, "ui-designer": designer},
	}
	service := &teamService{repo: repo, runtimeWorkspaceRoot: workspaceRoot}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":            "reply",
		"memberId":         "leader",
		"messageId":        messageID,
		"completionSource": teamTaskCompletionTool,
		"status":           "dispatched",
		"summary": "Bootstrap 团队介绍任务完成。已编制 product-discovery-team (Team 30) 完整团队画像，包含 4 名成员的角色职责、运行时状态、" +
			"技术能力边界，以及 leader_mediated 协作模式下的任务流转、消息同步、上下文共享与可调用方法说明。详细报告已落盘至 /team/results/team-30-task-55/team-introduction.md。",
		"resultMarkdown": "# Product Discovery Team (team-30) — Bootstrap 团队介绍\n\n" +
			"## 团队架构\n\n**协作模式**：`leader_mediated`。Leader 是唯一入口，Worker 之间禁止直连。\n\n" +
			"| 成员 | 角色 | 运行时 |\n|------|------|--------|\n| discovery-lead | Leader | OpenClaw Pro |\n| ui-designer | UI Designer | Hermes Pro |\n\n" +
			"详细报告：/team/results/team-30-task-55/team-introduction.md",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	if err := service.projectTeamEvent(&models.Team{ID: teamID, UserID: 1, SharedMountPath: "/team", CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID: "1781171178693-0", Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}
	if repo.updatedTask == nil || repo.updatedTask.Status != models.TeamTaskStatusSucceeded || repo.updatedTask.FinishedAt == nil {
		t.Fatalf("server bootstrap tool reply should close the root task, got %#v", repo.updatedTask)
	}
	if len(repo.workItems) != 0 {
		t.Fatalf("server bootstrap tool reply must not create synthetic assignments, got %#v", repo.workItems)
	}
	if len(repo.createdEvents) != 1 || repo.createdEvents[0].EventType != "task_completed" {
		t.Fatalf("expected one normalized task_completed event, got %#v", repo.createdEvents)
	}
}

func TestProjectTeamEventDoesNotCreateBootstrapAssignmentWorkItem(t *testing.T) {
	teamID := 30
	taskID := 55
	messageID := "team-30-bootstrap-introduction"
	task := &models.TeamTask{ID: taskID, TeamID: teamID, TargetMemberID: 120, MessageID: messageID, Status: models.TeamTaskStatusRunning, UpdatedAt: time.Now().UTC()}
	leader := &models.TeamMember{ID: 120, TeamID: teamID, MemberKey: "leader", Role: "leader", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	designer := &models.TeamMember{ID: 121, TeamID: teamID, MemberKey: "ui-designer", Role: "ui-ux-designer", Status: models.TeamMemberStatusIdle, Availability: models.TeamMemberAvailabilityIdle}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": leader, "ui-designer": designer},
	}
	service := &teamService{repo: repo, runtimeWorkspaceRoot: t.TempDir()}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":     "reply",
		"memberId":  "leader",
		"messageId": messageID,
		"text":      "任务分派：请 ui-designer 补充团队能力边界说明。",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	if err := service.projectTeamEvent(&models.Team{ID: teamID, UserID: 1, SharedMountPath: "/team", CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID: "1781171178694-0", Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}
	if len(repo.workItems) != 0 {
		t.Fatalf("bootstrap/control-plane replies must not create work items, got %#v", repo.workItems)
	}
}

func TestProjectTeamEventBootstrapCompletionIgnoresStaleSyntheticWorkItem(t *testing.T) {
	teamID := 30
	taskID := 55
	messageID := "team-30-bootstrap-introduction"
	workspaceRoot := t.TempDir()
	resultDir := filepath.Join(workspaceRoot, "teams", "user-1", "team-30-shared", "results", "team-30-task-55")
	if err := os.MkdirAll(resultDir, 0o775); err != nil {
		t.Fatalf("create result dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(resultDir, "team-introduction.md"), []byte("full report"), 0o664); err != nil {
		t.Fatalf("write result: %v", err)
	}
	task := &models.TeamTask{ID: taskID, TeamID: teamID, TargetMemberID: 120, MessageID: messageID, Status: models.TeamTaskStatusRunning, UpdatedAt: time.Now().UTC()}
	leader := &models.TeamMember{ID: 120, TeamID: teamID, MemberKey: "leader", Role: "leader", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	designer := &models.TeamMember{ID: 121, TeamID: teamID, MemberKey: "ui-designer", Role: "ui-ux-designer", Status: models.TeamMemberStatusIdle, Availability: models.TeamMemberAvailabilityIdle}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": leader, "ui-designer": designer},
		workItems: []models.TeamWorkItem{{
			TeamID:        teamID,
			RootTaskID:    taskID,
			WorkID:        "member-ui-designer",
			OwnerMemberID: &designer.ID,
			Title:         "Assign to ui-designer",
			Status:        models.TeamTaskStatusDispatched,
			UpdatedAt:     time.Now().UTC(),
		}},
	}
	service := &teamService{repo: repo, runtimeWorkspaceRoot: workspaceRoot}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":              "task_completed",
		"memberId":           "leader",
		"messageId":          messageID,
		"status":             "succeeded",
		"summary":            "Bootstrap complete.",
		"resultMarkdown":     "Bootstrap 完成。详见 /team/results/team-30-task-55/team-introduction.md",
		"artifactRefs":       []string{"/team/results/team-30-task-55/team-introduction.md"},
		"explicitCompletion": true,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	if err := service.projectTeamEvent(&models.Team{ID: teamID, UserID: 1, SharedMountPath: "/team", CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID: "1781171178695-0", Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}
	if repo.updatedTask == nil || repo.updatedTask.Status != models.TeamTaskStatusSucceeded {
		t.Fatalf("bootstrap completion must ignore stale synthetic work items, got %#v", repo.updatedTask)
	}
}

func TestProjectTeamEventDoesNotTreatLegacyAckAsCompletion(t *testing.T) {
	teamID := 31
	taskID := 68
	messageID := "team-31-bootstrap-introduction"
	task := &models.TeamTask{ID: taskID, TeamID: teamID, TargetMemberID: 120, MessageID: messageID, Status: models.TeamTaskStatusRunning, UpdatedAt: time.Now().UTC()}
	leader := &models.TeamMember{ID: 120, TeamID: teamID, MemberKey: "leader", Role: "leader", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": leader},
	}
	service := &teamService{repo: repo, runtimeWorkspaceRoot: t.TempDir()}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":     "reply",
		"memberId":  "leader",
		"messageId": messageID,
		"text":      "Redis Team task completed",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	if err := service.projectTeamEvent(&models.Team{ID: teamID, UserID: 1, SharedMountPath: "/team", CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID: "1781171178693-0", Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}
	if repo.updatedTask != nil {
		t.Fatalf("ack-only legacy reply must not close the root task, got %#v", repo.updatedTask)
	}
	if len(repo.createdEvents) != 1 || repo.createdEvents[0].EventType != "reply" {
		t.Fatalf("expected ack to stay as reply, got %#v", repo.createdEvents)
	}
}

func TestProjectTeamEventAcceptsExistingMarkdownArtifactReference(t *testing.T) {
	teamID := 22
	taskID := 40
	messageID := "team-22-bootstrap-introduction"
	workspaceRoot := t.TempDir()
	resultDir := filepath.Join(workspaceRoot, "teams", "user-1", "team-22-shared", "results", "team-22-task-40")
	if err := os.MkdirAll(resultDir, 0o775); err != nil {
		t.Fatalf("create result dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(resultDir, "team-introduction-report.md"), []byte("full report"), 0o664); err != nil {
		t.Fatalf("write result: %v", err)
	}
	task := &models.TeamTask{ID: taskID, TeamID: teamID, TargetMemberID: 120, MessageID: messageID, Status: models.TeamTaskStatusRunning, UpdatedAt: time.Now().UTC()}
	leader := &models.TeamMember{ID: 120, TeamID: teamID, MemberKey: "leader", Role: "leader", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": leader},
	}
	service := &teamService{repo: repo, runtimeWorkspaceRoot: workspaceRoot}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":              "task_completed",
		"memberId":           "leader",
		"messageId":          messageID,
		"status":             "succeeded",
		"summary":            "Team introduction complete.",
		"resultMarkdown":     "Full report: `/team/results/team-22-task-40/team-introduction-report.md`.",
		"artifactRefs":       []string{"/team/results/team-22-task-40/team-introduction-report.md"},
		"explicitCompletion": true,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	if err := service.projectTeamEvent(&models.Team{ID: teamID, UserID: 1, SharedMountPath: "/team", CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID: "1781171178688-0", Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}
	if repo.updatedTask == nil || repo.updatedTask.Status != models.TeamTaskStatusSucceeded {
		t.Fatalf("existing Markdown artifact should allow completion, got %#v", repo.updatedTask)
	}
	if len(repo.createdEvents) != 1 || repo.createdEvents[0].EventType != "task_completed" {
		t.Fatalf("expected task_completed event, got %#v", repo.createdEvents)
	}
}

func TestProjectTeamEventResolvesProtocolV2CompletionByMessageID(t *testing.T) {
	teamID := 22
	taskID := 52
	messageID := "team-22-bootstrap-introduction"
	workspaceRoot := t.TempDir()
	resultDir := filepath.Join(workspaceRoot, "teams", "user-1", "team-22-shared", "results", "team-22-task-52")
	if err := os.MkdirAll(resultDir, 0o775); err != nil {
		t.Fatalf("create result dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(resultDir, "result.md"), []byte("full report"), 0o664); err != nil {
		t.Fatalf("write result: %v", err)
	}
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         teamID,
		TargetMemberID: 120,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC(),
	}
	leader := &models.TeamMember{
		ID:            120,
		TeamID:        teamID,
		MemberKey:     "leader",
		Role:          "leader",
		Status:        models.TeamMemberStatusBusy,
		CurrentTaskID: &taskID,
		Availability:  models.TeamMemberAvailabilityBusy,
	}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": leader},
	}
	service := &teamService{repo: repo, runtimeWorkspaceRoot: workspaceRoot}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"protocolVersion":     2,
		"event":               "task_completed",
		"eventId":             "evt-team-22-task-52",
		"completionId":        "completion:22:team-22-task-52:leader",
		"completionSource":    teamTaskCompletionTool,
		"explicitCompletion":  true,
		"memberId":            "leader",
		"messageId":           messageID,
		"completionMessageId": messageID,
		// Some runtimes preserve their own internal task identifier here. The
		// root task must still resolve through the message identifiers above.
		"taskId":         "runtime-task-52",
		"rootTaskId":     "runtime-task-52",
		"status":         "succeeded",
		"summary":        "Team introduction complete.",
		"resultMarkdown": "Full report: /team/results/team-22-task-52/result.md",
		"artifactRefs":   []string{"/team/results/team-22-task-52/result.md"},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	if err := service.projectTeamEvent(&models.Team{ID: teamID, UserID: 1, SharedMountPath: "/team", CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID: "1781171178690-0", Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}
	if repo.updatedTask == nil || repo.updatedTask.Status != models.TeamTaskStatusSucceeded || repo.updatedTask.FinishedAt == nil {
		t.Fatalf("protocol v2 completion should resolve and close the root task, got %#v", repo.updatedTask)
	}
	if len(repo.createdEvents) != 1 || repo.createdEvents[0].EventType != "task_completed" {
		t.Fatalf("expected accepted task_completed event, got %#v", repo.createdEvents)
	}
}

func TestProjectTeamEventMissingArtifactKeepsFinalAnswerInWarning(t *testing.T) {
	teamID := 22
	taskID := 40
	messageID := "team-22-bootstrap-introduction"
	task := &models.TeamTask{ID: taskID, TeamID: teamID, TargetMemberID: 120, MessageID: messageID, Status: models.TeamTaskStatusRunning, UpdatedAt: time.Now().UTC()}
	leader := &models.TeamMember{ID: 120, TeamID: teamID, MemberKey: "leader", Role: "leader", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": leader},
	}
	service := &teamService{repo: repo, runtimeWorkspaceRoot: t.TempDir()}
	finalBody := "# Delivery Team\n\nDetailed final answer."
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"protocolVersion":    3,
		"event":              "completion_proposed",
		"eventId":            "evt-missing-artifact",
		"completionId":       "completion:22:team-22-task-40:leader",
		"attemptId":          "attempt-missing-artifact",
		"completionSource":   teamTaskCompletionTool,
		"memberId":           "leader",
		"messageId":          messageID,
		"taskId":             "team-22-task-40",
		"rootTaskId":         "team-22-task-40",
		"status":             "succeeded",
		"summary":            "Team introduction complete.",
		"resultMarkdown":     finalBody,
		"artifactRefs":       []string{"/team/results/team-22-task-40/missing.md"},
		"explicitCompletion": true,
		"rootTaskTerminal":   true,
		"workflowFinal":      true,
		"finalAnswerReady":   true,
		"remainingActions":   []string{},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	if err := service.projectTeamEvent(&models.Team{ID: teamID, UserID: 1, SharedMountPath: "/team", CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID: "1781171178689-0", Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}
	if repo.updatedTask == nil || repo.updatedTask.Status == models.TeamTaskStatusSucceeded || repo.updatedTask.FinishedAt != nil {
		t.Fatalf("missing artifact must reopen an incorrectly terminal task, got %#v", repo.updatedTask)
	}
	if len(repo.createdEvents) != 1 || repo.createdEvents[0].EventType != "message_warning" {
		t.Fatalf("expected artifact warning event, got %#v", repo.createdEvents)
	}
	stored := map[string]interface{}{}
	if err := json.Unmarshal([]byte(*repo.createdEvents[0].PayloadJSON), &stored); err != nil {
		t.Fatalf("decode warning: %v", err)
	}
	if stored["resultMarkdown"] != finalBody || stored["summary"] != "Team introduction complete." {
		t.Fatalf("artifact warning must preserve final answer, got %#v", stored)
	}
	if stored["artifactValidationMessage"] == "" {
		t.Fatalf("expected separate artifact validation diagnostic, got %#v", stored)
	}
	if stored["completionDecision"] != teamCompletionDecisionRejected || stored["completionDecisionReason"] != "missing_artifacts" {
		t.Fatalf("missing artifact must never acknowledge completion as accepted, got %#v", stored)
	}
	if len(repo.outboxRows) != 1 || !strings.Contains(repo.outboxRows[0].PayloadJSON, `"decision":"rejected"`) {
		t.Fatalf("missing artifact rejection must be delivered reliably, got %#v", repo.outboxRows)
	}
}

func TestNormalizeTeamArtifactReferencesDoesNotCorruptSerializedJSON(t *testing.T) {
	service := &teamService{runtimeWorkspaceRoot: "/workspaces"}
	payload := map[string]interface{}{
		"raw": `{"resultMarkdown":"line one\n/team/results/team-22-task-40/result.md","artifactRefs":["/team/results/team-22-task-40/result.md"]}`,
	}
	service.normalizeTeamArtifactReferences(&models.Team{ID: 22, UserID: 1, SharedMountPath: "/team"}, payload)
	got, _ := payload["raw"].(string)
	if strings.Contains(got, "line one/n") || !strings.Contains(got, `line one\n`) {
		t.Fatalf("serialized JSON escapes were corrupted: %q", got)
	}
}

func TestTeamTaskCompletionSignalsRemainBackwardCompatibleAcrossProtocolVersions(t *testing.T) {
	v1 := map[string]interface{}{
		"status":             "succeeded",
		"resultMarkdown":     "Legacy runtime final answer",
		"explicitCompletion": true,
	}
	if !isTeamTaskCompletionSignal("task_completed", "succeeded", v1) {
		t.Fatal("expected explicit legacy v1 task_completed event to remain supported")
	}
	plainV1 := cloneStringInterfaceMap(v1)
	delete(plainV1, "explicitCompletion")
	if isTeamTaskCompletionSignal("task_completed", "succeeded", plainV1) {
		t.Fatal("legacy task_completed without an explicit completion marker must remain non-terminal")
	}

	v2 := map[string]interface{}{
		"protocolVersion":    2,
		"eventId":            "evt-70",
		"status":             "succeeded",
		"completionId":       "completion:31:task-70:leader",
		"completionSource":   teamTaskCompletionTool,
		"explicitCompletion": true,
		"taskId":             "team-31-task-70",
		"rootTaskId":         "team-31-task-70",
		"memberId":           "leader",
		"summary":            "Protocol v2 final answer",
		"resultMarkdown":     "Protocol v2 final answer",
		"artifactRefs":       []interface{}{"/team/results/team-31-task-70/result.md"},
	}
	if isTeamTaskCompletionSignal("reply", "succeeded", v2) {
		t.Fatal("expected a v2 reply to remain non-terminal")
	}
	if !isTeamTaskCompletionSignal("task_completed", "succeeded", v2) {
		t.Fatal("expected an explicit v2 task_completed event to be terminal")
	}
	v2WithoutArtifacts := cloneStringInterfaceMap(v2)
	delete(v2WithoutArtifacts, "artifactRefs")
	if !isTeamTaskCompletionSignal("task_completed", "succeeded", v2WithoutArtifacts) {
		t.Fatal("expected explicit v2 completion with resultMarkdown to be terminal even without artifactRefs")
	}

	missingBody := cloneStringInterfaceMap(v2)
	delete(missingBody, "resultMarkdown")
	if isTeamTaskCompletionSignal("task_completed", "succeeded", missingBody) {
		t.Fatal("expected v2 completion without a result body to remain open")
	}

	automatic := cloneStringInterfaceMap(v2)
	automatic["explicitCompletion"] = false
	automatic["completionSource"] = "runtime_processing"
	if isTeamTaskCompletionSignal("task_completed", "succeeded", automatic) {
		t.Fatal("expected automatic runtime success to remain non-terminal")
	}
}

func TestTeamTaskFailureSignalsRemainBackwardCompatibleAcrossProtocolVersions(t *testing.T) {
	if !isTeamTaskFailureSignal("task_failed", "failed", map[string]interface{}{}) {
		t.Fatal("expected legacy task_failed event to remain supported")
	}

	v2RuntimeFailure := map[string]interface{}{
		"protocolVersion":  2,
		"eventId":          "evt-71",
		"status":           "failed",
		"completionId":     "completion:31:task-71:worker",
		"completionSource": "runtime_error",
		"taskId":           "team-31-task-71",
		"rootTaskId":       "team-31-task-71",
		"memberId":         "worker",
		"summary":          "runtime failed",
		"artifactRefs":     []interface{}{"/team/results/team-31-task-71/result.md"},
	}
	if !isTeamTaskFailureSignal("task_failed", "failed", v2RuntimeFailure) {
		t.Fatal("expected structured v2 runtime failure to be terminal")
	}
	if isTeamTaskFailureSignal("reply", "failed", v2RuntimeFailure) {
		t.Fatal("expected failed v2 reply to remain non-terminal")
	}
}

func TestAcceptedTeamCompletionIDOnlyDeduplicatesAcceptedTerminalEvents(t *testing.T) {
	completionID := "completion:31:task-72:leader"
	terminalPayload, err := json.Marshal(map[string]interface{}{
		"protocolVersion":    2,
		"eventId":            "evt-72",
		"status":             "succeeded",
		"completionId":       completionID,
		"completionSource":   teamTaskCompletionTool,
		"explicitCompletion": true,
		"taskId":             "team-31-task-72",
		"rootTaskId":         "team-31-task-72",
		"memberId":           "leader",
		"summary":            "Final answer",
		"resultMarkdown":     "Final answer",
		"artifactRefs":       []interface{}{"/team/results/team-31-task-72/result.md"},
	})
	if err != nil {
		t.Fatalf("marshal terminal payload: %v", err)
	}
	warningPayload, err := json.Marshal(map[string]interface{}{
		"protocolVersion":  2,
		"status":           "blocked",
		"completionId":     completionID,
		"completionSource": teamTaskCompletionTool,
	})
	if err != nil {
		t.Fatalf("marshal warning payload: %v", err)
	}

	repo := &teamRepositoryStub{createdEvents: []models.TeamEvent{{
		TeamID:      31,
		EventType:   "message_warning",
		PayloadJSON: stringPtr(string(warningPayload)),
	}}}
	service := &teamService{repo: repo}
	duplicate, err := service.hasAcceptedTeamCompletionID(31, completionID)
	if err != nil {
		t.Fatalf("check warning completion ID: %v", err)
	}
	if duplicate {
		t.Fatal("expected rejected artifact completion to remain retryable")
	}

	repo.createdEvents = append(repo.createdEvents, models.TeamEvent{
		TeamID:      31,
		EventType:   "task_completed",
		PayloadJSON: stringPtr(string(terminalPayload)),
	})
	duplicate, err = service.hasAcceptedTeamCompletionID(31, completionID)
	if err != nil {
		t.Fatalf("check terminal completion ID: %v", err)
	}
	if !duplicate {
		t.Fatal("expected accepted terminal completion ID to be deduplicated")
	}
}

func cloneStringInterfaceMap(source map[string]interface{}) map[string]interface{} {
	clone := make(map[string]interface{}, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func stringPtr(value string) *string { return &value }

func TestTaskHasRecentActivityUsesBusinessWorkItems(t *testing.T) {
	now := time.Now().UTC()
	task := &models.TeamTask{
		ID:        88,
		TeamID:    42,
		MessageID: "team-42-task-root",
		Status:    models.TeamTaskStatusRunning,
		UpdatedAt: now.Add(-time.Hour),
	}
	repo := &teamRepositoryStub{
		workItems: []models.TeamWorkItem{{
			TeamID:     42,
			RootTaskID: 88,
			WorkID:     "collect-paper",
			Status:     models.TeamTaskStatusRunning,
			UpdatedAt:  now.Add(-time.Minute),
		}},
	}
	service := &teamService{repo: repo}
	active, err := service.taskHasRecentActivity(&models.Team{ID: 42}, task, now.Add(-5*time.Minute))
	if err != nil {
		t.Fatalf("taskHasRecentActivity returned error: %v", err)
	}
	if !active {
		t.Fatal("expected a recently updated running work item to keep the root task active")
	}
}

func TestLeaderMediatedWorkerProgressIsNotConfirmedAsResult(t *testing.T) {
	teamID := 46
	taskID := 79
	messageID := "team-46-task-1783559040281180893"
	task := &models.TeamTask{ID: taskID, TeamID: teamID, TargetMemberID: 169, MessageID: messageID, Status: models.TeamTaskStatusRunning, UpdatedAt: time.Now().UTC()}
	leader := &models.TeamMember{ID: 169, TeamID: teamID, MemberKey: "delivery-lead", Role: "leader", Status: models.TeamMemberStatusBusy, Availability: models.TeamMemberAvailabilityBusy}
	developer := &models.TeamMember{ID: 170, TeamID: teamID, MemberKey: "developer", Role: "developer", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByID:      map[int]*models.TeamMember{169: leader, 170: developer},
		membersByKey:     map[string]*models.TeamMember{"delivery-lead": leader, "leader": leader, "developer": developer},
	}
	service := &teamService{repo: repo, runtimeWorkspaceRoot: t.TempDir()}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":         "reply",
		"memberId":      "developer",
		"messageId":     "msg-progress-1",
		"rootTaskId":    "team-46-task-79",
		"rootMessageId": messageID,
		"status":        "running",
		"text":          "RAG Papers Task - Progress Update: Starting paper search and crawl. I found candidates and am now processing all PDFs.",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := service.projectTeamEvent(&models.Team{ID: teamID, UserID: 1, SharedMountPath: "/team", CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID: "1783559160000-0", Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}

	if len(repo.workItems) != 0 {
		t.Fatalf("progress-only worker reply must not create a completed member result, got %#v", repo.workItems)
	}
	for _, event := range repo.createdEvents {
		if event.EventType == "member_result_confirmed" {
			t.Fatalf("progress-only worker reply must not emit result confirmation: %#v", event)
		}
	}
}

func TestLeaderMediatedMonitorBlockerDoesNotCloseRootTask(t *testing.T) {
	teamID := 46
	taskID := 79
	messageID := "team-46-task-1783559040281180893"
	task := &models.TeamTask{ID: taskID, TeamID: teamID, TargetMemberID: 169, MessageID: messageID, Status: models.TeamTaskStatusRunning, UpdatedAt: time.Now().UTC()}
	leader := &models.TeamMember{ID: 169, TeamID: teamID, MemberKey: "delivery-lead", Role: "leader", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByID:      map[int]*models.TeamMember{169: leader},
		membersByKey:     map[string]*models.TeamMember{"delivery-lead": leader, "leader": leader},
	}
	service := &teamService{repo: repo, runtimeWorkspaceRoot: t.TempDir()}
	monitorSummary := "Monitoring agent for dev-papers-001 completed. Developer unresponsive after 3 consecutive status checks. Zero paper files found, no summary created. Blocker report sent to leader for investigation."
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":         "task_failed",
		"memberId":      "delivery-lead",
		"messageId":     messageID,
		"rootTaskId":    "team-46-task-79",
		"rootMessageId": messageID,
		"status":        "failed",
		"summary":       monitorSummary,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := service.projectTeamEvent(&models.Team{ID: teamID, UserID: 1, SharedMountPath: "/team", CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID: "1783560082000-0", Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}

	if repo.updatedTask != nil && repo.updatedTask.Status == models.TeamTaskStatusFailed {
		t.Fatalf("monitor blocker candidate must not close root task, got %#v", repo.updatedTask)
	}
	if len(repo.createdEvents) != 1 || repo.createdEvents[0].EventType != "message_warning" {
		t.Fatalf("expected monitor blocker to be stored as warning, got %#v", repo.createdEvents)
	}
	stored := teamEventPayloadMap(repo.createdEvents[0])
	if !eventBool(stored, "monitorBlockerCandidate") || eventString(stored, "status") != "attention_required" {
		t.Fatalf("expected monitor blocker candidate markers, got %#v", stored)
	}
}

func TestLeaderMediatedMemberResultReopensNonAuthoritativeMonitorFailure(t *testing.T) {
	teamID := 46
	taskID := 79
	messageID := "team-46-task-1783559040281180893"
	finishedAt := time.Now().UTC().Add(-5 * time.Minute)
	monitorSummary := "Monitoring agent for dev-papers-001 completed. Developer unresponsive after 3 consecutive status checks. Zero paper files found, no summary created."
	task := &models.TeamTask{ID: taskID, TeamID: teamID, TargetMemberID: 169, MessageID: messageID, Status: models.TeamTaskStatusFailed, FinishedAt: &finishedAt, ErrorMessage: &monitorSummary, UpdatedAt: time.Now().UTC()}
	leader := &models.TeamMember{ID: 169, TeamID: teamID, MemberKey: "delivery-lead", Role: "leader", Status: models.TeamMemberStatusBusy, Availability: models.TeamMemberAvailabilityBusy}
	developer := &models.TeamMember{ID: 170, TeamID: teamID, MemberKey: "developer", Role: "developer", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByID:      map[int]*models.TeamMember{169: leader, 170: developer},
		membersByKey:     map[string]*models.TeamMember{"delivery-lead": leader, "leader": leader, "developer": developer},
	}
	service := &teamService{repo: repo, runtimeWorkspaceRoot: t.TempDir()}
	resultText := "Final Delivery Confirmation - dev-papers-001 COMPLETE\n\nFiles are now at /team/artifacts/team-46-task-79/members/developer/dev-papers-001/report.md."
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":          "reply",
		"memberId":       "developer",
		"messageId":      "msg-final-1",
		"rootTaskId":     "team-46-task-79",
		"rootMessageId":  messageID,
		"status":         "succeeded",
		"summary":        "Developer delivered the paper report.",
		"resultMarkdown": resultText,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := service.projectTeamEvent(&models.Team{ID: teamID, UserID: 1, SharedMountPath: "/team", CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID: "1783560514000-0", Fields: map[string]string{"payload": string(payloadJSON)},
	}); err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}

	if repo.updatedTask == nil || repo.updatedTask.Status != models.TeamTaskStatusRunning || repo.updatedTask.FinishedAt != nil || repo.updatedTask.ErrorMessage != nil {
		t.Fatalf("member result should reopen non-authoritative monitor failure, got %#v", repo.updatedTask)
	}
	if len(repo.workItems) != 1 || repo.workItems[0].Status != models.TeamTaskStatusSucceeded || repo.workItems[0].WorkID != "member-developer" {
		t.Fatalf("expected developer result work item, got %#v", repo.workItems)
	}
	if len(repo.createdEvents) < 2 || repo.createdEvents[len(repo.createdEvents)-1].EventType != "member_result_confirmed" {
		t.Fatalf("expected member result confirmation event, got %#v", repo.createdEvents)
	}
}

func TestAssignmentMonitorEnvelopeIsNonTerminalAndAddressedToWorker(t *testing.T) {
	now := time.Date(2026, 7, 9, 10, 30, 0, 0, time.UTC)
	team := &models.Team{ID: 46}
	task := &models.TeamTask{ID: 78, TeamID: 46, MessageID: "team-46-task-1783559040281180893"}
	item := &models.TeamWorkItem{ID: 9001, WorkID: "dev-papers-001", Title: "Assign to developer", UpdatedAt: now.Add(-4 * time.Minute)}
	owner := &models.TeamMember{ID: 301, TeamID: 46, MemberKey: "developer"}

	envelope, messageID := buildAssignmentStatusCheckEnvelope(team, task, item, owner, now)
	if messageID != "monitor:team-46-task-78:dev-papers-001:9908850" {
		t.Fatalf("unexpected monitor message id %q", messageID)
	}
	if envelope["intent"] != "assignment_status_check" || envelope["to"] != "developer" || envelope["from"] != "clawmanager-monitor" {
		t.Fatalf("unexpected monitor routing: %#v", envelope)
	}
	if envelope["requiresCompletion"] != false || envelope["rootTaskId"] != "team-46-task-78" || envelope["workId"] != "dev-papers-001" {
		t.Fatalf("monitor envelope should be non-terminal and assignment scoped: %#v", envelope)
	}
	if envelope["checkId"] != messageID || envelope["checkSequence"] != int64(9908850) || envelope["requestedAt"] == "" {
		t.Fatalf("monitor envelope must carry stable check identity: %#v", envelope)
	}
	monitorPolicy, ok := envelope["monitorPolicy"].(map[string]interface{})
	if !ok || monitorPolicy["enabled"] != true || monitorPolicy["visibleToChat"] != true {
		t.Fatalf("expected visible monitor policy on monitor envelope, got %#v", envelope["monitorPolicy"])
	}
	if monitorPolicy["heartbeatEverySec"] != 30 || monitorPolicy["visibleHeartbeatEverySec"] != 180 {
		t.Fatalf("expected monitor envelope to separate internal heartbeat and chat digest cadence, got %#v", monitorPolicy)
	}
	prompt, _ := envelope["prompt"].(string)
	if !strings.Contains(prompt, "call team_update_progress") || !strings.Contains(prompt, "assignment_check_result") || strings.Contains(prompt, "call team_complete_task") {
		t.Fatalf("monitor prompt should request progress without forcing completion: %s", prompt)
	}
	metadata, ok := envelope["metadata"].(map[string]interface{})
	if !ok || metadata["monitor"] != true || metadata["monitorType"] != "assignment_status_check" || metadata["eventKind"] != "assignment_check_requested" || metadata["visibleToChat"] != false {
		t.Fatalf("unexpected monitor metadata: %#v", envelope["metadata"])
	}
}

func TestAssignmentMonitorEligibilityAndThrottle(t *testing.T) {
	cutoff := time.Date(2026, 7, 9, 10, 30, 0, 0, time.UTC)
	ownerID := 301
	if !shouldMonitorTeamWorkItem(models.TeamWorkItem{OwnerMemberID: &ownerID, WorkID: "dev-papers-001", Status: models.TeamTaskStatusRunning, UpdatedAt: cutoff.Add(-time.Second)}, cutoff) {
		t.Fatal("expected stale running worker item to be monitored")
	}
	if shouldMonitorTeamWorkItem(models.TeamWorkItem{OwnerMemberID: &ownerID, WorkID: "dev-papers-001", Status: models.TeamTaskStatusRunning, UpdatedAt: cutoff.Add(time.Second)}, cutoff) {
		t.Fatal("fresh worker item should not be monitored")
	}
	if shouldMonitorTeamWorkItem(models.TeamWorkItem{OwnerMemberID: &ownerID, WorkID: "dev-papers-001", Status: models.TeamTaskStatusSucceeded, UpdatedAt: cutoff.Add(-time.Hour)}, cutoff) {
		t.Fatal("terminal worker item should not be monitored")
	}
	if shouldMonitorTeamWorkItem(models.TeamWorkItem{WorkID: "dev-papers-001", Status: models.TeamTaskStatusRunning, UpdatedAt: cutoff.Add(-time.Hour)}, cutoff) {
		t.Fatal("unowned worker item should not be monitored")
	}

	service := &teamService{}
	if !service.claimAssignmentMonitorSlot("46:78:dev-papers-001:301", cutoff) {
		t.Fatal("expected first monitor slot claim to succeed")
	}
	if service.claimAssignmentMonitorSlot("46:78:dev-papers-001:301", cutoff.Add(teamAssignmentMonitorEvery-time.Second)) {
		t.Fatal("expected monitor slot to be throttled inside interval")
	}
	if !service.claimAssignmentMonitorSlot("46:78:dev-papers-001:301", cutoff.Add(teamAssignmentMonitorEvery+time.Second)) {
		t.Fatal("expected monitor slot to reopen after interval")
	}
}

func TestLeaderMediatedRecoverableWarningClassification(t *testing.T) {
	team := &models.Team{ID: 47, CommunicationMode: teamCommunicationModeLeaderMediated}
	task := &models.TeamTask{ID: 82, TeamID: 47, Status: models.TeamTaskStatusRunning}
	member := &models.TeamMember{ID: 12, TeamID: 47, MemberKey: "developer", Role: "developer"}
	if !isLeaderMediatedRecoverableWarning(team, "message_warning", map[string]interface{}{
		"artifactValidationFailed": true,
		"rootTaskTerminal":         false,
	}, member, task) {
		t.Fatal("artifact validation warnings should start recovery")
	}
	if !isLeaderMediatedRecoverableWarning(team, "message_warning", map[string]interface{}{
		"nonAuthoritative":  true,
		"rootTaskTerminal":  false,
		"eventKind":         "completion_validation_warning",
		"assignmentResult":  "candidate",
		"assignmentId":      "member-developer",
		"assignment_result": "candidate",
	}, member, task) {
		t.Fatal("non-authoritative worker warnings should start recovery")
	}
	if isLeaderMediatedRecoverableWarning(team, "message_warning", map[string]interface{}{
		"eventKind":        "assignment_recovery_exhausted",
		"rootTaskTerminal": false,
	}, member, task) {
		t.Fatal("recovery exhausted should not start another automatic recovery")
	}
	if isLeaderMediatedRecoverableWarning(team, "assignment_heartbeat", map[string]interface{}{
		"eventKind":        "assignment_heartbeat",
		"nonAuthoritative": true,
		"rootTaskTerminal": false,
	}, member, task) {
		t.Fatal("heartbeat must not start recovery")
	}
	if isLeaderMediatedRecoverableWarning(team, "task_progress", map[string]interface{}{
		"eventKind":        "assignment_check_result",
		"nonAuthoritative": true,
		"rootTaskTerminal": false,
	}, member, task) {
		t.Fatal("passive assignment check result must not start recovery")
	}
}

func TestProjectTeamEventDropsHeartbeatAfterTerminalTask(t *testing.T) {
	taskID := 91
	messageID := "team-49-bootstrap-introduction"
	finishedAt := time.Now().UTC().Add(-time.Minute)
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         49,
		TargetMemberID: 700,
		MessageID:      messageID,
		Status:         models.TeamTaskStatusSucceeded,
		FinishedAt:     &finishedAt,
		UpdatedAt:      finishedAt,
	}
	leader := &models.TeamMember{
		ID:           700,
		TeamID:       49,
		MemberKey:    "delivery-lead",
		Role:         "leader",
		Status:       models.TeamMemberStatusIdle,
		Availability: models.TeamMemberAvailabilityIdle,
	}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"delivery-lead": leader},
	}
	service := &teamService{repo: repo}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":         "assignment_heartbeat",
		"eventKind":     "assignment_heartbeat",
		"memberId":      "delivery-lead",
		"messageId":     messageID,
		"taskId":        "team-49-task-91",
		"heartbeatSeq":  12,
		"visibleToChat": true,
		"summary":       "Agent turn is still running",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	err = service.projectTeamEvent(&models.Team{ID: 49, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID:     "1781171179000-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	})
	if err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}
	if len(repo.createdEvents) != 0 {
		t.Fatalf("terminal heartbeat must not be stored as a chat event, got %#v", repo.createdEvents)
	}
	if repo.updatedTask != nil || repo.updatedMember != nil {
		t.Fatalf("terminal heartbeat must not mutate task/member state, task=%#v member=%#v", repo.updatedTask, repo.updatedMember)
	}
}

func TestProjectTeamEventDropsHeartbeatAfterTerminalWorkItem(t *testing.T) {
	rootTaskID := 95
	memberID := 701
	task := &models.TeamTask{
		ID:             rootTaskID,
		TeamID:         52,
		TargetMemberID: 700,
		MessageID:      "team-52-task-1783601111",
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC().Add(-time.Minute),
	}
	developer := &models.TeamMember{
		ID:           memberID,
		TeamID:       52,
		MemberKey:    "developer",
		Role:         "developer",
		Status:       models.TeamMemberStatusIdle,
		Availability: models.TeamMemberAvailabilityIdle,
	}
	repo := &teamRepositoryStub{
		tasksByID:    map[int]*models.TeamTask{rootTaskID: task},
		membersByID:  map[int]*models.TeamMember{memberID: developer},
		membersByKey: map[string]*models.TeamMember{"developer": developer},
		workItems: []models.TeamWorkItem{{
			TeamID:        52,
			RootTaskID:    rootTaskID,
			WorkID:        "member-developer",
			OwnerMemberID: &memberID,
			Title:         "developer delivers result",
			Status:        models.TeamTaskStatusSucceeded,
			UpdatedAt:     time.Now().UTC().Add(-time.Minute),
		}},
	}
	service := &teamService{repo: repo}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":            "assignment_heartbeat",
		"eventKind":        "assignment_heartbeat",
		"memberId":         "developer",
		"from":             "developer",
		"taskId":           "team-52-task-95",
		"rootTaskId":       "team-52-task-95",
		"workId":           "member-developer",
		"assignmentId":     "member-developer",
		"status":           models.TeamTaskStatusRunning,
		"nonAuthoritative": true,
		"summary":          "Agent turn is still running",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	err = service.projectTeamEvent(&models.Team{ID: 52, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID:     "1783601200000-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	})
	if err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}
	if len(repo.createdEvents) != 0 {
		t.Fatalf("post-terminal work item heartbeat must not be stored, got %#v", repo.createdEvents)
	}
	if repo.updatedTask != nil || repo.updatedMember != nil {
		t.Fatalf("post-terminal work item heartbeat must not mutate task/member state, task=%#v member=%#v", repo.updatedTask, repo.updatedMember)
	}
}

func TestProjectTeamEventMapsGeneratedWorkerCompletionToExistingWorkItem(t *testing.T) {
	rootTaskID := 87
	rootMessageID := "team-50-task-1783587467953008490"
	ownerID := 183
	task := &models.TeamTask{
		ID:             rootTaskID,
		TeamID:         50,
		TargetMemberID: 181,
		MessageID:      rootMessageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      time.Now().UTC().Add(-time.Minute),
	}
	reviewer := &models.TeamMember{
		ID:           ownerID,
		TeamID:       50,
		MemberKey:    "reviewer",
		Role:         "reviewer",
		Status:       models.TeamMemberStatusBusy,
		Availability: models.TeamMemberAvailabilityBusy,
	}
	repo := &teamRepositoryStub{
		tasksByID:    map[int]*models.TeamTask{rootTaskID: task},
		membersByID:  map[int]*models.TeamMember{ownerID: reviewer},
		membersByKey: map[string]*models.TeamMember{"reviewer": reviewer},
		workItems: []models.TeamWorkItem{{
			TeamID:        50,
			RootTaskID:    rootTaskID,
			WorkID:        "member-reviewer",
			OwnerMemberID: &ownerID,
			Title:         "Assign to reviewer",
			Status:        models.TeamTaskStatusDispatched,
			UpdatedAt:     time.Now().UTC().Add(-2 * time.Minute),
		}},
	}
	service := &teamService{repo: repo}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":                "task_completed",
		"protocolVersion":      2,
		"completionId":         "completion:50:task_01d9:reviewer",
		"completionSource":     teamTaskCompletionTool,
		"explicitCompletion":   true,
		"assignmentResultOnly": true,
		"memberId":             "reviewer",
		"from":                 "reviewer",
		"to":                   "leader",
		"taskId":               "task_01d9ca72-9721-44ea-a389-d43bdf573521",
		"rootTaskId":           "task_01d9ca72-9721-44ea-a389-d43bdf573521",
		"workId":               "chat-app-review",
		"status":               models.TeamTaskStatusSucceeded,
		"summary":              "Review complete. Verdict: PASS.",
		"resultMarkdown":       "Review complete. Verdict: PASS.",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	err = service.projectTeamEvent(&models.Team{ID: 50, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID:     "1783589347769-0",
		Fields: map[string]string{"payload": string(payloadJSON)},
	})
	if err != nil {
		t.Fatalf("projectTeamEvent returned error: %v", err)
	}
	if len(repo.createdEvents) == 0 || repo.createdEvents[0].TaskID == nil || *repo.createdEvents[0].TaskID != rootTaskID {
		t.Fatalf("expected generated runtime completion to attach to root task, got %#v", repo.createdEvents)
	}
	if len(repo.createdEvents) < 2 {
		t.Fatalf("expected generated runtime completion to create a leader result notification, got %#v", repo.createdEvents)
	}
	notification := repo.createdEvents[len(repo.createdEvents)-1]
	if notification.EventType != "member_result_confirmed" || notification.TaskID == nil || *notification.TaskID != rootTaskID {
		t.Fatalf("expected member result confirmation on root task, got %#v", notification)
	}
	notificationPayload := teamEventPayloadMap(notification)
	if eventString(notificationPayload, "rootTaskId") != "team-50-task-87" ||
		eventString(notificationPayload, "rootMessageId") != rootMessageID ||
		eventString(notificationPayload, "workId") != "member-reviewer" {
		t.Fatalf("member result notification must preserve root context, got %#v", notificationPayload)
	}
	if eventBool(notificationPayload, "visibleToChat", "visible_to_chat") ||
		eventString(notificationPayload, "sourceCompletionId") != "completion:50:task_01d9:reviewer" ||
		eventString(notificationPayload, "sourceWorkId") != "chat-app-review" ||
		eventString(notificationPayload, "resultMarkdown", "result", "text") != "" {
		t.Fatalf("member result confirmation must be hidden and linked to its source: %#v", notificationPayload)
	}
	if len(repo.outboxRows) != 1 || repo.outboxRows[0].MessageID == "" || repo.outboxRows[0].SourceEventID == "" {
		t.Fatalf("member result confirmation must atomically create a delivery outbox row, got %#v", repo.outboxRows)
	}
	if len(repo.workItems) != 1 || repo.workItems[0].WorkID != "member-reviewer" || repo.workItems[0].Status != models.TeamTaskStatusSucceeded {
		t.Fatalf("expected reviewer work item to close, got %#v", repo.workItems)
	}
	if repo.updatedTask == nil || repo.updatedTask.ID != rootTaskID || repo.updatedTask.Status != models.TeamTaskStatusRunning {
		t.Fatalf("worker completion must touch but not close root task, got %#v", repo.updatedTask)
	}
	if repo.updatedMember == nil || repo.updatedMember.MemberKey != "reviewer" || repo.updatedMember.Status != models.TeamMemberStatusIdle {
		t.Fatalf("expected reviewer member to become idle, got %#v", repo.updatedMember)
	}
}

func TestTerminalMemberPresenceCannotRestoreRunningState(t *testing.T) {
	teamID := 51
	taskID := 91
	memberID := 220
	messageID := "team-51-task-91-root"
	task := &models.TeamTask{ID: taskID, TeamID: teamID, TargetMemberID: 219, MessageID: messageID, Status: models.TeamTaskStatusRunning, UpdatedAt: time.Now().UTC()}
	member := &models.TeamMember{ID: memberID, TeamID: teamID, MemberKey: "pm", Role: "product-manager", Status: models.TeamMemberStatusIdle, Availability: models.TeamMemberAvailabilityIdle, Progress: 100}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByID:      map[int]*models.TeamMember{memberID: member},
		membersByKey:     map[string]*models.TeamMember{"pm": member},
		workItems: []models.TeamWorkItem{{
			TeamID:        teamID,
			RootTaskID:    taskID,
			WorkID:        "member-pm",
			OwnerMemberID: &memberID,
			Status:        models.TeamTaskStatusSucceeded,
		}},
	}
	service := &teamService{repo: repo}
	payload, _ := json.Marshal(map[string]interface{}{
		"event":         "presence",
		"memberId":      "pm",
		"taskId":        "team-51-task-91",
		"rootTaskId":    "team-51-task-91",
		"availability":  "busy",
		"runtimeStatus": "running",
	})
	if err := service.projectTeamEvent(&models.Team{ID: teamID, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, redisStreamMessage{
		ID: "1783593000000-0", Fields: map[string]string{"payload": string(payload)},
	}); err != nil {
		t.Fatal(err)
	}
	if repo.updatedMember == nil || repo.updatedMember.Status != models.TeamMemberStatusIdle || repo.updatedMember.Availability != models.TeamMemberAvailabilityIdle || derefTeamString(repo.updatedMember.RuntimeStatus) != models.TeamTaskStatusSucceeded {
		t.Fatalf("terminal worker presence restored running state: %#v", repo.updatedMember)
	}
}

func TestThreeWorkerResultsEachCreateOneConfirmationAndOutbox(t *testing.T) {
	team := &models.Team{ID: 52, CommunicationMode: teamCommunicationModeLeaderMediated}
	task := &models.TeamTask{ID: 93, TeamID: 52, TargetMemberID: 230, MessageID: "team-52-root", Status: models.TeamTaskStatusRunning}
	leader := &models.TeamMember{ID: 230, TeamID: 52, MemberKey: "delivery-lead", Role: "leader"}
	members := []*models.TeamMember{
		{ID: 231, TeamID: 52, MemberKey: "pm", Role: "product-manager"},
		{ID: 232, TeamID: 52, MemberKey: "designer", Role: "ui-designer"},
		{ID: 233, TeamID: 52, MemberKey: "architect", Role: "solution-architect"},
	}
	repo := &teamRepositoryStub{
		membersByID:  map[int]*models.TeamMember{230: leader, 231: members[0], 232: members[1], 233: members[2]},
		membersByKey: map[string]*models.TeamMember{"delivery-lead": leader, "pm": members[0], "designer": members[1], "architect": members[2]},
	}
	for _, member := range members {
		ownerID := member.ID
		repo.workItems = append(repo.workItems, models.TeamWorkItem{
			TeamID:        team.ID,
			RootTaskID:    task.ID,
			WorkID:        "member-" + member.MemberKey,
			OwnerMemberID: &ownerID,
			Status:        models.TeamTaskStatusSucceeded,
		})
	}
	service := &teamService{repo: repo}
	var wg sync.WaitGroup
	errs := make(chan error, len(members))
	for index, member := range members {
		index, member := index, member
		wg.Add(1)
		go func() {
			defer wg.Done()
			completionID := fmt.Sprintf("completion:52:93:%s", member.MemberKey)
			payload := map[string]interface{}{
				"completionId":   completionID,
				"workId":         "assignment-" + member.MemberKey,
				"summary":        fmt.Sprintf("%s 已完成任务", member.MemberKey),
				"resultMarkdown": fmt.Sprintf("%s 已完成任务并提交产物。", member.MemberKey),
			}
			sourceEventID := fmt.Sprintf("evt-worker-%d", index)
			errs <- service.createLeaderMediatedResultNotification(team, nil, task, member, payload, &models.TeamEvent{EventID: &sourceEventID, CreatedAt: time.Now().UTC()})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	confirmations := 0
	seenMembers := map[string]bool{}
	for _, event := range repo.createdEvents {
		if event.EventType != "member_result_confirmed" {
			continue
		}
		confirmations++
		payload := teamEventPayloadMap(event)
		seenMembers[eventString(payload, "memberId")] = true
	}
	if confirmations != 3 || len(seenMembers) != 3 || len(repo.outboxRows) != 3 || !seenMembers["pm"] {
		t.Fatalf("three-worker confirmation reconciliation failed: events=%#v outbox=%#v", repo.createdEvents, repo.outboxRows)
	}
}

func TestLeaderSynthesisReminderCreatedWhenWorkersDone(t *testing.T) {
	now := time.Now().UTC()
	rootTaskID := 89
	leaderID := 201
	developerID := 202
	reviewerID := 203
	rootMessageID := "team-51-task-1783600000000"
	task := &models.TeamTask{
		ID:             rootTaskID,
		TeamID:         51,
		TargetMemberID: leaderID,
		MessageID:      rootMessageID,
		Status:         models.TeamTaskStatusRunning,
		UpdatedAt:      now.Add(-5 * time.Minute),
	}
	leader := &models.TeamMember{ID: leaderID, TeamID: 51, MemberKey: "delivery-lead", Role: "leader", Status: models.TeamMemberStatusBusy, Availability: models.TeamMemberAvailabilityBusy}
	developer := &models.TeamMember{ID: developerID, TeamID: 51, MemberKey: "developer", Role: "developer", Status: models.TeamMemberStatusIdle, Availability: models.TeamMemberAvailabilityIdle}
	reviewer := &models.TeamMember{ID: reviewerID, TeamID: 51, MemberKey: "reviewer", Role: "reviewer", Status: models.TeamMemberStatusIdle, Availability: models.TeamMemberAvailabilityIdle}
	developerResult := `{"summary":"Build complete","resultMarkdown":"Developer delivered the chat app."}`
	reviewerResult := `{"summary":"PASS","resultMarkdown":"Reviewer verdict: PASS."}`
	items := []models.TeamWorkItem{
		{TeamID: 51, RootTaskID: rootTaskID, WorkID: "member-developer", OwnerMemberID: &developerID, Title: "Assign to developer", Status: models.TeamTaskStatusSucceeded, ResultJSON: &developerResult, UpdatedAt: now.Add(-4 * time.Minute)},
		{TeamID: 51, RootTaskID: rootTaskID, WorkID: "member-reviewer", OwnerMemberID: &reviewerID, Title: "Assign to reviewer", Status: models.TeamTaskStatusSucceeded, ResultJSON: &reviewerResult, UpdatedAt: now.Add(-4 * time.Minute)},
	}
	membersByID := map[int]*models.TeamMember{
		leaderID:    leader,
		developerID: developer,
		reviewerID:  reviewer,
	}
	ready, resultItems := leaderMediatedRootNeedsSynthesisReminder(task, items, membersByID)
	if !ready || len(resultItems) != 2 {
		t.Fatalf("expected root to need leader synthesis after worker results, ready=%v items=%#v", ready, resultItems)
	}
	repo := &teamRepositoryStub{
		teamsByID:    map[int]*models.Team{51: &models.Team{ID: 51, CommunicationMode: teamCommunicationModeLeaderMediated}},
		tasksByID:    map[int]*models.TeamTask{rootTaskID: task},
		membersByID:  map[int]*models.TeamMember{leaderID: leader, developerID: developer, reviewerID: reviewer},
		membersByKey: map[string]*models.TeamMember{"delivery-lead": leader, "developer": developer, "reviewer": reviewer},
		workItems:    items,
	}
	service := &teamService{repo: repo}
	if err := service.createLeaderSynthesisReminder(&models.Team{ID: 51, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, task, leader, resultItems, now); err != nil {
		t.Fatalf("createLeaderSynthesisReminder returned error: %v", err)
	}
	if len(repo.createdEvents) != 1 {
		t.Fatalf("expected one leader synthesis reminder event, got %#v", repo.createdEvents)
	}
	event := repo.createdEvents[0]
	if event.EventType != "leader_synthesis_reminder" || event.TaskID == nil || *event.TaskID != rootTaskID || event.MemberID == nil || *event.MemberID != leaderID {
		t.Fatalf("unexpected leader synthesis reminder event: %#v", event)
	}
	payload := teamEventPayloadMap(event)
	if eventString(payload, "rootTaskId") != "team-51-task-89" ||
		eventString(payload, "rootMessageId") != rootMessageID ||
		eventString(payload, "workId") != "leader-final-synthesis" {
		t.Fatalf("leader synthesis reminder must preserve root context, got %#v", payload)
	}
	memberResults, _ := payload["memberResults"].([]interface{})
	if len(memberResults) != 2 {
		t.Fatalf("expected reminder to carry member result summaries, got %#v", payload["memberResults"])
	}
}

func TestReconcileDeferredCompletionNeverReusesReportFromOlderPlan(t *testing.T) {
	now := time.Now().UTC()
	taskID := 194
	task := &models.TeamTask{
		ID:             taskID,
		TeamID:         31,
		TargetMemberID: 120,
		MessageID:      "team-31-task-194",
		Status:         models.TeamTaskStatusRunning,
		WorkflowState:  teamWorkflowStateAwaitingLeaderDecision,
		PlanVersion:    2,
		LedgerVersion:  7,
		UpdatedAt:      now,
	}
	leader := &models.TeamMember{ID: 120, TeamID: 31, MemberKey: "leader", Role: "leader", Status: models.TeamMemberStatusBusy}
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"protocolVersion":    3,
		"event":              "completion_deferred",
		"completionId":       "completion:31:team-31-task-194:leader",
		"completionSource":   teamTaskCompletionTool,
		"explicitCompletion": true,
		"rootTaskTerminal":   false,
		"workflowFinal":      true,
		"finalAnswerReady":   true,
		"remainingActions":   []string{},
		"planVersion":        1,
		"ledgerVersion":      3,
		"resultMarkdown":     "# 第一阶段报告\n\n这份报告不包含第二阶段产物。",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	eventID := "old-plan-deferred"
	repo := &teamRepositoryStub{
		tasksByID:    map[int]*models.TeamTask{taskID: task},
		membersByKey: map[string]*models.TeamMember{"leader": leader},
		createdEvents: []models.TeamEvent{{
			TeamID: task.TeamID, TaskID: &taskID, MemberID: &leader.ID, EventID: &eventID,
			EventType: "completion_deferred", PayloadJSON: stringPtr(string(payloadJSON)),
		}},
	}
	service := &teamService{repo: repo}
	reconciled, err := service.reconcileDeferredTeamCompletion(&models.Team{ID: 31, CommunicationMode: teamCommunicationModeLeaderMediated}, nil, task, leader)
	if err != nil {
		t.Fatalf("reconcileDeferredTeamCompletion returned error: %v", err)
	}
	if reconciled || task.Status == models.TeamTaskStatusSucceeded || len(repo.createdEvents) != 1 {
		t.Fatalf("an older plan report must not be auto-accepted after plan advancement: reconciled=%v task=%#v events=%#v", reconciled, task, repo.createdEvents)
	}
}

type teamRepositoryStub struct {
	mu               sync.Mutex
	teamsByID        map[int]*models.Team
	membersByID      map[int]*models.TeamMember
	membersByKey     map[string]*models.TeamMember
	tasksByID        map[int]*models.TeamTask
	tasksByMessageID map[string]*models.TeamTask
	createdEvents    []models.TeamEvent
	workItems        []models.TeamWorkItem
	workflowPhases   []models.TeamWorkflowPhase
	outboxRows       []models.TeamEventOutbox
	updatedTask      *models.TeamTask
	updatedMember    *models.TeamMember
	updatedTeam      *models.Team
}

type teamOpenClawConfigPlannerStub struct {
	calls    int
	userID   int
	plan     *OpenClawConfigPlan
	nextPlan *OpenClawConfigPlan
	err      error
}

func (s *teamOpenClawConfigPlannerStub) PlanWithoutTeamMemberLeaderOnlyChannels(userID int, plan *OpenClawConfigPlan) (*OpenClawConfigPlan, error) {
	s.calls++
	s.userID = userID
	s.plan = plan
	return s.nextPlan, s.err
}

func (s *teamRepositoryStub) CreateTeam(team *models.Team) error { return nil }
func (s *teamRepositoryStub) UpdateTeam(team *models.Team) error {
	clone := *team
	s.updatedTeam = &clone
	return nil
}
func (s *teamRepositoryStub) GetTeamByID(id int) (*models.Team, error) {
	if s.teamsByID != nil {
		return s.teamsByID[id], nil
	}
	return nil, nil
}
func (s *teamRepositoryStub) GetTeamByUserIDAndName(userID int, name string) (*models.Team, error) {
	return nil, nil
}
func (s *teamRepositoryStub) ExistsByUserIDAndName(userID int, name string) (bool, error) {
	return false, nil
}
func (s *teamRepositoryStub) ListTeamsByUserID(userID int, offset, limit int) ([]models.Team, error) {
	return nil, nil
}
func (s *teamRepositoryStub) ListActiveTeams() ([]models.Team, error) { return nil, nil }
func (s *teamRepositoryStub) CountTeamsByUserID(userID int) (int, error) {
	return 0, nil
}
func (s *teamRepositoryStub) CreateMember(member *models.TeamMember) error { return nil }
func (s *teamRepositoryStub) UpdateMember(member *models.TeamMember) error {
	clone := *member
	s.updatedMember = &clone
	return nil
}
func (s *teamRepositoryStub) GetMemberByID(id int) (*models.TeamMember, error) {
	if s.membersByID != nil {
		return s.membersByID[id], nil
	}
	return nil, nil
}
func (s *teamRepositoryStub) GetMemberByTeamKey(teamID int, memberKey string) (*models.TeamMember, error) {
	if s.membersByKey == nil {
		return nil, nil
	}
	member := s.membersByKey[memberKey]
	if member == nil || member.TeamID != teamID {
		return nil, nil
	}
	return member, nil
}
func (s *teamRepositoryStub) ListMembersByTeamID(teamID int) ([]models.TeamMember, error) {
	result := make([]models.TeamMember, 0)
	seen := map[int]bool{}
	for _, member := range s.membersByKey {
		if member != nil && member.TeamID == teamID && !seen[member.ID] {
			result = append(result, *member)
			seen[member.ID] = true
		}
	}
	for _, member := range s.membersByID {
		if member != nil && member.TeamID == teamID && !seen[member.ID] {
			result = append(result, *member)
			seen[member.ID] = true
		}
	}
	return result, nil
}
func (s *teamRepositoryStub) CreateTask(task *models.TeamTask) error { return nil }
func (s *teamRepositoryStub) UpdateTask(task *models.TeamTask) error {
	clone := *task
	s.updatedTask = &clone
	return nil
}
func (s *teamRepositoryStub) GetTaskByID(id int) (*models.TeamTask, error) {
	if s.tasksByID != nil {
		return s.tasksByID[id], nil
	}
	return nil, nil
}
func (s *teamRepositoryStub) GetTaskByMessageID(teamID int, messageID string) (*models.TeamTask, error) {
	if s.tasksByMessageID == nil {
		return nil, nil
	}
	task := s.tasksByMessageID[messageID]
	if task == nil || task.TeamID != teamID {
		return nil, nil
	}
	return task, nil
}
func (s *teamRepositoryStub) ListTasksByTeamID(teamID int, limit int) ([]models.TeamTask, error) {
	return nil, nil
}
func (s *teamRepositoryStub) ListTasksBeforeID(teamID, beforeID, limit int) ([]models.TeamTask, error) {
	return nil, nil
}
func (s *teamRepositoryStub) ListStaleCandidateTasks(cutoff time.Time, limit int) ([]models.TeamTask, error) {
	return nil, nil
}
func (s *teamRepositoryStub) CreateEvent(event *models.TeamEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	clone := *event
	s.createdEvents = append(s.createdEvents, clone)
	return nil
}
func (s *teamRepositoryStub) EventExistsByStreamID(teamID int, streamID string) (bool, error) {
	return false, nil
}
func (s *teamRepositoryStub) EventExistsByEventID(teamID int, eventID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, event := range s.createdEvents {
		if event.TeamID == teamID && event.EventID != nil && *event.EventID == eventID {
			return true, nil
		}
	}
	return false, nil
}
func (s *teamRepositoryStub) EventExistsByCompletionID(teamID int, completionID string) (bool, error) {
	for _, event := range s.createdEvents {
		if event.TeamID == teamID {
			payload := teamEventPayloadMap(event)
			if eventString(payload, "completionId", "completion_id") == completionID &&
				(isTeamTaskCompletionSignal(event.EventType, normalizedTeamTaskEventStatus(payload), payload) ||
					isTeamTaskFailureSignal(event.EventType, normalizedTeamTaskEventStatus(payload), payload)) {
				return true, nil
			}
		}
	}
	return false, nil
}
func (s *teamRepositoryStub) ListEventsByTeamID(teamID int, limit int) ([]models.TeamEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := make([]models.TeamEvent, 0, len(s.createdEvents))
	for _, event := range s.createdEvents {
		if event.TeamID == teamID {
			events = append(events, event)
		}
	}
	if limit > 0 && len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}
func (s *teamRepositoryStub) ListEventsBeforeID(teamID, beforeID, limit int) ([]models.TeamEvent, error) {
	return nil, nil
}
func (s *teamRepositoryStub) UpsertWorkItem(item *models.TeamWorkItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.workItems {
		if s.workItems[idx].TeamID == item.TeamID && s.workItems[idx].RootTaskID == item.RootTaskID && s.workItems[idx].WorkID == item.WorkID {
			existing := s.workItems[idx]
			newRevision := item.Revision > existing.Revision
			existingTerminal := existing.Status == models.TeamTaskStatusSucceeded || existing.Status == models.TeamTaskStatusFailed || existing.Status == models.TeamTaskStatusStale
			itemTerminal := item.Status == models.TeamTaskStatusSucceeded || item.Status == models.TeamTaskStatusFailed || item.Status == models.TeamTaskStatusStale
			reopeningCurrent := !newRevision && existingTerminal && !itemTerminal &&
				!item.UpdatedAt.IsZero() &&
				(existing.UpdatedAt.IsZero() || item.UpdatedAt.After(existing.UpdatedAt))
			if !newRevision && !reopeningCurrent {
				if item.ResultJSON == nil {
					item.ResultJSON = existing.ResultJSON
				}
				if item.FinishedAt == nil {
					item.FinishedAt = existing.FinishedAt
				}
			}
			s.workItems[idx] = *item
			return nil
		}
	}
	s.workItems = append(s.workItems, *item)
	return nil
}
func (s *teamRepositoryStub) InvalidateWorkItemReview(workItemID int, updatedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.workItems {
		if s.workItems[idx].ID == workItemID && s.workItems[idx].ReviewRequired && s.workItems[idx].ValidatedRevision != nil {
			s.workItems[idx].ValidatedRevision = nil
			s.workItems[idx].UpdatedAt = updatedAt
		}
	}
	return nil
}
func (s *teamRepositoryStub) ListWorkItemsByRootTaskID(rootTaskID int) ([]models.TeamWorkItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]models.TeamWorkItem, 0)
	for _, item := range s.workItems {
		if item.RootTaskID == rootTaskID {
			result = append(result, item)
		}
	}
	return result, nil
}
func (s *teamRepositoryStub) ListWorkItemsByTeamID(teamID int, limit int) ([]models.TeamWorkItem, error) {
	result := make([]models.TeamWorkItem, 0)
	for _, item := range s.workItems {
		if item.TeamID == teamID {
			result = append(result, item)
		}
	}
	return result, nil
}
func (s *teamRepositoryStub) UpsertWorkflowPhase(phase *models.TeamWorkflowPhase) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.workflowPhases {
		current := s.workflowPhases[idx]
		if current.RootTaskID == phase.RootTaskID && current.PhaseID == phase.PhaseID && current.PlanVersion == phase.PlanVersion {
			s.workflowPhases[idx] = *phase
			return nil
		}
	}
	clone := *phase
	clone.ID = len(s.workflowPhases) + 1
	phase.ID = clone.ID
	s.workflowPhases = append(s.workflowPhases, clone)
	return nil
}
func (s *teamRepositoryStub) ListWorkflowPhasesByRootTaskID(rootTaskID int) ([]models.TeamWorkflowPhase, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]models.TeamWorkflowPhase, 0)
	for _, phase := range s.workflowPhases {
		if phase.RootTaskID == rootTaskID {
			result = append(result, phase)
		}
	}
	return result, nil
}
func (s *teamRepositoryStub) AcceptRootCompletion(task *models.TeamTask, expectedLedgerVersion int64, event *models.TeamEvent, outbox *models.TeamEventOutbox) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.tasksByID[task.ID]
	if current != nil && current != task && (current.LedgerVersion != expectedLedgerVersion || current.AcceptedCompletionID != nil || isTerminalTeamTaskStatus(current.Status)) {
		return false, nil
	}
	cloneTask := *task
	if s.tasksByID == nil {
		s.tasksByID = map[int]*models.TeamTask{}
	}
	s.tasksByID[task.ID] = &cloneTask
	s.updatedTask = &cloneTask
	cloneEvent := *event
	cloneEvent.ID = len(s.createdEvents) + 1
	event.ID = cloneEvent.ID
	s.createdEvents = append(s.createdEvents, cloneEvent)
	cloneOutbox := *outbox
	cloneOutbox.ID = len(s.outboxRows) + 1
	outbox.ID = cloneOutbox.ID
	s.outboxRows = append(s.outboxRows, cloneOutbox)
	return true, nil
}
func (s *teamRepositoryStub) ConfirmWorkItemResult(item *models.TeamWorkItem, event *models.TeamEvent, outbox *models.TeamEventOutbox) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	updated := false
	for idx := range s.workItems {
		if s.workItems[idx].TeamID == item.TeamID && s.workItems[idx].RootTaskID == item.RootTaskID && s.workItems[idx].WorkID == item.WorkID {
			s.workItems[idx] = *item
			updated = true
			break
		}
	}
	if !updated {
		s.workItems = append(s.workItems, *item)
	}
	eventExists := false
	for idx := range s.createdEvents {
		current := s.createdEvents[idx]
		if current.TeamID == event.TeamID && current.EventID != nil && *current.EventID == derefTeamString(event.EventID) {
			eventExists = true
			break
		}
	}
	if !eventExists {
		s.createdEvents = append(s.createdEvents, *event)
	}
	for idx := range s.outboxRows {
		if s.outboxRows[idx].TeamID == outbox.TeamID && s.outboxRows[idx].Destination == outbox.Destination && s.outboxRows[idx].MessageID == outbox.MessageID {
			return nil
		}
	}
	clone := *outbox
	clone.ID = len(s.outboxRows) + 1
	outbox.ID = clone.ID
	s.outboxRows = append(s.outboxRows, clone)
	return nil
}
func (s *teamRepositoryStub) ListPendingEventOutbox(now time.Time, limit int) ([]models.TeamEventOutbox, error) {
	result := make([]models.TeamEventOutbox, 0)
	for _, row := range s.outboxRows {
		if row.Status == "pending" && !row.AvailableAt.After(now) {
			result = append(result, row)
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}
func (s *teamRepositoryStub) CreateEventOutbox(outbox *models.TeamEventOutbox) error {
	if outbox == nil {
		return nil
	}
	for idx := range s.outboxRows {
		if s.outboxRows[idx].TeamID == outbox.TeamID && s.outboxRows[idx].MessageID == outbox.MessageID {
			return nil
		}
	}
	clone := *outbox
	clone.ID = len(s.outboxRows) + 1
	outbox.ID = clone.ID
	s.outboxRows = append(s.outboxRows, clone)
	return nil
}
func (s *teamRepositoryStub) MarkEventOutboxDelivered(id int, deliveredAt time.Time) error {
	for idx := range s.outboxRows {
		if s.outboxRows[idx].ID == id {
			s.outboxRows[idx].Status = "delivered"
			s.outboxRows[idx].DeliveredAt = &deliveredAt
		}
	}
	return nil
}
func (s *teamRepositoryStub) MarkEventOutboxFailed(id int, availableAt time.Time, cause string) error {
	for idx := range s.outboxRows {
		if s.outboxRows[idx].ID == id {
			s.outboxRows[idx].Status = "pending"
			s.outboxRows[idx].Attempts++
			s.outboxRows[idx].AvailableAt = availableAt
			s.outboxRows[idx].LastError = &cause
		}
	}
	return nil
}
