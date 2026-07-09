package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
)

func TestRuntimeSchedulerAssignsCreatingInstanceToReadyPod(t *testing.T) {
	ctx := context.Background()
	endpoint := "http://agent.runtime"
	workspacePath := "/workspaces/openclaw/user-45/instance-17"
	instanceRepo := newFakeRuntimeInstanceRepo()
	instanceRepo.creating = []models.Instance{{
		ID:                17,
		UserID:            45,
		Type:              RuntimeTypeOpenClaw,
		RuntimeType:       RuntimeBackendGateway,
		InstanceMode:      InstanceModeLite,
		Status:            "creating",
		CPUCores:          1.5,
		MemoryGB:          2,
		DiskGB:            8,
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 3,
	}}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			9: {
				ID:            9,
				RuntimeType:   RuntimeTypeOpenClaw,
				AgentEndpoint: &endpoint,
				State:         "ready",
				Capacity:      1,
			},
		},
		schedulable: []models.RuntimePod{{
			ID:            9,
			RuntimeType:   RuntimeTypeOpenClaw,
			AgentEndpoint: &endpoint,
			State:         "ready",
			Capacity:      1,
		}},
	}
	bindingRepo := newFakeRuntimeBindingRepo()
	agent := &fakeRuntimeAgentClient{
		createResponse: &RuntimeAgentCreateGatewayResponse{
			GatewayID: "gw-17",
			Port:      20017,
			Status:    "running",
		},
	}
	events := &fakeRuntimeEventService{}
	scheduler := NewRuntimeScheduler(
		instanceRepo,
		podRepo,
		bindingRepo,
		&fakeRuntimeRolloutRepo{},
		agent,
		events,
		nil,
		&fakeRuntimeDeploymentService{},
		time.Second,
	)

	if err := scheduler.reconcile(ctx); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	if got := len(agent.createRequests); got != 1 {
		t.Fatalf("CreateGateway calls = %d, want 1", got)
	}
	req := agent.createRequests[0]
	if req.endpoint != endpoint {
		t.Fatalf("CreateGateway endpoint = %q, want %q", req.endpoint, endpoint)
	}
	if req.req.WorkspacePath != "/workspaces/openclaw/user-45/instance-17" {
		t.Fatalf("workspace path = %q", req.req.WorkspacePath)
	}
	if req.req.PortRange.Start != 20000 || req.req.PortRange.End != 20099 {
		t.Fatalf("port range = %+v", req.req.PortRange)
	}
	if req.req.UID != RuntimeLinuxID(17) || req.req.GID != RuntimeLinuxID(17) {
		t.Fatalf("linux IDs = %d/%d", req.req.UID, req.req.GID)
	}
	if req.req.MemoryMB != 2048 || req.req.DiskQuotaMB != 8192 {
		t.Fatalf("resource MB = %d/%d", req.req.MemoryMB, req.req.DiskQuotaMB)
	}

	binding := bindingRepo.bindings[17]
	if binding == nil {
		t.Fatal("binding was not created")
	}
	if binding.RuntimePodID != 9 || binding.GatewayPort != 20017 {
		t.Fatalf("binding pod/port = %d/%d", binding.RuntimePodID, binding.GatewayPort)
	}
	if instanceRepo.workspacePaths[17] != "/workspaces/openclaw/user-45/instance-17" {
		t.Fatalf("instance workspace path = %q", instanceRepo.workspacePaths[17])
	}
	state := instanceRepo.runtimeStates[17]
	if state.status != "running" || state.generation != 3 {
		t.Fatalf("runtime state = %+v", state)
	}
	if podRepo.claims[9] != 1 {
		t.Fatalf("pod claims = %d, want 1", podRepo.claims[9])
	}
	if got := len(events.published); got != 1 {
		t.Fatalf("published events = %d, want 1", got)
	}
	if events.published[0].eventType != "runtime.instance.running" {
		t.Fatalf("published event = %q, want runtime.instance.running", events.published[0].eventType)
	}
}

func TestRuntimeSchedulerPassesGatewayEnvironmentToRuntimeAgent(t *testing.T) {
	ctx := context.Background()
	endpoint := "http://agent.runtime"
	token := "igt_instance_68"
	workspacePath := "/workspaces/openclaw/user-1/instance-68"
	instanceRepo := newFakeRuntimeInstanceRepo()
	instanceRepo.creating = []models.Instance{{
		ID:                68,
		UserID:            1,
		Type:              RuntimeTypeOpenClaw,
		RuntimeType:       RuntimeBackendGateway,
		InstanceMode:      InstanceModeLite,
		Status:            "creating",
		AccessToken:       &token,
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 4,
	}}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			10: {
				ID:            10,
				RuntimeType:   RuntimeTypeOpenClaw,
				AgentEndpoint: &endpoint,
				State:         "ready",
				Capacity:      10,
			},
		},
		schedulable: []models.RuntimePod{{
			ID:            10,
			RuntimeType:   RuntimeTypeOpenClaw,
			AgentEndpoint: &endpoint,
			State:         "ready",
			Capacity:      10,
		}},
	}
	agent := &fakeRuntimeAgentClient{
		createResponse: &RuntimeAgentCreateGatewayResponse{
			GatewayID: "gw-68-4",
			Port:      20068,
			Status:    "starting",
		},
	}
	scheduler := NewRuntimeScheduler(
		instanceRepo,
		podRepo,
		newFakeRuntimeBindingRepo(),
		&fakeRuntimeRolloutRepo{},
		agent,
		NewRuntimeEventService(nil),
		nil,
		&fakeRuntimeDeploymentService{},
		time.Second,
		WithRuntimeSchedulerGatewayEnvBuilder(func(instance *models.Instance) (map[string]string, error) {
			if instance == nil || instance.ID != 68 {
				t.Fatalf("gateway env builder received instance %#v, want 68", instance)
			}
			return map[string]string{
				"CLAWMANAGER_LLM_BASE_URL": "http://clawmanager-gateway.clawmanager-system.svc.cluster.local:9001/api/v1/gateway/llm",
				"CLAWMANAGER_LLM_API_KEY":  token,
				"CLAWMANAGER_LLM_MODEL":    `["auto","gpt-5.5"]`,
				"OPENAI_API_KEY":           token,
			}, nil
		}),
	)

	if err := scheduler.reconcile(ctx); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	if got := len(agent.createRequests); got != 1 {
		t.Fatalf("CreateGateway calls = %d, want 1", got)
	}
	env := agent.createRequests[0].req.Environment
	if env["CLAWMANAGER_LLM_API_KEY"] != token || env["OPENAI_API_KEY"] != token {
		t.Fatalf("gateway environment missing instance token aliases: %#v", env)
	}
	if env["CLAWMANAGER_LLM_BASE_URL"] != "http://clawmanager-gateway.clawmanager-system.svc.cluster.local:9001/api/v1/gateway/llm" {
		t.Fatalf("CLAWMANAGER_LLM_BASE_URL = %q", env["CLAWMANAGER_LLM_BASE_URL"])
	}
	if env["CLAWMANAGER_LLM_MODEL"] != `["auto","gpt-5.5"]` {
		t.Fatalf("CLAWMANAGER_LLM_MODEL = %q", env["CLAWMANAGER_LLM_MODEL"])
	}
	state := instanceRepo.runtimeStates[68]
	if state.status != "creating" || state.generation != 4 || state.message == nil || !strings.Contains(*state.message, "gateway starting") {
		t.Fatalf("runtime state after starting gateway = %+v, want creating with startup message", state)
	}
}

func TestRuntimeSchedulerUsesTeamSharedGIDForTeamLiteGateway(t *testing.T) {
	ctx := context.Background()
	endpoint := "http://agent.runtime"
	workspacePath := "/workspaces/openclaw/user-1/instance-130"
	instanceRepo := newFakeRuntimeInstanceRepo()
	instanceRepo.creating = []models.Instance{{
		ID:                130,
		UserID:            1,
		Type:              RuntimeTypeOpenClaw,
		RuntimeType:       RuntimeBackendGateway,
		InstanceMode:      InstanceModeLite,
		Status:            "creating",
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 1,
		MemoryGB:          1,
		DiskGB:            1,
	}}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			11: {
				ID:            11,
				RuntimeType:   RuntimeTypeOpenClaw,
				AgentEndpoint: &endpoint,
				State:         "ready",
				Capacity:      10,
			},
		},
		schedulable: []models.RuntimePod{{
			ID:            11,
			RuntimeType:   RuntimeTypeOpenClaw,
			AgentEndpoint: &endpoint,
			State:         "ready",
			Capacity:      10,
		}},
	}
	agent := &fakeRuntimeAgentClient{
		createResponse: &RuntimeAgentCreateGatewayResponse{GatewayID: "gw-130-1", Port: 20030, Status: "running"},
	}
	scheduler := NewRuntimeScheduler(
		instanceRepo,
		podRepo,
		newFakeRuntimeBindingRepo(),
		&fakeRuntimeRolloutRepo{},
		agent,
		NewRuntimeEventService(nil),
		nil,
		&fakeRuntimeDeploymentService{},
		time.Second,
		WithRuntimeSchedulerGatewayEnvBuilder(func(instance *models.Instance) (map[string]string, error) {
			return map[string]string{
				"CLAWMANAGER_TEAM_ENABLED":    "true",
				"CLAWMANAGER_TEAM_SHARED_GID": "1000",
			}, nil
		}),
	)

	if err := scheduler.reconcile(ctx); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if got := len(agent.createRequests); got != 1 {
		t.Fatalf("CreateGateway calls = %d, want 1", got)
	}
	req := agent.createRequests[0].req
	if req.UID != RuntimeLinuxID(130) {
		t.Fatalf("UID = %d, want isolated instance UID %d", req.UID, RuntimeLinuxID(130))
	}
	if req.GID != 1000 {
		t.Fatalf("GID = %d, want Team shared GID 1000", req.GID)
	}
}

