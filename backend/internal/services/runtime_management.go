package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"github.com/google/uuid"
	"github.com/upper/db/v4"
	"k8s.io/apimachinery/pkg/api/resource"
)

const RuntimeForceEvictionUnavailable = "Runtime management requires the confirmed slot teardown lifecycle"

type RuntimeSchedulingRequest struct {
	SourcePodID        int64  `json:"source_pod_id,omitempty"`
	InstanceIDs        []int  `json:"instance_ids"`
	TargetPodID        int64  `json:"target_pod_id"`
	TargetSlot         int    `json:"target_slot,omitempty"`
	Mode               string `json:"mode,omitempty"`
	GracePeriodSeconds int    `json:"grace_period_seconds,omitempty"`
	Concurrency        int    `json:"concurrency,omitempty"`
}

type RuntimeSchedulingItem struct {
	SourcePlacement  *RuntimeEvictionPlacement `json:"source_placement,omitempty"`
	StopGraceSeconds int                       `json:"-"`
	InstanceID       int                       `json:"instance_id"`
	SourcePodID      int64                     `json:"source_pod_id"`
	TargetPodID      int64                     `json:"target_pod_id"`
	TargetSlot       int                       `json:"target_slot"`
	State            string                    `json:"state"`
	ErrorMessage     string                    `json:"error_message,omitempty"`
	SourceGeneration int                       `json:"source_generation"`
	SourceGatewayID  string                    `json:"source_gateway_id"`
	SourcePodUID     string                    `json:"source_pod_uid"`
	TargetPodUID     string                    `json:"target_pod_uid"`
}

type RuntimeSchedulingPreflight struct {
	Allowed  bool                    `json:"allowed"`
	Blockers []string                `json:"blockers"`
	Items    []RuntimeSchedulingItem `json:"items"`
}

type RuntimeManagementOperation struct {
	CancelRequested bool                    `json:"cancel_requested"`
	ErrorMessage    string                  `json:"error_message,omitempty"`
	Phase           string                  `json:"phase,omitempty"`
	ID              string                  `json:"id"`
	Kind            string                  `json:"kind"`
	Status          string                  `json:"status"`
	Items           []RuntimeSchedulingItem `json:"items"`
	CreatedAt       time.Time               `json:"created_at"`
	UpdatedAt       time.Time               `json:"updated_at"`
}

type RuntimeManagementService struct {
	sess      db.Session
	scheduler *RuntimeScheduler
	guards    *repository.NorthboundRepository
	token     string
}

func NewRuntimeManagementService(sess db.Session, scheduler *RuntimeScheduler, guards *repository.NorthboundRepository) *RuntimeManagementService {
	s := &RuntimeManagementService{sess: sess, scheduler: scheduler, guards: guards, token: uuid.NewString()}
	if scheduler != nil {
		scheduler.management = s
	}
	return s
}

func validateRuntimeManagementTarget(source, target models.RuntimePod) error {
	if err := runtimeManagementCompatibleImage(source, target); err != nil {
		return err
	}
	state := target.AgentState
	if state == "" {
		state = target.State
	}
	switch {
	case target.ID <= 0:
		return errors.New("target must be a registered Runtime Pod")
	case source.ID > 0 && target.ID == source.ID:
		return errors.New("source and target must be different Runtime Pods")
	case !runtimePodEligibleForOrdinaryScheduling(target):
		return errors.New("target pool is not enabled for ordinary scheduling")
	case state != "ready" || target.Draining || target.SchedulingPaused || (target.SchedulingEnabled != nil && !*target.SchedulingEnabled):
		return errors.New("target is not ready or scheduling is paused")
	case target.UsedSlots >= target.Capacity:
		return errors.New("target has no free capacity")
	}
	return nil
}

// Deployment names are ownership, not a workspace format. A different owner is
// safe only with complete immutable storage/image and advertised format proof.
func runtimeManagementCompatibleImage(source, target models.RuntimePod) error {
	switch {
	case target.RuntimeType != source.RuntimeType:
		return errors.New("target Runtime type differs from source")
	case strings.TrimSpace(stringValue(source.ImageDigest)) == "" || stringValue(source.ImageDigest) != stringValue(target.ImageDigest):
		return errors.New("source and target must have the same confirmed running image digest")
	case strings.TrimSpace(target.ImageRef) == "" || target.ImageRef != source.ImageRef:
		return errors.New("target must run the exact source image")
	case !runtimeWorkspaceStorageCompatible(source.WorkspaceStorageIdentity, target.WorkspaceStorageIdentity) || source.Namespace != target.Namespace:
		return errors.New("source and target workspace storage identity and namespace must match and be known")
	}
	if source.DeploymentName == target.DeploymentName {
		return nil
	}
	for _, pair := range [][2]*string{{source.OpenClawVersion, target.OpenClawVersion}, {source.AgentProtocolVersion, target.AgentProtocolVersion}, {source.SessionStore, target.SessionStore}} {
		if (source.RuntimeType == RuntimeTypeOpenClaw && strings.TrimSpace(stringValue(pair[0])) == "") || stringValue(pair[0]) != stringValue(pair[1]) {
			return errors.New("cross-deployment placement requires identical advertised native version, Agent protocol and session format")
		}
	}
	a, b := source.Capabilities(), target.Capabilities()
	if len(a) == 0 || len(b) == 0 {
		return errors.New("cross-deployment placement requires known identical capabilities")
	}
	sort.Strings(a)
	sort.Strings(b)
	if strings.Join(a, "\x00") != strings.Join(b, "\x00") {
		return errors.New("cross-deployment placement requires known identical capabilities")
	}
	if source.RuntimeType != RuntimeTypeOpenClaw {
		found := false
		for _, capability := range a {
			if capability == "container-slots-v1" {
				found = true
			}
		}
		if !found {
			return errors.New("cross-deployment placement requires the container slots lifecycle capability")
		}
	}
	return nil
}

