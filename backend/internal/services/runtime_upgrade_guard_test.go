package services

import (
	"context"
	"errors"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

type upgradeBlockedRepo struct {
	repository.InstanceRepository
	failure error
}

func (r upgradeBlockedRepo) AcquireLifecycleLock(context.Context, int) (func(), error) {
	return nil, r.failure
}

func TestDurableUpgradeBlocksAllLifecycleEntrypoints(t *testing.T) {
	s := &instanceService{instanceRepo: upgradeBlockedRepo{failure: models.ErrRuntimeUpgradeInProgress}}
	for name, call := range map[string]func() error{
		"start": func() error { return s.Start(90321) }, "stop": func() error { return s.Stop(90321) }, "restart": func() error { return s.Restart(90321) },
		"restart-env": func() error { return s.RestartWithEnvironment(90321, nil, nil) }, "delete": func() error { return s.Delete(90321) },
		"update": func() error { return s.Update(90321, UpdateInstanceRequest{}) }, "sync": func() error { return s.ForceSyncInstance(90321) },
	} {
		if err := call(); !errors.Is(err, models.ErrRuntimeUpgradeInProgress) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// Nil dependencies would panic if background work bypassed the guard.
	s.completeDeletion(1, 90321)
}

func TestUpgradeBlocksSchedulerAndSyncWithoutMarkingHealthyInstancesFailed(t *testing.T) {
	for _, failure := range []error{models.ErrRuntimeUpgradeInProgress, errors.New("database unavailable")} {
		repo := upgradeBlockedRepo{failure: failure}
		s := &RuntimeScheduler{instanceRepo: repo}
		snapshot := models.Instance{ID: 90322, Type: "opencode", RuntimeType: "gateway", InstanceMode: "lite", Status: "creating"}
		// The desired path requires an eligible workspace before acquiring the guard.
		workspace := "/workspaces/opencode/user-1/instance-90322"
		snapshot.WorkspacePath = &workspace
		for _, errs := range [][]error{s.reconcileCreatingInstance(t.Context(), snapshot), s.reconcileDesiredInstance(t.Context(), snapshot)} {
			if errors.Is(failure, models.ErrRuntimeUpgradeInProgress) && len(errs) != 0 {
				t.Fatal(errs)
			}
			if !errors.Is(failure, models.ErrRuntimeUpgradeInProgress) && len(errs) != 1 {
				t.Fatal("database error swallowed", errs)
			}
		}
		if err := s.failoverBinding(t.Context(), 1, models.InstanceRuntimeBinding{InstanceID: 90322}, "lost"); !errors.Is(failure, models.ErrRuntimeUpgradeInProgress) && err == nil {
			t.Fatal("failover ignored database failure")
		}
		(&SyncService{instanceRepo: repo}).syncInstance(t.Context(), &snapshot)
	}
}

type upgradeSnapshotRepo struct {
	repository.InstanceRepository
	released int
}

func (r *upgradeSnapshotRepo) AcquireLifecycleLock(context.Context, int) (func(), error) {
	return func() { r.released++ }, nil
}
func (*upgradeSnapshotRepo) GetByID(id int) (*models.Instance, error) {
	return &models.Instance{ID: id, Status: "stopped"}, nil
}

func TestSchedulerRereadsSnapshotUnderLifecycleLock(t *testing.T) {
	repo := &upgradeSnapshotRepo{}
	s := &RuntimeScheduler{instanceRepo: repo}
	path := "/workspaces/opencode/user-1/instance-90323"
	snapshot := models.Instance{ID: 90323, Type: "opencode", RuntimeType: "gateway", InstanceMode: "lite", WorkspacePath: &path, Status: "creating"}
	if errs := s.reconcileCreatingInstance(t.Context(), snapshot); len(errs) > 0 {
		t.Fatal(errs)
	}
	if errs := s.reconcileDesiredInstance(t.Context(), snapshot); len(errs) > 0 {
		t.Fatal(errs)
	}
	if repo.released != 2 {
		t.Fatal("guard leaked", repo.released)
	}
}

func TestSchedulerSharesGuardWithForegroundLifecycle(t *testing.T) {
	release, err := acquireInstanceLifecycleOperation(t.Context(), nil, 90324)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	s := &RuntimeScheduler{}
	if errs := s.reconcileCreatingInstance(t.Context(), models.Instance{ID: 90324}); len(errs) > 0 {
		t.Fatal(errs)
	}
	if err := s.failoverBinding(t.Context(), 1, models.InstanceRuntimeBinding{InstanceID: 90324}, "lost"); err != nil {
		t.Fatal(err)
	}
}
