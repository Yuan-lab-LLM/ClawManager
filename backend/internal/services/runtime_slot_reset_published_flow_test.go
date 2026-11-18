package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"clawreef/internal/runtimeauth"
	"clawreef/internal/runtimeeviction"
	k8sservice "clawreef/internal/services/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

type publishedResetDeployment struct {
	*slotResetFlowDeployment
	recover func(context.Context, runtimeeviction.Scope, *corev1.Pod) (*runtimeeviction.Receipt, error)
}

func (d *publishedResetDeployment) RecoverRuntimeEviction(ctx context.Context, scope runtimeeviction.Scope, pod *corev1.Pod) (*runtimeeviction.Receipt, error) {
	return d.recover(ctx, scope, pod)
}

func TestSlotResetPublishedFlowMySQL(t *testing.T) {
	for _, binding := range []string{"canonical", "pending-alias", "no-binding"} {
		t.Run(binding, func(t *testing.T) {
			ctx := t.Context()
			sess, management, placementAgent := runtimeManagementMySQLMoveFixture(t)
			s := management.scheduler
			sqlExec := func(query string, args ...any) {
				t.Helper()
				if _, err := sess.SQL().Exec(query, args...); err != nil {
					t.Fatal(err)
				}
			}
			const namespace = "runtime"
			const endpoint = "https://10.30.0.1:19090"
			const marker = "private-environment-must-never-be-journaled"
			image := "fixture/runtime@sha256:" + strings.Repeat("a", 64)
			helper := "fixture/helper@sha256:" + strings.Repeat("d", 64)
			t.Setenv("CLAWMANAGER_RUNTIME_RECOVERY_IMAGE", helper)
			sqlExec("UPDATE runtime_pods SET capabilities_json=?,image_ref=? WHERE id=1", `["container-slots-v1","manual-slot-reset-v1"]`, image)
			if binding == "pending-alias" {
				sqlExec("UPDATE instance_runtime_bindings SET gateway_id='pending-101-1',state='creating' WHERE instance_id=101")
			}
			if binding == "no-binding" {
				sqlExec("DELETE FROM instance_runtime_bindings WHERE instance_id=101")
				sqlExec("UPDATE runtime_pods SET used_slots=0 WHERE id=1")
			}
			dep := k8sservice.BuildRuntimeDeployment(k8sservice.RuntimeDeploymentSpec{Name: "runtime", Namespace: namespace, RuntimeType: "opencode", Image: image, SlotCount: 2, AgentTokenSecretName: "agent", WorkspacePVCClaimName: "workspaces", WorkspaceMountPath: s.workspaceRoot, SandboxProfile: k8sservice.RuntimeNFSRootlessProfile, NetworkIsolation: true})
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: namespace, UID: "source-uid", Labels: dep.Spec.Template.Labels}, Spec: *dep.Spec.Template.Spec.DeepCopy()}
			pod.Spec.NodeName = "fixture-node"
			for i := range pod.Spec.Containers {
				pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, corev1.EnvVar{Name: "PRIVATE_FIXTURE_TOKEN", Value: marker})
			}
			cid := "containerd://" + strings.Repeat("a", 64)
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "slot-0", ContainerID: cid, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}
			node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: pod.Spec.NodeName, UID: "node-uid"}, Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{BootID: "boot"}}}
			claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "workspaces", Namespace: namespace, UID: "claim-uid", ResourceVersion: "1", CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour)), Finalizers: []string{"fixture.test/keep"}}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "fixture-pv"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
			pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: claim.Spec.VolumeName, UID: "pv-uid"}, Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Namespace: namespace, Name: claim.Name, UID: claim.UID}, PersistentVolumeSource: corev1.PersistentVolumeSource{NFS: &corev1.NFSVolumeSource{Server: "10.0.0.1", Path: "/fixture"}}}}
			kube := fake.NewSimpleClientset(pod, node, claim, pv)
			storage, err := k8sservice.RuntimeFaultStorageIdentity(ctx, kube, pod, s.workspaceRoot, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			instance, err := s.instanceRepo.GetByID(101)
			if err != nil {
				t.Fatal(err)
			}
			request, err := json.Marshal(map[string]any{"gateway_port": 20000, "workspace_path": stringValue(instance.WorkspacePath)})
			if err != nil {
				t.Fatal(err)
			}
			assignments := k8sservice.NewRuntimeAssignmentService(kube, namespace)
			assignment := k8sservice.RuntimeAssignment{InstanceID: 101, UserID: 1, Generation: 1, RuntimeType: "opencode", Endpoint: endpoint, GatewayID: "gw-101-1", PodUID: "source-uid", Request: request, Environment: map[string]string{"PRIVATE": marker}}
			if err = assignments.EnsureDesired(ctx, assignment); err != nil {
				t.Fatal(err)
			}
			configMaps := kube.CoreV1().ConfigMaps(namespace)
			cm, err := configMaps.Get(ctx, "lite-instance-101", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			cm.UID, cm.ResourceVersion = "publication-uid", "1"
			if _, err = configMaps.Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			secrets := kube.CoreV1().Secrets(namespace)
			secret, err := secrets.Get(ctx, cm.Data["secret_ref"], metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			secret.UID, secret.ResourceVersion = "secret-uid", "1"
			if _, err = secrets.Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}

			d := &publishedResetDeployment{slotResetFlowDeployment: &slotResetFlowDeployment{kube: kube, fakeRuntimeDeploymentService: *s.deployments.(*fakeRuntimeDeploymentService)}}
			s.deployments = d
			var operationID string
			var fence, external, writer, ack int
			var released bool
			oldEpoch, newEpoch, digest := strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("f", 64)
			owner := runtimeSlotAuditOwner{PodUID: "source-uid", WorkerEpoch: oldEpoch, InstanceID: 101, UserID: 1, Generation: 1, GatewayID: "gw-101-1", State: "running"}
			client := NewSlotRuntimeAgentClientWithHTTPClient("fixture", kube, namespace, &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				var body any
				switch r.URL.Path {
				case "/v1/slots/slot-0/state":
					epoch := oldEpoch
					if d.stops > 0 {
						epoch = newEpoch
					}
					state := map[string]any{"pod_uid": owner.PodUID, "worker_epoch": epoch, "quarantined": false, "idle_confirmed": released, "gateways": []any{}}
					if !released {
						state["assignment"] = owner
						state["digest"] = digest
					}
					body = state
				case "/v1/slots/slot-0/reconcile-reset", "/v1/slots/slot-0/release-reset":
					var proof struct {
						Scope   runtimeeviction.SlotScope    `json:"scope"`
						Receipt *runtimeeviction.SlotReceipt `json:"receipt"`
						Old     string                       `json:"worker_epoch"`
						New     string                       `json:"current_worker_epoch"`
						Digest  string                       `json:"assignment_digest"`
					}
					if e := json.NewDecoder(r.Body).Decode(&proof); e != nil {
						return nil, e
					}
					if proof.Scope.OperationID != operationID || proof.Scope.ContainerID != cid || runtimeeviction.ValidateSlotReceipt(proof.Scope, proof.Receipt) != nil || proof.Old != oldEpoch || proof.New != newEpoch || proof.Digest != digest || writer != 1 {
						return nil, errors.New("native ack lacks source receipt or writer retirement")
					}
					released = strings.HasSuffix(r.URL.Path, "/release-reset")
					if released != (ack == 1) {
						return nil, errors.New("native acknowledgments out of order")
					}
					ack++
					body = map[string]any{"operation_id": operationID, "scope_digest": runtimeeviction.SlotScopeDigest(proof.Scope), "container_id": cid, "worker_epoch": newEpoch, "stopped": true, "confirmed": true, "released": released}
				default:
					return nil, fmt.Errorf("unexpected native request %s", r.URL.Path)
				}
				b, e := json.Marshal(body)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, e
			})})
			s.agentClient = client
			identity := runtimeauth.Identity{InstanceID: 101, UserID: 1, Generation: 1, PodUID: owner.PodUID}
			if err = ConfigureSlotWorkspaceWriteFence(client, func(_ context.Context, id runtimeauth.Identity) error {
				if id != identity {
					return errors.New("workspace identity changed")
				}
				fence++
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err = ConfigureSlotResetExternalWriterCleanup(client, func(_ context.Context, id runtimeauth.Identity, current bool) error {
				if id != identity || current != (binding != "no-binding") {
					return errors.New("external writer authority changed")
				}
				external++
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			d.recover = func(ctx context.Context, scope runtimeeviction.Scope, original *corev1.Pod) (*runtimeeviction.Receipt, error) {
				if scope.OperationID != operationID || scope.ContainerID != cid || scope.GatewayID != owner.GatewayID || scope.Generation != 1 || scope.InstanceID != 101 || scope.UserID != 1 || scope.ContainerName != "slot-0" || scope.WorkspaceRoot != s.workspaceRoot || original.Annotations[k8sservice.RuntimeFaultHelperImageAnnotation] != helper || fence != 1 || external != 1 {
					return nil, errors.New("writer scope or external fences changed")
				}
				pin := &k8sservice.RuntimeSlotResetClaim{Namespace: namespace, Name: claim.Name, UID: string(claim.UID)}
				if e := k8sservice.VerifyRuntimeSlotResetStorage(ctx, kube, original, s.workspaceRoot, storage, pin, operationID); e != nil {
					return nil, e
				}
				cm, e := configMaps.Get(ctx, "lite-instance-101", metav1.GetOptions{})
				if e != nil || cm.Data["desired_state"] != "draining" {
					return nil, errors.New("writer helper received unfenced publication")
				}
				writer++
				// Model a completed, garbage-collected helper Job with its durable scoped receipt.
				return &runtimeeviction.Receipt{Confirmed: true, ScopeDigest: runtimeeviction.ScopeDigest(scope), OperationID: operationID, PodUID: scope.PodUID, ContainerName: scope.ContainerName, NodeUID: scope.NodeUID, NodeBootID: scope.NodeBootID, StoppedContainerIDs: []string{scope.ContainerID}, LedgerDigest: strings.Repeat("e", 64), FinishedAt: time.Now().UTC()}, nil
			}
			check, err := management.PreflightSlotReset(ctx, 1, "slot-0", RuntimeSlotResetRequest{})
			if err != nil {
				t.Fatal(err)
			}
			if !check.Allowed || check.RecoveryAction != "reschedule" || check.InstanceID != 101 || check.Generation != 1 || check.journal.StorageIdentity != storage || check.journal.Assignment == nil {
				t.Fatalf("incomplete published preflight %+v", check)
			}
			req := RuntimeSlotResetRequest{RequestID: "fixture-published-slot-reset", SourcePodUID: check.PodUID, SourceContainerID: cid, ScopeFingerprint: check.ScopeFingerprint}
			op, err := management.ResetSlot(ctx, 1, "slot-0", req, nil)
			if err != nil {
				t.Fatal(err)
			}
			operationID = op.ID
			var raw string
			row, err := sess.SQL().QueryRow("SELECT items_json FROM runtime_management_operations WHERE id=?", operationID)
			if err != nil {
				t.Fatal(err)
			}
			if err = row.Scan(&raw); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(raw, marker) || !strings.Contains(raw, "RUNTIME_WORKSPACE_ROOT") {
				t.Fatal("durable source lost the ledger root or retained private environment")
			}
			if err = management.Process(ctx); !errors.Is(err, k8sservice.ErrRuntimeFaultEvictionPending) {
				t.Fatalf("first controller pass: %v", err)
			}
			if writer != 1 || ack != 0 || d.stops != 1 {
				t.Fatalf("before replacement: stops=%d writer=%d ack=%d", d.stops, writer, ack)
			}
			updated, err := kube.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, claim.Name, metav1.GetOptions{})
			if err != nil || len(updated.Finalizers) != 1 || updated.Finalizers[0] != "fixture.test/keep" {
				t.Fatalf("PVC pin cleanup %v err=%v", updated.Finalizers, err)
			}
			if err = management.Process(ctx); err != nil {
				t.Fatal(err)
			}
			op, err = management.ResetSlot(ctx, 1, "slot-0", req, nil)
			if err != nil || op == nil || op.ID != operationID || op.Status != "completed" || !released || ack != 2 || d.stops != 1 || writer != 1 {
				t.Fatalf("operation replay: %+v err=%v writer=%d stops=%d ack=%d", op, err, writer, d.stops, ack)
			}
			cm, err = configMaps.Get(ctx, cm.Name, metav1.GetOptions{})
			if err != nil || cm.Data["desired_state"] != "stopped" || cm.Annotations["clawmanager.io/fault-eviction-receipt-1"] == "" {
				t.Fatalf("old publication retirement failed: %v", err)
			}
			b, err := s.bindingRepo.GetByInstanceID(ctx, 101)
			if err != nil || b != nil {
				t.Fatalf("old binding retained %+v: %v", b, err)
			}
			// Continue through the ordinary placement scheduler. Its external Runtime create
			// boundary uses the same healthy target from the move fixture.
			s.agentClient = placementAgent
			instance, err = s.instanceRepo.GetByID(101)
			if err != nil {
				t.Fatal(err)
			}
			handled, errs := s.resumeSlotResetRecoveryLocked(ctx, *instance)
			if !handled || len(errs) > 0 {
				t.Fatalf("automatic placement: handled=%v errors=%v", handled, errs)
			}
			b, err = s.bindingRepo.GetByInstanceID(ctx, 101)
			if err != nil || b == nil || b.Generation != 2 || b.GatewayID != "gw-101-2" || b.State != "running" || b.RuntimePodID == 1 && b.GatewayPort == 20000 {
				t.Fatalf("recovered binding %+v err=%v", b, err)
			}
		})
	}
}
