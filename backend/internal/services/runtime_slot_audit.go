package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"clawreef/internal/heartbeat"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const runtimeSlotAuditInterval = 60 * time.Second

type runtimeSlotAuditOwner struct {
	PodUID      string `json:"pod_uid"`
	WorkerEpoch string `json:"worker_epoch"`
	GatewayID   string `json:"gateway_id"`
	InstanceID  int    `json:"instance_id"`
	UserID      int    `json:"user_id"`
	Generation  int    `json:"generation"`
	State       string `json:"state"`
}
type runtimeSlotAuditSnapshot struct {
	PodUID                 string                 `json:"pod_uid"`
	WorkerEpoch            string                 `json:"worker_epoch"`
	IdleConfirmed          *bool                  `json:"idle_confirmed"`
	Quarantined            *bool                  `json:"quarantined"`
	MaintenanceOperationID string                 `json:"maintenance_operation_id"`
	Assignment             *runtimeSlotAuditOwner `json:"assignment"`
	Gateways               []struct {
		GatewayID string `json:"gateway_id"`
		State     string `json:"state"`
	} `json:"gateways"`
}
type runtimeSlotAuditObservation struct {
	Name       string
	Port       int
	Block      int
	Snapshot   runtimeSlotAuditSnapshot
	Err        error
	ReadOK     bool
	Diagnostic string
}

func (o runtimeSlotAuditObservation) free() bool {
	s := o.Snapshot
	if o.Err != nil || s.IdleConfirmed == nil || !*s.IdleConfirmed || s.Quarantined == nil || *s.Quarantined || s.MaintenanceOperationID != "" || s.Assignment != nil && s.Assignment.State != "stopped" {
		return false
	}
	for _, g := range s.Gateways {
		if g.State != "stopped" {
			return false
		}
	}
	return true
}

