package services

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
)

func TestLostLastSlotStaysPendingBeforeGatewayRPC(t *testing.T) {
	for _, rt := range []string{RuntimeTypeOpenClaw, RuntimeTypeHermes, RuntimeTypeOpenCode, RuntimeTypeDeepSeekHarness} {
		t.Run(rt, func(t *testing.T) {
			ctx := context.Background()
			workspace := fmt.Sprintf("/workspaces/%s/user-45/instance-10", rt)
			instance := models.Instance{ID: 10, UserID: 45, Type: rt, RuntimeType: RuntimeBackendGateway, InstanceMode: InstanceModeLite, Status: "creating", WorkspacePath: &workspace, RuntimeGeneration: 1}
			instances := newFakeRuntimeInstanceRepo()
			instances.byID[10] = &instance
			endpoint := "http://fixture.runtime"
			stale := models.RuntimePod{ID: 1, RuntimeType: rt, AgentEndpoint: &endpoint, State: "ready", Capacity: 8, UsedSlots: 7}
			pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{1: &stale}, schedulable: []models.RuntimePod{stale}, claimDenied: map[int64]bool{1: true}}
			bindings := newFakeRuntimeBindingRepo()
			agent := &fakeRuntimeAgentClient{}
			deployments := &fakeRuntimeDeploymentService{}
			scheduler := newLiveFixtureRuntimeScheduler(instances, pods, bindings, &fakeRuntimeRolloutRepo{}, agent, &fakeRuntimeEventService{}, nil, deployments, time.Second)
			// A second Pod is already expanding, but has not registered yet.
			deployments.pods[0].DesiredReplicas = 2
			deployments.pods = append(deployments.pods, k8s.RuntimeDeploymentPod{RuntimeType: rt, Namespace: stale.Namespace, DeploymentName: stale.DeploymentName, PodName: "expanding-runtime", PodUID: "expanding-uid", State: "pending", DesiredReplicas: 2})
			errs := scheduler.reconcileCreatingInstanceLocked(ctx, instance)
			if len(errs) != 0 {
				t.Fatalf("unexpected outcome: %v", errs)
			}
			if len(instances.runtimeStates) != 0 || len(agent.createRequests) != 0 || bindings.bindings[10] != nil || deployments.scaleCalls != 0 {
				t.Fatalf("unexpected state: status=%s creates=%d binding=%v scales=%d", instances.runtimeStates[10].status, len(agent.createRequests), bindings.bindings[10], deployments.scaleCalls)
			}
			// The expanding Pod registers before the next scheduler tick.
			registered := *pods.pods[1]
			registered.ID = 2
			registered.PodName = "expanding-runtime"
			registered.PodUID = ptrString("expanding-uid")
			registered.UsedSlots = 0
			pods.pods[2] = &registered
			pods.schedulable = []models.RuntimePod{registered}
			live := deployments.pods[0]
			live.PodName = registered.PodName
			live.PodUID = "expanding-uid"
			live.State = "ready"
			deployments.pods[1] = live
			agent.createResponse = &RuntimeAgentCreateGatewayResponse{GatewayID: "gw-next-tick", Status: "running"}
			if errs := scheduler.reconcileCreatingInstanceLocked(ctx, instance); len(errs) != 0 {
				t.Fatalf("next tick: %v", errs)
			}
			if len(agent.createRequests) != 1 || bindings.bindings[10] == nil || bindings.bindings[10].RuntimePodID != 2 {
				t.Fatalf("next tick did not start exactly once: creates=%d", len(agent.createRequests))
			}
		})
	}
}

func TestCandidateContentionDoesNotHideStartupFailure(t *testing.T) {
	for _, later := range []string{"claim", "pending"} {
		t.Run(later, func(t *testing.T) {
			ctx := context.Background()
			workspace := "/workspaces/openclaw/user-45/instance-10"
			instance := models.Instance{ID: 10, UserID: 45, Type: RuntimeTypeOpenClaw, RuntimeType: RuntimeBackendGateway, InstanceMode: InstanceModeLite, Status: "creating", WorkspacePath: &workspace, RuntimeGeneration: 1}
			instances := newFakeRuntimeInstanceRepo()
			instances.byID[10] = &instance
			a, b := "http://first.runtime", "http://second.runtime"
			first := models.RuntimePod{ID: 1, RuntimeType: RuntimeTypeOpenClaw, AgentEndpoint: &a, State: "ready", Capacity: 8}
			second := models.RuntimePod{ID: 2, RuntimeType: RuntimeTypeOpenClaw, AgentEndpoint: &b, State: "ready", Capacity: 8}
			pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{1: &first, 2: &second}, schedulable: []models.RuntimePod{first, second}, claimDenied: map[int64]bool{2: later == "claim"}}
			bindings := newFakeRuntimeBindingRepo()
			if later == "pending" {
				bindings.bindings[99] = &models.InstanceRuntimeBinding{InstanceID: 99, RuntimePodID: 2, State: "creating"}
			}
			agent := &fakeRuntimeAgentClient{createErr: fmt.Errorf("invalid startup configuration")}
			scheduler := newLiveFixtureRuntimeScheduler(instances, pods, bindings, &fakeRuntimeRolloutRepo{}, agent, &fakeRuntimeEventService{}, nil, &fakeRuntimeDeploymentService{}, time.Second)
			scheduler.gatewayStartInFlightLimit = 1
			err := scheduler.assignInstance(ctx, instance)
			if err == nil || isRuntimeAssignmentPending(err) || runtimeFailureCode(err) != "GATEWAY_START_FAILED" {
				t.Fatalf("startup failure hidden: %v", err)
			}
			if pods.pods[1].UsedSlots != 0 || bindings.bindings[10] != nil {
				t.Fatal("failed startup leaked slot/binding")
			}
		})
	}
}

