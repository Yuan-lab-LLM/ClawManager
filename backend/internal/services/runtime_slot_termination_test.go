package services

import (
	"clawreef/internal/runtimeauth"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func configureNoExternalWorkspaceWrites(t *testing.T, client RuntimeAgentClient) {
	t.Helper()
	if err := ConfigureSlotWorkspaceWriteFence(client, func(context.Context, runtimeauth.Identity) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := ConfigureSlotWorkspaceWriteActivation(client, func(context.Context, runtimeauth.Identity) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := ConfigureSlotExternalWriterCleanup(client, func(context.Context, runtimeauth.Identity) error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeSlotTerminationRequiresMatchingActualContainer(t *testing.T) {
	intent := runtimeSlotStopIntent{Version: 1, OperationID: "11111111111111111111111111111111", PodUID: "pod-uid", Slot: "slot-0", GatewayID: "gw-7-1", InstanceID: 7, UserID: 2, Generation: 1, WorkerEpoch: "22222222222222222222222222222222", AssignmentDigest: strings.Repeat("d", 64)}
	message := runtimeSlotTerminationMessage{runtimeSlotStopIntent: intent, CleanupConfirmed: true}
	body, err := containerTerminationCommitment(message)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 46 {
		t.Fatalf("termination receipt exceeds 256-slot kubelet budget: %d", len(body))
	}
	// Same fixed intent vector as AR, independently computed with Python hashlib.
	if body != "C1:58uQ7CZv4aGO9zYxjzLA7yAEWSjn406ue0L_0REyGq4" {
		t.Fatalf("CM/AR commitment schema drift: %s", body)
	}
	maintenance := message
	maintenance.Kind = "maintenance"
	native, _ := containerTerminationCommitment(maintenance)
	if native != "C1:6_cSrEvIx_mPxa61xMD0eoI9fWWeW7K9Q4ij0hCQX7M" {
		t.Fatalf("CM/AR maintenance commitment schema drift: %s", native)
	}
	terminated := &corev1.ContainerStateTerminated{ContainerID: "containerd://old", Message: body, FinishedAt: metav1.Now()}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{UID: types.UID(intent.PodUID)}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: intent.Slot, ContainerID: "containerd://new", RestartCount: 1, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}, LastTerminationState: corev1.ContainerState{Terminated: terminated}}}}}
	proof, err := runtimeSlotTerminationProof(pod, intent)
	if err != nil || proof.ContainerID != "containerd://old" || proof.WorkerEpoch != message.WorkerEpoch {
		t.Fatalf("missing exact termination proof: %+v %v", proof, err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*corev1.Pod)
	}{
		{"restart count without termination", func(p *corev1.Pod) { p.Status.ContainerStatuses[0].LastTerminationState.Terminated = nil }},
		{"other slot", func(p *corev1.Pod) { p.Status.ContainerStatuses[0].Name = "slot-1" }},
		{"replaced Pod", func(p *corev1.Pod) { p.UID = "new-pod" }},
		{"missing container ID", func(p *corev1.Pod) { p.Status.ContainerStatuses[0].LastTerminationState.Terminated.ContainerID = "" }},
		{"missing cleanup receipt", func(p *corev1.Pod) {
			m := message
			m.CleanupConfirmed = false
			b, _ := containerTerminationCommitment(m)
			p.Status.ContainerStatuses[0].LastTerminationState.Terminated.Message = b
		}},
		{"other stop operation", func(p *corev1.Pod) {
			m := message
			m.OperationID = "33333333333333333333333333333333"
			b, _ := containerTerminationCommitment(m)
			p.Status.ContainerStatuses[0].LastTerminationState.Terminated.Message = b
		}},
		{"truncated message", func(p *corev1.Pod) {
			p.Status.ContainerStatuses[0].LastTerminationState.Terminated.Message = body[:len(body)-1]
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := pod.DeepCopy()
			test.mutate(p)
			if _, err := runtimeSlotTerminationProof(p, intent); err == nil {
				t.Fatal("uncertain termination was accepted")
			}
		})
	}
}

func TestNFSProfileCompletesOnlyAfterTerminationAndWriterReconciliation(t *testing.T) {
	testNFSContainerStop(t, false)
}