// Physical inventory comes from the current Pod spec, never a historical SQL
// port range. Missing/ambiguous inventory grants no allocation authority.
func runtimeAuditRoutingEnv(container corev1.Container, names ...string) (map[string]string, error) {
	values := map[string]string{}
	wanted := map[string]bool{}
	for _, name := range names {
		wanted[name] = true
	}
	for _, env := range container.Env {
		if !wanted[env.Name] {
			continue
		}
		if _, seen := values[env.Name]; seen || env.ValueFrom != nil {
			return nil, fmt.Errorf("ambiguous routing environment in %s", container.Name)
		}
		values[env.Name] = env.Value
	}
	if len(values) != len(wanted) {
		return nil, fmt.Errorf("incomplete routing environment in %s", container.Name)
	}
	return values, nil
}
func runtimeAuditPhysicalSlots(pod *corev1.Pod, capacity int) ([]runtimeSlotAuditObservation, error) {
	if pod == nil || capacity < 1 || capacity > 256 {
		return nil, fmt.Errorf("invalid physical slot capacity")
	}
	var coordinator *corev1.Container
	for i := range pod.Spec.Containers {
		container := &pod.Spec.Containers[i]
		hasRole := false
		for _, env := range container.Env {
			if env.Name == "RUNTIME_AGENT_ROLE" {
				hasRole = true
			}
		}
		if !hasRole {
			continue
		}
		role, err := runtimeAuditRoutingEnv(*container, "RUNTIME_AGENT_ROLE")
		if err != nil {
			return nil, err
		}
		if role["RUNTIME_AGENT_ROLE"] == "pool" {
			if coordinator != nil {
				return nil, fmt.Errorf("ambiguous slot coordinator")
			}
			coordinator = container
		}
		if role["RUNTIME_AGENT_ROLE"] == "slot" && !strings.HasPrefix(container.Name, "slot-") {
			return nil, fmt.Errorf("worker container has no physical slot identity")
		}
	}
	if coordinator == nil {
		return nil, fmt.Errorf("slot coordinator is missing")
	}
	route, err := runtimeAuditRoutingEnv(*coordinator, "RUNTIME_GATEWAY_PORT_START", "RUNTIME_GATEWAY_PORT_END", "RUNTIME_GATEWAY_PORT_BLOCK_SIZE", "RUNTIME_SLOT_COUNT", "RUNTIME_SLOT_SOCKET_DIR")
	if err != nil {
		return nil, err
	}
	start, startErr := strconv.Atoi(route["RUNTIME_GATEWAY_PORT_START"])
	end, endErr := strconv.Atoi(route["RUNTIME_GATEWAY_PORT_END"])
	block, blockErr := strconv.Atoi(route["RUNTIME_GATEWAY_PORT_BLOCK_SIZE"])
	if startErr != nil || endErr != nil || blockErr != nil || start < 1 || end > 65535 || block < 1 || block > 65535/capacity || start > 65535-capacity*block+1 || end < start+capacity*block-1 || route["RUNTIME_SLOT_COUNT"] != strconv.Itoa(capacity) || route["RUNTIME_SLOT_SOCKET_DIR"] != "/run/clawmanager-slots" {
		return nil, fmt.Errorf("coordinator slot routing is invalid")
	}
	var slots []runtimeSlotAuditObservation
	names := map[string]bool{}
	for _, container := range pod.Spec.Containers {
		if !strings.HasPrefix(container.Name, "slot-") {
			continue
		}
		index, indexErr := strconv.Atoi(strings.TrimPrefix(container.Name, "slot-"))
		if indexErr != nil || index < 0 || index >= capacity || names[container.Name] || container.Name != fmt.Sprintf("slot-%d", index) {
			return nil, fmt.Errorf("invalid physical slot identity")
		}
		names[container.Name] = true
		worker, err := runtimeAuditRoutingEnv(container, "RUNTIME_AGENT_ROLE", "RUNTIME_SLOT_INDEX", "RUNTIME_SLOT_SOCKET", "RUNTIME_GATEWAY_PORT_START", "RUNTIME_GATEWAY_PORT_END", "RUNTIME_GATEWAY_PORT_BLOCK_SIZE")
		if err != nil {
			return nil, err
		}
		port := start + index*block
		if worker["RUNTIME_AGENT_ROLE"] != "slot" || worker["RUNTIME_SLOT_INDEX"] != strconv.Itoa(index) || worker["RUNTIME_SLOT_SOCKET"] != fmt.Sprintf("%s/slot-%d.sock", route["RUNTIME_SLOT_SOCKET_DIR"], index) || worker["RUNTIME_GATEWAY_PORT_START"] != strconv.Itoa(port) || worker["RUNTIME_GATEWAY_PORT_END"] != strconv.Itoa(port+block-1) || worker["RUNTIME_GATEWAY_PORT_BLOCK_SIZE"] != strconv.Itoa(block) {
			return nil, fmt.Errorf("worker routing differs from the coordinator in %s", container.Name)
		}
		slots = append(slots, runtimeSlotAuditObservation{Name: container.Name, Port: port, Block: block})
	}
	if len(slots) != capacity {
		return nil, fmt.Errorf("physical slot capacity does not match registered Pod")
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i].Port < slots[j].Port })
	return slots, nil
}

// This set describes addressable physical blocks, not a cached free grant.
func (c *slotRuntimeAgentClient) physicalGatewayPorts(ctx context.Context, pod models.RuntimePod) (map[int]struct{}, error) {
	if pod.AgentEndpoint == nil || pod.PodUID == nil || *pod.PodUID == "" {
		return nil, fmt.Errorf("registered slot target identity is required")
	}
	current, err := c.containerStopPod(ctx, *pod.AgentEndpoint, *pod.PodUID)
	if err != nil {
		return nil, err
	}
	slots, err := runtimeAuditPhysicalSlots(current, pod.Capacity)
	if err != nil {
		return nil, err
	}
	ports := make(map[int]struct{}, len(slots))
	for _, slot := range slots {
		if slot.Block != RuntimeGatewayPortBlockSize(pod.RuntimeType) {
			return nil, fmt.Errorf("registered Runtime type differs from physical slot block size")
		}
		ports[slot.Port] = struct{}{}
	}
	return ports, nil
}

func (c *slotRuntimeAgentClient) observeRuntimeSlots(ctx context.Context, pod models.RuntimePod) ([]runtimeSlotAuditObservation, error) {
	observations, err, _ := c.observeRuntimeSlotsForAudit(ctx, pod)
	return observations, err
}

