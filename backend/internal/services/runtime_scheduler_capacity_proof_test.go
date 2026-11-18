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
