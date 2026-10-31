package services

import (
	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRuntimeManagementTargetValidation(t *testing.T) {
	source := models.RuntimePod{ID: 1, RuntimeType: "openclaw", Namespace: "runtime", DeploymentName: "openclaw", ImageRef: "image@sha256:aaa", ImageDigest: stringPtrOrNil("sha256:aaa"), WorkspaceStorageIdentity: "pvc:r/workspaces"}
	target := source
	target.ID = 2
	target.State = "ready"
	target.Capacity = 3
	if err := validateRuntimeManagementTarget(source, target); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*models.RuntimePod){func(p *models.RuntimePod) { p.RuntimeType = "hermes" }, func(p *models.RuntimePod) { p.ImageRef = "other" }, func(p *models.RuntimePod) { p.State = "unhealthy" }, func(p *models.RuntimePod) { p.Draining = true }, func(p *models.RuntimePod) { p.SchedulingPaused = true }, func(p *models.RuntimePod) { p.DeploymentName = "other-volume" }, func(p *models.RuntimePod) { p.ID = 0 }} {
		bad := target
		mutate(&bad)
		if err := validateRuntimeManagementTarget(source, bad); err == nil {
			t.Fatalf("accepted incompatible target: %+v", bad)
		}
	}
}

func TestRuntimeManagementSlotPlanNeverOverlaps(t *testing.T) {
	p := models.RuntimePod{Capacity: 3, RuntimeType: "openclaw"}
	bindings := []models.InstanceRuntimeBinding{{GatewayPort: 20000}}
	slots, err := runtimeManagementFreeSlots(p, bindings, 20000, 20099)
	if err != nil || len(slots) != 2 || slots[0] != 2 || slots[1] != 3 {
		t.Fatalf("slots=%v err=%v", slots, err)
	}
}

func TestRuntimeManagementRejectsResourceOvercommit(t *testing.T) {
	pod := elasticManagementPod(4)
	if err := runtimeManagementResourceBudget(pod); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*models.RuntimePod){
		func(p *models.RuntimePod) {
			value := int64(1000)
			p.Slots[7].CPUMillisRequest = &value
			p.CPUMillisRequest = &value
		},
		func(p *models.RuntimePod) { value := int64(16 << 30); p.Slots[7].MemoryBytesRequest = &value },
		func(p *models.RuntimePod) { p.Slots[7].CPUMillisRequest = nil },
		func(p *models.RuntimePod) { p.CoordinatorMemoryBytesRequest = nil },
		func(p *models.RuntimePod) { p.CPUMillisRequest = nil },
	} {
		bad := elasticManagementPod(4)
		mutate(&bad)
		if runtimeManagementResourceBudget(bad) == nil {
			t.Fatal("invalid or unknown static reservation accepted")
		}
	}
	if runtimeManagementResourceBudget(models.RuntimePod{}) == nil {
		t.Fatal("unknown capacity accepted")
	}
}

func TestRuntimeManagementStatusWaitsForHealth(t *testing.T) {
	for _, tc := range []struct {
		states []string
		want   string
	}{
		{[]string{"completed", "completed"}, "completed"}, {[]string{"starting", "completed"}, "starting"}, {[]string{"failed", "completed"}, "partial_failure"}, {[]string{"failed", "failed"}, "failed"}, {[]string{"failed", "starting"}, "starting"},
	} {
		items := []RuntimeSchedulingItem{}
		for _, state := range tc.states {
			items = append(items, RuntimeSchedulingItem{State: state})
		}
		if got := runtimeManagementStatus(items); got != tc.want {
			t.Fatalf("%v => %s, want %s", tc.states, got, tc.want)
		}
	}
}

type managementFenceRepo struct{ *fakeRuntimeInstanceRepo }

func (r *managementFenceRepo) FenceRuntimeRestart(ctx context.Context, id, generation int) (bool, error) {
	instance := r.byID[id]
	if instance.RuntimeGeneration != generation {
		return false, nil
	}
	return true, r.UpdateRuntimeState(ctx, id, "stopped", generation+1, nil)
}

