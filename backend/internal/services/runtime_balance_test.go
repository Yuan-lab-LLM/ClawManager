package services

import (
	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRuntimeBalanceCountsEvenlyAndMinimizesMoves(t *testing.T) {
	for _, tc := range []struct{ before, capacity, want []int }{
		{[]int{8, 3}, []int{8, 8}, []int{6, 5}},
		{[]int{7, 4}, []int{8, 8}, []int{6, 5}},
		{[]int{8, 2, 1}, []int{8, 8, 8}, []int{4, 4, 3}},
		{[]int{1, 7, 3, 1}, []int{8, 8, 8, 8}, []int{3, 3, 3, 3}},
		{[]int{1, 7}, []int{1, 8}, []int{1, 7}},
		{[]int{5, 6}, []int{8, 8}, []int{5, 6}},
	} {
		if got := runtimeBalanceCounts(tc.before, tc.capacity); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%v => %v; want %v", tc.before, got, tc.want)
		}
	}
}

func TestRuntimeBalanceReportsCustomSlotResourceDifferences(t *testing.T) {
	service, pods, deployments, agent := runtimeBalanceFixture(t, []int{4, 0})
	cpu, memory := int64(500), int64(1<<30)
	for i := range deployments.pods[1].Slots {
		deployments.pods[1].Slots[i].CPUMillisLimit = &cpu
		deployments.pods[1].Slots[i].MemoryBytesLimit = &memory
	}
	plan, err := service.BalancePlan(context.Background(), RuntimeBalanceRequest{RuntimeType: "deepseek-harness"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Allowed || len(plan.Moves) != 0 || len(plan.Blocked) == 0 {
		t.Fatalf("weaker physical slots accepted: %+v", plan)
	}
	reasons := ""
	for _, blocked := range plan.Blocked {
		reasons += blocked.Reason + "\n"
	}
	for _, detail := range []string{"Pod #2", "500m", "1000m", "1Gi", "4Gi"} {
		if !strings.Contains(reasons, detail) {
			t.Fatalf("missing actionable resource difference %q in %s", detail, reasons)
		}
	}
	if len(agent.deleteRequests) > 0 || len(agent.createRequests) > 0 || len(pods.claims) > 0 {
		t.Fatal("resource explanation mutated a runtime")
	}
}

func runtimeBalanceFixture(t *testing.T, occupancy []int) (*RuntimeManagementService, *fakeRuntimePodRepo, *fakeRuntimeDeploymentService, *strictStopRuntimeAgent) {
	t.Helper()
	instances := newFakeRuntimeInstanceRepo()
	bindings := newFakeRuntimeBindingRepo()
	pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{}}
	deployments := &fakeRuntimeDeploymentService{}
	id := 1
	for i, count := range occupancy {
		pod := elasticManagementPod(int64(i + 1))
		pod.UsedSlots = count
		pods.pods[pod.ID] = &pod
		deployments.pods = append(deployments.pods, k8s.RuntimeDeploymentPod{RuntimeType: pod.RuntimeType, Namespace: pod.Namespace, DeploymentName: pod.DeploymentName, PodName: pod.PodName, PodUID: *pod.PodUID, State: "ready", ImageRef: pod.ImageRef, ImageDigest: *pod.ImageDigest, Capacity: pod.Capacity, Slots: pod.Slots, WorkspaceStorageIdentity: pod.WorkspaceStorageIdentity, CPUMillisLimit: pod.CPUMillisLimit, MemoryBytesLimit: pod.MemoryBytesLimit, CPUMillisRequest: pod.CPUMillisRequest, MemoryBytesRequest: pod.MemoryBytesRequest, CoordinatorCPUMillisRequest: pod.CoordinatorCPUMillisRequest, CoordinatorMemoryBytesRequest: pod.CoordinatorMemoryBytesRequest})
		for slot := 0; slot < count; slot++ {
			instances.byID[id] = &models.Instance{ID: id, Name: fmt.Sprintf("instance-%d", id), UserID: 1, Type: pod.RuntimeType, InstanceMode: "lite", Status: "running", RuntimeGeneration: 1}
			bindings.bindings[id] = &models.InstanceRuntimeBinding{InstanceID: id, RuntimePodID: pod.ID, RuntimeType: pod.RuntimeType, GatewayPort: 20000 + slot, GatewayID: fmt.Sprintf("gw-%d-1", id), Generation: 1, State: "running"}
			id++
		}
	}
	agent := &strictStopRuntimeAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, strict: true}
	scheduler := NewRuntimeScheduler(instances, pods, bindings, nil, agent, nil, nil, deployments, time.Minute, WithRuntimeSchedulerNamespace("runtime"), WithRuntimeSchedulerGatewayEnvBuilder(func(*models.Instance) (map[string]string, error) { return map[string]string{}, nil }))
	return &RuntimeManagementService{scheduler: scheduler}, pods, deployments, agent
}