func TestRuntimeSchedulerNoSchedulablePodReturnsErrorAndReleasesNothing(t *testing.T) {
	ctx := context.Background()
	instanceRepo := newFakeRuntimeInstanceRepo()
	podRepo := &fakeRuntimePodRepo{}
	scheduler := NewRuntimeScheduler(
		instanceRepo,
		podRepo,
		newFakeRuntimeBindingRepo(),
		&fakeRuntimeRolloutRepo{},
		&fakeRuntimeAgentClient{},
		NewRuntimeEventService(nil),
		nil,
		&fakeRuntimeDeploymentService{},
		time.Second,
	)

	err := scheduler.assignInstance(ctx, models.Instance{
		ID:            1,
		UserID:        2,
		Type:          RuntimeTypeOpenClaw,
		RuntimeType:   RuntimeBackendGateway,
		InstanceMode:  InstanceModeLite,
		WorkspacePath: ptrString("/workspaces/openclaw/user-2/instance-1"),
	})
	if err == nil {
		t.Fatal("assignInstance returned nil error")
	}
	if podRepo.releaseCount != 0 {
		t.Fatalf("release count = %d, want 0", podRepo.releaseCount)
	}
}

func TestRuntimeSchedulerScalesOutWhenAllReadyPodsAtCapacity(t *testing.T) {
	ctx := context.Background()
	endpoint := "http://pod-1.runtime"
	workspacePath := "/workspaces/openclaw/user-12/instance-27"
	instanceRepo := newFakeRuntimeInstanceRepo()
	instanceRepo.creating = []models.Instance{{
		ID:                27,
		UserID:            12,
		Type:              RuntimeTypeOpenClaw,
		RuntimeType:       RuntimeBackendGateway,
		InstanceMode:      InstanceModeLite,
		Status:            "creating",
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 2,
	}}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			5: {
				ID:             5,
				RuntimeType:    RuntimeTypeOpenClaw,
				Namespace:      "clawmanager-system",
				PodName:        "openclaw-runtime-abc",
				DeploymentName: "openclaw-runtime",
				AgentEndpoint:  &endpoint,
				State:          "ready",
				Capacity:       100,
				UsedSlots:      100,
			},
		},
	}
	deployments := &fakeRuntimeDeploymentService{}
	agent := &fakeRuntimeAgentClient{}
	scheduler := NewRuntimeScheduler(
		instanceRepo,
		podRepo,
		newFakeRuntimeBindingRepo(),
		&fakeRuntimeRolloutRepo{},
		agent,
		NewRuntimeEventService(nil),
		nil,
		deployments,
		time.Second,
	)

	if err := scheduler.reconcile(ctx); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if got := len(deployments.scales); got != 1 {
		t.Fatalf("deployment Scale calls = %d, want 1", got)
	}
	scale := deployments.scales[0]
	if scale.namespace != "clawmanager-system" || scale.name != "openclaw-runtime" || scale.replicas != 2 {
		t.Fatalf("deployment Scale call = %+v, want clawmanager-system/openclaw-runtime replicas 2", scale)
	}
	if got := len(agent.createRequests); got != 0 {
		t.Fatalf("CreateGateway calls = %d, want 0 while waiting for scale-out pod", got)
	}
	if state, ok := instanceRepo.runtimeStates[27]; ok {
		t.Fatalf("instance runtime state = %+v, want unchanged while waiting for scale-out pod", state)
	}
}

func TestRuntimeSchedulerDoesNotScaleOutWhenGatewayCreateFailsWithFreeSlot(t *testing.T) {
	ctx := context.Background()
	endpoint := "http://pod-1.runtime"
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			5: {
				ID:             5,
				RuntimeType:    RuntimeTypeOpenClaw,
				Namespace:      "clawmanager-system",
				DeploymentName: "openclaw-runtime",
				AgentEndpoint:  &endpoint,
				State:          "ready",
				Capacity:       100,
				UsedSlots:      1,
			},
		},
		schedulable: []models.RuntimePod{{
			ID:             5,
			RuntimeType:    RuntimeTypeOpenClaw,
			Namespace:      "clawmanager-system",
			DeploymentName: "openclaw-runtime",
			AgentEndpoint:  &endpoint,
			State:          "ready",
			Capacity:       100,
			UsedSlots:      1,
		}},
	}
	deployments := &fakeRuntimeDeploymentService{}
	scheduler := NewRuntimeScheduler(
		newFakeRuntimeInstanceRepo(),
		podRepo,
		newFakeRuntimeBindingRepo(),
		&fakeRuntimeRolloutRepo{},
		&fakeRuntimeAgentClient{createErr: errors.New("agent timeout")},
		NewRuntimeEventService(nil),
		nil,
		deployments,
		time.Second,
	)

	err := scheduler.assignInstance(ctx, models.Instance{
		ID:                28,
		UserID:            12,
		Type:              RuntimeTypeOpenClaw,
		RuntimeType:       RuntimeBackendGateway,
		InstanceMode:      InstanceModeLite,
		WorkspacePath:     ptrString("/workspaces/openclaw/user-12/instance-28"),
		RuntimeGeneration: 1,
	})
	if err == nil {
		t.Fatal("assignInstance returned nil error")
	}
	if got := len(deployments.scales); got != 0 {
		t.Fatalf("deployment Scale calls = %d, want 0", got)
	}
}

func TestRuntimeSchedulerReconcileAssignsDesiredRunningInstanceWithMissingBinding(t *testing.T) {
	ctx := context.Background()
	endpoint := "http://agent.runtime"
	workspacePath := "/workspaces/openclaw/user-46/instance-18"
	instanceRepo := newFakeRuntimeInstanceRepo()
	instanceRepo.desiredRunning = []models.Instance{{
		ID:                18,
		UserID:            46,
		Type:              RuntimeTypeOpenClaw,
		RuntimeType:       RuntimeBackendGateway,
		InstanceMode:      InstanceModeLite,
		Status:            "running",
		MemoryGB:          1,
		DiskGB:            1,
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 4,
	}}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			9: {ID: 9, RuntimeType: RuntimeTypeOpenClaw, AgentEndpoint: &endpoint, State: "ready", Capacity: 1},
		},
		schedulable: []models.RuntimePod{
			{ID: 9, RuntimeType: RuntimeTypeOpenClaw, AgentEndpoint: &endpoint, State: "ready", Capacity: 1},
		},
	}
	agent := &fakeRuntimeAgentClient{
		createResponse: &RuntimeAgentCreateGatewayResponse{GatewayID: "gw-18", Port: 20018, Status: "running"},
	}
	scheduler := NewRuntimeScheduler(
		instanceRepo,
		podRepo,
		newFakeRuntimeBindingRepo(),
		&fakeRuntimeRolloutRepo{},
		agent,
		NewRuntimeEventService(nil),
		nil,
		&fakeRuntimeDeploymentService{},
		time.Second,
	)

	if err := scheduler.reconcile(ctx); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if got := len(agent.createRequests); got != 1 {
		t.Fatalf("CreateGateway calls = %d, want 1", got)
	}
}

