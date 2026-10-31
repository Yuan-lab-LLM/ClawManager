package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/upper/db/v4"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func scaleInMySQLFixture(t *testing.T) (db.Session, *RuntimeManagementService, *fake.Clientset) {
	t.Helper()
	sess, maintenance, kube := runtimeScaleMySQLFixture(t, "hermes", "hermes-runtime")
	ctx := context.Background()
	d, err := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	d.Status.Replicas = 2
	d.Status.UpdatedReplicas = 2
	d.Status.ReadyReplicas = 2
	_, err = kube.AppsV1().Deployments("runtime").Update(ctx, d, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := kube.CoreV1().Pods("runtime").Get(ctx, "hermes-runtime-pod", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p.ResourceVersion = "1"
	_, _ = kube.CoreV1().Pods("runtime").Update(ctx, p, metav1.UpdateOptions{})
	q := p.DeepCopy()
	q.Name = "hermes-runtime-pod-2"
	q.UID = "hermes-runtime-pod-2-uid"
	_, err = kube.CoreV1().Pods("runtime").Create(ctx, q, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sess.SQL().Exec("INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,state,capacity,used_slots,last_seen_at,agent_endpoint) VALUES(2,'hermes','runtime',?,?,'hermes-runtime',?,'ready',8,0,?,'https://agent-2')", q.Name, string(q.UID), d.Spec.Template.Spec.Containers[0].Image, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err = sess.SQL().Exec("UPDATE runtime_pods SET agent_endpoint='https://agent-1' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	scheduler := maintenance.scheduler
	scheduler.agentClient = &strictStopRuntimeAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, strict: true}
	scheduler.heartbeatTimeout = time.Minute
	service := NewRuntimeManagementService(sess, scheduler, repository.NewNorthboundRepository(sess))
	kube.PrependReactor("delete", "pods", func(a ktesting.Action) (bool, runtime.Object, error) {
		action := a.(ktesting.DeleteAction)
		if len(action.GetDeleteOptions().DryRun) != 1 {
			t.Fatal("attempted non-dry-run Pod delete")
		}
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, action.GetName(), errors.New("CM_SCALE_IN_PROTECTED"))
	})
	return sess, service, kube
}
func scaleInQueue(t *testing.T, service *RuntimeManagementService) *RuntimeManagementOperation {
	t.Helper()
	req := RuntimePodScaleInRequest{RuntimeType: "hermes", PodCount: 1}
	plan, err := service.ScaleInPreflight(context.Background(), req)
	if err != nil || !plan.Allowed {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	req.PlanFingerprint = plan.PlanFingerprint
	op, err := service.ScaleInSubmit(context.Background(), req, nil)
	if err != nil {
		t.Fatal(err)
	}
	return op
}
func scaleInRead(t *testing.T, s *RuntimeManagementService, id string) (RuntimeManagementOperation, runtimePodScaleInJournal) {
	t.Helper()
	history, err := s.History(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range history {
		if op.ID == id {
			var raw string
			row, e := s.sess.SQL().QueryRow("SELECT request_json FROM runtime_management_operations WHERE id=?", id)
			if e == nil {
				e = row.Scan(&raw)
			}
			if e != nil {
				t.Fatal(e)
			}
			var journal runtimePodScaleInJournal
			if e = json.Unmarshal([]byte(raw), &journal); e != nil {
				t.Fatal(e)
			}
			return op, journal
		}
	}
	t.Fatal("operation missing")
	return RuntimeManagementOperation{}, runtimePodScaleInJournal{}
}
func TestScaleInMySQLZeroWorkJobAndSourceOnlyAdmission(t *testing.T) {
	sess, s, kube := scaleInMySQLFixture(t)
	ctx := context.Background()
	op := scaleInQueue(t, s)
	if op.Status != "pending" {
		t.Fatalf("zero-work job bypassed worker: %+v", op)
	}
	pods := repository.NewRuntimePodRepository(sess)
	if _, err := pods.TryClaimSlot(ctx, 1); err == nil {
		t.Fatal("claimed fenced source")
	}
	if err := s.Scheduling(ctx, 1, true); err == nil {
		t.Fatal("resumed fenced source")
	}
	claimed, err := pods.TryClaimSlot(ctx, 2)
	if err != nil || !claimed {
		t.Fatalf("retained admission blocked: %v %v", claimed, err)
	}
	_ = pods.ReleaseSlot(ctx, 2)
	if err = s.Process(ctx); err != nil {
		t.Fatal(err)
	}
	current, journal := scaleInRead(t, s, op.ID)
	if current.Status != "starting" || journal.Phase != "observing" {
		t.Fatalf("job did not observe: %+v %s", current, journal.Phase)
	}
	d, _ := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
	if *d.Spec.Replicas != 1 {
		t.Fatal("did not reduce count")
	}
	retained, _ := kube.CoreV1().Pods("runtime").Get(ctx, "hermes-runtime-pod-2", metav1.GetOptions{})
	if retained.Labels["clawmanager.io/scale-in-protected"] != op.ID {
		t.Fatal("retained Pod lacks protection")
	}
	if err = s.Process(ctx); err != nil {
		t.Fatal(err)
	}
	current, _ = scaleInRead(t, s, op.ID)
	if current.Status == "completed" {
		t.Fatal("completed before source UID gone")
	}
	// Simulate only the ReplicaSet controller's already-authorized removal.
	if err = kube.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), "runtime", "hermes-runtime-pod"); err != nil {
		t.Fatal(err)
	}
	d.Status.Replicas = 1
	d.Status.UpdatedReplicas = 1
	d.Status.ReadyReplicas = 1
	_, _ = kube.AppsV1().Deployments("runtime").Update(ctx, d, metav1.UpdateOptions{})
	if err = s.Process(ctx); err != nil {
		t.Fatal(err)
	}
	current, _ = scaleInRead(t, s, op.ID)
	if current.Status != "completed" {
		t.Fatalf("not complete: %+v", current)
	}
	retained, _ = kube.CoreV1().Pods("runtime").Get(ctx, "hermes-runtime-pod-2", metav1.GetOptions{})
	if retained.Labels["clawmanager.io/scale-in-protected"] != "" {
		t.Fatal("protection leaked")
	}
}
func TestScaleInMySQLMissingPolicyAndRestartPreserveReplicas(t *testing.T) {
	for _, scenario := range []string{"policy", "restart", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			sess, s, kube := scaleInMySQLFixture(t)
			ctx := context.Background()
			op := scaleInQueue(t, s)
			if scenario == "policy" {
				kube.PrependReactor("delete", "pods", func(a ktesting.Action) (bool, runtime.Object, error) { return true, nil, nil })
			} else if scenario == "restart" {
				if _, err := sess.SQL().Exec("UPDATE runtime_management_operations SET status='processing',owner_token='dead' WHERE id=?", op.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := s.Cancel(ctx, op.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Process(ctx); err != nil {
				t.Fatal(err)
			}
			if scenario == "restart" {
				if err := s.Process(ctx); err != nil {
					t.Fatal(err)
				}
			}
			current, _ := scaleInRead(t, s, op.ID)
			if current.Status != "failed" && current.Status != "cancelled" {
				t.Fatalf("not safely terminal: %+v", current)
			}
			d, _ := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
			if *d.Spec.Replicas != 2 {
				t.Fatal("failure shrank replicas")
			}
			pod, _ := s.scheduler.podRepo.GetByID(ctx, 1)
			if pod.SchedulingPaused {
				t.Fatal("source pause leaked")
			}
		})
	}
}
func TestScaleInMySQLSourceFenceBlocksPreclaimedBindingPublication(t *testing.T) {
	sess, s, _ := scaleInMySQLFixture(t)
	ctx := context.Background()
	op := scaleInQueue(t, s)
	if _, err := sess.SQL().Exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES(51,1,'hermes','hermes','lite','running',1,NOW(6))"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.SQL().Exec("INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,state,generation,workspace_path) VALUES(51,1,'hermes','gw-51-1',20000,'creating',1,'/workspaces/test')"); err != nil {
		t.Fatal(err)
	}
	uid := "hermes-runtime-pod-uid"
	published := false
	err := repository.WithRuntimePodAssignmentAdmission(ctx, sess, models.RuntimePod{Namespace: "runtime", PodName: "hermes-runtime-pod", PodUID: &uid, DeploymentName: "hermes-runtime", RuntimeType: "hermes"}, 51, 1, func() error { published = true; return nil })
	if err == nil || published || !strings.Contains(err.Error(), "scale-in source") {
		t.Fatalf("late publication passed: %v %v", published, err)
	}
	if err = s.Process(ctx); err != nil {
		t.Fatal(err)
	}
	current, _ := scaleInRead(t, s, op.ID)
	if current.Status != "failed" {
		t.Fatalf("late admission not rejected: %+v", current)
	}
}
func TestScaleInMySQLStaleCleanupCannotUnprotectNewOwnerResize(t *testing.T) {
	sess, s, kube := scaleInMySQLFixture(t)
	ctx := context.Background()
	op := scaleInQueue(t, s)
	if err := s.Process(ctx); err != nil {
		t.Fatal(err)
	}
	current, journal := scaleInRead(t, s, op.ID)
	// A stale observer still holds pre-resize data while another term owns resize.
	journal.Phase = "migrating"
	current.Status = "starting"
	if _, err := sess.SQL().Exec("UPDATE runtime_management_operations SET status='processing',owner_token='new-term' WHERE id=?", op.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.failScaleInBeforeResize(ctx, &current, &journal, errors.New("stale cleanup")); err == nil {
		t.Fatal("stale owner cleanup accepted")
	}
	retained, _ := kube.CoreV1().Pods("runtime").Get(ctx, "hermes-runtime-pod-2", metav1.GetOptions{})
	if retained.Labels["clawmanager.io/scale-in-protected"] != op.ID {
		t.Fatal("stale cleanup removed protection")
	}
}

func TestScaleInMySQLEveryDestinationMustStillBeRunning(t *testing.T) {
	for _, scenario := range []string{"lost-binding", "unhealthy-binding"} {
		t.Run(scenario, func(t *testing.T) {
			sess, s, kube := scaleInMySQLFixture(t)
			ctx := context.Background()
			op := scaleInQueue(t, s)
			_, journal := scaleInRead(t, s, op.ID)
			// Simulate moves already accepted. Item51 was previously RUNNING, but item52
			// still awaits health, so the worker must recheck item51 on later ticks.
			for _, id := range []int{51, 52} {
				if _, err := sess.SQL().Exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES(?,1,'hermes','hermes','lite','running',1,NOW(6))", id); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := sess.SQL().Exec("INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,state,generation,workspace_path) VALUES(52,2,'hermes','gw-52-1',20001,'creating',1,'/workspaces/test')"); err != nil {
				t.Fatal(err)
			}
			if scenario == "unhealthy-binding" {
				if _, err := sess.SQL().Exec("INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,state,generation,workspace_path) VALUES(51,2,'hermes','gw-51-1',20000,'error',1,'/workspaces/test')"); err != nil {
					t.Fatal(err)
				}
			}
			op.Items = []RuntimeSchedulingItem{{InstanceID: 51, SourcePodID: 1, TargetPodID: 2, TargetSlot: 1, State: "completed", SourceGeneration: 0, SourcePodUID: "hermes-runtime-pod-uid", TargetPodUID: "hermes-runtime-pod-2-uid"}, {InstanceID: 52, SourcePodID: 1, TargetPodID: 2, TargetSlot: 2, State: "starting", SourceGeneration: 0, SourcePodUID: "hermes-runtime-pod-uid", TargetPodUID: "hermes-runtime-pod-2-uid"}}
			journal.Phase = "acquiring_protection"
			raw, _ := json.Marshal(journal)
			if _, err := sess.SQL().Exec("UPDATE runtime_management_operations SET status='processing',owner_token=?,request_json=? WHERE id=?", s.token, string(raw), op.ID); err != nil {
				t.Fatal(err)
			}
			deploy := s.scheduler.deployments.(interface {
				ProtectRuntimeScaleIn(context.Context, k8s.RuntimePodScaleInProtection) error
			})
			if err := deploy.ProtectRuntimeScaleIn(ctx, journal.Protection); err != nil {
				t.Fatal(err)
			}
			journal.Phase = "migrating"
			op.Status = "starting"
			if err := s.saveScaleIn(ctx, op, &journal); err != nil {
				t.Fatal(err)
			}
			if err := s.Process(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := sess.SQL().Exec("UPDATE instance_runtime_bindings SET state='running' WHERE instance_id=52"); err != nil {
				t.Fatal(err)
			}
			if err := s.Process(ctx); err != nil {
				t.Fatal(err)
			}
			d, _ := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
			if *d.Spec.Replicas != 2 {
				t.Fatal("shrunk after earlier healthy destination regressed")
			}
			current, _ := scaleInRead(t, s, op.ID)
			if scenario == "lost-binding" {
				if current.Items[0].State == "completed" {
					t.Fatal("stale completion survived missing binding")
				}
			} else if current.Status != "failed" {
				t.Fatalf("unhealthy migration not failed: %+v", current)
			}
		})
	}
}
func TestScaleInMySQLFingerprintRejectsChangedUnselectedDestination(t *testing.T) {
	sess, s, _ := scaleInMySQLFixture(t)
	ctx := context.Background()
	plan, err := s.ScaleInPreflight(ctx, RuntimePodScaleInRequest{RuntimeType: "hermes", PodCount: 1})
	if err != nil || !plan.Allowed {
		t.Fatalf("%+v %v", plan, err)
	}
	if _, err = sess.SQL().Exec("UPDATE runtime_pods SET used_slots=1 WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ScaleInSubmit(ctx, RuntimePodScaleInRequest{RuntimeType: "hermes", PodCount: 1, PlanFingerprint: plan.PlanFingerprint}, nil); err == nil {
		t.Fatal("stale capacity plan accepted")
	}
}

func TestScaleInMySQLMoveFailurePreservesReplicas(t *testing.T) {
	sess, s, kube := scaleInMySQLFixture(t)
	sess.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	op := scaleInQueue(t, s)
	items := []RuntimeSchedulingItem{{InstanceID: 77, SourcePodID: 1, TargetPodID: 2, TargetSlot: 1, State: "pending", SourceGeneration: 1, SourcePodUID: "hermes-runtime-pod-uid", TargetPodUID: "hermes-runtime-pod-2-uid"}}
	raw, _ := json.Marshal(items)
	if _, err := sess.SQL().Exec("UPDATE runtime_management_operations SET items_json=? WHERE id=?", string(raw), op.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Process(ctx); err != nil {
		t.Fatal(err)
	}
	current, _ := scaleInRead(t, s, op.ID)
	if current.Status != "failed" || current.ErrorMessage == "" {
		t.Fatalf("move failure not visible: %+v", current)
	}
	d, _ := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
	if *d.Spec.Replicas != 2 || d.Labels["clawmanager.io/scale-in-operation"] != "" {
		t.Fatal("failed move changed replicas or leaked guard")
	}
}