func TestRuntimeBalancePlanReadOnlyMinimalStableAndCapacityAware(t *testing.T) {
	service, pods, deployments, agent := runtimeBalanceFixture(t, []int{8, 3})
	ctx := context.Background()
	first, err := service.BalancePlan(ctx, RuntimeBalanceRequest{RuntimeType: "deepseek-harness"})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Allowed || len(first.Moves) != 2 || len(first.Groups) != 1 || !first.Groups[0].Balanced || first.Groups[0].After[0].Count != 6 || first.Groups[0].After[1].Count != 5 {
		t.Fatalf("unexpected plan: %+v", first)
	}
	if first.Moves[0].TargetSlot == first.Moves[1].TargetSlot {
		t.Fatal("target slots overlap")
	}
	second, err := service.BalancePlan(ctx, RuntimeBalanceRequest{RuntimeType: "deepseek-harness"})
	if err != nil || first.PlanFingerprint != second.PlanFingerprint {
		t.Fatalf("unstable preview: %v", err)
	}
	for _, binding := range service.scheduler.bindingRepo.(*fakeRuntimeBindingRepo).bindings {
		now := time.Now()
		binding.LastHealthAt = &now
		binding.UpdatedAt = now
	}
	heartbeat, err := service.BalancePlan(ctx, RuntimeBalanceRequest{RuntimeType: "deepseek-harness"})
	if err != nil || first.PlanFingerprint != heartbeat.PlanFingerprint {
		t.Fatal("ordinary heartbeat invalidated unchanged placement plan")
	}
	deployments.pods[1].Slots[7].CPUMillisLimit = nil
	changed, err := service.BalancePlan(ctx, RuntimeBalanceRequest{RuntimeType: "deepseek-harness"})
	if err != nil || first.PlanFingerprint == changed.PlanFingerprint {
		t.Fatal("physical resource change did not invalidate confirmation")
	}
	if len(agent.deleteRequests) > 0 || len(agent.createRequests) > 0 || len(pods.claims) > 0 || len(pods.releases) > 0 {
		t.Fatal("preview mutated Runtime state")
	}
	if _, err := service.Balance(ctx, RuntimeBalanceRequest{RuntimeType: "deepseek-harness", PlanFingerprint: first.PlanFingerprint}, nil); err == nil {
		t.Fatal("stale confirmation accepted")
	}
}

