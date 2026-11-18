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
	"clawreef/internal/runtimeeviction"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

// The physical stop is simulated here; real CRI/NFS/website evidence is recorded
// separately by deployments/validation/runtime-slot-native-acceptance.py.
type slotResetFlowDeployment struct {
	fakeRuntimeDeploymentService
	kube  *fake.Clientset
	stops int
}

func (d *slotResetFlowDeployment) ResetPhysicalSlot(ctx context.Context, scope runtimeeviction.SlotScope, _ string) (*runtimeeviction.SlotReceipt, error) {
	d.stops++
	pod, err := d.kube.CoreV1().Pods(scope.Namespace).Get(ctx, scope.PodName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	pod.Status.ContainerStatuses[0].ContainerID = "containerd://" + strings.Repeat("c", 64)
	_, err = d.kube.CoreV1().Pods(scope.Namespace).UpdateStatus(ctx, pod, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}
	return &runtimeeviction.SlotReceipt{ScopeDigest: runtimeeviction.SlotScopeDigest(scope), OperationID: scope.OperationID, PodUID: scope.PodUID, ContainerName: scope.ContainerName, ContainerID: scope.ContainerID, NodeUID: scope.NodeUID, NodeBootID: scope.NodeBootID, FinishedAt: time.Now().UTC(), Confirmed: true}, nil
}

func TestSlotResetDurableControllerContinuationMySQL(t *testing.T) {
	for _, scenario := range []string{"unbound", "same-generation-successor", "same-generation-successor-unknown-owner", "never-published-first-reservation", "never-published-credential-exists", "never-published-foreign-publication"} {
		t.Run(scenario, func(t *testing.T) {
			sess := runtimeManagementMySQLDB(t)
			s := runtimeManagementMySQLService(sess)
			ctx := context.Background()
			exec := func(q string, args ...any) {
				t.Helper()
				if _, e := sess.SQL().Exec(q, args...); e != nil {
					t.Fatal(e)
				}
			}
			scope := runtimeeviction.SlotScope{Version: 1, OperationID: "reset-flow", Namespace: "runtime", PodName: "source", PodUID: "source-uid", NodeName: "node", NodeUID: "node-uid", NodeBootID: "boot", ContainerName: "slot-0", ContainerID: "containerd://" + strings.Repeat("a", 64)}
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: scope.PodName, Namespace: scope.Namespace, UID: types.UID(scope.PodUID)}, Spec: corev1.PodSpec{NodeName: scope.NodeName}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: scope.ContainerName, ContainerID: scope.ContainerID, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
			node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: scope.NodeName, UID: types.UID(scope.NodeUID)}, Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{BootID: scope.NodeBootID}}}
			kube := fake.NewSimpleClientset(pod, node)
			deployment := &slotResetFlowDeployment{kube: kube}
			endpoint := "https://10.0.0.2:19090"
			newEpoch := strings.Repeat("b", 32)
			released := false
			acks := 0
			j := &RuntimeSlotResetJournal{Phase: "planned", Slot: scope.ContainerName, PodUID: scope.PodUID, ContainerID: scope.ContainerID, GatewayPort: 20000, Scope: scope, Source: models.RuntimePod{ID: 1, RuntimeType: "opencode", AgentEndpoint: &endpoint}, SourcePod: pod.DeepCopy(), HelperImage: "helper@sha256:" + strings.Repeat("d", 64)}
			if strings.HasPrefix(scenario, "same-generation-successor") {
				exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,created_at,updated_at) VALUES(101,1,'opencode','gateway','lite','running',4,NOW(),NOW())")
				exec("INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,state,capacity,used_slots) VALUES(1,'opencode','runtime','source','source-uid','source','image','ready',8,1)")
				exec("INSERT INTO instance_runtime_bindings(id,instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES(72,101,1,'opencode','gw-101-4',20003,'/workspaces/opencode/user-1/instance-101','running',4)")
				j.InstanceID = 101
				j.Owner = &runtimeSlotAuditOwner{PodUID: scope.PodUID, WorkerEpoch: strings.Repeat("e", 32), GatewayID: "gw-101-4", InstanceID: 101, UserID: 1, Generation: 4, State: "running"}
				j.AssignmentDigest = strings.Repeat("f", 64)
				if scenario == "same-generation-successor-unknown-owner" {
					j.Owner = nil
					j.AssignmentDigest = ""
					j.SourceBinding = &models.InstanceRuntimeBinding{ID: 71, InstanceID: 101, RuntimePodID: 1, RuntimeType: "opencode", GatewayID: "gw-101-4", GatewayPort: 20000, WorkspacePath: "/workspaces/opencode/user-1/instance-101", Generation: 4}
				}
			}
			if strings.HasPrefix(scenario, "never-published-") {
				exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,created_at,updated_at) VALUES(101,1,'opencode','gateway','lite','creating',1,NOW(),NOW())")
				exec("UPDATE instances SET workspace_path='/workspaces/opencode/user-1/instance-101' WHERE id=101")
				exec("INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,state,capacity,used_slots) VALUES(1,'opencode','runtime','source','source-uid','source','image','ready',8,1)")
				exec("INSERT INTO instance_runtime_bindings(id,instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES(72,101,1,'opencode','pending-101-1',20000,'/workspaces/opencode/user-1/instance-101','creating',1)")
				j.InstanceID = 101
				j.SourceBinding = &models.InstanceRuntimeBinding{ID: 72, InstanceID: 101, RuntimePodID: 1, RuntimeType: "opencode", GatewayID: "pending-101-1", GatewayPort: 20000, WorkspacePath: "/workspaces/opencode/user-1/instance-101", Generation: 1}
			}
			if scenario == "never-published-credential-exists" {
				_, err := kube.CoreV1().Secrets("runtime").Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "lite-instance-101-g1", Namespace: "runtime"}}, metav1.CreateOptions{})
				if err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "never-published-foreign-publication" {
				_, err := kube.CoreV1().ConfigMaps("runtime").Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "lite-instance-101", Namespace: "runtime"}, Data: map[string]string{"generation": "1"}}, metav1.CreateOptions{})
				if err != nil {
					t.Fatal(err)
				}
			}
			client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				var body any
				if r.Method == http.MethodGet {
					state := map[string]any{"pod_uid": scope.PodUID, "worker_epoch": newEpoch, "idle_confirmed": true, "quarantined": false, "gateways": []any{}}
					if j.Owner != nil && !released {
						state["assignment"] = j.Owner
						state["digest"] = j.AssignmentDigest
						state["idle_confirmed"] = false
					}
					body = state
				} else {
					var proof struct {
						Scope   runtimeeviction.SlotScope    `json:"scope"`
						Receipt *runtimeeviction.SlotReceipt `json:"receipt"`
					}
					if e := json.NewDecoder(r.Body).Decode(&proof); e != nil {
						t.Fatal(e)
					}
					if proof.Scope != scope || runtimeeviction.ValidateSlotReceipt(scope, proof.Receipt) != nil {
						t.Fatal("native release lost exact physical proof")
					}
					released = strings.HasSuffix(r.URL.Path, "/release-reset")
					acks++
					body = map[string]any{"operation_id": scope.OperationID, "scope_digest": runtimeeviction.SlotScopeDigest(scope), "container_id": scope.ContainerID, "worker_epoch": newEpoch, "stopped": true, "confirmed": true, "released": released}
				}
				b, _ := json.Marshal(body)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
			})})
			s.scheduler.agentClient = client
			s.scheduler.deployments = deployment
			op := &RuntimeManagementOperation{ID: scope.OperationID, Kind: "slot_reset", Status: "processing", CreatedAt: time.Now().UTC(), Items: []RuntimeSchedulingItem{{InstanceID: j.InstanceID, SourcePodID: 1, SlotReset: j}}}
			raw, _ := json.Marshal(op.Items)
			exec("INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,owner_token,created_at,updated_at) VALUES(?,'slot_reset','processing','{}',?,?,NOW(),NOW())", op.ID, string(raw), s.token)
			exec("INSERT INTO runtime_slot_reset_requests(request_id,operation_id,pod_uid,slot,source_pod_id,gateway_port,instance_id,active,created_at) VALUES('request-flow',?,'source-uid','slot-0',1,20000,?,1,NOW())", op.ID, j.InstanceID)
			flowErr := s.processSlotReset(ctx, op)
			if scenario == "never-published-credential-exists" || scenario == "never-published-foreign-publication" {
				if flowErr == nil || op.Status != "needs_attention" {
					t.Fatalf("missing publication authority released: %v %s", flowErr, op.Status)
				}
				var count int
				row, _ := sess.SQL().QueryRow("SELECT COUNT(*) FROM instance_runtime_bindings WHERE id=72")
				if err := row.Scan(&count); err != nil || count != 1 {
					t.Fatalf("uncertain reservation removed: %d %v", count, err)
				}
				if deployment.stops != 1 || acks != 0 {
					t.Fatal("uncertain business identity reached native release")
				}
				return
			}
			if err := flowErr; err == nil || op.Status != "pending" {
				t.Fatalf("worker replacement should wait durably: status=%s err=%v", op.Status, err)
			}
			// Reload the durable journal as a restarted controller would.
			var stored string
			row, _ := sess.SQL().QueryRow("SELECT items_json FROM runtime_management_operations WHERE id=?", op.ID)
			if err := row.Scan(&stored); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(stored), &op.Items); err != nil {
				t.Fatal(err)
			}
			op.Status = "processing"
			exec("UPDATE runtime_management_operations SET status='processing',owner_token=? WHERE id=?", s.token, op.ID)
			if err := s.processSlotReset(ctx, op); err != nil {
				t.Fatal(err)
			}
			var active int
			row, _ = sess.SQL().QueryRow("SELECT COUNT(*) FROM runtime_slot_reset_requests WHERE active=1")
			if err := row.Scan(&active); err != nil {
				t.Fatal(err)
			}
			if op.Status != "completed" || active != 0 || deployment.stops != 1 || acks != 2 {
				t.Fatalf("flow incomplete: status=%s active=%d stops=%d acks=%d", op.Status, active, deployment.stops, acks)
			}
			if strings.HasPrefix(scenario, "same-generation-successor") {
				var port int
				row, _ = sess.SQL().QueryRow("SELECT gateway_port FROM instance_runtime_bindings WHERE id=72")
				if err := row.Scan(&port); err != nil || port != 20003 {
					t.Fatalf("successor modified: %d %v", port, err)
				}
			}
			if scenario == "never-published-first-reservation" {
				if op.Items[0].SlotReset.Owner != nil || op.Items[0].SlotReset.UnpublishedReservation == nil {
					t.Fatal("SQL reservation was confused with a native worker")
				}
				instance, err := s.scheduler.instanceRepo.GetByID(101)
				if err != nil {
					t.Fatal(err)
				}
				handled, _ := s.scheduler.resumeSlotResetRecoveryLocked(ctx, *instance)
				if !handled {
					t.Fatal("unpublished reset was not restored automatically")
				}
				var gen int
				row, _ := sess.SQL().QueryRow("SELECT runtime_generation FROM instances WHERE id=101")
				if err = row.Scan(&gen); err != nil || gen != 2 {
					t.Fatalf("unpublished recovery generation=%d err=%v", gen, err)
				}
			}
		})
	}
}