func TestRuntimeSchedulerReconcileRetriesRecoverableNoSchedulableError(t *testing.T) {
	ctx := context.Background()
	endpoint := "http://agent.runtime"
	workspacePath := "/workspaces/openclaw/user-46/instance-68"
	errorMessage := "no schedulable openclaw runtime pod"
	instanceRepo := newFakeRuntimeInstanceRepo()
	instanceRepo.desiredRunning = []models.Instance{{
		ID:                  68,
		UserID:              46,
		Type:                RuntimeTypeOpenClaw,
		RuntimeType:         RuntimeBackendGateway,
		InstanceMode:        InstanceModeLite,
		Status:              "error",
		RuntimeErrorMessage: &errorMessage,
		MemoryGB:            1,
		DiskGB:              1,
		WorkspacePath:       &workspacePath,
		RuntimeGeneration:   29,
	}}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			50: {ID: 50, RuntimeType: RuntimeTypeOpenClaw, AgentEndpoint: &endpoint, State: "ready", Capacity: 100, UsedSlots: 0},
		},
		schedulable: []models.RuntimePod{
			{ID: 50, RuntimeType: RuntimeTypeOpenClaw, AgentEndpoint: &endpoint, State: "ready", Capacity: 100, UsedSlots: 0},
		},
	}
	agent := &fakeRuntimeAgentClient{
		createResponse: &RuntimeAgentCreateGatewayResponse{GatewayID: "gw-68", Port: 20068, Status: "running"},
	}
	scheduler := NewRuntimeScheduler(
		instanceRepo,
		podRepo,
		newFakeRuntimeBindingRepo(),
		&fakeRuntimeRolloutRepo{},
		agent,
		NewRuntimeEventService(nil),
		nil,
		&fakeRuntimeDeploymentService{},
		time.Second,
	)

	if err := scheduler.reconcile(ctx); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if got := len(agent.createRequests); got != 1 {
		t.Fatalf("CreateGateway calls = %d, want 1", got)
	}
	state := instanceRepo.runtimeStates[68]
	if state.status != "running" || state.generation != 29 || state.message != nil {
		t.Fatalf("runtime state = %+v, want running generation 29 without error message", state)
	}
}

func TestRuntimeSchedulerReconcileSkipsNonRecoverableErrorInstance(t *testing.T) {
	ctx := context.Background()
	endpoint := "http://agent.runtime"
	workspacePath := "/workspaces/openclaw/user-46/instance-69"
	errorMessage := "create gateway failed: invalid runtime response"
	instanceRepo := newFakeRuntimeInstanceRepo()
	instanceRepo.desiredRunning = []models.Instance{{
		ID:                  69,
		UserID:              46,
		Type:                RuntimeTypeOpenClaw,
		RuntimeType:         RuntimeBackendGateway,
		InstanceMode:        InstanceModeLite,
		Status:              "error",
		RuntimeErrorMessage: &errorMessage,
		MemoryGB:            1,
		DiskGB:              1,
		WorkspacePath:       &workspacePath,
		RuntimeGeneration:   24,
	}}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			50: {ID: 50, RuntimeType: RuntimeTypeOpenClaw, AgentEndpoint: &endpoint, State: "ready", Capacity: 100, UsedSlots: 0},
		},
		schedulable: []models.RuntimePod{
			{ID: 50, RuntimeType: RuntimeTypeOpenClaw, AgentEndpoint: &endpoint, State: "ready", Capacity: 100, UsedSlots: 0},
		},
	}
	agent := &fakeRuntimeAgentClient{
		createResponse: &RuntimeAgentCreateGatewayResponse{GatewayID: "gw-69", Port: 20069, Status: "running"},
	}
	scheduler := NewRuntimeScheduler(
		instanceRepo,
		podRepo,
		newFakeRuntimeBindingRepo(),
		&fakeRuntimeRolloutRepo{},
		agent,
		NewRuntimeEventService(nil),
		nil,
		&fakeRuntimeDeploymentService{},
		time.Second,
	)

	if err := scheduler.reconcile(ctx); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if got := len(agent.createRequests); got != 0 {
		t.Fatalf("CreateGateway calls = %d, want 0 for non-recoverable error", got)
	}
	if _, ok := instanceRepo.runtimeStates[69]; ok {
		t.Fatalf("runtime state was changed for non-recoverable error: %+v", instanceRepo.runtimeStates[69])
	}
}

func TestRuntimeSchedulerReconcileSkipsAssignmentWhenBindingLookupErrors(t *testing.T) {
	ctx := context.Background()
	endpoint := "http://agent.runtime"
	lookupErr := errors.New("binding db unavailable")
	workspacePath := "/workspaces/openclaw/user-47/instance-19"
	instanceRepo := newFakeRuntimeInstanceRepo()
	instanceRepo.desiredRunning = []models.Instance{{
		ID:                19,
		UserID:            47,
		Type:              RuntimeTypeOpenClaw,
		RuntimeType:       RuntimeBackendGateway,
		InstanceMode:      InstanceModeLite,
		Status:            "running",
		MemoryGB:          1,
		DiskGB:            1,
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 5,
	}}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			9: {ID: 9, RuntimeType: RuntimeTypeOpenClaw, AgentEndpoint: &endpoint, State: "ready", Capacity: 1},
		},
		schedulable: []models.RuntimePod{
			{ID: 9, RuntimeType: RuntimeTypeOpenClaw, AgentEndpoint: &endpoint, State: "ready", Capacity: 1},
		},
	}
	bindingRepo := newFakeRuntimeBindingRepo()
	bindingRepo.runningErr = lookupErr
	agent := &fakeRuntimeAgentClient{
		createResponse: &RuntimeAgentCreateGatewayResponse{GatewayID: "gw-19", Port: 20019, Status: "running"},
	}
	scheduler := NewRuntimeScheduler(
		instanceRepo,
		podRepo,
		bindingRepo,
		&fakeRuntimeRolloutRepo{},
		agent,
		NewRuntimeEventService(nil),
		nil,
		&fakeRuntimeDeploymentService{},
		time.Second,
	)

	err := scheduler.reconcile(ctx)
	if err == nil {
		t.Fatal("reconcile returned nil error")
	}
	if !errors.Is(err, lookupErr) {
		t.Fatalf("reconcile error = %v, want lookup error", err)
	}
	if got := len(agent.createRequests); got != 0 {
		t.Fatalf("CreateGateway calls = %d, want 0", got)
	}
	if bindingRepo.bindings[19] != nil {
		t.Fatal("binding was created despite lookup error")
	}
	if podRepo.claims[9] != 0 {
		t.Fatalf("pod claims = %d, want 0", podRepo.claims[9])
	}
}

func TestRuntimeSchedulerReconcileSkipsLegacyInstanceWithoutWorkspacePath(t *testing.T) {
	ctx := context.Background()
	endpoint := "http://agent.runtime"
	instanceRepo := newFakeRuntimeInstanceRepo()
	instanceRepo.creating = []models.Instance{{
		ID:                20,
		UserID:            48,
		Type:              RuntimeTypeOpenClaw,
		RuntimeType:       RuntimeBackendGateway,
		InstanceMode:      InstanceModeLite,
		Status:            "creating",
		MemoryGB:          1,
		DiskGB:            1,
		RuntimeGeneration: 1,
	}}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			9: {ID: 9, RuntimeType: RuntimeTypeOpenClaw, AgentEndpoint: &endpoint, State: "ready", Capacity: 1},
		},
		schedulable: []models.RuntimePod{
			{ID: 9, RuntimeType: RuntimeTypeOpenClaw, AgentEndpoint: &endpoint, State: "ready", Capacity: 1},
		},
	}
	agent := &fakeRuntimeAgentClient{
		createResponse: &RuntimeAgentCreateGatewayResponse{GatewayID: "gw-20", Port: 20020, Status: "running"},
	}
	scheduler := NewRuntimeScheduler(
		instanceRepo,
		podRepo,
		newFakeRuntimeBindingRepo(),
		&fakeRuntimeRolloutRepo{},
		agent,
		NewRuntimeEventService(nil),
		nil,
		&fakeRuntimeDeploymentService{},
		time.Second,
	)

	if err := scheduler.reconcile(ctx); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if got := len(agent.createRequests); got != 0 {
		t.Fatalf("CreateGateway calls = %d, want 0", got)
	}
	if _, ok := instanceRepo.runtimeStates[20]; ok {
		t.Fatalf("legacy instance was marked with runtime state: %+v", instanceRepo.runtimeStates[20])
	}
}

func TestRuntimeSchedulerSkipsCreatingInstanceWithExistingBinding(t *testing.T) {
	ctx := context.Background()
	endpoint := "http://agent.runtime"
	workspacePath := "/workspaces/openclaw/user-49/instance-21"
	instanceRepo := newFakeRuntimeInstanceRepo()
	instanceRepo.creating = []models.Instance{{
		ID:                21,
		UserID:            49,
		Type:              RuntimeTypeOpenClaw,
		RuntimeType:       RuntimeBackendGateway,
		InstanceMode:      InstanceModeLite,
		Status:            "creating",
		MemoryGB:          1,
		DiskGB:            1,
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 1,
	}}
	bindingRepo := newFakeRuntimeBindingRepo()
	bindingRepo.bindings[21] = &models.InstanceRuntimeBinding{InstanceID: 21, RuntimePodID: 9, State: "creating", Generation: 1}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			9: {ID: 9, RuntimeType: RuntimeTypeOpenClaw, AgentEndpoint: &endpoint, State: "ready", Capacity: 1},
		},
		schedulable: []models.RuntimePod{
			{ID: 9, RuntimeType: RuntimeTypeOpenClaw, AgentEndpoint: &endpoint, State: "ready", Capacity: 1},
		},
	}
	agent := &fakeRuntimeAgentClient{
		createResponse: &RuntimeAgentCreateGatewayResponse{GatewayID: "gw-21", Port: 20021, Status: "running"},
	}
	scheduler := NewRuntimeScheduler(
		instanceRepo,
		podRepo,
		bindingRepo,
		&fakeRuntimeRolloutRepo{},
		agent,
		NewRuntimeEventService(nil),
		nil,
		&fakeRuntimeDeploymentService{},
		time.Second,
	)

	if err := scheduler.reconcile(ctx); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if got := len(agent.createRequests); got != 0 {
		t.Fatalf("CreateGateway calls = %d, want 0", got)
	}
	if podRepo.claims[9] != 0 {
		t.Fatalf("pod claims = %d, want 0", podRepo.claims[9])
	}
}

