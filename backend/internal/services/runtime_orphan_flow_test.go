package services

import (
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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"strings"
	"testing"
	"time"
)

type orphanFlowBackend struct {
	*fakeRuntimeDeploymentService
	real         runtimeOrphanRecoveryBackend
	kube         *fake.Clientset
	scopes       []runtimeeviction.PodScope
	failure      error
	retireCalls  int
	observed     *runtimeeviction.PodReceipt
	observeCalls int
}

func (f *orphanFlowBackend) PrepareRuntimeOrphanSource(ctx context.Context, ns, name, uid, root string) (*k8s.RuntimeOrphanSource, error) {
	return f.real.PrepareRuntimeOrphanSource(ctx, ns, name, uid, root)
}
func (f *orphanFlowBackend) RetireRuntimeOrphanController(ctx context.Context, s *k8s.RuntimeOrphanSource, op string) (bool, error) {
	f.retireCalls++
	return f.real.RetireRuntimeOrphanController(ctx, s, op)
}
func (f *orphanFlowBackend) FinalizeRuntimeOrphanSource(ctx context.Context, s *k8s.RuntimeOrphanSource) error {
	return f.real.FinalizeRuntimeOrphanSource(ctx, s)
}
func (f *orphanFlowBackend) ObserveRuntimeOrphanReceipt(context.Context, runtimeeviction.PodScope, *corev1.Pod) (*runtimeeviction.PodReceipt, error) {
	f.observeCalls++
	return f.observed, nil
}
func (f *orphanFlowBackend) RecoverRuntimeOrphan(ctx context.Context, s runtimeeviction.PodScope, p *corev1.Pod) (*runtimeeviction.PodReceipt, error) {
	f.scopes = append(f.scopes, s)
	node, e := f.kube.CoreV1().Nodes().Get(ctx, s.NodeName, metav1.GetOptions{})
	if e != nil {
		return nil, e
	}
	if string(node.UID) != s.NodeUID || node.Status.NodeInfo.BootID != s.NodeBootID {
		return nil, errors.New("source node boot identity changed")
	}
	if f.failure != nil {
		return nil, f.failure
	}
	return &runtimeeviction.PodReceipt{ScopeDigest: runtimeeviction.PodScopeDigest(s), OperationID: s.OperationID, PodUID: s.PodUID, NodeUID: s.NodeUID, NodeBootID: s.NodeBootID, InventoryDigest: strings.Repeat("a", 64), LedgerDigest: strings.Repeat("b", 64), StoppedContainerCount: 1, Confirmed: true, FinishedAt: time.Now().UTC()}, nil
}
func orphanFlowFixture(t *testing.T) (*RuntimeManagementService, *fake.Clientset, *orphanFlowBackend) {
	t.Helper()
	s, kube, _ := recoveryFlowFixture(t, "deleting")
	ctx := context.Background()
	root := t.TempDir()
	s.scheduler.workspaceRoot = root
	s.scheduler.runtimeNamespace = "runtime"
	exec := func(q string) {
		t.Helper()
		if _, e := s.sess.SQL().Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	exec("ALTER TABLE team_members ADD COLUMN team_id INT")
	exec("DELETE FROM instance_runtime_bindings")
	exec("DELETE FROM instances WHERE id IN (101,102)")
	exec("CREATE TABLE IF NOT EXISTS instance_workspace_write_intents(operation_id VARCHAR(64),instance_id INT)")
	exec("CREATE TABLE IF NOT EXISTS runtime_pod_deletion_intents(namespace VARCHAR(255),deployment_name VARCHAR(255),deployment_uid VARCHAR(255),PRIMARY KEY(namespace,deployment_name))")
	yes := true
	one := int32(1)
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "runtime", UID: "dep", ResourceVersion: "10", Labels: map[string]string{"clawmanager.io/runtime-type": "opencode"}}, Spec: appsv1.DeploymentSpec{Replicas: &one, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "pool"}}}}
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "pool-rs", Namespace: "runtime", UID: "rs", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: d.Name, UID: d.UID, Controller: &yes}}}}
	kube.AppsV1().Deployments("runtime").Create(ctx, d, metav1.CreateOptions{})
	kube.AppsV1().ReplicaSets("runtime").Create(ctx, rs, metav1.CreateOptions{})
	p, _ := kube.CoreV1().Pods("runtime").Get(ctx, "pool-old", metav1.GetOptions{})
	p.ResourceVersion = "11"
	p.CreationTimestamp = metav1.NewTime(time.Now().Add(-time.Minute))
	p.Labels = map[string]string{"app": "pool"}
	p.OwnerReferences = []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: &yes}}
	p.Status.PodIP = "10.1.0.2"
	p.Spec.Containers[0].VolumeMounts[0].MountPath = root
	p.Spec.Containers[0].Env[1].Value = root
	kube.CoreV1().Pods("runtime").Update(ctx, p, metav1.UpdateOptions{})
	t.Setenv("POD_NAME", "manager")
	manager := p.DeepCopy()
	manager.Name = "manager"
	manager.UID = "manager-uid"
	manager.OwnerReferences = []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "manager-rs", UID: "manager-rs-uid", Controller: &yes}}
	manager.Labels = map[string]string{"app": "clawmanager-app"}
	manager.Status.PodIP = "10.1.0.3"
	kube.CoreV1().Pods("runtime").Create(ctx, manager, metav1.CreateOptions{})
	app := d.DeepCopy()
	app.Name = "clawmanager-app"
	app.UID = "manager-dep"
	app.Labels = map[string]string{}
	app.Spec.Template.Spec.Containers = []corev1.Container{{Name: "app", Image: "app@sha256:" + strings.Repeat("a", 64)}}
	kube.AppsV1().Deployments("runtime").Create(ctx, app, metav1.CreateOptions{})
	appRS := rs.DeepCopy()
	appRS.Name = "manager-rs"
	appRS.UID = "manager-rs-uid"
	appRS.OwnerReferences = []metav1.OwnerReference{{Kind: "Deployment", Name: app.Name, UID: app.UID, Controller: &yes}}
	kube.AppsV1().ReplicaSets("runtime").Create(ctx, appRS, metav1.CreateOptions{})
	for _, id := range []int{101, 102} {
		cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(ctx, fmt.Sprintf("lite-instance-%d", id), metav1.GetOptions{})
		cm.Data["request"] = strings.ReplaceAll(cm.Data["request"], "/workspaces", root)
		kube.CoreV1().ConfigMaps("runtime").Update(ctx, cm, metav1.UpdateOptions{})
	}
	// Simulate the independent Deployment/RS controllers acknowledging scale
	// before Pod deletion. The K8s regression separately exercises delayed ack.
	kube.PrependReactor("update", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		updated := action.(ktesting.UpdateAction).GetObject().(*appsv1.Deployment).DeepCopy()
		if updated.Name != "pool" || updated.Spec.Replicas == nil || *updated.Spec.Replicas != 0 {
			return false, nil, nil
		}
		updated.Generation++
		updated.Status.ObservedGeneration = updated.Generation
		objSet, err := kube.Tracker().Get(appsv1.SchemeGroupVersion.WithResource("replicasets"), "runtime", "pool-rs")
		if err != nil {
			return true, nil, err
		}
		set := objSet.(*appsv1.ReplicaSet)
		zero := int32(0)
		set.Spec.Replicas = &zero
		set.Status.Replicas = 0
		set.Status.ObservedGeneration = set.Generation
		kube.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("replicasets"), set, "runtime")
		updated.Status.Replicas = 0
		e := kube.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), updated, "runtime")
		return true, updated, e
	})
	kube.PrependReactor("delete", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		name := action.(ktesting.DeleteAction).GetName()
		obj, e := kube.Tracker().Get(corev1.SchemeGroupVersion.WithResource("pods"), "runtime", name)
		var pod *corev1.Pod
		if obj != nil {
			pod = obj.(*corev1.Pod)
		}
		if e != nil {
			return true, nil, e
		}

		pod.DeletionTimestamp = &metav1.Time{Time: time.Now()}
		pod.Status.Phase = corev1.PodFailed
		pod.ResourceVersion = "terminal-rv"
		e = kube.Tracker().Update(corev1.SchemeGroupVersion.WithResource("pods"), pod, "runtime")
		return true, nil, e
	})
	agent := s.scheduler.agentClient.(*slotRuntimeAgentClient)
	agent.workspaceWriteFence = func(context.Context, runtimeauth.Identity) error { return nil }
	agent.workspaceExternalStop = agent.workspaceWriteFence
	real := k8s.NewRuntimeDeploymentService(kube).(runtimeOrphanRecoveryBackend)
	backend := &orphanFlowBackend{fakeRuntimeDeploymentService: &fakeRuntimeDeploymentService{}, real: real, kube: kube}
	s.scheduler.deployments = backend
	s.scheduler.upgrade = NewRuntimeUpgradeService(s.sess, s.scheduler.rolloutRepo, s.scheduler.podRepo, s.scheduler.bindingRepo, s.scheduler.agentClient, root, "")
	return s, kube, backend
}
func TestRuntimeOrphanRecoveryDurableFlowMySQL(t *testing.T) {
	s, kube, backend := orphanFlowFixture(t)
	ctx := context.Background()
	req := RuntimeSchedulingRequest{Mode: "orphan-recovery", SourcePodUID: "old"}
	op, e := s.Evict(ctx, 1, req, nil)
	if e != nil {
		t.Fatal(e)
	}
	if len(op.Items) != 1 || op.Kind != "orphan_recovery" {
		t.Fatalf("wrong operation %+v", op)
	}
	backend.failure = k8s.ErrRuntimeFaultEvictionPending
	for i := 0; i < 2; i++ {
		if e = s.Process(ctx); e != nil {
			t.Fatal(e)
		}
	}
	pending, _, _, e := s.runtimeManagementOperation(ctx, op.ID)
	if e != nil {
		t.Fatal(e)
	}
	if pending.Status != "pending" || pending.Items[0].OrphanRecovery.Phase != "helper_pending" {
		t.Fatalf("bad pending %+v", pending)
	}
	pod, _ := s.scheduler.podRepo.GetByID(ctx, 1)
	if pod.UsedSlots != 2 {
		t.Fatal("zeroed stale occupancy before proof")
	}
	if e = s.Cancel(ctx, op.ID); e == nil {
		t.Fatal("cancelled after irreversible fencing")
	}
	backend.failure = errors.New("helper identity conflict")
	if e = s.Process(ctx); e != nil {
		t.Fatal(e)
	}
	held, _, _, _ := s.runtimeManagementOperation(ctx, op.ID)
	if held.Status != "needs_attention" {
		t.Fatalf("conflict not held %+v", held)
	}
	if _, e = s.RetryOperation(ctx, op.ID); e != nil {
		t.Fatal(e)
	}
	backend.failure = nil
	if e = s.Process(ctx); e != nil {
		t.Fatal(e)
	}
	done, _, _, _ := s.runtimeManagementOperation(ctx, op.ID)
	j := done.Items[0].OrphanRecovery
	if done.Status != "completed" || j.Phase != "completed" || runtimeeviction.ValidatePodReceipt(j.Scope, j.Receipt) != nil {
		b, _ := json.Marshal(done)
		t.Fatalf("not completed %s", b)
	}
	pod, _ = s.scheduler.podRepo.GetByID(ctx, 1)
	if pod.State != "deleted" || pod.UsedSlots != 0 || !pod.SchedulingPaused {
		t.Fatalf("bad retired SQL source %+v", pod)
	}
	if len(backend.scopes) != 3 || runtimeeviction.PodScopeDigest(backend.scopes[0]) != runtimeeviction.PodScopeDigest(backend.scopes[2]) {
		t.Fatal("retry changed helper scope")
	}
	for _, id := range []int{101, 102} {
		cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(ctx, fmt.Sprintf("lite-instance-%d", id), metav1.GetOptions{})
		if cm.Data["desired_state"] != "stopped" || cm.Annotations[k8s.RuntimeStoppedSourceAnnotationPrefix+"4"] == "" {
			t.Fatal("source publication not retired")
		}
	}
}
func TestRuntimeOrphanAdmissionAndCancellationMySQL(t *testing.T) {
	for _, kind := range []string{"binding", "publisher", "running", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			s, _, backend := orphanFlowFixture(t)
			ctx := context.Background()
			if kind == "binding" {
				if _, e := s.sess.SQL().Exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES(166,1,'opencode','gateway','lite','creating',1,NOW())"); e != nil {
					t.Fatal(e)
				}
				if _, e := s.sess.SQL().Exec("INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES(166,1,'opencode','gw-166-1',20000,'/workspaces/opencode/user-1/instance-166','creating',1)"); e != nil {
					t.Fatal(e)
				}
			}
			if kind == "publisher" || kind == "running" {
				status := "creating"
				if kind == "running" {
					status = "running"
				}
				if _, e := s.sess.SQL().Exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES(101,1,'opencode','gateway','lite',?,4,NOW())", status); e != nil {
					t.Fatal(e)
				}
			}
			op, e := s.Evict(ctx, 1, RuntimeSchedulingRequest{Mode: "orphan-recovery", SourcePodUID: "old"}, nil)
			if kind != "cancel" {
				if e == nil || backend.retireCalls != 0 {
					t.Fatalf("active owner admitted: %v", e)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if e = s.Cancel(ctx, op.ID); e != nil {
				t.Fatal(e)
			}
			if e = s.Process(ctx); e != nil {
				t.Fatal(e)
			}
			current, _, _, _ := s.runtimeManagementOperation(ctx, op.ID)
			if current.Status != "cancelled" || backend.retireCalls != 0 {
				t.Fatal("cancelled operation stopped source")
			}
		})
	}
}