func TestNFSProfileGracefulStopUsesTerminatedContainerProof(t *testing.T) {
	testNFSContainerStop(t, true)
}

func TestNFSProfileManualOperatorFenceUsesExistingWriterReconciliation(t *testing.T) {
	testNFSContainerStop(t, false, "external")
}

func TestNFSContainerStopWaitsForKubeletAndReplacementWorker(t *testing.T) {
	testNFSContainerStop(t, false, "delayed")
}

func TestNFSContainerStopTimeoutRetainsIntentForRetry(t *testing.T) {
	testNFSContainerStop(t, false, "retry")
}

func testNFSContainerStop(t *testing.T, graceful bool, mode ...string) {
	t.Helper()
	external := len(mode) > 0 && mode[0] == "external"
	pod := slotTestPod()
	pod.Labels["clawmanager.io/sandbox-profile"] = "nfs-rootless-v1"
	pod.Spec.Containers = []corev1.Container{{Name: "slot-0", Env: []corev1.EnvVar{{Name: "RUNTIME_GATEWAY_PORT_START", Value: "20000"}}}}
	if external {
		yes, no := true, false
		pod.Spec.HostUsers = &yes
		pod.Spec.ShareProcessNamespace = &no
		pod.Spec.NodeName = "node"
		pod.Spec.Containers[0].Image = "registry/runtime@sha256:" + strings.Repeat("a", 64)
		pod.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{Privileged: &no, AllowPrivilegeEscalation: &no, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}, Add: []corev1.Capability{"SETUID", "SETGID"}}}
	}
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "slot-0", ContainerID: "containerd://old"}, {Name: "slot-1", ContainerID: "containerd://other"}}
	kube := fake.NewSimpleClientset(pod)
	reads := 0
	if len(mode) > 0 && !external {
		kube.PrependReactor("get", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
			obj, err := kube.Tracker().Get(corev1.SchemeGroupVersion.WithResource("pods"), "runtime", pod.Name)
			if err != nil {
				return true, nil, err
			}
			p := obj.(*corev1.Pod).DeepCopy()
			if p.Status.ContainerStatuses[0].LastTerminationState.Terminated != nil && reads < 3 {
				reads++
				p.Status.ContainerStatuses[0].LastTerminationState.Terminated = nil
			}
			return true, p, nil
		})
	}
	reconcileCalls := 0
	var previousIntent *runtimeSlotStopIntent
	reconciled := false
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		body := `{"gateway_id":"gw-7-1"}`
		switch {
		case r.URL.Path == "/v1/slots/slot-0/state":
			b, _ := json.Marshal(map[string]any{"digest": strings.Repeat("d", 64), "assignment": map[string]any{"gateway_id": "gw-7-1", "instance_id": 7, "user_id": 2, "generation": 1, "pod_uid": "pod-uid", "worker_epoch": strings.Repeat("2", 32)}})
			body = string(b)
		case r.Method == http.MethodDelete:
			var intent runtimeSlotStopIntent
			if err := json.NewDecoder(r.Body).Decode(&intent); err != nil {
				t.Fatal(err)
			}
			if intent.Graceful != graceful {
				t.Fatal("graceful policy changed before reset")
			}
			if previousIntent != nil && *previousIntent != intent {
				t.Fatal("retry changed persisted stop identity")
			}
			previousIntent = &intent
			message, _ := containerTerminationCommitment(runtimeSlotTerminationMessage{runtimeSlotStopIntent: intent, CleanupConfirmed: true})
			if external {
				yes := true
				raw, _ := json.Marshal(runtimeSlotFenceRequest{Version: 1, Intent: intent, ContainerID: "containerd://old", ReplacementImage: pod.Spec.Containers[0].Image})
				_, e := kube.CoreV1().ConfigMaps("runtime").Create(r.Context(), &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "runtime-slot-fence-" + intent.OperationID, Namespace: "runtime", UID: "operator-request", CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Minute)), Labels: map[string]string{"clawmanager.io/operator-recovery": slotKernelFenceSource}}, Immutable: &yes, Data: map[string]string{"request": string(raw)}}, metav1.CreateOptions{})
				if e != nil {
					t.Fatal(e)
				}
				message = "no native cleanup receipt"
			}
			current, err := kube.CoreV1().Pods("runtime").Get(r.Context(), pod.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			current.Status.ContainerStatuses[0].ContainerID = "containerd://new"
			if external {
				current.Status.ContainerStatuses[0].RestartCount = 1
				current.Status.ContainerStatuses[0].State.Running = &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(time.Now().Add(time.Second))}
			}
			// The last slot is quarantined after restart, so PodReady is false.
			// Stop recovery must still reach the coordinator by its pinned Pod IP.
			current.Status.Conditions[0].Status = corev1.ConditionFalse
			current.Status.ContainerStatuses[0].LastTerminationState.Terminated = &corev1.ContainerStateTerminated{ContainerID: "containerd://old", Message: string(message), FinishedAt: metav1.NewTime(time.Now())}
			if _, err = kube.CoreV1().Pods("runtime").UpdateStatus(r.Context(), current, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			body = `{"pending":true,"confirmed":false,"stopped":false}`
		case strings.HasSuffix(r.URL.Path, "/reconcile-stop"):
			reconcileCalls++
			if len(mode) > 0 && !external && reconcileCalls == 1 {
				return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("slot unavailable"))}, nil
			}
			var proof runtimeSlotContainerProof
			if err := json.NewDecoder(r.Body).Decode(&proof); err != nil {
				t.Fatal(err)
			}
			if proof.ContainerID != "containerd://old" || !proof.CleanupConfirmed {
				t.Fatal("writer reconciliation lacked exact terminated container")
			}
			if external && (proof.ProofSource != slotKernelFenceSource || proof.FenceRequestUID != "operator-request" || proof.Graceful) {
				t.Fatal("external fencing origin was lost")
			}
			reconciled = true
			b, _ := json.Marshal(map[string]any{"gateway_id": proof.GatewayID, "operation_id": proof.OperationID, "worker_epoch": proof.WorkerEpoch, "container_id": proof.ContainerID, "stopped": true, "confirmed": true, "unmounted": true, "graceful": proof.Graceful})
			body = string(b)
		case strings.HasSuffix(r.URL.Path, "/release-stop"):
			var proof runtimeSlotContainerProof
			if err := json.NewDecoder(r.Body).Decode(&proof); err != nil {
				t.Fatal(err)
			}
			current, err := kube.CoreV1().ConfigMaps("runtime").Get(r.Context(), "lite-instance-7", metav1.GetOptions{})
			if err != nil || current.Data["desired_state"] != "stopped" {
				t.Fatal("worker released slot before controller stop was durable")
			}
			b, _ := json.Marshal(map[string]any{"gateway_id": proof.GatewayID, "operation_id": proof.OperationID, "worker_epoch": proof.WorkerEpoch, "container_id": proof.ContainerID, "released": true})
			body = string(b)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	configureNoExternalWorkspaceWrites(t, client)
	ctx := context.Background()
	if _, err := client.CreateGateway(ctx, "https://10.0.0.2:19090", RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 1, AgentType: "openclaw", GatewayPort: 20000}); err != nil {
		t.Fatal(err)
	}
	var stopErr error
	if len(mode) > 0 && mode[0] == "retry" {
		bounded, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
		err := client.DeleteGateway(bounded, "https://10.0.0.2:19090", "gw-7-1")
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("missing bounded pending stop: %v", err)
		}
		cm, err := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-7", metav1.GetOptions{})
		if err != nil || reconciled || cm.Data["desired_state"] != "draining" || cm.Data["container_termination_proof"] != "" || cm.Data["container_stop_intent"] == "" {
			t.Fatal("timeout released unconfirmed writer or lost recovery intent")
		}
	}
	if graceful {
		stopErr = client.(RuntimeIdleStopClient).IdleStop(ctx, "https://10.0.0.2:19090", "gw-7-1", RuntimeIdleStopRequest{InstanceID: 7, Generation: 1, PodUID: "pod-uid"})
	} else {
		stopErr = client.DeleteGateway(ctx, "https://10.0.0.2:19090", "gw-7-1")
	}
	if stopErr != nil {
		t.Fatal(stopErr)
	}
	cm, err := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-7", metav1.GetOptions{})
	if err != nil || !reconciled || cm.Data["desired_state"] != "stopped" || cm.Data["container_termination_proof"] == "" || cm.Data["container_stop_released"] != "true" {
		t.Fatalf("writer stop was not durably coordinated: %+v %v", cm, err)
	}
	current, _ := kube.CoreV1().Pods("runtime").Get(ctx, pod.Name, metav1.GetOptions{})
	if current.Status.ContainerStatuses[1].ContainerID != "containerd://other" {
		t.Fatal("other slot identity changed")
	}
	if graceful {
		if err := client.(RuntimeIdleStopClient).ConfirmIdleStop(ctx, "https://10.0.0.2:19090", "gw-7-1", RuntimeIdleStopRequest{InstanceID: 7, Generation: 1, PodUID: "pod-uid"}); err != nil {
			t.Fatalf("unready Pod blocked graceful receipt recovery: %v", err)
		}
	}
}

