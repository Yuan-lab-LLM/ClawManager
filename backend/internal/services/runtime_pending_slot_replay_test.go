package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	k8sservice "clawreef/internal/services/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

type pendingSlotReplayFixture struct {
	scheduler *RuntimeScheduler
	instance  models.Instance
	pod       *models.RuntimePod
	binding   *models.InstanceRuntimeBinding
	pods      *fakeRuntimePodRepo
	bindings  *fakeRuntimeBindingRepo
	client    *slotRuntimeAgentClient
	kube      *fake.Clientset
	requests  []RuntimeAgentCreateGatewayRequest
	envCalls  int
	createErr error
}

func newPendingSlotReplayFixture(t *testing.T) *pendingSlotReplayFixture {
	t.Helper()
	endpoint, uid, ip := "https://10.0.0.2:19090", "pod-uid", "10.0.0.2"
	now := time.Now()
	workspace := "/workspaces/openclaw/user-2/instance-7"
	f := &pendingSlotReplayFixture{
		instance: models.Instance{ID: 7, UserID: 2, Type: RuntimeTypeOpenClaw, RuntimeType: RuntimeBackendGateway, InstanceMode: InstanceModeLite, Status: "creating", RuntimeGeneration: 2, WorkspacePath: &workspace},
		pod:      &models.RuntimePod{ID: 9, RuntimeType: RuntimeTypeOpenClaw, Namespace: "runtime", PodName: "pool", DeploymentName: "openclaw-runtime", ImageRef: "repo/openclaw:test", PodIP: &ip, LastSeenAt: &now, AgentEndpoint: &endpoint, PodUID: &uid, State: "ready", Capacity: 8},
		binding:  &models.InstanceRuntimeBinding{InstanceID: 7, RuntimePodID: 9, RuntimeType: RuntimeTypeOpenClaw, GatewayID: "pending-7-2", GatewayPort: 20003, WorkspacePath: workspace, State: "creating", Generation: 2},
		kube:     fake.NewSimpleClientset(slotTestPod()), bindings: newFakeRuntimeBindingRepo(),
	}
	f.pods = &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{9: f.pod}}
	f.bindings.bindings[7] = f.binding
	httpClient := &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/gateways" {
			t.Fatalf("unexpected runtime call %s %s", r.Method, r.URL)
		}
		var req RuntimeAgentCreateGatewayRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		f.requests = append(f.requests, req)
		if f.createErr != nil {
			return nil, f.createErr
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"gateway_id":"gw-7-2","status":"running","port":%d}`, req.GatewayPort)))}, nil
	})}
	f.client = NewSlotRuntimeAgentClientWithHTTPClient("token", f.kube, "runtime", httpClient).(*slotRuntimeAgentClient)
	deployments := &fakeRuntimeDeploymentService{pods: []k8sservice.RuntimeDeploymentPod{{RuntimeType: f.pod.RuntimeType, Namespace: f.pod.Namespace, PodName: f.pod.PodName, DeploymentName: f.pod.DeploymentName, PodUID: uid, PodIP: &ip, ImageRef: f.pod.ImageRef, State: "ready"}}}
	f.scheduler = NewRuntimeScheduler(newFakeRuntimeInstanceRepo(), f.pods, f.bindings, &fakeRuntimeRolloutRepo{}, f.client, nil, nil, deployments, time.Second,
		WithRuntimeSchedulerNamespace("runtime"),
		WithRuntimeSchedulerGatewayEnvBuilder(func(*models.Instance) (map[string]string, error) {
			f.envCalls++
			return map[string]string{"BUSINESS_TOKEN": "newly-issued-token"}, nil
		}))
	return f
}

func (f *pendingSlotReplayFixture) seed(t *testing.T, generation int, state string) RuntimeAgentCreateGatewayRequest {
	t.Helper()
	req := RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: generation, AgentType: RuntimeTypeOpenClaw, PodUID: "pod-uid", WorkspacePath: f.binding.WorkspacePath, GatewayPort: 20003, UID: 200007, GID: 200007, CPUCores: 1.25, MemoryMB: 1234, PortRange: RuntimeAgentPortRange{Start: 20000, End: 20999}, Environment: map[string]string{"BUSINESS_TOKEN": "original-issued-once"}}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("gw-7-%d", generation)
	if err = f.client.assignments.EnsureDesired(context.Background(), k8sservice.RuntimeAssignment{InstanceID: 7, UserID: 2, Generation: generation, RuntimeType: RuntimeTypeOpenClaw, Endpoint: *f.pod.AgentEndpoint, PodUID: "pod-uid", GatewayID: id, Request: raw, Environment: req.Environment}); err != nil {
		t.Fatal(err)
	}
	if state != "running" {
		if err = f.client.assignments.MarkDraining(context.Background(), *f.pod.AgentEndpoint, id); err != nil {
			t.Fatal(err)
		}
	}
	if state == "stopped" {
		if err = f.client.assignments.ConfirmStopped(context.Background(), *f.pod.AgentEndpoint, id); err != nil {
			t.Fatal(err)
		}
	}
	return req
}

func (f *pendingSlotReplayFixture) assertReservationUnchanged(t *testing.T) {
	t.Helper()
	if f.bindings.bindings[7] != f.binding || f.binding.GatewayPort != 20003 || f.binding.Generation != 2 || f.pods.claims[9] != 0 || f.pods.releases[9] != 0 || f.bindings.deleteCalls[7] != 0 || f.bindings.deleteAndReleaseCalls[7] != 0 {
		t.Fatalf("reservation changed: binding=%+v claims=%v releases=%v delete=%v release=%v", f.bindings.bindings[7], f.pods.claims, f.pods.releases, f.bindings.deleteCalls, f.bindings.deleteAndReleaseCalls)
	}
}

func TestPendingSlotReplayHoldsPublishedGenerationAndKeepsImmutableSecret(t *testing.T) {
	f := newPendingSlotReplayFixture(t)
	f.seed(t, 2, "running")
	// A published request is recovered by the existing assignment reconciler/report,
	// never by rebuilding its expiring business tokens in the scheduler.
	if err := f.client.assignments.MarkApplied(context.Background(), *f.pod.AgentEndpoint, "gw-7-2"); err != nil {
		t.Fatal(err)
	}
	f.scheduler.reconcileCreatingInstance(context.Background(), f.instance)
	if len(f.requests) != 0 || f.envCalls != 0 {
		t.Fatalf("published request regenerated: requests=%+v env calls=%d", f.requests, f.envCalls)
	}
	secret, err := f.kube.CoreV1().Secrets("runtime").Get(context.Background(), "lite-instance-7-g2", metav1.GetOptions{})
	if err != nil || string(secret.Data["BUSINESS_TOKEN"]) != "original-issued-once" {
		t.Fatalf("immutable generation Secret changed: %v", err)
	}
	f.assertReservationUnchanged(t)
}

func TestPendingSlotReplayAfterConfirmedPreviousGenerationStop(t *testing.T) {
	f := newPendingSlotReplayFixture(t)
	f.seed(t, 1, "stopped")
	if errs := f.scheduler.reconcileCreatingInstance(context.Background(), f.instance); len(errs) > 0 {
		t.Fatal(errs)
	}
	if len(f.requests) != 1 || f.requests[0].Generation != 2 || f.requests[0].GatewayPort != 20003 || f.requests[0].PodUID != "pod-uid" || f.envCalls != 1 {
		t.Fatalf("pending reservation not resumed: requests=%+v env=%d", f.requests, f.envCalls)
	}
	f.assertReservationUnchanged(t)
	if f.binding.GatewayID != "gw-7-2" || f.binding.State != "running" {
		t.Fatalf("pending assignment not recovered: %+v", f.binding)
	}
	f.scheduler.reconcileCreatingInstance(context.Background(), f.instance)
	if len(f.requests) != 1 || f.envCalls != 1 {
		t.Fatal("running assignment replayed")
	}
}

func TestPendingSlotReplayRetainsReservationOnUnknownCreate(t *testing.T) {
	f := newPendingSlotReplayFixture(t)
	f.seed(t, 1, "stopped")
	f.createErr = context.DeadlineExceeded
	f.scheduler.reconcileCreatingInstance(context.Background(), f.instance)
	if len(f.requests) != 1 || f.binding.GatewayID != "pending-7-2" || f.binding.State != "creating" {
		t.Fatalf("unknown create lost pending reservation: calls=%d binding=%+v", len(f.requests), f.binding)
	}
	f.assertReservationUnchanged(t)
	// First attempt published generation 2 before losing the HTTP response. A new
	// scheduler pass must not regenerate that generation's immutable credentials.
	f.scheduler.reconcileCreatingInstance(context.Background(), f.instance)
	if len(f.requests) != 1 || f.envCalls != 1 {
		t.Fatalf("unknown published create regenerated: calls=%d env=%d", len(f.requests), f.envCalls)
	}
}

func TestPendingSlotReplayRejectsUnprovenTargetsAndAssignments(t *testing.T) {
	for _, name := range []string{"missing-assignment", "old-active", "same-stopped", "same-draining", "newer-assignment", "unseen-uid", "unready", "missing-pod", "draining-pod", "endpoint-changed", "invalid-reservation-port", "wrong-reservation-workspace", "wrong-reservation-instance", "orphan-generation-secret", "wrong-assignment-owner", "wrong-assignment-runtime"} {
		t.Run(name, func(t *testing.T) {
			f := newPendingSlotReplayFixture(t)
			gen, state := 2, "running"
			if name == "old-active" {
				gen = 1
			}
			switch name {
			case "unseen-uid", "unready", "missing-pod", "draining-pod", "endpoint-changed", "invalid-reservation-port", "wrong-reservation-workspace", "wrong-reservation-instance", "orphan-generation-secret", "wrong-assignment-owner", "wrong-assignment-runtime":
				gen, state = 1, "stopped"
			}
			if name == "same-stopped" {
				state = "stopped"
			}
			if name == "same-draining" {
				state = "draining"
			}
			if name == "newer-assignment" {
				gen = 3
			}
			f.seed(t, gen, state)
			switch name {
			case "missing-assignment":
				f.kube.CoreV1().ConfigMaps("runtime").Delete(context.Background(), "lite-instance-7", metav1.DeleteOptions{})
			case "unseen-uid":
				p, _ := f.kube.CoreV1().Pods("runtime").Get(context.Background(), "pool", metav1.GetOptions{})
				p.UID = types.UID("replacement")
				f.kube.CoreV1().Pods("runtime").Update(context.Background(), p, metav1.UpdateOptions{})
			case "unready":
				p, _ := f.kube.CoreV1().Pods("runtime").Get(context.Background(), "pool", metav1.GetOptions{})
				p.Status.Conditions[0].Status = corev1.ConditionFalse
				f.kube.CoreV1().Pods("runtime").Update(context.Background(), p, metav1.UpdateOptions{})
			case "missing-pod":
				delete(f.pods.pods, 9)
			case "draining-pod":
				f.pod.Draining = true
			case "endpoint-changed":
				ep := "https://10.0.0.3:19090"
				f.pod.AgentEndpoint = &ep
			case "invalid-reservation-port":
				f.binding.GatewayPort = 20004
			case "wrong-reservation-workspace":
				f.binding.WorkspacePath = "/workspaces/openclaw/user-2/instance-8"
			case "wrong-reservation-instance":
				f.binding.InstanceID = 8
			case "orphan-generation-secret":
				// Secret may have been published before a ConfigMap CAS failure. It must
				// never be replaced by a newly issued token, even with an older stopped CM.
				v := true
				_, err := f.kube.CoreV1().Secrets("runtime").Create(context.Background(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "lite-instance-7-g2", Namespace: "runtime"}, Type: corev1.SecretTypeOpaque, Immutable: &v, Data: map[string][]byte{"BUSINESS_TOKEN": []byte("orphan-issued-once")}}, metav1.CreateOptions{})
				if err != nil {
					t.Fatal(err)
				}
			case "wrong-assignment-owner", "wrong-assignment-runtime":
				cm, _ := f.kube.CoreV1().ConfigMaps("runtime").Get(context.Background(), "lite-instance-7", metav1.GetOptions{})
				if name == "wrong-assignment-owner" {
					cm.Data["user_id"] = "3"
				} else {
					cm.Data["runtime_type"] = "hermes"
				}
				if _, err := f.kube.CoreV1().ConfigMaps("runtime").Update(context.Background(), cm, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			f.scheduler.reconcileCreatingInstance(context.Background(), f.instance)
			if len(f.requests) != 0 || f.envCalls != 0 || f.binding.GatewayID != "pending-7-2" {
				t.Fatalf("unproven replay proceeded: requests=%+v env=%d binding=%+v", f.requests, f.envCalls, f.binding)
			}
			if f.pods.releases[9] != 0 || f.bindings.deleteAndReleaseCalls[7] != 0 {
				t.Fatal("unproven reservation released")
			}
		})
	}
}

func TestPendingSlotReplayDoesNotApplyToLegacyOrDifferentBindings(t *testing.T) {
	for _, name := range []string{"legacy", "running", "old-binding", "newer-binding", "wrong-pending-id"} {
		t.Run(name, func(t *testing.T) {
			f := newPendingSlotReplayFixture(t)
			f.seed(t, 2, "running")
			switch name {
			case "legacy":
				f.scheduler.agentClient = &fakeRuntimeAgentClient{}
			case "running":
				f.binding.State = "running"
				f.binding.GatewayID = "gw-7-2"
			case "old-binding":
				f.binding.Generation = 1
				f.binding.GatewayID = "pending-7-1"
			case "newer-binding":
				f.binding.Generation = 3
				f.binding.GatewayID = "pending-7-3"
			case "wrong-pending-id":
				f.binding.GatewayID = "pending-8-2"
			}
			f.scheduler.reconcileCreatingInstance(context.Background(), f.instance)
			if len(f.requests) != 0 || f.envCalls != 0 {
				t.Fatalf("nonmatching binding replayed: requests=%+v env=%d", f.requests, f.envCalls)
			}
		})
	}
}