func TestRuntimeOrphanNodeBootRecoveryMySQL(t *testing.T) {
	for _, kind := range []string{"rebase", "historical", "changed-uid"} {
		t.Run(kind, func(t *testing.T) {
			s, kube, backend := orphanFlowFixture(t)
			ctx := context.Background()
			op, e := s.Evict(ctx, 1, RuntimeSchedulingRequest{Mode: "orphan-recovery", SourcePodUID: "old"}, nil)
			if e != nil {
				t.Fatal(e)
			}
			backend.failure = k8s.ErrRuntimeFaultEvictionPending
			for i := 0; i < 2; i++ {
				if e = s.Process(ctx); e != nil {
					t.Fatal(e)
				}
			}
			if len(backend.scopes) != 1 {
				t.Fatal("missing initial helper scope")
			}
			old := backend.scopes[0]
			node, _ := kube.CoreV1().Nodes().Get(ctx, "node", metav1.GetOptions{})
			node.Status.NodeInfo.BootID = "boot-2"
			if kind == "changed-uid" {
				node.UID = "replacement-node"
			}
			kube.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
			backend.failure = nil
			if kind == "historical" {
				backend.observed = &runtimeeviction.PodReceipt{ScopeDigest: runtimeeviction.PodScopeDigest(old), OperationID: old.OperationID, PodUID: old.PodUID, NodeUID: old.NodeUID, NodeBootID: old.NodeBootID, InventoryDigest: strings.Repeat("a", 64), LedgerDigest: strings.Repeat("b", 64), Confirmed: true, FinishedAt: time.Now().UTC()}
				backend.failure = errors.New("completed old scope must not execute another helper")
			}
			if e = s.Process(ctx); e != nil {
				t.Fatal(e)
			}
			current, _, _, _ := s.runtimeManagementOperation(ctx, op.ID)
			j := current.Items[0].OrphanRecovery
			if kind == "changed-uid" {
				if current.Status != "needs_attention" || len(backend.scopes) != 1 || j.Receipt != nil {
					t.Fatal("replacement node accepted")
				}
				return
			}
			if current.Status != "completed" {
				t.Fatalf("node recovery held: %+v", current)
			}
			if kind == "rebase" {
				if len(backend.scopes) != 2 || j.Scope.NodeBootID != "boot-2" || runtimeeviction.PodScopeDigest(old) == runtimeeviction.PodScopeDigest(j.Scope) {
					t.Fatal("same-node boot was not durably rebased")
				}
			} else if len(backend.scopes) != 1 || backend.observeCalls != 1 || j.Scope.NodeBootID != old.NodeBootID {
				t.Fatal("historical receipt was replayed or rebased")
			}
		})
	}
}
func TestRuntimeOrphanFenceRechecksOwnerBeforeEffectsMySQL(t *testing.T) {
	for _, kind := range []string{"publisher", "controller"} {
		t.Run(kind, func(t *testing.T) {
			s, kube, backend := orphanFlowFixture(t)
			ctx := context.Background()
			op, e := s.Evict(ctx, 1, RuntimeSchedulingRequest{Mode: "orphan-recovery", SourcePodUID: "old"}, nil)
			if e != nil {
				t.Fatal(e)
			}
			if kind == "publisher" {
				if _, e = s.sess.SQL().Exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES(101,1,'opencode','gateway','lite','creating',4,NOW())"); e != nil {
					t.Fatal(e)
				}
			} else {
				d, _ := kube.AppsV1().Deployments("runtime").Get(ctx, "pool", metav1.GetOptions{})
				d.UID = "new-controller"
				kube.AppsV1().Deployments("runtime").Update(ctx, d, metav1.UpdateOptions{})
			}
			if e = s.Process(ctx); e != nil {
				t.Fatal(e)
			}
			current, _, _, _ := s.runtimeManagementOperation(ctx, op.ID)
			cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-101", metav1.GetOptions{})
			if current.Status != "needs_attention" || backend.retireCalls != 0 || len(backend.scopes) != 0 || cm.Data["desired_state"] != "running" {
				t.Fatalf("changed owner/controller received stop effects %+v", current)
			}
		})
	}
}

