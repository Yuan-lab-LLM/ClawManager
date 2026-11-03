package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/upper/db/v4"
)

type RuntimePodScaleInRequest struct {
	RuntimeType     string `json:"runtime_type"`
	PodCount        int    `json:"pod_count"`
	PlanFingerprint string `json:"plan_fingerprint,omitempty"`
}

func (r RuntimePodScaleInRequest) Validate() error {
	return (k8s.RuntimePodScaleRequest{RuntimeType: r.RuntimeType, PodCount: r.PodCount}).Validate()
}

type RuntimePodScaleInSource struct {
	ID            int64  `json:"id"`
	PodName       string `json:"pod_name"`
	PodUID        string `json:"pod_uid"`
	NodeName      string `json:"node_name"`
	InstanceCount int    `json:"instance_count"`
}
type RuntimePodScaleInPreflight struct {
	Allowed          bool                      `json:"allowed"`
	Blockers         []string                  `json:"blockers"`
	PlanFingerprint  string                    `json:"plan_fingerprint"`
	RuntimeType      string                    `json:"runtime_type"`
	Namespace        string                    `json:"namespace"`
	DeploymentName   string                    `json:"deployment_name"`
	DeploymentUID    string                    `json:"deployment_uid"`
	PreviousReplicas int32                     `json:"previous_replicas"`
	Replicas         int32                     `json:"replicas"`
	RemovedPods      int                       `json:"removed_pods"`
	SourcePods       []RuntimePodScaleInSource `json:"source_pods"`
	Items            []RuntimeSchedulingItem   `json:"items"`
}

// Durable proof owns the source admission fence and all Kubernetes cleanup.
// Phase is journaled before each external mutation; a lost response is observed,
// never replayed as an increment/decrement or as a second instance move.
type runtimePodScaleInJournal struct {
	RuntimePodScaleInRequest
	Plan         RuntimePodScaleInPreflight      `json:"plan"`
	Target       k8s.RuntimePodScaleTarget       `json:"target"`
	Snapshot     *k8s.RuntimePodScaleSnapshot    `json:"snapshot"`
	Protection   k8s.RuntimePodScaleInProtection `json:"protection"`
	Phase        string                          `json:"phase"`
	ErrorMessage string                          `json:"error_message,omitempty"`
}

