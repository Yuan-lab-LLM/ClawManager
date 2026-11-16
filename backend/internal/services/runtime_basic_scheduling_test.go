package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
)

func TestBasicSchedulingWaitsForOrdinaryPodStartup(t *testing.T) {
	for _, state := range []string{"pending", "ready"} {
		t.Run(state, func(t *testing.T) {
			d := &fakeRuntimeDeploymentService{pods: []k8s.RuntimeDeploymentPod{{RuntimeType: RuntimeTypeHermes, Namespace: "runtime", DeploymentName: "hermes-runtime", PodName: "new-pod", PodUID: "new-uid", State: state, DesiredReplicas: 1}}}
			s := NewRuntimeScheduler(newFakeRuntimeInstanceRepo(), &fakeRuntimePodRepo{}, newFakeRuntimeBindingRepo(), &fakeRuntimeRolloutRepo{}, &fakeRuntimeAgentClient{}, nil, nil, d, time.Second, WithRuntimeSchedulerNamespace("runtime"))
			workspace := "/workspaces/hermes/user-1/instance-1"
			i := models.Instance{ID: 1, UserID: 1, Type: RuntimeTypeHermes, RuntimeType: RuntimeBackendGateway, InstanceMode: InstanceModeLite, Status: "creating", RuntimeGeneration: 1, WorkspacePath: &workspace}
			if err := s.assignInstance(t.Context(), i); !isRuntimeAssignmentPending(err) {
				t.Fatalf("Pod %s before registration must wait, got %v", state, err)
			}
			if d.scaleCalls != 0 {
				t.Fatal("starting Pod caused repeated expansion")
			}
		})
	}
}

func TestBasicSchedulingDoesNotSyncAnotherBindingGeneration(t *testing.T) {
	r := newFakeRuntimeInstanceRepo()
	b := newFakeRuntimeBindingRepo()
	workspace := "/workspaces/hermes/user-1/instance-1"
	i := models.Instance{ID: 1, UserID: 1, Type: RuntimeTypeHermes, RuntimeType: RuntimeBackendGateway, InstanceMode: InstanceModeLite, Status: "running", RuntimeGeneration: 4, WorkspacePath: &workspace}
	b.bindings[1] = &models.InstanceRuntimeBinding{InstanceID: 1, RuntimeType: RuntimeTypeHermes, Generation: 3, State: "error"}
	s := NewRuntimeScheduler(r, &fakeRuntimePodRepo{}, b, &fakeRuntimeRolloutRepo{}, &fakeRuntimeAgentClient{}, nil, nil, nil, time.Second)
	if errs := s.reconcileDesiredInstance(t.Context(), i); len(errs) == 0 {
		t.Fatal("mismatched binding was not reported")
	}
	if len(r.runtimeStates) != 0 || b.bindings[1] == nil {
		t.Fatal("old binding changed the current generation or lost ownership")
	}
}

func TestBasicSchedulingHoldsInterruptedRecoveryBeforeGenerationAdvance(t *testing.T) {
	s, r, bindings, agent, instance, _ := recoveryFixture()
	instance.Status = "creating"
	r.byID[instance.ID] = &instance
	r.state = &models.InstanceRuntimeRecovery{InstanceID: instance.ID, Generation: instance.RuntimeGeneration, Phase: "source_stopped", Attempts: 1}
	delete(bindings.bindings, instance.ID)
	errs := s.reconcileCreatingInstanceLocked(t.Context(), instance)
	if len(errs) == 0 || !strings.Contains(errs[0].Error(), "interrupted Runtime recovery") {
		t.Fatalf("recovery ownership was not reported as the blocker: %v", errs)
	}
	if len(agent.createRequests) != 0 || len(agent.deleteRequests) != 0 || r.byID[instance.ID].RuntimeGeneration != instance.RuntimeGeneration {
		t.Fatal("ordinary placement bypassed interrupted recovery generation ownership")
	}
}

type blockedSchedulingRollouts struct {
	*fakeRuntimeRolloutRepo
	release chan struct{}
}