func TestRuntimeBalancePlanReportsBlockedInstances(t *testing.T) {
	service, _, _, _ := runtimeBalanceFixture(t, []int{8, 0})
	for _, instance := range service.scheduler.instanceRepo.(*fakeRuntimeInstanceRepo).byID {
		instance.Status = "error"
	}
	plan, err := service.BalancePlan(context.Background(), RuntimeBalanceRequest{RuntimeType: "deepseek-harness"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Allowed || len(plan.Moves) != 0 || plan.Groups[0].Balanced || len(plan.Blocked) == 0 {
		t.Fatalf("blocked plan declared balanced: %+v", plan)
	}
}

func TestRuntimeBalancePlanKeepsUnavailableOccupantsFixed(t *testing.T) {
	for _, paused := range []bool{false, true} {
		service, pods, _, _ := runtimeBalanceFixture(t, []int{8, 8, 0})
		if paused {
			pods.pods[1].SchedulingPaused = true
		} else {
			for id, instance := range service.scheduler.instanceRepo.(*fakeRuntimeInstanceRepo).byID {
				if id <= 8 {
					instance.Status = "error"
				}
			}
		}
		plan, err := service.BalancePlan(context.Background(), RuntimeBalanceRequest{RuntimeType: "deepseek-harness"})
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Moves) != 4 || plan.Groups[0].After[0].Count != 8 || plan.Groups[0].After[1].Count != 4 || plan.Groups[0].After[2].Count != 4 || plan.Groups[0].Balanced || len(plan.Blocked) == 0 {
			t.Fatalf("unavailable occupants distorted eligible balance: %+v", plan)
		}
	}
}

func TestRuntimeBalancePlanReassignsFlexibleInstanceToScarceSlot(t *testing.T) {
	service, pods, deployments, agent := runtimeBalanceFixture(t, []int{4, 0, 0})
	smallCPU, smallMemory := int64(500), int64(2<<30)
	deployments.pods[0].Slots[0].CPUMillisLimit = &smallCPU
	deployments.pods[0].Slots[0].MemoryBytesLimit = &smallMemory
	for i := range deployments.pods[2].Slots {
		deployments.pods[2].Slots[i].CPUMillisLimit = &smallCPU
		deployments.pods[2].Slots[i].MemoryBytesLimit = &smallMemory
	}
	plan, err := service.BalancePlan(context.Background(), RuntimeBalanceRequest{RuntimeType: "deepseek-harness"})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Allowed || len(plan.Moves) != 2 || !plan.Groups[0].Balanced || plan.Groups[0].After[0].Count != 2 || plan.Groups[0].After[1].Count != 1 || plan.Groups[0].After[2].Count != 1 {
		t.Fatalf("feasible minimum move matching missed: %+v", plan)
	}
	for _, move := range plan.Moves {
		if move.TargetPodID == 3 && move.InstanceID != 1 {
			t.Fatal("large source placed in physically smaller slot")
		}
	}
	if len(agent.deleteRequests) > 0 || len(agent.createRequests) > 0 || len(pods.claims) > 0 {
		t.Fatal("matching preview mutated runtime")
	}
}

func TestRuntimeBalancePlanChoosesFeasibleReceiverAndDonorRemainders(t *testing.T) {
	smallCPU, smallMemory := int64(500), int64(2<<30)
	for _, donorRemainder := range []bool{false, true} {
		occupancy := []int{5, 0, 0}
		want := []int{2, 1, 2}
		moves := 3
		if donorRemainder {
			occupancy = []int{3, 2, 0, 0}
			want = []int{1, 2, 1, 1}
			moves = 2
		}
		service, _, deployments, _ := runtimeBalanceFixture(t, occupancy)
		smallSlot := func(pod, index int) {
			deployments.pods[pod].Slots[index].CPUMillisLimit = &smallCPU
			deployments.pods[pod].Slots[index].MemoryBytesLimit = &smallMemory
		}
		if donorRemainder {
			for i := 0; i < 3; i++ {
				smallSlot(0, i)
			}
			for pod := 2; pod < 4; pod++ {
				for i := range deployments.pods[pod].Slots {
					smallSlot(pod, i)
				}
			}
		} else {
			smallSlot(0, 0)
			for i := range deployments.pods[1].Slots {
				smallSlot(1, i)
			}
		}
		plan, err := service.BalancePlan(context.Background(), RuntimeBalanceRequest{RuntimeType: "deepseek-harness"})
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Moves) != moves || !plan.Groups[0].Balanced {
			t.Fatalf("feasible remainder ownership missed: %+v", plan)
		}
		for i, count := range want {
			if plan.Groups[0].After[i].Count != count {
				t.Fatalf("wrong feasible final counts: %+v", plan.Groups[0].After)
			}
		}
	}
}