// Source verification is separate from protocol proof: even a failed/legacy
// response can explain inventory, but only a stable current Pod UID may persist it.
func (c *slotRuntimeAgentClient) observeRuntimeSlotsForAudit(ctx context.Context, pod models.RuntimePod) ([]runtimeSlotAuditObservation, error, bool) {
	if pod.AgentEndpoint == nil || pod.PodUID == nil || *pod.PodUID == "" {
		return nil, fmt.Errorf("registered slot target identity is required"), false
	}
	current, err := c.containerStopPod(ctx, *pod.AgentEndpoint, *pod.PodUID)
	if err != nil {
		return nil, err, false
	}
	slots, err := runtimeAuditPhysicalSlots(current, pod.Capacity)
	if err == nil {
		for i := range slots {
			if slots[i].Block != RuntimeGatewayPortBlockSize(pod.RuntimeType) {
				err = fmt.Errorf("registered Runtime type differs from physical slot block size")
				break
			}
		}
		if err == nil {
			for i := range slots {
				c.observeRuntimeSlot(ctx, pod, &slots[i])
			}
		}
	}

	// An IP-reused replacement must not turn an old observation into admission.
	after, sourceErr := c.containerStopPod(ctx, *pod.AgentEndpoint, *pod.PodUID)
	if sourceErr != nil || after.UID != current.UID {
		return nil, fmt.Errorf("runtime slot Pod identity changed during inspection"), false
	}
	if err != nil {
		return nil, err, true
	}
	return slots, nil, true
}

func (c *slotRuntimeAgentClient) observeRuntimeSlot(ctx context.Context, pod models.RuntimePod, o *runtimeSlotAuditObservation) {
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	o.Err = c.do(requestCtx, http.MethodGet, *pod.AgentEndpoint, "/v1/slots/"+o.Name+"/state", nil, &o.Snapshot)
	o.ReadOK = o.Err == nil
	if o.Err != nil {
		o.Diagnostic = "runtime slot inspection unavailable"
		return
	}
	s := o.Snapshot
	if o.Err == nil && (s.PodUID != *pod.PodUID || !stopTokenValid(s.WorkerEpoch) || s.IdleConfirmed == nil || s.Quarantined == nil || s.Gateways == nil) {
		o.Err = fmt.Errorf("runtime slot exit proof is unavailable or belongs to another Pod")
		o.Diagnostic = "runtime slot exit proof unavailable or Pod identity differs"
	}
	if a := s.Assignment; a != nil && (a.PodUID != *pod.PodUID || a.InstanceID < 1 || a.UserID < 1 || a.Generation < 1 || a.GatewayID != fmt.Sprintf("gw-%d-%d", a.InstanceID, a.Generation) || !stopTokenValid(a.WorkerEpoch) || a.WorkerEpoch != s.WorkerEpoch) {
		o.Err = fmt.Errorf("runtime slot owner identity is invalid")
		o.Diagnostic = "runtime slot owner identity invalid"
	}
	if runtimeSlotGatewayOwnershipAmbiguous(s) {
		o.Err = fmt.Errorf("runtime slot gateway ownership is ambiguous")
		o.Diagnostic = "runtime slot gateway ownership ambiguous"
	}
}

func runtimeSlotGatewayOwnershipAmbiguous(s runtimeSlotAuditSnapshot) bool {
	active := 0
	for _, gateway := range s.Gateways {
		if gateway.State == "stopped" {
			continue
		}
		active++
		if s.Assignment != nil && gateway.GatewayID != "" && gateway.GatewayID != s.Assignment.GatewayID {
			return true
		}
	}
	return active > 1
}

func runtimeSlotReportedState(state string) string {
	switch state {
	case "creating", "pending", "starting", "running", "ready", "stopping", "draining", "stopped", "error", "failed", "idle", "unknown":
		return state
	default:
		return "unknown"
	}
}

func runtimeSlotReportedOwner(slot *models.RuntimeSlotObservation, owner *runtimeSlotAuditOwner) {
	if owner == nil {
		return
	}
	if owner.InstanceID > 0 {
		slot.InstanceID = owner.InstanceID
	}
	if owner.UserID > 0 {
		slot.UserID = owner.UserID
	}
	if owner.Generation > 0 {
		slot.Generation = owner.Generation
	}
	var instanceID, generation int
	if _, err := fmt.Sscanf(owner.GatewayID, "gw-%d-%d", &instanceID, &generation); err == nil && instanceID > 0 && generation > 0 && owner.GatewayID == fmt.Sprintf("gw-%d-%d", instanceID, generation) {
		slot.GatewayID = owner.GatewayID
	}
	slot.InstanceState = runtimeSlotReportedState(owner.State)
}

