package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
)

func TestSlotPortAllocationHonorsPhysicalCapacity(t *testing.T) {
	for _, kind := range []string{RuntimeTypeOpenClaw, RuntimeTypeHermes, RuntimeTypeOpenCode, RuntimeTypeDeepSeekHarness} {
		for _, scenario := range []string{"full", "last-free", "last-excluded", "zero-capacity", "legacy"} {
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				repo := newFakeRuntimeBindingRepo()
				block := RuntimeGatewayPortBlockSize(kind)
				count := 8
				if scenario == "last-free" || scenario == "last-excluded" {
					count = 7
				}
				for i := 0; i < count; i++ {
					repo.bindings[100+i] = &models.InstanceRuntimeBinding{InstanceID: 100 + i, RuntimePodID: 9, GatewayPort: RuntimeGatewayPortStart + i*block, State: "running"}
				}
				var agent RuntimeAgentClient = &strictSchedulerAgent{&fakeRuntimeAgentClient{}}
				if scenario == "legacy" {
					agent = &fakeRuntimeAgentClient{}
				}
				s := NewRuntimeScheduler(nil, nil, repo, nil, agent, nil, nil, nil, time.Second)
				pod := models.RuntimePod{ID: 9, RuntimeType: kind, Capacity: 8, UsedSlots: 0} // A heartbeat can lag SQL reservations.
				if scenario == "zero-capacity" {
					pod.Capacity = 0
				}
				excluded := map[int]struct{}{}
				if scenario == "last-excluded" {
					excluded[RuntimeGatewayPortStart+7*block] = struct{}{}
				}
				binding, err := s.reserveGatewayPortExcludingPorts(context.Background(), models.Instance{ID: 999, RuntimeGeneration: 1}, kind, pod, "/workspace", excluded)
				if scenario == "last-free" || scenario == "legacy" {
					want := RuntimeGatewayPortStart + count*block
					if err != nil || binding == nil || binding.GatewayPort != want {
						t.Fatalf("port allocation=%+v error=%v; want %d", binding, err, want)
					}
				} else if err == nil || binding != nil || repo.bindings[999] != nil {
					t.Fatalf("allocated a nonexistent slot: binding=%+v error=%v", binding, err)
				}
			})
		}
	}
}

func TestSlotSchedulerSkipsFullBindingsWhenHeartbeatLags(t *testing.T) {
	for _, kind := range []string{RuntimeTypeOpenClaw, RuntimeTypeHermes, RuntimeTypeOpenCode, RuntimeTypeDeepSeekHarness} {
		t.Run(kind, func(t *testing.T) {
			repo := newFakeRuntimeBindingRepo()
			block := RuntimeGatewayPortBlockSize(kind)
			for i := 0; i < 8; i++ {
				repo.bindings[100+i] = &models.InstanceRuntimeBinding{InstanceID: 100 + i, RuntimePodID: 9, RuntimeType: kind, GatewayPort: RuntimeGatewayPortStart + i*block, State: "running", Generation: 1}
			}
			pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{}}
			deployments := &fakeRuntimeDeploymentService{}
			for _, id := range []int64{9, 10} {
				endpoint, uid, ip := fmt.Sprintf("https://10.0.0.%d:19090", id), fmt.Sprintf("uid-%d", id), fmt.Sprintf("10.0.0.%d", id)
				now := time.Now()
				p := models.RuntimePod{ID: id, RuntimeType: kind, Namespace: "clawmanager-system", PodName: fmt.Sprintf("pool-%d", id), DeploymentName: kind + "-runtime", PodUID: &uid, AgentEndpoint: &endpoint, State: "ready", Capacity: 8, PodIP: &ip, LastSeenAt: &now, ImageRef: "repo/" + kind + ":test"}
				pods.pods[id] = &p
				pods.schedulable = append(pods.schedulable, p)
				deployments.pods = append(deployments.pods, k8s.RuntimeDeploymentPod{RuntimeType: kind, Namespace: p.Namespace, PodName: p.PodName, PodUID: uid, DeploymentName: p.DeploymentName, State: "ready", PodIP: p.PodIP, ImageRef: p.ImageRef})
			}
			fake := &fakeRuntimeAgentClient{createResponse: &RuntimeAgentCreateGatewayResponse{GatewayID: "gw-999-1", Status: "running"}}
			agent := &strictSchedulerAgent{fake}
			s := NewRuntimeScheduler(newFakeRuntimeInstanceRepo(), pods, repo, &fakeRuntimeRolloutRepo{}, agent, nil, nil, deployments, time.Second, WithRuntimeSchedulerWorkspaceRoot(canonicalCompatibilityTestDir(t)), WithRuntimeSchedulerGatewayEnvBuilder(func(*models.Instance) (map[string]string, error) { return map[string]string{}, nil }))
			instance := models.Instance{ID: 999, UserID: 1, Type: kind, RuntimeType: RuntimeBackendGateway, InstanceMode: InstanceModeLite, Status: "creating", RuntimeGeneration: 1, WorkspacePath: stringPtrOrNil("/workspace")}
			if err := s.assignInstance(context.Background(), instance); err != nil {
				t.Fatal(err)
			}
			if len(fake.createRequests) != 1 || fake.createRequests[0].endpoint != *pods.pods[10].AgentEndpoint {
				t.Fatalf("request went to full Pod: %+v", fake.createRequests)
			}
			if b := repo.bindings[999]; b == nil || b.RuntimePodID != 10 || b.GatewayPort != RuntimeGatewayPortStart {
				t.Fatalf("invalid final reservation: %+v", b)
			}
			if pods.releases[9] != 1 {
				t.Fatalf("full Pod's transient claim was not released: %v", pods.releases)
			}
			for i := 1; i < 8; i++ {
				repo.bindings[200+i] = &models.InstanceRuntimeBinding{InstanceID: 200 + i, RuntimePodID: 10, RuntimeType: kind, GatewayPort: RuntimeGatewayPortStart + i*block, State: "running", Generation: 1}
			}
			instance.ID = 1000
			err := s.assignInstance(context.Background(), instance)
			if !isRuntimeAssignmentPending(err) || len(fake.createRequests) != 1 || repo.bindings[1000] != nil {
				t.Fatalf("full pools must wait for capacity without publishing a request: error=%v calls=%d binding=%+v", err, len(fake.createRequests), repo.bindings[1000])
			}
		})
	}
}