func TestRuntimeSchedulerSyncsCreatingInstanceFromRunningBinding(t *testing.T) {
	ctx := context.Background()
	workspacePath := "/workspaces/openclaw/user-1/instance-25"
	instanceRepo := newFakeRuntimeInstanceRepo()
	instanceRepo.creating = []models.Instance{{
		ID:                25,
		UserID:            1,
		Type:              RuntimeTypeOpenClaw,
		RuntimeType:       RuntimeBackendGateway,
		InstanceMode:      InstanceModeLite,
		Status:            "creating",
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 2,
	}}
	bindingRepo := newFakeRuntimeBindingRepo()
	bindingRepo.bindings[25] = &models.InstanceRuntimeBinding{
		InstanceID:  25,
		RuntimeType: RuntimeTypeOpenClaw,
		State:       "running",
		Generation:  2,
	}
	scheduler := NewRuntimeScheduler(
		instanceRepo,
		&fakeRuntimePodRepo{},
		bindingRepo,
		&fakeRuntimeRolloutRepo{},
		&fakeRuntimeAgentClient{},
		NewRuntimeEventService(nil),
		nil,
		&fakeRuntimeDeploymentService{},
		time.Second,
	)

	if err := scheduler.reconcile(ctx); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	state := instanceRepo.runtimeStates[25]
	if state.status != "running" || state.generation != 2 || state.message != nil {
		t.Fatalf("expected instance to sync to running from binding, got %+v", state)
	}
}

func TestRuntimeSchedulerCleansUpGatewayAndReleasesSlotWhenBindingCreateFails(t *testing.T) {
	ctx := context.Background()
	endpoint := "http://agent.runtime"
	createErr := errors.New("binding insert failed")
	instanceRepo := newFakeRuntimeInstanceRepo()
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			9: {ID: 9, RuntimeType: RuntimeTypeOpenClaw, AgentEndpoint: &endpoint, State: "ready", Capacity: 1},
		},
		schedulable: []models.RuntimePod{
			{ID: 9, RuntimeType: RuntimeTypeOpenClaw, AgentEndpoint: &endpoint, State: "ready", Capacity: 1},
		},
	}
	bindingRepo := newFakeRuntimeBindingRepo()
	bindingRepo.createErr = createErr
	agent := &fakeRuntimeAgentClient{
		createResponse: &RuntimeAgentCreateGatewayResponse{GatewayID: "gw-22", Port: 20022, Status: "running"},
	}
	scheduler := NewRuntimeScheduler(
		instanceRepo,
		podRepo,
		bindingRepo,
		&fakeRuntimeRolloutRepo{},
		agent,
		NewRuntimeEventService(nil),
		nil,
		&fakeRuntimeDeploymentService{},
		time.Second,
	)

	err := scheduler.assignInstance(ctx, models.Instance{
		ID:                22,
		UserID:            50,
		Type:              RuntimeTypeOpenClaw,
		RuntimeType:       RuntimeBackendGateway,
		InstanceMode:      InstanceModeLite,
		WorkspacePath:     ptrString("/workspaces/openclaw/user-50/instance-22"),
		MemoryGB:          1,
		DiskGB:            1,
		RuntimeGeneration: 1,
	})
	if err == nil {
		t.Fatal("assignInstance returned nil error")
	}
	if !errors.Is(err, createErr) {
		t.Fatalf("assignInstance error = %v, want binding create error", err)
	}
	if got := len(agent.deleteRequests); got != 1 {
		t.Fatalf("DeleteGateway calls = %d, want 1", got)
	}
	if agent.deleteRequests[0].gatewayID != "gw-22" {
		t.Fatalf("deleted gateway = %q, want gw-22", agent.deleteRequests[0].gatewayID)
	}
	if podRepo.releases[9] != 1 {
		t.Fatalf("pod releases = %d, want 1", podRepo.releases[9])
	}
	if bindingRepo.bindings[22] != nil {
		t.Fatal("binding remains after failed create")
	}
}

func TestRuntimeSchedulerCleansUpGatewayBindingAndSlotWhenWorkspacePathUpdateFails(t *testing.T) {
	ctx := context.Background()
	endpoint := "http://agent.runtime"
	workspaceErr := errors.New("workspace path update failed")
	instanceRepo := newFakeRuntimeInstanceRepo()
	instanceRepo.setWorkspacePathErr = workspaceErr
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			9: {ID: 9, RuntimeType: RuntimeTypeOpenClaw, AgentEndpoint: &endpoint, State: "ready", Capacity: 1},
		},
		schedulable: []models.RuntimePod{
			{ID: 9, RuntimeType: RuntimeTypeOpenClaw, AgentEndpoint: &endpoint, State: "ready", Capacity: 1},
		},
	}
	bindingRepo := newFakeRuntimeBindingRepo()
	agent := &fakeRuntimeAgentClient{
		createResponse: &RuntimeAgentCreateGatewayResponse{GatewayID: "gw-23", Port: 20023, Status: "running"},
	}
	scheduler := NewRuntimeScheduler(
		instanceRepo,
		podRepo,
		bindingRepo,
		&fakeRuntimeRolloutRepo{},
		agent,
		NewRuntimeEventService(nil),
		nil,
		&fakeRuntimeDeploymentService{},
		time.Second,
	)

	err := scheduler.assignInstance(ctx, models.Instance{
		ID:                23,
		UserID:            51,
		Type:              RuntimeTypeOpenClaw,
		RuntimeType:       RuntimeBackendGateway,
		InstanceMode:      InstanceModeLite,
		WorkspacePath:     ptrString("/workspaces/openclaw/user-51/instance-23"),
		MemoryGB:          1,
		DiskGB:            1,
		RuntimeGeneration: 1,
	})
	if err == nil {
		t.Fatal("assignInstance returned nil error")
	}
	if !errors.Is(err, workspaceErr) {
		t.Fatalf("assignInstance error = %v, want workspace update error", err)
	}
	if got := len(agent.deleteRequests); got != 1 {
		t.Fatalf("DeleteGateway calls = %d, want 1", got)
	}
	if agent.deleteRequests[0].gatewayID != "gw-23" {
		t.Fatalf("deleted gateway = %q, want gw-23", agent.deleteRequests[0].gatewayID)
	}
	if bindingRepo.deleteCalls[23] != 1 {
		t.Fatalf("binding delete calls = %d, want 1", bindingRepo.deleteCalls[23])
	}
	if bindingRepo.bindings[23] != nil {
		t.Fatal("binding remains after workspace update failure")
	}
	if podRepo.releases[9] != 1 {
		t.Fatalf("pod releases = %d, want 1", podRepo.releases[9])
	}
}

func TestRuntimeSchedulerDesiredRunningSkipsNonRunningExistingBinding(t *testing.T) {
	ctx := context.Background()
	endpoint := "http://agent.runtime"
	workspacePath := "/workspaces/openclaw/user-52/instance-24"
	instanceRepo := newFakeRuntimeInstanceRepo()
	instanceRepo.desiredRunning = []models.Instance{{
		ID:                24,
		UserID:            52,
		Type:              RuntimeTypeOpenClaw,
		RuntimeType:       RuntimeBackendGateway,
		InstanceMode:      InstanceModeLite,
		Status:            "running",
		MemoryGB:          1,
		DiskGB:            1,
		WorkspacePath:     &workspacePath,
		RuntimeGeneration: 1,
	}}
	bindingRepo := newFakeRuntimeBindingRepo()
	bindingRepo.bindings[24] = &models.InstanceRuntimeBinding{
		InstanceID:   24,
		RuntimePodID: 9,
		State:        "creating",
		Generation:   1,
	}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			9: {ID: 9, RuntimeType: RuntimeTypeOpenClaw, AgentEndpoint: &endpoint, State: "ready", Capacity: 1},
		},
		schedulable: []models.RuntimePod{
			{ID: 9, RuntimeType: RuntimeTypeOpenClaw, AgentEndpoint: &endpoint, State: "ready", Capacity: 1},
		},
	}
	agent := &fakeRuntimeAgentClient{
		createResponse: &RuntimeAgentCreateGatewayResponse{GatewayID: "gw-24", Port: 20024, Status: "running"},
	}
	scheduler := NewRuntimeScheduler(
		instanceRepo,
		podRepo,
		bindingRepo,
		&fakeRuntimeRolloutRepo{},
		agent,
		NewRuntimeEventService(nil),
		nil,
		&fakeRuntimeDeploymentService{},
		time.Second,
	)

	if err := scheduler.reconcile(ctx); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if got := len(agent.createRequests); got != 0 {
		t.Fatalf("CreateGateway calls = %d, want 0", got)
	}
	if podRepo.claims[9] != 0 {
		t.Fatalf("pod claims = %d, want 0", podRepo.claims[9])
	}
	if bindingRepo.bindings[24].GatewayID != "" {
		t.Fatalf("binding was overwritten: %+v", bindingRepo.bindings[24])
	}
}

