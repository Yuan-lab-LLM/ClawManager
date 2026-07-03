package services

import (
	"context"
	"errors"
	"os"
	"path"
	"strings"
	"testing"

	"clawreef/internal/models"
)

func TestInstanceServiceCreateV2CreatesWorkspaceOnly(t *testing.T) {
	workspaceRoot := strings.ReplaceAll(t.TempDir(), "\\", "/")
	instanceRepo := newV2LifecycleInstanceRepo()
	service := &instanceService{
		instanceRepo:  instanceRepo,
		quotaRepo:     v2LifecycleQuotaRepo{},
		workspaceRoot: workspaceRoot,
	}

	instance, err := service.Create(45, CreateInstanceRequest{
		Name:      "OpenClaw Dev",
		Type:      " OpenClaw ",
		CPUCores:  2,
		MemoryGB:  4,
		DiskGB:    20,
		OSType:    "openclaw",
		OSVersion: "latest",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	expectedWorkspacePath := RuntimeWorkspacePathWithRoot(workspaceRoot, "openclaw", 45, instance.ID)
	if instance.Type != "openclaw" {
		t.Fatalf("instance type = %q, want openclaw", instance.Type)
	}
	if instance.RuntimeType != "gateway" {
		t.Fatalf("runtime type = %q, want gateway", instance.RuntimeType)
	}
	if instance.InstanceMode != InstanceModeLite {
		t.Fatalf("instance mode = %q, want lite", instance.InstanceMode)
	}
	if instance.Status != "creating" {
		t.Fatalf("status = %q, want creating", instance.Status)
	}
	if instance.RuntimeGeneration != 1 {
		t.Fatalf("runtime generation = %d, want 1", instance.RuntimeGeneration)
	}
	if instance.WorkspacePath == nil || *instance.WorkspacePath != expectedWorkspacePath {
		t.Fatalf("workspace path = %v, want %q", instance.WorkspacePath, expectedWorkspacePath)
	}
	if _, err := os.Stat(expectedWorkspacePath); err != nil {
		t.Fatalf("expected workspace directory to exist: %v", err)
	}
	if os.PathSeparator == '/' {
		assertDirMode(t, path.Dir(path.Dir(expectedWorkspacePath)), 0711)
		assertDirMode(t, path.Dir(expectedWorkspacePath), 0711)
		assertDirMode(t, expectedWorkspacePath, 0750)
	}
	if instance.PodName != nil || instance.PodNamespace != nil || instance.PodIP != nil {
		t.Fatalf("V2 create must not set per-instance pod fields: %#v", instance)
	}
	if len(instanceRepo.created) != 1 {
		t.Fatalf("created instance records = %d, want 1", len(instanceRepo.created))
	}
}

func TestInstanceServiceCreateV2PersistsAgentTokensAndSnapshot(t *testing.T) {
	workspaceRoot := strings.ReplaceAll(t.TempDir(), "\\", "/")
	instanceRepo := newV2LifecycleInstanceRepo()
	configService := &liteOpenClawConfigStub{
		snapshot: &models.OpenClawInjectionSnapshot{ID: 99, UserID: 45},
	}
	service := &instanceService{
		instanceRepo:          instanceRepo,
		quotaRepo:             v2LifecycleQuotaRepo{},
		workspaceRoot:         workspaceRoot,
		openClawConfigService: configService,
	}

	instance, err := service.Create(45, CreateInstanceRequest{
		Name:      "Lite With Skills",
		Type:      "openclaw",
		Mode:      InstanceModeLite,
		CPUCores:  2,
		MemoryGB:  4,
		DiskGB:    20,
		OSType:    "openclaw",
		OSVersion: "latest",
		OpenClawConfigPlan: &OpenClawConfigPlan{
			Mode:        OpenClawConfigPlanModeManual,
			ResourceIDs: []int{7},
		},
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if instance.AccessToken == nil || strings.TrimSpace(*instance.AccessToken) == "" {
		t.Fatalf("expected lite instance gateway token to be provisioned")
	}
	if instance.AgentBootstrapToken == nil || strings.TrimSpace(*instance.AgentBootstrapToken) == "" {
		t.Fatalf("expected lite instance agent bootstrap token to be provisioned")
	}
	if instance.OpenClawConfigSnapshotID == nil || *instance.OpenClawConfigSnapshotID != 99 {
		t.Fatalf("snapshot id = %v, want 99", instance.OpenClawConfigSnapshotID)
	}
	if !configService.activated {
		t.Fatalf("expected lite bootstrap snapshot to be marked active")
	}
	persisted := instanceRepo.byID[instance.ID]
	if persisted == nil || persisted.AgentBootstrapToken == nil || persisted.OpenClawConfigSnapshotID == nil {
		t.Fatalf("expected token and snapshot reference to be persisted, got %#v", persisted)
	}
}

func TestBuildGatewayEnvInjectsLiteAgentAndBootstrapEnv(t *testing.T) {
	t.Setenv("CLAWMANAGER_LLM_GATEWAY_BASE_URL", "http://gateway.example/api/v1/gateway/llm")
	t.Setenv("CLAWMANAGER_AGENT_CONTROL_BASE_URL", "http://agent-control.example")

	accessToken := "igt_test_token"
	agentToken := "agt_boot_test_token"
	snapshotID := 12
	workspacePath := "/workspaces/openclaw/user-45/instance-88"
	service := &instanceService{
		llmModelRepo: &stubLLMModelRepository{active: []models.LLMModel{{DisplayName: "auto"}}},
		openClawConfigService: &liteOpenClawConfigStub{runtimeEnv: map[string]string{
			OpenClawSkillsEnv: `{"schemaVersion":1,"items":[{"key":"paper-ranker"}]}`,
		}},
	}

	env, err := service.BuildGatewayEnv(&models.Instance{
		ID:                       88,
		UserID:                   45,
		Type:                     "openclaw",
		RuntimeType:              RuntimeBackendGateway,
		InstanceMode:             InstanceModeLite,
		AccessToken:              &accessToken,
		AgentBootstrapToken:      &agentToken,
		OpenClawConfigSnapshotID: &snapshotID,
		WorkspacePath:            &workspacePath,
		DiskGB:                   20,
	})
	if err != nil {
		t.Fatalf("BuildGatewayEnv returned error: %v", err)
	}

	if env["CLAWMANAGER_AGENT_ENABLED"] != "true" || env["CLAWMANAGER_AGENT_BOOTSTRAP_TOKEN"] != agentToken {
		t.Fatalf("expected lite agent env to be injected, got %#v", env)
	}
	wantPersistentDir := path.Join(workspacePath, "home", ".openclaw")
	if env["CLAWMANAGER_AGENT_PERSISTENT_DIR"] != wantPersistentDir {
		t.Fatalf("persistent dir = %q, want %q", env["CLAWMANAGER_AGENT_PERSISTENT_DIR"], wantPersistentDir)
	}
	if !strings.Contains(env[OpenClawSkillsEnv], "paper-ranker") {
		t.Fatalf("expected bootstrap skill env to be injected, got %q", env[OpenClawSkillsEnv])
	}
}
func TestBuildGatewayEnvInjectsHermesLitePersistentDir(t *testing.T) {
	t.Setenv("CLAWMANAGER_LLM_GATEWAY_BASE_URL", "http://gateway.example/api/v1/gateway/llm")
	t.Setenv("CLAWMANAGER_AGENT_CONTROL_BASE_URL", "http://agent-control.example")

	accessToken := "igt_test_token"
	agentToken := "agt_boot_test_token"
	workspacePath := "/workspaces/hermes/user-45/instance-90"
	service := &instanceService{
		llmModelRepo:          &stubLLMModelRepository{active: []models.LLMModel{{DisplayName: "auto"}}},
		openClawConfigService: &liteOpenClawConfigStub{},
	}

	env, err := service.BuildGatewayEnv(&models.Instance{
		ID:                  90,
		UserID:              45,
		Type:                "hermes",
		RuntimeType:         RuntimeBackendGateway,
		InstanceMode:        InstanceModeLite,
		AccessToken:         &accessToken,
		AgentBootstrapToken: &agentToken,
		WorkspacePath:       &workspacePath,
		DiskGB:              20,
	})
	if err != nil {
		t.Fatalf("BuildGatewayEnv returned error: %v", err)
	}

	wantPersistentDir := path.Join(workspacePath, "home", ".hermes")
	if env["CLAWMANAGER_AGENT_PERSISTENT_DIR"] != wantPersistentDir {
		t.Fatalf("persistent dir = %q, want %q", env["CLAWMANAGER_AGENT_PERSISTENT_DIR"], wantPersistentDir)
	}
}
func TestInstanceServiceCreateLiteSkipsPerInstanceResourceQuota(t *testing.T) {
	workspaceRoot := strings.ReplaceAll(t.TempDir(), "\\", "/")
	instanceRepo := newV2LifecycleInstanceRepo()
	service := &instanceService{
		instanceRepo:  instanceRepo,
		quotaRepo:     fixedV2QuotaRepo{cpu: 1, memory: 1, storage: 10, gpu: 0},
		workspaceRoot: workspaceRoot,
	}

	instance, err := service.Create(45, CreateInstanceRequest{
		Name:         "Lite Gateway",
		Type:         "openclaw",
		Mode:         InstanceModeLite,
		RuntimeType:  RuntimeBackendGateway,
		CPUCores:     8,
		MemoryGB:     32,
		DiskGB:       200,
		GPUEnabled:   true,
		GPUCount:     1,
		OSType:       "openclaw",
		OSVersion:    "latest",
		StorageClass: "manual",
	})
	if err != nil {
		t.Fatalf("Create lite returned error: %v", err)
	}
	if instance.InstanceMode != InstanceModeLite || instance.RuntimeType != RuntimeBackendGateway {
		t.Fatalf("created runtime = mode %q type %q, want lite gateway", instance.InstanceMode, instance.RuntimeType)
	}
}

func TestInstanceServiceCreateProEnforcesPerInstanceResourceQuota(t *testing.T) {
	instanceRepo := newV2LifecycleInstanceRepo()
	service := &instanceService{
		instanceRepo:          instanceRepo,
		quotaRepo:             fixedV2QuotaRepo{cpu: 1, memory: 1, storage: 10, gpu: 0},
		pvcService:            nil,
		deploymentService:     nil,
		serviceService:        nil,
		networkPolicyService:  nil,
		openClawConfigService: nil,
	}

	_, err := service.Create(45, CreateInstanceRequest{
		Name:        "Pro Desktop",
		Type:        "openclaw",
		Mode:        InstanceModePro,
		RuntimeType: RuntimeBackendDesktop,
		CPUCores:    8,
		MemoryGB:    32,
		DiskGB:      200,
		OSType:      "openclaw",
		OSVersion:   "latest",
	})
	if err == nil || !strings.Contains(err.Error(), "CPU cores exceed quota") {
		t.Fatalf("Create pro error = %v, want CPU quota failure", err)
	}
}

func assertDirMode(t *testing.T, dir string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat %s: %v", dir, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode %s = %04o, want %04o", dir, got, want)
	}
}

func TestInstanceServiceStartV2MarksCreatingWithNextGeneration(t *testing.T) {
	workspacePath := "/workspaces/hermes/user-45/instance-77"
	instanceRepo := newV2LifecycleInstanceRepo()
	instanceRepo.byID[77] = &models.Instance{
		ID:                77,
		UserID:            45,
		Type:              "hermes",
		RuntimeType:       "gateway",
		Status:            "stopped",
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 4,
	}
	service := &instanceService{instanceRepo: instanceRepo}

	if err := service.Start(77); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	state := instanceRepo.runtimeStates[77]
	if state.status != "creating" || state.generation != 5 {
		t.Fatalf("runtime state = %#v, want creating generation 5", state)
	}
}

func TestInstanceServiceStopV2DeletesGatewayBindingAndReleasesSlot(t *testing.T) {
	workspacePath := "/workspaces/openclaw/user-45/instance-88"
	instanceRepo := newV2LifecycleInstanceRepo()
	instanceRepo.byID[88] = &models.Instance{
		ID:                88,
		UserID:            45,
		Type:              "openclaw",
		RuntimeType:       "gateway",
		Status:            "running",
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 3,
	}
	endpoint := "http://agent.local:19090"
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			9: {ID: 9, AgentEndpoint: &endpoint},
		},
	}
	bindingRepo := newFakeRuntimeBindingRepo()
	bindingRepo.bindings[88] = &models.InstanceRuntimeBinding{
		InstanceID:   88,
		RuntimePodID: 9,
		GatewayID:    "gw-88",
		GatewayPort:  20018,
		State:        "running",
		Generation:   3,
	}
	agent := &fakeRuntimeAgentClient{}
	service := &instanceService{
		instanceRepo:   instanceRepo,
		runtimePodRepo: podRepo,
		bindingRepo:    bindingRepo,
		agentClient:    agent,
		workspaceRoot:  "/workspaces",
	}

	if err := service.Stop(88); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	if len(agent.deleteRequests) != 1 || agent.deleteRequests[0].endpoint != endpoint || agent.deleteRequests[0].gatewayID != "gw-88" {
		t.Fatalf("delete gateway requests = %#v", agent.deleteRequests)
	}
	if bindingRepo.deleteAndReleaseCalls[88] != 1 {
		t.Fatalf("binding delete and release calls = %d, want 1", bindingRepo.deleteAndReleaseCalls[88])
	}
	state := instanceRepo.runtimeStates[88]
	if state.status != "stopped" || state.generation != 3 {
		t.Fatalf("runtime state = %#v, want stopped generation 3", state)
	}
}

