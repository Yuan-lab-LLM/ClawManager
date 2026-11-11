package services

import (
	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	"clawreef/internal/runtimeeviction"
	"clawreef/internal/services/k8s"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"net/http"
	"strings"
	"testing"
	"time"
)

type recoveryFlowStopper struct {
	*fakeRuntimeDeploymentService
	scopes  []runtimeeviction.Scope
	failure error
}

func (f *recoveryFlowStopper) RecoverRuntimeEviction(_ context.Context, s runtimeeviction.Scope, _ *corev1.Pod) (*runtimeeviction.Receipt, error) {
	f.scopes = append(f.scopes, s)
	if f.failure != nil {
		return nil, f.failure
	}
	return &runtimeeviction.Receipt{Confirmed: true, ScopeDigest: runtimeeviction.ScopeDigest(s), OperationID: s.OperationID, PodUID: s.PodUID, ContainerName: s.ContainerName, NodeUID: s.NodeUID, NodeBootID: s.NodeBootID, StoppedContainerIDs: []string{s.ContainerID}, LedgerDigest: strings.Repeat("a", 64), FinishedAt: time.Now().UTC()}, nil
}
func recoveryFlowFixture(t *testing.T, status string) (*RuntimeManagementService, *fake.Clientset, *recoveryFlowStopper) {
	t.Helper()
	t.Setenv("CLAWMANAGER_RUNTIME_RECOVERY_IMAGE", "app@sha256:"+strings.Repeat("a", 64))
	ctx := context.Background()
	s := runtimeManagementMySQLService(runtimeManagementMySQLDB(t))
	exec := func(q string, args ...interface{}) {
		t.Helper()
		if _, e := s.sess.SQL().Exec(q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec("ALTER TABLE team_members ADD COLUMN instance_id INT, ADD COLUMN status VARCHAR(32)")
	exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES(101,1,'opencode','gateway','lite',?,4,NOW()),(102,2,'opencode','gateway','lite','running',4,NOW())", status)
	exec("INSERT INTO instance_desired_state(instance_id,desired_power_state) VALUES(101,?)", map[bool]string{true: "stopped", false: "running"}[status == "stopped"])
	exec("INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,node_name,agent_endpoint,deployment_name,image_ref,state,capacity,used_slots,created_at) VALUES(1,'opencode','runtime','pool-old','old','node','https://10.1.0.2:19090','pool','image','error',2,2,NOW())")
	exec("INSERT INTO instance_runtime_bindings(id,instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES(71,101,1,'opencode','gw-101-4',20000,'/workspaces/opencode/user-1/instance-101','running',4),(72,102,1,'opencode','gw-102-4',20003,'/workspaces/opencode/user-2/instance-102','running',4)")
	before := metav1.NewTime(time.Now().Add(-time.Hour))
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pool-old", Namespace: "runtime", UID: "old"}, Spec: corev1.PodSpec{NodeName: "node", Volumes: []corev1.Volume{{Name: "workspaces", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}}}, Containers: []corev1.Container{{Name: "slot-0", Image: "image", Env: []corev1.EnvVar{{Name: "RUNTIME_GATEWAY_PORT_START", Value: "20000"}, {Name: "RUNTIME_WORKSPACE_ROOT", Value: "/workspaces"}}, VolumeMounts: []corev1.VolumeMount{{Name: "workspaces", MountPath: "/workspaces"}}}}}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "slot-0", ContainerID: "containerd://old-slot-0"}}}}
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "runtime", UID: "claim", CreationTimestamp: before}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "pv"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
	pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv", UID: "pv-uid", CreationTimestamp: before}, Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Namespace: "runtime", Name: "data", UID: "claim"}, PersistentVolumeSource: corev1.PersistentVolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/data"}}}}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node", UID: "node-uid", CreationTimestamp: before}, Status: corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{BootID: "boot"}, Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}}
	kube := fake.NewSimpleClientset(pod, claim, pv, node)
	a := k8s.NewRuntimeAssignmentService(kube, "runtime")
	for _, id := range []int{101, 102} {
		user, port := 1, 20000
		if id == 102 {
			user, port = 2, 20003
		}
		raw, _ := json.Marshal(RuntimeAgentCreateGatewayRequest{InstanceID: id, UserID: user, Generation: 4, PodUID: "old", AgentType: "opencode", GatewayPort: port, WorkspacePath: fmt.Sprintf("/workspaces/opencode/user-%d/instance-%d", user, id)})
		if e := a.EnsureDesired(ctx, k8s.RuntimeAssignment{InstanceID: id, UserID: user, Generation: 4, PodUID: "old", RuntimeType: "opencode", Endpoint: "https://10.1.0.2:19090", GatewayID: fmt.Sprintf("gw-%d-4", id), Request: raw}); e != nil {
			t.Fatal(e)
		}
		cm, e := kube.CoreV1().ConfigMaps("runtime").Get(ctx, fmt.Sprintf("lite-instance-%d", id), metav1.GetOptions{})
		if e != nil {
			t.Fatal(e)
		}
		cm.UID = types.UID(fmt.Sprintf("cm-%d", id))
		cm.ResourceVersion = "17"
		if _, e = kube.CoreV1().ConfigMaps("runtime").Update(ctx, cm, metav1.UpdateOptions{}); e != nil {
			t.Fatal(e)
		}
		sec, e := kube.CoreV1().Secrets("runtime").Get(ctx, cm.Data["secret_ref"], metav1.GetOptions{})
		if e != nil {
			t.Fatal(e)
		}
		sec.UID = types.UID(fmt.Sprintf("sec-%d", id))
		sec.ResourceVersion = "18"
		if _, e = kube.CoreV1().Secrets("runtime").Update(ctx, sec, metav1.UpdateOptions{}); e != nil {
			t.Fatal(e)
		}
	}
	agent := NewSlotRuntimeAgentClientWithHTTPClient("control", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(*http.Request) (*http.Response, error) { return nil, errors.New("old Agent unavailable") })}).(*slotRuntimeAgentClient)
	agent.workspaceWriteFence = func(_ context.Context, id runtimeauth.Identity) error {
		if id.InstanceID != 101 {
			t.Fatal("neighbor fenced")
		}
		return nil
	}
	agent.workspaceExternalStop = agent.workspaceWriteFence
	stopper := &recoveryFlowStopper{fakeRuntimeDeploymentService: &fakeRuntimeDeploymentService{}}
	s.scheduler.agentClient = agent
	s.scheduler.deployments = stopper
	s.scheduler.workspaceRoot = "/workspaces"
	return s, kube, stopper
}

