package services

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"time"

	"clawreef/internal/runtimeeviction"
	k8sservice "clawreef/internal/services/k8s"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type runtimeFaultSlotProof struct {
	Scope              runtimeeviction.Scope    `json:"scope"`
	Receipt            *runtimeeviction.Receipt `json:"receipt"`
	WorkerEpoch        string                   `json:"worker_epoch"`
	AssignmentDigest   string                   `json:"assignment_digest"`
	CurrentWorkerEpoch string                   `json:"current_worker_epoch"`
}
type RuntimeFaultSlotRelease struct {
	LastAttemptAt time.Time              `json:"last_attempt_at,omitempty"`
	ScopeDigest   string                 `json:"scope_digest"`
	PodUID        string                 `json:"pod_uid"`
	Slot          string                 `json:"slot"`
	Proof         *runtimeFaultSlotProof `json:"proof,omitempty"`
	Reconciled    bool                   `json:"reconciled,omitempty"`
	Released      bool                   `json:"released,omitempty"`
	SourceRetired bool                   `json:"source_retired,omitempty"`
	ContainerID   string                 `json:"container_id,omitempty"`
	Error         string                 `json:"error,omitempty"`
}

func faultSlotReleaseRequired(j *RuntimeFaultEvictionJournal) bool {
	return j != nil && j.Receipt != nil && j.SourcePod != nil && j.SourcePod.Annotations[k8sservice.RuntimeFaultSandboxProfileAnnotation] == "nfs-rootless-v1"
}
func faultSlotReleaseComplete(j *RuntimeFaultEvictionJournal) bool {
	r := j.SlotRelease
	if r == nil || !r.Released || r.ScopeDigest != runtimeeviction.ScopeDigest(j.Scope) || r.PodUID != j.Scope.PodUID || r.Slot != j.Scope.ContainerName {
		return false
	}
	if r.SourceRetired {
		return true
	}
	if !r.Reconciled || r.Proof == nil || r.Proof.Scope != j.Scope || r.ContainerID == "" || r.ContainerID == j.Scope.ContainerID || r.Proof.CurrentWorkerEpoch == r.Proof.WorkerEpoch || !stopTokenValid(r.Proof.WorkerEpoch) || !stopTokenValid(r.Proof.CurrentWorkerEpoch) {
		return false
	}
	a, _ := json.Marshal(r.Proof.Receipt)
	b, _ := json.Marshal(j.Receipt)
	return string(a) == string(b)
}
func faultSlotContainer(p *corev1.Pod, slot string) (string, error) {
	for _, s := range p.Status.ContainerStatuses {
		if s.Name == slot && s.Ready && s.State.Running != nil && s.ContainerID != "" {
			return s.ContainerID, nil
		}
	}
	return "", fmt.Errorf("%w: fault slot replacement container is not ready", k8sservice.ErrRuntimeFaultEvictionPending)
}
func faultSlotOldOwner(s runtimeSlotAuditSnapshot, p runtimeFaultSlotProof) bool {
	a := s.Assignment
	return s.PodUID == p.Scope.PodUID && a != nil && a.PodUID == p.Scope.PodUID && a.GatewayID == p.Scope.GatewayID && a.InstanceID == p.Scope.InstanceID && a.UserID == p.Scope.UserID && a.Generation == p.Scope.Generation && a.WorkerEpoch == p.WorkerEpoch && s.Digest == p.AssignmentDigest
}

