package services

import (
	k8sservice "clawreef/internal/services/k8s"
	"context"
	"encoding/json"
	"errors"
	"io"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"net/http"
	"strings"
	"testing"

	"clawreef/internal/models"
)

func TestStrictSchedulerKeepsErrorBindingPortReserved(t *testing.T) {
	bindings := newFakeRuntimeBindingRepo()
	old := &models.InstanceRuntimeBinding{InstanceID: 25, RuntimePodID: 132, RuntimeType: RuntimeTypeHermes, GatewayID: "gw-25-14", GatewayPort: 20000, State: "error", Generation: 14}
	bindings.bindings[25] = old
	scheduler := &RuntimeScheduler{bindingRepo: bindings, agentClient: &strictSchedulerAgent{&fakeRuntimeAgentClient{}}, gatewayPortStart: 20000, gatewayPortEnd: 20299}
	got, err := scheduler.reserveGatewayPort(context.Background(), models.Instance{ID: 161, RuntimeGeneration: 1}, RuntimeTypeHermes, models.RuntimePod{ID: 132, Capacity: 8}, "/workspace")
	if err != nil {
		t.Fatal(err)
	}
	if got.GatewayPort != 20001 || bindings.bindings[25] != old || len(bindings.deleteByPodPortCalls) != 0 {
		t.Fatalf("unconfirmed error writer reused: port=%d old=%v deletes=%v", got.GatewayPort, bindings.bindings[25], bindings.deleteByPodPortCalls)
	}
}

func TestStrictSchedulerBindingConflictNeverDeletesErrorWriter(t *testing.T) {
	bindings := newFakeRuntimeBindingRepo()
	bindings.enforceUniqueGateway = true
	old := &models.InstanceRuntimeBinding{InstanceID: 25, RuntimePodID: 132, GatewayPort: 20000, State: "error", Generation: 14}
	bindings.bindings[25] = old
	scheduler := &RuntimeScheduler{bindingRepo: bindings, agentClient: &strictSchedulerAgent{&fakeRuntimeAgentClient{}}}
	err := scheduler.createRuntimeBindingWithStalePortRecovery(context.Background(), &models.InstanceRuntimeBinding{InstanceID: 161, RuntimePodID: 132, GatewayPort: 20000, Generation: 1})
	if err == nil || bindings.bindings[25] != old || len(bindings.deleteByPodPortCalls) != 0 {
		t.Fatalf("conflict removed unconfirmed writer: err=%v old=%v", err, bindings.bindings[25])
	}
}

type occupiedPortsAgent struct {
	*strictSchedulerAgent
	ports map[int]struct{}
	err   error
}

func (a *occupiedPortsAgent) reservedGatewayPorts(context.Context, models.RuntimePod) (map[int]struct{}, error) {
	return a.ports, a.err
}
func TestStrictSchedulerExcludesPersistedAssignmentWithoutSQLBinding(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		bindings := newFakeRuntimeBindingRepo()
		agent := &occupiedPortsAgent{strictSchedulerAgent: &strictSchedulerAgent{&fakeRuntimeAgentClient{}}, ports: map[int]struct{}{20000: {}}}
		if unavailable {
			agent.err = errors.New("assignment inventory unavailable")
		}
		s := &RuntimeScheduler{bindingRepo: bindings, agentClient: agent, gatewayPortStart: 20000, gatewayPortEnd: 20299}
		got, err := s.reserveGatewayPort(context.Background(), models.Instance{ID: 161, RuntimeGeneration: 2}, RuntimeTypeHermes, models.RuntimePod{ID: 132, Capacity: 8}, "/workspace")
		if unavailable {
			if err == nil || len(bindings.bindings) != 0 {
				t.Fatal("unknown persisted ownership admitted")
			}
			continue
		}
		if err != nil || got.GatewayPort != 20001 {
			t.Fatalf("orphan persisted writer port reused: got=%+v err=%v", got, err)
		}
	}
}

