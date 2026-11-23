package services

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
)

type strictSchedulerAgent struct{ *fakeRuntimeAgentClient }

func (*strictSchedulerAgent) RequiresConfirmedGatewayStop() bool { return true }

func TestSlotSchedulerRejectsStaleInventoryBeforeReservation(t *testing.T) {
	for _, runtimeType := range []string{RuntimeTypeOpenClaw, RuntimeTypeHermes, RuntimeTypeOpenCode, RuntimeTypeDeepSeekHarness} {
		for _, scenario := range []string{"deleted", "replaced", "not-ready", "missing-uid", "missing-live-uid", "different-name", "different-namespace", "inventory-error", "ready"} {
			t.Run(runtimeType+"/"+scenario, func(t *testing.T) {
				endpoint, uid, ip := "https://10.0.0.2:19090", "selected-uid", "10.0.0.2"
				now := time.Now()
				pod := models.RuntimePod{ID: 9, RuntimeType: runtimeType, Namespace: "clawmanager-system", PodName: "pool-pod", PodUID: &uid, DeploymentName: runtimeType + "-runtime", AgentEndpoint: &endpoint, State: "ready", Capacity: 8, PodIP: &ip, LastSeenAt: &now, ImageRef: "repo/" + runtimeType + ":test"}
				live := k8s.RuntimeDeploymentPod{RuntimeType: runtimeType, Namespace: pod.Namespace, PodName: pod.PodName, PodUID: uid, DeploymentName: pod.DeploymentName, State: "ready", PodIP: pod.PodIP, ImageRef: pod.ImageRef}
				deployments := &fakeRuntimeDeploymentService{pods: []k8s.RuntimeDeploymentPod{live}}
				switch scenario {
				case "deleted":
					deployments.pods = nil
				case "replaced":
					deployments.pods[0].PodUID = "replacement-uid"
				case "not-ready":
					deployments.pods[0].State = "pending"
				case "missing-uid":
					pod.PodUID = nil
				case "missing-live-uid":
					deployments.pods[0].PodUID = ""
				case "different-name":
					deployments.pods[0].PodName = "another-pod"
				case "different-namespace":
					deployments.pods[0].Namespace = "another-namespace"
				case "inventory-error":
					deployments.listErr = errors.New("inventory unavailable")
				}
				pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{9: &pod}, schedulable: []models.RuntimePod{pod}}
				bindings := newFakeRuntimeBindingRepo()
				agent := &strictSchedulerAgent{&fakeRuntimeAgentClient{createResponse: &RuntimeAgentCreateGatewayResponse{GatewayID: "gw", Status: "running"}}}
				root, rootErr := filepath.EvalSymlinks(t.TempDir())
				if rootErr != nil {
					t.Fatal(rootErr)
				}
				scheduler := NewRuntimeScheduler(newFakeRuntimeInstanceRepo(), pods, bindings, &fakeRuntimeRolloutRepo{}, agent, nil, nil, deployments, time.Second, WithRuntimeSchedulerWorkspaceRoot(root), WithRuntimeSchedulerGatewayEnvBuilder(func(*models.Instance) (map[string]string, error) { return map[string]string{}, nil }))
				instance := models.Instance{ID: 17, UserID: 45, Type: runtimeType, RuntimeType: RuntimeBackendGateway, InstanceMode: InstanceModeLite, Status: "creating", RuntimeGeneration: 3, WorkspacePath: stringPtrOrNil("/workspace")}
				err := scheduler.assignInstance(context.Background(), instance)
				if scenario != "ready" {
					if len(pods.claims) != 0 || len(bindings.bindings) != 0 || len(agent.createRequests) != 0 {
						t.Fatalf("unsafe candidate reserved or created: claims=%v bindings=%v creates=%v", pods.claims, bindings.bindings, agent.createRequests)
					}
					if err == nil {
						t.Fatal("unavailable inventory accepted")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if len(agent.createRequests) != 1 || agent.createRequests[0].req.PodUID != uid {
					t.Fatalf("create did not pin selected Pod UID: %+v", agent.createRequests)
				}
			})
		}
	}
}

func TestSlotSchedulerRequiresInventoryButLegacyAllowsMissingUID(t *testing.T) {
	pods := []models.RuntimePod{{ID: 9, State: "ready"}}
	legacy := &RuntimeScheduler{agentClient: &fakeRuntimeAgentClient{}}
	got, err := legacy.annotateAndFilterOrdinaryRuntimePods(context.Background(), RuntimeTypeOpenCode, pods)
	if err != nil || len(got) != 1 {
		t.Fatalf("legacy behavior changed: %v %v", got, err)
	}
	strict := &RuntimeScheduler{agentClient: &strictSchedulerAgent{&fakeRuntimeAgentClient{}}}
	got, err = strict.annotateAndFilterOrdinaryRuntimePods(context.Background(), RuntimeTypeOpenCode, pods)
	if err == nil || len(got) != 0 {
		t.Fatalf("strict scheduler accepted unknown inventory: %v %v", got, err)
	}
	got, err = strict.verifiedOrdinaryRuntimePods(context.Background(), RuntimeTypeOpenCode, pods)
	if err == nil || len(got) != 0 {
		t.Fatalf("strict assignment accepted unknown inventory: %v %v", got, err)
	}
	if err := strict.verifyOrdinaryRuntimePod(context.Background(), pods[0]); err == nil {
		t.Fatal("strict start accepted unknown inventory")
	}
	got, err = legacy.verifiedOrdinaryRuntimePods(context.Background(), RuntimeTypeOpenCode, pods)
	if err != nil || len(got) != 1 {
		t.Fatalf("legacy assignment changed: %v %v", got, err)
	}
	if err := legacy.verifyOrdinaryRuntimePod(context.Background(), pods[0]); err != nil {
		t.Fatalf("legacy start changed: %v", err)
	}
}

func TestFreshRuntimePoolIsSpecificToItsApprovedInstance(t *testing.T) {
	active := true
	ordinaryUID, freshUID := "ordinary", "fresh"
	ip, endpoint := "10.2.0.2", "https://10.2.0.2:19090"
	now := time.Now()
	ordinary := models.RuntimePod{ID: 1, RuntimeType: RuntimeTypeOpenCode, Namespace: "tenant", PodName: "ordinary", DeploymentName: "pool", PodUID: &ordinaryUID, PodIP: &ip, AgentEndpoint: &endpoint, ImageRef: "registry/opencode:old", State: "ready", KubernetesReady: true, LastSeenAt: &now}
	fresh := ordinary
	fresh.ID, fresh.PodName, fresh.DeploymentName, fresh.PodUID, fresh.ImageRef = 2, "fresh", "pool-new-7", &freshUID, "registry/opencode:new"
	live := []models.RuntimePod{ordinary, fresh}
	live[1].PoolPurpose, live[1].SchedulingEnabled = "fresh-instance-7", &active
	s := &RuntimeScheduler{runtimeNamespace: ordinary.Namespace}
	for _, tc := range []struct {
		id   int
		want int64
	}{{0, 1}, {6, 1}, {7, 2}} {
		got := s.currentRuntimePodsForInstance([]models.RuntimePod{ordinary, fresh}, live, tc.id)
		if len(got) != 1 || got[0].ID != tc.want {
			t.Fatalf("instance %d got %+v, want Pod %d", tc.id, got, tc.want)
		}
	}
	live[1].KubernetesReady = false
	if got := s.currentRuntimePodsForInstance([]models.RuntimePod{ordinary, fresh}, live, 7); len(got) != 0 {
		t.Fatalf("approved scope fell back to an unrelated image: %+v", got)
	}
}
