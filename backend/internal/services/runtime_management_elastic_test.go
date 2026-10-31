package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
)

func elasticManagementPod(id int64) models.RuntimePod {
	cpu, memory := int64(4000), int64(16<<30)
	coordCPU, coordMemory := int64(100), int64(128<<20)
	slotCPU, slotMemory, requestCPU, requestMemory := int64(1000), int64(4<<30), int64(100), int64(1<<30)
	now := time.Now()
	pod := models.RuntimePod{ID: id, RuntimeType: "deepseek-harness", Namespace: "runtime", DeploymentName: "deepseek", PodName: fmt.Sprintf("pod-%d", id), PodUID: stringPtrOrNil(fmt.Sprintf("uid-%d", id)), AgentEndpoint: stringPtrOrNil(fmt.Sprintf("https://agent-%d", id)), State: "ready", Capacity: 8, LastSeenAt: &now, ImageRef: "image", ImageDigest: stringPtrOrNil("sha256:same"), WorkspaceStorageIdentity: "pvc:runtime/shared", CPUMillisLimit: &cpu, MemoryBytesLimit: &memory, CPUMillisRequest: &cpu, MemoryBytesRequest: &memory, CoordinatorCPUMillisRequest: &coordCPU, CoordinatorMemoryBytesRequest: &coordMemory, CoordinatorCPUMillis: &coordCPU, CoordinatorMemoryBytes: &coordMemory}
	for i := 1; i <= 8; i++ {
		port := 20000 + i - 1
		pod.Slots = append(pod.Slots, models.RuntimeSlot{Index: i, ContainerName: fmt.Sprintf("slot-%d", i-1), GatewayPort: &port, Ready: true, CPUMillisLimit: &slotCPU, MemoryBytesLimit: &slotMemory, CPUMillisRequest: &requestCPU, MemoryBytesRequest: &requestMemory})
	}
	return pod
}

func TestRuntimeManagementElasticPreflightUsesPhysicalSlots(t *testing.T) {
	source, target := elasticManagementPod(9), elasticManagementPod(4)
	source.UsedSlots = 5
	target.UsedSlots = 6
	pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{9: &source, 4: &target}}
	instances := newFakeRuntimeInstanceRepo()
	bindings := newFakeRuntimeBindingRepo()
	instances.byID[45] = &models.Instance{ID: 45, UserID: 1, Type: "deepseek-harness", InstanceMode: "lite", Status: "running", RuntimeGeneration: 7, CPUCores: 2, MemoryGB: 4}
	bindings.bindings[45] = &models.InstanceRuntimeBinding{InstanceID: 45, RuntimePodID: 9, RuntimeType: source.RuntimeType, GatewayID: "gw-45-7", GatewayPort: 20004, Generation: 7, State: "running"}
	for i := 0; i < 6; i++ {
		id := 100 + i
		instances.byID[id] = &models.Instance{ID: id, Type: source.RuntimeType, InstanceMode: "lite", Status: "running", CPUCores: 2, MemoryGB: 4}
		bindings.bindings[id] = &models.InstanceRuntimeBinding{InstanceID: id, RuntimePodID: 4, GatewayPort: 20000 + i, State: "running", Generation: 1}
	}
	deployments := &fakeRuntimeDeploymentService{}
	for _, pod := range []models.RuntimePod{source, target} {
		deployments.pods = append(deployments.pods, k8s.RuntimeDeploymentPod{RuntimeType: pod.RuntimeType, Namespace: pod.Namespace, DeploymentName: pod.DeploymentName, PodName: pod.PodName, PodUID: *pod.PodUID, State: "ready", ImageRef: pod.ImageRef, ImageDigest: *pod.ImageDigest, Capacity: pod.Capacity, Slots: pod.Slots, WorkspaceStorageIdentity: pod.WorkspaceStorageIdentity, CPUMillisLimit: pod.CPUMillisLimit, MemoryBytesLimit: pod.MemoryBytesLimit, CPUMillisRequest: pod.CPUMillisRequest, MemoryBytesRequest: pod.MemoryBytesRequest, CoordinatorCPUMillisRequest: pod.CoordinatorCPUMillisRequest, CoordinatorMemoryBytesRequest: pod.CoordinatorMemoryBytesRequest, CoordinatorCPUMillis: pod.CoordinatorCPUMillis, CoordinatorMemoryBytes: pod.CoordinatorMemoryBytes})
	}
	agent := &strictStopRuntimeAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, strict: true}
	scheduler := NewRuntimeScheduler(instances, pods, bindings, nil, agent, nil, nil, deployments, time.Second, WithRuntimeSchedulerNamespace("runtime"))
	service := &RuntimeManagementService{scheduler: scheduler}
	result, err := service.Preflight(context.Background(), RuntimeSchedulingRequest{InstanceIDs: []int{45}, TargetPodID: 4, TargetSlot: 8})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Allowed || len(result.Items) != 1 || result.Items[0].TargetSlot != 8 {
		t.Fatalf("elastic slot migration rejected: %+v", result)
	}
	if len(agent.createRequests) != 0 || len(pods.claims) != 0 {
		t.Fatal("preflight mutated runtime")
	}
}

func TestRuntimeManagementPodPauseDoesNotSpreadAcrossDeployment(t *testing.T) {
	enabled, disabled := true, false
	pods := []models.RuntimePod{{ID: 4, Namespace: "runtime", DeploymentName: "deepseek", PodName: "target", PodUID: stringPtrOrNil("target-uid")}}
	inventory := []models.RuntimePod{
		{ID: 4, Namespace: "runtime", DeploymentName: "deepseek", PodName: "target", PodUID: stringPtrOrNil("target-uid"), SchedulingEnabled: &enabled},
		{ID: 9, Namespace: "runtime", DeploymentName: "deepseek", PodName: "source", PodUID: stringPtrOrNil("source-uid"), SchedulingEnabled: &disabled, SchedulingPaused: true},
	}
	annotated := annotateRuntimePods(pods, inventory)
	if len(filterOrdinaryRuntimePods(annotated)) != 1 {
		t.Fatal("pausing source excluded the ready target in the same Deployment")
	}
}
