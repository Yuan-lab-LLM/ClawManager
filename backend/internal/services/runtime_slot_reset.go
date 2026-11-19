package services

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/runtimeeviction"
	k8sservice "clawreef/internal/services/k8s"
	"github.com/google/uuid"
	"github.com/upper/db/v4"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var ErrRuntimeSlotResetCommitUncertain = errors.New("slot reset acceptance is not confirmed; query the original request")

type RuntimeSlotResetRequest struct {
	SourcePodUID      string `json:"source_pod_uid"`
	SourceContainerID string `json:"source_container_id"`
	ScopeFingerprint  string `json:"scope_fingerprint"`
	RequestID         string `json:"request_id"`
}

type RuntimeSlotResetPreflight struct {
	Allowed          bool     `json:"allowed"`
	Blockers         []string `json:"blockers"`
	RuntimeType      string   `json:"runtime_type"`
	PodUID           string   `json:"pod_uid"`
	PodName          string   `json:"pod_name"`
	NodeName         string   `json:"node_name"`
	Slot             string   `json:"slot"`
	ContainerID      string   `json:"container_id"`
	ScopeFingerprint string   `json:"scope_fingerprint"`
	InstanceID       int      `json:"instance_id"`
	Generation       int      `json:"generation"`
	CurrentPodID     int64    `json:"current_pod_id"`
	CurrentSlot      string   `json:"current_slot"`
	RecoveryAction   string   `json:"recovery_action"`
	journal          *RuntimeSlotResetJournal
}

type RuntimeSlotResetJournal struct {
	Phase                  string                             `json:"phase"`
	Slot                   string                             `json:"slot"`
	PodUID                 string                             `json:"pod_uid"`
	ContainerID            string                             `json:"container_id"`
	InstanceID             int                                `json:"instance_id"`
	GatewayPort            int                                `json:"gateway_port"`
	RecoveryAction         string                             `json:"recovery_action"`
	RecoveryGeneration     int                                `json:"recovery_generation,omitempty"`
	RecoveryState          string                             `json:"recovery_state"`
	ErrorMessage           string                             `json:"error_message,omitempty"`
	Scope                  runtimeeviction.SlotScope          `json:"scope"`
	Receipt                *runtimeeviction.SlotReceipt       `json:"receipt,omitempty"`
	Source                 models.RuntimePod                  `json:"source"`
	Owner                  *runtimeSlotAuditOwner             `json:"owner,omitempty"`
	AssignmentDigest       string                             `json:"assignment_digest,omitempty"`
	SourceBinding          *models.InstanceRuntimeBinding     `json:"source_binding,omitempty"`
	SourcePod              *corev1.Pod                        `json:"source_pod"`
	HelperImage            string                             `json:"helper_image"`
	CleanupCommitted       bool                               `json:"cleanup_committed"`
	CurrentWorkerEpoch     string                             `json:"current_worker_epoch,omitempty"`
	Assignment             *k8sservice.RuntimeFaultAssignment `json:"assignment,omitempty"`
	WriterScope            *runtimeeviction.Scope             `json:"writer_scope,omitempty"`
	WriterReceipt          *runtimeeviction.Receipt           `json:"writer_receipt,omitempty"`
	WriterClaim            *k8sservice.RuntimeSlotResetClaim  `json:"writer_claim,omitempty"`
	WriterJobAuthorized    bool                               `json:"writer_job_authorized,omitempty"`
	StorageIdentity        string                             `json:"storage_identity,omitempty"`
	ScopeFingerprint       string                             `json:"scope_fingerprint,omitempty"`
	UnpublishedReservation *runtimeSlotResetUnpublishedProof  `json:"unpublished_reservation,omitempty"`
}

// SQL provenance of a never-published first reservation is distinct from a
// native owner. It never grants authority to delete instance-wide writers.
type runtimeSlotResetUnpublishedProof struct {
	UserID      int    `json:"user_id"`
	WorkerEpoch string `json:"worker_epoch"`
}

func sameSlotResetBinding(a, b models.InstanceRuntimeBinding) bool {
	return a.ID == b.ID && a.InstanceID == b.InstanceID && a.RuntimePodID == b.RuntimePodID && a.RuntimeType == b.RuntimeType && a.Generation == b.Generation && a.GatewayID == b.GatewayID && a.GatewayPort == b.GatewayPort && a.WorkspacePath == b.WorkspacePath
}

