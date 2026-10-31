package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"context"
	"errors"
	"testing"
	"time"
)

type guardedRolloutStore struct {
	repository.RuntimeRolloutRepository
	status string
}

func (s *guardedRolloutStore) GetByID(context.Context, int64) (*models.RuntimeRollout, error) {
	return &models.RuntimeRollout{ID: 1, RuntimeType: "opencode", TargetImageRef: "test/new"}, nil
}
func (s *guardedRolloutStore) UpdateStatus(_ context.Context, _ int64, status string, _ *time.Time, _ *time.Time, _ *string) error {
	s.status = status
	return nil
}

type forbiddenUpgradePods struct {
	repository.RuntimePodRepository
}

func (forbiddenUpgradePods) List(context.Context, string) ([]models.RuntimePod, error) {
	panic("must not inspect/change pools via the unsafe OpenCode rollout")
}
func TestRuntimeOpenCodeUpgradeNeverUsesSharedRollingReplacement(t *testing.T) {
	store := &guardedRolloutStore{}
	scheduler := &RuntimeScheduler{rolloutRepo: store, podRepo: forbiddenUpgradePods{}}
	if err := scheduler.StartRollout(t.Context(), 1); !errors.Is(err, models.ErrOpenCodeSafeUpgradeRequired) {
		t.Fatal(err)
	}
	if store.status != "error" {
		t.Fatal(store.status)
	}
}