func TestRuntimeSchedulerFailoverPodMarksUnhealthyDeletesBindingsReleasesSlotsAndRecreatesInstances(t *testing.T) {
	ctx := context.Background()
	reason := "agent heartbeat lost"
	instanceRepo := newFakeRuntimeInstanceRepo()
	instanceRepo.byID[17] = &models.Instance{ID: 17, RuntimeGeneration: 3}
	bindingRepo := newFakeRuntimeBindingRepo()
	bindingRepo.bindings[17] = &models.InstanceRuntimeBinding{
		InstanceID:   17,
		RuntimePodID: 9,
		Generation:   3,
	}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			9: {ID: 9, RuntimeType: RuntimeTypeOpenClaw, State: "ready"},
		},
	}
	scheduler := NewRuntimeScheduler(
		instanceRepo,
		podRepo,
		bindingRepo,
		&fakeRuntimeRolloutRepo{},
		&fakeRuntimeAgentClient{},
		NewRuntimeEventService(nil),
		nil,
		&fakeRuntimeDeploymentService{},
		time.Second,
	)

	if err := scheduler.FailoverPod(ctx, 9, reason); err != nil {
		t.Fatalf("FailoverPod returned error: %v", err)
	}

	if got := podRepo.marked[9]; got.state != "unhealthy" || got.draining {
		t.Fatalf("marked pod state = %+v", got)
	}
	if bindingRepo.bindings[17] != nil {
		t.Fatal("binding was not deleted")
	}
	if podRepo.releases[9] != 1 {
		t.Fatalf("pod releases = %d, want 1", podRepo.releases[9])
	}
	state := instanceRepo.runtimeStates[17]
	if state.status != "creating" || state.generation != 4 {
		t.Fatalf("runtime state = %+v, want creating generation 4", state)
	}
	if state.message == nil || *state.message != reason {
		t.Fatalf("runtime state message = %v, want %q", state.message, reason)
	}
}

func TestRuntimeSchedulerFailoverLeavesBindingAndSlotWhenInstanceUpdateFails(t *testing.T) {
	ctx := context.Background()
	reason := "agent heartbeat lost"
	updateErr := errors.New("instance update failed")
	instanceRepo := newFakeRuntimeInstanceRepo()
	instanceRepo.byID[17] = &models.Instance{ID: 17, RuntimeGeneration: 3}
	instanceRepo.updateRuntimeStateErr = updateErr
	bindingRepo := newFakeRuntimeBindingRepo()
	bindingRepo.bindings[17] = &models.InstanceRuntimeBinding{
		InstanceID:   17,
		RuntimePodID: 9,
		Generation:   3,
	}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			9: {ID: 9, RuntimeType: RuntimeTypeOpenClaw, State: "ready"},
		},
	}
	scheduler := NewRuntimeScheduler(
		instanceRepo,
		podRepo,
		bindingRepo,
		&fakeRuntimeRolloutRepo{},
		&fakeRuntimeAgentClient{},
		NewRuntimeEventService(nil),
		nil,
		&fakeRuntimeDeploymentService{},
		time.Second,
	)

	err := scheduler.FailoverPod(ctx, 9, reason)
	if err == nil {
		t.Fatal("FailoverPod returned nil error")
	}
	if !errors.Is(err, updateErr) {
		t.Fatalf("FailoverPod error = %v, want update error", err)
	}
	if bindingRepo.bindings[17] == nil {
		t.Fatal("binding was deleted despite instance update failure")
	}
	if podRepo.releases[9] != 0 {
		t.Fatalf("pod releases = %d, want 0", podRepo.releases[9])
	}
}

func TestRuntimeSchedulerReconcileFailoversStalePod(t *testing.T) {
	ctx := context.Background()
	staleSeen := time.Now().UTC().Add(-30 * time.Second)
	instanceRepo := newFakeRuntimeInstanceRepo()
	instanceRepo.byID[17] = &models.Instance{ID: 17, RuntimeGeneration: 3}
	bindingRepo := newFakeRuntimeBindingRepo()
	bindingRepo.bindings[17] = &models.InstanceRuntimeBinding{
		InstanceID:   17,
		RuntimePodID: 9,
		Generation:   3,
		State:        "running",
	}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			9: {ID: 9, RuntimeType: RuntimeTypeOpenClaw, State: "ready", LastSeenAt: &staleSeen},
		},
	}
	scheduler := NewRuntimeScheduler(
		instanceRepo,
		podRepo,
		bindingRepo,
		&fakeRuntimeRolloutRepo{},
		&fakeRuntimeAgentClient{},
		NewRuntimeEventService(nil),
		nil,
		&fakeRuntimeDeploymentService{},
		time.Second,
		WithRuntimeSchedulerHeartbeatTimeout(10*time.Second),
	)

	if err := scheduler.reconcile(ctx); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	if got := podRepo.marked[9]; got.state != "unhealthy" || got.draining {
		t.Fatalf("marked pod state = %+v", got)
	}
	if bindingRepo.bindings[17] != nil {
		t.Fatal("binding was not deleted")
	}
	if podRepo.releases[9] != 1 {
		t.Fatalf("pod releases = %d, want 1", podRepo.releases[9])
	}
	state := instanceRepo.runtimeStates[17]
	if state.status != "creating" || state.generation != 4 {
		t.Fatalf("runtime state = %+v, want creating generation 4", state)
	}
	if state.message == nil || !strings.Contains(*state.message, "runtime pod heartbeat lost") {
		t.Fatalf("runtime state message = %v, want heartbeat lost reason", state.message)
	}
}

func TestRuntimeSchedulerReconcileKeepsRecentPodBinding(t *testing.T) {
	ctx := context.Background()
	recentSeen := time.Now().UTC()
	instanceRepo := newFakeRuntimeInstanceRepo()
	instanceRepo.byID[17] = &models.Instance{ID: 17, RuntimeGeneration: 3}
	bindingRepo := newFakeRuntimeBindingRepo()
	bindingRepo.bindings[17] = &models.InstanceRuntimeBinding{
		InstanceID:   17,
		RuntimePodID: 9,
		Generation:   3,
		State:        "running",
	}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			9: {ID: 9, RuntimeType: RuntimeTypeOpenClaw, State: "ready", LastSeenAt: &recentSeen},
		},
	}
	scheduler := NewRuntimeScheduler(
		instanceRepo,
		podRepo,
		bindingRepo,
		&fakeRuntimeRolloutRepo{},
		&fakeRuntimeAgentClient{},
		NewRuntimeEventService(nil),
		nil,
		&fakeRuntimeDeploymentService{},
		time.Second,
		WithRuntimeSchedulerHeartbeatTimeout(10*time.Second),
	)

	if err := scheduler.reconcile(ctx); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	if got := podRepo.marked[9]; got.state != "" || got.draining {
		t.Fatalf("pod was unexpectedly marked: %+v", got)
	}
	if bindingRepo.bindings[17] == nil {
		t.Fatal("binding was deleted")
	}
	if podRepo.releases[9] != 0 {
		t.Fatalf("pod releases = %d, want 0", podRepo.releases[9])
	}
}

func TestRuntimeSchedulerRolloutExistingDrainingPodsPreventAdditionalDrains(t *testing.T) {
	ctx := context.Background()
	readyEndpoint := "http://ready.runtime"
	drainingEndpoint := "http://draining.runtime"
	rolloutRepo := &fakeRuntimeRolloutRepo{
		rollouts: map[int64]*models.RuntimeRollout{
			7: {ID: 7, RuntimeType: RuntimeTypeOpenClaw, Status: "pending", BatchSize: 3, MaxUnavailable: 1},
		},
	}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			1: {ID: 1, RuntimeType: RuntimeTypeOpenClaw, State: "ready", Draining: true, AgentEndpoint: &drainingEndpoint},
			2: {ID: 2, RuntimeType: RuntimeTypeOpenClaw, State: "ready", AgentEndpoint: &readyEndpoint},
		},
	}
	agent := &fakeRuntimeAgentClient{}
	scheduler := NewRuntimeScheduler(
		newFakeRuntimeInstanceRepo(),
		podRepo,
		newFakeRuntimeBindingRepo(),
		rolloutRepo,
		agent,
		NewRuntimeEventService(nil),
		nil,
		&fakeRuntimeDeploymentService{},
		time.Second,
	)

	if err := scheduler.StartRollout(ctx, 7); err != nil {
		t.Fatalf("StartRollout returned error: %v", err)
	}
	if got := len(agent.drainEndpoints); got != 0 {
		t.Fatalf("Drain calls = %d, want 0", got)
	}
	if len(rolloutRepo.statuses) != 1 || rolloutRepo.statuses[0].status != "running" {
		t.Fatalf("rollout statuses = %+v, want only running", rolloutRepo.statuses)
	}
}

