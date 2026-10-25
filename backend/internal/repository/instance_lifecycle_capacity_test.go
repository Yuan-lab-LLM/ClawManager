package repository

import (
	"clawreef/internal/models"
	"testing"
)

func TestLifecycleCapacityPerActionAndGlobal(t *testing.T) {
	batches := map[string]models.InstanceLifecycleBatch{"r": {Action: "restart"}, "s": {Action: "reset"}}
	for restarts := 0; restarts <= 10; restarts++ {
		for resets := 0; resets <= 10; resets++ {
			active := []models.InstanceLifecycleBatchItem{}
			for i := 0; i < restarts; i++ {
				active = append(active, models.InstanceLifecycleBatchItem{BatchID: "r"})
			}
			for i := 0; i < resets; i++ {
				active = append(active, models.InstanceLifecycleBatchItem{BatchID: "s"})
			}
			for _, action := range []string{"restart", "reset"} {
				count := restarts
				if action == "reset" {
					count = resets
				}
				want := restarts+resets < 10 && count < 5
				if got := lifecycleHasCapacity(action, active, batches); got != want {
					t.Fatalf("restart=%d reset=%d action=%s: got %v want %v", restarts, resets, action, got, want)
				}
			}
		}
	}
	if lifecycleHasCapacity("reset", []models.InstanceLifecycleBatchItem{{BatchID: "missing"}}, batches) {
		t.Fatal("unknown reservation admitted work")
	}
}
