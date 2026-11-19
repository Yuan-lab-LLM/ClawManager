package services

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"k8s.io/client-go/kubernetes/fake"
)

func TestPhysicalExpansionRequiresCompatibleWorkerProof(t *testing.T) {
	for _, scenario := range []string{"unknown-protocol", "unknown-protocol-full", "quarantined", "free"} {
		t.Run(scenario, func(t *testing.T) {
			physical := auditRoutingPod(1, 1)
			endpoint, uid := "https://10.0.0.2:19090", string(physical.UID)
			snapshot := auditFreeSnapshot()
			switch scenario {
			case "unknown-protocol", "unknown-protocol-full":
				delete(snapshot, "worker_epoch")
				delete(snapshot, "idle_confirmed")
			case "quarantined":
				snapshot["quarantined"] = true
			}
			client := NewSlotRuntimeAgentClientWithHTTPClient("control", fake.NewSimpleClientset(physical), "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet {
					t.Fatalf("capacity query mutated Runtime: %s", r.Method)
				}
				raw, _ := json.Marshal(snapshot)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
			})}).(*slotRuntimeAgentClient)
			pod := models.RuntimePod{ID: 9, RuntimeType: RuntimeTypeHermes, PodName: physical.Name, Namespace: physical.Namespace, DeploymentName: "hermes-runtime", PodIP: ptrString("10.0.0.2"), PodUID: &uid, AgentEndpoint: &endpoint, State: "ready", Capacity: 1}
			if scenario == "unknown-protocol-full" {
				pod.UsedSlots = 1
			}
			pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{9: &pod}}
			deployments := &fakeRuntimeDeploymentService{}
			s := newLiveFixtureRuntimeScheduler(newFakeRuntimeInstanceRepo(), pods, newFakeRuntimeBindingRepo(), nil, client, nil, nil, deployments, time.Second, WithRuntimeSchedulerNamespace("runtime"))
			if !strings.HasPrefix(scenario, "unknown-protocol") {
				if _, err := s.allocatableGatewaySlots(context.Background(), *pods.pods[9]); err != nil {
					t.Fatalf("test capacity inventory invalid: %v", err)
				}
			}
			var scaled bool
			if scenario == "unknown-protocol-full" {
				instance := models.Instance{ID: 17, UserID: 1, Type: RuntimeTypeHermes, RuntimeType: RuntimeBackendGateway, InstanceMode: InstanceModeLite, Status: "creating", RuntimeGeneration: 1, WorkspacePath: ptrString("/workspace")}
				err := s.assignInstance(context.Background(), instance)
				if err == nil {
					t.Fatal("full unknown pool accepted assignment")
				}
				scaled = errors.Is(err, errRuntimeScaleOutPending)
			} else {
				var err error
				scaled, err = s.scaleOutIfBacklogExceedsCapacity(context.Background(), RuntimeTypeHermes, 1)
				if err != nil {
					t.Fatal(err)
				}
			}
			want := scenario == "quarantined"
			if scaled != want || (deployments.scaleCalls == 1) != want {
				t.Fatalf("scenario=%s scaled=%v calls=%d", scenario, scaled, deployments.scaleCalls)
			}
		})
	}
}

type layoutCapacityAgent struct {
	*occupiedPortsAgent
	physical map[int]struct{}
}

func (a *layoutCapacityAgent) physicalGatewayPorts(context.Context, models.RuntimePod) (map[int]struct{}, error) {
	return a.physical, nil
}
func TestPhysicalExpansionRejectsConfiguredSubsetOfLayout(t *testing.T) {
	agent := &layoutCapacityAgent{occupiedPortsAgent: &occupiedPortsAgent{strictSchedulerAgent: &strictSchedulerAgent{&fakeRuntimeAgentClient{}}, ports: map[int]struct{}{20000: {}}}, physical: map[int]struct{}{20000: {}, 20001: {}}}
	s := NewRuntimeScheduler(newFakeRuntimeInstanceRepo(), &fakeRuntimePodRepo{}, newFakeRuntimeBindingRepo(), nil, agent, nil, nil, nil, time.Second, WithRuntimeSchedulerGatewayPortRange(20000, 20000))
	if free, err := s.allocatableGatewaySlots(context.Background(), models.RuntimePod{ID: 9, RuntimeType: RuntimeTypeHermes, Capacity: 2}); err == nil {
		t.Fatalf("configured subset gained expansion authority: free=%d", free)
	}
}

