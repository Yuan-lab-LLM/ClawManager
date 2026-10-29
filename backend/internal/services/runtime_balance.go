package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"clawreef/internal/models"
)

type RuntimeBalanceRequest struct {
	RuntimeType     string  `json:"runtime_type,omitempty"`
	PodIDs          []int64 `json:"pod_ids,omitempty"`
	PlanFingerprint string  `json:"plan_fingerprint,omitempty"`
}
type RuntimeBalanceMove struct {
	RuntimeSchedulingItem
	InstanceName string `json:"instance_name"`
	RuntimeType  string `json:"runtime_type"`
}
type RuntimeBalanceCount struct {
	PodID    int64 `json:"pod_id"`
	Count    int   `json:"count"`
	Capacity int   `json:"capacity"`
}
type RuntimeBalanceGroup struct {
	RuntimeType string                `json:"runtime_type"`
	PodIDs      []int64               `json:"pod_ids"`
	Before      []RuntimeBalanceCount `json:"before"`
	After       []RuntimeBalanceCount `json:"after"`
	Balanced    bool                  `json:"balanced"`
}
type RuntimeBalanceBlocker struct {
	InstanceID  int    `json:"instance_id,omitempty"`
	PodID       int64  `json:"pod_id,omitempty"`
	RuntimeType string `json:"runtime_type"`
	Reason      string `json:"reason"`
}
type RuntimeBalancePlan struct {
	RuntimeType     string                  `json:"runtime_type"`
	PodIDs          []int64                 `json:"pod_ids,omitempty"`
	PlanFingerprint string                  `json:"plan_fingerprint"`
	Allowed         bool                    `json:"allowed"`
	Moves           []RuntimeBalanceMove    `json:"moves"`
	Groups          []RuntimeBalanceGroup   `json:"groups"`
	Blocked         []RuntimeBalanceBlocker `json:"blocked"`
}

// Water filling uses instance counts, never CPU weights. Keeping an existing
// occupant wins ties, which minimizes the number of moves between equal optima.
func runtimeBalanceCounts(before, capacity []int, fixed ...[]int) []int {
	result := make([]int, len(before))
	if len(fixed) > 0 {
		copy(result, fixed[0])
	}
	total := 0
	for i, n := range before {
		total += n - result[i]
	}
	for ; total > 0; total-- {
		selected := -1
		for i := range result {
			if result[i] >= capacity[i] {
				continue
			}
			if selected < 0 || result[i] < result[selected] || (result[i] == result[selected] && before[i] > result[i] && before[selected] <= result[selected]) {
				selected = i
			}
		}
		if selected < 0 {
			break
		}
		result[selected]++
	}
	return result
}

