package services

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
		"message": "worker 正在整理结果",
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
	} {
		if !strings.Contains(bootstrap, expected) {
			t.Fatalf("bootstrap contract missing %q: %s", expected, bootstrap)
		}
	}
	for _, forbidden := range []string{"For multi-member Teams", "workers report deliverables back to the Leader"} {
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
		"`team Software Engineering Team`",
		"Redis Team成员构成",
		"运行状态与技术能力边界",
		"协作与通信机制(team_send)",
		"任务流转方式",
		"消息同步方式",
		"上下文共享方式",
		"可调用的方法、工具与操作能力",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("bootstrap prompt missing %q: %s", expected, prompt)
		}
	}
}

func TestBuildTeamTaskEnvelopeIncludesCompletionContract(t *testing.T) {
	task := &models.TeamTask{ID: 67}
	payload := map[string]interface{}{
		"intent": "team_bootstrap_introduction",
		"title":  "Introduce the team",
		"prompt": "Generate the team report.",
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
	resultSink, ok := envelope["resultSink"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected resultSink map, got %#v", envelope["resultSink"])
	}
	if resultSink["type"] != "redis_stream" || resultSink["eventsKey"] != "claw:team:31:events" {
		t.Fatalf("unexpected resultSink: %#v", resultSink)
	}
	if resultSink["successEvent"] != "task_completed" || resultSink["failureEvent"] != "task_failed" {
		t.Fatalf("unexpected resultSink events: %#v", resultSink)
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
			"【任务分派 · 自我介绍】",
			"",
			"designer 你好，我是 Leader。用户想让你做一段自我介绍。",
			"",
			"请写一段自我介绍，内容包括你的角色身份、能力范围和工作风格。",
			"完成后将内容写入共享目录，并回传结果给我。",
			"",
			"共享目录：$CLAWMANAGER_TEAM_SHARED_DIR/results/team-31-task-169/",
			"规范路径：/team/results/team-31-task-169/intro-designer.md",
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
	if got, _ := step["content"].(string); !strings.Contains(got, "designer 你好") {
		t.Fatalf("expected assignment content preserved, got %#v", got)
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
	if len(repo.createdEvents) != 1 || repo.createdEvents[0].EventType != "reply" {
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
	if len(repo.createdEvents) != 1 || repo.createdEvents[0].TaskID == nil || *repo.createdEvents[0].TaskID != taskID {
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
	step := stored["collaborationStep"].(map[string]interface{})
	if step["type"] != "result" || step["status"] != models.TeamTaskStatusSucceeded || step["actor"] != "designer" {
		t.Fatalf("expected worker result collaboration step, got %#v", step)
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
	if stored["leaderPrematureCompletion"] != true || stored["rootTaskTerminal"] != false {
		t.Fatalf("expected premature completion marker, got %#v", stored)
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

func TestProjectTeamEventMissingArtifactKeepsFinalAnswerInWarning(t *testing.T) {
	teamID := 22
	taskID := 40
	messageID := "team-22-bootstrap-introduction"
	previouslyFinished := time.Now().UTC().Add(-time.Minute)
	task := &models.TeamTask{ID: taskID, TeamID: teamID, TargetMemberID: 120, MessageID: messageID, Status: models.TeamTaskStatusSucceeded, FinishedAt: &previouslyFinished, UpdatedAt: time.Now().UTC()}
	leader := &models.TeamMember{ID: 120, TeamID: teamID, MemberKey: "leader", Role: "leader", Status: models.TeamMemberStatusBusy, CurrentTaskID: &taskID, Availability: models.TeamMemberAvailabilityBusy}
	repo := &teamRepositoryStub{
		tasksByID:        map[int]*models.TeamTask{taskID: task},
		tasksByMessageID: map[string]*models.TeamTask{messageID: task},
		membersByKey:     map[string]*models.TeamMember{"leader": leader},
	}
	service := &teamService{repo: repo, runtimeWorkspaceRoot: t.TempDir()}
	finalBody := "# Delivery Team\n\nDetailed final answer."
	payloadJSON, err := json.Marshal(map[string]interface{}{
		"event":              "task_completed",
		"memberId":           "leader",
		"messageId":          messageID,
		"status":             "succeeded",
		"summary":            "Team introduction complete.",
		"resultMarkdown":     finalBody,
		"artifactRefs":       []string{"/team/results/team-22-task-40/missing.md"},
		"explicitCompletion": true,
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

type teamRepositoryStub struct {
	teamsByID        map[int]*models.Team
	membersByID      map[int]*models.TeamMember
	membersByKey     map[string]*models.TeamMember
	tasksByID        map[int]*models.TeamTask
	tasksByMessageID map[string]*models.TeamTask
	createdEvents    []models.TeamEvent
	workItems        []models.TeamWorkItem
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
	return nil, nil
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
	clone := *event
	s.createdEvents = append(s.createdEvents, clone)
	return nil
}
func (s *teamRepositoryStub) EventExistsByStreamID(teamID int, streamID string) (bool, error) {
	return false, nil
}
func (s *teamRepositoryStub) EventExistsByEventID(teamID int, eventID string) (bool, error) {
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
	for idx := range s.workItems {
		if s.workItems[idx].TeamID == item.TeamID && s.workItems[idx].RootTaskID == item.RootTaskID && s.workItems[idx].WorkID == item.WorkID {
			s.workItems[idx] = *item
			return nil
		}
	}
	s.workItems = append(s.workItems, *item)
	return nil
}
func (s *teamRepositoryStub) ListWorkItemsByRootTaskID(rootTaskID int) ([]models.TeamWorkItem, error) {
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
