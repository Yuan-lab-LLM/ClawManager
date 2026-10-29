package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"context"
	"github.com/upper/db/v4/adapter/mysql"
	"io"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"strings"
	"testing"
	"time"
)

func TestRuntimeMaintenanceMySQLEmptyDeletionAdmission(t *testing.T) {
	sess := runtimeManagementMySQLDB(t)
	ctx := context.Background()
	management := runtimeManagementMySQLService(sess)
	scheduler := management.scheduler
	scheduler.runtimeNamespace = "runtime"
	one := int32(1)
	yes := true
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "managed", Namespace: "runtime", UID: types.UID("deployment-uid"), ResourceVersion: "2", Labels: map[string]string{"clawmanager.io/managed-pool": "true", "clawmanager.io/purpose": "runtime-managed-pool", "clawmanager.io/runtime-type": "opencode"}}, Spec: appsv1.DeploymentSpec{Replicas: &one}}
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "managed-rs", Namespace: "runtime", UID: types.UID("rs-uid"), OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "managed", UID: deployment.UID, Controller: &yes}}}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "managed-pod", Namespace: "runtime", UID: types.UID("pod-uid"), Labels: map[string]string{"app": "managed"}, OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: &yes}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	kube := fake.NewSimpleClientset(deployment, rs, pod)
	scheduler.deployments = k8s.NewRuntimeDeploymentService(kube)
	scheduler.agentClient = NewSlotRuntimeAgentClient("test", kube, "runtime")
	service := NewRuntimePodMaintenanceService(sess, scheduler)
	if _, err := sess.SQL().Exec("INSERT INTO runtime_pods (id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,state,capacity,used_slots,last_seen_at) VALUES (9,'opencode','runtime','managed-pod','pod-uid','managed','image','ready',2,1,NOW())"); err != nil {
		t.Fatal(err)
	}
	if err := service.Delete(ctx, 9, "pod-uid"); err == nil {
		t.Fatal("deleted claimed pool")
	}
	reported, err := scheduler.podRepo.GetByID(ctx, 9)
	if err != nil || reported.SchedulingPaused {
		t.Fatalf("occupied delete left an unnecessary pause: %v %v", reported, err)
	}
	if err = repository.CheckRuntimePodDeletionIntent(ctx, sess, "runtime", "managed"); err != nil {
		t.Fatalf("occupied delete left an unnecessary intent: %v", err)
	}
	if err := scheduler.podRepo.UpdateHeartbeat(ctx, 9, "ready", 0, 2, false, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.SQL().Exec("INSERT INTO instances (id,user_id,type,runtime_type,instance_mode,status,runtime_generation,workspace_path,updated_at) VALUES (101,1,'opencode','gateway','lite','creating',1,'/workspace',NOW())"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.SQL().Exec("INSERT INTO instance_runtime_bindings (instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES (101,9,'opencode','pending-101-1',20000,'/workspace','starting',1)"); err != nil {
		t.Fatal(err)
	}
	// Publication uses actual Kubernetes ownership, never the latest row with
	// an attacker-reported duplicate UID or a rewritten controller name.
	published := 0
	client := scheduler.agentClient.(*slotRuntimeAgentClient)
	if err := client.assignmentAdmission(ctx, "pod-uid", "opencode", 101, 1, func() error { published++; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.SQL().Exec("INSERT INTO runtime_pods(runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,state,capacity) VALUES('opencode','other','shadow','pod-uid','other','image','ready',2)"); err != nil {
		t.Fatal(err)
	}
	if err := client.assignmentAdmission(ctx, "pod-uid", "opencode", 101, 1, func() error { published++; return nil }); err == nil {
		t.Fatal("duplicate SQL UID bypassed actual controller identity")
	}
	if _, err := sess.SQL().Exec("DELETE FROM runtime_pods WHERE namespace='other'"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.SQL().Exec("UPDATE runtime_pods SET deployment_name='other' WHERE id=9"); err != nil {
		t.Fatal(err)
	}
	if err := client.assignmentAdmission(ctx, "pod-uid", "opencode", 101, 1, func() error { published++; return nil }); err == nil {
		t.Fatal("rewritten SQL owner bypassed actual controller identity")
	}
	if _, err := sess.SQL().Exec("UPDATE runtime_pods SET deployment_name='managed' WHERE id=9"); err != nil {
		t.Fatal(err)
	}
	if published != 1 {
		t.Fatalf("unexpected writer publications: %d", published)
	}
	if err := service.Delete(ctx, 9, "pod-uid"); err == nil {
		t.Fatal("deleted pool with pending binding")
	}
	if _, err := sess.SQL().Exec("DELETE FROM instance_runtime_bindings WHERE runtime_pod_id=9"); err != nil {
		t.Fatal(err)
	}
	kube.PrependReactor("delete", "deployments", func(action clienttesting.Action) (bool, runtime.Object, error) {
		if err := kube.Tracker().Delete(appsv1.SchemeGroupVersion.WithResource("deployments"), "runtime", "managed"); err != nil {
			t.Fatal(err)
		}
		if err := kube.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), "runtime", "managed-pod"); err != nil {
			t.Fatal(err)
		}
		return true, nil, io.ErrUnexpectedEOF
	})
	if err := service.Delete(ctx, 9, "pod-uid"); err == nil || !strings.Contains(err.Error(), "intent remains active") {
		t.Fatalf("ambiguous deletion did not preserve explicit fence: %v", err)
	}
	if err := repository.CheckRuntimePodDeletionIntent(ctx, sess, "runtime", "managed"); err == nil {
		t.Fatal("ambiguous deletion cleared its durable fence")
	}
	if err := service.DeletePool(ctx, "runtime", "managed", "wrong-uid"); err == nil {
		t.Fatal("wrong UID retry accepted")
	}
	if err := service.DeletePool(ctx, "runtime", "managed", "deployment-uid"); err != nil {
		t.Fatalf("same UID retry after confirmed disappearance: %v", err)
	}
	if _, err := kube.AppsV1().Deployments("runtime").Get(ctx, "managed", metav1.GetOptions{}); err == nil {
		t.Fatal("controller survived accepted deletion")
	}
}

func TestRuntimeMaintenanceMySQLDeleteFenceRejectsLastMomentAdmission(t *testing.T) {
	for _, scenario := range []string{"resume-and-claim", "pending-register-and-claim"} {
		t.Run(scenario, func(t *testing.T) {
			sess := runtimeManagementMySQLDB(t)
			ctx := context.Background()
			management := runtimeManagementMySQLService(sess)
			scheduler := management.scheduler
			scheduler.runtimeNamespace = "runtime"
			// A distinct connection exercises admission outside the deleting service.
			second, openErr := mysql.Open(sess.ConnectionURL())
			if openErr != nil {
				t.Fatal(openErr)
			}
			defer second.Close()
			outside := repository.NewRuntimePodRepository(second)
			one := int32(1)
			yes := true
			deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "managed", Namespace: "runtime", UID: types.UID("deployment-uid"), ResourceVersion: "2", Labels: map[string]string{"clawmanager.io/managed-pool": "true", "clawmanager.io/purpose": "runtime-managed-pool", "clawmanager.io/runtime-type": "opencode"}}, Spec: appsv1.DeploymentSpec{Replicas: &one}}
			rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "rs", Namespace: "runtime", UID: types.UID("rs-uid"), OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: deployment.Name, UID: deployment.UID, Controller: &yes}}}}
			phase := corev1.PodRunning
			if scenario == "pending-register-and-claim" {
				phase = corev1.PodPending
			}
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "runtime", UID: types.UID("pod-uid"), Labels: map[string]string{"app": "managed"}, OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: &yes}}}, Status: corev1.PodStatus{Phase: phase}}
			kube := fake.NewSimpleClientset(deployment, rs, pod)
			scheduler.deployments = k8s.NewRuntimeDeploymentService(kube)
			scheduler.agentClient = NewSlotRuntimeAgentClient("test", kube, "runtime")
			outsideKube := fake.NewSimpleClientset(deployment.DeepCopy(), rs.DeepCopy(), pod.DeepCopy())
			outsideManagement := runtimeManagementMySQLService(second)
			outsideManagement.scheduler.runtimeNamespace = "runtime"
			outsideManagement.scheduler.podRepo = outside
			outsideManagement.scheduler.deployments = k8s.NewRuntimeDeploymentService(outsideKube)
			outsideManagement.scheduler.agentClient = NewSlotRuntimeAgentClient("test", outsideKube, "runtime")
			_ = NewRuntimePodMaintenanceService(second, outsideManagement.scheduler)
			service := NewRuntimePodMaintenanceService(sess, scheduler)
			insert := func() {
				t.Helper()
				if _, err := second.SQL().Exec("INSERT INTO runtime_pods (id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,state,capacity,used_slots,last_seen_at) VALUES (9,'opencode','runtime','pod','pod-uid','managed','image','ready',2,0,?)", time.Now().UTC()); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "resume-and-claim" {
				insert()
				// A different scheduler already claimed, then a zero-process heartbeat
				// erased that counter before its delayed binding insert. The fence must
				// reject that late insert as well as brand-new claims and Resume.
				claimed, err := outside.TryClaimSlot(ctx, 9)
				if err != nil || !claimed {
					t.Fatalf("pre-fence claim: %v %v", claimed, err)
				}
				if err = outside.UpdateHeartbeat(ctx, 9, "ready", 0, 2, false, time.Now().UTC()); err != nil {
					t.Fatal(err)
				}
			}
			podUIDForFenceTest := "pod-uid"
			resumes, claims, unsafeDeletes := 0, 0, 0
			publications := 0
			kube.PrependReactor("delete", "deployments", func(action clienttesting.Action) (bool, runtime.Object, error) {
				for _, bad := range []struct{ namespace, podName, owner string }{{"runtime", "pod", "other"}, {"other", "pod", "managed"}, {"runtime", "duplicate", "managed"}} {
					now := time.Now().UTC()
					uid := "pod-uid"
					report := &models.RuntimePod{Namespace: bad.namespace, PodName: bad.podName, PodUID: &uid, DeploymentName: bad.owner, RuntimeType: "opencode", ImageRef: "image", State: "ready", Capacity: 2, LastSeenAt: &now}
					if err := outside.UpsertFromAgent(ctx, report); err == nil {
						t.Fatalf("registration changed verified controller identity: %+v", bad)
					}
				}
				if scenario == "pending-register-and-claim" {
					insert()
					live := pod.DeepCopy()
					live.Status.Phase = corev1.PodRunning
					if err := kube.Tracker().Update(corev1.SchemeGroupVersion.WithResource("pods"), live, "runtime"); err != nil {
						t.Fatal(err)
					}
				}
				now := time.Now().UTC()
				uid := "pod-uid"
				good := &models.RuntimePod{Namespace: "runtime", PodName: "pod", PodUID: &uid, DeploymentName: "managed", RuntimeType: "opencode", ImageRef: "image", State: "ready", Capacity: 2, LastSeenAt: &now}
				if err := outside.UpsertFromAgent(ctx, good); err != nil || good.ID != 9 || !good.SchedulingPaused {
					t.Fatalf("valid fenced registration did not preserve identity/pause: %+v %v", good, err)
				}
				legacy := repository.NewRuntimePodRepository(second)
				changed := *good
				changed.DeploymentName = "other"
				if err := legacy.UpsertFromAgent(ctx, &changed); err == nil || !strings.Contains(err.Error(), "fenced") {
					t.Fatalf("transactional owner freeze bypassed: %v", err)
				}
				if err := outside.(interface {
					SetSchedulingPaused(context.Context, int64, bool) error
				}).SetSchedulingPaused(ctx, 9, false); err == nil {
					resumes++
				}
				if claimed, err := outside.TryClaimSlot(ctx, 9); err != nil {
					t.Fatal(err)
				} else if claimed {
					claims++
					unsafeDeletes++
				}
				if err := repository.WithRuntimePodAssignmentAdmission(ctx, second, models.RuntimePod{Namespace: "runtime", PodName: "pod", PodUID: &podUIDForFenceTest, DeploymentName: "managed", RuntimeType: "opencode"}, 101, 1, func() error { publications++; return nil }); err == nil {
					t.Fatal("fenced assignment publication was admitted")
				}

				late := &models.InstanceRuntimeBinding{RuntimePodID: 9, InstanceID: 101, RuntimeType: "opencode", Generation: 1, GatewayID: "pending-101-1", GatewayPort: 20000, WorkspacePath: "/workspace", State: "creating"}
				bindings := repository.NewInstanceRuntimeBindingRepository(second)
				if err := bindings.Create(ctx, late); err == nil || !strings.Contains(err.Error(), "fenced") {
					t.Fatalf("late binding publication not fenced: %v", err)
				}
				reported := *late
				reported.GatewayID = "gw-101-1"
				reported.State = "running"
				if _, err := bindings.(repository.ReportedGatewayBindingReconciler).ReconcileReportedGateway(ctx, &reported, 1, 1); err == nil || !strings.Contains(err.Error(), "fenced") {
					t.Fatalf("late report replay not fenced: %v", err)
				}
				target := models.RuntimePod{ID: 9, PodUID: &podUIDForFenceTest}
				if _, err := bindings.(repository.UpgradeGatewayReserver).ReserveUpgradeGateway(ctx, repository.UpgradeGatewayReservation{RolloutID: 1, InstanceID: 101, Generation: 1, Pod: target, WorkspacePath: "/workspace", PortStart: 20000, PortEnd: 20010, PortBlockSize: 1}); err == nil || !strings.Contains(err.Error(), "fenced") {
					t.Fatalf("late upgrade reservation not fenced: %v", err)
				}
				return false, nil, nil
			})

			var err error
			if scenario == "resume-and-claim" {
				err = service.Delete(ctx, 9, "pod-uid")
			} else {
				err = service.DeletePool(ctx, "runtime", "managed", "deployment-uid")
			}
			if err != nil {
				t.Fatal(err)
			}
			if resumes != 0 || claims != 0 || unsafeDeletes != 0 || publications != 0 {
				t.Fatalf("durable fence failed: resumes=%d claims=%d unsafeDeletes=%d", resumes, claims, unsafeDeletes)
			}
		})
	}
}