func sameSlotResetOwner(a, b *runtimeSlotAuditOwner) bool {
	return a != nil && b != nil && a.PodUID == b.PodUID && a.WorkerEpoch == b.WorkerEpoch && a.GatewayID == b.GatewayID && a.InstanceID == b.InstanceID && a.UserID == b.UserID && a.Generation == b.Generation
}

func validSlotResetOwner(a *runtimeSlotAuditOwner, podUID, digest string) bool {
	b, err := hex.DecodeString(digest)
	return a != nil && a.PodUID == podUID && a.InstanceID > 0 && a.UserID > 0 && a.Generation > 0 && a.GatewayID == fmt.Sprintf("gw-%d-%d", a.InstanceID, a.Generation) && stopTokenValid(a.WorkerEpoch) && err == nil && len(b) == 32 && hex.EncodeToString(b) == digest
}

func slotResetRecoveryAction(instance *models.Instance, current *models.InstanceRuntimeBinding, source models.InstanceRuntimeBinding, desired string) string {
	if current != nil && !sameSlotResetBinding(*current, source) {
		return "preserve_current"
	}
	if instance == nil || instance.RuntimeGeneration < source.Generation || desired != "running" || instance.Status != "running" && instance.Status != "creating" && instance.Status != "error" {
		return "none"
	}
	if instance.Type != "" && source.RuntimeType != "" && instance.Type != source.RuntimeType || instance.InstanceMode != "" && instance.InstanceMode != "lite" || instance.RuntimeType != "" && instance.RuntimeType != "gateway" {
		return "none"
	}
	return "reschedule"
}