func TestInstanceServiceStopV2KeepsBindingWhenAgentDeleteFails(t *testing.T) {
	workspacePath := "/workspaces/openclaw/user-45/instance-188"
	instanceRepo := newV2LifecycleInstanceRepo()
	instanceRepo.byID[188] = &models.Instance{
		ID:                188,
		UserID:            45,
		Type:              "openclaw",
		RuntimeType:       "gateway",
		Status:            "running",
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 6,
	}
	endpoint := "http://agent.local:19090"
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			19: {ID: 19, AgentEndpoint: &endpoint},
		},
	}
	bindingRepo := newFakeRuntimeBindingRepo()
	bindingRepo.bindings[188] = &models.InstanceRuntimeBinding{
		InstanceID:   188,
		RuntimePodID: 19,
		GatewayID:    "gw-188",
		GatewayPort:  20018,
		State:        "running",
		Generation:   6,
	}
	agent := &fakeRuntimeAgentClient{deleteErr: errors.New("agent delete failed")}
	service := &instanceService{
		instanceRepo:   instanceRepo,
		runtimePodRepo: podRepo,
		bindingRepo:    bindingRepo,
		agentClient:    agent,
	}

	err := service.Stop(188)
	if err == nil || !strings.Contains(err.Error(), "failed to delete v2 gateway") {
		t.Fatalf("Stop error = %v, want gateway delete failure", err)
	}
	if bindingRepo.bindings[188] == nil {
		t.Fatal("binding was deleted after gateway delete failed")
	}
	if bindingRepo.deleteAndReleaseCalls[188] != 0 {
		t.Fatalf("delete and release calls = %d, want 0", bindingRepo.deleteAndReleaseCalls[188])
	}
	state := instanceRepo.runtimeStates[188]
	if state.status != "stopped" || state.generation != 6 {
		t.Fatalf("runtime state = %#v, want stopped generation 6", state)
	}
}