func runtimeManagementFreeSlots(pod models.RuntimePod, bindings []models.InstanceRuntimeBinding, start, end int) ([]int, error) {
	block := RuntimeGatewayPortBlockSize(pod.RuntimeType)
	if block <= 0 || pod.Capacity <= 0 {
		return nil, errors.New("target slot capacity unavailable")
	}
	slots := []int{}
	for slot := 1; slot <= pod.Capacity; slot++ {
		port := start + (slot-1)*block
		if port+block-1 > end {
			break
		}
		if !runtimeGatewayPortBlockOccupied(bindings, port, block) {
			slots = append(slots, slot)
		}
	}
	return slots, nil
}

func (s *RuntimeManagementService) Scheduling(ctx context.Context, podID int64, enabled bool) error {
	if s.scheduler == nil {
		return errors.New("runtime scheduler unavailable")
	}
	pod, err := s.scheduler.podRepo.GetByID(ctx, podID)
	if err != nil {
		return err
	}
	if pod == nil {
		return errors.New("Runtime Pod not found")
	}
	setter, ok := s.scheduler.podRepo.(interface {
		SetSchedulingPaused(context.Context, int64, bool) error
	})
	if !ok {
		return errors.New("persistent scheduling control unavailable")
	}
	return setter.SetSchedulingPaused(ctx, podID, !enabled)
}

func (s *RuntimeManagementService) Preflight(ctx context.Context, req RuntimeSchedulingRequest) (*RuntimeSchedulingPreflight, error) {
	return s.preflight(ctx, req, false)
}

// sourceOnly is private: public migration requests always require a target.
func (s *RuntimeManagementService) preflight(ctx context.Context, req RuntimeSchedulingRequest, sourceOnly bool) (*RuntimeSchedulingPreflight, error) {
	return s.preflightWithInventory(ctx, req, sourceOnly, map[string][]models.RuntimePod{})
}

