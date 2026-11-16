package services

import (
	"clawreef/internal/models"
	"context"
	"testing"
)

func TestOpenClawRecoveryWaitsForEarlierRolloutBeforePrepareOrAdvance(t *testing.T) {
	for _, status := range []string{"pending", "running"} {
		t.Run(status, func(t *testing.T) {
			preflight := "accepted-preflight"
			earlier := &models.RuntimeRollout{ID: 10, RuntimeType: RuntimeTypeOpenClaw, Status: status}
			later := &models.RuntimeRollout{ID: 11, RuntimeType: RuntimeTypeOpenClaw, Status: "pending", PreflightID: &preflight, TargetImageRef: "reviewed-target"}
			repo := &fakeRuntimeRolloutRepo{rollouts: map[int64]*models.RuntimeRollout{10: earlier, 11: later}}
			scheduler := &RuntimeScheduler{rolloutRepo: repo, podRepo: &fakeRuntimePodRepo{}}
			// No upgrade service: reaching Prepare would fail, while the queued path
			// must wait without changing status, maintenance or source processes.
			if err := scheduler.StartRollout(context.Background(), later.ID); err != nil {
				t.Fatalf("queued prepare: %v", err)
			}
			later.Status = "running" // Resume old controllers' already-admitted rows.
			if err := scheduler.finishRolloutIfReady(context.Background(), *later); err != nil {
				t.Fatalf("queued advance: %v", err)
			}
			if len(repo.statuses) != 0 {
				t.Fatal("queued rollout mutated state")
			}
			earlier.Status = "finished"
			later.Status = "pending"
			if err := scheduler.StartRollout(context.Background(), later.ID); err == nil {
				t.Fatal("head never reached the required upgrade service")
			}
		})
	}
}