func TestInstanceServiceStopV2KeepsBindingWhenRuntimePodLookupFails(t *testing.T) {
	workspacePath := "/workspaces/openclaw/user-45/instance-189"
	instanceRepo := newV2LifecycleInstanceRepo()
	instanceRepo.byID[189] = &models.Instance{
		ID:                189,
		UserID:            45,
		Type:              "openclaw",
		RuntimeType:       "gateway",
		Status:            "running",
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 6,
	}
	bindingRepo := newFakeRuntimeBindingRepo()
	bindingRepo.bindings[189] = &models.InstanceRuntimeBinding{
		InstanceID:   189,
		RuntimePodID: 29,
		GatewayID:    "gw-189",
		GatewayPort:  20019,
		State:        "running",
		Generation:   6,
	}
	service := &instanceService{
		instanceRepo:   instanceRepo,
		runtimePodRepo: &fakeRuntimePodRepo{getErr: errors.New("pod lookup failed")},
		bindingRepo:    bindingRepo,
		agentClient:    &fakeRuntimeAgentClient{},
	}

	err := service.Stop(189)
	if err == nil || !strings.Contains(err.Error(), "failed to get runtime pod") {
		t.Fatalf("Stop error = %v, want pod lookup failure", err)
	}
	if bindingRepo.bindings[189] == nil {
		t.Fatal("binding was deleted after pod lookup failed")
	}
	if bindingRepo.deleteAndReleaseCalls[189] != 0 {
		t.Fatalf("delete and release calls = %d, want 0", bindingRepo.deleteAndReleaseCalls[189])
	}
	state := instanceRepo.runtimeStates[189]
	if state.status != "stopped" || state.generation != 6 {
		t.Fatalf("runtime state = %#v, want stopped generation 6", state)
	}
}