// This only finalizes the historical slot. It never replays stop, fences a
// successor, changes an instance generation, or opens its workspace writer.
func (c *slotRuntimeAgentClient) finalizeFaultSlot(ctx context.Context, j *RuntimeFaultEvictionJournal, checkpoint func() error) error {
	if j == nil || j.Receipt == nil || j.SourcePod == nil || j.Assignment == nil {
		return errors.New("fault source journal or receipt missing")
	}
	scope := j.Scope
	if err := runtimeeviction.ValidateReceipt(scope, j.Receipt); err != nil {
		return err
	}
	if scope.Namespace != c.namespace || j.SourceBinding.InstanceID != scope.InstanceID || j.SourceBinding.RuntimeType != scope.RuntimeType || j.SourceBinding.Generation != scope.Generation || j.SourceUserID != scope.UserID || string(j.SourcePod.UID) != scope.PodUID || j.SourcePod.Name != scope.PodName || j.SourcePod.Namespace != scope.Namespace {
		return errors.New("fault slot source scope is not pinned")
	}
	if j.SlotRelease == nil {
		j.SlotRelease = &RuntimeFaultSlotRelease{ScopeDigest: runtimeeviction.ScopeDigest(scope), PodUID: scope.PodUID, Slot: scope.ContainerName}
	}
	release := j.SlotRelease
	if release.ScopeDigest != runtimeeviction.ScopeDigest(scope) || release.PodUID != scope.PodUID || release.Slot != scope.ContainerName {
		return errors.New("fault slot release authority changed")
	}
	pod, err := c.kube.CoreV1().Pods(scope.Namespace).Get(ctx, scope.PodName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) || err == nil && string(pod.UID) != scope.PodUID {
		// The local book is Pod-scoped storage. A replacement UID cannot inherit
		// admission authority for this retired Pod or stand in as its old execution.
		release.SourceRetired = true
		release.Released = true
		return checkpoint()
	}
	if err != nil {
		return err
	}
	profile, verified := j.SourcePod.Annotations[k8sservice.RuntimeFaultSandboxProfileAnnotation]
	if !verified {
		profile = pod.Labels["clawmanager.io/sandbox-profile"]
		if j.SourcePod.Annotations == nil {
			j.SourcePod.Annotations = map[string]string{}
		}
		j.SourcePod.Annotations[k8sservice.RuntimeFaultSandboxProfileAnnotation] = profile
		if err = checkpoint(); err != nil {
			return err
		}
	}
	if pod.Labels["clawmanager.io/sandbox-profile"] != profile || pod.Spec.NodeName != scope.NodeName {
		return errors.New("fault slot source profile or node changed")
	}
	if profile == "" {
		release.Released = true
		return checkpoint()
	} // Only the verified legacy lifecycle has no retained native slot book.
	if profile != "nfs-rootless-v1" {
		return errors.New("fault slot source profile is unsupported")
	}
	if scope.ContainerID == "" {
		return errors.New("retained fault slot requires an exact stopped container")
	}
	pinned, err := c.containerStopPod(ctx, j.SourceEndpoint, scope.PodUID)
	if err != nil {
		return err
	}
	if pinned.Name != scope.PodName || pinned.UID != pod.UID {
		return errors.New("fault slot endpoint no longer names the source")
	}
	slot, err := containerSlotName(pod, j.Assignment.Request)
	if err != nil || slot != scope.ContainerName {
		return errors.New("fault slot port identity changed")
	}
	container, err := faultSlotContainer(pod, slot)
	if err != nil {
		return err
	}
	if container == scope.ContainerID {
		return fmt.Errorf("%w: old fault container still occupies slot", k8sservice.ErrRuntimeFaultEvictionPending)
	}
	var snapshot runtimeSlotAuditSnapshot
	if err = c.do(ctx, http.MethodGet, j.SourceEndpoint, "/v1/slots/"+slot+"/state", nil, &snapshot); err != nil {
		return err
	}
	if snapshot.PodUID != scope.PodUID || !stopTokenValid(snapshot.WorkerEpoch) || snapshot.Quarantined == nil || snapshot.IdleConfirmed == nil || snapshot.Gateways == nil {
		return errors.New("fault slot native inspection is unavailable")
	}
	if release.Proof == nil {
		a := snapshot.Assignment
		digest, digestErr := hex.DecodeString(snapshot.Digest)
		if a == nil || a.WorkerEpoch == snapshot.WorkerEpoch || !stopTokenValid(a.WorkerEpoch) || digestErr != nil || len(digest) != 32 {
			return errors.New("fault slot original book identity is unknown")
		}
		candidate := &runtimeFaultSlotProof{Scope: scope, Receipt: j.Receipt, WorkerEpoch: a.WorkerEpoch, AssignmentDigest: snapshot.Digest, CurrentWorkerEpoch: snapshot.WorkerEpoch}
		if !faultSlotOldOwner(snapshot, *candidate) {
			return errors.New("fault slot owner differs from journal")
		}
		release.Proof = candidate
	}
	proof := release.Proof
	a, _ := json.Marshal(proof.Receipt)
	b, _ := json.Marshal(j.Receipt)
	if proof.Scope != scope || string(a) != string(b) {
		return errors.New("fault slot saved receipt changed")
	}
	proof.CurrentWorkerEpoch = snapshot.WorkerEpoch
	release.ContainerID = container
	release.Error = ""
	if err = checkpoint(); err != nil {
		return err
	}
	call := func(action string, released bool) error {
		var ack struct {
			OperationID string `json:"operation_id"`
			ScopeDigest string `json:"scope_digest"`
			GatewayID   string `json:"gateway_id"`
			WorkerEpoch string `json:"worker_epoch"`
			Stopped     bool   `json:"stopped"`
			Confirmed   bool   `json:"confirmed"`
			Released    bool   `json:"released"`
		}
		if err := c.containerStopRequest(ctx, j.SourceEndpoint, "/v1/slots/"+slot+"/"+action, proof, &ack); err != nil {
			return err
		}
		if ack.OperationID != scope.OperationID || ack.ScopeDigest != release.ScopeDigest || ack.GatewayID != scope.GatewayID || ack.WorkerEpoch != snapshot.WorkerEpoch || !ack.Stopped || !ack.Confirmed || ack.Released != released {
			return errors.New("fault slot acknowledgement identity mismatch")
		}
		return nil
	}
	if !release.Reconciled {
		if !faultSlotOldOwner(snapshot, *proof) {
			return errors.New("fault slot owner changed before reconcile")
		}
		if err = call("reconcile-fault-stop", false); err != nil {
			return err
		}
		release.Reconciled = true
		if err = checkpoint(); err != nil {
			return err
		}
	}
	if err = call("release-fault-stop", true); err != nil {
		return err
	}
	after, err := c.containerStopPod(ctx, j.SourceEndpoint, scope.PodUID)
	if err != nil {
		return err
	}
	current, err := faultSlotContainer(after, slot)
	if err != nil || current != container {
		return errors.New("fault slot container changed during release")
	}
	var fresh runtimeSlotAuditSnapshot
	if err = c.do(ctx, http.MethodGet, j.SourceEndpoint, "/v1/slots/"+slot+"/state", nil, &fresh); err != nil {
		return err
	}
	if fresh.PodUID != scope.PodUID || fresh.WorkerEpoch != snapshot.WorkerEpoch {
		return errors.New("fault slot worker changed during release")
	}
	if (faultSlotOldOwner(fresh, *proof) || fresh.Assignment == nil) && !(runtimeSlotAuditObservation{Snapshot: fresh}).free() {
		return errors.New("fault slot release lacks fresh native free proof")
	}
	// A superseded owner is accepted only after the exact durable release ACK;
	// the worker retains its last finalized fault receipt across new admission.
	release.Released = true
	return checkpoint()
}