// Inventories belong to one planning call. Worker revalidation starts a new
// preflight and therefore always discovers current Kubernetes/Agent identities.
func (s *RuntimeManagementService) preflightWithInventory(ctx context.Context, req RuntimeSchedulingRequest, sourceOnly bool, inventories map[string][]models.RuntimePod) (*RuntimeSchedulingPreflight, error) {
	result := &RuntimeSchedulingPreflight{Allowed: true, Blockers: []string{}, Items: []RuntimeSchedulingItem{}}
	block := func(msg string) { result.Allowed = false; result.Blockers = append(result.Blockers, msg) }
	if s.scheduler == nil || s.scheduler.deployments == nil {
		block("Runtime scheduler and Kubernetes inventory are required")
		return result, nil
	}
	if !requiresConfirmedGatewayStop(s.scheduler.agentClient) {
		block("Runtime management requires the confirmed slot teardown lifecycle")
		return result, nil
	}
	if req.GracePeriodSeconds < 0 || req.GracePeriodSeconds > 300 || req.Concurrency < 0 || req.Concurrency > 5 {
		block("grace period must be at most 300 seconds; concurrency must be at most 5")
		return result, nil
	}
	if len(req.InstanceIDs) == 0 || len(req.InstanceIDs) > 100 {
		block("select between 1 and 100 instances")
		return result, nil
	}
	if req.TargetSlot < 0 || (req.TargetSlot > 0 && len(req.InstanceIDs) != 1) {
		block("an exact target slot requires one instance")
		return result, nil
	}
	if s.sess != nil {
		var active int
		row, err := s.sess.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM runtime_rollouts WHERE status IN ('pending','running')")
		if err != nil {
			return nil, err
		}
		if err = row.Scan(&active); err != nil {
			return nil, err
		}
		if active > 0 {
			block("Runtime rollout is active; scheduling is locked")
			return result, nil
		}
	}
	seen := map[int]bool{}
	allocated := map[int64]map[int]bool{}
	for _, id := range req.InstanceIDs {
		item := RuntimeSchedulingItem{InstanceID: id, TargetPodID: req.TargetPodID, State: "ready"}
		fail := func(err error) {
			item.State = "blocked"
			item.ErrorMessage = err.Error()
			block(fmt.Sprintf("instance %d: %s", id, err))
		}
		if id <= 0 || seen[id] {
			fail(errors.New("invalid or duplicate instance"))
			result.Items = append(result.Items, item)
			continue
		}
		seen[id] = true
		instance, err := s.scheduler.instanceRepo.GetByID(id)
		if err != nil {
			return nil, err
		}
		if instance == nil || instance.InstanceMode != "lite" || (instance.Status != "running" && instance.Status != "error") {
			fail(errors.New("instance must be a running or errored Lite instance"))
			result.Items = append(result.Items, item)
			continue
		}
		if s.sess != nil {
			var count int
			row, e := s.sess.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM team_members WHERE instance_id=? AND status NOT IN ('deleted','deleting')", id)
			if e != nil {
				return nil, e
			}
			if e = row.Scan(&count); e != nil {
				return nil, e
			}
			if count > 0 {
				fail(errors.New("Team members require coordinated maintenance and cannot be moved individually"))
				result.Items = append(result.Items, item)
				continue
			}
		}
		if s.scheduler.upgrade != nil {
			if err := s.scheduler.upgrade.validateInstanceMutation(ctx, s.sess, id); err != nil {
				if !errors.Is(err, ErrWorkspaceUpgradeBusy) {
					return nil, err
				}
				fail(err)
				result.Items = append(result.Items, item)
				continue
			}
		}
		binding, err := s.scheduler.bindingRepo.GetByInstanceID(ctx, id)
		if err != nil {
			return nil, err
		}
		if binding == nil || !runtimeManagementSourceState(binding.State) || binding.Generation != instance.RuntimeGeneration {
			fail(errors.New("source running binding is missing or stale"))
			result.Items = append(result.Items, item)
			continue
		}
		item.SourcePodID = binding.RuntimePodID
		item.SourceGeneration = binding.Generation
		item.SourceGatewayID = binding.GatewayID
		source, err := s.scheduler.podRepo.GetByID(ctx, binding.RuntimePodID)
		if err != nil {
			return nil, err
		}
		if source == nil || source.AgentEndpoint == nil || source.PodUID == nil {
			fail(errors.New("source Runtime identity unavailable"))
			result.Items = append(result.Items, item)
			continue
		}
		item.SourcePodUID = *source.PodUID
		inventory, discovered := inventories[source.RuntimeType]
		if !discovered {
			inventory, err = s.scheduler.RuntimeDeploymentPods(ctx, source.RuntimeType)
			if err != nil {
				return nil, err
			}
			inventories[source.RuntimeType] = inventory
		}
		var liveSource *models.RuntimePod
		for i := range inventory {
			if inventory[i].ID == source.ID {
				liveSource = &inventory[i]
				break
			}
		}
		if liveSource == nil || stringValue(liveSource.KubernetesPodUID) != *source.PodUID {
			fail(errors.New("source Kubernetes Pod identity is unavailable or changed"))
			result.Items = append(result.Items, item)
			continue
		}
		source = liveSource
		if sourceOnly {
			if strings.TrimSpace(source.ImageRef) == "" || strings.TrimSpace(stringValue(source.ImageDigest)) == "" || source.WorkspaceStorageIdentity == "" {
				fail(errors.New("source immutable image and workspace identity are required for pending scheduling"))
			}
			required, resourceErr := runtimeManagementSourceSlot(*source, *binding)
			if resourceErr != nil {
				fail(resourceErr)
			} else {
				item.SourcePlacement = runtimeEvictionPlacement(*source, required)
			}
			if req.SourcePodID != source.ID {
				fail(errors.New("source placement changed during eviction"))
			} else if _, e := s.scheduler.gatewayEnvironment(instance); e != nil {
				fail(e)
			}
			result.Items = append(result.Items, item)
			continue
		}
		var target *models.RuntimePod
		for i := range inventory {
			p := &inventory[i]
			if p.ID != req.TargetPodID {
				continue
			}
			target = p
			break
		}
		if target == nil {
			fail(errors.New("target is absent from Kubernetes inventory"))
			result.Items = append(result.Items, item)
			continue
		}
		if source.RuntimeType == RuntimeTypeOpenClaw && s.scheduler.upgrade != nil {
			allowed, e := s.scheduler.upgrade.filterUpgradePlacementWithInventory(ctx, id, []models.RuntimePod{*target}, inventory)
			if e != nil {
				return nil, e
			}
			if len(allowed) == 0 {
				fail(errors.New("target is excluded by persisted upgrade recovery placement policy"))
				result.Items = append(result.Items, item)
				continue
			}
		}
		if err := validateRuntimeManagementTarget(*source, *target); err != nil {
			fail(err)
			result.Items = append(result.Items, item)
			continue
		}
		registeredTarget, e := s.scheduler.podRepo.GetByID(ctx, target.ID)
		if e != nil {
			return nil, e
		}
		if registeredTarget == nil || registeredTarget.PodUID == nil || registeredTarget.State != "ready" || registeredTarget.Draining || registeredTarget.SchedulingPaused {
			fail(errors.New("target Agent is not ready or scheduling is paused"))
			result.Items = append(result.Items, item)
			continue
		}
		if !target.KubernetesReady || target.PodUID == nil || target.RegisteredPodUID == nil || target.AgentState != "ready" || target.KubernetesPodUID == nil || *target.KubernetesPodUID != *registeredTarget.PodUID || *target.RegisteredPodUID != *registeredTarget.PodUID || target.AgentEndpoint == nil {
			fail(errors.New("target Kubernetes readiness and Agent Pod UID must agree"))
			result.Items = append(result.Items, item)
			continue
		}
		item.TargetPodUID = *target.PodUID
		if target.LastSeenAt == nil || time.Since(*target.LastSeenAt) > s.scheduler.heartbeatTimeout*3 {
			fail(errors.New("target Agent heartbeat is stale"))
			result.Items = append(result.Items, item)
			continue
		}
		if _, err := s.scheduler.gatewayEnvironment(instance); err != nil {
			fail(err)
			result.Items = append(result.Items, item)
			continue
		}
		bindings, err := s.scheduler.bindingRepo.ListByRuntimePodID(ctx, target.ID)
		if err != nil {
			return nil, err
		}
		required, e := runtimeManagementSourceSlot(*source, *binding)
		if e != nil {
			fail(e)
			result.Items = append(result.Items, item)
			continue
		}
		item.SourcePlacement = runtimeEvictionPlacement(*source, required)
		if err := runtimeManagementResourceBudget(*target); err != nil {
			fail(err)
			result.Items = append(result.Items, item)
			continue
		}
		slots, err := runtimeManagementFreeSlots(*target, bindings, s.scheduler.gatewayPortStart, s.scheduler.gatewayPortEnd)
		if err != nil {
			fail(err)
			result.Items = append(result.Items, item)
			continue
		}
		if allocated[target.ID] == nil {
			allocated[target.ID] = map[int]bool{}
		}
		for _, slot := range slots {
			if !allocated[target.ID][slot] && runtimeManagementSlotReady(*target, slot) && runtimeManagementSlotCanFit(*target, slot, required, s.scheduler.gatewayPortStart) && (req.TargetSlot == 0 || req.TargetSlot == slot) {
				item.TargetSlot = slot
				break
			}
		}
		if item.TargetSlot == 0 || len(allocated[target.ID]) >= target.Capacity-target.UsedSlots {
			fail(errors.New("requested target slot or batch capacity is unavailable"))
		} else {
			allocated[target.ID][item.TargetSlot] = true
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func (s *RuntimeManagementService) Submit(ctx context.Context, req RuntimeSchedulingRequest, actor *int) (*RuntimeManagementOperation, error) {
	plan, err := s.Preflight(ctx, req)
	if err != nil {
		return nil, err
	}
	if !plan.Allowed {
		return nil, errors.New(strings.Join(plan.Blockers, "; "))
	}
	return s.enqueue(ctx, "migration", req, plan.Items, actor)
}

func (s *RuntimeManagementService) Evict(ctx context.Context, podID int64, req RuntimeSchedulingRequest, actor *int) (*RuntimeManagementOperation, error) {
	if req.Mode != "" && req.Mode != "graceful" && req.Mode != "force" {
		return nil, errors.New("eviction mode must be graceful or force")
	}
	if req.GracePeriodSeconds < 0 || req.GracePeriodSeconds > 300 || req.Concurrency < 0 || req.Concurrency > 5 {
		return nil, errors.New("grace period must be 1..300 seconds and concurrency 1..5")
	}
	if s.scheduler == nil {
		return nil, errors.New("Runtime scheduler unavailable")
	}
	if req.Mode == "force" && len(s.SupportedEvictionModes()) < 2 {
		return nil, errors.New(RuntimeForceEvictionUnavailable)
	}
	source, err := s.scheduler.podRepo.GetByID(ctx, podID)
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, errors.New("Runtime Pod not found")
	}
	// Serialize with ordinary claim+binding preparation before taking occupancy.
	sourceUnlock := s.scheduler.lockGatewayCreateForPod(podID)
	// Pause admission before planning; a failed eviction leaves the Pod paused for inspection.
	if err = s.Scheduling(ctx, podID, false); err != nil {
		sourceUnlock()
		return nil, err
	}
	bindings, err := s.scheduler.bindingRepo.ListByRuntimePodID(ctx, podID)
	sourceUnlock()
	if err != nil {
		return nil, err
	}
	if req.Mode == "force" {
		req.SourcePodID, req.TargetPodID, req.TargetSlot = podID, 0, 0
		req.InstanceIDs = nil
		for _, binding := range bindings {
			req.InstanceIDs = append(req.InstanceIDs, binding.InstanceID)
		}
		if len(bindings) == 0 {
			return s.enqueue(ctx, "eviction", req, nil, actor)
		}
		plan, e := s.preflight(ctx, req, true)
		if e != nil {
			return nil, e
		}
		if !plan.Allowed {
			return nil, errors.New(strings.Join(plan.Blockers, "; "))
		}
		return s.enqueue(ctx, "eviction", req, plan.Items, actor)
	}
	items := []RuntimeSchedulingItem{}
	ids := []int{}
	inventory, err := s.scheduler.RuntimeDeploymentPods(ctx, source.RuntimeType)
	if err != nil {
		return nil, err
	}
	allocated := map[int64]map[int]bool{}
	var liveSource *models.RuntimePod
	for i := range inventory {
		if inventory[i].ID == source.ID {
			liveSource = &inventory[i]
			break
		}
	}
	if liveSource == nil || stringValue(liveSource.KubernetesPodUID) != stringValue(source.PodUID) {
		return nil, errors.New("source Kubernetes Pod identity is unavailable or changed")
	}
	inventories := map[string][]models.RuntimePod{source.RuntimeType: inventory}
	for _, binding := range bindings {
		required, e := runtimeManagementSourceSlot(*liveSource, binding)
		if e != nil {
			return nil, e
		}
		var selected *RuntimeSchedulingItem
		for _, target := range inventory {
			if target.ID == source.ID || validateRuntimeManagementTarget(*liveSource, target) != nil {
				continue
			}
			plan, err := s.preflightWithInventory(ctx, RuntimeSchedulingRequest{InstanceIDs: []int{binding.InstanceID}, TargetPodID: target.ID}, false, inventories)
			if err != nil {
				return nil, err
			}
			if !plan.Allowed {
				continue
			}
			existing, err := s.scheduler.bindingRepo.ListByRuntimePodID(ctx, target.ID)
			if err != nil {
				return nil, err
			}
			slots, _ := runtimeManagementFreeSlots(target, existing, s.scheduler.gatewayPortStart, s.scheduler.gatewayPortEnd)
			if allocated[target.ID] == nil {
				allocated[target.ID] = map[int]bool{}
			}
			if len(allocated[target.ID]) >= target.Capacity-target.UsedSlots {
				continue
			}
			for _, slot := range slots {
				if !allocated[target.ID][slot] && runtimeManagementSlotReady(target, slot) && runtimeManagementSlotCanFit(target, slot, required, s.scheduler.gatewayPortStart) {
					item := plan.Items[0]
					item.TargetSlot = slot
					selected = &item
					allocated[target.ID][slot] = true
					break
				}
			}
			if selected != nil {
				break
			}
		}
		if selected == nil {
			return nil, fmt.Errorf("no ready compatible destination capacity for instance %d; Pod remains scheduling paused", binding.InstanceID)
		}
		items = append(items, *selected)
		ids = append(ids, binding.InstanceID)
	}
	req.SourcePodID = podID
	req.InstanceIDs = ids
	return s.enqueue(ctx, "eviction", req, items, actor)
}

func runtimeManagementSlotReady(target models.RuntimePod, slot int) bool {
	for _, s := range target.Slots {
		if s.Index == slot {
			return s.Ready && s.InstanceID == nil
		}
	}
	return false
}

func (s *RuntimeManagementService) enqueue(ctx context.Context, kind string, req RuntimeSchedulingRequest, items []RuntimeSchedulingItem, actor *int) (*RuntimeManagementOperation, error) {
	if s.guards == nil {
		return nil, errors.New("instance mutation reservation unavailable")
	}
	release, err := s.guards.ReserveInstanceMutations(req.InstanceIDs)
	if err != nil {
		return nil, err
	}
	defer release()
	now := time.Now().UTC()
	op := &RuntimeManagementOperation{ID: "runtime_mgmt_" + uuid.NewString(), Kind: kind, Status: "pending", Items: items, CreatedAt: now, UpdatedAt: now}
	for i := range op.Items {
		op.Items[i].State = "pending"
	}
	if len(items) == 0 && kind != "eviction" {
		op.Status = "completed"
	}
	requestJSON, _ := json.Marshal(req)
	itemsJSON, _ := json.Marshal(op.Items)
	err = s.sess.TxContext(ctx, func(tx db.Session) error {
		if err := repository.LockRuntimeManagementAdmission(ctx, tx); err != nil {
			return err
		}
		var active int
		row, err := tx.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM runtime_rollouts WHERE status IN ('pending','running')")
		if err != nil {
			return err
		}
		if err = row.Scan(&active); err != nil {
			return err
		}
		if active > 0 {
			return errors.New("Runtime rollout is active; scheduling is locked")
		}
		if _, err = tx.SQL().ExecContext(ctx, `INSERT INTO runtime_management_operations (id,kind,status,actor_user_id,request_json,items_json,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?)`, op.ID, kind, op.Status, actor, string(requestJSON), string(itemsJSON), now, now); err != nil {
			return err
		}
		for _, id := range req.InstanceIDs {
			if _, err = tx.SQL().ExecContext(ctx, `UPDATE instance_lifecycle_manual_guards SET token=? WHERE instance_id=?`, op.ID, id); err != nil {
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

func (s *RuntimeManagementService) save(ctx context.Context, op *RuntimeManagementOperation) error {
	op.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(op.Items)
	if err != nil {
		return err
	}
	result, err := s.sess.SQL().ExecContext(ctx, `UPDATE runtime_management_operations SET status=?,items_json=?,updated_at=? WHERE id=? AND (status IN ('starting','partial_failure') OR (status='processing' AND owner_token=?))`, op.Status, string(data), op.UpdatedAt, op.ID, s.token)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("operation execution ownership lost; further work held")
	}
	return nil
}

func (s *RuntimeManagementService) History(ctx context.Context) ([]RuntimeManagementOperation, error) {
	return s.operations(ctx, false)
}

func (s *RuntimeManagementService) operations(ctx context.Context, activeOnly bool) ([]RuntimeManagementOperation, error) {
	items := []RuntimeManagementOperation{}
	query := `SELECT id,kind,status,items_json,created_at,updated_at,cancel_requested,request_json FROM runtime_management_operations`
	if activeOnly {
		query += ` WHERE status IN ('starting','partial_failure') ORDER BY created_at`
	} else {
		query += ` ORDER BY created_at DESC LIMIT 100`
	}
	rows, err := s.sess.SQL().QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var op RuntimeManagementOperation
		var raw, requestRaw string
		if err = rows.Scan(&op.ID, &op.Kind, &op.Status, &raw, &op.CreatedAt, &op.UpdatedAt, &op.CancelRequested, &requestRaw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &op.Items); err != nil {
			return nil, err
		}
		if op.Kind == "scale_in" {
			var journal runtimePodScaleInJournal
			if err = json.Unmarshal([]byte(requestRaw), &journal); err != nil {
				return nil, err
			}
			op.ErrorMessage = journal.ErrorMessage
			op.Phase = journal.Phase
		}
		items = append(items, op)
	}
	return items, rows.Err()
}

// Process runs inside the elected scheduler before taking an instance snapshot.
// An interrupted processing item is held for inspection; it is never replayed.
func (s *RuntimeManagementService) Process(ctx context.Context) error {
	if err := s.refresh(ctx); err != nil {
		return err
	}
	_, err := s.sess.SQL().ExecContext(ctx, `UPDATE runtime_management_operations SET status='needs_attention',owner_token='',updated_at=? WHERE status='processing' AND owner_token<>?`, time.Now().UTC(), s.token)
	if err != nil {
		return err
	}
	var id, raw, kind string
	rows, err := s.sess.SQL().QueryContext(ctx, `SELECT id,kind,request_json FROM runtime_management_operations WHERE status='pending' ORDER BY created_at LIMIT 1`)
	if err != nil {
		return err
	}
	if !rows.Next() {
		err = rows.Err()
		rows.Close()
		return err
	}
	err = rows.Scan(&id, &kind, &raw)
	rows.Close()
	if err != nil {
		return err
	}
	claimed, err := s.sess.SQL().ExecContext(ctx, `UPDATE runtime_management_operations SET status='processing',owner_token=?,updated_at=? WHERE id=? AND status='pending'`, s.token, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	count, err := claimed.RowsAffected()
	if err != nil || count != 1 {
		return err
	}
	var req RuntimeSchedulingRequest
	if err = json.Unmarshal([]byte(raw), &req); err != nil {
		return err
	}
	var itemJSON string
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT items_json FROM runtime_management_operations WHERE id=?`, id)
	if err != nil {
		return err
	}
	if err = row.Scan(&itemJSON); err != nil {
		return err
	}
	op := &RuntimeManagementOperation{ID: id, Kind: kind, Status: "processing"}
	if err = json.Unmarshal([]byte(itemJSON), &op.Items); err != nil {
		return err
	}
	operationCtx, cancelOperation := context.WithCancel(ctx)
	defer cancelOperation()
	if s.scheduler.leader != nil {
		go func() {
			ticker := time.NewTicker(4 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-operationCtx.Done():
					return
				case <-ticker.C:
					if !s.scheduler.leader.IsLeader(operationCtx) {
						cancelOperation()
						return
					}
				}
			}
		}()
	}
	if kind == "scale_in" {
		return s.processScaleIn(operationCtx, op, raw)
	}
	// ponytail: bounded per-operation workers; one scheduler operation runs at a time.
	concurrency := req.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > 5 {
		concurrency = 5
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var journalErr error
	for worker := 0; worker < concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				mu.Lock()
				cancelled, e := s.cancelRequested(ctx, op.ID)
				if e != nil {
					journalErr = e
				}
				if cancelled {
					op.Items[i].State = "cancelled"
					journalErr = s.save(ctx, op)
					mu.Unlock()
					continue
				}
				if journalErr != nil {
					mu.Unlock()
					continue
				}
				op.Items[i].State = "processing"
				journalErr = s.save(ctx, op)
				item := op.Items[i]
				failedJournal := journalErr != nil
				mu.Unlock()
				if failedJournal {
					continue
				}
				seconds := req.GracePeriodSeconds
				if seconds <= 0 {
					seconds = 30
				}
				item.StopGraceSeconds = seconds
				stopCtx, cancel := context.WithTimeout(operationCtx, time.Duration(seconds)*time.Second+runtimeAgentControlTimeout)
				run := func() error {
					if kind == "eviction" && req.Mode == "force" {
						return s.evictSource(stopCtx, item, op.ID)
					}
					return s.move(stopCtx, item, op.ID)
				}
				var moveErr error
				if s.scheduler.upgrade != nil {
					moveErr = s.scheduler.upgrade.WithInstanceMutation(stopCtx, item.InstanceID, run)
				} else {
					moveErr = run()
				}
				cancel()
				mu.Lock()
				if moveErr != nil && isRuntimeAssignmentPending(moveErr) {
					op.Items[i].State = "starting"
					op.Items[i].ErrorMessage = moveErr.Error()
				} else if moveErr != nil {
					op.Items[i].State = "failed"
					op.Items[i].ErrorMessage = moveErr.Error()
				} else if kind == "eviction" && req.Mode == "force" {
					op.Items[i].State = "waiting_capacity"
				} else {
					op.Items[i].State = "starting"
				}
				if e := s.save(ctx, op); e != nil {
					journalErr = e
				}
				mu.Unlock()
			}
		}()
	}
	for i := range op.Items {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	if journalErr != nil {
		return journalErr
	}

	if kind == "eviction" {
		sourceIDs := map[int64]bool{req.SourcePodID: true}
		for _, item := range op.Items {
			sourceIDs[item.SourcePodID] = true
		}
		for sourceID := range sourceIDs {
			remaining, e := s.scheduler.bindingRepo.ListByRuntimePodID(ctx, sourceID)
			if e != nil {
				return e
			}
			for _, binding := range remaining {
				found := false
				for _, item := range op.Items {
					if item.InstanceID == binding.InstanceID {
						found = true
					}
				}
				if !found {
					op.Items = append(op.Items, RuntimeSchedulingItem{InstanceID: binding.InstanceID, SourcePodID: sourceID, State: "failed", ErrorMessage: "instance arrived during eviction admission; rerun eviction after outstanding starts settle"})
				}
			}
		}
	}
	op.Status = runtimeManagementStatus(op.Items)
	if err = s.save(ctx, op); err != nil {
		return err
	}
	_, err = s.sess.SQL().ExecContext(ctx, `DELETE FROM instance_lifecycle_manual_guards WHERE token=?`, op.ID)
	return err
}

func (s *RuntimeManagementService) move(ctx context.Context, item RuntimeSchedulingItem, ownerIDs ...string) error {
	ownerID := ""
	if len(ownerIDs) > 0 {
		ownerID = ownerIDs[0]
	}
	if err := s.assertOwner(ctx, ownerID); err != nil {
		return err
	}
	sch := s.scheduler
	unlock := sch.lockGatewayCreateForPod(item.TargetPodID)
	defer unlock()
	plan, err := s.Preflight(ctx, RuntimeSchedulingRequest{InstanceIDs: []int{item.InstanceID}, TargetPodID: item.TargetPodID, TargetSlot: item.TargetSlot})
	if err != nil {
		return err
	}
	if !plan.Allowed {
		return errors.New(strings.Join(plan.Blockers, "; "))
	}
	current := plan.Items[0]
	if current.SourceGeneration != item.SourceGeneration || current.SourceGatewayID != item.SourceGatewayID || current.SourcePodUID != item.SourcePodUID || current.TargetPodUID != item.TargetPodUID || (item.SourcePlacement != nil && !runtimeEvictionPlacementMatches(current.SourcePlacement, item.SourcePlacement)) {
		return errors.New("source or target identity changed since confirmation; move cancelled")
	}
	instance, err := sch.instanceRepo.GetByID(item.InstanceID)
	if err != nil {
		return err
	}
	binding, err := sch.bindingRepo.GetByInstanceID(ctx, item.InstanceID)
	if err != nil {
		return err
	}
	if instance == nil || binding == nil || binding.RuntimePodID != item.SourcePodID || binding.Generation != item.SourceGeneration || binding.GatewayID != item.SourceGatewayID || instance.RuntimeGeneration != item.SourceGeneration {
		return errors.New("source binding changed since preflight")
	}
	source, err := sch.podRepo.GetByID(ctx, item.SourcePodID)
	if err != nil {
		return err
	}
	target, err := sch.podRepo.GetByID(ctx, item.TargetPodID)
	if err != nil {
		return err
	}
	if source == nil || target == nil || source.AgentEndpoint == nil || target.AgentEndpoint == nil || stringValue(source.PodUID) != item.SourcePodUID || stringValue(target.PodUID) != item.TargetPodUID {
		return errors.New("Runtime Pod identity changed; move cancelled")
	}
	if err := s.assertOwner(ctx, ownerID); err != nil {
		return err
	}
	claimed, err := sch.podRepo.TryClaimSlot(ctx, target.ID)
	if err != nil {
		return err
	}
	if !claimed {
		return errors.New("target admission changed; source preserved")
	}
	reservationOwned := true
	defer func() {
		if reservationOwned {
			_ = sch.podRepo.ReleaseSlot(context.Background(), target.ID)
		}
	}()
	instanceCopy := *instance
	instanceCopy.RuntimeGeneration++
	instanceCopy.Status = "stopped"
	start, err := sch.prepareGatewayStartParameters(ctx, instanceCopy, target.RuntimeType, *target)
	if err != nil {
		return err
	}
	if err := s.stopSource(ctx, item, instance, binding, source, ownerID, false); err != nil {
		return err
	}

	if err := s.assertOwner(ctx, ownerID); err != nil {
		return err
	}
	excluded := map[int]struct{}{}
	port := sch.gatewayPortStart + (item.TargetSlot-1)*RuntimeGatewayPortBlockSize(target.RuntimeType)
	for p := sch.gatewayPortStart; p <= sch.gatewayPortEnd; p += RuntimeGatewayPortBlockSize(target.RuntimeType) {
		if p != port {
			excluded[p] = struct{}{}
		}
	}
	reserved, err := sch.reserveGatewayPortExcludingPorts(ctx, instanceCopy, target.RuntimeType, *target, start.workspacePath, excluded)
	if err != nil {
		return err
	}
	reservationOwned = false
	start.reservedBinding = reserved
	// Do not free an ambiguous create reservation: the existing lifecycle engine
	// reconciles only this exact target identity after a lost HTTP response.
	if err := s.assertOwner(ctx, ownerID); err != nil {
		return err
	}
	return sch.createGatewayOnPod(ctx, instanceCopy, target.RuntimeType, *target, start)
}

// Instances occupy pre-created containers. Reservations belong to the static
// Kubernetes slot profile, not the legacy Instance CPUCores/MemoryGB fields.
func runtimeManagementResourceBudget(pod models.RuntimePod) error {
	if pod.CPUMillisLimit == nil || pod.MemoryBytesLimit == nil || *pod.CPUMillisLimit <= 0 || *pod.MemoryBytesLimit <= 0 || pod.CPUMillisRequest == nil || pod.MemoryBytesRequest == nil || pod.CoordinatorCPUMillisRequest == nil || pod.CoordinatorMemoryBytesRequest == nil || pod.Capacity <= 0 || len(pod.Slots) != pod.Capacity {
		return errors.New("Runtime Pod resource reservations unavailable")
	}
	cpu, memory := *pod.CPUMillisRequest, *pod.MemoryBytesRequest
	if cpu < 0 || memory < 0 || cpu > *pod.CPUMillisLimit || memory > *pod.MemoryBytesLimit || *pod.CoordinatorCPUMillisRequest < 0 || *pod.CoordinatorMemoryBytesRequest < 0 || *pod.CoordinatorCPUMillisRequest > cpu || *pod.CoordinatorMemoryBytesRequest > memory {
		return errors.New("Runtime Pod resource reservation budget exceeded")
	}
	cpu -= *pod.CoordinatorCPUMillisRequest
	memory -= *pod.CoordinatorMemoryBytesRequest
	for _, slot := range pod.Slots {
		if slot.CPUMillisRequest == nil || slot.MemoryBytesRequest == nil || *slot.CPUMillisRequest < 0 || *slot.MemoryBytesRequest < 0 {
			return errors.New("slot resource reservations unavailable")
		}
		if *slot.CPUMillisRequest > cpu || *slot.MemoryBytesRequest > memory {
			return errors.New("configured slot requests exceed the shared Pod reservation budget")
		}
		cpu -= *slot.CPUMillisRequest
		memory -= *slot.MemoryBytesRequest
	}
	return nil
}

func runtimeManagementSourceSlot(source models.RuntimePod, binding models.InstanceRuntimeBinding) (models.RuntimeSlot, error) {
	if err := runtimeManagementResourceBudget(source); err != nil {
		return models.RuntimeSlot{}, err
	}
	for _, slot := range source.Slots {
		if slot.GatewayPort != nil && *slot.GatewayPort == binding.GatewayPort {
			return runtimeManagementEffectiveSlot(source, slot)
		}
	}
	return models.RuntimeSlot{}, errors.New("source slot resource profile is unavailable for its gateway port")
}

func runtimeManagementEffectiveSlot(pod models.RuntimePod, slot models.RuntimeSlot) (models.RuntimeSlot, error) {
	if pod.CPUMillisLimit == nil || pod.MemoryBytesLimit == nil || slot.CPUMillisLimit == nil || slot.MemoryBytesLimit == nil || slot.CPUMillisRequest == nil || slot.MemoryBytesRequest == nil {
		return models.RuntimeSlot{}, errors.New("slot physical resource profile unavailable")
	}
	cpu, memory := *slot.CPUMillisLimit, *slot.MemoryBytesLimit
	if *pod.CPUMillisLimit < cpu {
		cpu = *pod.CPUMillisLimit
	}
	if *pod.MemoryBytesLimit < memory {
		memory = *pod.MemoryBytesLimit
	}
	if cpu <= 0 || memory <= 0 || *slot.CPUMillisRequest < 0 || *slot.MemoryBytesRequest < 0 || *slot.CPUMillisRequest > cpu || *slot.MemoryBytesRequest > memory {
		return models.RuntimeSlot{}, errors.New("slot resource requests exceed its effective hard limits")
	}
	slot.CPUMillisLimit, slot.MemoryBytesLimit = &cpu, &memory
	return slot, nil
}

func runtimeManagementStatus(items []RuntimeSchedulingItem) string {
	failed := 0
	cancelled := 0
	for _, item := range items {
		if item.State == "starting" || item.State == "waiting_capacity" || item.State == "pending" || item.State == "processing" {
			return "starting"
		}
		if item.State == "cancelled" {
			cancelled++
		}
		if item.State == "failed" {
			failed++
		}
	}
	if failed == 0 && cancelled > 0 {
		return "cancelled"
	}
	if failed == 0 {
		return "completed"
	}
	if failed == len(items) {
		return "failed"
	}
	return "partial_failure"
}

func (s *RuntimeManagementService) refresh(ctx context.Context) error {
	if err := s.refreshScaleIn(ctx); err != nil {
		return err
	}
	// Recover the narrow crash window between terminal journaling and guard cleanup.
	if _, err := s.sess.SQL().ExecContext(ctx, `DELETE g FROM instance_lifecycle_manual_guards g JOIN runtime_management_operations o ON o.id=g.token COLLATE utf8mb4_unicode_ci WHERE o.status IN ('completed','failed','partial_failure','cancelled')`); err != nil {
		return err
	}
	ops, err := s.operations(ctx, true)
	if err != nil {
		return err
	}
	for _, op := range ops {
		if op.Kind == "scale_in" {
			continue
		}
		if op.Status != "starting" && op.Status != "partial_failure" {
			continue
		}
		changed := false
		for i := range op.Items {
			item := &op.Items[i]
			if item.State != "starting" && item.State != "waiting_capacity" {
				continue
			}
			binding, err := s.scheduler.bindingRepo.GetByInstanceID(ctx, item.InstanceID)
			if err != nil {
				return err
			}
			if binding == nil {
				instance, e := s.scheduler.instanceRepo.GetByID(item.InstanceID)
				if e != nil {
					return e
				}
				// An explicit stop/delete or later generation ends either pending
				// start path. Preserve uncertain live starts; never replay them here.
				if instance == nil || instance.RuntimeGeneration != item.SourceGeneration+1 || instance.Status == "stopped" || instance.Status == "deleted" || instance.Status == "deleting" || (isRuntimeErrorInstance(*instance) && !isRetryableRuntimeSchedulingError(*instance)) {
					item.State = "failed"
					item.ErrorMessage = "instance intent changed or scheduling failed after confirmed source stop"
					changed = true
				}
				continue
			}
			if item.State == "waiting_capacity" && item.TargetPodID == 0 {
				destination, e := s.scheduler.podRepo.GetByID(ctx, binding.RuntimePodID)
				if e != nil {
					return e
				}
				if destination == nil || destination.PodUID == nil {
					continue
				}
				item.TargetPodID, item.TargetPodUID = binding.RuntimePodID, *destination.PodUID
				item.TargetSlot = (binding.GatewayPort-s.scheduler.gatewayPortStart)/RuntimeGatewayPortBlockSize(destination.RuntimeType) + 1
				item.State = "starting"
				changed = true
			}
			target, e := s.scheduler.podRepo.GetByID(ctx, item.TargetPodID)
			if e != nil {
				return e
			}
			if binding.RuntimePodID != item.TargetPodID || binding.Generation != item.SourceGeneration+1 || target == nil || stringValue(target.PodUID) != item.TargetPodUID {
				item.State = "failed"
				item.ErrorMessage = "instance placement identity changed after scheduling"
				changed = true
				continue
			}
			if binding.State == "running" {
				item.State = "completed"
				item.ErrorMessage = ""
				changed = true
			} else if binding.State == "error" || binding.State == "failed" || binding.State == "unhealthy" {
				item.State = "failed"
				item.ErrorMessage = stringValue(binding.ErrorMessage)
				changed = true
			}
		}
		if changed {
			op.Status = runtimeManagementStatus(op.Items)
			if err = s.save(ctx, &op); err != nil {
				return err
			}
			if op.Status != "starting" {
				if _, err = s.sess.SQL().ExecContext(ctx, "DELETE FROM instance_lifecycle_manual_guards WHERE token=?", op.ID); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func runtimeManagementSourceState(state string) bool {
	switch state {
	case "running", "error", "failed", "stopped", "unhealthy":
		return true
	}
	return false
}

func runtimeManagementSlotResources(target models.RuntimePod, slot, required models.RuntimeSlot) error {
	actual, err := runtimeManagementEffectiveSlot(target, slot)
	if err != nil {
		return err
	}
	if required.CPUMillisLimit == nil || required.MemoryBytesLimit == nil || required.CPUMillisRequest == nil || required.MemoryBytesRequest == nil {
		return errors.New("source slot physical resource profile unavailable")
	}
	var differences []string
	if *actual.CPUMillisLimit < *required.CPUMillisLimit {
		differences = append(differences, fmt.Sprintf("CPU limit %dm < source %dm", *actual.CPUMillisLimit, *required.CPUMillisLimit))
	}
	if *actual.MemoryBytesLimit < *required.MemoryBytesLimit {
		differences = append(differences, fmt.Sprintf("memory limit %s < source %s", resource.NewQuantity(*actual.MemoryBytesLimit, resource.BinarySI), resource.NewQuantity(*required.MemoryBytesLimit, resource.BinarySI)))
	}
	if *actual.CPUMillisRequest < *required.CPUMillisRequest {
		differences = append(differences, fmt.Sprintf("CPU request %dm < source %dm", *actual.CPUMillisRequest, *required.CPUMillisRequest))
	}
	if *actual.MemoryBytesRequest < *required.MemoryBytesRequest {
		differences = append(differences, fmt.Sprintf("memory request %s < source %s", resource.NewQuantity(*actual.MemoryBytesRequest, resource.BinarySI), resource.NewQuantity(*required.MemoryBytesRequest, resource.BinarySI)))
	}
	if len(differences) > 0 {
		return fmt.Errorf("target Pod #%d (%s) Slot %d has insufficient physical resources: %s", target.ID, target.PodName, slot.Index, strings.Join(differences, "; "))
	}
	return nil
}

func runtimeManagementSlotCanFit(target models.RuntimePod, index int, required models.RuntimeSlot, portStarts ...int) bool {
	return runtimeManagementSlotFitError(target, index, required, portStarts...) == nil
}

func runtimeManagementSlotFitError(target models.RuntimePod, index int, required models.RuntimeSlot, portStarts ...int) error {
	for _, slot := range target.Slots {
		if slot.Index == index {
			if len(portStarts) > 0 && (slot.GatewayPort == nil || *slot.GatewayPort != portStarts[0]+(index-1)*RuntimeGatewayPortBlockSize(target.RuntimeType)) {
				return fmt.Errorf("target Pod #%d Slot %d gateway port evidence is unavailable or inconsistent", target.ID, index)
			}
			return runtimeManagementSlotResources(target, slot, required)
		}
	}
	return fmt.Errorf("target Pod #%d physical Slot %d is unavailable", target.ID, index)
}

func (s *RuntimeManagementService) assertOwner(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if id == "" {
		return nil
	}
	var count int
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_management_operations WHERE id=? AND status='processing' AND owner_token=?`, id, s.token)
	if err != nil {
		return err
	}
	if err = row.Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return errors.New("operation ownership lost; work held")
	}
	return nil
}

func (s *RuntimeManagementService) cancelRequested(ctx context.Context, id string) (bool, error) {
	var cancelled bool
	row, err := s.sess.SQL().QueryRowContext(ctx, "SELECT cancel_requested FROM runtime_management_operations WHERE id=?", id)
	if err != nil {
		return false, err
	}
	err = row.Scan(&cancelled)
	return cancelled, err
}
func (s *RuntimeManagementService) Cancel(ctx context.Context, id string) error {
	result, err := s.sess.SQL().ExecContext(ctx, "UPDATE runtime_management_operations SET cancel_requested=1,updated_at=? WHERE id=? AND (status IN ('pending','processing') OR (kind='scale_in' AND status IN ('starting','needs_attention')))", time.Now().UTC(), id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("operation is not pending or processing")
	}
	return nil
}