func TestInstanceServiceRestartV2RecreatesGatewayViaScheduler(t *testing.T) {
	workspacePath := "/workspaces/openclaw/user-45/instance-89"
	instanceRepo := newV2LifecycleInstanceRepo()
	instanceRepo.byID[89] = &models.Instance{
		ID:                89,
		UserID:            45,
		Type:              "openclaw",
		RuntimeType:       "gateway",
		Status:            "running",
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 8,
	}
	endpoint := "http://agent.local:19090"
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			11: {ID: 11, AgentEndpoint: &endpoint},
		},
	}
	bindingRepo := newFakeRuntimeBindingRepo()
	bindingRepo.bindings[89] = &models.InstanceRuntimeBinding{
		InstanceID:   89,
		RuntimePodID: 11,
		GatewayID:    "gw-89",
		GatewayPort:  20019,
		State:        "running",
		Generation:   8,
	}
	agent := &fakeRuntimeAgentClient{}
	service := &instanceService{
		instanceRepo:   instanceRepo,
		runtimePodRepo: podRepo,
		bindingRepo:    bindingRepo,
		agentClient:    agent,
	}

	if err := service.Restart(89); err != nil {
		t.Fatalf("Restart returned error: %v", err)
	}

	if len(agent.deleteRequests) != 1 || agent.deleteRequests[0].gatewayID != "gw-89" {
		t.Fatalf("delete gateway requests = %#v", agent.deleteRequests)
	}
	if bindingRepo.deleteAndReleaseCalls[89] != 1 {
		t.Fatalf("binding delete and release calls = %d, want 1", bindingRepo.deleteAndReleaseCalls[89])
	}
	state := instanceRepo.runtimeStates[89]
	if state.status != "creating" || state.generation != 9 {
		t.Fatalf("runtime state = %#v, want creating generation 9", state)
	}
}

