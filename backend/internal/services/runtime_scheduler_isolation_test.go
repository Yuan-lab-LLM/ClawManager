package services

import (
	"clawreef/internal/models"
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type stalledRolloutInventory struct {
	*fakeRuntimeRolloutRepo
	entered chan struct{}
	exited  chan struct{}
	once    sync.Once
}

func (r *stalledRolloutInventory) ListActive(ctx context.Context, _ string) ([]models.RuntimeRollout, error) {
	r.once.Do(func() { close(r.entered) })
	<-ctx.Done()
	close(r.exited)
	return nil, ctx.Err()
}

type observedCreatingInventory struct {
	*fakeRuntimeInstanceRepo
	checks atomic.Int32
}

func (r *observedCreatingInventory) GetV2Creating(context.Context, int) ([]models.Instance, error) {
	r.checks.Add(1)
	return nil, nil
}

func TestRuntimeSchedulerLongRolloutDoesNotBlockOrdinaryReconciliation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	instances := &observedCreatingInventory{fakeRuntimeInstanceRepo: newFakeRuntimeInstanceRepo()}
	rollouts := &stalledRolloutInventory{fakeRuntimeRolloutRepo: &fakeRuntimeRolloutRepo{}, entered: make(chan struct{}), exited: make(chan struct{})}
	scheduler := NewRuntimeScheduler(instances, &fakeRuntimePodRepo{}, &fakeRuntimeBindingRepo{}, rollouts, nil, nil, nil, nil, 10*time.Millisecond)
	scheduler.Start(ctx)
	select {
	case <-rollouts.entered:
	case <-time.After(time.Second):
		t.Fatal("rollout reconciliation did not start")
	}
	deadline := time.Now().Add(time.Second)
	for instances.checks.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if instances.checks.Load() < 2 {
		t.Fatal("a stalled rollout blocked independent instance creation and recovery")
	}
	cancel()
	select {
	case <-rollouts.exited:
	case <-time.After(time.Second):
		t.Fatal("rollout did not observe cancellation")
	}
}
