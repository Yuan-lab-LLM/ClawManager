package services

import (
	"clawreef/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"io"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"net/http"
	"strings"
	"testing"
	"time"
)

type forceInactiveProbe interface {
	openCodeForceInactiveTarget(context.Context, models.RuntimePod, json.RawMessage) error
}

func forceInactiveFixture(t *testing.T) (*slotRuntimeAgentClient, *fake.Clientset, models.RuntimePod, json.RawMessage, map[string]map[string]any, *corev1.ConfigMap, *corev1.Secret) {
	t.Helper()
	ctx := context.Background()
	c, kube, items, target, cm, secret := forceNativeEvidenceFixture(t)
	evidence, err := c.openCodeForceNativeEvidence(ctx, 14, items, target)
	if err != nil {
		t.Fatal(err)
	}
	target.Capacity = 8
	target.RuntimeType = "opencode"
	target.ImageRef = "registry.local/opencode@sha256:" + strings.Repeat("b", 64)
	pod := auditRoutingPod(8, RuntimeGatewayPortBlockSize("opencode"))
	for i := range pod.Spec.Containers {
		pod.Spec.Containers[i].Image = target.ImageRef
	}
	var proof runtimeSlotContainerProof
	if err = json.Unmarshal([]byte(cm.Data["termination_proof"]), &proof); err != nil {
		t.Fatal(err)
	}
	commitment, err := containerTerminationCommitment(proof.runtimeSlotTerminationMessage)
	if err != nil {
		t.Fatal(err)
	}
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "slot-0", Ready: true, ContainerID: "containerd://fresh-worker", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(time.Now().UTC())}}, LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ContainerID: proof.ContainerID, Message: commitment, FinishedAt: metav1.NewTime(proof.FinishedAt)}}}}
	if _, err = kube.CoreV1().Pods("runtime").Update(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	snapshots := map[string]map[string]any{}
	for i := 0; i < 8; i++ {
		snapshots[fmt.Sprintf("slot-%d", i)] = auditFreeSnapshot()
	}
	retired := snapshots["slot-0"]
	retired["idle_confirmed"] = false
	retired["maintenance_operation_id"] = proof.OperationID
	retired["digest"] = proof.AssignmentDigest
	retired["assignment"] = map[string]any{"pod_uid": proof.PodUID, "worker_epoch": proof.WorkerEpoch, "gateway_id": proof.GatewayID, "instance_id": proof.InstanceID, "user_id": proof.UserID, "generation": proof.Generation, "state": "stopped"}
	c.runtimeAgentHTTPClient = NewRuntimeAgentClientWithHTTPClient("FIXTURE_TOKEN_MUST_NOT_PRINT", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Fatal("inactive probe dispatched mutation")
		}
		name := strings.Split(r.URL.Path, "/")[3]
		data, _ := json.Marshal(snapshots[name])
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(data))), Header: make(http.Header)}, nil
	})}).(*runtimeAgentHTTPClient)
	return c, kube, target, evidence, snapshots, cm, secret
}
func TestOpenCodeForceInactiveTargetAllowsOnlyProvedRetiredCompatibility(t *testing.T) {
	c, kube, target, evidence, _, _, _ := forceInactiveFixture(t)
	probe, ok := any(c).(forceInactiveProbe)
	if !ok {
		t.Fatal("exact released-maintenance retire observation unavailable")
	}
	if err := c.openCodePoolIdle(context.Background(), target); err == nil {
		t.Fatal("fixture must remain rejected by generic idle gate")
	}
	from := len(kube.Actions())
	if err := probe.openCodeForceInactiveTarget(context.Background(), target, evidence); err != nil {
		t.Fatal(err)
	}
	forceEvidenceAssertReadOnly(t, kube, from)
	if err := c.openCodePoolIdle(context.Background(), target); err == nil {
		t.Fatal("force observation weakened generic idle admission")
	}
}
func TestOpenCodeForceInactiveTargetRejectsUnknownAndChangedWriters(t *testing.T) {
	for _, scenario := range []string{"other-writer-owned", "writer-owned", "unknown-operation", "wrong-digest", "same-worker", "wrong-owner", "running-owner", "quarantined", "active-gateway", "missing-gateways", "missing-idle", "other-occupied", "other-unknown", "changed-record", "active-record", "active-owner", "mutable-result", "changed-result", "wrong-kube-c1", "current-not-running", "current-old-container", "wrong-pod"} {
		t.Run(scenario, func(t *testing.T) {
			c, kube, target, evidence, snapshots, cm, secret := forceInactiveFixture(t)
			probe, ok := any(c).(forceInactiveProbe)
			if !ok {
				t.Fatal("exact released-maintenance retire observation unavailable")
			}
			s := snapshots["slot-0"]
			a := s["assignment"].(map[string]any)
			switch scenario {
			case "other-writer-owned":
				snapshots["slot-1"]["nfs_writer_owned"] = true
			case "writer-owned":
				s["nfs_writer_owned"] = true
			case "unknown-operation":
				s["maintenance_operation_id"] = strings.Repeat("9", 32)
			case "wrong-digest":
				s["digest"] = strings.Repeat("9", 64)
			case "same-worker":
				s["worker_epoch"] = a["worker_epoch"]
			case "wrong-owner":
				a["instance_id"] = 8
			case "running-owner":
				a["state"] = "running"
			case "quarantined":
				s["quarantined"] = true
			case "active-gateway":
				s["gateways"] = []any{map[string]any{"gateway_id": "gw-7-1", "state": "running"}}
			case "missing-gateways":
				delete(s, "gateways")
			case "missing-idle":
				delete(s, "idle_confirmed")
			case "other-occupied":
				snapshots["slot-1"] = auditOccupiedSnapshot("running")
			case "other-unknown":
				delete(snapshots["slot-1"], "worker_epoch")
			case "changed-record":
				cm.UID = "replacement"
			case "active-record":
				cm.Data["state"] = "running"
			case "active-owner":
				owner, _ := kube.CoreV1().ConfigMaps("runtime").Get(context.Background(), "lite-maint-owner-7", metav1.GetOptions{})
				owner.Data["state"] = "active"
				_, _ = kube.CoreV1().ConfigMaps("runtime").Update(context.Background(), owner, metav1.UpdateOptions{})
			case "mutable-result":
				no := false
				secret.Immutable = &no
			case "changed-result":
				secret.Data["result"] = []byte(`{"error":"FIXTURE_TOKEN_MUST_NOT_PRINT"}`)
			case "wrong-pod":
				*target.PodUID = "replacement"
			case "wrong-kube-c1", "current-not-running", "current-old-container":
				pod, _ := kube.CoreV1().Pods("runtime").Get(context.Background(), target.PodName, metav1.GetOptions{})
				if scenario == "wrong-kube-c1" {
					pod.Status.ContainerStatuses[0].LastTerminationState.Terminated.ContainerID = "containerd://foreign"
				}
				if scenario == "current-not-running" {
					pod.Status.ContainerStatuses[0].State = corev1.ContainerState{}
				}
				if scenario == "current-old-container" {
					pod.Status.ContainerStatuses[0].ContainerID = pod.Status.ContainerStatuses[0].LastTerminationState.Terminated.ContainerID
				}
				_, _ = kube.CoreV1().Pods("runtime").Update(context.Background(), pod, metav1.UpdateOptions{})
			}
			_, _ = kube.CoreV1().ConfigMaps("runtime").Update(context.Background(), cm, metav1.UpdateOptions{})
			_, _ = kube.CoreV1().Secrets("runtime").Update(context.Background(), secret, metav1.UpdateOptions{})
			from := len(kube.Actions())
			err := probe.openCodeForceInactiveTarget(context.Background(), target, evidence)
			if err == nil {
				t.Fatal("unknown writer accepted", scenario)
			}
			if strings.Contains(err.Error(), "FIXTURE_TOKEN") {
				t.Fatal("private result echoed")
			}
			forceEvidenceAssertReadOnly(t, kube, from)
		})
	}
}