func TestInstanceServiceDeleteV2MissingBindingStillDeletesInstance(t *testing.T) {
	workspacePath := "/workspaces/hermes/user-45/instance-90"
	instanceRepo := newV2LifecycleInstanceRepo()
	instanceRepo.byID[90] = &models.Instance{
		ID:                90,
		UserID:            45,
		Type:              "hermes",
		RuntimeType:       "gateway",
		Status:            "stopped",
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 2,
	}
	service := &instanceService{
		instanceRepo:   instanceRepo,
		runtimePodRepo: &fakeRuntimePodRepo{},
		bindingRepo:    newFakeRuntimeBindingRepo(),
		agentClient:    &fakeRuntimeAgentClient{},
	}

	if err := service.Delete(90); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if len(instanceRepo.deleted) != 1 || instanceRepo.deleted[0] != 90 {
		t.Fatalf("deleted instances = %#v, want [90]", instanceRepo.deleted)
	}
}

func TestInstanceServiceDeleteV2KeepsInstanceAndBindingWhenAgentDeleteFails(t *testing.T) {
	workspacePath := "/workspaces/hermes/user-45/instance-190"
	instanceRepo := newV2LifecycleInstanceRepo()
	instanceRepo.byID[190] = &models.Instance{
		ID:                190,
		UserID:            45,
		Type:              "hermes",
		RuntimeType:       "gateway",
		Status:            "running",
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 2,
	}
	endpoint := "http://agent.local:19090"
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			39: {ID: 39, AgentEndpoint: &endpoint},
		},
	}
	bindingRepo := newFakeRuntimeBindingRepo()
	bindingRepo.bindings[190] = &models.InstanceRuntimeBinding{
		InstanceID:   190,
		RuntimePodID: 39,
		GatewayID:    "gw-190",
		GatewayPort:  20090,
		State:        "running",
		Generation:   2,
	}
	service := &instanceService{
		instanceRepo:   instanceRepo,
		runtimePodRepo: podRepo,
		bindingRepo:    bindingRepo,
		agentClient:    &fakeRuntimeAgentClient{deleteErr: errors.New("agent delete failed")},
	}

	err := service.Delete(190)
	if err == nil || !strings.Contains(err.Error(), "failed to delete v2 gateway") {
		t.Fatalf("Delete error = %v, want gateway delete failure", err)
	}
	if len(instanceRepo.deleted) != 0 {
		t.Fatalf("deleted instances = %#v, want none", instanceRepo.deleted)
	}
	if instanceRepo.byID[190] == nil {
		t.Fatal("instance was removed after cleanup failure")
	}
	if bindingRepo.bindings[190] == nil {
		t.Fatal("binding was removed after cleanup failure")
	}
	if bindingRepo.deleteAndReleaseCalls[190] != 0 {
		t.Fatalf("delete and release calls = %d, want 0", bindingRepo.deleteAndReleaseCalls[190])
	}
}