func (s *RuntimeManagementService) PreflightSlotReset(ctx context.Context, podID int64, slot string, req RuntimeSlotResetRequest) (*RuntimeSlotResetPreflight, error) {
	if s == nil || s.scheduler == nil || s.sess == nil {
		return nil, errors.New("persistent slot reset is unavailable")
	}
	c, ok := s.scheduler.agentClient.(*slotRuntimeAgentClient)
	if !ok || c.kube == nil {
		return nil, errors.New("native slot lifecycle is unavailable")
	}
	source, err := s.scheduler.podRepo.GetByID(ctx, podID)
	if err != nil {
		return nil, err
	}
	if source == nil || stringValue(source.PodUID) == "" {
		return nil, errors.New("registered physical Runtime identity required")
	}
	resetSupported := false
	for _, capability := range source.Capabilities() {
		if capability == "manual-slot-reset-v1" {
			resetSupported = true
		}
	}
	if !resetSupported {
		return nil, errors.New("current Runtime image does not support manual slot reset; use the native Runtime upgrade flow first")
	}
	pod, err := c.kube.CoreV1().Pods(source.Namespace).Get(ctx, source.PodName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if string(pod.UID) != *source.PodUID || pod.DeletionTimestamp != nil {
		return nil, errors.New("source Pod identity changed or is being deleted")
	}
	layout, err := runtimeAuditPhysicalSlots(pod, source.Capacity)
	if err != nil {
		return nil, err
	}
	port := 0
	for _, physical := range layout {
		if physical.Name == slot {
			port = physical.Port
		}
	}
	if port == 0 {
		return nil, errors.New("selected name is not a physical business slot")
	}
	var containerID, image string
	for _, st := range pod.Status.ContainerStatuses {
		if st.Name == slot {
			containerID = st.ContainerID
		}
	}
	for _, container := range pod.Spec.Containers {
		if container.Name == slot {
			image = container.Image
		}
	}
	if !closedSlotContainer(pod, slot, image) {
		return nil, errors.New("selected Runtime lacks a closed native container boundary")
	}
	if !runtimePodMatchesImage(*source, image) {
		return nil, errors.New("slot reset capability does not belong to the current physical slot image")
	}
	node, err := c.kube.CoreV1().Nodes().Get(ctx, pod.Spec.NodeName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	scope := runtimeeviction.SlotScope{Version: 1, OperationID: "preflight", Namespace: pod.Namespace, PodName: pod.Name, PodUID: string(pod.UID), NodeName: node.Name, NodeUID: string(node.UID), NodeBootID: node.Status.NodeInfo.BootID, ContainerName: slot, ContainerID: containerID}
	if err = scope.Validate(); err != nil {
		return nil, err
	}
	if req.SourcePodUID != "" && req.SourcePodUID != scope.PodUID || req.SourceContainerID != "" && req.SourceContainerID != containerID {
		return nil, errors.New("selected slot container changed; inspect it again")
	}
	if err = repository.CheckRuntimePreparationReservationsForType(ctx, s.sess, "", source.RuntimeType); err != nil {
		return nil, err
	}
	if err = repository.CheckRuntimeRolloutActivityForType(ctx, s.sess, source.RuntimeType); err != nil {
		return nil, err
	}
	helperImage := strings.TrimSpace(osRuntimeSlotHelperImage())
	if helperImage == "" {
		app, e := c.kube.AppsV1().Deployments(source.Namespace).Get(ctx, "clawmanager-app", metav1.GetOptions{})
		if e != nil {
			return nil, fmt.Errorf("pin physical reset helper: %w", e)
		}
		for _, container := range app.Spec.Template.Spec.Containers {
			if container.Name == "clawmanager-app" {
				helperImage = container.Image
			}
		}
	}
	if helperImage == "" {
		return nil, errors.New("immutable physical reset helper image is not configured")
	}
	if _, err = k8sservice.BuildRuntimeSlotResetJob(scope, helperImage); err != nil {
		return nil, err
	}
	j := &RuntimeSlotResetJournal{Phase: "planned", Slot: slot, PodUID: scope.PodUID, ContainerID: containerID, GatewayPort: port, RecoveryAction: "none", RecoveryState: "none", Scope: scope, Source: *source, SourcePod: pod.DeepCopy(), HelperImage: helperImage}
	if storage, e := k8sservice.RuntimeFaultStorageIdentity(ctx, c.kube, pod, s.scheduler.workspaceRoot, source.CreatedAt); e == nil {
		j.StorageIdentity = storage
	}
	// The old worker may be unreachable; physical reset remains possible. Only
	// authenticated, matching native state supplies an optional business owner.
	if source.AgentEndpoint != nil {
		var snapshot runtimeSlotAuditSnapshot
		readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = c.do(readCtx, http.MethodGet, *source.AgentEndpoint, "/v1/slots/"+slot+"/state", nil, &snapshot)
		cancel()
		if err == nil && snapshot.PodUID == scope.PodUID && validSlotResetOwner(snapshot.Assignment, scope.PodUID, snapshot.Digest) {
			j.Owner = snapshot.Assignment
			j.AssignmentDigest = snapshot.Digest
			j.InstanceID = j.Owner.InstanceID
		}
	}
	bindings, err := s.scheduler.bindingRepo.ListByRuntimePodID(ctx, podID)
	if err != nil {
		return nil, err
	}
	for _, binding := range bindings {
		if binding.GatewayPort == port {
			if j.SourceBinding != nil {
				return nil, errors.New("multiple SQL reservations claim this physical slot; business ownership is ambiguous")
			}
			if j.Owner != nil && (binding.InstanceID != j.Owner.InstanceID || binding.Generation != j.Owner.Generation || binding.RuntimeType != source.RuntimeType || binding.GatewayID != j.Owner.GatewayID && binding.GatewayID != fmt.Sprintf("pending-%d-%d", j.Owner.InstanceID, j.Owner.Generation)) {
				return nil, errors.New("slot book and SQL owner disagree; recheck source identity")
			}
			copy := binding
			j.SourceBinding = &copy
			if j.InstanceID == 0 {
				j.InstanceID = binding.InstanceID
			}
		}
	}
	result := &RuntimeSlotResetPreflight{Allowed: true, Blockers: []string{}, RuntimeType: source.RuntimeType, PodUID: scope.PodUID, PodName: pod.Name, NodeName: node.Name, Slot: slot, ContainerID: containerID, InstanceID: j.InstanceID, RecoveryAction: "none", journal: j}
	if j.InstanceID > 0 {
		instance, e := s.scheduler.instanceRepo.GetByID(j.InstanceID)
		if e != nil {
			return nil, e
		}
		current, e := s.scheduler.bindingRepo.GetByInstanceID(ctx, j.InstanceID)
		if e != nil {
			return nil, e
		}
		desired, e := s.slotResetDesiredPower(ctx, s.sess, j.InstanceID, instance)
		if e != nil {
			return nil, e
		}
		old := models.InstanceRuntimeBinding{InstanceID: j.InstanceID, RuntimePodID: podID, RuntimeType: source.RuntimeType, GatewayPort: port}
		if j.Owner != nil {
			old.Generation = j.Owner.Generation
			old.GatewayID = j.Owner.GatewayID
		}
		if j.SourceBinding != nil {
			old = *j.SourceBinding
		}
		result.Generation = old.Generation
		j.RecoveryAction = slotResetRecoveryAction(instance, current, old, desired)
		result.RecoveryAction = j.RecoveryAction
		if current != nil {
			result.CurrentPodID = current.RuntimePodID
			result.CurrentSlot = fmt.Sprintf("port-%d", current.GatewayPort)
		}
	}
	if j.Owner != nil && j.RecoveryAction != "preserve_current" && source.AgentEndpoint != nil {
		assignment, e := c.assignments.FaultEvictionSource(ctx, *source.AgentEndpoint, j.Owner.GatewayID)
		assignedSlot, slotErr := containerSlotName(pod, assignmentRequest(assignment))
		if e == nil && slotErr == nil && assignedSlot == j.Slot && assignment.PodUID == j.PodUID && assignment.InstanceID == j.Owner.InstanceID && assignment.UserID == j.Owner.UserID && assignment.Generation == j.Owner.Generation {
			j.Assignment = assignment
		}
	}
	// The helper needs mounts and physical identity, never the runtime's env,
	// command line or Kubernetes managedFields in this persistent journal.
	j.SourcePod.ManagedFields = nil
	j.SourcePod.Annotations = nil
	j.SourcePod.Spec.InitContainers = nil
	for index := range j.SourcePod.Spec.Containers {
		container := &j.SourcePod.Spec.Containers[index]
		var routing []corev1.EnvVar
		for _, env := range container.Env {
			if env.Name == "RUNTIME_GATEWAY_PORT_START" || env.Name == "RUNTIME_WORKSPACE_ROOT" && env.ValueFrom == nil && env.Value == s.scheduler.workspaceRoot {
				routing = append(routing, env)
			}
		}
		container.Env = routing
		container.EnvFrom = nil
		container.Command = nil
		container.Args = nil
	}
	var stableBinding *models.InstanceRuntimeBinding
	if j.SourceBinding != nil {
		copy := *j.SourceBinding
		copy.CreatedAt = time.Time{}
		copy.UpdatedAt = time.Time{}
		copy.LastHealthAt = nil
		copy.GatewayPID = nil
		copy.ErrorMessage = nil
		copy.State = ""
		stableBinding = &copy
	}
	var stableOwner *runtimeSlotAuditOwner
	if j.Owner != nil {
		copy := *j.Owner
		copy.State = ""
		stableOwner = &copy
	}
	encoded, _ := json.Marshal(struct {
		Scope   runtimeeviction.SlotScope
		Digest  string
		Owner   *runtimeSlotAuditOwner
		Binding *models.InstanceRuntimeBinding
	}{scope, j.AssignmentDigest, stableOwner, stableBinding})
	digest := sha256.Sum256(encoded)
	result.ScopeFingerprint = hex.EncodeToString(digest[:])
	j.ScopeFingerprint = result.ScopeFingerprint
	if req.ScopeFingerprint != "" && req.ScopeFingerprint != result.ScopeFingerprint {
		return nil, errors.New("slot ownership changed after preflight")
	}
	return result, nil
}

func assignmentRequest(a *k8sservice.RuntimeFaultAssignment) []byte {
	if a == nil {
		return nil
	}
	return a.Request
}

func (s *RuntimeManagementService) slotResetDesiredPower(ctx context.Context, tx db.Session, id int, i *models.Instance) (string, error) {
	var desired string
	row, err := tx.SQL().QueryRowContext(ctx, "SELECT desired_power_state FROM instance_desired_state WHERE instance_id=?", id)
	if err != nil {
		return "", err
	}
	err = row.Scan(&desired)
	if errors.Is(err, sql.ErrNoRows) {
		if i != nil && (i.Status == "running" || i.Status == "creating" || i.Status == "error") {
			return "running", nil
		}
		return "stopped", nil
	}
	return desired, err
}

func osRuntimeSlotHelperImage() string { return k8sservice.RuntimePhysicalResetHelperImage() }

func (s *RuntimeManagementService) SlotResetOperation(ctx context.Context, requestID string) (*RuntimeManagementOperation, error) {
	var id string
	row, err := s.sess.SQL().QueryRowContext(ctx, "SELECT operation_id FROM runtime_slot_reset_requests WHERE request_id=?", requestID)
	if err != nil {
		return nil, err
	}
	if err = row.Scan(&id); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var op RuntimeManagementOperation
	var raw string
	row, err = s.sess.SQL().QueryRowContext(ctx, "SELECT id,kind,status,items_json,created_at,updated_at,retry_requested FROM runtime_management_operations WHERE id=?", id)
	if err != nil {
		return nil, err
	}
	if err = row.Scan(&op.ID, &op.Kind, &op.Status, &raw, &op.CreatedAt, &op.UpdatedAt, &op.RetryRequested); err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(raw), &op.Items); err != nil {
		return nil, err
	}
	s.refreshSlotResetProjection(ctx, &op)
	public := publicSlotResetOperation(op)
	return &public, nil
}

// Call both admission checks under the shared management lock. A native
// recovery keeps authority over its original slot until that item is released.
func checkRecoveryEvictionBeforeReset(ctx context.Context, tx db.Session, podUID, slot string) error {
	row, err := tx.SQL().QueryRowContext(ctx, `SELECT o.id
		FROM runtime_management_operations o JOIN JSON_TABLE(o.items_json, '$[*]' COLUMNS (
			pod_uid VARCHAR(64) PATH '$.recovery.scope.pod_uid',
			slot_name VARCHAR(64) PATH '$.recovery.scope.container_name',
			phase VARCHAR(64) PATH '$.recovery.phase'
		)) AS item
		WHERE o.kind='eviction' AND o.status IN ('pending','processing','starting','needs_attention')
		  AND BINARY item.pod_uid=BINARY ? AND BINARY item.slot_name=BINARY ?
		  AND COALESCE(item.phase,'')<>'released' LIMIT 1`, podUID, slot)
	if err != nil {
		return err
	}
	var operationID string
	if err := row.Scan(&operationID); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("slot %s is the source of active Runtime eviction %s", slot, operationID)
}

func checkResetBeforeRecoveryEviction(ctx context.Context, tx db.Session, items []RuntimeSchedulingItem) error {
	for _, item := range items {
		if item.Recovery == nil {
			continue
		}
		scope := item.Recovery.Scope
		var blocked bool
		row, err := tx.SQL().QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM runtime_slot_reset_requests WHERE pod_uid=? AND slot=? AND active=1)", scope.PodUID, scope.ContainerName)
		if err != nil {
			return err
		}
		if err := row.Scan(&blocked); err != nil {
			return err
		}
		if blocked {
			return fmt.Errorf("%w: Pod %s slot %s", repository.ErrRuntimeSlotResetFenced, scope.PodUID, scope.ContainerName)
		}
	}
	return nil
}