func TestRuntimeSchedulerRolloutBatchLimitedByMaxUnavailable(t *testing.T) {
	ctx := context.Background()
	endpoint1 := "http://pod-1.runtime"
	endpoint2 := "http://pod-2.runtime"
	endpoint3 := "http://pod-3.runtime"
	rolloutRepo := &fakeRuntimeRolloutRepo{
		rollouts: map[int64]*models.RuntimeRollout{
			8: {ID: 8, RuntimeType: RuntimeTypeOpenClaw, Status: "pending", BatchSize: 3, MaxUnavailable: 2},
		},
	}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			1: {ID: 1, RuntimeType: RuntimeTypeOpenClaw, State: "ready", AgentEndpoint: &endpoint1},
			2: {ID: 2, RuntimeType: RuntimeTypeOpenClaw, State: "ready", AgentEndpoint: &endpoint2},
			3: {ID: 3, RuntimeType: RuntimeTypeOpenClaw, State: "ready", AgentEndpoint: &endpoint3},
		},
	}
	agent := &fakeRuntimeAgentClient{}
	scheduler := NewRuntimeScheduler(
		newFakeRuntimeInstanceRepo(),
		podRepo,
		newFakeRuntimeBindingRepo(),
		rolloutRepo,
		agent,
		NewRuntimeEventService(nil),
		nil,
		&fakeRuntimeDeploymentService{},
		time.Second,
	)

	if err := scheduler.StartRollout(ctx, 8); err != nil {
		t.Fatalf("StartRollout returned error: %v", err)
	}
	if got := len(agent.drainEndpoints); got != 2 {
		t.Fatalf("Drain calls = %d, want 2", got)
	}
}

func TestRuntimeSchedulerRolloutUpdatesDeployments(t *testing.T) {
	ctx := context.Background()
	endpoint := "http://pod-1.runtime"
	rolloutRepo := &fakeRuntimeRolloutRepo{
		rollouts: map[int64]*models.RuntimeRollout{
			9: {ID: 9, RuntimeType: RuntimeTypeOpenClaw, TargetImageRef: "new-image", Status: "pending", BatchSize: 1, MaxUnavailable: 1},
		},
	}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			1: {
				ID:             1,
				RuntimeType:    RuntimeTypeOpenClaw,
				State:          "ready",
				Namespace:      "runtime-system",
				DeploymentName: "openclaw-runtime",
				AgentEndpoint:  &endpoint,
			},
		},
	}
	deployments := &fakeRuntimeDeploymentService{}
	scheduler := NewRuntimeScheduler(
		newFakeRuntimeInstanceRepo(),
		podRepo,
		newFakeRuntimeBindingRepo(),
		rolloutRepo,
		&fakeRuntimeAgentClient{},
		NewRuntimeEventService(nil),
		nil,
		deployments,
		time.Second,
	)

	if err := scheduler.StartRollout(ctx, 9); err != nil {
		t.Fatalf("StartRollout returned error: %v", err)
	}
	if got := len(deployments.rolloutImageCalls); got != 1 {
		t.Fatalf("deployment RolloutImage calls = %d, want 1", got)
	}
	call := deployments.rolloutImageCalls[0]
	if call.namespace != "runtime-system" || call.name != "openclaw-runtime" || call.image != "new-image" {
		t.Fatalf("RolloutImage call = %+v, want runtime-system/openclaw-runtime new-image", call)
	}
	if call.maxUnavailable != 1 || call.maxSurge != 1 {
		t.Fatalf("RolloutImage strategy = %+v, want maxUnavailable=1 maxSurge=1", call)
	}
}

func TestRuntimeSchedulerRolloutUsesStalePodDeploymentRefWhenNoCurrentPods(t *testing.T) {
	ctx := context.Background()
	staleSeen := time.Now().UTC().Add(-5 * time.Minute)
	rolloutRepo := &fakeRuntimeRolloutRepo{
		rollouts: map[int64]*models.RuntimeRollout{
			12: {ID: 12, RuntimeType: RuntimeTypeOpenClaw, TargetImageRef: "registry/openclaw:v2", Status: "pending", BatchSize: 1, MaxUnavailable: 1},
		},
	}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			41: {
				ID:             41,
				RuntimeType:    RuntimeTypeOpenClaw,
				State:          "unhealthy",
				Namespace:      "runtime-system",
				DeploymentName: "openclaw-runtime",
				ImageRef:       "registry/openclaw:v1",
				LastSeenAt:     &staleSeen,
			},
		},
	}
	agent := &fakeRuntimeAgentClient{}
	deployments := &fakeRuntimeDeploymentService{}
	scheduler := NewRuntimeScheduler(
		newFakeRuntimeInstanceRepo(),
		podRepo,
		newFakeRuntimeBindingRepo(),
		rolloutRepo,
		agent,
		NewRuntimeEventService(nil),
		nil,
		deployments,
		time.Second,
		WithRuntimeSchedulerHeartbeatTimeout(10*time.Second),
	)

	if err := scheduler.StartRollout(ctx, 12); err != nil {
		t.Fatalf("StartRollout returned error: %v", err)
	}
	if got := len(deployments.rolloutImageCalls); got != 1 {
		t.Fatalf("deployment RolloutImage calls = %d, want 1", got)
	}
	call := deployments.rolloutImageCalls[0]
	if call.namespace != "runtime-system" || call.name != "openclaw-runtime" || call.image != "registry/openclaw:v2" {
		t.Fatalf("RolloutImage call = %+v, want runtime-system/openclaw-runtime registry/openclaw:v2", call)
	}
	if got := len(agent.drainEndpoints); got != 0 {
		t.Fatalf("Drain calls = %d, want 0 with no current runtime pods", got)
	}
	if len(rolloutRepo.statuses) != 1 || rolloutRepo.statuses[0].status != "running" {
		t.Fatalf("rollout statuses = %+v, want only running", rolloutRepo.statuses)
	}
}

func TestRuntimeSchedulerRolloutFallsBackToDefaultDeploymentWhenNoPodRows(t *testing.T) {
	ctx := context.Background()
	rolloutRepo := &fakeRuntimeRolloutRepo{
		rollouts: map[int64]*models.RuntimeRollout{
			13: {ID: 13, RuntimeType: RuntimeTypeOpenClaw, TargetImageRef: "registry/openclaw:v3", Status: "pending", BatchSize: 2, MaxUnavailable: 1},
		},
	}
	deployments := &fakeRuntimeDeploymentService{}
	scheduler := NewRuntimeScheduler(
		newFakeRuntimeInstanceRepo(),
		&fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{}},
		newFakeRuntimeBindingRepo(),
		rolloutRepo,
		&fakeRuntimeAgentClient{},
		NewRuntimeEventService(nil),
		nil,
		deployments,
		time.Second,
		WithRuntimeSchedulerNamespace("runtime-system"),
	)

	if err := scheduler.StartRollout(ctx, 13); err != nil {
		t.Fatalf("StartRollout returned error: %v", err)
	}
	if got := len(deployments.rolloutImageCalls); got != 1 {
		t.Fatalf("deployment RolloutImage calls = %d, want 1", got)
	}
	call := deployments.rolloutImageCalls[0]
	if call.namespace != "runtime-system" || call.name != "openclaw-runtime" || call.image != "registry/openclaw:v3" {
		t.Fatalf("RolloutImage call = %+v, want runtime-system/openclaw-runtime registry/openclaw:v3", call)
	}
}

func TestRuntimeSchedulerRolloutSameImageFinishesWithoutDrain(t *testing.T) {
	ctx := context.Background()
	recentSeen := time.Now().UTC()
	endpoint := "http://pod-1.runtime"
	rolloutRepo := &fakeRuntimeRolloutRepo{
		rollouts: map[int64]*models.RuntimeRollout{
			11: {ID: 11, RuntimeType: RuntimeTypeHermes, TargetImageRef: "registry/hermes:v2", Status: "pending", BatchSize: 1, MaxUnavailable: 1},
		},
	}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			4: {
				ID:             4,
				RuntimeType:    RuntimeTypeHermes,
				State:          "ready",
				Namespace:      "runtime-system",
				DeploymentName: "hermes-runtime",
				ImageRef:       "registry/hermes:v2",
				AgentEndpoint:  &endpoint,
				LastSeenAt:     &recentSeen,
			},
		},
	}
	agent := &fakeRuntimeAgentClient{}
	deployments := &fakeRuntimeDeploymentService{}
	scheduler := NewRuntimeScheduler(
		newFakeRuntimeInstanceRepo(),
		podRepo,
		newFakeRuntimeBindingRepo(),
		rolloutRepo,
		agent,
		NewRuntimeEventService(nil),
		nil,
		deployments,
		time.Second,
	)

	if err := scheduler.StartRollout(ctx, 11); err != nil {
		t.Fatalf("StartRollout returned error: %v", err)
	}
	if got := len(agent.drainEndpoints); got != 0 {
		t.Fatalf("Drain calls = %d, want 0", got)
	}
	if got := len(deployments.rolloutImageCalls); got != 0 {
		t.Fatalf("RolloutImage calls = %d, want 0", got)
	}
	if len(rolloutRepo.statuses) != 2 || rolloutRepo.statuses[0].status != "running" || rolloutRepo.statuses[1].status != "finished" {
		t.Fatalf("rollout statuses = %+v, want running then finished", rolloutRepo.statuses)
	}
}

