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
	"clawreef/internal/runtimeauth"
	k8sservice "clawreef/internal/services/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestNativeRecoveryRequiresFreshAutomaticExitProvenance(t *testing.T) {
	for _, scenario := range []string{"verified", "healthy", "instance", "owner", "generation", "gateway", "port", "pod", "epoch", "digest", "idle", "attempt", "quarantine", "profile", "opaque", "unreadable", "stopped", "stopping", "configuration"} {
		t.Run(scenario, func(t *testing.T) {
			s, r, b, _, i, now := recoveryFixture()
			message := "gateway_stop_failed"
			i.RuntimeErrorMessage = &message
			r.byID[i.ID] = &i
			b.bindings[i.ID].ErrorMessage = &message
			b.bindings[i.ID].GatewayPort = 20000
			b.bindings[i.ID].RuntimeType = "openclaw"
			b.bindings[i.ID].WorkspacePath = *i.WorkspacePath
			endpoint := "https://10.0.0.2:19090"
			p := s.podRepo.(*fakeRuntimePodRepo).pods[9]
			p.AgentEndpoint = &endpoint
			p.RuntimeType = "openclaw"
			p.PodName = "pool"
			p.Namespace = "runtime"
			pod := slotTestPod()
			pod.Labels["clawmanager.io/sandbox-profile"] = "nfs-rootless-v1"
			pod.Spec.Containers = []corev1.Container{{Name: "slot-0", Env: []corev1.EnvVar{{Name: "RUNTIME_GATEWAY_PORT_START", Value: "20000"}}}}
			assignment := map[string]any{"pod_uid": "pod-uid", "gateway_id": "gw-7-2", "instance_id": 7, "user_id": 3, "generation": 2, "worker_epoch": strings.Repeat("2", 32), "state": "starting"}
			gateway := map[string]any{"gateway_id": "gw-7-2", "instance_id": 7, "user_id": 3, "generation": 2, "runtime_type": "openclaw", "gateway_port": 20000, "state": "error", "error_message": "gateway_stop_failed", "restart_attempt": 1}
			snapshot := map[string]any{"pod_uid": "pod-uid", "worker_epoch": strings.Repeat("2", 32), "digest": strings.Repeat("d", 64), "idle_confirmed": false, "quarantined": true, "assignment": assignment, "gateways": []any{gateway}}
			switch scenario {
			case "healthy":
				gateway["state"] = "running"
			case "instance":
				gateway["instance_id"] = 8
			case "owner":
				assignment["user_id"] = 4
			case "generation":
				gateway["generation"] = 3
			case "gateway":
				gateway["gateway_id"] = "gw-8-2"
			case "port":
				gateway["gateway_port"] = 20003
			case "pod":
				snapshot["pod_uid"] = "other"
			case "epoch":
				assignment["worker_epoch"] = strings.Repeat("3", 32)
			case "digest":
				snapshot["digest"] = ""
			case "idle":
				assignment["idle_stop"] = true
			case "attempt":
				gateway["restart_attempt"] = 0
			case "quarantine":
				snapshot["quarantined"] = false
			case "profile":
				pod.Labels["clawmanager.io/sandbox-profile"] = "other"
			case "opaque":
				gateway["error_message"] = "gateway_start_failed"
			case "stopping":
				i.AutoStopPhase = "draining"
				r.byID[i.ID] = &i
			case "configuration":
				code := "INVALID_CREDENTIALS"
				i.RuntimeErrorCode = &code
				r.byID[i.ID] = &i
			case "stopped":
				i.Status = "stopped"
				r.byID[i.ID] = &i
			}
			reads, deletes := 0, 0
			client := NewSlotRuntimeAgentClientWithHTTPClient("token", fake.NewSimpleClientset(pod), "runtime", &http.Client{Transport: slotRoundTripper(func(req *http.Request) (*http.Response, error) {
				if req.Header.Get("X-ClawManager-Control-Token") != "token" {
					t.Fatal("missing authentication")
				}
				if req.Method != http.MethodGet {
					deletes++
					return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("pending"))}, nil
				}
				reads++
				status := 200
				if scenario == "unreadable" {
					status = 503
				}
				raw, _ := json.Marshal(snapshot)
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
			})}).(*slotRuntimeAgentClient)
			request, _ := json.Marshal(RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 3, Generation: 2, PodUID: "pod-uid", AgentType: "openclaw", WorkspacePath: *i.WorkspacePath, GatewayPort: 20000})
			if err := client.assignments.EnsureDesired(context.Background(), k8sservice.RuntimeAssignment{InstanceID: 7, UserID: 3, Generation: 2, RuntimeType: "openclaw", Endpoint: endpoint, GatewayID: "gw-7-2", PodUID: "pod-uid", Request: request}); err != nil {
				t.Fatal(err)
			}
			s.agentClient = client
			s.reconcileInstanceRecoveryLocked(context.Background(), i, now)
			if scenario == "verified" {
				if r.state == nil || r.state.NextRetryAt == nil || r.state.Attempts != 0 || reads != 1 {
					t.Fatalf("verified native exit was not admitted: state=%+v reads=%d", r.state, reads)
				}
				// A second authenticated observation must reject a source that became healthy.
				gateway["state"] = "running"
				s.reconcileInstanceRecoveryLocked(context.Background(), i, now.Add(30*time.Second))
			} else if r.state != nil {
				t.Fatalf("unverified native exit created recovery intent: %+v", r.state)
			}
			if deletes != 0 || b.bindings[i.ID] == nil {
				t.Fatal("unverified/unconfirmed source released writer")
			}
		})
	}
}

