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

	"clawreef/internal/models"
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
		State string `json:"state"`
	} `json:"gateways"`
}
type runtimeSlotAuditObservation struct {
	Name     string
	Port     int
	Snapshot runtimeSlotAuditSnapshot
	Err      error
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
func runtimeAuditPhysicalSlots(pod *corev1.Pod, capacity int) ([]runtimeSlotAuditObservation, error) {
	var slots []runtimeSlotAuditObservation
	ports := map[int]bool{}
	names := map[string]bool{}
	for _, container := range pod.Spec.Containers {
		if !strings.HasPrefix(container.Name, "slot-") {
			continue
		}
		index, err := strconv.Atoi(strings.TrimPrefix(container.Name, "slot-"))
		if err != nil || index < 0 || index >= capacity || index >= 256 || names[container.Name] || container.Name != fmt.Sprintf("slot-%d", index) {
			return nil, fmt.Errorf("invalid physical slot identity")
		}
		names[container.Name] = true
		port := 0
		found := false
		for _, env := range container.Env {
			if env.Name == "RUNTIME_GATEWAY_PORT_START" {
				if found || env.ValueFrom != nil {
					return nil, fmt.Errorf("ambiguous physical slot port")
				}
				found = true
				port, err = strconv.Atoi(env.Value)
				if err != nil {
					return nil, fmt.Errorf("unknown physical slot port")
				}
			}
		}
		if port < 1 || port > 65535 || ports[port] {
			return nil, fmt.Errorf("missing or duplicate physical slot port")
		}
		ports[port] = true
		slots = append(slots, runtimeSlotAuditObservation{Name: container.Name, Port: port})
	}
	if capacity < 1 || capacity > 256 || len(slots) != capacity {
		return nil, fmt.Errorf("physical slot capacity does not match registered Pod")
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i].Port < slots[j].Port })
	return slots, nil
}
func (c *slotRuntimeAgentClient) observeRuntimeSlots(ctx context.Context, pod models.RuntimePod) ([]runtimeSlotAuditObservation, error) {
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
	for i := range slots {
		c.observeRuntimeSlot(ctx, pod, &slots[i])
	}

	// An IP-reused replacement must not turn an old observation into admission.
	after, err := c.containerStopPod(ctx, *pod.AgentEndpoint, *pod.PodUID)
	if err != nil || after.UID != current.UID {
		return nil, fmt.Errorf("runtime slot Pod identity changed during inspection")
	}
	return slots, nil
}

func (c *slotRuntimeAgentClient) observeRuntimeSlot(ctx context.Context, pod models.RuntimePod, o *runtimeSlotAuditObservation) {
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	o.Err = c.do(requestCtx, http.MethodGet, *pod.AgentEndpoint, "/v1/slots/"+o.Name+"/state", nil, &o.Snapshot)
	s := o.Snapshot
	if o.Err == nil && (s.PodUID != *pod.PodUID || !stopTokenValid(s.WorkerEpoch) || s.IdleConfirmed == nil || s.Quarantined == nil || s.Gateways == nil) {
		o.Err = fmt.Errorf("runtime slot exit proof is unavailable or belongs to another Pod")
	}
	if a := s.Assignment; a != nil && (a.PodUID != *pod.PodUID || a.InstanceID < 1 || a.UserID < 1 || a.Generation < 1 || a.GatewayID != fmt.Sprintf("gw-%d-%d", a.InstanceID, a.Generation) || !stopTokenValid(a.WorkerEpoch)) {
		o.Err = fmt.Errorf("runtime slot owner identity is invalid")
	}
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
func (s *RuntimeScheduler) auditRuntimePodSlots(ctx context.Context, c *slotRuntimeAgentClient, pod models.RuntimePod) error {
	if pod.PodUID == nil || pod.AgentEndpoint == nil || *pod.PodUID == "" || *pod.AgentEndpoint == "" {
		return fmt.Errorf("registered runtime Pod identity is missing")
	}
	prior, _, err := c.readSlotAudit(ctx, pod)
	if err != nil {
		return err
	}
	record := runtimeSlotAuditRecord{PodUID: *pod.PodUID, Endpoint: *pod.AgentEndpoint, RuntimeType: pod.RuntimeType, Ports: map[int]string{}, InspectedAt: time.Now().UTC()}
	observations, inspectErr := c.observeRuntimeSlots(ctx, pod)
	bindings, bindingsErr := s.bindingRepo.ListByRuntimePodID(ctx, pod.ID)
	if inspectErr != nil || bindingsErr != nil {
		record.PodError = "runtime slot inventory unavailable"
		return errors.Join(inspectErr, bindingsErr, c.writeSlotAudit(ctx, pod, prior, record))
	}
	byPort := map[int]models.InstanceRuntimeBinding{}
	for _, b := range bindings {
		byPort[b.GatewayPort] = b
	}
	var failures []error
	for _, o := range observations {
		if o.Err != nil {
			record.Ports[o.Port] = "runtime slot inspection unavailable"
			failures = append(failures, o.Err)
			continue
		}
		b, bound := byPort[o.Port]
		if o.free() {
			if bound {
				record.Ports[o.Port] = "SQL binding still reserves runtime slot"
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
		if state == "running" && bound && b.InstanceID == owner.InstanceID && b.Generation == owner.Generation && b.GatewayID == owner.GatewayID && !*o.Snapshot.Quarantined && o.Snapshot.MaintenanceOperationID == "" && owner.State != "stopped" {
			continue
		}
		record.Ports[o.Port] = "runtime owner remains occupied or cleanup is pending"
		if state != "draining" && state != "stopped" {
			continue
		}
		if err := s.auditStoppedRuntimeSlot(ctx, c, pod, o); err != nil {
			failures = append(failures, err)
		}
	}
	// Persist all unresolved slots, including a successfully requested stop. Only
	// a later fresh full-exit proof can remove quarantine.
	return errors.Join(append(failures, c.writeSlotAudit(ctx, pod, prior, record))...)
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
	return c.DeleteGateway(ctx, *pod.AgentEndpoint, owner.GatewayID)
}