func (r *blockedSchedulingRollouts) ListActive(ctx context.Context, _ string) ([]models.RuntimeRollout, error) {
	select {
	case <-r.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestBasicSchedulingDoesNotWaitForMaintenance(t *testing.T) {
	rollouts := &blockedSchedulingRollouts{fakeRuntimeRolloutRepo: &fakeRuntimeRolloutRepo{}, release: make(chan struct{})}
	defer close(rollouts.release)
	s := NewRuntimeScheduler(newFakeRuntimeInstanceRepo(), &fakeRuntimePodRepo{}, newFakeRuntimeBindingRepo(), rollouts, &fakeRuntimeAgentClient{}, nil, nil, nil, time.Second)
	done := make(chan error, 1)
	go func() { done <- s.reconcile(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("ordinary scheduling waited for Runtime upgrade work")
	}
}

func TestBasicSchedulingErrorsRequireExplicitStart(t *testing.T) {
	for _, code := range []string{"RUNTIME_POOL_UNAVAILABLE", "GATEWAY_START_FAILED", "MODEL_CONFIGURATION_REQUIRED"} {
		t.Run(code, func(t *testing.T) {
			instances := newFakeRuntimeInstanceRepo()
			workspace := "/workspaces/hermes/user-1/instance-1"
			message := "no schedulable hermes runtime pod"
			instance := models.Instance{ID: 1, UserID: 1, Type: RuntimeTypeHermes, RuntimeType: RuntimeBackendGateway, InstanceMode: InstanceModeLite, Status: "error", RuntimeGeneration: 4, WorkspacePath: &workspace, RuntimeErrorCode: &code, RuntimeErrorMessage: &message}
			agent := &fakeRuntimeAgentClient{}
			s := NewRuntimeScheduler(instances, &fakeRuntimePodRepo{}, newFakeRuntimeBindingRepo(), &fakeRuntimeRolloutRepo{}, agent, nil, nil, nil, time.Second)
			if errs := s.reconcileDesiredInstance(context.Background(), instance); len(errs) != 0 {
				t.Fatalf("error instance was retried: %v", errs)
			}
			if len(instances.runtimeStates) != 0 || len(agent.createRequests) != 0 {
				t.Fatal("error instance changed without an explicit start")
			}
		})
	}
}

type failingBasicScale struct {
	*fakeRuntimeDeploymentService
	calls map[string]bool
}

func (d *failingBasicScale) Scale(ctx context.Context, namespace, name string, replicas int32) error {
	d.calls[name] = true
	return errors.New(name + " unavailable")
}

func TestBasicSchedulingExpansionFailuresAreTypeScoped(t *testing.T) {
	pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{}}
	d := &failingBasicScale{fakeRuntimeDeploymentService: &fakeRuntimeDeploymentService{}, calls: map[string]bool{}}
	var instances []models.Instance
	for n, runtimeType := range []string{RuntimeTypeOpenClaw, RuntimeTypeHermes, RuntimeTypeOpenCode, RuntimeTypeDeepSeekHarness} {
		id := n + 1
		workspace := "/workspace"
		instances = append(instances, models.Instance{ID: id, UserID: 1, Type: runtimeType, RuntimeType: RuntimeBackendGateway, InstanceMode: InstanceModeLite, Status: "creating", WorkspacePath: &workspace})
		pods.pods[int64(id)] = &models.RuntimePod{ID: int64(id), RuntimeType: runtimeType, Namespace: "runtime", DeploymentName: runtimeType + "-runtime", State: "ready", Capacity: 1, UsedSlots: 1, DesiredReplicas: 1}
	}
	s := NewRuntimeScheduler(newFakeRuntimeInstanceRepo(), pods, newFakeRuntimeBindingRepo(), &fakeRuntimeRolloutRepo{}, &fakeRuntimeAgentClient{}, nil, nil, d, time.Second)
	if err := s.scaleOutForPendingBacklog(context.Background(), instances); err == nil {
		t.Fatal("lost per-type expansion errors")
	}
	if len(d.calls) != 4 {
		t.Fatalf("only %d of 4 types were attempted: %v", len(d.calls), d.calls)
	}
}