func TestRecoveryInspectionExplainsUnclassifiedSource(t *testing.T) {
	for _, message := range []string{"gateway_stop_failed", "arbitrary reported error"} {
		s, r, b, _, i, _ := recoveryFixture()
		i.RuntimeErrorMessage = &message
		r.byID[i.ID] = &i
		b.bindings[i.ID].ErrorMessage = &message
		info, err := s.InspectInstanceRecovery(context.Background(), i.ID)
		if err != nil || info.BlockerCode == "" || info.Blocker == "" {
			t.Fatalf("silent recovery blocker: %+v %v", info, err)
		}
		if r.state != nil {
			t.Fatal("inspection armed recovery")
		}
		if _, ok := runtimeRecoveryFailureReason(models.Instance{Status: "error"}, b.bindings[i.ID]); ok {
			t.Fatal("generic stop failure was globally whitelisted")
		}
	}
}

func TestNativeRecoveryContinuesOnlyOwnedStopAndWaitsForProof(t *testing.T) {
	for _, scenario := range []string{"confirmed", "pending", "graceful", "no durable recovery", "wrong recovery source"} {
		t.Run(scenario, func(t *testing.T) {
			s, r, b, _, i, now := recoveryFixture()
			message := "gateway_stop_failed"
			i.RuntimeErrorMessage = &message
			r.byID[i.ID] = &i
			binding := b.bindings[i.ID]
			binding.RuntimeType = "openclaw"
			binding.WorkspacePath = *i.WorkspacePath
			binding.GatewayPort = 20000
			binding.ErrorMessage = &message
			endpoint := "https://10.0.0.2:19090"
			source := s.podRepo.(*fakeRuntimePodRepo).pods[9]
			source.AgentEndpoint = &endpoint
			source.RuntimeType = "openclaw"
			source.PodName = "pool"
			source.Namespace = "runtime"
			pod := slotTestPod()
			pod.Labels["clawmanager.io/sandbox-profile"] = "nfs-rootless-v1"
			pod.Spec.Containers = []corev1.Container{{Name: "slot-0", Env: []corev1.EnvVar{{Name: "RUNTIME_GATEWAY_PORT_START", Value: "20000"}}}}
			kube := fake.NewSimpleClientset(pod)
			stopCalls, reconciles, releases := 0, 0, 0
			client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(req *http.Request) (*http.Response, error) {
				var response any
				switch {
				case req.Method == http.MethodDelete:
					stopCalls++
					var intent runtimeSlotStopIntent
					if err := json.NewDecoder(req.Body).Decode(&intent); err != nil {
						t.Fatal(err)
					}
					if scenario == "confirmed" {
						commitment, _ := containerTerminationCommitment(runtimeSlotTerminationMessage{runtimeSlotStopIntent: intent, CleanupConfirmed: true})
						current, _ := kube.CoreV1().Pods("runtime").Get(req.Context(), pod.Name, metav1.GetOptions{})
						current.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "slot-0", ContainerID: "containerd://new", LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ContainerID: "containerd://old", Message: commitment, FinishedAt: metav1.Now()}}}}
						if _, err := kube.CoreV1().Pods("runtime").UpdateStatus(req.Context(), current, metav1.UpdateOptions{}); err != nil {
							t.Fatal(err)
						}
					}
					response = map[string]any{"pending": true}
				case strings.HasSuffix(req.URL.Path, "/reconcile-stop"), strings.HasSuffix(req.URL.Path, "/release-stop"):
					var proof runtimeSlotContainerProof
					if err := json.NewDecoder(req.Body).Decode(&proof); err != nil {
						t.Fatal(err)
					}
					if proof.ContainerID != "containerd://old" || !proof.CleanupConfirmed {
						t.Fatal("missing exact termination")
					}
					response = map[string]any{"gateway_id": proof.GatewayID, "operation_id": proof.OperationID, "worker_epoch": proof.WorkerEpoch, "container_id": proof.ContainerID, "stopped": true, "confirmed": true, "unmounted": true, "released": true}
					if strings.HasSuffix(req.URL.Path, "/reconcile-stop") {
						reconciles++
					} else {
						releases++
					}
				default:
					t.Fatalf("continuation unexpectedly trusted a missing worker report: %s", req.URL.Path)
				}
				raw, _ := json.Marshal(response)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
			})}).(*slotRuntimeAgentClient)
			configureNoExternalWorkspaceWrites(t, client)
			request, _ := json.Marshal(RuntimeAgentCreateGatewayRequest{InstanceID: i.ID, UserID: i.UserID, Generation: i.RuntimeGeneration, PodUID: "pod-uid", AgentType: "openclaw", WorkspacePath: *i.WorkspacePath, GatewayPort: 20000})
			if err := client.assignments.EnsureDesired(context.Background(), k8sservice.RuntimeAssignment{InstanceID: i.ID, UserID: i.UserID, Generation: i.RuntimeGeneration, RuntimeType: "openclaw", Endpoint: endpoint, GatewayID: binding.GatewayID, PodUID: "pod-uid", Request: request}); err != nil {
				t.Fatal(err)
			}
			if err := client.assignments.MarkDraining(context.Background(), endpoint, binding.GatewayID); err != nil {
				t.Fatal(err)
			}
			intent := runtimeSlotStopIntent{Version: 1, OperationID: strings.Repeat("1", 32), PodUID: "pod-uid", Slot: "slot-0", GatewayID: binding.GatewayID, InstanceID: i.ID, UserID: i.UserID, Generation: i.RuntimeGeneration, WorkerEpoch: strings.Repeat("2", 32), AssignmentDigest: strings.Repeat("d", 64), Graceful: scenario == "graceful"}
			if _, err := client.assignments.BeginContainerStop(context.Background(), endpoint, intent); err != nil {
				t.Fatal(err)
			}
			r.state = &models.InstanceRuntimeRecovery{InstanceID: i.ID, Generation: i.RuntimeGeneration, Reason: runtimeRecoveryNativeResetReason, Attempts: 1, NextRetryAt: &now, SourcePodID: 9, SourceGatewayPort: 20000, UpdatedAt: now}
			if scenario == "no durable recovery" {
				r.state = nil
			}
			if scenario == "wrong recovery source" {
				r.state.SourcePodID = 10
			}
			s.agentClient = client
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			s.reconcileInstanceRecoveryLocked(ctx, i, now)
			current, _ := r.GetByID(i.ID)
			if scenario == "confirmed" {
				if stopCalls != 1 || reconciles != 1 || releases != 1 || b.bindings[i.ID] != nil || current.RuntimeGeneration != i.RuntimeGeneration+1 {
					t.Fatalf("verified reset did not advance exactly once: stops=%d reconcile=%d release=%d current=%+v", stopCalls, reconciles, releases, current)
				}
			} else {
				if b.bindings[i.ID] == nil || current.RuntimeGeneration != i.RuntimeGeneration || reconciles != 0 || releases != 0 {
					t.Fatal("missing proof or unrelated stop released writer")
				}
				if scenario != "pending" && stopCalls != 0 {
					t.Fatal("unowned/graceful stop was continued")
				}
				if scenario == "pending" && (stopCalls != 1 || r.state.Attempts != 2) {
					t.Fatal("pending stop lost bounded attempt")
				}
			}
		})
	}
}

