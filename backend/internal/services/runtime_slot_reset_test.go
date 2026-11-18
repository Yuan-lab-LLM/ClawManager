package services

import (
	"clawreef/internal/models"
	"clawreef/internal/runtimeeviction"
	"encoding/json"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"strings"
	"testing"
	"time"
)

func TestSlotResetRecoveryProtectsOtherSameGenerationPlacement(t *testing.T) {
	source := models.InstanceRuntimeBinding{ID: 71, InstanceID: 101, RuntimePodID: 1, RuntimeType: "hermes", GatewayID: "gw-101-4", GatewayPort: 20000, Generation: 4, WorkspacePath: "/workspace"}
	instance := &models.Instance{ID: 101, UserID: 1, Status: "running", RuntimeGeneration: 4}
	for _, field := range []string{"pod", "port", "binding", "gateway", "workspace"} {
		t.Run(field, func(t *testing.T) {
			current := source
			switch field {
			case "pod":
				current.RuntimePodID = 2
			case "port":
				current.GatewayPort++
			case "binding":
				current.ID++
			case "gateway":
				current.GatewayID = "gw-other"
			case "workspace":
				current.WorkspacePath = "/other"
			}
			if got := slotResetRecoveryAction(instance, &current, source, "running"); got != "preserve_current" {
				t.Fatalf("new placement affected: %s", got)
			}
		})
	}
	if got := slotResetRecoveryAction(instance, &source, source, "running"); got != "reschedule" {
		t.Fatalf("reset running source not rescheduled: %s", got)
	}
	if got := slotResetRecoveryAction(instance, nil, source, "running"); got != "reschedule" {
		t.Fatalf("unbound source not rescheduled: %s", got)
	}
}

func TestSlotResetKubeletReceiptUsesAPITimestampPrecision(t *testing.T) {
	scope := runtimeeviction.SlotScope{Version: 1, OperationID: "op", Namespace: "ns", PodName: "pod", PodUID: "uid", NodeName: "node", NodeUID: "nodeuid", NodeBootID: "boot", ContainerName: "slot-0", ContainerID: "containerd://" + strings.Repeat("a", 64)}
	created := time.Now().UTC().Add(-time.Minute).Truncate(time.Second).Add(900 * time.Millisecond)
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: types.UID(scope.PodUID)}, Spec: corev1.PodSpec{NodeName: scope.NodeName}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: scope.ContainerName, LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ContainerID: scope.ContainerID, FinishedAt: metav1.NewTime(created.Truncate(time.Second))}}}}}}
	if _, err := slotResetKubeletReceipt(pod, scope, created); err != nil {
		t.Fatalf("whole-second kubelet exit was lost: %v", err)
	}
	pod.Status.ContainerStatuses[0].LastTerminationState.Terminated.FinishedAt = metav1.NewTime(created.Add(-time.Second).Truncate(time.Second))
	if _, err := slotResetKubeletReceipt(pod, scope, created); err == nil {
		t.Fatal("unrelated historical exit accepted")
	}
	pod.Status.ContainerStatuses[0].LastTerminationState.Terminated.FinishedAt = metav1.NewTime(created)
	pod.Status.ContainerStatuses[0].LastTerminationState.Terminated.ContainerID = "containerd://" + strings.Repeat("b", 64)
	if _, err := slotResetKubeletReceipt(pod, scope, created); err == nil {
		t.Fatal("replacement CID exit substituted for old execution")
	}
}

func TestSlotResetPublicHistoryExcludesPrivateAuthority(t *testing.T) {
	j := &RuntimeSlotResetJournal{Phase: "waiting_worker", PodUID: "source-uid", Slot: "slot-0", ScopeFingerprint: strings.Repeat("a", 64), HelperImage: "private-helper", AssignmentDigest: "private-digest", StorageIdentity: "private-storage", SourcePod: &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Env: []corev1.EnvVar{{Name: "TOKEN", Value: "private-business-secret"}}}}}}, UnpublishedReservation: &runtimeSlotResetUnpublishedProof{UserID: 1, WorkerEpoch: "private-worker"}}
	op := RuntimeManagementOperation{Kind: "slot_reset", Items: []RuntimeSchedulingItem{{SourcePodID: 1, SlotReset: j}}}
	public := publicSlotResetOperation(op)
	b, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-helper", "private-digest", "private-storage", "private-business-secret", "private-worker"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("history leaked %s", secret)
		}
	}
	if !strings.Contains(string(b), j.ScopeFingerprint) || j.SourcePod == nil || j.HelperImage != "private-helper" {
		t.Fatal("public projection lost identity or mutated durable private journal")
	}
}

func TestSlotResetRecoveryRespectsExplicitPowerAndNewGeneration(t *testing.T) {
	source := models.InstanceRuntimeBinding{ID: 71, InstanceID: 101, RuntimePodID: 1, Generation: 4}
	for _, tc := range []struct {
		status, desired string
		generation      int
	}{{"stopped", "running", 4}, {"sleeping", "running", 4}, {"deleting", "running", 4}, {"running", "stopped", 4}, {"creating", "sleeping", 4}, {"creating", "running", 3}} {
		i := &models.Instance{ID: 101, Status: tc.status, RuntimeGeneration: tc.generation}
		if got := slotResetRecoveryAction(i, nil, source, tc.desired); got != "none" {
			t.Fatalf("intent %+v overridden: %s", tc, got)
		}
	}
	for _, generation := range []int{4, 5, 7} {
		i := &models.Instance{ID: 101, Status: "creating", RuntimeGeneration: generation}
		if got := slotResetRecoveryAction(i, nil, source, "running"); got != "reschedule" {
			t.Fatalf("unbound current generation %d not scheduled: %s", generation, got)
		}
	}
}