func TestNFSContainerStopRejectsReusedPodIPBeforeWorkerCall(t *testing.T) {
	pod := slotTestPod()
	pod.Labels["clawmanager.io/sandbox-profile"] = "nfs-rootless-v1"
	kube := fake.NewSimpleClientset(pod)
	calls := 0
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"gateway_id":"gw-7-1"}`))}, nil
	})})
	configureNoExternalWorkspaceWrites(t, client)
	ctx := context.Background()
	if _, err := client.CreateGateway(ctx, "https://10.0.0.2:19090", RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 1, AgentType: "openclaw", GatewayPort: 20000}); err != nil {
		t.Fatal(err)
	}
	pod.UID = "replacement-pod"
	if _, err := kube.CoreV1().Pods("runtime").Update(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteGateway(ctx, "https://10.0.0.2:19090", "gw-7-1"); err == nil {
		t.Fatal("stale assignment authorized a different Pod")
	}
	if calls != 1 {
		t.Fatal("stale stop reached replacement worker")
	}
}

func TestNFSProfileRejectsLegacyStoppedReceipt(t *testing.T) {
	pod := slotTestPod()
	pod.Labels["clawmanager.io/sandbox-profile"] = "nfs-rootless-v1"
	pod.Spec.Containers = []corev1.Container{{Name: "slot-0", Env: []corev1.EnvVar{{Name: "RUNTIME_GATEWAY_PORT_START", Value: "20000"}}}}
	kube := fake.NewSimpleClientset(pod)
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		body := `{"gateway_id":"gw-7-1","stopped":true,"confirmed":true,"unmounted":true}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	configureNoExternalWorkspaceWrites(t, client)
	ctx := context.Background()
	if _, err := client.CreateGateway(ctx, "https://10.0.0.2:19090", RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 1, AgentType: "openclaw", GatewayPort: 20000}); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteGateway(ctx, "https://10.0.0.2:19090", "gw-7-1"); err == nil {
		t.Fatal("HTTP acknowledgement released writer without actual old container termination")
	}
	cm, err := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-7", metav1.GetOptions{})
	if err != nil || cm.Data["desired_state"] != "draining" {
		t.Fatalf("uncertain writer ownership was not retained: %+v %v", cm, err)
	}
}