// One old journal per elected pass, oldest attempt first. Failed/legacy types
// retain their own quarantine and cannot prevent other Runtime work proceeding.
func (s *RuntimeManagementService) reconcileHistoricalFaultSlots(ctx context.Context) error {
	c, ok := s.scheduler.agentClient.(*slotRuntimeAgentClient)
	if !ok {
		return nil
	}
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT id,items_json FROM runtime_management_operations WHERE kind='eviction' AND status='completed' AND JSON_UNQUOTE(JSON_EXTRACT(request_json,'$.mode'))='recovery' AND COALESCE(JSON_LENGTH(JSON_EXTRACT(items_json,'$[*].recovery.receipt')),0)>COALESCE(JSON_LENGTH(JSON_EXTRACT(items_json,'$[*].recovery.slot_release.released')),0) ORDER BY updated_at,id LIMIT 1`)
	if err != nil {
		return err
	}
	var id, raw string
	if err = row.Scan(&id, &raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	var items []RuntimeSchedulingItem
	if err = json.Unmarshal([]byte(raw), &items); err != nil {
		return err
	}
	checkpoint := func() error {
		encoded, e := json.Marshal(items)
		if e != nil {
			return e
		}
		result, e := s.sess.SQL().ExecContext(ctx, `UPDATE runtime_management_operations SET items_json=?,updated_at=? WHERE id=? AND status='completed' AND BINARY items_json=BINARY ?`, string(encoded), time.Now().UTC(), id, raw)
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return errors.New("historical fault journal changed")
		}
		raw = string(encoded)
		return nil
	}
	indices := make([]int, 0, len(items))
	for i := range items {
		j := items[i].Recovery
		if j != nil && j.Receipt != nil && j.Phase == "released" && (j.SlotRelease == nil || !j.SlotRelease.Released) {
			indices = append(indices, i)
		}
	}
	sort.SliceStable(indices, func(a, b int) bool {
		x, y := items[indices[a]].Recovery.SlotRelease, items[indices[b]].Recovery.SlotRelease
		if x == nil {
			return y != nil
		}
		if y == nil {
			return false
		}
		return x.LastAttemptAt.Before(y.LastAttemptAt)
	})
	for _, i := range indices {
		j := items[i].Recovery
		if j == nil || j.Receipt == nil || j.Phase != "released" || j.SlotRelease != nil && j.SlotRelease.Released {
			continue
		}
		if j.SlotRelease == nil {
			j.SlotRelease = &RuntimeFaultSlotRelease{ScopeDigest: runtimeeviction.ScopeDigest(j.Scope), PodUID: j.Scope.PodUID, Slot: j.Scope.ContainerName}
		}
		j.SlotRelease.LastAttemptAt = time.Now().UTC()
		if err = checkpoint(); err != nil {
			return err
		}
		attempt, cancel := context.WithTimeout(ctx, 8*time.Second)
		err = c.finalizeFaultSlot(attempt, j, checkpoint)
		cancel()
		if err != nil {
			if j.SlotRelease != nil {
				j.SlotRelease.Error = err.Error()
			}
			if e := checkpoint(); e != nil {
				return e
			}
			log.Printf("Runtime historical fault slot %s/%s pending: %v", id, j.Scope.ContainerName, err)
			return nil
		}
		s.scheduler.RequestSlotAudit(items[i].SourcePodID)
		return nil
	}
	return nil
}

func resumableFaultSlotError(err error) error {
	var transport *url.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrRuntimeAgentUnavailable) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &transport) {
		return errors.Join(k8sservice.ErrRuntimeFaultEvictionPending, err)
	}
	return err
}
