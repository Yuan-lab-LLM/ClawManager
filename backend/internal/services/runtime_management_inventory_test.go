package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
)

type managementInventoryDeployments struct {
	*fakeRuntimeDeploymentService
	listCalls int
}

func (s *managementInventoryDeployments) ListPods(ctx context.Context, namespace, runtimeType string) ([]k8s.RuntimeDeploymentPod, error) {
	s.listCalls++
	return s.fakeRuntimeDeploymentService.ListPods(ctx, namespace, runtimeType)
}

type managementInventoryPods struct{ *fakeRuntimePodRepo }

func (r *managementInventoryPods) SetSchedulingPaused(_ context.Context, id int64, paused bool) error {
	r.pods[id].SchedulingPaused = paused
	return nil
}

func managementInventoryFixture() (*RuntimeManagementService, *managementInventoryDeployments) {
	source, target := elasticManagementPod(9), elasticManagementPod(4)
	source.UsedSlots = 3
	pods := &managementInventoryPods{&fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{9: &source, 4: &target}}}
	instances, bindings := newFakeRuntimeInstanceRepo(), newFakeRuntimeBindingRepo()
	for i, id := range []int{45, 46, 47} {
		instances.byID[id] = &models.Instance{ID: id, UserID: 1, Type: source.RuntimeType, InstanceMode: "lite", Status: "running", RuntimeGeneration: 7, CPUCores: 2, MemoryGB: 4}
		bindings.bindings[id] = &models.InstanceRuntimeBinding{InstanceID: id, RuntimePodID: 9, RuntimeType: source.RuntimeType, GatewayID: "gateway", GatewayPort: 20000 + i, Generation: 7, State: "running"}
	}
	deployments := &managementInventoryDeployments{fakeRuntimeDeploymentService: &fakeRuntimeDeploymentService{}}
	for _, pod := range []models.RuntimePod{source, target} {
		deployments.pods = append(deployments.pods, k8s.RuntimeDeploymentPod{RuntimeType: pod.RuntimeType, Namespace: pod.Namespace, DeploymentName: pod.DeploymentName, PodName: pod.PodName, PodUID: *pod.PodUID, State: "ready", ImageRef: pod.ImageRef, ImageDigest: *pod.ImageDigest, Capacity: pod.Capacity, Slots: pod.Slots, WorkspaceStorageIdentity: pod.WorkspaceStorageIdentity, CPUMillisLimit: pod.CPUMillisLimit, MemoryBytesLimit: pod.MemoryBytesLimit, CPUMillisRequest: pod.CPUMillisRequest, MemoryBytesRequest: pod.MemoryBytesRequest, CoordinatorCPUMillisRequest: pod.CoordinatorCPUMillisRequest, CoordinatorMemoryBytesRequest: pod.CoordinatorMemoryBytesRequest, CoordinatorCPUMillis: pod.CoordinatorCPUMillis, CoordinatorMemoryBytes: pod.CoordinatorMemoryBytes})
	}
	agent := &strictStopRuntimeAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, strict: true}
	scheduler := NewRuntimeScheduler(instances, pods, bindings, nil, agent, nil, nil, deployments, time.Second, WithRuntimeSchedulerNamespace("runtime"))
	return &RuntimeManagementService{scheduler: scheduler}, deployments
}

func TestRuntimeManagementPreflightInventoryIsFreshPerRequest(t *testing.T) {
	service, deployments := managementInventoryFixture()
	req := RuntimeSchedulingRequest{InstanceIDs: []int{45, 46, 47}, TargetPodID: 4}
	plan, err := service.Preflight(context.Background(), req)
	if err != nil || !plan.Allowed || len(plan.Items) != 3 {
		t.Fatalf("batch preflight=%+v err=%v", plan, err)
	}
	for i, item := range plan.Items {
		if item.TargetSlot != i+1 || item.SourcePodUID != "uid-9" || item.TargetPodUID != "uid-4" {
			t.Fatalf("unexpected batch slot/identity: %+v", item)
		}
	}
	if deployments.listCalls != 1 {
		t.Fatalf("batch fetched live inventory %d times, want once", deployments.listCalls)
	}
	// A later HTTP request must see replacement and reject its stale Agent UID.
	deployments.pods[1].PodUID = "replacement-uid"
	plan, err = service.Preflight(context.Background(), req)
	if err != nil || plan.Allowed || !strings.Contains(strings.Join(plan.Blockers, " "), "Pod UID must agree") {
		t.Fatalf("new request reused stale target inventory: %+v err=%v", plan, err)
	}
	if deployments.listCalls != 2 {
		t.Fatalf("second request fetched inventory %d total times, want twice", deployments.listCalls)
	}
	if calls := service.scheduler.agentClient.(*strictStopRuntimeAgent).fakeRuntimeAgentClient; len(calls.deleteRequests) != 0 || len(calls.createRequests) != 0 {
		t.Fatal("preflight reached gateway mutation boundary")
	}
}

func TestRuntimeManagementGracefulNoDestinationDiscoversInventoryOnce(t *testing.T) {
	service, deployments := managementInventoryFixture()
	// The source is the only Pod in its deployment. Other Pods cannot share its
	// workspace placement even when their runtime/image and free slots match.
	deployments.pods[1].DeploymentName = "another-upgrade-pool"
	if _, err := service.Evict(context.Background(), 9, RuntimeSchedulingRequest{Mode: "graceful"}, nil); err == nil || !strings.Contains(err.Error(), "no ready compatible destination") {
		t.Fatalf("unexpected no-capacity result: %v", err)
	}
	if deployments.listCalls != 1 {
		t.Fatalf("no-capacity eviction fetched live inventory %d times, want once", deployments.listCalls)
	}
	source, err := service.scheduler.podRepo.GetByID(context.Background(), 9)
	if err != nil || !source.SchedulingPaused {
		t.Fatalf("source did not stay paused: %+v err=%v", source, err)
	}
	if calls := service.scheduler.agentClient.(*strictStopRuntimeAgent).fakeRuntimeAgentClient; len(calls.deleteRequests) != 0 || len(calls.createRequests) != 0 {
		t.Fatal("no-capacity rejection reached gateway mutation boundary")
	}
}