func TestLostClaimTriesNextCandidate(t *testing.T) {
	ctx := context.Background()
	workspace := "/workspaces/openclaw/user-45/instance-10"
	instance := models.Instance{ID: 10, UserID: 45, Type: RuntimeTypeOpenClaw, RuntimeType: RuntimeBackendGateway, InstanceMode: InstanceModeLite, Status: "creating", WorkspacePath: &workspace, RuntimeGeneration: 1}
	instances := newFakeRuntimeInstanceRepo()
	instances.byID[10] = &instance
	a, b := "http://first.runtime", "http://second.runtime"
	first := models.RuntimePod{ID: 1, RuntimeType: RuntimeTypeOpenClaw, AgentEndpoint: &a, State: "ready", Capacity: 8}
	second := models.RuntimePod{ID: 2, RuntimeType: RuntimeTypeOpenClaw, AgentEndpoint: &b, State: "ready", Capacity: 8}
	pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{1: &first, 2: &second}, schedulable: []models.RuntimePod{first, second}, claimDenied: map[int64]bool{1: true}}
	bindings := newFakeRuntimeBindingRepo()
	agent := &fakeRuntimeAgentClient{createResponse: &RuntimeAgentCreateGatewayResponse{GatewayID: "gw-alternative", Status: "running"}}
	scheduler := newLiveFixtureRuntimeScheduler(instances, pods, bindings, &fakeRuntimeRolloutRepo{}, agent, &fakeRuntimeEventService{}, nil, &fakeRuntimeDeploymentService{}, time.Second)
	scheduler.gatewayStartInFlightLimit = 1
	err := scheduler.assignInstance(ctx, instance)
	if err != nil {
		t.Fatal(err)
	}
	if len(agent.createRequests) != 1 || bindings.bindings[10] == nil || bindings.bindings[10].RuntimePodID != 2 {
		t.Fatal("did not use alternative candidate exactly once")
	}
}

func TestScaleOutSentinelKeepsCreatingInstance(t *testing.T) {
	ctx := context.Background()
	workspace := "/workspaces/hermes/user-45/instance-10"
	instance := models.Instance{ID: 10, UserID: 45, Type: RuntimeTypeHermes, RuntimeType: RuntimeBackendGateway, InstanceMode: InstanceModeLite, Status: "creating", WorkspacePath: &workspace, RuntimeGeneration: 1}
	instances := newFakeRuntimeInstanceRepo()
	instances.byID[10] = &instance
	endpoint := "http://full.runtime"
	full := models.RuntimePod{ID: 1, RuntimeType: RuntimeTypeHermes, AgentEndpoint: &endpoint, State: "ready", Capacity: 8, UsedSlots: 8, DesiredReplicas: 1}
	pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{1: &full}}
	bindings := newFakeRuntimeBindingRepo()
	agent := &fakeRuntimeAgentClient{}
	base := &fakeRuntimeDeploymentService{}
	scheduler := newLiveFixtureRuntimeScheduler(instances, pods, bindings, &fakeRuntimeRolloutRepo{}, agent, &fakeRuntimeEventService{}, nil, base, time.Second)
	scales := &monotonicRuntimeScaleFake{fakeRuntimeDeploymentService: base, replicas: 1, scaleOutErr: k8s.ErrRuntimeScaleOutPending}
	scheduler.deployments = scales
	if errs := scheduler.reconcileCreatingInstanceLocked(ctx, instance); len(errs) != 0 {
		t.Fatalf("scale pending became terminal: %v", errs)
	}
	if len(scales.floors) != 1 || len(instances.runtimeStates) != 0 || len(agent.createRequests) != 0 || bindings.bindings[10] != nil {
		t.Fatalf("pending assignment leaked or did not scale: floors=%v state=%v", scales.floors, instances.runtimeStates)
	}
	scales.scaleOutErr = context.DeadlineExceeded
	if errs := scheduler.reconcileCreatingInstanceLocked(ctx, instance); len(errs) != 0 {
		t.Fatalf("initial GET timeout became terminal: %v", errs)
	}
	base.listErr = context.DeadlineExceeded
	if errs := scheduler.reconcileCreatingInstanceLocked(ctx, instance); len(errs) != 0 {
		t.Fatalf("inventory timeout became terminal: %v", errs)
	}
	base.listErr = nil
	// A refused protected write may wrap a timeout; only the local marker grants pending.
	scales.scaleOutErr = errors.Join(k8s.ErrRuntimeScaleOutRejected, fmt.Errorf("protected Deployment changed: %w", context.DeadlineExceeded))
	if errs := scheduler.reconcileCreatingInstanceLocked(ctx, instance); len(errs) == 0 {
		t.Fatal("unmarked ScaleOut rejection became pending")
	}
	if instances.runtimeStates[10].status != "error" {
		t.Fatal("rejection did not retain terminal classification")
	}

}
