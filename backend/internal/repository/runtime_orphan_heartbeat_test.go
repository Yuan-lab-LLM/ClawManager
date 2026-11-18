package repository

import (
	"clawreef/internal/models"
	"context"
	"testing"
	"time"
)

func TestRetiredRuntimeCannotBeRevivedByLateReportsMySQL(t *testing.T) {
	sess := upgradeMySQL(t)
	ctx := context.Background()
	repo := NewRuntimePodRepository(sess)
	uid := "old-pod"
	pod := models.RuntimePod{Namespace: "runtime", PodName: "old", PodUID: &uid, DeploymentName: "pool", RuntimeType: "hermes", ImageRef: "image", State: "ready", Capacity: 8, UsedSlots: 2}
	if err := repo.UpsertFromAgent(ctx, &pod); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.SQL().Exec("INSERT INTO runtime_pod_deletion_intents(namespace,deployment_name,deployment_uid) VALUES('runtime','pool','controller')"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		update func() error
	}{
		{"heartbeat", func() error { return repo.UpdateHeartbeat(ctx, pod.ID, "ready", 2, 8, false, time.Now()) }},
		{"health scan", func() error { return repo.MarkUnseenUnhealthy(ctx, time.Now().Add(time.Hour)) }},
		{"scheduler state", func() error { return repo.MarkState(ctx, pod.ID, "ready", false) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := sess.SQL().Exec("UPDATE runtime_pods SET state='deleted',used_slots=0 WHERE id=?", pod.ID); err != nil {
				t.Fatal(err)
			}
			if err := tc.update(); err != nil {
				t.Fatal(err)
			}
			got, err := repo.GetByID(ctx, pod.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != "deleted" || got.UsedSlots != 0 {
				t.Fatalf("retired source revived: state=%s used=%d", got.State, got.UsedSlots)
			}
		})
	}
}
