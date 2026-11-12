package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/runtimeauth"
	k8sservice "clawreef/internal/services/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type runtimeSlotStopIntent = k8sservice.RuntimeContainerStopIntent

var errContainerTerminationPending = errors.New("matching old slot container termination is not confirmed")

type runtimeSlotTerminationMessage struct {
	runtimeSlotStopIntent
	CleanupConfirmed bool `json:"cleanup_confirmed"`
}

type runtimeSlotContainerProof struct {
	runtimeSlotTerminationMessage
	ContainerID     string    `json:"container_id"`
	FinishedAt      time.Time `json:"finished_at"`
	ProofSource     string    `json:"proof_source,omitempty"`
	FenceRequestUID string    `json:"fence_request_uid,omitempty"`
}

func validContainerIntent(i runtimeSlotStopIntent) bool {
	digest, err := hex.DecodeString(i.AssignmentDigest)
	index, indexErr := strconv.Atoi(strings.TrimPrefix(i.Slot, "slot-"))
	return i.Version == 1 && (i.Kind == "" || i.Kind == "maintenance") && stopTokenValid(i.OperationID) && stopTokenValid(i.WorkerEpoch) && i.PodUID != "" &&
		indexErr == nil && index >= 0 && index < 256 && i.Slot == fmt.Sprintf("slot-%d", index) && i.InstanceID > 0 && i.UserID > 0 && i.Generation > 0 &&
		i.GatewayID == fmt.Sprintf("gw-%d-%d", i.InstanceID, i.Generation) && err == nil && len(digest) == 32
}

func decodeContainerProof(data []byte, out any) error {
	if len(data) == 0 || len(data) > 4096 {
		return fmt.Errorf("missing or oversized container proof")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("trailing container proof")
	}
	return nil
}

// Only a kubelet termination object supplies the exact old container identity.
// Running.ContainerID, restartCount and new-container readiness grant no proof.
func runtimeSlotTerminationProof(pod *corev1.Pod, intent runtimeSlotStopIntent) (*runtimeSlotContainerProof, error) {
	if pod == nil || string(pod.UID) != intent.PodUID || !validContainerIntent(intent) {
		return nil, fmt.Errorf("container stop Pod or execution identity changed")
	}
	var found *runtimeSlotContainerProof
	message := runtimeSlotTerminationMessage{runtimeSlotStopIntent: intent, CleanupConfirmed: true}
	commitment, err := containerTerminationCommitment(message)
	if err != nil {
		return nil, err
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name != intent.Slot {
			continue
		}
		for _, terminated := range []*corev1.ContainerStateTerminated{status.State.Terminated, status.LastTerminationState.Terminated} {
			if terminated == nil || terminated.ContainerID == "" || terminated.FinishedAt.IsZero() {
				continue
			}
			if terminated.Message != commitment {
				continue
			}
			proof := &runtimeSlotContainerProof{runtimeSlotTerminationMessage: message, ContainerID: terminated.ContainerID, FinishedAt: terminated.FinishedAt.Time}
			if found != nil && *found != *proof {
				return nil, fmt.Errorf("ambiguous slot termination proof")
			}
			found = proof
		}
	}
	if found == nil {
		return nil, errContainerTerminationPending
	}
	return found, nil
}

func containerTerminationCommitment(message runtimeSlotTerminationMessage) (string, error) {
	data, err := json.Marshal(message)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return "C1:" + base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

// Lookup for stop/recovery deliberately does not require PodReady: a single slot
// may be restarting while other slots continue serving through the coordinator.
func (c *slotRuntimeAgentClient) containerStopPod(ctx context.Context, endpoint, expectedUID string) (*corev1.Pod, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") || strings.ContainsAny(endpoint, "?#") || strings.TrimSpace(endpoint) != endpoint {
		return nil, fmt.Errorf("invalid container stop endpoint")
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("invalid container stop endpoint port")
		}
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil {
		return nil, fmt.Errorf("container stop requires a Pod IP")
	}
	if c.kube == nil || c.namespace == "" {
		return nil, fmt.Errorf("container stop requires Kubernetes")
	}
	list, err := c.kube.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{FieldSelector: "status.podIP=" + u.Hostname(), LabelSelector: "clawmanager.io/runtime-protocol=container-slots-v1"})
	if err != nil {
		return nil, err
	}
	var found *corev1.Pod
	for index := range list.Items {
		p := &list.Items[index]
		if p.Namespace != c.namespace || !ip.Equal(net.ParseIP(p.Status.PodIP)) || p.Labels["clawmanager.io/runtime-protocol"] != "container-slots-v1" || p.DeletionTimestamp != nil || string(p.UID) == "" || expectedUID != "" && string(p.UID) != expectedUID {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("ambiguous container stop Pod")
		}
		found = p
	}
	if found == nil {
		return nil, fmt.Errorf("container stop Pod identity is unavailable")
	}
	return found, nil
}