func TestRecoveryLegacySnapshotRehydratesExactSourceProfile(t *testing.T) {
	uid, node := "old", "node"
	source := models.RuntimePod{Namespace: "ns", PodName: "pool-old", PodUID: &uid, NodeName: &node}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "pool-old", UID: "old", Labels: map[string]string{"clawmanager.io/sandbox-profile": "nfs-rootless-v1"}}, Spec: corev1.PodSpec{NodeName: "node"}}
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprint(changed), func(t *testing.T) {
			current := pod.DeepCopy()
			if changed {
				current.UID = "replacement"
			}
			client := fake.NewSimpleClientset(current)
			snapshot := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "pool-old", UID: "old", Annotations: map[string]string{"clawmanager.io/fault-eviction-helper-image": "pinned"}}}
			err := restoreFaultRecoveryProfile(context.Background(), client, source, snapshot)
			if changed {
				if err == nil {
					t.Fatal("replacement Pod profile accepted without original ReplicaSet")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.Annotations["clawmanager.io/fault-eviction-sandbox-profile"] != "nfs-rootless-v1" || snapshot.Annotations["clawmanager.io/fault-eviction-helper-image"] != "pinned" {
				t.Fatal("verified profile or pinned image lost")
			}
			if pod.Annotations != nil {
				t.Fatal("live source was mutated")
			}
		})
	}
}