func TestRuntimeSchedulerReconcileStartsPendingRollouts(t *testing.T) {
	ctx := context.Background()
	recentSeen := time.Now().UTC()
	endpoint := "http://pod-1.runtime"
	rolloutRepo := &fakeRuntimeRolloutRepo{
		rollouts: map[int64]*models.RuntimeRollout{
			10: {
				ID:             10,
				RuntimeType:    RuntimeTypeHermes,
				TargetImageRef: "registry/hermes:v2",
				Status:         "pending",
				BatchSize:      2,
				MaxUnavailable: 1,
			},
		},
	}
	podRepo := &fakeRuntimePodRepo{
		pods: map[int64]*models.RuntimePod{
			3: {
				ID:             3,
				RuntimeType:    RuntimeTypeHermes,
				State:          "ready",
				Namespace:      "runtime-system",
				DeploymentName: "hermes-runtime",
				AgentEndpoint:  &endpoint,
				LastSeenAt:     &recentSeen,
			},
		},
	}
	deployments := &fakeRuntimeDeploymentService{}
	scheduler := NewRuntimeScheduler(
		newFakeRuntimeInstanceRepo(),
		podRepo,
		newFakeRuntimeBindingRepo(),
		rolloutRepo,
		&fakeRuntimeAgentClient{},
		NewRuntimeEventService(nil),
		nil,
		deployments,
		time.Second,
	)

	if err := scheduler.reconcile(ctx); err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if got := len(deployments.rolloutImageCalls); got != 1 {
		t.Fatalf("deployment RolloutImage calls = %d, want 1", got)
	}
	call := deployments.rolloutImageCalls[0]
	if call.namespace != "runtime-system" || call.name != "hermes-runtime" || call.image != "registry/hermes:v2" {
		t.Fatalf("RolloutImage call = %+v, want runtime-system/hermes-runtime registry/hermes:v2", call)
	}
	if len(rolloutRepo.statuses) == 0 || rolloutRepo.statuses[0].status != "running" {
		t.Fatalf("rollout statuses = %+v, want running", rolloutRepo.statuses)
	}
}

type fakeRuntimeInstanceRepo struct {
	creating              []models.Instance
	desiredRunning        []models.Instance
	byID                  map[int]*models.Instance
	workspacePaths        map[int]string
	runtimeStates         map[int]fakeRuntimeState
	updateRuntimeStateErr error
	setWorkspacePathErr   error
}

type fakeRuntimeState struct {
	status     string
	generation int
	message    *string
}

func newFakeRuntimeInstanceRepo() *fakeRuntimeInstanceRepo {
	return &fakeRuntimeInstanceRepo{
		byID:           map[int]*models.Instance{},
		workspacePaths: map[int]string{},
		runtimeStates:  map[int]fakeRuntimeState{},
	}
}

func (r *fakeRuntimeInstanceRepo) Create(instance *models.Instance) error { return nil }
func (r *fakeRuntimeInstanceRepo) GetByID(id int) (*models.Instance, error) {
	return r.byID[id], nil
}
func (r *fakeRuntimeInstanceRepo) GetByAccessToken(accessToken string) (*models.Instance, error) {
	return nil, nil
}
func (r *fakeRuntimeInstanceRepo) GetByAgentBootstrapToken(bootstrapToken string) (*models.Instance, error) {
	return nil, nil
}
func (r *fakeRuntimeInstanceRepo) GetAll(offset, limit int) ([]models.Instance, error) {
	return nil, nil
}
func (r *fakeRuntimeInstanceRepo) CountAll() (int, error) { return 0, nil }
func (r *fakeRuntimeInstanceRepo) GetByUserID(userID int, offset, limit int) ([]models.Instance, error) {
	return nil, nil
}
func (r *fakeRuntimeInstanceRepo) CountByUserID(userID int) (int, error) { return 0, nil }
func (r *fakeRuntimeInstanceRepo) CountActiveByMode(ctx context.Context, mode string) (int, error) {
	return 0, nil
}
func (r *fakeRuntimeInstanceRepo) ExistsByUserIDAndName(userID int, name string) (bool, error) {
	return false, nil
}
func (r *fakeRuntimeInstanceRepo) GetAllRunning() ([]models.Instance, error) { return nil, nil }
func (r *fakeRuntimeInstanceRepo) GetV2DesiredRunning(ctx context.Context, limit int) ([]models.Instance, error) {
	return r.desiredRunning, nil
}
func (r *fakeRuntimeInstanceRepo) GetV2Creating(ctx context.Context, limit int) ([]models.Instance, error) {
	return r.creating, nil
}
func (r *fakeRuntimeInstanceRepo) UpdateRuntimeState(ctx context.Context, id int, status string, generation int, message *string) error {
	if r.updateRuntimeStateErr != nil {
		return r.updateRuntimeStateErr
	}
	r.runtimeStates[id] = fakeRuntimeState{status: status, generation: generation, message: message}
	return nil
}
func (r *fakeRuntimeInstanceRepo) SetWorkspacePath(ctx context.Context, id int, workspacePath string) error {
	if r.setWorkspacePathErr != nil {
		return r.setWorkspacePathErr
	}
	r.workspacePaths[id] = workspacePath
	return nil
}
func (r *fakeRuntimeInstanceRepo) UpdateWorkspaceUsage(ctx context.Context, id int, usageBytes int64) error {
	return nil
}
func (r *fakeRuntimeInstanceRepo) Update(instance *models.Instance) error { return nil }
func (r *fakeRuntimeInstanceRepo) Delete(id int) error                    { return nil }

type fakeRuntimePodRepo struct {
	pods         map[int64]*models.RuntimePod
	schedulable  []models.RuntimePod
	claims       map[int64]int
	releases     map[int64]int
	marked       map[int64]fakePodMark
	releaseCount int
	getErr       error
}

type fakePodMark struct {
	state    string
	draining bool
}

func (r *fakeRuntimePodRepo) UpsertFromAgent(ctx context.Context, pod *models.RuntimePod) error {
	return nil
}
func (r *fakeRuntimePodRepo) GetByID(ctx context.Context, id int64) (*models.RuntimePod, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.pods[id], nil
}
func (r *fakeRuntimePodRepo) GetByNamespaceName(ctx context.Context, namespace, podName string) (*models.RuntimePod, error) {
	return nil, nil
}
func (r *fakeRuntimePodRepo) List(ctx context.Context, runtimeType string) ([]models.RuntimePod, error) {
	var pods []models.RuntimePod
	for _, pod := range r.pods {
		if runtimeType == "" || pod.RuntimeType == runtimeType {
			pods = append(pods, *pod)
		}
	}
	return pods, nil
}
func (r *fakeRuntimePodRepo) ListSchedulable(ctx context.Context, runtimeType string) ([]models.RuntimePod, error) {
	return r.schedulable, nil
}
func (r *fakeRuntimePodRepo) TryClaimSlot(ctx context.Context, podID int64) (bool, error) {
	if r.claims == nil {
		r.claims = map[int64]int{}
	}
	r.claims[podID]++
	return true, nil
}
func (r *fakeRuntimePodRepo) ReleaseSlot(ctx context.Context, podID int64) error {
	if r.releases == nil {
		r.releases = map[int64]int{}
	}
	r.releases[podID]++
	r.releaseCount++
	return nil
}
func (r *fakeRuntimePodRepo) MarkState(ctx context.Context, podID int64, state string, draining bool) error {
	if r.marked == nil {
		r.marked = map[int64]fakePodMark{}
	}
	r.marked[podID] = fakePodMark{state: state, draining: draining}
	return nil
}
func (r *fakeRuntimePodRepo) UpdateHeartbeat(ctx context.Context, podID int64, state string, usedSlots int, capacity int, draining bool, lastSeenAt time.Time) error {
	if r.pods != nil {
		if pod := r.pods[podID]; pod != nil {
			pod.State = state
			pod.UsedSlots = usedSlots
			pod.Capacity = capacity
			pod.Draining = draining
			pod.LastSeenAt = &lastSeenAt
		}
	}
	return nil
}
func (r *fakeRuntimePodRepo) MarkUnseenUnhealthy(ctx context.Context, cutoff time.Time) error {
	return nil
}
func (r *fakeRuntimePodRepo) UpdateMetrics(ctx context.Context, podID int64, metrics repository.RuntimePodMetricsUpdate) error {
	return nil
}