func containerSlotName(pod *corev1.Pod, request []byte) (string, error) {
	var req struct {
		GatewayPort int `json:"gateway_port"`
	}
	if json.Unmarshal(request, &req) != nil || req.GatewayPort < 1 {
		return "", fmt.Errorf("slot port identity is missing")
	}
	name := ""
	for _, container := range pod.Spec.Containers {
		if !strings.HasPrefix(container.Name, "slot-") {
			continue
		}
		for _, env := range container.Env {
			if env.Name == "RUNTIME_GATEWAY_PORT_START" && env.Value == strconv.Itoa(req.GatewayPort) {
				if name != "" {
					return "", fmt.Errorf("ambiguous slot port identity")
				}
				name = container.Name
			}
		}
	}
	if name == "" {
		return "", fmt.Errorf("assigned slot container is missing")
	}
	return name, nil
}

func (c *slotRuntimeAgentClient) stopContainerGateway(ctx context.Context, endpoint, gatewayID string, graceful bool) error {
	if _, bounded := ctx.Deadline(); !bounded {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, runtimeAgentControlTimeout)
		defer cancel()
	}
	a, state, intent, saved, err := c.assignments.ReadContainerStop(ctx, endpoint, gatewayID)
	if err != nil {
		return err
	}
	if intent != nil && (!validContainerIntent(*intent) || intent.Kind != "") {
		return fmt.Errorf("stored container stop identity is invalid")
	}
	pod, err := c.containerStopPod(ctx, endpoint, a.PodUID)
	if err != nil {
		return err
	}
	if pod.Labels["clawmanager.io/sandbox-profile"] != "nfs-rootless-v1" {
		return fmt.Errorf("container lifecycle profile changed")
	}
	identity := runtimeauth.Identity{InstanceID: a.InstanceID, UserID: a.UserID, Generation: a.Generation, PodUID: a.PodUID}
	if err = c.requireWorkspaceWriteFence(ctx, identity); err != nil {
		return err
	}
	if err = c.requireExternalWriterCleanup(ctx, identity); err != nil {
		return err
	}
	if intent == nil {
		slot, err := containerSlotName(pod, a.Request)
		if err != nil {
			return err
		}
		// Explicit tags are needed because the wire uses snake_case.
		var raw struct {
			Digest     string          `json:"digest"`
			Assignment json.RawMessage `json:"assignment"`
		}
		if err = c.do(ctx, http.MethodGet, endpoint, "/v1/slots/"+slot+"/state", nil, &raw); err != nil {
			return err
		}
		var assignment struct {
			PodUID      string `json:"pod_uid"`
			GatewayID   string `json:"gateway_id"`
			WorkerEpoch string `json:"worker_epoch"`
			InstanceID  int    `json:"instance_id"`
			UserID      int    `json:"user_id"`
			Generation  int    `json:"generation"`
		}
		if json.Unmarshal(raw.Assignment, &assignment) != nil || assignment.PodUID != a.PodUID || assignment.GatewayID != gatewayID || assignment.InstanceID != a.InstanceID || assignment.UserID != a.UserID || assignment.Generation != a.Generation {
			if graceful {
				return fmt.Errorf("slot execution identity is unknown")
			}
			return c.cancelUnstartedContainer(ctx, endpoint, gatewayID, a.PodUID)
		}
		var token [16]byte
		if _, err = rand.Read(token[:]); err != nil {
			return err
		}
		candidate := runtimeSlotStopIntent{Version: 1, OperationID: hex.EncodeToString(token[:]), PodUID: a.PodUID, Slot: slot, GatewayID: gatewayID, InstanceID: a.InstanceID, UserID: a.UserID, Generation: a.Generation, WorkerEpoch: assignment.WorkerEpoch, AssignmentDigest: raw.Digest, Graceful: graceful}
		if !validContainerIntent(candidate) {
			return fmt.Errorf("slot execution identity is incomplete")
		}
		intent, err = c.assignments.BeginContainerStop(ctx, endpoint, candidate)
		if err != nil {
			return err
		}
	}
	if graceful && !intent.Graceful {
		return fmt.Errorf("existing stop did not establish graceful shutdown")
	}
	var proof runtimeSlotContainerProof
	if len(saved) != 0 {
		if decodeContainerProof(saved, &proof) != nil || proof.runtimeSlotStopIntent != *intent || !validSlotProofSource(proof) || !proof.CleanupConfirmed || proof.ContainerID == "" || proof.FinishedAt.IsZero() {
			return fmt.Errorf("stored container termination proof is invalid")
		}
	} else {
		// The stop request only prepares teardown and returns pending. An uncertain
		// HTTP result is retried after inspecting Kubernetes, never treated as stop.
		var pending any
		stopErr := c.do(ctx, http.MethodDelete, endpoint, "/v1/gateways/"+url.PathEscape(gatewayID), intent, &pending)
		observed, err := c.waitContainerTermination(ctx, pod.Name, *intent)
		if err != nil {
			return errors.Join(stopErr, err)
		}
		proof = *observed
		data, err := json.Marshal(proof)
		if err != nil {
			return err
		}
		if err = c.assignments.StoreContainerTermination(ctx, endpoint, *intent, data); err != nil {
			return err
		}
	}
	if state == "stopped" {
		return c.releaseStoppedContainer(ctx, endpoint, *intent, proof)
	}
	var receipt struct {
		GatewayID   string `json:"gateway_id"`
		OperationID string `json:"operation_id"`
		WorkerEpoch string `json:"worker_epoch"`
		ContainerID string `json:"container_id"`
		Stopped     bool   `json:"stopped"`
		Confirmed   bool   `json:"confirmed"`
		Unmounted   bool   `json:"unmounted"`
		Graceful    bool   `json:"graceful"`
	}
	if err = c.containerStopRequest(ctx, endpoint, "/v1/slots/"+intent.Slot+"/reconcile-stop", proof, &receipt); err != nil {
		return err
	}
	if receipt.GatewayID != gatewayID || receipt.OperationID != intent.OperationID || receipt.WorkerEpoch != intent.WorkerEpoch || receipt.ContainerID != proof.ContainerID || !receipt.Stopped || !receipt.Confirmed || !receipt.Unmounted || intent.Graceful && !receipt.Graceful {
		return fmt.Errorf("slot writer reconciliation receipt does not match termination")
	}
	if err = c.requireWorkspaceWriteFence(ctx, identity); err != nil {
		return err
	}
	if err = c.requireExternalWriterCleanup(ctx, identity); err != nil {
		return err
	}
	if err = c.assignments.ConfirmStopped(ctx, endpoint, gatewayID); err != nil {
		return err
	}
	return c.releaseStoppedContainer(ctx, endpoint, *intent, proof)
}