func TestNativeRecoveryResumesInterruptedDrainingBeforeStopIntent(t *testing.T) {
	for _, scenario := range []string{"resume", "unowned", "idle", "manual", "healthy", "wrong source", "wrong generation", "wrong port", "no attempt"} {
		t.Run(scenario, func(t *testing.T) {
			s, r, b, _, i, now := recoveryFixture()
			message := "gateway_stop_failed"
			i.RuntimeErrorMessage = &message
			r.byID[i.ID] = &i
			binding := b.bindings[i.ID]
			binding.RuntimeType = "openclaw"
			binding.WorkspacePath = *i.WorkspacePath
			binding.GatewayPort = 20000
			binding.ErrorMessage = &message
			endpoint := "https://10.0.0.2:19090"
			source := s.podRepo.(*fakeRuntimePodRepo).pods[9]
			source.AgentEndpoint = &endpoint
			source.RuntimeType = "openclaw"
			source.PodName = "pool"
			source.Namespace = "runtime"
			pod := slotTestPod()
			pod.Labels["clawmanager.io/sandbox-profile"] = "nfs-rootless-v1"
			pod.Spec.Containers = []corev1.Container{{Name: "slot-0", Env: []corev1.EnvVar{{Name: "RUNTIME_GATEWAY_PORT_START", Value: "20000"}}}}
			owner := map[string]any{"pod_uid": "pod-uid", "gateway_id": "gw-7-2", "instance_id": 7, "user_id": 3, "generation": 2, "worker_epoch": strings.Repeat("2", 32), "state": "starting"}
			gateway := map[string]any{"gateway_id": "gw-7-2", "instance_id": 7, "user_id": 3, "generation": 2, "runtime_type": "openclaw", "gateway_port": 20000, "state": "error", "error_message": "gateway_stop_failed", "restart_attempt": 1}
			snapshot := map[string]any{"pod_uid": "pod-uid", "worker_epoch": strings.Repeat("2", 32), "digest": strings.Repeat("d", 64), "idle_confirmed": false, "quarantined": true, "assignment": owner, "gateways": []any{gateway}}
			stops, fences := 0, 0
			client := NewSlotRuntimeAgentClientWithHTTPClient("token", fake.NewSimpleClientset(pod), "runtime", &http.Client{Transport: slotRoundTripper(func(req *http.Request) (*http.Response, error) {
				if req.Header.Get("X-ClawManager-Control-Token") != "token" {
					t.Fatal("missing authenticated snapshot")
				}
				if req.Method == http.MethodDelete {
					stops++
					return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("pending"))}, nil
				}
				if req.Method != http.MethodGet || req.URL.Path != "/v1/slots/slot-0/state" {
					t.Fatalf("unexpected call %s %s", req.Method, req.URL.Path)
				}
				raw, _ := json.Marshal(snapshot)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
			})}).(*slotRuntimeAgentClient)
			configureNoExternalWorkspaceWrites(t, client)
			client.workspaceWriteFence = func(context.Context, runtimeauth.Identity) error {
				fences++
				if fences == 1 {
					return errors.New("temporary workspace fence unavailable")
				}
				return nil
			}
			request, _ := json.Marshal(RuntimeAgentCreateGatewayRequest{InstanceID: i.ID, UserID: i.UserID, Generation: i.RuntimeGeneration, PodUID: "pod-uid", AgentType: "openclaw", WorkspacePath: *i.WorkspacePath, GatewayPort: 20000})
			if err := client.assignments.EnsureDesired(context.Background(), k8sservice.RuntimeAssignment{InstanceID: i.ID, UserID: i.UserID, Generation: i.RuntimeGeneration, RuntimeType: "openclaw", Endpoint: endpoint, GatewayID: binding.GatewayID, PodUID: "pod-uid", Request: request}); err != nil {
				t.Fatal(err)
			}
			s.agentClient = client
			s.reconcileInstanceRecoveryLocked(context.Background(), i, now)
			_, errs := s.reconcileInstanceRecoveryLocked(context.Background(), i, now.Add(30*time.Second))
			_, assignmentState, intent, _, err := client.assignments.ReadContainerStop(context.Background(), endpoint, binding.GatewayID)
			current, _ := r.GetByID(i.ID)
			if err != nil || len(errs) == 0 || assignmentState != "draining" || intent != nil || r.state.Attempts != 1 || stops != 0 || b.bindings[i.ID] == nil || current.RuntimeGeneration != i.RuntimeGeneration {
				t.Fatalf("failed to reproduce fenced pre-intent interruption: state=%s intent=%+v recovery=%+v errors=%v", assignmentState, intent, r.state, errs)
			}
			switch scenario {
			case "unowned":
				r.state = nil
			case "idle":
				owner["idle_stop"] = true
			case "manual":
				i.AutoStopPhase = "draining"
				r.byID[i.ID] = &i
			case "healthy":
				gateway["state"] = "running"
			case "wrong source":
				r.state.SourcePodID++
			case "wrong generation":
				r.state.Generation++
			case "wrong port":
				r.state.SourceGatewayPort++
			case "no attempt":
				r.state.Attempts = 0
			}
			s.reconcileInstanceRecoveryLocked(context.Background(), i, now.Add(149*time.Second))
			if stops != 0 || fences != 1 {
				t.Fatal("pre-intent interruption bypassed bounded retry deadline")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			s.reconcileInstanceRecoveryLocked(ctx, i, now.Add(150*time.Second))
			current, _ = r.GetByID(i.ID)
			_, _, intent, _, err = client.assignments.ReadContainerStop(context.Background(), endpoint, binding.GatewayID)
			if scenario == "resume" {
				if err != nil || stops != 1 || intent == nil || r.state.Attempts != 2 || r.state.NextRetryAt == nil || !r.state.NextRetryAt.Equal(now.Add(450*time.Second)) {
					t.Fatalf("owned pre-intent recovery did not resume within its budget: stops=%d intent=%+v recovery=%+v", stops, intent, r.state)
				}
			} else if stops != 0 || fences != 1 || intent != nil {
				t.Fatal("unowned/manual/idle/changed source resumed reset")
			}
			if b.bindings[i.ID] == nil || current.RuntimeGeneration != i.RuntimeGeneration {
				t.Fatal("pending stop released writer or advanced generation")
			}
		})
	}
}

func TestLegacyNativeRecoveryBudgetDoesNotOfferUnsupportedManualRetry(t *testing.T) {
	s, r, b, _, i, now := recoveryFixture()
	message := "gateway_stop_failed"
	i.RuntimeErrorMessage = &message
	r.byID[i.ID] = &i
	b.bindings[i.ID].ErrorMessage = &message
	r.state = &models.InstanceRuntimeRecovery{InstanceID: i.ID, Generation: i.RuntimeGeneration, Attempts: 3, Reason: runtimeRecoveryNativeResetReason, UpdatedAt: now}
	info, err := s.InspectInstanceRecovery(context.Background(), i.ID)
	if err != nil || info.BlockerCode != "budget" || strings.Contains(info.Blocker, "administrator can retry") || strings.Contains(info.Blocker, "administrator can Retry") {
		t.Fatalf("legacy budget offers an unsupported action: %+v %v", info, err)
	}
}
