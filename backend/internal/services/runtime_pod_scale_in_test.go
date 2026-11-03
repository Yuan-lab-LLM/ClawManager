package services

import (
	"clawreef/internal/models"
	"context"
	"testing"
	"time"
)

func TestScaleInSourcesAreDeterministicAndPreserveOnePod(t *testing.T) {
	pods := []models.RuntimePod{elasticManagementPod(3), elasticManagementPod(1), elasticManagementPod(2)}
	pods[0].UsedSlots = 2
	pods[1].UsedSlots = 1
	pods[2].UsedSlots = 1
	sources, err := runtimeScaleInSelectSources(pods, 2)
	if err != nil {
		t.Fatal(err)
	}
	if sources[0].ID != 1 || sources[1].ID != 2 {
		t.Fatalf("unstable source set: %+v", sources)
	}
	if _, err = runtimeScaleInSelectSources(pods, 3); err == nil {
		t.Fatal("removed final Runtime Pod")
	}
}

func TestScaleInPlanAllocatesDistinctSlotsAndBlocksInsufficientCapacity(t *testing.T) {
	source, target := elasticManagementPod(1), elasticManagementPod(2)
	source.UsedSlots = 2
	target.UsedSlots = 7
	pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{1: &source, 2: &target}}
	instances := newFakeRuntimeInstanceRepo()
	bindings := newFakeRuntimeBindingRepo()
	for _, id := range []int{41, 42} {
		instances.byID[id] = &models.Instance{ID: id, UserID: 1, Type: source.RuntimeType, InstanceMode: "lite", Status: "running", RuntimeGeneration: 1}
		bindings.bindings[id] = &models.InstanceRuntimeBinding{InstanceID: id, RuntimePodID: 1, RuntimeType: source.RuntimeType, GatewayID: "gw", GatewayPort: 20000 + id - 41, Generation: 1, State: "running"}
	}
	for i := 0; i < 7; i++ {
		id := 100 + i
		bindings.bindings[id] = &models.InstanceRuntimeBinding{InstanceID: id, RuntimePodID: 2, GatewayPort: 20000 + i, Generation: 1, State: "running"}
	}
	scheduler := NewRuntimeScheduler(instances, pods, bindings, nil, &strictStopRuntimeAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, strict: true}, nil, nil, &fakeRuntimeDeploymentService{}, time.Second, WithRuntimeSchedulerNamespace("runtime"))
	service := &RuntimeManagementService{scheduler: scheduler}
	inventory := []models.RuntimePod{source, target}
	for i := range inventory {
		inventory[i].KubernetesReady = true
		inventory[i].KubernetesPodUID = inventory[i].PodUID
		inventory[i].RegisteredPodUID = inventory[i].PodUID
		inventory[i].AgentState = "ready"
	}
	plan, err := service.runtimeScaleInPlanItems(context.Background(), inventory, []models.RuntimePod{inventory[0]})
	if err == nil || len(plan) != 0 {
		t.Fatalf("admitted insufficient batch: %v %v", plan, err)
	}
	delete(bindings.bindings, 106)
	target.UsedSlots = 6
	inventory[1].UsedSlots = 6
	plan, err = service.runtimeScaleInPlanItems(context.Background(), inventory, []models.RuntimePod{inventory[0]})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 2 || plan[0].TargetSlot == plan[1].TargetSlot {
		t.Fatalf("overlapping plan: %+v", plan)
	}
	// Same total capacity with a weaker physical slot is insufficient.
	small := int64(1)
	inventory[1].Slots[7].MemoryBytesLimit = &small
	if _, err = service.runtimeScaleInPlanItems(context.Background(), inventory, []models.RuntimePod{inventory[0]}); err == nil {
		t.Fatal("admitted weaker physical slot")
	}
	if len(pods.claims) != 0 {
		t.Fatal("planning claimed live capacity")
	}
}

func TestScaleInRejectsStaleSourceAndUnsettledClaims(t *testing.T) {
	pod := elasticManagementPod(1)
	pod.KubernetesPodUID = pod.PodUID
	pod.RegisteredPodUID = pod.PodUID
	pod.KubernetesReady = true
	pod.AgentState = "ready"
	if runtimeScaleInFreshPod(pod, time.Minute) == nil { // valid baseline
	} else {
		t.Fatal("fresh source rejected")
	}
	old := time.Now().Add(-time.Hour)
	pod.LastSeenAt = &old
	if runtimeScaleInFreshPod(pod, time.Minute) == nil {
		t.Fatal("stale source accepted")
	}
}