func (c *slotRuntimeAgentClient) releaseStoppedContainer(ctx context.Context, endpoint string, intent runtimeSlotStopIntent, proof runtimeSlotContainerProof) error {
	if err := c.requireWorkspaceWriteFence(ctx, runtimeauth.Identity{InstanceID: intent.InstanceID, UserID: intent.UserID, Generation: intent.Generation, PodUID: intent.PodUID}); err != nil {
		return err
	}
	if err := c.requireExternalWriterCleanup(ctx, runtimeauth.Identity{InstanceID: intent.InstanceID, UserID: intent.UserID, Generation: intent.Generation, PodUID: intent.PodUID}); err != nil {
		return err
	}
	var receipt struct {
		GatewayID   string `json:"gateway_id"`
		OperationID string `json:"operation_id"`
		WorkerEpoch string `json:"worker_epoch"`
		ContainerID string `json:"container_id"`
		Released    bool   `json:"released"`
	}
	if err := c.containerStopRequest(ctx, endpoint, "/v1/slots/"+intent.Slot+"/release-stop", proof, &receipt); err != nil {
		return err
	}
	if receipt.GatewayID != intent.GatewayID || receipt.OperationID != intent.OperationID || receipt.WorkerEpoch != intent.WorkerEpoch || receipt.ContainerID != proof.ContainerID || !receipt.Released {
		return fmt.Errorf("slot release acknowledgement does not match confirmed stop")
	}
	return c.assignments.CompleteContainerRelease(ctx, endpoint, intent)
}