type fakeRuntimeBindingRepo struct {
	bindings              map[int]*models.InstanceRuntimeBinding
	deleteCalls           map[int]int
	deleteAndReleaseCalls map[int]int
	runningErr            error
	getErr                error
	createErr             error
}

func newFakeRuntimeBindingRepo() *fakeRuntimeBindingRepo {
	return &fakeRuntimeBindingRepo{
		bindings:              map[int]*models.InstanceRuntimeBinding{},
		deleteCalls:           map[int]int{},
		deleteAndReleaseCalls: map[int]int{},
	}
}

func (r *fakeRuntimeBindingRepo) Create(ctx context.Context, binding *models.InstanceRuntimeBinding) error {
	if r.createErr != nil {
		return r.createErr
	}
	copy := *binding
	r.bindings[binding.InstanceID] = &copy
	return nil
}
func (r *fakeRuntimeBindingRepo) GetByInstanceID(ctx context.Context, instanceID int) (*models.InstanceRuntimeBinding, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.bindings[instanceID], nil
}
func (r *fakeRuntimeBindingRepo) GetRunningByInstanceID(ctx context.Context, instanceID int) (*models.InstanceRuntimeBinding, error) {
	if r.runningErr != nil {
		return nil, r.runningErr
	}
	binding := r.bindings[instanceID]
	if binding == nil || binding.State != "running" {
		return nil, nil
	}
	return binding, nil
}
func (r *fakeRuntimeBindingRepo) ListByRuntimePodID(ctx context.Context, runtimePodID int64) ([]models.InstanceRuntimeBinding, error) {
	var bindings []models.InstanceRuntimeBinding
	for _, binding := range r.bindings {
		if binding.RuntimePodID == runtimePodID {
			bindings = append(bindings, *binding)
		}
	}
	return bindings, nil
}
func (r *fakeRuntimeBindingRepo) ListByRuntimePodIDs(ctx context.Context, runtimePodIDs []int64) ([]models.InstanceRuntimeBinding, error) {
	return nil, nil
}
func (r *fakeRuntimeBindingRepo) UpdateRunning(ctx context.Context, instanceID int, generation int, gatewayID string, port int, pid *int) error {
	return nil
}
func (r *fakeRuntimeBindingRepo) UpdateState(ctx context.Context, instanceID int, generation int, state string, message *string) error {
	return nil
}
func (r *fakeRuntimeBindingRepo) DeleteByInstanceID(ctx context.Context, instanceID int) error {
	r.deleteCalls[instanceID]++
	delete(r.bindings, instanceID)
	return nil
}
func (r *fakeRuntimeBindingRepo) DeleteByInstanceIDAndReleaseSlot(ctx context.Context, instanceID int, runtimePodID int64) error {
	r.deleteAndReleaseCalls[instanceID]++
	delete(r.bindings, instanceID)
	return nil
}

type fakeRuntimeRolloutRepo struct {
	rollouts map[int64]*models.RuntimeRollout
	statuses []fakeRolloutStatus
}

type fakeRolloutStatus struct {
	id         int64
	status     string
	startedAt  *time.Time
	finishedAt *time.Time
	message    *string
}

func (r *fakeRuntimeRolloutRepo) Create(ctx context.Context, rollout *models.RuntimeRollout) error {
	return nil
}
func (r *fakeRuntimeRolloutRepo) GetByID(ctx context.Context, id int64) (*models.RuntimeRollout, error) {
	if r.rollouts == nil {
		return nil, nil
	}
	return r.rollouts[id], nil
}
func (r *fakeRuntimeRolloutRepo) ListActive(ctx context.Context, runtimeType string) ([]models.RuntimeRollout, error) {
	var active []models.RuntimeRollout
	for _, rollout := range r.rollouts {
		if rollout == nil {
			continue
		}
		if runtimeType != "" && rollout.RuntimeType != runtimeType {
			continue
		}
		if rollout.Status == "pending" || rollout.Status == "running" {
			active = append(active, *rollout)
		}
	}
	return active, nil
}
func (r *fakeRuntimeRolloutRepo) UpdateStatus(ctx context.Context, id int64, status string, startedAt *time.Time, finishedAt *time.Time, message *string) error {
	r.statuses = append(r.statuses, fakeRolloutStatus{id: id, status: status, startedAt: startedAt, finishedAt: finishedAt, message: message})
	return nil
}

type fakeRuntimeAgentClient struct {
	createResponse *RuntimeAgentCreateGatewayResponse
	createErr      error
	deleteErr      error
	createRequests []fakeCreateGatewayRequest
	deleteRequests []fakeDeleteGatewayRequest
	drainEndpoints []string
}

type fakeCreateGatewayRequest struct {
	endpoint string
	req      RuntimeAgentCreateGatewayRequest
}

type fakeDeleteGatewayRequest struct {
	endpoint  string
	gatewayID string
}

func (c *fakeRuntimeAgentClient) Health(ctx context.Context, endpoint string) error { return nil }
func (c *fakeRuntimeAgentClient) CreateGateway(ctx context.Context, endpoint string, req RuntimeAgentCreateGatewayRequest) (*RuntimeAgentCreateGatewayResponse, error) {
	c.createRequests = append(c.createRequests, fakeCreateGatewayRequest{endpoint: endpoint, req: req})
	if c.createErr != nil {
		return nil, c.createErr
	}
	if c.createResponse == nil {
		return nil, errors.New("missing create response")
	}
	return c.createResponse, nil
}
func (c *fakeRuntimeAgentClient) DeleteGateway(ctx context.Context, endpoint, gatewayID string) error {
	c.deleteRequests = append(c.deleteRequests, fakeDeleteGatewayRequest{endpoint: endpoint, gatewayID: gatewayID})
	return c.deleteErr
}
func (c *fakeRuntimeAgentClient) Drain(ctx context.Context, endpoint string) error {
	c.drainEndpoints = append(c.drainEndpoints, endpoint)
	return nil
}

type fakeRuntimeEventService struct {
	published []fakeRuntimeEvent
}

type fakeRuntimeEvent struct {
	eventType string
	payload   any
}

func (s *fakeRuntimeEventService) Publish(ctx context.Context, eventType string, payload any) error {
	s.published = append(s.published, fakeRuntimeEvent{eventType: eventType, payload: payload})
	return nil
}
func (s *fakeRuntimeEventService) Read(ctx context.Context, lastID string, block time.Duration) ([]redisStreamMessage, error) {
	return nil, nil
}

type fakeRuntimeDeploymentService struct {
	ensureCalls       int
	scaleCalls        int
	scales            []fakeScaleCall
	rolloutImageCalls []fakeRolloutImageCall
	pods              []k8s.RuntimeDeploymentPod
}

type fakeScaleCall struct {
	namespace string
	name      string
	replicas  int32
}

type fakeRolloutImageCall struct {
	namespace      string
	name           string
	image          string
	maxUnavailable int
	maxSurge       int
}

func (s *fakeRuntimeDeploymentService) Ensure(ctx context.Context, spec k8s.RuntimeDeploymentSpec) error {
	s.ensureCalls++
	return nil
}
func (s *fakeRuntimeDeploymentService) Scale(ctx context.Context, namespace, name string, replicas int32) error {
	s.scaleCalls++
	s.scales = append(s.scales, fakeScaleCall{namespace: namespace, name: name, replicas: replicas})
	return nil
}
func (s *fakeRuntimeDeploymentService) RolloutImage(ctx context.Context, namespace, name, image string, maxUnavailable, maxSurge int) error {
	s.rolloutImageCalls = append(s.rolloutImageCalls, fakeRolloutImageCall{
		namespace:      namespace,
		name:           name,
		image:          image,
		maxUnavailable: maxUnavailable,
		maxSurge:       maxSurge,
	})
	return nil
}
func (s *fakeRuntimeDeploymentService) ListPods(ctx context.Context, namespace, runtimeType string) ([]k8s.RuntimeDeploymentPod, error) {
	var pods []k8s.RuntimeDeploymentPod
	for _, pod := range s.pods {
		if namespace != "" && pod.Namespace != namespace {
			continue
		}
		if runtimeType != "" && pod.RuntimeType != runtimeType {
			continue
		}
		pods = append(pods, pod)
	}
	return pods, nil
}

func TestIsSchedulerManagedV2InstanceRejectsProDesktop(t *testing.T) {
	workspacePath := "/workspaces/openclaw/user-7/instance-42"
	instance := models.Instance{
		ID:            42,
		Type:          RuntimeTypeOpenClaw,
		RuntimeType:   RuntimeBackendDesktop,
		InstanceMode:  InstanceModePro,
		WorkspacePath: &workspacePath,
	}
	if isSchedulerManagedV2Instance(instance) {
		t.Fatal("pro desktop instance must not be scheduler-managed")
	}

	instance.RuntimeType = RuntimeBackendGateway
	instance.InstanceMode = InstanceModeLite
	if !isSchedulerManagedV2Instance(instance) {
		t.Fatal("lite gateway instance should be scheduler-managed")
	}
}

func ptrString(value string) *string {
	return &value
}