func TestPhysicalExpansionCountsOnlyVerifiedEightSlotCapacity(t *testing.T) {
	ctx := context.Background()
	for _, scenario := range []string{"unknown-slot", "full-except-unverified", "quarantine", "slot-rpc-unreachable", "stale-sql-binding", "unknown-pod"} {
		t.Run(scenario, func(t *testing.T) {
			physical := auditRoutingPod(8, 1)
			endpoint, uid := "https://10.0.0.2:19090", string(physical.UID)
			client := NewSlotRuntimeAgentClientWithHTTPClient("control", fake.NewSimpleClientset(physical), "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				snapshot := auditFreeSnapshot()
				if strings.Contains(r.URL.Path, "/slot-0/") {
					switch scenario {
					case "unknown-slot", "full-except-unverified":
						delete(snapshot, "idle_confirmed")
					case "quarantine":
						snapshot["quarantined"] = true
					case "slot-rpc-unreachable":
						return nil, errors.New("unreachable worker")
					}
				}
				if scenario == "unknown-pod" {
					return nil, errors.New("unreachable coordinator")
				}
				if !strings.Contains(r.URL.Path, "/slot-0/") && (!strings.Contains(r.URL.Path, "/slot-7/") || scenario == "full-except-unverified") {
					snapshot = auditOccupiedSnapshot("running")
				}
				raw, _ := json.Marshal(snapshot)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
			})}).(*slotRuntimeAgentClient)
			bindings := newFakeRuntimeBindingRepo()
			if scenario == "stale-sql-binding" {
				bindings.bindings[9] = &models.InstanceRuntimeBinding{InstanceID: 9, RuntimePodID: 9, GatewayPort: 20000, State: "error"}
			}
			scheduler := NewRuntimeScheduler(newFakeRuntimeInstanceRepo(), &fakeRuntimePodRepo{}, bindings, nil, client, nil, nil, nil, time.Second)
			pod := models.RuntimePod{ID: 9, RuntimeType: RuntimeTypeHermes, PodName: physical.Name, Namespace: physical.Namespace, PodIP: ptrString("10.0.0.2"), PodUID: &uid, AgentEndpoint: &endpoint, State: "ready", Capacity: 8}
			free, err := scheduler.allocatableGatewaySlots(ctx, pod)
			if scenario == "unknown-pod" {
				if free != 0 || err == nil {
					t.Fatalf("unknown pod counted available capacity: free=%d err=%v", free, err)
				}
			} else {
				wantFree := 1
				if scenario == "full-except-unverified" {
					wantFree = 0
				}
				if free != wantFree || err != nil {
					t.Fatalf("availability was not isolated by slot: free=%d want=%d err=%v", free, wantFree, err)
				}
			}
		})
	}
}

func TestRuntimeCapacityDeficit(t *testing.T) {
	for _, tc := range []struct{ waiting, free, starting, want int }{
		{1, 0, 0, 1}, {1, 1, 0, 0}, {9, 0, 8, 1}, {1, 0, 8, 0},
		{8, 2, 2, 4}, {0, 1, 9, 0}, {-1, 2, 3, 0}, {2, -1, -1, 2},
	} {
		if got := runtimeCapacityDeficit(tc.waiting, tc.free, tc.starting); got != tc.want {
			t.Fatalf("%+v: got %d", tc, got)
		}
	}
}