func TestRuntimeManagementNeverStartsWithoutSourceReceipt(t *testing.T) {
	now := time.Now()
	zero := int64(0)
	port1, port2 := 20000, 20003
	cpu, mem := int64(2000), int64(4*1024*1024*1024)
	source := models.RuntimePod{ID: 1, RuntimeType: "opencode", Namespace: "runtime", PodName: "source", DeploymentName: "runtime", ImageRef: "same", ImageDigest: stringPtrOrNil("sha256:same"), State: "ready", Capacity: 2, PodUID: stringPtrOrNil("source-uid"), AgentEndpoint: stringPtrOrNil("https://source"), LastSeenAt: &now}
	target := source
	target.ID = 2
	target.PodName = "target"
	target.PodUID = stringPtrOrNil("target-uid")
	target.AgentEndpoint = stringPtrOrNil("https://target")
	pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{1: &source, 2: &target}}
	instances := &managementFenceRepo{newFakeRuntimeInstanceRepo()}
	instances.byID[7] = &models.Instance{ID: 7, Type: "opencode", InstanceMode: "lite", Status: "running", RuntimeGeneration: 1, CPUCores: 1, MemoryGB: 1}
	bindings := newFakeRuntimeBindingRepo()
	bindings.bindings[7] = &models.InstanceRuntimeBinding{InstanceID: 7, RuntimePodID: 1, RuntimeType: "opencode", GatewayPort: 20000, GatewayID: "gw-7-1", State: "running", Generation: 1}
	agent := &strictStopRuntimeAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{deleteErr: errors.New("missing source stop receipt")}, strict: true}
	deployments := &fakeRuntimeDeploymentService{pods: []k8s.RuntimeDeploymentPod{{RuntimeType: "opencode", Namespace: "runtime", DeploymentName: "runtime", PodName: "source", PodUID: "source-uid", ImageRef: "same", ImageDigest: "sha256:same", WorkspaceStorageIdentity: "pvc:runtime/workspaces", CoordinatorCPUMillis: &zero, CoordinatorMemoryBytes: &zero, State: "ready", Capacity: 2}, {RuntimeType: "opencode", Namespace: "runtime", DeploymentName: "runtime", PodName: "target", PodUID: "target-uid", ImageRef: "same", ImageDigest: "sha256:same", WorkspaceStorageIdentity: "pvc:runtime/workspaces", CoordinatorCPUMillis: &zero, CoordinatorMemoryBytes: &zero, State: "ready", Capacity: 2, CPUMillisLimit: &cpu, MemoryBytesLimit: &mem, Slots: []models.RuntimeSlot{{Index: 1, Ready: true, GatewayPort: &port1, CPUMillisLimit: &cpu, MemoryBytesLimit: &mem}, {Index: 2, Ready: true, GatewayPort: &port2, CPUMillisLimit: &cpu, MemoryBytesLimit: &mem}}}}}
	for i := range deployments.pods {
		p := &deployments.pods[i]
		p.CPUMillisLimit, p.MemoryBytesLimit = &cpu, &mem
		p.CPUMillisRequest, p.MemoryBytesRequest = &cpu, &mem
		p.CoordinatorCPUMillisRequest, p.CoordinatorMemoryBytesRequest = &zero, &zero
		if len(p.Slots) == 0 {
			p.Slots = append([]models.RuntimeSlot{}, deployments.pods[1].Slots...)
		}
		for j := range p.Slots {
			p.Slots[j].CPUMillisRequest, p.Slots[j].MemoryBytesRequest = &zero, &zero
		}
	}
	sch := NewRuntimeScheduler(instances, pods, bindings, nil, agent, nil, nil, deployments, time.Second, WithRuntimeSchedulerNamespace("runtime"))
	service := &RuntimeManagementService{scheduler: sch}
	err := service.move(context.Background(), RuntimeSchedulingItem{InstanceID: 7, SourcePodID: 1, TargetPodID: 2, TargetSlot: 1, SourceGeneration: 1, SourceGatewayID: "gw-7-1", SourcePodUID: "source-uid", TargetPodUID: "target-uid"})
	if err == nil || !strings.Contains(err.Error(), "source stop unconfirmed") {
		t.Fatalf("unexpected error %v", err)
	}
	if bindings.bindings[7] == nil || bindings.bindings[7].RuntimePodID != 1 || len(agent.createRequests) > 0 || pods.claims[2] != 1 || pods.releases[2] != 1 {
		t.Fatal("unconfirmed source stop mutated destination or released binding")
	}
}

func TestRuntimeManagementRequiresMatchingRunningDigest(t *testing.T) {
	source := models.RuntimePod{ID: 1, RuntimeType: "opencode", Namespace: "r", DeploymentName: "r", ImageRef: "mutable:latest", ImageDigest: stringPtrOrNil("sha256:old")}
	target := source
	target.ID = 2
	target.State = "ready"
	target.Capacity = 2
	target.ImageDigest = stringPtrOrNil("sha256:new")
	if validateRuntimeManagementTarget(source, target) == nil {
		t.Fatal("same tag with different running image digests accepted")
	}
	target.ImageDigest = nil
	if validateRuntimeManagementTarget(source, target) == nil {
		t.Fatal("unknown running image accepted")
	}
}

func TestRuntimeManagementSlotHardLimits(t *testing.T) {
	source, target := elasticManagementPod(9), elasticManagementPod(4)
	required, err := runtimeManagementSourceSlot(source, models.InstanceRuntimeBinding{GatewayPort: 20004})
	if err != nil {
		t.Fatal(err)
	}
	if err = runtimeManagementSlotResources(target, target.Slots[7], required); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*models.RuntimePod){
		func(p *models.RuntimePod) { value := int64(500); p.Slots[7].CPUMillisLimit = &value },
		func(p *models.RuntimePod) { value := int64(2 << 30); p.MemoryBytesLimit = &value },
		func(p *models.RuntimePod) { value := int64(50); p.Slots[7].CPUMillisRequest = &value },
		func(p *models.RuntimePod) { value := int64(512 << 20); p.Slots[7].MemoryBytesRequest = &value },
		func(p *models.RuntimePod) { p.Slots[7].MemoryBytesLimit = nil },
	} {
		bad := elasticManagementPod(4)
		mutate(&bad)
		if runtimeManagementSlotResources(bad, bad.Slots[7], required) == nil {
			t.Fatal("weaker or unknown physical target accepted")
		}
	}
	if _, err = runtimeManagementSourceSlot(source, models.InstanceRuntimeBinding{GatewayPort: 29999}); err == nil {
		t.Fatal("unknown source port accepted")
	}
}

func TestRuntimeManagementForceRequiresConfirmedStop(t *testing.T) {
	service := &RuntimeManagementService{scheduler: &RuntimeScheduler{agentClient: &fakeRuntimeAgentClient{}}}
	if modes := service.SupportedEvictionModes(); len(modes) != 1 || modes[0] != "graceful" {
		t.Fatalf("legacy modes=%v", modes)
	}
	if _, err := service.Evict(context.Background(), 1, RuntimeSchedulingRequest{Mode: "force"}, nil); err == nil {
		t.Fatal("legacy Agent accepted force")
	}
	if status := runtimeManagementStatus([]RuntimeSchedulingItem{{State: "waiting_capacity"}}); status != "starting" {
		t.Fatalf("pending work marked complete: %s", status)
	}
}
