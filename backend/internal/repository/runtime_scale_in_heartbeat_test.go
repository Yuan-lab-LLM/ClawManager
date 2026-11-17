package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"clawreef/internal/models"
)

func TestScaleInHeartbeatPreservesSourceClaimsMySQL(t *testing.T) {
	for _, status := range []string{"pending", "processing", "starting", "needs_attention", "completed", "failed", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			sess := upgradeMySQL(t)
			ctx := context.Background()
			repo := NewRuntimePodRepository(sess)
			var pods []*models.RuntimePod
			for _, name := range []string{"source", "retained", "other-type", "manually-paused"} {
				uid := name + "-uid"
				kind := "deepseek-harness"
				if name == "other-type" {
					kind = "hermes"
				}
				seen := time.Now().UTC().Add(-time.Minute)
				pod := &models.RuntimePod{Namespace: "runtime", PodName: name, PodUID: &uid, DeploymentName: kind + "-runtime", RuntimeType: kind, ImageRef: "image", State: "ready", Capacity: 2, LastSeenAt: &seen}
				if err := repo.UpsertFromAgent(ctx, pod); err != nil {
					t.Fatal(err)
				}
				pods = append(pods, pod)
			}
			if _, err := sess.SQL().Exec("UPDATE runtime_pods SET scheduling_paused=1 WHERE id IN (?,?)", pods[0].ID, pods[3].ID); err != nil {
				t.Fatal(err)
			}
			journal, _ := json.Marshal(map[string]any{"runtime_type": "deepseek-harness", "phase": "cleanup", "plan": map[string]any{"source_pods": []map[string]any{{"id": pods[0].ID, "pod_uid": *pods[0].PodUID}}}})
			if _, err := sess.SQL().Exec("INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES('scale-in','scale_in',?,?,'[]',NOW(),NOW())", status, string(journal)); err != nil {
				t.Fatal(err)
			}
			active := status != "completed" && status != "failed" && status != "cancelled"
			seen := time.Now().UTC().Truncate(time.Second)
			for index, pod := range pods {
				if err := repo.UpdateHeartbeat(ctx, pod.ID, "ready", 1, 2, false, seen); err != nil {
					t.Fatal(err)
				}
				got, err := repo.GetByID(ctx, pod.ID)
				if err != nil {
					t.Fatal(err)
				}
				want := 1
				if index == 0 && active {
					want = 0
				}
				if got.UsedSlots != want || got.LastSeenAt == nil || !got.LastSeenAt.Equal(seen) {
					t.Fatalf("pod=%s status=%s used=%d want=%d last_seen=%v want=%v", pod.PodName, status, got.UsedSlots, want, got.LastSeenAt, seen)
				}
			}
			if active {
				// Existing real claims still require explicit lifecycle release.
				if _, err := sess.SQL().Exec("UPDATE runtime_pods SET used_slots=2 WHERE id=?", pods[0].ID); err != nil {
					t.Fatal(err)
				}
				for remaining := 2; remaining >= 0; remaining-- {
					if err := repo.UpdateHeartbeat(ctx, pods[0].ID, "ready", 0, 2, false, seen); err != nil {
						t.Fatal(err)
					}
					got, _ := repo.GetByID(ctx, pods[0].ID)
					if got.UsedSlots != remaining {
						t.Fatalf("heartbeat released real claims: got=%d want=%d", got.UsedSlots, remaining)
					}
					if remaining > 0 {
						if err := repo.ReleaseSlot(ctx, pods[0].ID); err != nil {
							t.Fatal(err)
						}
					}
				}
				// A reused row for a different immutable UID is not this source.
				if _, err := sess.SQL().Exec("UPDATE runtime_pods SET pod_uid='replacement-uid' WHERE id=?", pods[0].ID); err != nil {
					t.Fatal(err)
				}
				if err := repo.UpdateHeartbeat(ctx, pods[0].ID, "ready", 1, 2, false, seen); err != nil {
					t.Fatal(err)
				}
				got, _ := repo.GetByID(ctx, pods[0].ID)
				if got.UsedSlots != 1 {
					t.Fatal("old source UID froze a different Runtime identity")
				}
			}
		})
	}
}