func TestSlotClientAllocationUsesFrozenCurrentPodAssignments(t *testing.T) {
	for _, scenario := range []string{"running", "draining", "stopped", "unreleased-stop", "other-pod", "old-pod-reused-ip", "wrong-request-owner", "wrong-runtime", "bad-port", "deleted", "list-failed", "pod-replaced"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			pod := auditRoutingPod(8, 1)
			kube := fake.NewSimpleClientset(pod)
			endpoint, uid := "https://10.0.0.2:19090", string(pod.UID)
			client := NewSlotRuntimeAgentClientWithHTTPClient("control", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/state") {
					t.Fatalf("unexpected runtime mutation: %s %s", r.Method, r.URL.Path)
				}
				data, _ := json.Marshal(auditFreeSnapshot())
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(data)))}, nil
			})}).(*slotRuntimeAgentClient)
			req := RuntimeAgentCreateGatewayRequest{InstanceID: 25, UserID: 1, Generation: 14, PodUID: uid, AgentType: RuntimeTypeHermes, GatewayPort: 20000}
			raw, _ := json.Marshal(req)
			if err := client.assignments.EnsureDesired(ctx, k8sservice.RuntimeAssignment{InstanceID: 25, UserID: 1, Generation: 14, PodUID: uid, RuntimeType: RuntimeTypeHermes, Endpoint: endpoint, GatewayID: "gw-25-14", Request: raw}); err != nil {
				t.Fatal(err)
			}
			cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-25", metav1.GetOptions{})
			cm.Labels["clawmanager.io/needs-reconcile"] = "false"
			cm.Data["applied_generation"], cm.Data["applied_pod_uid"] = "14", uid
			wantError, wantPort := false, 20001
			switch scenario {
			case "draining":
				cm.Data["desired_state"] = "draining"
			case "stopped":
				cm.Data["desired_state"] = "stopped"
				wantPort = 20000
			case "unreleased-stop":
				cm.Data["desired_state"] = "stopped"
				cm.Data["container_termination_proof"] = "recorded"
			case "other-pod":
				cm.Data["pod_uid"] = "older-pod"
				cm.Data["endpoint"] = "https://10.0.0.3:19090"
				wantPort = 20000
			case "old-pod-reused-ip":
				cm.Data["pod_uid"] = "older-pod"
				wantPort = 20000
			case "wrong-request-owner":
				req.InstanceID = 26
				raw, _ = json.Marshal(req)
				cm.Data["request"] = string(raw)
				wantError = true
			case "wrong-runtime":
				cm.Data["runtime_type"] = "openclaw"
				wantError = true
			case "bad-port":
				req.GatewayPort = 19999
				raw, _ = json.Marshal(req)
				cm.Data["request"] = string(raw)
				wantError = true
			case "deleted":
				now := metav1.Now()
				cm.DeletionTimestamp = &now
				wantError = true
			case "list-failed":
				kube.PrependReactor("list", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) { return true, nil, errors.New("forbidden") })
				wantError = true
			case "pod-replaced":
				pod.UID = "changed"
				_, _ = kube.CoreV1().Pods("runtime").Update(ctx, pod, metav1.UpdateOptions{})
				wantError = true
			}
			_, _ = kube.CoreV1().ConfigMaps("runtime").Update(ctx, cm, metav1.UpdateOptions{})
			bindings := newFakeRuntimeBindingRepo()
			s := &RuntimeScheduler{bindingRepo: bindings, agentClient: client, gatewayPortStart: 20000, gatewayPortEnd: 20299}
			actionsBefore := len(kube.Actions())
			got, err := s.reserveGatewayPort(ctx, models.Instance{ID: 161, RuntimeGeneration: 2}, RuntimeTypeHermes, models.RuntimePod{ID: 132, RuntimeType: RuntimeTypeHermes, Capacity: 8, AgentEndpoint: &endpoint, PodUID: &uid}, "/workspace")
			if wantError {
				if err == nil || len(bindings.bindings) != 0 {
					t.Fatal("untrusted assignment inventory admitted")
				}
				return
			}
			if err != nil || got.GatewayPort != wantPort {
				t.Fatalf("allocation=%+v err=%v want port%d", got, err, wantPort)
			}
			for _, action := range kube.Actions()[actionsBefore:] {
				if action.GetResource().Resource == "secrets" {
					t.Fatal("inventory read business secrets")
				}
			}
		})
	}
}