func runtimeSlotInventoryObservation(o runtimeSlotAuditObservation, uid string) models.RuntimeSlotObservation {
	slot := models.RuntimeSlotObservation{ContainerName: o.Name, GatewayPort: o.Port, State: "unknown", Error: o.Diagnostic}
	snapshot := o.Snapshot
	reportedUID := snapshot.PodUID
	// Legacy responses scope only the assignment. This fallback is display-only:
	// the missing top-level proof still leaves o.Err nonnil and free() false.
	if reportedUID == "" && snapshot.Assignment != nil {
		reportedUID = snapshot.Assignment.PodUID
	}
	if !o.ReadOK || reportedUID != uid || snapshot.Assignment != nil && snapshot.Assignment.PodUID != uid {
		if slot.Error == "" {
			slot.Error = "runtime slot inspection unavailable"
		}
		return slot
	}
	if o.free() {
		slot.State = "free"
		return slot
	}
	runtimeSlotReportedOwner(&slot, snapshot.Assignment)
	if owner := snapshot.Assignment; owner != nil {
		if owner.State != "stopped" {
			slot.State = "occupied"
		}
	}
	for _, gateway := range snapshot.Gateways {
		if gateway.State != "stopped" {
			slot.State = "occupied"
		}
		if snapshot.Assignment != nil && gateway.GatewayID != "" && gateway.GatewayID == snapshot.Assignment.GatewayID {
			slot.GatewayState = runtimeSlotReportedState(gateway.State)
		} else if len(snapshot.Gateways) == 1 {
			slot.GatewayState = runtimeSlotReportedState(gateway.State)
		}
	}
	if runtimeSlotGatewayOwnershipAmbiguous(snapshot) {
		slot.State, slot.GatewayState = "unknown", "unknown"
	}
	slot.IdentityVerified = o.Err == nil && slot.State == "occupied" && snapshot.Assignment != nil
	if slot.State == "unknown" && slot.Error == "" {
		slot.Error = "runtime slot cleanup incomplete without confirmed exit proof"
	}
	return slot
}

func runtimeSlotAuditName(uid string) string {
	sum := sha256.Sum256([]byte(uid))
	return "runtime-slot-audit-" + hex.EncodeToString(sum[:16])
}

type runtimeSlotAuditRecord struct {
	PodUID      string         `json:"pod_uid"`
	Endpoint    string         `json:"endpoint"`
	RuntimeType string         `json:"runtime_type"`
	PodError    string         `json:"pod_error,omitempty"`
	Ports       map[int]string `json:"ports"`
	InspectedAt time.Time      `json:"inspected_at"`
}

func (c *slotRuntimeAgentClient) readSlotAudit(ctx context.Context, pod models.RuntimePod) (*corev1.ConfigMap, runtimeSlotAuditRecord, error) {
	var record runtimeSlotAuditRecord
	if pod.PodUID == nil || pod.AgentEndpoint == nil {
		return nil, record, fmt.Errorf("audit target identity is missing")
	}
	cm, err := c.kube.CoreV1().ConfigMaps(c.namespace).Get(ctx, runtimeSlotAuditName(*pod.PodUID), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, record, nil
	}
	if err != nil {
		return nil, record, err
	}
	if cm.DeletionTimestamp != nil || cm.Labels["clawmanager.io/purpose"] != "runtime-slot-audit" || json.Unmarshal([]byte(cm.Data["record"]), &record) != nil || record.PodUID != *pod.PodUID || record.Endpoint != *pod.AgentEndpoint || record.RuntimeType != pod.RuntimeType || record.Ports == nil {
		return nil, record, fmt.Errorf("persisted runtime slot quarantine identity is invalid")
	}
	return cm, record, nil
}
func (c *slotRuntimeAgentClient) writeSlotAudit(ctx context.Context, pod models.RuntimePod, prior *corev1.ConfigMap, record runtimeSlotAuditRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if prior == nil {
		_, err = c.kube.CoreV1().ConfigMaps(c.namespace).Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: runtimeSlotAuditName(*pod.PodUID), Labels: map[string]string{"clawmanager.io/purpose": "runtime-slot-audit"}}, Data: map[string]string{"record": string(data)}}, metav1.CreateOptions{})
	} else {
		cm := prior.DeepCopy()
		cm.Data["record"] = string(data)
		_, err = c.kube.CoreV1().ConfigMaps(c.namespace).Update(ctx, cm, metav1.UpdateOptions{})
	}
	// Do not retry stale scan results after a resourceVersion conflict. A newer
	// quarantine wins and the next pass must gather fresh evidence before clear.
	return err
}