func TestRecoveryProfileRejectsForeignRetainedController(t *testing.T) {
	uid, node := "old", "node"
	source := models.RuntimePod{Namespace: "ns", PodName: "pool-rs-old", PodUID: &uid, NodeName: &node, DeploymentName: "pool", CreatedAt: time.Now()}
	controller := true
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "pool-rs", CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour)), OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "pool", UID: "original-controller", Controller: &controller}}}}
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "pool", UID: "replacement-controller"}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"clawmanager.io/sandbox-profile": "nfs-rootless-v1"}}}}}
	client := fake.NewSimpleClientset(rs, dep)
	snapshot := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: source.PodName, UID: "old", Labels: map[string]string{"clawmanager.io/sandbox-profile": "nfs-rootless-v1"}}}
	if err := restoreFaultRecoveryProfile(context.Background(), client, source, snapshot); err == nil || !strings.Contains(err.Error(), "controller UID changed") {
		t.Fatalf("foreign source controller was accepted: %v", err)
	}
	if _, verified := snapshot.Annotations[k8s.RuntimeFaultSandboxProfileAnnotation]; verified {
		t.Fatal("failed verification persisted a profile")
	}
}
func TestRuntimeRecoveryProcessPendingHelperPreservesNeighbor(t *testing.T) {
	for _, status := range []string{"stopped", "deleting"} {
		t.Run(status, func(t *testing.T) {
			s, kube, helper := recoveryFlowFixture(t, status)
			ctx := context.Background()
			op, e := s.Evict(ctx, 1, RuntimeSchedulingRequest{Mode: "recovery"}, nil)
			if e != nil {
				t.Fatal(e)
			}
			if len(op.Items) != 1 || op.Items[0].InstanceID != 101 {
				t.Fatalf("default touched healthy neighbor: %+v", op.Items)
			}
			helper.failure = k8s.ErrRuntimeFaultEvictionPending
			if e = s.Process(ctx); e != nil {
				t.Fatal(e)
			}
			pending, _, _, e := s.runtimeManagementOperation(ctx, op.ID)
			if e != nil {
				t.Fatal(e)
			}
			if pending.Status != "pending" || pending.Items[0].Recovery.Phase != "stopping" {
				t.Fatalf("pending checkpoint: %+v", pending)
			}
			b, e := s.scheduler.bindingRepo.GetByInstanceID(ctx, 101)
			if e != nil || b == nil {
				t.Fatalf("pending helper released binding: %v", e)
			}
			helper.failure = errors.New("source isolation identity conflict")
			if e = s.Process(ctx); e != nil {
				t.Fatal(e)
			}
			held, _, _, e := s.runtimeManagementOperation(ctx, op.ID)
			if e != nil || held.Status != "needs_attention" || !strings.Contains(held.Items[0].ErrorMessage, "identity conflict") {
				t.Fatalf("structural conflict not held: %+v %v", held, e)
			}
			if _, e = s.RetryOperation(ctx, op.ID); e != nil {
				t.Fatal(e)
			}
			helper.failure = nil
			if e = s.Process(ctx); e != nil {
				t.Fatal(e)
			}
			done, _, _, e := s.runtimeManagementOperation(ctx, op.ID)
			if e != nil {
				t.Fatal(e)
			}
			if done.Items[0].Recovery.Phase != "released" {
				t.Fatalf("recovery not released: %+v", done)
			}
			if len(helper.scopes) != 3 || runtimeeviction.ScopeDigest(helper.scopes[0]) != runtimeeviction.ScopeDigest(helper.scopes[2]) || helper.scopes[0].OperationID != op.ID {
				t.Fatalf("helper scope changed: %+v", helper.scopes)
			}
			b, e = s.scheduler.bindingRepo.GetByInstanceID(ctx, 101)
			if e != nil || b != nil {
				t.Fatalf("binding retained: %+v %v", b, e)
			}
			neighbor, e := s.scheduler.bindingRepo.GetByInstanceID(ctx, 102)
			if e != nil || neighbor == nil || neighbor.GatewayID != "gw-102-4" {
				t.Fatalf("neighbor changed: %+v %v", neighbor, e)
			}
			inst, e := s.scheduler.instanceRepo.GetByID(101)
			if e != nil || inst.Status != status || inst.RuntimeGeneration != 4 {
				t.Fatalf("intent changed: %+v %v", inst, e)
			}
			cm, e := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-101", metav1.GetOptions{})
			if e != nil || cm.Data["desired_state"] != "stopped" {
				t.Fatalf("assignment not retired: %+v %v", cm, e)
			}
			cm, e = kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-102", metav1.GetOptions{})
			if e != nil || cm.Annotations["clawmanager.io/fault-eviction"] != "" || cm.Data["desired_state"] == "stopped" {
				t.Fatalf("neighbor assignment changed: %+v %v", cm, e)
			}
			source, e := s.scheduler.podRepo.GetByID(ctx, 1)
			if e != nil || !source.SchedulingPaused || source.UsedSlots != 1 {
				t.Fatalf("source admission/occupancy: %+v %v", source, e)
			}
		})
	}
}
func TestRuntimeRecoveryPreflightRejectsChangedSourceIdentity(t *testing.T) {
	for _, change := range []string{"user", "generation", "workspace", "pod"} {
		t.Run(change, func(t *testing.T) {
			s, kube, _ := recoveryFlowFixture(t, "stopped")
			ctx := context.Background()
			baseline, err := s.recoveryEvictionPreflight(ctx, RuntimeSchedulingRequest{SourcePodID: 1, Mode: "recovery"})
			if err != nil || !baseline.Allowed {
				t.Fatalf("baseline preflight failed: %+v %v", baseline, err)
			}
			if len(baseline.Items) != 1 || baseline.Items[0].InstanceID != 101 {
				t.Fatalf("healthy neighbor selected: %+v", baseline.Items)
			}
			if change == "user" {
				if _, err = s.sess.SQL().Exec("UPDATE instances SET status='stopped' WHERE id=102"); err != nil {
					t.Fatal(err)
				}
				selected, err := s.recoveryEvictionPreflight(ctx, RuntimeSchedulingRequest{SourcePodID: 1, Mode: "recovery", InstanceIDs: []int{101}})
				if err != nil || !selected.Allowed || len(selected.Items) != 1 || selected.Items[0].InstanceID != 101 {
					t.Fatalf("explicit selection expanded to later abnormal neighbor: %+v %v", selected, err)
				}
				if _, err = s.sess.SQL().Exec("UPDATE instances SET status='running' WHERE id=102"); err != nil {
					t.Fatal(err)
				}
			}
			cm, e := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-101", metav1.GetOptions{})
			if e != nil {
				t.Fatal(e)
			}
			switch change {
			case "user":
				cm.Data["user_id"] = "9"
			case "generation":
				cm.Data["generation"] = "5"
			case "pod":
				cm.Data["pod_uid"] = "replacement"
			case "workspace":
				if _, e = s.sess.SQL().Exec("UPDATE instance_runtime_bindings SET workspace_path='/foreign' WHERE instance_id=101"); e != nil {
					t.Fatal(e)
				}
			}
			if _, e = kube.CoreV1().ConfigMaps("runtime").Update(ctx, cm, metav1.UpdateOptions{}); e != nil {
				t.Fatal(e)
			}
			plan, e := s.recoveryEvictionPreflight(ctx, RuntimeSchedulingRequest{SourcePodID: 1, Mode: "recovery"})
			if e == nil && plan.Allowed {
				t.Fatalf("changed %s accepted", change)
			}
		})
	}
}