// Kubelet publishes termination asynchronously after the worker's HTTP 202.
// Poll only the pinned Pod and exact persisted execution commitment. Missing
// evidence is pending; replacement Pods and ambiguous evidence fail closed.
func (c *slotRuntimeAgentClient) waitContainerTermination(ctx context.Context, name string, intent runtimeSlotStopIntent) (*runtimeSlotContainerProof, error) {
	for {
		pod, err := c.kube.CoreV1().Pods(c.namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		proof, err := runtimeSlotTerminationProof(pod, intent)
		// Explicit operator fencing is independent evidence. Automatic/graceful
		// stops still require the native receipt; waiting must not drop either
		// the upstream delayed proof path or the existing operator recovery.
		if err != nil && !intent.Graceful {
			if fenced, fenceErr := c.operatorContainerFenceProof(ctx, pod, intent); fenceErr == nil {
				return fenced, nil
			}
		}
		if !errors.Is(err, errContainerTerminationPending) {
			return proof, err
		}
		if err = waitContainerStopPoll(ctx, err); err != nil {
			return nil, err
		}
	}
}

// The termination record can arrive before the replacement worker is listening.
// Retry transport/unavailability errors with the SAME proof, never conflicts or
// invalid receipts, and never infer success from a new worker's readiness.
func (c *slotRuntimeAgentClient) containerStopRequest(ctx context.Context, endpoint, path string, proof runtimeSlotContainerProof, receipt any) error {
	for {
		err := c.do(ctx, http.MethodPost, endpoint, path, proof, receipt)
		var transport *url.Error
		if !errors.Is(err, ErrRuntimeAgentUnavailable) && !errors.As(err, &transport) && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			return err
		}
		if err = waitContainerStopPoll(ctx, err); err != nil {
			return err
		}
	}
}

func waitContainerStopPoll(ctx context.Context, pending error) error {
	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return errors.Join(pending, ctx.Err())
	case <-timer.C:
		return nil
	}
}

// This separate receipt fences a generation which never acquired a worker.
// An active or quarantined old writer must be rejected by the worker; absence
// alone is never a teardown receipt and cannot bypass container-stop evidence.
func (c *slotRuntimeAgentClient) cancelUnstartedContainer(ctx context.Context, endpoint, gatewayID, podUID string) error {
	scope, err := c.assignments.PendingCancellation(ctx, endpoint, gatewayID, podUID)
	if err != nil {
		return err
	}
	var receipt struct {
		GatewayID    string `json:"gateway_id"`
		Stopped      bool   `json:"stopped"`
		Confirmed    bool   `json:"confirmed"`
		Unmounted    bool   `json:"unmounted"`
		NeverStarted bool   `json:"never_started"`
	}
	if err = c.do(ctx, http.MethodPost, endpoint, "/v1/gateways/cancel-pending", scope, &receipt); err != nil {
		return err
	}
	if receipt.GatewayID != gatewayID || !receipt.Stopped || !receipt.Confirmed || !receipt.Unmounted || !receipt.NeverStarted {
		return fmt.Errorf("unstarted generation cancellation is not confirmed")
	}
	return c.assignments.ConfirmStopped(ctx, endpoint, gatewayID)
}
