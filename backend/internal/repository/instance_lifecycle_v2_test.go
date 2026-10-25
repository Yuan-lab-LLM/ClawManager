package repository

import (
	"clawreef/internal/models"
	"testing"
)

func TestLifecycleV2SamePodFive(t *testing.T) {
	b := models.InstanceLifecycleBatch{BatchID: "new", Action: "restart", PolicyVersion: 2}
	batches := map[string]models.InstanceLifecycleBatch{"new": b}
	item := models.InstanceLifecycleBatchItem{BatchID: "new", SourceID: 99, RuntimeType: "hermes", RuntimePodID: 7}
	var active []models.InstanceLifecycleBatchItem
	for n := 0; n <= 5; n++ {
		if got := lifecyclePodHasCapacity(item, b, active, batches); got != (n < 5) {
			t.Fatalf("active=%d admitted=%v", n, got)
		}
		active = append(active, models.InstanceLifecycleBatchItem{BatchID: "new", SourceID: n + 1, RuntimeType: "hermes", RuntimePodID: 7})
	}
	b.Action = "reset"
	batches["new"] = b
	if !lifecyclePodHasCapacity(item, b, active[:4], batches) {
		t.Fatal("same-type reset remained serial")
	}
	item.SourceID = 1
	if lifecyclePodHasCapacity(item, b, active[:1], batches) {
		t.Fatal("same instance admitted twice")
	}
}

func TestLifecycleV2PreservesLegacyExclusion(t *testing.T) {
	b := models.InstanceLifecycleBatch{BatchID: "new", Action: "restart", PolicyVersion: 2}
	old := models.InstanceLifecycleBatch{BatchID: "old", Action: "restart"}
	active := []models.InstanceLifecycleBatchItem{{BatchID: "old", SourceID: 1, RuntimePodID: 7, RuntimeType: "hermes"}}
	if lifecyclePodHasCapacity(models.InstanceLifecycleBatchItem{SourceID: 2, RuntimePodID: 7, RuntimeType: "hermes"}, b, active, map[string]models.InstanceLifecycleBatch{"old": old}) {
		t.Fatal("new task bypassed legacy protection")
	}
}

func TestLifecycleRetryEligibility(t *testing.T) {
	for _, tc := range []struct {
		action, code string
		want         bool
	}{
		{"restart", "LIFECYCLE_FAILED", true}, {"restart", "LIFECYCLE_TIMEOUT", true},
		{"restart", "INSTANCE_NOT_FOUND", false}, {"reset", "RESET_REPLACEMENT_FAILED", true},
		{"reset", "OLD_INSTANCE_CLEANUP_PENDING", true}, {"reset", "RESET_UNAVAILABLE", false},
		{"reset", "BATCH_RETRY_UNSAFE", false},
	} {
		code := tc.code
		if lifecycleRetryCandidate(tc.action, models.NorthboundOperation{ErrorCode: &code}) != tc.want {
			t.Fatalf("unexpected eligibility %+v", tc)
		}
	}
}
