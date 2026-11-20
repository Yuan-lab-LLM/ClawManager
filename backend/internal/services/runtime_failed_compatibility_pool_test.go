package services

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"clawreef/internal/services/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestFailedCompatibilityPoolRequiresExitAndReleasedOwner(t *testing.T) {
	intent := runtimeSlotStopIntent{Version: 1, OperationID: strings.Repeat("a", 32), PodUID: "old-pod", Slot: "slot-7", GatewayID: "gw-7-3", InstanceID: 7, UserID: 2, Generation: 3, WorkerEpoch: strings.Repeat("b", 32), AssignmentDigest: strings.Repeat("c", 64), Kind: "maintenance"}
	proof := runtimeSlotContainerProof{runtimeSlotTerminationMessage: runtimeSlotTerminationMessage{runtimeSlotStopIntent: intent, CleanupConfirmed: true}, ContainerID: "containerd://original", FinishedAt: time.Now().UTC()}
	intentJSON, _ := json.Marshal(intent)
	proofJSON, _ := json.Marshal(proof)
	for _, tc := range []struct {
		name, proof, owner string
		allowed            bool
	}{
		{name: "missing termination", owner: "released"},
		{name: "owner still active", proof: string(proofJSON), owner: "active"},
		{name: "exact termination and release", proof: string(proofJSON), owner: "released", allowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "maintenance-record", Namespace: "runtime", Labels: map[string]string{"clawmanager.io/purpose": "lite-native-maintenance"}}, Data: map[string]string{"state": "released", "intent": string(intentJSON), "termination_proof": tc.proof, "operation_id": intent.OperationID, "instance_id": "7", "pod_uid": intent.PodUID, "result_secret": "result"}}
			owner := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "lite-maint-owner-7", Namespace: "runtime", Labels: map[string]string{"clawmanager.io/purpose": "lite-native-maintenance-owner"}}, Data: map[string]string{"state": tc.owner, "operation_id": intent.OperationID}}
			kube := fake.NewSimpleClientset(record, owner)
			client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", nil).(*slotRuntimeAgentClient)
			if _, err := client.failedCompatibilityReleases(t.Context(), []k8s.RuntimePodScaleInIdentity{{PodUID: intent.PodUID}}); (err == nil) != tc.allowed {
				t.Fatalf("expected allowed=%v, got %v", tc.allowed, err)
			}
		})
	}
}

func TestFailedCompatibilityPoolDoesNotTreatReleasedTombstoneAsFreeCapacity(t *testing.T) {
	intent := runtimeSlotStopIntent{OperationID: strings.Repeat("a", 32), PodUID: "old-pod", Slot: "slot-7", WorkerEpoch: strings.Repeat("b", 32), AssignmentDigest: strings.Repeat("c", 64), GatewayID: "gw-7-3", InstanceID: 7, UserID: 2, Generation: 3}
	proof := runtimeSlotContainerProof{runtimeSlotTerminationMessage: runtimeSlotTerminationMessage{runtimeSlotStopIntent: intent}}
	no := false
	var snapshot runtimeSlotAuditSnapshot
	if err := json.Unmarshal([]byte(`{"gateways":[]}`), &snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.PodUID, snapshot.WorkerEpoch, snapshot.Digest = intent.PodUID, strings.Repeat("d", 32), intent.AssignmentDigest
	snapshot.IdleConfirmed, snapshot.Quarantined, snapshot.MaintenanceOperationID = &no, &no, intent.OperationID
	snapshot.Assignment = &runtimeSlotAuditOwner{PodUID: intent.PodUID, WorkerEpoch: intent.WorkerEpoch, GatewayID: intent.GatewayID, InstanceID: 7, UserID: 2, Generation: 3, State: "stopped"}
	slot := runtimeSlotAuditObservation{Name: "slot-7", ReadOK: true, Err: errRuntimeSlotOwnerIdentity, Snapshot: snapshot}
	if slot.free() || !releasedMaintenanceSlot(slot, proof) {
		t.Fatal("released tombstone is safe only for retiring its exact pool")
	}
	slot.Snapshot.Assignment.State = "running"
	if releasedMaintenanceSlot(slot, proof) {
		t.Fatal("a running assignment would be retired")
	}
}