func TestNFSContainerStopRejectsUnresolvedManagementWrites(t *testing.T) {
	pod := slotTestPod()
	pod.Labels["clawmanager.io/sandbox-profile"] = "nfs-rootless-v1"
	kube := fake.NewSimpleClientset(pod)
	calls := 0
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"gateway_id":"gw-7-1"}`))}, nil
	})})
	ctx := context.Background()
	if err := ConfigureSlotWorkspaceWriteActivation(client, func(context.Context, runtimeauth.Identity) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateGateway(ctx, "https://10.0.0.2:19090", RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 2, Generation: 1, AgentType: "openclaw", GatewayPort: 20000}); err != nil {
		t.Fatal(err)
	}
	if err := ConfigureSlotWorkspaceWriteFence(client, func(context.Context, runtimeauth.Identity) error { return io.ErrUnexpectedEOF }); err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteGateway(ctx, "https://10.0.0.2:19090", "gw-7-1"); err == nil {
		t.Fatal("management write uncertainty was ignored")
	}
	if calls != 1 {
		t.Fatal("old slot reset was started while a management writer was unresolved")
	}
	cm, err := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-7", metav1.GetOptions{})
	if err != nil || cm.Data["desired_state"] != "draining" {
		t.Fatal("uncertain management writer released assignment")
	}
}