func (s *RuntimeManagementService) ResetSlot(ctx context.Context, podID int64, slot string, req RuntimeSlotResetRequest, actor *int) (*RuntimeManagementOperation, error) {
	if len(req.RequestID) < 16 || len(req.RequestID) > 80 || req.SourcePodUID == "" || req.SourceContainerID == "" || len(req.ScopeFingerprint) != 64 {
		return nil, errors.New("fixed reset request and completed preflight required")
	}
	if previous, err := s.SlotResetOperation(ctx, req.RequestID); err != nil {
		return nil, errors.Join(ErrRuntimeSlotResetCommitUncertain, err)
	} else if previous != nil {
		if !sameSlotResetRequest(previous, podID, slot, req) {
			return nil, errors.New("reset request ID already belongs to another source")
		}
		return previous, nil
	}
	check, err := s.PreflightSlotReset(ctx, podID, slot, req)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	operation := &RuntimeManagementOperation{ID: "runtime_mgmt_" + uuid.NewString(), Kind: "slot_reset", Status: "pending", CreatedAt: now, UpdatedAt: now}
	j := check.journal
	j.Scope.OperationID = operation.ID
	var sourceGeneration int
	var sourceGateway string
	// A SQL reservation identifies the logical placement, but only the worker
	// can authenticate the retired execution for late-report admission.
	if j.Owner != nil {
		sourceGeneration = j.Owner.Generation
		sourceGateway = j.Owner.GatewayID
	}
	operation.Items = []RuntimeSchedulingItem{{InstanceID: j.InstanceID, SourcePodID: podID, SourcePodUID: j.PodUID, SourceGeneration: sourceGeneration, SourceGatewayID: sourceGateway, State: "pending", SlotReset: j}}
	rawRequest, _ := json.Marshal(req)
	rawItems, _ := json.Marshal(operation.Items)
	callbackCompleted := false
	err = s.sess.TxContext(ctx, func(tx db.Session) error {
		if e := repository.LockRuntimeManagementAdmission(ctx, tx); e != nil {
			return e
		}
		if e := repository.CheckRuntimePreparationReservationsForType(ctx, tx, "", j.Source.RuntimeType); e != nil {
			return e
		}
		if e := repository.CheckRuntimeRolloutActivityForType(ctx, tx, j.Source.RuntimeType); e != nil {
			return e
		}
		if e := repository.CheckRuntimePodBindingAdmission(ctx, tx, j.Source.ID); e != nil {
			return e
		}
		if e := checkRecoveryEvictionBeforeReset(ctx, tx, j.PodUID, j.Slot); e != nil {
			return e
		}
		var current models.InstanceRuntimeBinding
		row, e := tx.SQL().QueryRowContext(ctx, "SELECT id,instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,generation FROM instance_runtime_bindings WHERE runtime_pod_id=? AND gateway_port=? FOR UPDATE", j.Source.ID, j.GatewayPort)
		if e != nil {
			return e
		}
		e = row.Scan(&current.ID, &current.InstanceID, &current.RuntimePodID, &current.RuntimeType, &current.GatewayID, &current.GatewayPort, &current.WorkspacePath, &current.Generation)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if e == nil && (j.SourceBinding == nil || !sameSlotResetBinding(current, *j.SourceBinding)) {
			return errors.New("slot SQL ownership changed after preflight; inspect the new occupant")
		}
		_, e = tx.SQL().ExecContext(ctx, "INSERT INTO runtime_management_operations(id,kind,status,actor_user_id,request_json,items_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)", operation.ID, operation.Kind, operation.Status, actor, string(rawRequest), string(rawItems), now, now)
		if e != nil {
			return e
		}
		_, e = tx.SQL().ExecContext(ctx, "INSERT INTO runtime_slot_reset_requests(request_id,operation_id,pod_uid,slot,source_pod_id,gateway_port,instance_id,source_generation,source_gateway_id,active,created_at) VALUES(?,?,?,?,?,?,?,?,?,1,?)", req.RequestID, operation.ID, j.PodUID, j.Slot, j.Source.ID, j.GatewayPort, j.InstanceID, sourceGeneration, sourceGateway, now)
		callbackCompleted = e == nil
		return e
	}, nil)
	if err != nil {
		if existing, readErr := s.SlotResetOperation(ctx, req.RequestID); readErr == nil && existing != nil {
			if !sameSlotResetRequest(existing, podID, slot, req) {
				return nil, errors.New("reset request ID already belongs to another source")
			}
			return existing, nil
		}
		if !callbackCompleted {
			return nil, fmt.Errorf("slot reset request rejected before commit: %w", err)
		}
		return nil, errors.Join(ErrRuntimeSlotResetCommitUncertain, err)
	}
	public := publicSlotResetOperation(*operation)
	return &public, nil
}

