package services

import (
	"encoding/json"
	"strings"
	"testing"

	"clawreef/internal/models"
)

func TestPendingPublishedFailureRequiresCurrentFailedSlot(t *testing.T) {
	for _, scenario := range []string{"failed", "healthy", "foreign", "changed-epoch", "maintenance", "empty-proof", "no-digest", "configuration", "empty-report"} {
		t.Run(scenario, func(t *testing.T) {
			instance := models.Instance{ID: 7, UserID: 3, RuntimeGeneration: 1}
			gateway := map[string]any{"gateway_id": "gw-7-1", "state": "error", "error_message": "gateway_start_failed"}
			owner := map[string]any{"gateway_id": "gw-7-1", "instance_id": 7, "user_id": 3, "generation": 1, "worker_epoch": strings.Repeat("1", 32), "state": "starting"}
			data := map[string]any{"worker_epoch": owner["worker_epoch"], "digest": strings.Repeat("a", 64), "idle_confirmed": false, "assignment": owner, "gateways": []any{gateway}}
			switch scenario {
			case "healthy":
				gateway["state"] = "running"
			case "foreign":
				owner["user_id"] = 4
			case "changed-epoch":
				data["worker_epoch"] = strings.Repeat("2", 32)
			case "maintenance":
				data["maintenance_operation_id"] = "other-operation"
			case "empty-proof":
				data["idle_confirmed"] = true
			case "no-digest":
				delete(data, "digest")
			case "configuration":
				gateway["error_message"] = "invalid_configuration"
			case "empty-report":
				data["gateways"] = []any{}
			}
			raw, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			var snapshot runtimeSlotAuditSnapshot
			if err := json.Unmarshal(raw, &snapshot); err != nil {
				t.Fatal(err)
			}
			if got := pendingPublishedSlotFailed(snapshot, instance, "gw-7-1"); got != (scenario == "failed") {
				t.Fatalf("pending start failure admitted = %v", got)
			}
		})
	}
}
