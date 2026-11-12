package services

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"clawreef/internal/models"
	"k8s.io/client-go/kubernetes/fake"
)

func TestStoppedSlotAfterWorkerRestartRequiresFreshFullExitProof(t *testing.T) {
	for _, scenario := range []string{"fresh-idle", "owner-running", "busy", "quarantined", "maintenance", "gateway-remains", "missing-idle", "wrong-pod"} {
		t.Run(scenario, func(t *testing.T) {
			snapshot := auditOccupiedSnapshot("stopped")
			snapshot["idle_confirmed"] = true
			owner := snapshot["assignment"].(map[string]any)
			owner["worker_epoch"] = strings.Repeat("b", 32)
			switch scenario {
			case "owner-running":
				owner["state"] = "running"
			case "busy":
				snapshot["idle_confirmed"] = false
			case "quarantined":
				snapshot["quarantined"] = true
			case "maintenance":
				snapshot["maintenance_operation_id"] = "incomplete-operation"
			case "gateway-remains":
				snapshot["gateways"] = []any{map[string]any{"gateway_id": "gw-7-1", "state": "running"}}
			case "missing-idle":
				delete(snapshot, "idle_confirmed")
			case "wrong-pod":
				owner["pod_uid"] = "another-pod"
			}
			client := NewSlotRuntimeAgentClientWithHTTPClient("token", fake.NewSimpleClientset(auditTestPod()), "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				b, err := json.Marshal(snapshot)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, err
			})}).(*slotRuntimeAgentClient)
			endpoint, uid := "https://10.0.0.2:19090", "pod-uid"
			ports, err := client.reservedGatewayPorts(context.Background(), models.RuntimePod{ID: 1, RuntimeType: "openclaw", Capacity: 1, AgentEndpoint: &endpoint, PodUID: &uid})
			_, reserved := ports[20000]
			if scenario == "fresh-idle" {
				if err != nil || reserved {
					t.Fatalf("confirmed empty restarted worker blocked by historical epoch: ports=%v err=%v", ports, err)
				}
			} else if err == nil && !reserved {
				t.Fatal("incomplete exit proof permitted placement")
			}
		})
	}
}