func runtimeScaleInFreshPod(p models.RuntimePod, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = time.Minute
	}
	if p.ID <= 0 || p.PodUID == nil || *p.PodUID == "" || stringValue(p.KubernetesPodUID) != *p.PodUID || stringValue(p.RegisteredPodUID) != *p.PodUID || !p.KubernetesReady || p.AgentState != "ready" || p.Draining || p.SchedulingPaused || p.AgentEndpoint == nil || p.LastSeenAt == nil || time.Since(*p.LastSeenAt) > timeout*3 || p.LastSeenAt.After(time.Now().Add(time.Minute)) {
		return errors.New("all default Runtime Pods require fresh, ready Kubernetes and Agent identities and enabled scheduling")
	}
	return nil
}
func runtimeScaleInSelectSources(pods []models.RuntimePod, count int) ([]models.RuntimePod, error) {
	if count < 1 || count > 10 || count >= len(pods) {
		return nil, errors.New("scale-in must remove 1..10 Pods and retain at least one default Pod")
	}
	ordered := append([]models.RuntimePod{}, pods...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].UsedSlots != ordered[j].UsedSlots {
			return ordered[i].UsedSlots < ordered[j].UsedSlots
		}
		if ordered[i].ID != ordered[j].ID {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].PodName < ordered[j].PodName
	})
	return ordered[:count], nil
}
func (s *RuntimeManagementService) runtimeScaleInPlanItems(ctx context.Context, inventory, sources []models.RuntimePod) ([]RuntimeSchedulingItem, error) {
	selected := map[int64]bool{}
	for _, p := range sources {
		selected[p.ID] = true
	}
	sort.Slice(inventory, func(i, j int) bool { return inventory[i].ID < inventory[j].ID })
	inventories := map[string][]models.RuntimePod{}
	for _, p := range inventory {
		inventories[p.RuntimeType] = inventory
	}
	// Reuse the existing capacity matcher so flexible instances can be reassigned
	// when another source requires their only physically compatible target slot.
	graph := &runtimeBalanceFlow{}
	start, end := graph.node(), graph.node()
	targetNodes := map[int64]int{}
	type slotKey struct {
		pod  int64
		slot int
	}
	slotNodes := map[slotKey]int{}
	type choice struct {
		item       RuntimeSchedulingItem
		from, edge int
	}
	choices := []choice{}
	demand := 0
	for _, source := range sources {
		bindings, err := s.scheduler.bindingRepo.ListByRuntimePodID(ctx, source.ID)
		if err != nil {
			return nil, err
		}
		if source.UsedSlots != len(bindings) {
			return nil, fmt.Errorf("source Pod %s has unsettled slot claims; retry after starts settle", source.PodName)
		}
		sort.Slice(bindings, func(i, j int) bool { return bindings[i].InstanceID < bindings[j].InstanceID })
		for _, binding := range bindings {
			demand++
			if demand > 100 {
				return nil, errors.New("scale-in supports at most 100 instance moves per operation")
			}
			required, err := runtimeManagementSourceSlot(source, binding)
			if err != nil {
				return nil, err
			}
			instanceNode := graph.node()
			graph.edge(start, instanceNode, 1)
			for _, target := range inventory {
				if selected[target.ID] || validateRuntimeManagementTarget(source, target) != nil {
					continue
				}
				existing, err := s.scheduler.bindingRepo.ListByRuntimePodID(ctx, target.ID)
				if err != nil {
					return nil, err
				}
				slots, err := runtimeManagementFreeSlots(target, existing, s.scheduler.gatewayPortStart, s.scheduler.gatewayPortEnd)
				if err != nil {
					continue
				}
				for _, slot := range slots {
					if !runtimeManagementSlotReady(target, slot) || !runtimeManagementSlotCanFit(target, slot, required, s.scheduler.gatewayPortStart) {
						continue
					}
					plan, err := s.preflightWithInventory(ctx, RuntimeSchedulingRequest{InstanceIDs: []int{binding.InstanceID}, TargetPodID: target.ID, TargetSlot: slot}, false, inventories)
					if err != nil {
						return nil, err
					}
					if !plan.Allowed {
						continue
					}
					targetNode, ok := targetNodes[target.ID]
					if !ok {
						targetNode = graph.node()
						targetNodes[target.ID] = targetNode
						graph.edge(targetNode, end, target.Capacity-target.UsedSlots)
					}
					key := slotKey{target.ID, slot}
					slotNode, ok := slotNodes[key]
					if !ok {
						slotNode = graph.node()
						slotNodes[key] = slotNode
						graph.edge(slotNode, targetNode, 1)
					}
					choices = append(choices, choice{plan.Items[0], instanceNode, graph.edge(instanceNode, slotNode, 1)})
				}
			}
		}
	}
	if graph.maximum(start, end, demand) != demand {
		return nil, errors.New("insufficient ready compatible destination capacity; scale-in blocked")
	}
	items := []RuntimeSchedulingItem{}
	for _, choice := range choices {
		if graph.edges[choice.from][choice.edge].capacity == 0 {
			items = append(items, choice.item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].InstanceID < items[j].InstanceID })
	return items, nil
}
func (s *RuntimeManagementService) scaleInKube() (k8s.RuntimePodScaleInDeploymentService, error) {
	if s == nil || s.sess == nil || s.scheduler == nil || s.scheduler.deployments == nil {
		return nil, errors.New("persistent Runtime management and Kubernetes inventory required")
	}
	kube, ok := s.scheduler.deployments.(k8s.RuntimePodScaleInDeploymentService)
	if !ok {
		return nil, errors.New("safe Kubernetes Runtime scale-in is unavailable")
	}
	if !requiresConfirmedGatewayStop(s.scheduler.agentClient) {
		return nil, errors.New(RuntimeForceEvictionUnavailable)
	}
	return kube, nil
}
func (s *RuntimeManagementService) scaleInPlan(ctx context.Context, req RuntimePodScaleInRequest) (*runtimePodScaleInJournal, error) {
	plan := RuntimePodScaleInPreflight{RuntimeType: req.RuntimeType, RemovedPods: req.PodCount, Allowed: true, Blockers: []string{}, SourcePods: []RuntimePodScaleInSource{}, Items: []RuntimeSchedulingItem{}}
	journal := &runtimePodScaleInJournal{RuntimePodScaleInRequest: req, Plan: plan, Phase: "pending"}
	block := func(err error) (*runtimePodScaleInJournal, error) {
		journal.Plan.Allowed = false
		journal.Plan.Blockers = append(journal.Plan.Blockers, err.Error())
		return journal, nil
	}
	if err := req.Validate(); err != nil {
		return block(err)
	}
	kube, err := s.scaleInKube()
	if err != nil {
		return block(err)
	}
	maintenance := &RuntimePodMaintenanceService{sess: s.sess, scheduler: s.scheduler}
	target, snapshot, err := maintenance.defaultScaleSnapshot(ctx, s.sess, kube, req.RuntimeType)
	if err != nil {
		return block(err)
	}
	journal.Target, journal.Snapshot = target, snapshot
	journal.Plan.Namespace = target.Namespace
	journal.Plan.DeploymentName = target.DeploymentName
	journal.Plan.DeploymentUID = snapshot.Option.DeploymentUID
	journal.Plan.PreviousReplicas = snapshot.Option.Replicas
	journal.Plan.Replicas = snapshot.Option.Replicas - int32(req.PodCount)
	if journal.Plan.Replicas < 1 {
		return block(errors.New("scale-in must retain at least one default Runtime Pod"))
	}
	if err = s.scaleInAdmission(ctx, s.sess); err != nil {
		return block(err)
	}
	native, err := kube.InspectRuntimeScaleInPods(ctx, target, snapshot)
	if err != nil {
		return block(err)
	}
	inventory, err := s.scheduler.RuntimeDeploymentPods(ctx, req.RuntimeType)
	if err != nil {
		return nil, err
	}
	defaults := []models.RuntimePod{}
	for _, id := range native {
		var found *models.RuntimePod
		for i := range inventory {
			p := &inventory[i]
			if p.Namespace == target.Namespace && p.DeploymentName == target.DeploymentName && p.PodName == id.PodName && stringValue(p.PodUID) == id.PodUID {
				found = p
				break
			}
		}
		if found == nil {
			return block(fmt.Errorf("default Runtime Pod %s has no verified registered identity", id.PodName))
		}
		if err = runtimeScaleInFreshPod(*found, s.scheduler.heartbeatTimeout); err != nil {
			return block(err)
		}
		defaults = append(defaults, *found)
	}
	sources, err := runtimeScaleInSelectSources(defaults, req.PodCount)
	if err != nil {
		return block(err)
	}
	items, err := s.runtimeScaleInPlanItems(ctx, inventory, sources)
	if err != nil {
		return block(err)
	}
	journal.Plan.Items = items
	selected := map[string]bool{}
	for _, p := range sources {
		selected[*p.PodUID] = true
		journal.Plan.SourcePods = append(journal.Plan.SourcePods, RuntimePodScaleInSource{ID: p.ID, PodName: p.PodName, PodUID: *p.PodUID, NodeName: stringValue(p.NodeName), InstanceCount: p.UsedSlots})
	}
	proof := k8s.RuntimePodScaleInProtection{Namespace: target.Namespace, DeploymentName: target.DeploymentName, DeploymentUID: snapshot.Option.DeploymentUID, PreviousReplicas: snapshot.Option.Replicas, Replicas: journal.Plan.Replicas}
	for _, p := range native {
		if selected[p.PodUID] {
			proof.SourcePods = append(proof.SourcePods, p)
		} else {
			proof.RetainedPods = append(proof.RetainedPods, p)
		}
	}
	sort.Slice(proof.SourcePods, func(i, j int) bool { return proof.SourcePods[i].PodName < proof.SourcePods[j].PodName })
	sort.Slice(proof.RetainedPods, func(i, j int) bool { return proof.RetainedPods[i].PodName < proof.RetainedPods[j].PodName })
	journal.Protection = proof
	// Exclude resourceVersion/heartbeat timestamps: controller status refreshes do
	// not invalidate a plan; immutable identities/template/bindings/slots do.
	evidence, err := s.runtimeScaleInEvidence(ctx, inventory)
	if err != nil {
		return nil, err
	}
	data, _ := json.Marshal(struct {
		Plan       RuntimePodScaleInPreflight
		Template   any
		Protection k8s.RuntimePodScaleInProtection
		Evidence   []any
	}{journal.Plan, snapshot.Template, proof, evidence})
	sum := sha256.Sum256(data)
	journal.Plan.PlanFingerprint = hex.EncodeToString(sum[:])
	return journal, nil
}
func (s *RuntimeManagementService) ScaleInPreflight(ctx context.Context, req RuntimePodScaleInRequest) (*RuntimePodScaleInPreflight, error) {
	journal, err := s.scaleInPlan(ctx, req)
	if err != nil {
		return nil, err
	}
	return &journal.Plan, nil
}
func (s *RuntimeManagementService) scaleInAdmission(ctx context.Context, tx db.Session) error {
	for _, query := range []string{"SELECT COUNT(*) FROM runtime_rollouts WHERE status IN ('pending','running')", "SELECT COUNT(*) FROM runtime_management_operations WHERE status IN ('pending','processing','starting','needs_attention')", "SELECT COUNT(*) FROM runtime_upgrade_items ui JOIN runtime_rollouts r ON r.id=ui.rollout_id WHERE ui.recovery_status IN ('pending','starting','retrying','needs_attention') AND ui.state NOT IN ('verified','gateway_verified') AND NOT EXISTS (SELECT 1 FROM runtime_upgrade_items newer JOIN runtime_rollouts nr ON nr.id=newer.rollout_id WHERE newer.instance_id=ui.instance_id AND nr.id>r.id AND nr.status IN ('pending','running','finished','error'))"} {
		var n int
		row, err := tx.SQL().QueryRowContext(ctx, query)
		if err == nil {
			err = row.Scan(&n)
		}
		if err != nil {
			return err
		}
		if n > 0 {
			return errors.New("Runtime upgrade, recovery or scheduling operation is active")
		}
	}
	return nil
}
func (s *RuntimeManagementService) ScaleInSubmit(ctx context.Context, req RuntimePodScaleInRequest, actor *int) (*RuntimeManagementOperation, error) {
	journal, err := s.scaleInPlan(ctx, req)
	if err != nil {
		return nil, err
	}
	if !journal.Plan.Allowed {
		return nil, errors.New(strings.Join(journal.Plan.Blockers, "; "))
	}
	if req.PlanFingerprint == "" || req.PlanFingerprint != journal.Plan.PlanFingerprint {
		return nil, errors.New("scale-in plan changed; run preflight again before confirming")
	}
	if s.guards == nil {
		return nil, errors.New("instance mutation reservation unavailable")
	}
	ids := []int{}
	for _, item := range journal.Plan.Items {
		ids = append(ids, item.InstanceID)
	}
	release, err := s.guards.ReserveInstanceMutations(ids)
	if err != nil {
		return nil, err
	}
	defer release()
	now := time.Now().UTC()
	op := &RuntimeManagementOperation{ID: "runtime_mgmt_" + uuid.NewString(), Kind: "scale_in", Status: "pending", Items: append([]RuntimeSchedulingItem{}, journal.Plan.Items...), CreatedAt: now, UpdatedAt: now}
	journal.Protection.OperationID = op.ID
	for i := range op.Items {
		op.Items[i].State = "pending"
	}
	requestJSON, _ := json.Marshal(journal)
	itemsJSON, _ := json.Marshal(op.Items)
	err = s.sess.TxContext(ctx, func(tx db.Session) error {
		if err := repository.LockRuntimeManagementAdmission(ctx, tx); err != nil {
			return err
		}
		if err := s.scaleInAdmission(ctx, tx); err != nil {
			return err
		}
		// Publish the source fence atomically with the pause after checking every
		// original binding. Preclaimed publication then sees the same durable fence.
		for _, source := range journal.Plan.SourcePods {
			var uid string
			var used int
			var paused bool
			row, err := tx.SQL().QueryRowContext(ctx, "SELECT pod_uid,used_slots,scheduling_paused FROM runtime_pods WHERE id=? FOR UPDATE", source.ID)
			if err == nil {
				err = row.Scan(&uid, &used, &paused)
			}
			if err != nil {
				return err
			}
			if uid != source.PodUID || paused || used != source.InstanceCount {
				return errors.New("source occupancy or identity changed; refresh preflight")
			}
			bindings, err := repository.NewInstanceRuntimeBindingRepository(tx).ListByRuntimePodID(ctx, source.ID)
			if err != nil {
				return err
			}
			if len(bindings) != source.InstanceCount {
				return errors.New("source has unsettled admission claims; retry after starts settle")
			}
			for _, b := range bindings {
				found := false
				for _, item := range journal.Plan.Items {
					if item.InstanceID == b.InstanceID && item.SourcePodID == b.RuntimePodID && item.SourceGeneration == b.Generation && item.SourceGatewayID == b.GatewayID {
						found = true
						break
					}
				}
				if !found {
					return errors.New("source binding changed; refresh preflight")
				}
			}
		}
		if _, err := tx.SQL().ExecContext(ctx, `INSERT INTO runtime_management_operations(id,kind,status,actor_user_id,request_json,items_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, op.ID, op.Kind, op.Status, actor, string(requestJSON), string(itemsJSON), now, now); err != nil {
			return err
		}
		for _, source := range journal.Plan.SourcePods {
			if _, err := tx.SQL().ExecContext(ctx, "UPDATE runtime_pods SET scheduling_paused=1,updated_at=? WHERE id=? AND pod_uid=?", now, source.ID, source.PodUID); err != nil {
				return err
			}
		}
		for _, id := range ids {
			if _, err := tx.SQL().ExecContext(ctx, "UPDATE instance_lifecycle_manual_guards SET token=? WHERE instance_id=?", op.ID, id); err != nil {
				return err
			}
		}
		return nil
	}, nil)
	if err != nil {
		return nil, err
	}
	return op, nil
}

func (s *RuntimeManagementService) runtimeScaleInEvidence(ctx context.Context, inventory []models.RuntimePod) ([]any, error) {
	ordered := append([]models.RuntimePod{}, inventory...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	evidence := []any{}
	for _, pod := range ordered {
		bindings, err := s.scheduler.bindingRepo.ListByRuntimePodID(ctx, pod.ID)
		if err != nil {
			return nil, err
		}
		sort.Slice(bindings, func(i, j int) bool { return bindings[i].InstanceID < bindings[j].InstanceID })
		for i := range bindings {
			bindings[i].LastHealthAt = nil
			bindings[i].UpdatedAt = time.Time{}
			bindings[i].CreatedAt = time.Time{}
			bindings[i].GatewayPID = nil
		}
		slots := append([]models.RuntimeSlot{}, pod.Slots...)
		requests := [][2]*int64{}
		for i := range slots {
			requests = append(requests, [2]*int64{slots[i].CPUMillisRequest, slots[i].MemoryBytesRequest})
			slots[i].CPUMillisUsed = nil
			slots[i].MemoryBytesUsed = nil
			slots[i].RestartCount = 0
		}
		pod.Slots = slots
		pod.LastSeenAt = nil
		pod.CreatedAt = time.Time{}
		pod.UpdatedAt = time.Time{}
		pod.CPUMillisUsed = 0
		pod.MemoryBytesUsed = 0
		pod.DiskBytesUsed = 0
		pod.NetworkRXBytes = 0
		pod.NetworkTXBytes = 0
		pod.MetricsJSON = nil
		capabilities := pod.Capabilities()
		sort.Strings(capabilities)
		evidence = append(evidence, struct {
			Pod                                                          models.RuntimePod
			Bindings                                                     []models.InstanceRuntimeBinding
			Storage                                                      string
			CPURequest, MemoryRequest, CoordinatorCPU, CoordinatorMemory *int64
			Capabilities                                                 []string
			SlotRequests                                                 [][2]*int64
			KubernetesUID, RegisteredUID, AgentState                     string
			KubernetesReady                                              bool
		}{pod, bindings, pod.WorkspaceStorageIdentity, pod.CPUMillisRequest, pod.MemoryBytesRequest, pod.CoordinatorCPUMillisRequest, pod.CoordinatorMemoryBytesRequest, capabilities, requests, stringValue(pod.KubernetesPodUID), stringValue(pod.RegisteredPodUID), pod.AgentState, pod.KubernetesReady})
	}
	return evidence, nil
}
