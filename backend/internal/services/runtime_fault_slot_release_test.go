package services

import (
	"bytes"
	"clawreef/internal/models"
	"clawreef/internal/runtimeeviction"
	"context"
	"encoding/json"
	"errors"
	"io"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"net/http"
	"strings"
	"testing"
	"time"

	"clawreef/internal/services/k8s"
)

func TestFaultRecoveryCannotReleaseBindingBeforeNativeSlotFinalization(t *testing.T) {
	s, _, stopper := recoveryFlowFixture(t, "stopped")
	ctx := context.Background()
	plan, err := s.recoveryEvictionPreflight(ctx, RuntimeSchedulingRequest{SourcePodID: 1, Mode: "recovery", InstanceIDs: []int{101}})
	if err != nil || !plan.Allowed {
		t.Fatalf("preflight %v %+v", err, plan)
	}
	item := plan.Items[0]
	j := item.Recovery
	j.Scope.OperationID = "op"
	j.Receipt, err = stopper.RecoverRuntimeEviction(ctx, j.Scope, j.SourcePod)
	if err != nil {
		t.Fatal(err)
	}
	j.Phase = "source_stopped"
	j.SourcePod.Annotations[k8s.RuntimeFaultSandboxProfileAnnotation] = "nfs-rootless-v1"
	raw, _ := json.Marshal([]RuntimeSchedulingItem{item})
	if _, err = s.sess.SQL().Exec("INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,owner_token,created_at,updated_at) VALUES('op','eviction','processing','{}',?,?,NOW(),NOW())", string(raw), s.token); err != nil {
		t.Fatal(err)
	}
	if _, err = s.sess.SQL().Exec("INSERT INTO instance_lifecycle_manual_guards(instance_id,token,created_at) VALUES(101,'op',NOW())"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.commitRecoveredEviction(ctx, item, "op"); err == nil {
		t.Fatal("released CM binding while Runtime still holds quarantined assignment")
	}
	var count int
	row, _ := s.sess.SQL().QueryRow("SELECT COUNT(*) FROM instance_runtime_bindings WHERE id=71")
	if err = row.Scan(&count); err != nil || count != 1 {
		t.Fatal("unfinalized fault recovery removed binding")
	}
}

// HTTP and Kubernetes fixtures exercise the actual CM adapter without touching
// a real worker, business instance, or workspace.
func TestFaultSlotFinalizationRetriesImmutableAuthority(t *testing.T) {
	for _, scenario := range []string{"success", "lost reconcile checkpoint", "lost release reply", "wrong ack", "old container", "unknown native", "new worker", "changed profile", "unknown profile"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			j, pod := faultSlotTestJournal()
			phase := 0
			worker := strings.Repeat("b", 32)
			calls := []string{}
			kube := fake.NewSimpleClientset(pod)
			handler := func(r *http.Request) (*http.Response, error) {
				calls = append(calls, r.URL.Path)
				reply := func(status int, body any) (*http.Response, error) {
					raw, _ := json.Marshal(body)
					return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(raw)), Header: make(http.Header)}, nil
				}
				if r.URL.Path == "/v1/slots/slot-0/state" {
					if scenario == "unknown native" {
						return reply(200, map[string]any{"gateways": []any{}})
					}
					return reply(200, map[string]any{"pod_uid": "old", "worker_epoch": worker, "digest": strings.Repeat("d", 64), "idle_confirmed": phase == 2, "quarantined": phase != 2, "gateways": []any{}, "assignment": map[string]any{"pod_uid": "old", "worker_epoch": strings.Repeat("a", 32), "instance_id": 101, "user_id": 1, "generation": 4, "gateway_id": "gw-101-4", "state": map[bool]string{true: "stopped", false: "running"}[phase > 0]}})
				}
				var proof runtimeFaultSlotProof
				if err := json.NewDecoder(r.Body).Decode(&proof); err != nil {
					t.Fatal(err)
				}
				if proof.Scope != j.Scope || proof.WorkerEpoch != strings.Repeat("a", 32) || proof.AssignmentDigest != strings.Repeat("d", 64) || proof.CurrentWorkerEpoch != worker {
					t.Fatal("retry changed original recovery authority")
				}
				released := strings.HasSuffix(r.URL.Path, "release-fault-stop")
				if released {
					phase = 2
					if scenario == "lost release reply" && len(calls) < 5 {
						return nil, errors.New("lost release response")
					}
				} else {
					phase = 1
				}
				digest := runtimeeviction.ScopeDigest(j.Scope)
				if scenario == "wrong ack" {
					digest = strings.Repeat("f", 64)
				}
				return reply(200, map[string]any{"operation_id": j.Scope.OperationID, "scope_digest": digest, "gateway_id": j.Scope.GatewayID, "worker_epoch": worker, "stopped": true, "confirmed": true, "released": released})
			}
			c := NewSlotRuntimeAgentClientWithHTTPClient("control", kube, "runtime", &http.Client{Transport: slotRoundTripper(handler)}).(*slotRuntimeAgentClient)
			if scenario == "old container" {
				pod.Status.ContainerStatuses[0].ContainerID = j.Scope.ContainerID
				kube.CoreV1().Pods("runtime").UpdateStatus(ctx, pod, metav1.UpdateOptions{})
			}
			if scenario == "changed profile" {
				j.SourcePod.Annotations[k8s.RuntimeFaultSandboxProfileAnnotation] = ""
			}
			if scenario == "unknown profile" {
				j.SourcePod.Annotations[k8s.RuntimeFaultSandboxProfileAnnotation] = "future-profile"
				pod.Labels["clawmanager.io/sandbox-profile"] = "future-profile"
				kube.CoreV1().Pods("runtime").Update(ctx, pod, metav1.UpdateOptions{})
			}
			failSave := scenario == "lost reconcile checkpoint"
			checkpoint := func() error {
				if failSave && j.SlotRelease != nil && j.SlotRelease.Reconciled {
					failSave = false
					return errors.New("database unavailable")
				}
				return nil
			}
			err := c.finalizeFaultSlot(ctx, j, checkpoint)
			blocked := scenario == "wrong ack" || scenario == "old container" || scenario == "unknown native" || scenario == "changed profile" || scenario == "unknown profile"
			if blocked {
				if err == nil || j.SlotRelease != nil && j.SlotRelease.Released {
					t.Fatal("unproven slot released")
				}
				return
			}
			if scenario == "new worker" {
				j.SlotRelease.Released = false
				worker = strings.Repeat("c", 32)
				pod.Status.ContainerStatuses[0].ContainerID = "containerd://newer"
				kube.CoreV1().Pods("runtime").UpdateStatus(ctx, pod, metav1.UpdateOptions{})
				err = c.finalizeFaultSlot(ctx, j, checkpoint)
			}
			if err != nil {
				err = c.finalizeFaultSlot(ctx, j, checkpoint)
			}
			if err != nil || !faultSlotReleaseComplete(j) {
				t.Fatalf("did not finish same slot: %v %+v", err, j.SlotRelease)
			}
			if phase != 2 {
				t.Fatal("CM completed without native release")
			}
		})
	}
}
func faultSlotTestJournal() (*RuntimeFaultEvictionJournal, *corev1.Pod) {
	scope := runtimeeviction.Scope{Version: 1, OperationID: "recovery", Namespace: "runtime", PodName: "pool-old", PodUID: "old", NodeName: "node", NodeUID: "node-uid", NodeBootID: "boot", ContainerName: "slot-0", ContainerID: "containerd://old-container", RuntimeType: "opencode", GatewayID: "gw-101-4", WorkspaceRoot: "/workspaces", InstanceID: 101, UserID: 1, Generation: 4}
	receipt := &runtimeeviction.Receipt{ScopeDigest: runtimeeviction.ScopeDigest(scope), OperationID: scope.OperationID, PodUID: scope.PodUID, ContainerName: scope.ContainerName, NodeUID: scope.NodeUID, NodeBootID: scope.NodeBootID, StoppedContainerIDs: []string{scope.ContainerID}, LedgerDigest: strings.Repeat("a", 64), FinishedAt: time.Now().UTC(), Confirmed: true}
	raw, _ := json.Marshal(RuntimeAgentCreateGatewayRequest{GatewayPort: 20000})
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "runtime", Name: "pool-old", UID: "old", Labels: map[string]string{"clawmanager.io/runtime-protocol": "container-slots-v1", "clawmanager.io/sandbox-profile": "nfs-rootless-v1"}, Annotations: map[string]string{k8s.RuntimeFaultSandboxProfileAnnotation: "nfs-rootless-v1"}}, Spec: corev1.PodSpec{NodeName: "node", Containers: []corev1.Container{{Name: "slot-0", Env: []corev1.EnvVar{{Name: "RUNTIME_GATEWAY_PORT_START", Value: "20000"}}}}}, Status: corev1.PodStatus{PodIP: "10.1.0.2", ContainerStatuses: []corev1.ContainerStatus{{Name: "slot-0", ContainerID: "containerd://replacement", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
	return &RuntimeFaultEvictionJournal{Phase: "released", SourceUserID: 1, Scope: scope, Receipt: receipt, SourcePod: pod.DeepCopy(), SourceEndpoint: "https://10.1.0.2:19090", SourceBinding: models.InstanceRuntimeBinding{InstanceID: 101, RuntimeType: "opencode", Generation: 4}, Assignment: &k8s.RuntimeFaultAssignment{RuntimeAssignment: k8s.RuntimeAssignment{Request: raw}}}, pod
}

func TestHistoricalFaultSlotBackfillPreservesSuccessorAndRotatesItems(t *testing.T) {
	s, kube, _ := recoveryFlowFixture(t, "running")
	ctx := context.Background()
	j, pod := faultSlotTestJournal()
	j.Scope.OperationID = "historical"
	j.Receipt.OperationID = "historical"
	j.Receipt.ScopeDigest = runtimeeviction.ScopeDigest(j.Scope)
	if _, err := kube.CoreV1().Pods("runtime").Update(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	phase := 0
	handler := func(r *http.Request) (*http.Response, error) {
		var body any
		if r.URL.Path == "/v1/slots/slot-0/state" {
			body = map[string]any{"pod_uid": "old", "worker_epoch": strings.Repeat("b", 32), "digest": strings.Repeat("d", 64), "idle_confirmed": phase == 2, "quarantined": phase != 2, "gateways": []any{}, "assignment": map[string]any{"pod_uid": "old", "worker_epoch": strings.Repeat("a", 32), "instance_id": 101, "user_id": 1, "generation": 4, "gateway_id": "gw-101-4", "state": map[bool]string{true: "stopped", false: "running"}[phase > 0]}}
		} else {
			released := strings.HasSuffix(r.URL.Path, "release-fault-stop")
			phase = 1
			if released {
				phase = 2
			}
			body = map[string]any{"operation_id": j.Scope.OperationID, "scope_digest": j.Receipt.ScopeDigest, "gateway_id": j.Scope.GatewayID, "worker_epoch": strings.Repeat("b", 32), "stopped": true, "confirmed": true, "released": released}
		}
		raw, _ := json.Marshal(body)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(raw)), Header: make(http.Header)}, nil
	}
	c := s.scheduler.agentClient.(*slotRuntimeAgentClient)
	c.httpClient = &http.Client{Transport: slotRoundTripper(handler)}
	// The old generation is already unbound; the user is running a newer one.
	if _, err := s.sess.SQL().Exec("INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,state,capacity) VALUES(99,'opencode','runtime','successor','successor','pool','image','ready',8)"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.sess.SQL().Exec("UPDATE instances SET runtime_generation=7,status='running' WHERE id=101"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.sess.SQL().Exec("UPDATE instance_runtime_bindings SET runtime_pod_id=99,generation=7,gateway_id='gw-101-7' WHERE id=71"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.sess.SQL().Exec("UPDATE runtime_pods SET scheduling_paused=1 WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	// A permanently invalid earlier item must not starve the recoverable slot.
	bad := *j
	bad.Scope.InstanceID = 999
	bad.Receipt = nil
	raw, _ := json.Marshal(j.Receipt)
	json.Unmarshal(raw, &bad.Receipt)
	bad.Receipt.ScopeDigest = "invalid"
	items := []RuntimeSchedulingItem{{InstanceID: 999, SourcePodID: 1, Recovery: &bad}, {InstanceID: 101, SourcePodID: 1, Recovery: j}}
	raw, _ = json.Marshal(items)
	if _, err := s.sess.SQL().Exec("INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES('historical','eviction','completed','{\"mode\":\"recovery\"}',?,NOW(),NOW())", string(raw)); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 3; attempt++ {
		if err := s.reconcileHistoricalFaultSlots(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var saved string
	row, _ := s.sess.SQL().QueryRow("SELECT items_json FROM runtime_management_operations WHERE id='historical'")
	if err := row.Scan(&saved); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(saved), &items); err != nil {
		t.Fatal(err)
	}
	if !faultSlotReleaseComplete(items[1].Recovery) || items[0].Recovery.SlotRelease.Released {
		t.Fatal("backfill failed to rotate held item or finalized invalid proof")
	}
	var status, gateway string
	var generation, podID int
	row, _ = s.sess.SQL().QueryRow("SELECT i.status,i.runtime_generation,b.runtime_pod_id,b.gateway_id FROM instances i JOIN instance_runtime_bindings b ON b.instance_id=i.id WHERE i.id=101")
	if err := row.Scan(&status, &generation, &podID, &gateway); err != nil {
		t.Fatal(err)
	}
	if status != "running" || generation != 7 || podID != 99 || gateway != "gw-101-7" {
		t.Fatal("backfill changed the current lifecycle")
	}
}
