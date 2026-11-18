package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/runtimeeviction"
	k8sservice "clawreef/internal/services/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSlotResetAdmissionFlowMySQL(t *testing.T) {
	for _, scenario := range []string{"source-scale-in", "source-deletion", "other-pod-scale-in", "same-slot-eviction", "neighbor-eviction", "reverse-same-slot-eviction", "reverse-neighbor-eviction", "unreserved-legacy", "unreserved-protected"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := t.Context()
			sess, management, _ := runtimeManagementMySQLMoveFixture(t)
			scheduler := management.scheduler
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := sess.SQL().Exec(query, args...); err != nil {
					t.Fatal(err)
				}
			}
			sourceImage := "fixture/runtime@sha256:" + strings.Repeat("a", 64)
			t.Setenv("CLAWMANAGER_RUNTIME_RECOVERY_IMAGE", "fixture/helper@sha256:"+strings.Repeat("d", 64))
			exec("UPDATE runtime_pods SET image_ref=?,capabilities_json=? WHERE id=1", sourceImage, `["manual-slot-reset-v1"]`)
			template := k8sservice.BuildRuntimeDeployment(k8sservice.RuntimeDeploymentSpec{Name: "runtime", Namespace: "runtime", RuntimeType: "opencode", Image: sourceImage, SlotCount: 2, WorkspaceMountPath: scheduler.workspaceRoot, SandboxProfile: k8sservice.RuntimeNFSRootlessProfile, NetworkIsolation: true})
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: "runtime", UID: "source-uid", Labels: template.Spec.Template.Labels}, Spec: *template.Spec.Template.Spec.DeepCopy()}
			pod.Spec.NodeName = "node"
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "slot-0", ContainerID: "containerd://" + strings.Repeat("a", 64), State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}
			node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node", UID: "node-uid"}, Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{BootID: "boot"}}}
			kube := fake.NewSimpleClientset(pod, node)
			deployment := &slotResetFlowDeployment{kube: kube, fakeRuntimeDeploymentService: *scheduler.deployments.(*fakeRuntimeDeploymentService)}
			scheduler.deployments = deployment
			epoch := strings.Repeat("b", 32)
			scheduler.agentClient = NewSlotRuntimeAgentClientWithHTTPClient("fixture", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				if deployment.stops == 0 {
					return nil, errors.New("old worker unavailable")
				}
				var response any
				switch r.URL.Path {
				case "/v1/slots/slot-0/state":
					response = map[string]any{"pod_uid": "source-uid", "worker_epoch": epoch, "quarantined": false, "idle_confirmed": true, "gateways": []any{}}
				case "/v1/slots/slot-0/reconcile-reset", "/v1/slots/slot-0/release-reset":
					var proof struct {
						Scope runtimeeviction.SlotScope `json:"scope"`
					}
					if err := json.NewDecoder(r.Body).Decode(&proof); err != nil {
						return nil, err
					}
					response = map[string]any{"operation_id": proof.Scope.OperationID, "scope_digest": runtimeeviction.SlotScopeDigest(proof.Scope), "container_id": proof.Scope.ContainerID, "worker_epoch": epoch, "stopped": true, "confirmed": true, "released": strings.HasSuffix(r.URL.Path, "/release-reset")}
				default:
					return nil, fmt.Errorf("unexpected request %s", r.URL.Path)
				}
				raw, err := json.Marshal(response)
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(raw)))}, err
			})})
			if !strings.HasPrefix(scenario, "unreserved") {
				exec("DELETE FROM instance_runtime_bindings WHERE instance_id=101")
				exec("UPDATE runtime_pods SET used_slots=0 WHERE id=1")
			}
			check, err := management.PreflightSlotReset(ctx, 1, "slot-0", RuntimeSlotResetRequest{})
			if err != nil {
				t.Fatal(err)
			}
			if check.journal.Owner != nil || (check.journal.SourceBinding != nil) != strings.HasPrefix(scenario, "unreserved") {
				t.Fatal("unexpected source evidence")
			}
			req := RuntimeSlotResetRequest{RequestID: "reset-admission-request", SourcePodUID: check.PodUID, SourceContainerID: check.ContainerID, ScopeFingerprint: check.ScopeFingerprint}

			enqueueEviction := func(slot string) error {
				scope := runtimeeviction.Scope{PodUID: check.PodUID, ContainerName: slot, RuntimeType: "opencode", ContainerID: check.ContainerID}
				item := RuntimeSchedulingItem{InstanceID: 101, SourcePodID: 1, SourcePodUID: check.PodUID, Recovery: &RuntimeFaultEvictionJournal{Scope: scope}}
				_, err := management.enqueue(ctx, "eviction", RuntimeSchedulingRequest{Mode: "recovery", InstanceIDs: []int{101}}, []RuntimeSchedulingItem{item}, nil)
				return err
			}
			switch scenario {
			case "same-slot-eviction", "neighbor-eviction":
				slot := "slot-0"
				if scenario == "neighbor-eviction" {
					slot = "slot-1"
				}
				if err := enqueueEviction(slot); err != nil {
					t.Fatal(err)
				}
			case "source-scale-in", "other-pod-scale-in":
				id := 1
				if scenario == "other-pod-scale-in" {
					id = 2
				}
				exec("INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES('scale-op','scale_in','pending',?,'[]',NOW(),NOW())", fmt.Sprintf(`{"plan":{"source_pods":[{"id":%d}]}}`, id))
			case "source-deletion":
				exec("INSERT INTO runtime_pod_deletion_intents(namespace,deployment_name,deployment_uid) VALUES('runtime','runtime','deployment')")
			}
			op, err := management.ResetSlot(ctx, 1, "slot-0", req, nil)
			if scenario == "source-scale-in" || scenario == "source-deletion" || scenario == "same-slot-eviction" {
				if err == nil || op != nil {
					t.Fatal("reset was admitted after source retirement had been fenced")
				}
				row, err := sess.SQL().QueryRow("SELECT COUNT(*) FROM runtime_slot_reset_requests")
				if err != nil {
					t.Fatal(err)
				}
				var count int
				if err = row.Scan(&count); err != nil || count != 0 {
					t.Fatalf("rejected reset left a request: %d %v", count, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "other-pod-scale-in" || scenario == "neighbor-eviction" {
				return
			}
			if strings.HasPrefix(scenario, "reverse-") {
				slot := "slot-0"
				if scenario == "reverse-neighbor-eviction" {
					slot = "slot-1"
				}
				err := enqueueEviction(slot)
				if scenario == "reverse-same-slot-eviction" && !errors.Is(err, repository.ErrRuntimeSlotResetFenced) || scenario == "reverse-neighbor-eviction" && err != nil {
					t.Fatalf("eviction admission after reset: %v", err)
				}
				return
			}

			// A has moved on the same generation. Neither its prior SQL reservation
			// nor the empty replacement book authenticates the retired worker's owner.
			exec("UPDATE instance_runtime_bindings SET gateway_port=20003 WHERE instance_id=101")
			if err = management.Process(ctx); !errors.Is(err, k8sservice.ErrRuntimeFaultEvictionPending) {
				t.Fatalf("first pass: %v", err)
			}
			if err = management.Process(ctx); err != nil {
				t.Fatal(err)
			}
			completed, err := management.SlotResetOperation(ctx, req.RequestID)
			if err != nil || completed == nil || completed.Status != "completed" || completed.Items[0].SlotReset.RecoveryAction != "preserve_current" {
				t.Fatalf("reset did not preserve successor: %+v %v", completed, err)
			}
			port := 20000
			workspace := RuntimeWorkspacePathWithRoot(scheduler.workspaceRoot, "opencode", 1, 102)
			exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,runtime_security_profile,workspace_path,updated_at) VALUES(102,1,'opencode','gateway','lite','creating',1,'isolated-network-v1',?,NOW())", workspace)
			report := models.InstanceRuntimeBinding{InstanceID: 102, RuntimePodID: 1, RuntimeType: "opencode", GatewayID: "gw-102-1", GatewayPort: port, WorkspacePath: workspace, Generation: 1, State: "running"}
			var accepted bool
			if scenario == "unreserved-legacy" {
				accepted, err = scheduler.bindingRepo.(repository.ReportedGatewayBindingReconciler).ReconcileReportedGateway(ctx, &report, 1, 3)
			} else {
				accepted, err = scheduler.bindingRepo.(repository.UnboundProtectedReportedGatewayReconciler).ReconcileUnboundProtectedReportedGateway(ctx, &report, 1, "source-uid", 3)
			}
			if err != nil || accepted {
				t.Fatalf("late unreserved report: accepted=%v err=%v", accepted, err)
			}
			reservation := report
			reservation.GatewayID = "pending-102-1"
			reservation.State = "creating"
			if err = scheduler.bindingRepo.Create(ctx, &reservation); err != nil {
				t.Fatalf("new reservation blocked: %v", err)
			}
			accepted, err = scheduler.bindingRepo.(repository.PendingReportedGatewayBindingReconciler).ReconcilePendingReportedGateway(ctx, &report, 1, 3)
			if err != nil || !accepted {
				t.Fatalf("new reservation report: accepted=%v err=%v", accepted, err)
			}
		})
	}
}