func TestInstanceServiceV2StatusRequiresRunningBindingAndPodIP(t *testing.T) {
	workspacePath := "/workspaces/openclaw/user-45/instance-92"
	instanceRepo := newV2LifecycleInstanceRepo()
	instanceRepo.byID[92] = &models.Instance{
		ID:                  92,
		UserID:              45,
		Type:                "openclaw",
		RuntimeType:         "gateway",
		Status:              "running",
		WorkspacePath:       &workspacePath,
		WorkspaceUsageBytes: 2048,
		RuntimeGeneration:   1,
	}
	bindingRepo := newFakeRuntimeBindingRepo()
	bindingRepo.bindings[92] = &models.InstanceRuntimeBinding{
		InstanceID:   92,
		RuntimePodID: 12,
		GatewayPort:  20020,
		State:        "running",
	}
	service := &instanceService{
		instanceRepo:   instanceRepo,
		runtimePodRepo: &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{12: {ID: 12}}},
		bindingRepo:    bindingRepo,
	}

	status, err := service.GetInstanceStatus(92)
	if err != nil {
		t.Fatalf("GetInstanceStatus returned error: %v", err)
	}
	if status.Availability != "unavailable" {
		t.Fatalf("availability without pod IP = %q, want unavailable", status.Availability)
	}

	podIP := "10.42.0.92"
	service.runtimePodRepo = &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{12: {ID: 12, PodIP: &podIP}}}
	status, err = service.GetInstanceStatus(92)
	if err != nil {
		t.Fatalf("GetInstanceStatus returned error after pod IP: %v", err)
	}
	if status.Availability != "available" {
		t.Fatalf("availability with running binding and pod IP = %q, want available", status.Availability)
	}
}

func TestSyncInstanceSkipsV2RuntimeGatewayInstances(t *testing.T) {
	workspacePath := "/workspaces/openclaw/user-45/instance-91"
	instanceRepo := newV2LifecycleInstanceRepo()
	service := &SyncService{instanceRepo: instanceRepo}
	instance := &models.Instance{
		ID:            91,
		UserID:        45,
		Type:          "openclaw",
		RuntimeType:   "gateway",
		InstanceMode:  InstanceModeLite,
		Status:        "running",
		WorkspacePath: &workspacePath,
	}

	service.syncInstance(context.Background(), instance)

	if len(instanceRepo.updated) != 0 {
		t.Fatalf("sync should not update V2 gateway instance via pod status, got %#v", instanceRepo.updated)
	}
}

func TestV2RuntimeTypeForInstanceRequiresLiteGatewayBoundary(t *testing.T) {
	workspacePath := "/workspaces/openclaw/user-45/instance-191"
	lite := &models.Instance{
		ID:            191,
		Type:          "openclaw",
		RuntimeType:   RuntimeBackendGateway,
		InstanceMode:  InstanceModeLite,
		WorkspacePath: &workspacePath,
	}
	if runtimeType, ok := v2RuntimeTypeForInstance(lite); !ok || runtimeType != RuntimeTypeOpenClaw {
		t.Fatalf("lite gateway instance was not recognized: %q %v", runtimeType, ok)
	}

	pro := &models.Instance{
		ID:            192,
		Type:          "openclaw",
		RuntimeType:   RuntimeBackendDesktop,
		InstanceMode:  InstanceModePro,
		WorkspacePath: &workspacePath,
	}
	if runtimeType, ok := v2RuntimeTypeForInstance(pro); ok {
		t.Fatalf("pro desktop instance must not be scheduler-managed, got %q", runtimeType)
	}
}

func TestInstanceModeCapacityCanDisablePro(t *testing.T) {
	t.Setenv("CLAWMANAGER_PRO_CAPACITY", "0")
	service := &instanceService{instanceRepo: newV2LifecycleInstanceRepo()}

	err := service.enforceInstanceModeLimits(context.Background(), InstanceModePro, 2, 4, 20, 0)
	if err == nil || !strings.Contains(err.Error(), "pro instance mode is disabled") {
		t.Fatalf("capacity error = %v, want pro disabled", err)
	}
}

