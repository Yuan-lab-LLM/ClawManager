package services

import (
	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
	"context"
	"encoding/json"
	"errors"
	"io"
	"k8s.io/client-go/kubernetes/fake"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceCapacityNativeEligibilityReadOnly(t *testing.T) {
	for _, scenario := range []string{"ready", "stale", "maintenance", "upgrade-lab", "unready", "reserved", "unknown-layout"} {
		t.Run(scenario, func(t *testing.T) {
			pod, live := ordinaryPlacementFixture()
			pod.RuntimeType = RuntimeTypeHermes
			live.RuntimeType = RuntimeTypeHermes
			pod.Capacity = 2
			pod.UsedSlots = 0
			agent := &layoutCapacityAgent{occupiedPortsAgent: &occupiedPortsAgent{strictSchedulerAgent: &strictSchedulerAgent{&fakeRuntimeAgentClient{}}, ports: map[int]struct{}{}}, physical: map[int]struct{}{20000: {}, 20001: {}}}
			want := 2
			switch scenario {
			case "stale":
				at := time.Now().Add(-time.Hour)
				pod.LastSeenAt = &at
				want = 0
			case "maintenance":
				pod.Draining = true
				want = 0
			case "upgrade-lab":
				live.PoolRole = "upgrade-lab"
				want = 0
			case "unready":
				live.State = "pending"
				want = 0
			case "reserved":
				agent.ports[20000] = struct{}{}
				want = 1
			case "unknown-layout":
				agent.err = errors.New("unknown")
			}
			pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{pod.ID: &pod}, schedulable: []models.RuntimePod{pod}}
			dep := &fakeRuntimeDeploymentService{pods: []k8s.RuntimeDeploymentPod{live}}
			s := NewRuntimeScheduler(newFakeRuntimeInstanceRepo(), pods, newFakeRuntimeBindingRepo(), nil, agent, nil, nil, dep, time.Second, WithRuntimeSchedulerNamespace("tenant"), WithRuntimeSchedulerGatewayPortRange(20000, 20001))
			got, err := s.WorkspaceCapacity(context.Background())
			if scenario == "unknown-layout" {
				if err == nil {
					t.Fatal("unknown inventory treated as capacity")
				}
				return
			}
			if err != nil || got[RuntimeTypeHermes] != want {
				t.Fatalf("%+v %v want %d", got, err, want)
			}
			if len(pods.claims) > 0 || len(agent.createRequests) > 0 || dep.scaleCalls > 0 {
				t.Fatal("capacity read mutated runtime")
			}
		})
	}
}

func TestWorkspaceCapacityDoesNotPersistSlotAudit(t *testing.T) {
	ctx := context.Background()
	physical := auditRoutingPod(1, 1)
	kube := fake.NewSimpleClientset(physical)
	endpoint, uid := "https://10.0.0.2:19090", string(physical.UID)
	client := NewSlotRuntimeAgentClientWithHTTPClient("control", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet {
			t.Fatal("mutation", r.Method)
		}
		raw, _ := json.Marshal(auditFreeSnapshot())
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	})}).(*slotRuntimeAgentClient)
	pod := models.RuntimePod{ID: 9, RuntimeType: RuntimeTypeHermes, PodName: physical.Name, Namespace: physical.Namespace, DeploymentName: "hermes-runtime", PodIP: ptrString("10.0.0.2"), PodUID: &uid, AgentEndpoint: &endpoint, State: "ready", Capacity: 1}
	record := runtimeSlotAuditRecord{PodUID: uid, Endpoint: endpoint, RuntimeType: RuntimeTypeHermes, Ports: map[int]string{20000: "previous unknown"}}
	if err := client.writeSlotAudit(ctx, pod, nil, record); err != nil {
		t.Fatal(err)
	}
	kube.ClearActions()
	scheduler := NewRuntimeScheduler(nil, nil, newFakeRuntimeBindingRepo(), nil, client, nil, nil, nil, time.Second)
	free, err := scheduler.inspectAllocatableGatewaySlots(ctx, pod, true)
	if err != nil || free != 1 {
		t.Fatal(free, err)
	}
	for _, action := range kube.Actions() {
		if action.GetVerb() != "get" && action.GetVerb() != "list" {
			t.Fatal("read wrote Kubernetes", action.GetVerb(), action.GetResource())
		}
	}
	_, persisted, err := client.readSlotAudit(ctx, pod)
	if err != nil || persisted.Ports[20000] != "previous unknown" {
		t.Fatal("read changed scheduler quarantine", persisted, err)
	}
	// Existing allocator still resolves the same proven-free slot and persists its audit.
	legacyFree, err := scheduler.allocatableGatewaySlots(ctx, pod)
	if err != nil || legacyFree != free {
		t.Fatal("eligibility drift", legacyFree, err)
	}
	_, persisted, err = client.readSlotAudit(ctx, pod)
	if err != nil || len(persisted.Ports) != 0 {
		t.Fatal("native allocator behavior changed", persisted, err)
	}
}