func TestRuntimeRecoveryDefaultEligibility(t *testing.T) {
	for _, status := range []string{"error", "creating", "deleting", "stopped", "running", "upgrading", "deleted"} {
		instance := &models.Instance{InstanceMode: "lite", RuntimeType: RuntimeBackendGateway, Status: status}
		want := status == "error" || status == "creating" || status == "deleting" || status == "stopped"
		if recoveryEvictionEligible(instance, false) != want {
			t.Errorf("default status %s", status)
		}
		if status == "running" && !recoveryEvictionEligible(instance, true) {
			t.Error("explicit healthy instance rejected")
		}
	}
}

func TestRuntimeRecoveryCancelSerializesBeforeFence(t *testing.T) {
	s, _, _ := recoveryFlowFixture(t, "stopped")
	ctx := context.Background()
	op, err := s.Evict(ctx, 1, RuntimeSchedulingRequest{Mode: "recovery"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.sess.SQL().Exec("UPDATE runtime_management_operations SET status='processing',owner_token=? WHERE id=?", s.token, op.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Cancel(ctx, op.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.beginRecoveryFence(ctx, op.ID, op.Items[0]); !errors.Is(err, errFaultRecoveryCancelled) {
		t.Fatalf("cancelled operation began fence: %v", err)
	}
	if _, err = s.sess.SQL().Exec("UPDATE runtime_management_operations SET cancel_requested=0 WHERE id=?", op.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.beginRecoveryFence(ctx, op.ID, op.Items[0]); err != nil {
		t.Fatal(err)
	}
	if err = s.Cancel(ctx, op.ID); err == nil {
		t.Fatal("fencing operation accepted cancellation")
	}
}

func TestRuntimeRecoveryCancelAttentionEnqueuesCleanup(t *testing.T) {
	s, _, _ := recoveryFlowFixture(t, "stopped")
	ctx := context.Background()
	op, err := s.Evict(ctx, 1, RuntimeSchedulingRequest{Mode: "recovery"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.sess.SQL().Exec("UPDATE runtime_management_operations SET status='needs_attention' WHERE id=?", op.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Cancel(ctx, op.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Process(ctx); err != nil {
		t.Fatal(err)
	}
	latest, _, _, err := s.runtimeManagementOperation(ctx, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Status != "cancelled" {
		t.Fatalf("cancel remained unprocessed: %s", latest.Status)
	}
	var n int
	row, err := s.sess.SQL().QueryRow("SELECT COUNT(*) FROM instance_lifecycle_manual_guards WHERE token=?", op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("cancel retained %d guards", n)
	}
}

func TestRuntimeRecoveryCompletionAndGuardRemovalAtomic(t *testing.T) {
	s, _, _ := recoveryFlowFixture(t, "stopped")
	ctx := context.Background()
	op, err := s.Evict(ctx, 1, RuntimeSchedulingRequest{Mode: "recovery"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.sess.SQL().Exec("UPDATE runtime_management_operations SET status='processing',owner_token=? WHERE id=?", s.token, op.ID); err != nil {
		t.Fatal(err)
	}
	op.Status = "starting"
	op.Items[0].Recovery.Phase = "released"
	if _, err = s.sess.SQL().Exec("CREATE TRIGGER fail_recovery_guard_delete BEFORE DELETE ON instance_lifecycle_manual_guards FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected cleanup failure'"); err != nil {
		t.Fatal(err)
	}
	if err = s.completeRecoveryEviction(ctx, op); err == nil {
		t.Fatal("injected cleanup failure ignored")
	}
	latest, _, _, err := s.runtimeManagementOperation(ctx, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Status != "processing" {
		t.Fatalf("completion escaped failed guard cleanup: %s", latest.Status)
	}
	if _, err = s.sess.SQL().Exec("DROP TRIGGER fail_recovery_guard_delete"); err != nil {
		t.Fatal(err)
	}
	if err = s.completeRecoveryEviction(ctx, op); err != nil {
		t.Fatal(err)
	}
	latest, _, _, err = s.runtimeManagementOperation(ctx, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Status != "starting" {
		t.Fatalf("completion missing: %s", latest.Status)
	}
	var n int
	row, err := s.sess.SQL().QueryRow("SELECT COUNT(*) FROM instance_lifecycle_manual_guards WHERE token=?", op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("completion retained %d guards", n)
	}
}