func TestInstanceModeResourceLimitRejectsOversizedPro(t *testing.T) {
	t.Setenv("CLAWMANAGER_PRO_MAX_CPU_CORES", "1.5")
	service := &instanceService{instanceRepo: newV2LifecycleInstanceRepo()}

	err := service.enforceInstanceModeLimits(context.Background(), InstanceModePro, 2, 4, 20, 0)
	if err == nil || !strings.Contains(err.Error(), "pro CPU cores exceed mode limit") {
		t.Fatalf("resource limit error = %v, want pro CPU limit", err)
	}
}

func TestInstanceModeResourceLimitAllowsOversizedLite(t *testing.T) {
	t.Setenv("CLAWMANAGER_LITE_MAX_CPU_CORES", "1.5")
	service := &instanceService{instanceRepo: newV2LifecycleInstanceRepo()}

	err := service.enforceInstanceModeLimits(context.Background(), InstanceModeLite, 2, 4, 20, 0)
	if err != nil {
		t.Fatalf("resource limit error = %v, want lite to skip dedicated resource limits", err)
	}
}

type liteOpenClawConfigStub struct {
	OpenClawConfigService
	snapshot   *models.OpenClawInjectionSnapshot
	runtimeEnv map[string]string
	activated  bool
	failed     bool
}

func (s *liteOpenClawConfigStub) CreateSnapshotForInstance(userID int, instance *models.Instance, plan *OpenClawConfigPlan) (*models.OpenClawInjectionSnapshot, error) {
	if s.snapshot == nil {
		return nil, nil
	}
	return s.snapshot, nil
}

func (s *liteOpenClawConfigStub) MarkSnapshotActive(snapshot *models.OpenClawInjectionSnapshot) error {
	s.activated = true
	if snapshot != nil {
		snapshot.Status = openClawActiveSnapshotStatus
	}
	return nil
}

func (s *liteOpenClawConfigStub) MarkSnapshotFailed(snapshot *models.OpenClawInjectionSnapshot, err error) error {
	s.failed = true
	return nil
}

func (s *liteOpenClawConfigStub) RuntimeEnvForSnapshot(userID int, instanceType string, snapshotID int) (map[string]string, error) {
	result := map[string]string{}
	for key, value := range s.runtimeEnv {
		result[key] = value
	}
	return result, nil
}

type v2LifecycleInstanceRepo struct {
	byID          map[int]*models.Instance
	created       []models.Instance
	updated       []models.Instance
	deleted       []int
	workspacePath map[int]string
	runtimeStates map[int]v2RuntimeStateRecord
	nextID        int
}

type v2RuntimeStateRecord struct {
	status     string
	generation int
	message    *string
}

func newV2LifecycleInstanceRepo() *v2LifecycleInstanceRepo {
	return &v2LifecycleInstanceRepo{
		byID:          map[int]*models.Instance{},
		workspacePath: map[int]string{},
		runtimeStates: map[int]v2RuntimeStateRecord{},
		nextID:        1,
	}
}

func (r *v2LifecycleInstanceRepo) Create(instance *models.Instance) error {
	if instance.ID == 0 {
		instance.ID = r.nextID
		r.nextID++
	}
	copy := *instance
	r.byID[instance.ID] = &copy
	r.created = append(r.created, copy)
	return nil
}

func (r *v2LifecycleInstanceRepo) GetByID(id int) (*models.Instance, error) {
	return r.byID[id], nil
}

func (r *v2LifecycleInstanceRepo) GetByAccessToken(string) (*models.Instance, error) {
	return nil, nil
}

func (r *v2LifecycleInstanceRepo) GetByAgentBootstrapToken(string) (*models.Instance, error) {
	return nil, nil
}

func (r *v2LifecycleInstanceRepo) GetAll(offset, limit int) ([]models.Instance, error) {
	return nil, nil
}

func (r *v2LifecycleInstanceRepo) CountAll() (int, error) { return len(r.byID), nil }

func (r *v2LifecycleInstanceRepo) GetByUserID(userID int, offset, limit int) ([]models.Instance, error) {
	instances := make([]models.Instance, 0)
	for _, instance := range r.byID {
		if instance.UserID == userID {
			instances = append(instances, *instance)
		}
	}
	return instances, nil
}