func sameSlotResetRequest(op *RuntimeManagementOperation, podID int64, slot string, req RuntimeSlotResetRequest) bool {
	if op == nil || len(op.Items) != 1 || op.Items[0].SlotReset == nil {
		return false
	}
	j := op.Items[0].SlotReset
	return op.Items[0].SourcePodID == podID && j.PodUID == req.SourcePodUID && j.Slot == slot && j.ContainerID == req.SourceContainerID && j.ScopeFingerprint == req.ScopeFingerprint
}

// The scheduler consumes this persistent fence before choosing a port. It is
// physical and therefore also covers sources with no discoverable instance.
func (s *RuntimeScheduler) slotResetReservedPorts(ctx context.Context, pod models.RuntimePod) (map[int]struct{}, error) {
	ports := map[int]struct{}{}
	if s.management == nil || s.management.sess == nil || stringValue(pod.PodUID) == "" {
		return ports, nil
	}
	c, ok := s.agentClient.(*slotRuntimeAgentClient)
	if !ok {
		return ports, nil
	}
	rows, err := s.management.sess.SQL().QueryContext(ctx, "SELECT slot FROM runtime_slot_reset_requests WHERE pod_uid=? AND active=1", *pod.PodUID)
	if err != nil {
		return nil, err
	}
	var names []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			rows.Close()
			return nil, err
		}
		names = append(names, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return ports, nil
	}
	physical, err := c.kube.CoreV1().Pods(pod.Namespace).Get(ctx, pod.PodName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if string(physical.UID) != *pod.PodUID {
		return nil, errors.New("slot fence Pod identity changed")
	}
	layout, err := runtimeAuditPhysicalSlots(physical, pod.Capacity)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		for _, slot := range layout {
			if slot.Name == name {
				ports[slot.Port] = struct{}{}
			}
		}
	}
	return ports, nil
}