// runtimeSlotAuditor is scheduler-owned. Zero value is ready; each elected pass
// inspects at most 32 Pods with at most eight requests in flight. Free snapshots
// are never cached as permission to place another writer.
type runtimeSlotAuditor struct {
	mu      sync.Mutex
	lastRun time.Time
	cursor  int64
}

func (a *runtimeSlotAuditor) Reconcile(ctx context.Context, s *RuntimeScheduler) error {
	if s == nil || s.podRepo == nil || s.bindingRepo == nil || s.instanceRepo == nil {
		return nil
	}
	c, ok := s.agentClient.(*slotRuntimeAgentClient)
	if !ok {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	if !a.lastRun.IsZero() && now.Sub(a.lastRun) < runtimeSlotAuditInterval {
		return nil
	}
	a.lastRun = now
	passCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	pods, err := s.podRepo.List(passCtx, "")
	if err != nil {
		return err
	}
	// Historical, paused and stale reports cannot receive ordinary placements.
	// Leave their durable quarantine untouched and spend this pass's bounded
	// requests on current candidates. Allocation still inspects the exact live UID.
	timeout := s.heartbeatTimeout
	if timeout <= 0 {
		timeout = heartbeat.Timeout
	}
	eligible := make([]models.RuntimePod, 0, len(pods))
	for _, pod := range pods {
		if pod.State != "ready" || pod.Draining || pod.SchedulingPaused || !runtimePodEligibleForOrdinaryScheduling(pod) || pod.Capacity < 1 || pod.PodUID == nil || strings.TrimSpace(*pod.PodUID) == "" || pod.AgentEndpoint == nil || strings.TrimSpace(*pod.AgentEndpoint) == "" || pod.LastSeenAt == nil || now.Sub(*pod.LastSeenAt) > 3*timeout || pod.LastSeenAt.After(now.Add(time.Minute)) {
			continue
		}
		eligible = append(eligible, pod)
	}
	pods = eligible
	sort.Slice(pods, func(i, j int) bool { return pods[i].ID < pods[j].ID })
	if len(pods) == 0 {
		return nil
	}
	start := sort.Search(len(pods), func(i int) bool { return pods[i].ID > a.cursor })
	if start == len(pods) {
		start = 0
	}
	jobs := make(chan models.RuntimePod)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failures []error
	for i := 0; i < min(8, len(pods)); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for pod := range jobs {
				podCtx, done := context.WithTimeout(passCtx, 8*time.Second)
				err := s.auditRuntimePodSlots(podCtx, c, pod)
				done()
				if err != nil {
					mu.Lock()
					failures = append(failures, fmt.Errorf("runtime Pod %d slot audit: %w", pod.ID, err))
					mu.Unlock()
				}
			}
		}()
	}
dispatch:
	for i := 0; i < min(32, len(pods)); i++ {
		p := pods[(start+i)%len(pods)]
		select {
		case jobs <- p:
			a.cursor = p.ID
		case <-passCtx.Done():
			break dispatch
		}
	}
	close(jobs)
	wg.Wait()
	return errors.Join(append(failures, passCtx.Err())...)
}