func TestRuntimeOrphanStoppedAndDeletingSourcesAcquireLifecycleGuardMySQL(t *testing.T) {
	for _, status := range []string{"stopped", "deleting"} {
		t.Run(status, func(t *testing.T) {
			s, _, backend := orphanFlowFixture(t)
			ctx := context.Background()
			if _, e := s.sess.SQL().Exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES(101,1,'opencode','gateway','lite',?,4,NOW())", status); e != nil {
				t.Fatal(e)
			}
			op, e := s.Evict(ctx, 1, RuntimeSchedulingRequest{Mode: "orphan-recovery", SourcePodUID: "old"}, nil)
			if e != nil {
				t.Fatal(e)
			}
			if len(op.Items[0].OrphanRecovery.GuardedInstanceIDs) != 1 {
				t.Fatal("source was not guarded")
			}
			if release, e := s.guards.ReserveInstanceMutations([]int{101}); e == nil {
				release()
				t.Fatal("new lifecycle mutation entered while old source was being stopped")
			}
			for i := 0; i < 2; i++ {
				if e = s.Process(ctx); e != nil {
					t.Fatal(e)
				}
			}
			done, _, _, _ := s.runtimeManagementOperation(ctx, op.ID)
			if done.Status != "completed" || len(backend.scopes) != 1 {
				t.Fatalf("unbound stopped/deleting source did not retire: %+v", done)
			}
			instance, e := s.scheduler.instanceRepo.GetByID(101)
			if e != nil || instance.Status != status || instance.RuntimeGeneration != 4 {
				t.Fatalf("lifecycle intent changed %+v %v", instance, e)
			}
			release, e := s.guards.ReserveInstanceMutations([]int{101})
			if e != nil {
				t.Fatal("completed operation stranded lifecycle guard", e)
			}
			release()
		})
	}
}
