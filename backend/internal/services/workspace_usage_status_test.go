package services

import (
	"clawreef/internal/models"
	"encoding/json"
	"testing"
	"time"
)

func TestWorkspaceUsageNativeStatusCarriesProof(t *testing.T) {
	repo := newV2LifecycleInstanceRepo()
	repo.byID[7] = &models.Instance{ID: 7, UserID: 3, Type: "openclaw", RuntimeType: "gateway", InstanceMode: "lite", Status: "stopped"}
	s := &instanceService{instanceRepo: repo}
	measured := time.Now().UTC()
	for _, at := range []*time.Time{nil, &measured} {
		repo.byID[7].WorkspaceUsageObservedAt = at
		status, err := s.GetInstanceStatus(7)
		if err != nil {
			t.Fatal(err)
		}
		if status.WorkspaceUsageObservedAt != at {
			t.Fatal("native status discarded observation")
		}
		raw, err := json.Marshal(status)
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err = json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded["workspace_usage_bytes"] != float64(0) {
			t.Fatal("empty observation omitted zero", string(raw))
		}
		value, ok := decoded["workspace_usage_observed_at"]
		if !ok || (value == nil) != (at == nil) {
			t.Fatal("status omitted availability", string(raw))
		}
	}
}