// Match the assignment reservations used by admission. A Runtime exit proof
// cannot release a persisted writer reservation or repair its identity.
func (c *slotRuntimeAgentClient) runtimeAuditReservedPorts(ctx context.Context, pod models.RuntimePod) (map[int]struct{}, error) {
	assignments, err := c.assignments.ReservedAssignments(ctx, *pod.AgentEndpoint, *pod.PodUID, pod.RuntimeType)
	if err != nil {
		return nil, err
	}
	ports := make(map[int]struct{}, len(assignments))
	for _, assignment := range assignments {
		var req RuntimeAgentCreateGatewayRequest
		if json.Unmarshal(assignment.Request, &req) != nil || req.InstanceID != assignment.InstanceID || req.UserID != assignment.UserID || req.Generation != assignment.Generation || req.PodUID != *pod.PodUID || req.AgentType != assignment.RuntimeType || req.GatewayPort < 1 || req.GatewayPort > 65535 {
			return nil, fmt.Errorf("persisted slot request does not match assignment identity")
		}
		ports[req.GatewayPort] = struct{}{}
	}
	return ports, nil
}

func (s *RuntimeScheduler) auditRuntimePodSlots(ctx context.Context, c *slotRuntimeAgentClient, pod models.RuntimePod) error {
	if pod.PodUID == nil || pod.AgentEndpoint == nil || *pod.PodUID == "" || *pod.AgentEndpoint == "" {
		return fmt.Errorf("registered runtime Pod identity is missing")
	}
	record := runtimeSlotAuditRecord{PodUID: *pod.PodUID, Endpoint: *pod.AgentEndpoint, RuntimeType: pod.RuntimeType, Ports: map[int]string{}, InspectedAt: time.Now().UTC()}
	prior, quarantine, priorErr := c.readSlotAudit(ctx, pod)
	// Preserve exclusions for ports without fresh evidence. Admission also keeps
	// a pod-wide quarantine when any physical slot remains uninspectable.
	for port, reason := range quarantine.Ports {
		record.Ports[port] = reason
	}
	record.PodError = quarantine.PodError
	observations, inspectErr, sourceVerified := c.observeRuntimeSlotsForAudit(ctx, pod)
	complete := inspectErr == nil
	for _, o := range observations {
		if o.Err != nil {
			complete = false
		}
	}
	if complete {
		record.PodError = ""
	}
	bindings, bindingsErr := s.bindingRepo.ListByRuntimePodID(ctx, pod.ID)
	reserved, reservationsErr := c.runtimeAuditReservedPorts(ctx, pod)
	if priorErr != nil || inspectErr != nil || bindingsErr != nil || reservationsErr != nil {
		record.PodError = "runtime slot inventory or reservations unavailable"
	}
	byPort := map[int]models.InstanceRuntimeBinding{}
	for _, binding := range bindings {
		byPort[binding.GatewayPort] = binding
	}
	canCleanup := record.PodError == ""
	var cleanup []func() error
	var failures []error
	for _, o := range observations {
		if o.Err != nil {
			record.Ports[o.Port] = "runtime slot inspection unavailable"
			failures = append(failures, o.Err)
			continue
		}
		binding, bound := byPort[o.Port]
		if o.free() {
			delete(record.Ports, o.Port)
			if bound {
				record.Ports[o.Port] = "SQL binding still reserves runtime slot"
				if canCleanup {
					cleanup = append(cleanup, func() error { return s.auditFreeStoppedRuntimeSlot(ctx, c, pod, o, binding) })
				}
			} else if _, assigned := reserved[o.Port]; assigned {
				record.Ports[o.Port] = "persisted runtime assignment still reserves slot"
			}
			continue
		}
		owner := o.Snapshot.Assignment
		if owner == nil {
			record.Ports[o.Port] = "runtime cleanup incomplete without a known owner"
			continue
		}
		assignment, state, _, _, lookupErr := c.assignments.ReadContainerStop(ctx, *pod.AgentEndpoint, owner.GatewayID)
		if lookupErr != nil || assignment.InstanceID != owner.InstanceID || assignment.UserID != owner.UserID || assignment.Generation != owner.Generation || assignment.PodUID != owner.PodUID || assignment.RuntimeType != pod.RuntimeType {
			record.Ports[o.Port] = "runtime owner has no matching persisted assignment"
			continue
		}
		var req RuntimeAgentCreateGatewayRequest
		if json.Unmarshal(assignment.Request, &req) != nil || req.GatewayPort != o.Port {
			record.Ports[o.Port] = "persisted runtime owner port does not match"
			continue
		}
		if state == "running" && bound && binding.InstanceID == owner.InstanceID && binding.Generation == owner.Generation && binding.GatewayID == owner.GatewayID && !*o.Snapshot.Quarantined && o.Snapshot.MaintenanceOperationID == "" && owner.State != "stopped" {
			delete(record.Ports, o.Port)
			continue
		}
		record.Ports[o.Port] = "runtime owner remains occupied or cleanup is pending"
		if canCleanup && (state == "draining" || state == "stopped") {
			cleanup = append(cleanup, func() error { return s.auditStoppedRuntimeSlot(ctx, c, pod, o) })
		}
	}

	// Publish the same unresolved reservations as admission before attempting any
	// lifecycle cleanup. The original scan timestamp makes superseded saves fail.
	observation := models.RuntimePodObservation{PodUID: *pod.PodUID, ObservedAt: record.InspectedAt, Slots: make([]models.RuntimeSlotObservation, 0, len(observations))}
	if record.PodError != "" {
		observation.Error = "runtime slot inventory or quarantine unresolved"
	}
	for _, o := range observations {
		slot := runtimeSlotInventoryObservation(o, *pod.PodUID)
		reason := record.Ports[o.Port]
		if record.PodError != "" {
			reason = observation.Error
		} else if slot.State == "free" && reason == "" && (quarantine.PodError != "" || quarantine.Ports[o.Port] != "") {
			// The pending ConfigMap CAS may fail. A following fresh scan can
			// publish free only after it observes that this exclusion is gone.
			reason = "runtime slot quarantine release pending"
		}
		if reason != "" {
			if slot.State == "free" {
				slot.State = "unknown"
				runtimeSlotReportedOwner(&slot, o.Snapshot.Assignment)
			}
			if slot.Error == "" {
				slot.Error = reason
			}
		}
		observation.Slots = append(observation.Slots, slot)
	}
	var saveErr error
	if store, ok := s.podRepo.(repository.RuntimePodObservationStore); ok && sourceVerified {
		var saved bool
		saved, saveErr = store.SaveRuntimeObservation(ctx, pod.ID, *pod.PodUID, observation)
		if saveErr == nil && !saved {
			saveErr = fmt.Errorf("runtime slot observation was superseded or Pod identity changed")
		}
	}
	if priorErr != nil {
		return errors.Join(inspectErr, bindingsErr, reservationsErr, saveErr, priorErr)
	}
	if saveErr != nil {
		return errors.Join(append(failures, inspectErr, bindingsErr, reservationsErr, saveErr)...)
	}
	writeErr := c.writeSlotAudit(ctx, pod, prior, record)
	if inspectErr != nil || bindingsErr != nil || reservationsErr != nil || writeErr != nil {
		return errors.Join(append(failures, inspectErr, bindingsErr, reservationsErr, writeErr)...)
	}
	for _, action := range cleanup {
		if err := action(); err != nil {
			failures = append(failures, err)
		}
	}
	// Requested stops remain quarantined until a later fresh full-exit proof.
	return errors.Join(failures...)
}

