package k8s

import (
	"context"
	"encoding/json"
	"k8s.io/client-go/kubernetes/fake"
	"strings"
	"testing"
)

func TestContainerForceDeleteOnlySupersedesSameExecution(t *testing.T) {
	for _, change := range []string{"same", "operation", "epoch", "digest", "slot", "maintenance", "graceful-force"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			client := fake.NewSimpleClientset()
			service := NewRuntimeAssignmentService(client, "tenant")
			assignment := assignmentFixture()
			assignment.PodUID = "pod-a"
			assignment.Request = json.RawMessage(`{"pod_uid":"pod-a"}`)
			if err := service.EnsureDesired(ctx, assignment); err != nil {
				t.Fatal(err)
			}
			if err := service.MarkDraining(ctx, assignment.Endpoint, assignment.GatewayID); err != nil {
				t.Fatal(err)
			}
			old := RuntimeContainerStopIntent{Version: 1, OperationID: strings.Repeat("1", 32), PodUID: assignment.PodUID, Slot: "slot-0", GatewayID: assignment.GatewayID, InstanceID: assignment.InstanceID, UserID: assignment.UserID, Generation: assignment.Generation, WorkerEpoch: strings.Repeat("2", 32), AssignmentDigest: strings.Repeat("d", 64), Graceful: true}
			if change == "maintenance" {
				old.Kind = "maintenance"
			}
			if _, err := service.BeginContainerStop(ctx, assignment.Endpoint, old); err != nil {
				t.Fatal(err)
			}
			candidate := old
			candidate.Kind = "force-delete"
			candidate.Graceful = false
			switch change {
			case "operation":
				candidate.OperationID = strings.Repeat("3", 32)
			case "epoch":
				candidate.WorkerEpoch = strings.Repeat("3", 32)
			case "digest":
				candidate.AssignmentDigest = strings.Repeat("c", 64)
			case "slot":
				candidate.Slot = "slot-1"
			case "graceful-force":
				candidate.Graceful = true
			}
			result, err := service.BeginContainerStop(ctx, assignment.Endpoint, candidate)
			if change == "same" {
				if err != nil || result == nil || *result != candidate {
					t.Fatalf("explicit deletion did not supersede graceful waiting: %v %v", result, err)
				}
				retried, err := service.BeginContainerStop(ctx, assignment.Endpoint, old)
				if err != nil || *retried != candidate {
					t.Fatal("ordinary retry changed explicit deletion policy")
				}
			} else {
				_, _, current, _, readErr := service.ReadContainerStop(ctx, assignment.Endpoint, assignment.GatewayID)
				if readErr != nil || current == nil || *current != old {
					t.Fatalf("invalid force request changed the committed execution: %v", readErr)
				}
			}
		})
	}
}