func (r *v2LifecycleInstanceRepo) CountByUserID(userID int) (int, error) {
	count := 0
	for _, instance := range r.byID {
		if instance.UserID == userID {
			count++
		}
	}
	return count, nil
}

func (r *v2LifecycleInstanceRepo) CountActiveByMode(ctx context.Context, mode string) (int, error) {
	count := 0
	for _, instance := range r.byID {
		if instance.InstanceMode == mode && (instance.Status == "creating" || instance.Status == "running") {
			count++
		}
	}
	return count, nil
}

func (r *v2LifecycleInstanceRepo) ExistsByUserIDAndName(userID int, name string) (bool, error) {
	normalized := strings.TrimSpace(strings.ToLower(name))
	for _, instance := range r.byID {
		if instance.UserID == userID && strings.TrimSpace(strings.ToLower(instance.Name)) == normalized {
			return true, nil
		}
	}
	return false, nil
}

func (r *v2LifecycleInstanceRepo) GetAllRunning() ([]models.Instance, error) {
	instances := make([]models.Instance, 0, len(r.byID))
	for _, instance := range r.byID {
		instances = append(instances, *instance)
	}
	return instances, nil
}

func (r *v2LifecycleInstanceRepo) GetV2DesiredRunning(context.Context, int) ([]models.Instance, error) {
	return nil, nil
}

func (r *v2LifecycleInstanceRepo) GetV2Creating(context.Context, int) ([]models.Instance, error) {
	return nil, nil
}

func (r *v2LifecycleInstanceRepo) UpdateRuntimeState(ctx context.Context, id int, status string, generation int, message *string) error {
	instance := r.byID[id]
	if instance != nil {
		instance.Status = status
		instance.RuntimeGeneration = generation
		instance.RuntimeErrorMessage = message
	}
	r.runtimeStates[id] = v2RuntimeStateRecord{status: status, generation: generation, message: message}
	return nil
}

func (r *v2LifecycleInstanceRepo) SetWorkspacePath(ctx context.Context, id int, workspacePath string) error {
	instance := r.byID[id]
	if instance != nil {
		instance.WorkspacePath = &workspacePath
	}
	r.workspacePath[id] = workspacePath
	return nil
}

func (r *v2LifecycleInstanceRepo) UpdateWorkspaceUsage(context.Context, int, int64) error {
	return nil
}

func (r *v2LifecycleInstanceRepo) Update(instance *models.Instance) error {
	copy := *instance
	r.byID[instance.ID] = &copy
	r.updated = append(r.updated, copy)
	return nil
}

func (r *v2LifecycleInstanceRepo) Delete(id int) error {
	delete(r.byID, id)
	r.deleted = append(r.deleted, id)
	return nil
}

type v2LifecycleQuotaRepo struct{}

func (v2LifecycleQuotaRepo) Create(*models.UserQuota) error { return nil }
func (v2LifecycleQuotaRepo) GetByUserID(userID int) (*models.UserQuota, error) {
	return &models.UserQuota{
		UserID:       userID,
		MaxInstances: 100,
		MaxCPUCores:  100,
		MaxMemoryGB:  1000,
		MaxStorageGB: 1000,
		MaxGPUCount:  8,
	}, nil
}
func (v2LifecycleQuotaRepo) Update(*models.UserQuota) error { return nil }
func (v2LifecycleQuotaRepo) DeleteByUserID(int) error       { return nil }
func (v2LifecycleQuotaRepo) CreateDefaultQuota(userID int) (*models.UserQuota, error) {
	return v2LifecycleQuotaRepo{}.GetByUserID(userID)
}

type fixedV2QuotaRepo struct {
	cpu     float64
	memory  int
	storage int
	gpu     int
}

func (fixedV2QuotaRepo) Create(*models.UserQuota) error { return nil }
func (r fixedV2QuotaRepo) GetByUserID(userID int) (*models.UserQuota, error) {
	return &models.UserQuota{
		UserID:       userID,
		MaxInstances: 100,
		MaxCPUCores:  r.cpu,
		MaxMemoryGB:  r.memory,
		MaxStorageGB: r.storage,
		MaxGPUCount:  r.gpu,
	}, nil
}
func (fixedV2QuotaRepo) Update(*models.UserQuota) error { return nil }
func (fixedV2QuotaRepo) DeleteByUserID(int) error       { return nil }
func (r fixedV2QuotaRepo) CreateDefaultQuota(userID int) (*models.UserQuota, error) {
	return r.GetByUserID(userID)
}
