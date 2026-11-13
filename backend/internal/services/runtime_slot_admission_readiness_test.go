package services

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	k8sservice "clawreef/internal/services/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func nativeAdmissionPod() *corev1.Pod {
	pod := auditRoutingPod(2, 3)
	pod.Labels["clawmanager.io/sandbox-profile"] = k8sservice.RuntimeNFSRootlessProfile
	pod.Status.Phase = corev1.PodRunning
	pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse, Reason: "ContainersNotReady"}}
	for _, name := range []string{"runtime", "slot-0"} {
		pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{Name: name, Ready: true, ContainerID: "containerd://" + name, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.Now()}}})
	}
	pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{Name: "slot-1", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}})
	return pod
}

func TestNativeSlotAdmissionUsesSelectedSlotReadiness(t *testing.T) {
	for _, scenario := range []string{"healthy-sibling", "selected-restarting", "invalid-native-port", "coordinator-unready", "unknown-condition", "readiness-gate", "stale-uid", "legacy-profile"} {
		t.Run(scenario, func(t *testing.T) {
			pod := nativeAdmissionPod()
			req := RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 1, AgentType: "openclaw", GatewayPort: 20000, PodUID: "pod-uid"}
			switch scenario {
			case "selected-restarting":
				req.GatewayPort = 20003
			case "invalid-native-port":
				req.GatewayPort = -1
				pod.Status.Conditions[0].Status = corev1.ConditionTrue
			case "coordinator-unready":
				pod.Status.ContainerStatuses[0].Ready = false
			case "unknown-condition":
				pod.Status.Conditions[0].Reason = "NodeNotReady"
			case "readiness-gate":
				pod.Spec.ReadinessGates = []corev1.PodReadinessGate{{ConditionType: "external-ready"}}
			case "stale-uid":
				req.PodUID = "previous-pod"
			case "legacy-profile":
				delete(pod.Labels, "clawmanager.io/sandbox-profile")
			}
			kube := fake.NewSimpleClientset(pod)
			calls := 0
			client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"gateway_id":"gw-7-1"}`))}, nil
			})})
			configureNoExternalWorkspaceWrites(t, client)
			_, err := client.CreateGateway(context.Background(), "https://10.0.0.2:19090", req)
			if scenario == "healthy-sibling" {
				if err != nil || calls != 1 {
					t.Fatalf("healthy selected slot blocked by peer: calls=%d err=%v", calls, err)
				}
			} else {
				records, _ := kube.CoreV1().ConfigMaps("runtime").List(context.Background(), metav1.ListOptions{})
				if err == nil || calls != 0 || len(records.Items) != 0 {
					t.Fatalf("unsafe admission published: calls=%d records=%d err=%v", calls, len(records.Items), err)
				}
			}
		})
	}
}

func TestNativeSlotReservationExcludesUnreadySibling(t *testing.T) {
	pod := nativeAdmissionPod()
	kube := fake.NewSimpleClientset(pod)
	endpoint, uid := "https://10.0.0.2:19090", "pod-uid"
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet {
			t.Fatal("inspection mutated runtime")
		}
		body, _ := json.Marshal(auditFreeSnapshot())
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}).(*slotRuntimeAgentClient)
	ports, err := client.reservedGatewayPorts(context.Background(), models.RuntimePod{ID: 1, RuntimeType: "openclaw", Capacity: 2, PodUID: &uid, AgentEndpoint: &endpoint})
	if err != nil {
		t.Fatalf("healthy sibling inventory blocked: %v", err)
	}
	if _, blocked := ports[20000]; blocked {
		t.Fatal("healthy free slot reserved")
	}
	if _, blocked := ports[20003]; !blocked {
		t.Fatal("stale idle proof admitted restarting slot")
	}
}

func TestNativeOrdinaryPlacementPreservesHealthySibling(t *testing.T) {
	for _, scenario := range []string{"healthy-sibling", "none-ready", "legacy-profile", "stale", "replaced", "disabled"} {
		t.Run(scenario, func(t *testing.T) {
			reported, live := ordinaryPlacementFixture()
			live.State = "unhealthy"
			live.SandboxProfile = k8sservice.RuntimeNFSRootlessProfile
			live.ReadyGatewayPorts = map[int]bool{20000: true}
			switch scenario {
			case "none-ready":
				live.ReadyGatewayPorts = map[int]bool{20000: false}
			case "legacy-profile":
				live.SandboxProfile = ""
			case "stale":
				old := time.Now().Add(-time.Hour)
				reported.LastSeenAt = &old
			case "replaced":
				live.PodUID = "replacement"
			case "disabled":
				enabled := false
				live.SchedulingEnabled = &enabled
			}
			s := NewRuntimeScheduler(nil, nil, nil, nil, nil, nil, nil, &fakeRuntimeDeploymentService{pods: []k8sservice.RuntimeDeploymentPod{live}}, time.Second, WithRuntimeSchedulerNamespace("tenant"))
			got, err := s.verifiedOrdinaryRuntimePods(context.Background(), "openclaw", []models.RuntimePod{reported})
			if err != nil || (len(got) == 1) != (scenario == "healthy-sibling") {
				t.Fatalf("candidates=%+v err=%v", got, err)
			}
			if len(got) == 1 && got[0].KubernetesReady {
				t.Fatal("partial readiness rewrote whole Pod readiness")
			}
		})
	}
}

func TestNativeOrdinaryAssignmentStartsOnHealthySibling(t *testing.T) {
	ctx := context.Background()
	pod := nativeAdmissionPod()
	// The first physical slot is restarting; allocation must skip it and start
	// on the second one while the complete Pod remains unready.
	pod.Status.ContainerStatuses[1].Name = "slot-1"
	pod.Status.ContainerStatuses[2].Name = "slot-0"
	image := "repo/openclaw:test"
	deployment := k8sservice.BuildRuntimeDeployment(k8sservice.RuntimeDeploymentSpec{Name: "openclaw-runtime", Namespace: "runtime", Replicas: 1, RuntimeType: "openclaw", Image: image, SlotCount: 2, SandboxProfile: k8sservice.RuntimeNFSRootlessProfile, NetworkIsolation: true, AgentTokenSecretName: "agent", GatewayPortStart: 20000, GatewayPortEnd: 20005})
	pod.Spec = *deployment.Spec.Template.Spec.DeepCopy()
	pod.Labels = deployment.Spec.Template.Labels
	// Runtime credentials are independently exercised by the security suites.
	delete(pod.Labels, "clawmanager.io/runtime-security")
	kube := fake.NewSimpleClientset(deployment, pod)
	now := time.Now().UTC()
	endpoint, uid, ip := "https://10.0.0.2:19090", "pod-uid", "10.0.0.2"
	report := models.RuntimePod{ID: 9, RuntimeType: "openclaw", Namespace: "runtime", DeploymentName: deployment.Name, PodName: pod.Name, PodUID: &uid, PodIP: &ip, AgentEndpoint: &endpoint, ImageRef: image, State: "ready", LastSeenAt: &now, Capacity: 2}
	pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{9: &report}, schedulable: []models.RuntimePod{report}}
	bindings := newFakeRuntimeBindingRepo()
	instance := models.Instance{ID: 7, UserID: 2, Type: "openclaw", InstanceMode: "lite", RuntimeType: "gateway", Status: "creating", RuntimeGeneration: 1, WorkspacePath: ptrString("/workspaces/openclaw/user-2/instance-7")}
	instances := newFakeRuntimeInstanceRepo()
	instances.byID[7] = &instance
	var starts []RuntimeAgentCreateGatewayRequest
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		body := ""
		switch r.Method {
		case http.MethodGet:
			data, _ := json.Marshal(auditFreeSnapshot())
			body = string(data)
		case http.MethodPost:
			var request RuntimeAgentCreateGatewayRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			starts = append(starts, request)
			body = `{"gateway_id":"gw-7-1","port":20003,"status":"running"}`
		default:
			t.Fatalf("unexpected runtime mutation: %s %s", r.Method, r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	configureNoExternalWorkspaceWrites(t, client)
	scheduler := NewRuntimeScheduler(instances, pods, bindings, nil, client, nil, nil, k8sservice.NewRuntimeDeploymentService(kube), time.Second, WithRuntimeSchedulerNamespace("runtime"), WithRuntimeSchedulerGatewayEnvBuilder(func(*models.Instance) (map[string]string, error) { return map[string]string{}, nil }))
	if err := scheduler.assignInstance(ctx, instance); err != nil {
		t.Fatalf("healthy sibling assignment failed: %v", err)
	}
	binding := bindings.bindings[7]
	if len(starts) != 1 || starts[0].GatewayPort != 20003 || starts[0].PodUID != uid || binding == nil || binding.GatewayPort != 20003 || binding.State != "running" || pods.claims[9] != 1 {
		t.Fatalf("incorrect sibling assignment: requests=%+v binding=%+v claims=%v", starts, binding, pods.claims)
	}
	current, _ := kube.CoreV1().Pods("runtime").Get(ctx, pod.Name, metav1.GetOptions{})
	if current.Status.Conditions[0].Status != corev1.ConditionFalse {
		t.Fatal("admission rewrote whole Pod readiness")
	}
}

func TestNativePendingSlotReplayUsesReservedHealthySlot(t *testing.T) {
	f := newPendingSlotReplayFixture(t)
	f.seed(t, 1, "stopped")
	pod := nativeAdmissionPod()
	pod.Status.ContainerStatuses[1].Name = "slot-1"
	pod.Status.ContainerStatuses[2].Name = "slot-0"
	if _, err := f.kube.CoreV1().Pods("runtime").Update(context.Background(), pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	configureNoExternalWorkspaceWrites(t, f.client)
	deployments := f.scheduler.deployments.(*fakeRuntimeDeploymentService)
	deployments.pods[0].State = "unhealthy"
	deployments.pods[0].SandboxProfile = k8sservice.RuntimeNFSRootlessProfile
	deployments.pods[0].ReadyGatewayPorts = map[int]bool{20003: true}
	if errs := f.scheduler.reconcileCreatingInstance(context.Background(), f.instance); len(errs) != 0 {
		t.Fatal(errs)
	}
	if len(f.requests) != 1 || f.requests[0].GatewayPort != f.binding.GatewayPort || f.requests[0].PodUID != "pod-uid" || f.binding.State != "running" {
		t.Fatalf("healthy reserved slot did not resume: requests=%+v binding=%+v", f.requests, f.binding)
	}
	f.assertReservationUnchanged(t)
}