func (s *RuntimeManagementService) BalancePlan(ctx context.Context, req RuntimeBalanceRequest) (*RuntimeBalancePlan, error) {
	scope := append([]int64{}, req.PodIDs...)
	sort.Slice(scope, func(i, j int) bool { return scope[i] < scope[j] })
	scoped, found := map[int64]bool{}, map[int64]bool{}
	for _, id := range scope {
		if id <= 0 || scoped[id] {
			return nil, errors.New("Pod scope must contain unique positive registered Pod IDs")
		}
		scoped[id] = true
	}
	typ := strings.TrimSpace(req.RuntimeType)
	if typ == "" {
		typ = "all"
	}
	types := []string{RuntimeTypeOpenClaw, RuntimeTypeHermes, RuntimeTypeOpenCode, RuntimeTypeDeepSeekHarness}
	if typ != "all" {
		found := false
		for _, t := range types {
			if t == typ {
				found = true
			}
		}
		if !found {
			return nil, errors.New("unsupported Runtime type")
		}
		types = []string{typ}
	}
	plan := &RuntimeBalancePlan{RuntimeType: typ, PodIDs: scope, Allowed: true, Moves: []RuntimeBalanceMove{}, Groups: []RuntimeBalanceGroup{}, Blocked: []RuntimeBalanceBlocker{}}
	if s.scheduler == nil || s.scheduler.deployments == nil {
		return nil, errors.New("Runtime scheduler and Kubernetes inventory are required")
	}
	inventories := map[string][]models.RuntimePod{}
	allBindings := map[int64][]models.InstanceRuntimeBinding{}
	instances := map[int]*models.Instance{}
	// The fingerprint deliberately excludes volatile metrics and heartbeat times.
	// Include every admission/physical identity field, even for unmoved Pods.
	evidence := []any{}
	for _, t := range types {
		inventory, err := s.scheduler.RuntimeDeploymentPods(ctx, t)
		if err != nil {
			return nil, err
		}
		sort.Slice(inventory, func(i, j int) bool { return inventory[i].ID < inventory[j].ID })
		inventories[t] = inventory
		if len(scoped) > 0 {
			selected := []models.RuntimePod{}
			for _, pod := range inventory {
				if !scoped[pod.ID] {
					continue
				}
				uid := stringValue(pod.PodUID)
				if uid == "" || stringValue(pod.KubernetesPodUID) != uid || stringValue(pod.RegisteredPodUID) != uid || pod.LastSeenAt == nil || time.Since(*pod.LastSeenAt) > s.scheduler.heartbeatTimeout*3 {
					return nil, fmt.Errorf("scoped Pod %d registration or Kubernetes identity is stale", pod.ID)
				}
				found[pod.ID] = true
				selected = append(selected, pod)
			}
			inventory = selected
		}
		groups := [][]models.RuntimePod{}
		for _, pod := range inventory {
			bindings, err := s.scheduler.bindingRepo.ListByRuntimePodID(ctx, pod.ID)
			if err != nil {
				return nil, err
			}
			sort.Slice(bindings, func(i, j int) bool { return bindings[i].InstanceID < bindings[j].InstanceID })
			allBindings[pod.ID] = bindings
			bindingEvidence := append([]models.InstanceRuntimeBinding{}, bindings...)
			for i := range bindingEvidence {
				bindingEvidence[i].LastHealthAt = nil
				bindingEvidence[i].UpdatedAt = time.Time{}
				bindingEvidence[i].CreatedAt = time.Time{}
				bindingEvidence[i].GatewayPID = nil
			}
			requests := make([][2]*int64, 0, len(pod.Slots))
			for _, slot := range pod.Slots {
				requests = append(requests, [2]*int64{slot.CPUMillisRequest, slot.MemoryBytesRequest})
			}
			evidence = append(evidence, requests)
			slots := append([]models.RuntimeSlot{}, pod.Slots...)
			for i := range slots {
				slots[i].CPUMillisUsed = nil
				slots[i].MemoryBytesUsed = nil
				slots[i].RestartCount = 0
			}
			capabilities := pod.Capabilities()
			sort.Strings(capabilities)
			evidence = append(evidence, struct {
				Pod                                                          models.RuntimePod
				Bindings                                                     []models.InstanceRuntimeBinding
				Storage                                                      string
				CPURequest, MemoryRequest, CoordinatorCPU, CoordinatorMemory *int64
				Capabilities                                                 []string
			}{
				Pod: models.RuntimePod{ID: pod.ID, RuntimeType: pod.RuntimeType, Namespace: pod.Namespace, DeploymentName: pod.DeploymentName, PodUID: pod.PodUID, AgentEndpoint: pod.AgentEndpoint, ImageRef: pod.ImageRef, ImageDigest: pod.ImageDigest, OpenClawVersion: pod.OpenClawVersion, AgentProtocolVersion: pod.AgentProtocolVersion, SessionStore: pod.SessionStore, State: pod.State, Capacity: pod.Capacity, UsedSlots: pod.UsedSlots, Draining: pod.Draining, SchedulingPaused: pod.SchedulingPaused, SchedulingEnabled: pod.SchedulingEnabled, KubernetesReady: pod.KubernetesReady, Slots: slots, CPUMillisLimit: pod.CPUMillisLimit, MemoryBytesLimit: pod.MemoryBytesLimit}, Bindings: bindingEvidence, Storage: pod.WorkspaceStorageIdentity, CPURequest: pod.CPUMillisRequest, MemoryRequest: pod.MemoryBytesRequest, CoordinatorCPU: pod.CoordinatorCPUMillisRequest, CoordinatorMemory: pod.CoordinatorMemoryBytesRequest, Capabilities: capabilities,
			})
			added := false
			for i := range groups {
				if runtimeManagementCompatibleImage(groups[i][0], pod) == nil {
					groups[i] = append(groups[i], pod)
					added = true
					break
				}
			}
			if !added {
				groups = append(groups, []models.RuntimePod{pod})
			}
		}
		if len(groups) > 1 {
			plan.Blocked = append(plan.Blocked, RuntimeBalanceBlocker{RuntimeType: t, Reason: fmt.Sprintf("Runtime type has %d incompatible image, storage or protocol groups; counts can be balanced only within each group", len(groups))})
		}
		for _, pods := range groups {
			group := RuntimeBalanceGroup{RuntimeType: t, PodIDs: []int64{}, Before: []RuntimeBalanceCount{}, After: []RuntimeBalanceCount{}, Balanced: true}
			before, capacity := make([]int, len(pods)), make([]int, len(pods))
			counts := make([]int, len(pods))
			eligible := make([]bool, len(pods))
			fixed := make([]int, len(pods))
			for i, pod := range pods {
				count := len(allBindings[pod.ID])
				if pod.UsedSlots > count {
					count = pod.UsedSlots
				}
				before[i], counts[i], capacity[i] = count, count, pod.Capacity
				fixed[i] = count - len(allBindings[pod.ID])
				if fixed[i] > 0 {
					group.Balanced = false
					plan.Blocked = append(plan.Blocked, RuntimeBalanceBlocker{PodID: pod.ID, RuntimeType: t, Reason: "outstanding slot claims are fixed occupancy until their lifecycle settles"})
				}
				for _, binding := range allBindings[pod.ID] {
					instance, e := s.scheduler.instanceRepo.GetByID(binding.InstanceID)
					if e != nil {
						return nil, e
					}
					instances[binding.InstanceID] = instance
					if instance == nil || instance.Status != "running" || binding.State != "running" {
						fixed[i]++
						group.Balanced = false
						plan.Blocked = append(plan.Blocked, RuntimeBalanceBlocker{InstanceID: binding.InstanceID, PodID: pod.ID, RuntimeType: t, Reason: "only running instances with a running binding can be balanced"})
					}
				}
				group.PodIDs = append(group.PodIDs, pod.ID)
				group.Before = append(group.Before, RuntimeBalanceCount{pod.ID, count, pod.Capacity})
				eligible[i] = pod.ID > 0 && runtimePodEligibleForOrdinaryScheduling(pod) && !pod.Draining && !pod.SchedulingPaused && pod.AgentState == "ready" && pod.KubernetesReady && pod.AgentEndpoint != nil && pod.PodUID != nil && stringValue(pod.KubernetesPodUID) == *pod.PodUID && stringValue(pod.RegisteredPodUID) == *pod.PodUID && pod.LastSeenAt != nil && time.Since(*pod.LastSeenAt) <= s.scheduler.heartbeatTimeout*3 && runtimeManagementResourceBudget(pod) == nil
				if !eligible[i] {
					capacity[i] = count
					fixed[i] = count
					plan.Blocked = append(plan.Blocked, RuntimeBalanceBlocker{PodID: pod.ID, RuntimeType: t, Reason: "Pod is unavailable for balancing: scheduling, Agent readiness, pool policy or physical resource evidence is unavailable"})
					group.Balanced = false
				}
			}
			goal := runtimeBalanceCounts(before, capacity, fixed)
			lower, upper := runtimeBalanceCountBounds(goal, capacity, fixed)
			admission := make([]int, len(pods))
			for i, pod := range pods {
				if eligible[i] {
					admission[i] = pod.Capacity - counts[i]
				}
			}
			moved := map[int]bool{}
			rejected := map[int]string{}
			candidates := []runtimeBalanceCandidate{}
			minimum := 0
			for i, count := range before {
				if count > goal[i] {
					minimum += count - goal[i]
				}
			}
			if minimum > 0 {
				for sourceIndex, source := range pods {
					if !eligible[sourceIndex] || before[sourceIndex] <= lower[sourceIndex] {
						continue
					}
					for _, binding := range allBindings[source.ID] {
						instance := instances[binding.InstanceID]
						if instance == nil || instance.Status != "running" || binding.State != "running" {
							continue
						}
						required, err := runtimeManagementSourceSlot(source, binding)
						if err != nil {
							rejected[binding.InstanceID] = err.Error()
							continue
						}
						for targetIndex, target := range pods {
							if !eligible[targetIndex] || before[targetIndex] >= upper[targetIndex] || admission[targetIndex] <= 0 {
								continue
							}
							slots, err := runtimeManagementFreeSlots(target, allBindings[target.ID], s.scheduler.gatewayPortStart, s.scheduler.gatewayPortEnd)
							if err != nil {
								rejected[binding.InstanceID] = err.Error()
								continue
							}
							for _, slot := range slots {
								if !runtimeManagementSlotReady(target, slot) {
									rejected[binding.InstanceID] = fmt.Sprintf("target Pod #%d (%s) Slot %d is not ready", target.ID, target.PodName, slot)
									continue
								}
								if err := runtimeManagementSlotFitError(target, slot, required, s.scheduler.gatewayPortStart); err != nil {
									rejected[binding.InstanceID] = err.Error()
									continue
								}
								preflight, err := s.preflightWithInventory(ctx, RuntimeSchedulingRequest{InstanceIDs: []int{binding.InstanceID}, TargetPodID: target.ID, TargetSlot: slot}, false, inventories)
								if err != nil {
									return nil, err
								}
								if !preflight.Allowed {
									rejected[binding.InstanceID] = strings.Join(preflight.Blockers, "; ")
									continue
								}
								item := preflight.Items[0]
								if item.SourcePodID != source.ID || item.SourceGeneration != binding.Generation || item.SourceGatewayID != binding.GatewayID {
									rejected[binding.InstanceID] = "source binding changed during planning; refresh the plan"
									continue
								}
								candidates = append(candidates, runtimeBalanceCandidate{RuntimeBalanceMove{RuntimeSchedulingItem: item, InstanceName: instance.Name, RuntimeType: t}, sourceIndex, targetIndex})
							}
						}
					}
				}
			}
			// Static requests reserve the entire configured slot inventory up front.
			// Matching debits each physical slot once and also bounds virtual claims
			// by capacity minus all bindings and outstanding admission claims.
			for _, index := range runtimeBalanceMatch(candidates, before, goal, lower, upper, admission) {
				candidate := candidates[index]
				plan.Moves = append(plan.Moves, candidate.move)
				moved[candidate.move.InstanceID] = true
				counts[candidate.source]--
				counts[candidate.target]++
			}
			for i, pod := range pods {
				group.After = append(group.After, RuntimeBalanceCount{pod.ID, counts[i], pod.Capacity})
				if counts[i] < lower[i] || counts[i] > upper[i] {
					group.Balanced = false
				}
			}
			if !group.Balanced {
				ids := []int{}
				for id := range rejected {
					if !moved[id] {
						ids = append(ids, id)
					}
				}
				sort.Ints(ids)
				for _, id := range ids {
					plan.Blocked = append(plan.Blocked, RuntimeBalanceBlocker{InstanceID: id, RuntimeType: t, Reason: rejected[id]})
				}
				plan.Blocked = append(plan.Blocked, RuntimeBalanceBlocker{RuntimeType: t, Reason: fmt.Sprintf("compatible Pods %v cannot fully balance within the current direct-move resource constraints and target count intervals", group.PodIDs)})
			}
			plan.Groups = append(plan.Groups, group)
		}
	}
	if len(found) != len(scoped) {
		return nil, errors.New("scoped Pod IDs are unknown, stale or outside the requested Runtime type")
	}
	plan.Allowed = len(plan.Moves) > 0
	raw, err := json.Marshal(struct {
		Plan     *RuntimeBalancePlan
		Evidence []any
	}{plan, evidence})
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(raw)
	plan.PlanFingerprint = hex.EncodeToString(hash[:])
	return plan, nil
}

func (s *RuntimeManagementService) Balance(ctx context.Context, req RuntimeBalanceRequest, actor *int) (*RuntimeManagementOperation, error) {
	if strings.TrimSpace(req.PlanFingerprint) == "" {
		return nil, errors.New("a confirmed balance plan fingerprint is required")
	}
	plan, err := s.BalancePlan(ctx, req)
	if err != nil {
		return nil, err
	}
	if req.PlanFingerprint != plan.PlanFingerprint {
		return nil, errors.New("balance plan changed; preview and confirm the current plan")
	}
	if !plan.Allowed {
		return nil, errors.New("balance plan has no safe moves")
	}
	items := make([]RuntimeSchedulingItem, 0, len(plan.Moves))
	ids := make([]int, 0, len(plan.Moves))
	for _, move := range plan.Moves {
		items = append(items, move.RuntimeSchedulingItem)
		ids = append(ids, move.InstanceID)
	}
	return s.enqueue(ctx, "balance", RuntimeSchedulingRequest{InstanceIDs: ids, Concurrency: 1}, items, actor)
}