func TestRuntimeBalancePlanIncludesManagedPoolWithDifferentSlotCount(t *testing.T) {
	service, pods, deployments, _ := runtimeBalanceFixture(t, []int{4, 0})
	for id, pod := range pods.pods {
		pod.CapabilitiesJSON = stringPtrOrNil(`["container-slots-v1"]`)
		slots := 8
		if id == 2 {
			slots = 2
			pod.DeploymentName = "managed-two-slots"
			pod.Capacity = slots
		}
		deployments.pods[id-1].DeploymentName = pod.DeploymentName
		deployments.pods[id-1].Capacity = slots
		deployments.pods[id-1].Slots = deployments.pods[id-1].Slots[:slots]
		deployments.pods[id-1].WorkspaceStorageIdentity = runtimeStorageIdentityForTest(slots, "pvc:runtime/shared:pvc-uid:pv-name", "/data", "", false, false)
	}
	plan, err := service.BalancePlan(context.Background(), RuntimeBalanceRequest{RuntimeType: "deepseek-harness"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Groups) != 1 || !plan.Groups[0].Balanced || len(plan.Moves) != 2 || plan.Groups[0].After[0].Count != 2 || plan.Groups[0].After[1].Count != 2 {
		t.Fatalf("managed smaller slot-count pool isolated: %+v", plan)
	}
}

func TestRuntimeBalancePlanBoundsVirtualClaimsByActualAdmission(t *testing.T) {
	service, pods, deployments, _ := runtimeBalanceFixture(t, []int{4, 4, 0, 0})
	pods.pods[3].UsedSlots = 1
	pods.pods[3].Capacity = 2
	deployments.pods[2].Capacity = 2
	deployments.pods[2].Slots = deployments.pods[2].Slots[:2]
	plan, err := service.BalancePlan(context.Background(), RuntimeBalanceRequest{RuntimeType: "deepseek-harness"})
	if err != nil {
		t.Fatal(err)
	}
	toConstrained := 0
	used := map[[2]int64]bool{}
	for _, move := range plan.Moves {
		if move.TargetPodID == 3 {
			toConstrained++
		}
		key := [2]int64{move.TargetPodID, int64(move.TargetSlot)}
		if used[key] {
			t.Fatal("physical target slot spent twice")
		}
		used[key] = true
	}
	if len(plan.Moves) != 3 || toConstrained != 1 || plan.Groups[0].Balanced || len(plan.Blocked) == 0 {
		t.Fatalf("outstanding claim was reused as free admission: %+v", plan)
	}
}

func TestRuntimeBalancePlanScopesOnlyRequestedPods(t *testing.T) {
	service, _, _, _ := runtimeBalanceFixture(t, []int{4, 0, 0})
	var req RuntimeBalanceRequest
	if err := json.Unmarshal([]byte(`{"runtime_type":"deepseek-harness","pod_ids":[2,1]}`), &req); err != nil {
		t.Fatal(err)
	}
	plan, err := service.BalancePlan(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Groups) != 1 || !reflect.DeepEqual(plan.Groups[0].PodIDs, []int64{1, 2}) || len(plan.Moves) != 2 {
		t.Fatalf("Pod scope was ignored: %+v", plan)
	}
	for _, move := range plan.Moves {
		if move.SourcePodID > 2 || move.TargetPodID > 2 {
			t.Fatal("move escaped requested Pod scope")
		}
	}
}

func TestRuntimeBalanceScopeConfirmationAndValidation(t *testing.T) {
	service, pods, deployments, agent := runtimeBalanceFixture(t, []int{4, 0})
	ctx := context.Background()
	req := RuntimeBalanceRequest{RuntimeType: "deepseek-harness", PodIDs: []int64{2, 1}}
	plan, err := service.BalancePlan(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.PodIDs, []int64{1, 2}) {
		t.Fatal("scope not normalized in response")
	}
	req.PodIDs = []int64{1, 2}
	reordered, err := service.BalancePlan(ctx, req)
	if err != nil || reordered.PlanFingerprint != plan.PlanFingerprint {
		t.Fatal("scope ordering changed confirmation")
	}
	unscoped, err := service.BalancePlan(ctx, RuntimeBalanceRequest{RuntimeType: req.RuntimeType})
	if err != nil || unscoped.PlanFingerprint == plan.PlanFingerprint {
		t.Fatal("scope itself was not fingerprinted")
	}
	if _, err := service.Balance(ctx, RuntimeBalanceRequest{RuntimeType: req.RuntimeType, PlanFingerprint: plan.PlanFingerprint}, nil); err == nil || !strings.Contains(err.Error(), "plan changed") {
		t.Fatalf("omitted execution scope accepted: %v", err)
	}
	for _, invalid := range []RuntimeBalanceRequest{
		{RuntimeType: req.RuntimeType, PodIDs: []int64{1, 1}},
		{RuntimeType: req.RuntimeType, PodIDs: []int64{0, 2}},
		{RuntimeType: req.RuntimeType, PodIDs: []int64{1, 99}},
		{RuntimeType: "hermes", PodIDs: []int64{1, 2}},
	} {
		if _, err := service.BalancePlan(ctx, invalid); err == nil {
			t.Fatalf("invalid scope accepted: %+v", invalid)
		}
	}
	pods.pods[2].PodUID = stringPtrOrNil("old-registered-uid")
	if _, err := service.BalancePlan(ctx, req); err == nil {
		t.Fatal("stale registered scope identity accepted")
	}
	pods.pods[2].PodUID = stringPtrOrNil("uid-2")
	stale := time.Now().Add(-time.Hour)
	pods.pods[2].LastSeenAt = &stale
	if _, err := service.BalancePlan(ctx, req); err == nil {
		t.Fatal("stale heartbeat in scope accepted")
	}
	now := time.Now()
	pods.pods[2].LastSeenAt = &now
	deployments.pods = deployments.pods[:1]
	if _, err := service.BalancePlan(ctx, req); err == nil {
		t.Fatal("removed Kubernetes Pod in scope silently discarded")
	}
	if len(agent.createRequests) > 0 || len(agent.deleteRequests) > 0 || len(pods.claims) > 0 {
		t.Fatal("scope validation mutated Runtime state")
	}
}

func TestRuntimeBalanceScopeIgnoresUnselectedPodsAndSeparatesTypes(t *testing.T) {
	service, pods, deployments, _ := runtimeBalanceFixture(t, []int{4, 0, 0})
	req := RuntimeBalanceRequest{RuntimeType: "deepseek-harness", PodIDs: []int64{1, 2}}
	first, err := service.BalancePlan(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	pods.pods[3].SchedulingPaused = true
	second, err := service.BalancePlan(context.Background(), req)
	if err != nil || first.PlanFingerprint != second.PlanFingerprint {
		t.Fatal("unselected Pod changed scoped confirmation")
	}
	pods.pods[2].RuntimeType = "opencode"
	deployments.pods[1].RuntimeType = "opencode"
	plan, err := service.BalancePlan(context.Background(), RuntimeBalanceRequest{RuntimeType: "all", PodIDs: []int64{1, 2}})
	if err != nil || len(plan.Groups) != 2 || len(plan.Moves) != 0 {
		t.Fatalf("cross-runtime scope not separated: %+v %v", plan, err)
	}
	for _, group := range plan.Groups {
		if len(group.PodIDs) != 1 {
			t.Fatal("different Runtime types grouped")
		}
	}
	one, err := service.BalancePlan(context.Background(), RuntimeBalanceRequest{RuntimeType: "deepseek-harness", PodIDs: []int64{1}})
	if err != nil || one.Allowed || len(one.Moves) != 0 || len(one.Groups) != 1 {
		t.Fatalf("single Pod scope not read-only no-op: %+v %v", one, err)
	}
}

func TestRuntimeManagementCrossDeploymentRequiresCompleteProof(t *testing.T) {
	source := elasticManagementPod(1)
	source.RuntimeType = "openclaw"
	source.OpenClawVersion = stringPtrOrNil("28")
	source.AgentProtocolVersion = stringPtrOrNil("v2")
	source.SessionStore = stringPtrOrNil("jsonl")
	source.CapabilitiesJSON = stringPtrOrNil(`["a","b"]`)
	target := source
	target.ID = 2
	target.DeploymentName = "managed-copy"
	target.CapabilitiesJSON = stringPtrOrNil(`["b","a"]`)
	if err := validateRuntimeManagementTarget(source, target); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*models.RuntimePod){
		func(p *models.RuntimePod) { p.OpenClawVersion = nil },
		func(p *models.RuntimePod) { p.AgentProtocolVersion = stringPtrOrNil("v1") },
		func(p *models.RuntimePod) { p.SessionStore = nil },
		func(p *models.RuntimePod) { p.CapabilitiesJSON = stringPtrOrNil(`["a"]`) },
		func(p *models.RuntimePod) { p.WorkspaceStorageIdentity = "another-pvc" },
	} {
		bad := target
		mutate(&bad)
		if validateRuntimeManagementTarget(source, bad) == nil {
			t.Fatal("incomplete cross-deployment proof accepted")
		}
	}
}

func TestRuntimeManagementNonOpenClawCopyUsesImmutableBinaryProof(t *testing.T) {
	source := elasticManagementPod(1)
	source.CapabilitiesJSON = stringPtrOrNil(`["container-slots-v1"]`)
	target := source
	target.ID = 2
	target.DeploymentName = "managed-copy"
	if err := validateRuntimeManagementTarget(source, target); err != nil {
		t.Fatal(err)
	}
	target.AgentProtocolVersion = stringPtrOrNil("new-protocol")
	if validateRuntimeManagementTarget(source, target) == nil {
		t.Fatal("advertised protocol mismatch accepted")
	}
}

func TestRuntimeEvictionPlacementProofComparison(t *testing.T) {
	pod := elasticManagementPod(1)
	pod.CapabilitiesJSON = stringPtrOrNil(`["b","a"]`)
	slot, err := runtimeManagementSourceSlot(pod, models.InstanceRuntimeBinding{GatewayPort: 20000})
	if err != nil {
		t.Fatal(err)
	}
	first := runtimeEvictionPlacement(pod, slot)
	pod.CapabilitiesJSON = stringPtrOrNil(`["a","b"]`)
	second := runtimeEvictionPlacement(pod, slot)
	if !runtimeEvictionPlacementMatches(first, second) {
		t.Fatal("capability ordering changed proof")
	}
	old := *first
	old.NativeVersion = ""
	old.AgentProtocol = ""
	old.SessionStore = ""
	old.Capabilities = nil
	if !runtimeEvictionPlacementMatches(first, &old) {
		t.Fatal("legacy same-deployment proof rejected")
	}
	changed := *first
	changed.StorageIdentity = "foreign-pvc"
	if runtimeEvictionPlacementMatches(first, &changed) {
		t.Fatal("changed source identity accepted")
	}
	changed = *first
	changed.Capabilities = []string{"a"}
	if runtimeEvictionPlacementMatches(first, &changed) {
		t.Fatal("changed advertised capabilities accepted")
	}
}
