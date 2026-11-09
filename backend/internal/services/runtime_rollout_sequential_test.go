package services

import (
	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
	"context"
	"errors"
	"testing"
	"time"
)

type checkpointRepo struct {
	fakeRuntimeRolloutRepo
	raw  string
	fail bool
}

func (r *checkpointRepo) LoadRolloutCheckpoint(context.Context, int64) (string, error) {
	return r.raw, nil
}
func (r *checkpointRepo) SaveRolloutCheckpoint(_ context.Context, _ int64, raw string) error {
	if r.fail {
		return errors.New("storage unavailable")
	}
	r.raw = raw
	return nil
}

type checkpointDeployment struct {
	fakeRuntimeDeploymentService
	targets []k8s.RuntimeRolloutTarget
}

func (d *checkpointDeployment) RolloutTargets(context.Context, string, string) ([]k8s.RuntimeRolloutTarget, error) {
	return d.targets, nil
}

func TestPoolRolloutCheckpointsBeforeMutationAndResumes(t *testing.T) {
	ctx := context.Background()
	r := &models.RuntimeRollout{ID: 1, RuntimeType: RuntimeTypeOpenCode, TargetImageRef: "new", Status: "pending"}
	repo := &checkpointRepo{fakeRuntimeRolloutRepo: fakeRuntimeRolloutRepo{rollouts: map[int64]*models.RuntimeRollout{1: r}}}
	dep := &checkpointDeployment{targets: []k8s.RuntimeRolloutTarget{
		{Namespace: "tenant", Name: "pool-a", UID: "a", Image: "old"},
		{Namespace: "tenant", Name: "pool-b", UID: "b", Image: "old"},
	}}
	s := &RuntimeScheduler{rolloutRepo: repo, deployments: dep, runtimeNamespace: "tenant"}
	repo.fail = true
	if _, err := s.startPoolRollout(ctx, r); err == nil {
		t.Fatal("must fail before changing deployments")
	}
	if len(dep.rolloutImageCalls) != 0 {
		t.Fatal("changed pool before durable plan")
	}
	repo.fail = false
	if handled, err := s.startPoolRollout(ctx, r); !handled || err != nil {
		t.Fatal(handled, err)
	}
	p, err := LoadRuntimePoolProgress(ctx, repo, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.advancePoolRollout(ctx, *r, p); err != nil {
		t.Fatal(err)
	}
	if len(dep.rolloutImageCalls) != 0 {
		t.Fatal("step intent must persist first")
	}
	// Simulate a fresh scheduler resuming only the saved state.
	s = &RuntimeScheduler{rolloutRepo: repo, deployments: dep, runtimeNamespace: "tenant"}
	p, _ = LoadRuntimePoolProgress(ctx, repo, 1)
	if err = s.advancePoolRollout(ctx, *r, p); err != nil {
		t.Fatal(err)
	}
	if len(dep.rolloutImageCalls) != 1 || dep.rolloutImageCalls[0].name != "pool-a" {
		t.Fatal(dep.rolloutImageCalls)
	}
	p, _ = LoadRuntimePoolProgress(ctx, repo, 1)
	if err = s.advancePoolRollout(ctx, *r, p); err != nil {
		t.Fatal(err)
	}
	if p.Completed != 0 || len(dep.rolloutImageCalls) != 1 {
		t.Fatal("advanced before readiness")
	}
	dep.targets[0].Image, dep.targets[0].Ready = "new", true
	if err = s.advancePoolRollout(ctx, *r, p); err != nil {
		t.Fatal(err)
	}
	if p.Completed != 1 || len(dep.rolloutImageCalls) != 1 {
		t.Fatal("must finish one pool at a time")
	}
	dep.targets[1].UID = "replacement"
	if err = s.advancePoolRollout(ctx, *r, p); err == nil {
		t.Fatal("replaced target must stop the rollout")
	}
	if len(dep.rolloutImageCalls) != 1 {
		t.Fatal("changed replaced deployment")
	}
}

func TestPoolRolloutTimeoutAndOpenClawIsolation(t *testing.T) {
	repo := &checkpointRepo{}
	dep := &checkpointDeployment{targets: []k8s.RuntimeRolloutTarget{{Namespace: "tenant", Name: "a", UID: "uid", Image: "old"}}}
	s := &RuntimeScheduler{rolloutRepo: repo, deployments: dep}
	if handled, err := s.startPoolRollout(context.Background(), &models.RuntimeRollout{RuntimeType: RuntimeTypeOpenClaw}); handled || err != nil {
		t.Fatal("must retain OpenClaw safety state machine")
	}
	p := &RuntimePoolProgress{Version: 1, Targets: dep.targets, Applying: true, StepStarted: time.Now().Add(-11 * time.Minute)}
	if err := s.advancePoolRollout(context.Background(), models.RuntimeRollout{ID: 1, TargetImageRef: "new"}, p); err == nil {
		t.Fatal("timeout must stop rollout")
	}
	if len(dep.rolloutImageCalls) != 0 {
		t.Fatal("timeout changed pool")
	}
	repo.raw = `{"version":99,"targets":[]}`
	if _, err := LoadRuntimePoolProgress(context.Background(), repo, 1); err == nil {
		t.Fatal("unknown checkpoint accepted")
	}
}
