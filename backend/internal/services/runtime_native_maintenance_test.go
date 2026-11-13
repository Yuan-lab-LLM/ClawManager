package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes/fake"
)

func TestNFSNativePreflightRequiresTerminatedMaintenanceContainer(t *testing.T) {
	pod := slotTestPod()
	pod.Labels["clawmanager.io/sandbox-profile"] = "nfs-rootless-v1"
	kube := fake.NewSimpleClientset(pod)
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"workspace_path":"/workspace","status":"complete"}`))}, nil
	})})
	_, err := client.(RuntimeUpgradeAgentClient).PreflightWorkspace(context.Background(), "https://10.0.0.2:19090", RuntimeAgentWorkspaceRequest{RolloutID: "rollout-test", InstanceID: 7, UserID: 2, Generation: 1})
	if err == nil {
		t.Fatal("native CLI result published before exact maintenance container termination")
	}
}

func TestNFSNativeMaintenanceControllerYieldsAndReplaysExactOperation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		controller bool
		action     string
	}{
		{"controller-compatibility-yields-without-proof", true, "compatibility"},
		{"api-compatibility-keeps-upgrade-timeout", false, "compatibility"},
		{"controller-inventory-keeps-upgrade-timeout", true, "inventory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			yields := tc.controller && tc.action == "compatibility"
			pod := slotTestPod()
			pod.Labels["clawmanager.io/sandbox-profile"] = "nfs-rootless-v1"
			kube := fake.NewSimpleClientset(pod)
			starts, executions, finishes, releases := 0, 0, 0, 0
			var intent runtimeSlotStopIntent
			provideProof := func(ctx context.Context) {
				t.Helper()
				commitment, _ := containerTerminationCommitment(runtimeSlotTerminationMessage{runtimeSlotStopIntent: intent, CleanupConfirmed: true})
				current, err := kube.CoreV1().Pods("runtime").Get(ctx, pod.Name, metav1.GetOptions{})
				if err != nil {
					t.Fatal(err)
				}
				current.Status.Conditions[0].Status = corev1.ConditionFalse
				current.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "slot-0", LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ContainerID: "containerd://maintenance-old", Message: commitment, FinishedAt: metav1.NewTime(time.Now())}}}}
				if _, err = kube.CoreV1().Pods("runtime").UpdateStatus(ctx, current, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				var body []byte
				switch {
				case strings.HasSuffix(r.URL.Path, "/start"):
					starts++
					var op runtimeNativeUpgradeOperation
					if err := json.NewDecoder(r.Body).Decode(&op); err != nil {
						t.Fatal(err)
					}
					if intent.OperationID == "" {
						executions++
						child := op
						child.Request.LeaseToken = ""
						canonical, _ := canonicalNativeMaintenance(child)
						digest := sha256.Sum256(canonical)
						intent = runtimeSlotStopIntent{Version: 1, OperationID: op.OperationID, PodUID: "pod-uid", Slot: "slot-0", GatewayID: "gw-7-1", InstanceID: 7, UserID: 2, Generation: 1, WorkerEpoch: strings.Repeat("2", 32), AssignmentDigest: hex.EncodeToString(digest[:]), Kind: "maintenance"}
					} else if op.OperationID != intent.OperationID {
						t.Fatal("controller retry dispatched another helper operation")
					}
					if !yields {
						// API and non-compatibility controller calls keep their long timeout.
						select {
						case <-time.After(2200 * time.Millisecond):
						case <-r.Context().Done():
							return nil, r.Context().Err()
						}
						provideProof(r.Context())
					}
					body, _ = json.Marshal(map[string]any{"pending": true, "intent": intent})
				case strings.HasSuffix(r.URL.Path, "/finish"):
					finishes++
					var proof runtimeSlotContainerProof
					if err := json.NewDecoder(r.Body).Decode(&proof); err != nil || proof.runtimeSlotStopIntent != intent || proof.ContainerID != "containerd://maintenance-old" {
						t.Fatal("finish lacks the original operation's exact Kubelet proof")
					}
					body, _ = json.Marshal(runtimeNativeMaintenanceResult{Intent: intent, Result: json.RawMessage(`{"workspace_path":"/workspace","file_count":7}`), Confirmed: true})
				case strings.HasSuffix(r.URL.Path, "/release"):
					releases++
					secrets, err := kube.CoreV1().Secrets("runtime").List(r.Context(), metav1.ListOptions{})
					if err != nil || len(secrets.Items) != 1 {
						t.Fatal("release preceded durable result")
					}
					body, _ = json.Marshal(map[string]any{"operation_id": intent.OperationID, "released": true})
				default:
					t.Fatalf("unexpected native transport %s", r.URL.Path)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
			})})
			parent, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			ctx := parent
			if tc.controller {
				ctx = context.WithValue(ctx, upgradeClaimKey{}, "controller-claim")
			}
			req := RuntimeAgentWorkspaceRequest{RolloutID: "rollout-test", InstanceID: 7, UserID: 2, Generation: 1}
			started := time.Now()
			var result struct {
				FileCount int `json:"file_count"`
			}
			run := func() error {
				return client.(*slotRuntimeAgentClient).runNativeMaintenance(ctx, "https://10.0.0.2:19090", tc.action, req, pod, &result)
			}
			err := run()
			if yields {
				if !errors.Is(err, ErrRuntimeUpgradeBusy) || !errors.Is(err, context.DeadlineExceeded) || parent.Err() != nil || time.Since(started) > 4*time.Second {
					t.Fatalf("controller did not yield its work slice: elapsed=%s err=%v parent=%v", time.Since(started), err, parent.Err())
				}
				if finishes != 0 || releases != 0 {
					t.Fatal("unproven maintenance was finished or released")
				}
				maps, _ := kube.CoreV1().ConfigMaps("runtime").List(ctx, metav1.ListOptions{})
				for _, cm := range maps.Items {
					if cm.Labels["clawmanager.io/purpose"] == "lite-native-maintenance" && (cm.Data["operation_id"] != intent.OperationID || cm.Data["intent"] == "" || cm.Data["state"] != "running" || cm.Data["result_secret"] != "" || cm.Data["termination_proof"] != "") {
						t.Fatal("yield lost or prematurely completed durable operation")
					}
					if cm.Labels["clawmanager.io/purpose"] == "lite-native-maintenance-owner" && cm.Data["state"] != "active" {
						t.Fatal("yield released maintenance owner without proof")
					}
				}
				secrets, _ := kube.CoreV1().Secrets("runtime").List(ctx, metav1.ListOptions{})
				if len(secrets.Items) != 0 {
					t.Fatal("yield published result without proof")
				}
				provideProof(ctx)
				err = run()
			}
			if err != nil || result.FileCount != 7 {
				t.Fatalf("native result unavailable after proof: %+v %v", result, err)
			}
			err = run()
			wantStarts := 1
			if yields {
				wantStarts = 2
			}
			if err != nil || result.FileCount != 7 || starts != wantStarts || executions != 1 || finishes != 1 || releases != 1 {
				t.Fatalf("retry changed native result or repeated helper work: %+v %v start=%d execution=%d finish=%d release=%d", result, err, starts, executions, finishes, releases)
			}
			maps, err := kube.CoreV1().ConfigMaps("runtime").List(ctx, metav1.ListOptions{})
			if err != nil || len(maps.Items) != 2 {
				t.Fatalf("durable operation and owner unavailable: %v", err)
			}
			for _, cm := range maps.Items {
				if cm.Data["state"] != "released" || cm.Data["operation_id"] != intent.OperationID {
					t.Fatal("proven operation or owner did not release")
				}
			}
		})
	}
}

func TestNFSNativeMaintenanceControllerHTTPDeadlineIsBusy(t *testing.T) {
	pod := slotTestPod()
	kube := fake.NewSimpleClientset(pod)
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}).(*slotRuntimeAgentClient)
	parent, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	ctx := context.WithValue(parent, upgradeClaimKey{}, "controller-claim")
	err := client.runNativeMaintenance(ctx, "https://10.0.0.2:19090", "compatibility", RuntimeAgentWorkspaceRequest{RolloutID: "rollout-test", InstanceID: 7, UserID: 2, Generation: 1}, pod, &RuntimeAgentWorkspacePreflight{})
	if !errors.Is(err, ErrRuntimeUpgradeBusy) || !errors.Is(err, context.DeadlineExceeded) || parent.Err() != nil {
		t.Fatalf("local HTTP deadline must yield without consuming failure budget: err=%v parent=%v", err, parent.Err())
	}
}

func TestNFSNativeMaintenanceCommitsProofAndResultBeforeRelease(t *testing.T) {
	pod := slotTestPod()
	pod.Labels["clawmanager.io/sandbox-profile"] = "nfs-rootless-v1"
	kube := fake.NewSimpleClientset(pod)
	starts, finishes, releases, resumes := 0, 0, 0, 0
	sessionAvailable := true
	var intent runtimeSlotStopIntent
	client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		var body []byte
		switch {
		case strings.HasSuffix(r.URL.Path, "/start"):
			starts++
			var op runtimeNativeUpgradeOperation
			if err := json.NewDecoder(r.Body).Decode(&op); err != nil {
				t.Fatal(err)
			}
			if op.Request.UID != 200007 || op.Request.GID != 200007 {
				t.Fatal("native operation did not use stable instance identity")
			}
			child := op
			child.Request.LeaseToken = ""
			canonical, _ := canonicalNativeMaintenance(child)
			digest := sha256.Sum256(canonical)
			intent = runtimeSlotStopIntent{Version: 1, OperationID: op.OperationID, PodUID: "pod-uid", Slot: "slot-0", GatewayID: "gw-7-1", InstanceID: 7, UserID: 2, Generation: 1, WorkerEpoch: strings.Repeat("2", 32), AssignmentDigest: hex.EncodeToString(digest[:]), Kind: "maintenance"}
			commitment, _ := containerTerminationCommitment(runtimeSlotTerminationMessage{runtimeSlotStopIntent: intent, CleanupConfirmed: true})
			current, err := kube.CoreV1().Pods("runtime").Get(r.Context(), pod.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			current.Status.Conditions[0].Status = corev1.ConditionFalse
			current.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "slot-0", LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ContainerID: "containerd://maintenance-old", Message: commitment, FinishedAt: metav1.NewTime(time.Now())}}}}
			if _, err = kube.CoreV1().Pods("runtime").UpdateStatus(r.Context(), current, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			body, _ = json.Marshal(map[string]any{"pending": true, "intent": intent})
		case strings.HasSuffix(r.URL.Path, "/resume"):
			resumes++
			var op runtimeNativeUpgradeOperation
			if err := json.NewDecoder(r.Body).Decode(&op); err != nil || op.OperationID != intent.OperationID {
				t.Fatal("resume changed operation")
			}
			sessionAvailable = true
			body, _ = json.Marshal(map[string]any{"pending": true, "intent": intent})
		case strings.HasSuffix(r.URL.Path, "/finish"):
			finishes++
			var proof runtimeSlotContainerProof
			if err := json.NewDecoder(r.Body).Decode(&proof); err != nil {
				t.Fatal(err)
			}
			if proof.ContainerID != "containerd://maintenance-old" || proof.runtimeSlotStopIntent != intent {
				t.Fatal("native result lacked actual exact container proof")
			}
			body, _ = json.Marshal(runtimeNativeMaintenanceResult{Intent: intent, Result: json.RawMessage(`{"workspace_path":"/workspace","file_count":7}`), Confirmed: true})
		case strings.HasSuffix(r.URL.Path, "/release"):
			releases++
			if releases == 1 {
				sessionAvailable = false
				return &http.Response{StatusCode: 409, Body: io.NopCloser(strings.NewReader("coordinator restarted before release"))}, nil
			}
			if !sessionAvailable {
				return &http.Response{StatusCode: 409, Body: io.NopCloser(strings.NewReader("native operation is not finalized"))}, nil
			}
			secrets, err := kube.CoreV1().Secrets("runtime").List(r.Context(), metav1.ListOptions{})
			if err != nil || len(secrets.Items) != 1 {
				t.Fatal("slot released before native result was durable")
			}
			body, _ = json.Marshal(map[string]any{"operation_id": intent.OperationID, "released": true})
		default:
			t.Fatalf("unexpected native transport %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})})
	ctx := context.Background()
	req := RuntimeAgentWorkspaceRequest{RolloutID: "rollout-test", InstanceID: 7, UserID: 2, Generation: 1, LeaseToken: "private-native-lease"}
	if _, err := client.(RuntimeUpgradeAgentClient).PreflightWorkspace(ctx, "https://10.0.0.2:19090", req); err == nil {
		t.Fatal("injected restart was ignored")
	}
	// Another CM process shares only the durable Kubernetes records.
	previous := client.(*slotRuntimeAgentClient)
	client = NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", previous.runtimeAgentHTTPClient.httpClient)
	for i := 0; i < 2; i++ {
		result, err := client.(RuntimeUpgradeAgentClient).PreflightWorkspace(ctx, "https://10.0.0.2:19090", req)
		if err != nil || result.FileCount != 7 {
			t.Fatalf("native result unavailable after proof: %+v %v", result, err)
		}
	}
	if starts != 1 || finishes != 1 || releases != 2 || resumes != 1 {
		t.Fatalf("native replay repeated CLI work: start=%d finish=%d release=%d", starts, finishes, releases)
	}
	maps, err := kube.CoreV1().ConfigMaps("runtime").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, cm := range maps.Items {
		raw, _ := json.Marshal(cm)
		if strings.Contains(string(raw), req.LeaseToken) {
			t.Fatal("native management credential leaked into ConfigMap")
		}
		if cm.Labels["clawmanager.io/purpose"] == "lite-native-maintenance" && (cm.Data["state"] != "released" || cm.Data["termination_proof"] == "" || cm.Data["result_secret"] == "") {
			t.Fatal("native maintenance lacks durable completion")
		}
	}
}

func TestNativeMaintenanceOwnerBlocksUnknownOldActionAndPod(t *testing.T) {
	pod := slotTestPod()
	pod.Labels["clawmanager.io/sandbox-profile"] = "nfs-rootless-v1"
	kube := fake.NewSimpleClientset(pod)
	c := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", nil).(*slotRuntimeAgentClient)
	req := RuntimeAgentWorkspaceRequest{RolloutID: "rollout-test", InstanceID: 7, UserID: 2, Generation: 1}
	if _, _, err := c.ensureNativeMaintenance(context.Background(), "https://10.0.0.2:19090", pod, "inventory", req); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.ensureNativeMaintenance(context.Background(), "https://10.0.0.2:19090", pod, "compatibility", req); err == nil {
		t.Fatal("another action replaced an unconfirmed native execution")
	}
	replacement := pod.DeepCopy()
	replacement.UID = "replacement"
	if _, _, err := c.ensureNativeMaintenance(context.Background(), "https://10.0.0.2:19090", replacement, "inventory", req); err == nil {
		t.Fatal("Pod replacement bypassed old native writer uncertainty")
	}
}