func TestOpenCodeForcePreflightStagesNeverEchoNativeError(t *testing.T) {
	for _, stage := range []string{"operation", "source_controller", "old_target_slots", "source_stop", "native_receipt", "lease_absence", "image_contract"} {
		err := &OpenCodeForcePreflightError{stage: stage, cause: fmt.Errorf("FIXTURE_TOKEN_MUST_NOT_PRINT")}
		if OpenCodeForcePreflightStage(err) != stage || strings.Contains(err.Error(), "FIXTURE_TOKEN") {
			t.Fatal("unsafe stage", stage)
		}
	}
	if OpenCodeForcePreflightStage(fmt.Errorf("FIXTURE_TOKEN_MUST_NOT_PRINT")) != "operation" {
		t.Fatal("unclassified error reflected")
	}
}

func TestOpenCodeForceInactiveTargetMatchesC1InstantAcrossTimezones(t *testing.T) {
	c, kube, target, evidence, _, cm, _ := forceInactiveFixture(t)
	var proof runtimeSlotContainerProof
	if err := json.Unmarshal([]byte(cm.Data["termination_proof"]), &proof); err != nil {
		t.Fatal(err)
	}
	proof.FinishedAt = proof.FinishedAt.In(time.FixedZone("fixture+08", 8*3600))
	cm.Data["termination_proof"] = string(mustForceJSON(t, proof))
	if _, err := kube.CoreV1().ConfigMaps("runtime").Update(context.Background(), cm, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := c.openCodeForceInactiveTarget(context.Background(), target, evidence); err != nil {
		t.Fatal("same C1 instant misclassified", err)
	}
}
