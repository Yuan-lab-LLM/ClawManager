package services

import (
	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
	"context"
	"errors"
	"testing"
	"time"
)

type monotonicRuntimeScaleFake struct {
	*fakeRuntimeDeploymentService
	replicas    int32
	floors      []fakeScaleCall
	scaleOutErr error
}

func (f *monotonicRuntimeScaleFake) Scale(ctx context.Context, namespace, name string, replicas int32) error {
	f.replicas = replicas
	return f.fakeRuntimeDeploymentService.Scale(ctx, namespace, name, replicas)
}
func (f *monotonicRuntimeScaleFake) ScaleOut(ctx context.Context, namespace, name string, floor int32) error {
	f.floors = append(f.floors, fakeScaleCall{namespace: namespace, name: name, replicas: floor})
	if f.scaleOutErr != nil {
		return f.scaleOutErr
	}
	if f.replicas < floor {
		f.replicas = floor
	}
	return nil
}

func TestRuntimeSchedulerAutomaticScaleOutPreservesManualCapacity(t *testing.T) {
	for _, path := range []string{"capacity", "backlog"} {
		for _, mode := range []string{"optional", "legacy", "optional-error"} {
			t.Run(path+"/"+mode, func(t *testing.T) {
				pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{
					1: {ID: 1, RuntimeType: RuntimeTypeHermes, Namespace: "runtime", DeploymentName: "hermes-runtime", State: "ready", Capacity: 1, UsedSlots: 1, DesiredReplicas: 2},
					2: {ID: 2, RuntimeType: RuntimeTypeHermes, Namespace: "runtime", DeploymentName: "hermes-runtime", State: "ready", Capacity: 1, UsedSlots: 1, DesiredReplicas: 2},
				}}
				legacy := &fakeRuntimeDeploymentService{}
				optional := &monotonicRuntimeScaleFake{fakeRuntimeDeploymentService: legacy, replicas: 6}
				var deployments k8s.RuntimeDeploymentService = optional
				if mode == "legacy" {
					deployments = legacy
				}
				writeErr := errors.New("unconfirmed scale write")
				if mode == "optional-error" {
					optional.scaleOutErr = writeErr
				}
				scheduler := NewRuntimeScheduler(newFakeRuntimeInstanceRepo(), pods, newFakeRuntimeBindingRepo(), &fakeRuntimeRolloutRepo{}, &fakeRuntimeAgentClient{}, nil, nil, deployments, time.Second)
				var scaled bool
				var err error
				if path == "capacity" {
					scaled, err = scheduler.scaleOutIfAtCapacity(context.Background(), RuntimeTypeHermes)
				} else {
					scaled, err = scheduler.scaleOutIfBacklogExceedsCapacity(context.Background(), RuntimeTypeHermes, 1)
				}
				if mode == "optional-error" {
					if !errors.Is(err, writeErr) || scaled || legacy.scaleCalls != 0 || len(optional.floors) != 1 {
						t.Fatalf("optional error fell back/retried: scaled=%v err=%v legacy=%d floors=%v", scaled, err, legacy.scaleCalls, optional.floors)
					}
					return
				}
				if !scaled || err != nil {
					t.Fatalf("automatic scale-out=(%v,%v)", scaled, err)
				}
				if mode == "legacy" {
					if legacy.scaleCalls != 1 || legacy.scales[0].replicas != 3 {
						t.Fatalf("legacy fallback changed: %+v", legacy.scales)
					}
				} else if legacy.scaleCalls != 0 || len(optional.floors) != 1 || optional.floors[0].replicas != 3 || optional.replicas != 6 {
					t.Fatalf("stale automatic floor overwrote manual replicas: current=%d legacy=%+v floors=%+v", optional.replicas, legacy.scales, optional.floors)
				}
			})
		}
	}
}
