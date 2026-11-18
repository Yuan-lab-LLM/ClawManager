package handlers

import (
	"clawreef/internal/models"
	"clawreef/internal/services"
	"encoding/json"
	"testing"
	"time"
)

func TestWorkspaceUsageStatusProof(t *testing.T) {
	i := &models.Instance{ID: 7, UserID: 3, Type: "openclaw", RuntimeType: "gateway", InstanceMode: "lite"}
	status := &services.InstanceStatus{InstanceID: 7, Status: "stopped"}
	for _, observed := range []*time.Time{nil, func() *time.Time { x := time.Now().UTC(); return &x }()} {
		i.WorkspaceUsageObservedAt = observed
		payload := buildUserSafeInstanceStatus(i, status)
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err = json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		value, exists := decoded["workspace_usage_observed_at"]
		if !exists || (value == nil) != (observed == nil) {
			t.Fatalf("status omitted measurement availability: %s", raw)
		}
		raw, err = json.Marshal(i)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if _, ok := decoded["workspace_usage_epoch"]; ok {
			t.Fatal("internal reset fence exposed")
		}
	}
}