// A fresh full-exit proof permits releasing an exact stopped SQL reservation.
// This never sends cancellation or reset requests to the Runtime.
func (s *RuntimeScheduler) auditFreeStoppedRuntimeSlot(ctx context.Context, c *slotRuntimeAgentClient, pod models.RuntimePod, o runtimeSlotAuditObservation, expected models.InstanceRuntimeBinding) error {
	release, err := acquireInstanceLifecycleOperation(ctx, s.instanceRepo, expected.InstanceID)
	if err != nil {
		return err
	}
	defer release()
	instance, err := s.instanceRepo.GetByID(expected.InstanceID)
	if err != nil {
		return err
	}
	if instance == nil || instance.ID != expected.InstanceID || instance.Status != "stopped" || instance.RuntimeGeneration != expected.Generation || instance.UserID < 1 {
		return nil
	}
	current, err := s.bindingRepo.GetByInstanceID(ctx, expected.InstanceID)
	if err != nil {
		return err
	}
	if current == nil || current.InstanceID != expected.InstanceID || current.RuntimePodID != pod.ID || current.Generation != expected.Generation || current.GatewayPort != o.Port || current.GatewayID != expected.GatewayID || expected.GatewayID != fmt.Sprintf("gw-%d-%d", expected.InstanceID, expected.Generation) {
		return nil
	}
	binding := *current
	if _, err = c.containerStopPod(ctx, *pod.AgentEndpoint, *pod.PodUID); err != nil {
		return err
	}
	fresh := runtimeSlotAuditObservation{Name: o.Name, Port: o.Port}
	c.observeRuntimeSlot(ctx, pod, &fresh)
	if _, err = c.containerStopPod(ctx, *pod.AgentEndpoint, *pod.PodUID); err != nil {
		return err
	}
	if !fresh.free() || fresh.Snapshot.WorkerEpoch != o.Snapshot.WorkerEpoch {
		return fresh.Err
	}
	releaser, ok := s.bindingRepo.(repository.ConfirmedStoppedSlotBindingReleaser)
	if !ok {
		return fmt.Errorf("confirmed free slot requires an exact conditional binding release")
	}
	deleted, err := releaser.DeleteConfirmedStoppedSlotBinding(ctx, *instance, binding)
	if err != nil {
		return err
	}
	if !deleted {
		return fmt.Errorf("stopped slot binding or instance changed after fresh exit proof")
	}
	return nil
}

func (s *RuntimeScheduler) auditStoppedRuntimeSlot(ctx context.Context, c *slotRuntimeAgentClient, pod models.RuntimePod, o runtimeSlotAuditObservation) error {
	owner := o.Snapshot.Assignment
	release, err := acquireInstanceLifecycleOperation(ctx, s.instanceRepo, owner.InstanceID)
	if err != nil {
		return err
	}
	defer release()
	instance, err := s.instanceRepo.GetByID(owner.InstanceID)
	if err != nil {
		return err
	}
	// User desired state is authoritative. Desired-running owners belong to the
	// recovery path; unknown/deleted owners require explicit reconciliation.
	if instance == nil || instance.Status != "stopped" || instance.RuntimeGeneration != owner.Generation || instance.UserID != owner.UserID {
		return nil
	}
	current, err := s.bindingRepo.GetByInstanceID(ctx, owner.InstanceID)
	if err != nil {
		return err
	}
	if current != nil && (current.RuntimePodID != pod.ID || current.Generation != owner.Generation || current.GatewayPort != o.Port || current.GatewayID != owner.GatewayID) {
		return nil
	}
	assignment, state, _, _, err := c.assignments.ReadContainerStop(ctx, *pod.AgentEndpoint, owner.GatewayID)
	if err != nil {
		return err
	}
	if assignment.PodUID != owner.PodUID || assignment.UserID != owner.UserID || assignment.Generation != owner.Generation || (state != "draining" && state != "stopped") {
		return nil
	}
	// Recheck after obtaining the instance lock: the observed slot may have
	// changed owners while the scan was waiting. An old stopped SQL row is no
	// authority to send cancellation/reset requests to that different owner.
	fresh := runtimeSlotAuditObservation{Name: o.Name, Port: o.Port}
	c.observeRuntimeSlot(ctx, pod, &fresh)
	if fresh.Err != nil {
		return fresh.Err
	}
	if fresh.Snapshot.WorkerEpoch != o.Snapshot.WorkerEpoch || fresh.Snapshot.Assignment == nil || *fresh.Snapshot.Assignment != *owner {
		return nil
	}
	if _, err := c.containerStopPod(ctx, *pod.AgentEndpoint, *pod.PodUID); err != nil {
		return err
	}
	// DeleteGateway itself rechecks the physical runtime owner and requires the
	// existing matching teardown/termination/reset receipts. Never synthesize any.
	if err := c.DeleteGateway(ctx, *pod.AgentEndpoint, owner.GatewayID); err != nil {
		return err
	}
	if current == nil {
		return nil
	}
	releaser, ok := s.bindingRepo.(repository.ConfirmedStoppedSlotBindingReleaser)
	if !ok {
		return fmt.Errorf("confirmed stopped slot requires an exact conditional binding release")
	}
	deleted, err := releaser.DeleteConfirmedStoppedSlotBinding(ctx, *instance, *current)
	if err != nil {
		return err
	}
	if !deleted {
		return fmt.Errorf("stopped slot binding or instance changed after confirmed teardown")
	}
	return nil
}
