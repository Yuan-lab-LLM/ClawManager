package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/upper/db/v4"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"strings"
	"testing"
	"time"
)

func TestRuntimeRolloutPrepareAcceptsCurrentImageAndFreezesScope(t *testing.T) {
	ctx := context.Background()
	sess, s, kube, old, _ := completedRolloutSleepFixture(t)
	d, err := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	image := "repo/hermes@sha256:" + strings.Repeat("d", 64)
	for i := range d.Spec.Template.Spec.Containers {
		c := &d.Spec.Template.Spec.Containers[i]
		c.Image = image
		for e := range c.Env {
			switch c.Env[e].Name {
			case "CLAWMANAGER_RUNTIME_IMAGE_REF":
				c.Env[e].Value = image
			case "CLAWMANAGER_RUNTIME_IMAGE_DIGEST":
				c.Env[e].Value = "sha256:" + strings.Repeat("d", 64)
			}
		}
	}
	d.Generation += 3
	if _, err = kube.AppsV1().Deployments("runtime").Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	op, err := s.PrepareRollout(ctx, "hermes", nil)
	if err != nil {
		t.Fatalf("image update blocked preparation: %v", err)
	}
	_, raw, _, err := s.runtimeManagementOperation(ctx, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	var prepared rolloutWakeJournal
	if err = json.Unmarshal([]byte(raw), &prepared); err != nil {
		t.Fatal(err)
	}
	if len(prepared.Pools) != 1 || len(prepared.Scope) != 1 || prepared.Scope[0].Image != image || prepared.Pools[0].Snapshot.Option.ImageRef != image {
		t.Fatalf("preparation retained stale image: %+v", prepared)
	}
	row, _ := sess.SQL().QueryRow("SELECT request_json FROM runtime_management_operations WHERE kind='scale_in'")
	var historical string
	if err = row.Scan(&historical); err != nil {
		t.Fatal(err)
	}
	var journal runtimePodScaleInJournal
	if err = json.Unmarshal([]byte(historical), &journal); err != nil {
		t.Fatal(err)
	}
	if journal.Snapshot.Option.ImageRef != old.Snapshot.Option.ImageRef {
		t.Fatal("rewrote historical sleep journal")
	}
	// Current image is frozen for this preparation. A subsequent external edit
	// requires another preparation rather than silently changing the reviewed scope.
	d.Spec.Template.Spec.Containers[0].Image = "repo/other:changed-after-prepare"
	d.Generation++
	_, _ = kube.AppsV1().Deployments("runtime").Update(ctx, d, metav1.UpdateOptions{})
	if err = s.Process(ctx); err != nil {
		t.Fatal(err)
	}
	loaded, _, _, err := s.runtimeManagementOperation(ctx, op.ID)
	if err != nil || loaded.Status != "needs_attention" {
		t.Fatalf("lost frozen scope: %+v %v", loaded, err)
	}
	current, _ := kube.AppsV1().Deployments("runtime").Get(ctx, d.Name, metav1.GetOptions{})
	if *current.Spec.Replicas != 0 {
		t.Fatal("woke changed preparation target")
	}
}

func completedRolloutSleepFixture(t *testing.T) (db.Session, *RuntimeManagementService, *fake.Clientset, runtimePodScaleInJournal, *corev1.Pod) {
	t.Helper()
	ctx := context.Background()
	sess, s, kube := scaleInMySQLFixture(t)
	original, _ := kube.CoreV1().Pods("runtime").Get(ctx, "hermes-runtime-pod", metav1.GetOptions{})
	pods, err := s.scheduler.RuntimeDeploymentPods(ctx, "hermes")
	if err != nil {
		t.Fatal(err)
	}
	control, _ := s.scaleInKube()
	// Historical fixture: simulate a completed pre-minimum-one operation on
	// fake Kubernetes. New production admission must never create this plan.
	j, err := s.idleRuntimeScaleInPlan(ctx, control, pods, map[string]bool{stringValue(pods[0].PodUID): true})
	if err != nil {
		t.Fatal(err)
	}
	const operationID = "historical-idle-sleep"
	j.PodCount = len(pods)
	j.Plan.Replicas, j.Protection.Replicas = 0, 0
	j.Plan.RemovedPods = len(pods)
	j.Plan.SourcePods = nil
	for _, p := range pods {
		j.Plan.SourcePods = append(j.Plan.SourcePods, RuntimePodScaleInSource{ID: p.ID, PodName: p.PodName, PodUID: stringValue(p.PodUID)})
	}
	j.Protection.SourcePods = append(j.Protection.SourcePods, j.Protection.RetainedPods...)
	j.Protection.RetainedPods = nil
	j.Protection.OperationID, j.Protection.AutomaticIdle = operationID, true
	j.Snapshot.IdleSleepOperation = operationID
	j.Protection.ExpectedSnapshot = j.Snapshot
	if err = control.ProtectRuntimeScaleIn(ctx, j.Protection); err != nil {
		t.Fatal(err)
	}
	if err = control.ResizeRuntimeScaleIn(ctx, j.Target, j.Snapshot, j.Protection); err != nil {
		t.Fatal(err)
	}
	for _, p := range pods {
		if err = kube.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), "runtime", p.PodName); err != nil {
			t.Fatal(err)
		}
	}
	d, _ := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
	d.Generation++
	d.Status.Replicas = 0
	d.Status.ReadyReplicas = 0
	d.Status.UpdatedReplicas = 0
	d.Status.AvailableReplicas = 0
	d.Status.ObservedGeneration = d.Generation
	_, _ = kube.AppsV1().Deployments("runtime").Update(ctx, d, metav1.UpdateOptions{})
	if err = control.ReleaseRuntimeScaleIn(ctx, j.Protection, false); err != nil {
		t.Fatal(err)
	}
	j.Phase = "finished"
	raw, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sess.SQL().Exec("INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES(?,'scale_in','completed',?,'[]',NOW(6),NOW(6))", operationID, string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err = sess.SQL().Exec("UPDATE runtime_pods SET scheduling_paused=1"); err != nil {
		t.Fatal(err)
	}
	return sess, s, kube, *j, original
}

func TestRuntimeRolloutPrepareAllSleepingAndResume(t *testing.T) {
	for _, changedImage := range []bool{false, true} {
		t.Run(fmt.Sprintf("changed-image=%v", changedImage), func(t *testing.T) {
			ctx := context.Background()
			sess, s, kube, j, original := completedRolloutSleepFixture(t)
			expectedImage := j.Snapshot.Option.ImageRef
			if changedImage {
				expectedImage = "repo/hermes@sha256:" + strings.Repeat("f", 64)
				sleeping, _ := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
				for i := range sleeping.Spec.Template.Spec.Containers {
					c := &sleeping.Spec.Template.Spec.Containers[i]
					c.Image = expectedImage
					for e := range c.Env {
						switch c.Env[e].Name {
						case "CLAWMANAGER_RUNTIME_IMAGE_REF":
							c.Env[e].Value = expectedImage
						case "CLAWMANAGER_RUNTIME_IMAGE_DIGEST":
							c.Env[e].Value = "sha256:" + strings.Repeat("f", 64)
						}
					}
				}
				sleeping.Generation += 3
				_, _ = kube.AppsV1().Deployments("runtime").Update(ctx, sleeping, metav1.UpdateOptions{})
				original.Spec = sleeping.Spec.Template.Spec
				for i := range original.Status.ContainerStatuses {
					original.Status.ContainerStatuses[i].ImageID = expectedImage
				}
			}
			before := len(kube.Actions())
			op, err := s.PrepareRollout(ctx, "hermes", nil)
			if err != nil {
				t.Fatal(err)
			}
			if op.Kind != "rollout_wake" || op.Status != "pending" || op.RuntimeType != "hermes" {
				t.Fatalf("%+v", op)
			}
			for _, a := range kube.Actions()[before:] {
				if a.GetVerb() != "get" && a.GetVerb() != "list" {
					t.Fatal("publication mutated Kubernetes")
				}
			}
			if err = s.Process(ctx); err != nil {
				t.Fatal(err)
			}
			d, _ := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
			if *d.Spec.Replicas != 1 {
				t.Fatal("wake was not submitted")
			}
			d.Generation++
			_, _ = kube.AppsV1().Deployments("runtime").Update(ctx, d, metav1.UpdateOptions{})
			// Fresh scheduler observes the same frozen operation, never selects a new pool.
			s = NewRuntimeManagementService(sess, s.scheduler, s.guards)
			if err = s.Process(ctx); err != nil {
				t.Fatal(err)
			}
			loaded, _, _, err := s.runtimeManagementOperation(ctx, op.ID)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.Status != "starting" {
				t.Fatalf("completed without native registration: %+v", loaded)
			}
			original.Name = "hermes-woken"
			original.UID = types.UID("woken-uid")
			original.ResourceVersion = ""
			original.Labels = d.Spec.Template.Labels
			_, err = kube.CoreV1().Pods("runtime").Create(ctx, original, metav1.CreateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			d.Status.Replicas = 1
			d.Status.UpdatedReplicas = 1
			d.Status.ReadyReplicas = 1
			d.Status.AvailableReplicas = 1
			d.Status.ObservedGeneration = d.Generation
			_, _ = kube.AppsV1().Deployments("runtime").Update(ctx, d, metav1.UpdateOptions{})
			if _, err = sess.SQL().Exec("UPDATE runtime_pods SET pod_ip='10.0.0.1',draining=0,pod_name=?,pod_uid=?,state='ready',last_seen_at=?,scheduling_paused=0,image_ref=?,image_digest=? WHERE id=1", original.Name, string(original.UID), time.Now().UTC(), expectedImage, imageDigestFromReference(expectedImage)); err != nil {
				t.Fatal(err)
			}
			if err = s.Process(ctx); err != nil {
				t.Fatal(err)
			}
			loaded, _, _, err = s.runtimeManagementOperation(ctx, op.ID)
			if err != nil || loaded.Status != "completed" {
				t.Fatalf("%+v %v", loaded, err)
			}
			if loaded.RuntimeType != "hermes" || loaded.Phase != "ready" {
				t.Fatalf("missing operation identity: %+v", loaded)
			}
			if err = s.Process(ctx); err != nil {
				t.Fatal(err)
			}
			d, _ = kube.AppsV1().Deployments("runtime").Get(ctx, d.Name, metav1.GetOptions{})
			if *d.Spec.Replicas != 1 || d.Spec.Template.Spec.Containers[0].Image != expectedImage {
				t.Fatal("preparation changed image or restored replicas externally")
			}

			if err = s.scaleInAdmission(ctx, sess); err == nil {
				t.Fatal("ready preparation released capacity before human confirmation")
			}

			if _, err = s.enqueue(ctx, "scheduling", RuntimeSchedulingRequest{}, nil, nil); err == nil {
				t.Fatal("scheduling bypassed ready upgrade reservation")
			}
			maintenance := &RuntimePodMaintenanceService{sess: sess}
			ran := false
			if err = maintenance.admissionTx(ctx, func(db.Session) error { ran = true; return nil }); err == nil || ran {
				t.Fatal("pool deletion bypassed ready upgrade reservation")
			}
			if err = maintenance.createAdmission(ctx, "hermes", func(db.Session) error { ran = true; return nil }); err == nil || ran {
				t.Fatal("new same-type pool changed reserved upgrade scope")
			}
			if err = s.scheduler.ValidateRolloutPreparation(ctx, op.ID, "hermes"); err != nil {
				t.Fatal(err)
			}
			rollouts := repository.NewRuntimeRolloutRepository(sess)
			rollout := &models.RuntimeRollout{RuntimeType: "hermes", TargetImageRef: "repo/new@sha256:" + strings.Repeat("b", 64), Status: "pending", Phase: "requested"}
			if err = rollouts.Create(ctx, rollout); err == nil {
				t.Fatal("rollout bypassed another ready preparation")
			}
			wrongType := repository.WithRuntimeRolloutPreparation(ctx, op.ID, "openclaw")
			if err = rollouts.Create(wrongType, rollout); err == nil {
				t.Fatal("preparation token accepted for a different runtime type")
			}
			admitted := repository.WithRuntimeRolloutPreparation(ctx, op.ID, "hermes")
			admitted = repository.WithRuntimeRolloutAdmissionValidation(admitted, func() error { return s.scheduler.ValidateRolloutPreparation(ctx, op.ID, "hermes") })

			if _, err = sess.SQL().Exec("INSERT INTO runtime_pod_deletion_intents(namespace,deployment_name,deployment_uid) VALUES('runtime',?,?)", d.Name, string(d.UID)); err != nil {
				t.Fatal(err)
			}
			if err = rollouts.Create(admitted, rollout); err == nil {
				t.Fatal("admitted rollout over pending native pool deletion")
			}
			if _, err = sess.SQL().Exec("DELETE FROM runtime_pod_deletion_intents"); err != nil {
				t.Fatal(err)
			}
			d.Labels["clawmanager.io/scheduling-enabled"] = "false"
			_, _ = kube.AppsV1().Deployments("runtime").Update(ctx, d, metav1.UpdateOptions{})
			if err = rollouts.Create(admitted, rollout); err == nil {
				t.Fatal("native scope changed between handler check and admission")
			}
			delete(d.Labels, "clawmanager.io/scheduling-enabled")
			_, _ = kube.AppsV1().Deployments("runtime").Update(ctx, d, metav1.UpdateOptions{})
			if err = rollouts.Create(admitted, rollout); err != nil {
				t.Fatal(err)
			}
			_, raw, _, err := s.runtimeManagementOperation(ctx, op.ID)
			if err != nil {
				t.Fatal(err)
			}
			var reserved rolloutWakeJournal
			if err = json.Unmarshal([]byte(raw), &reserved); err != nil {
				t.Fatal(err)
			}
			if reserved.ConsumedBy != rollout.ID {
				t.Fatal("preparation was not consumed in admission transaction")
			}
			if err = s.scheduler.ValidateRolloutPreparation(ctx, op.ID, "hermes"); err == nil {
				t.Fatal("consumed preparation reused")
			}
		})
	}
}

func TestRuntimeRolloutPrepareRejectsDriftAndActiveOperations(t *testing.T) {
	for _, scenario := range []string{"template", "foreign-operation", "opencode"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			sess, s, kube, _, _ := completedRolloutSleepFixture(t)
			if scenario == "template" {
				d, _ := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
				d.Spec.Template.Spec.Containers[0].Command = []string{"/foreign-command"}
				_, _ = kube.AppsV1().Deployments("runtime").Update(ctx, d, metav1.UpdateOptions{})
			}
			if scenario == "foreign-operation" {
				_, err := sess.SQL().Exec("INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES('held','scale_in','needs_attention','{}','[]',NOW(6),NOW(6))")
				if err != nil {
					t.Fatal(err)
				}
			}
			rt := "hermes"
			if scenario == "opencode" {
				rt = "opencode"
			}
			if _, err := s.PrepareRollout(ctx, rt, nil); err == nil {
				t.Fatal("unsafe preparation accepted")
			}
			d, _ := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
			if *d.Spec.Replicas != 0 {
				t.Fatal("rejected preparation woke the pool")
			}
		})
	}
}

func TestRuntimeRolloutPrepareCancelLeavesSafeCapacity(t *testing.T) {
	ctx := context.Background()
	_, s, kube, _, _ := completedRolloutSleepFixture(t)
	op, err := s.PrepareRollout(ctx, "hermes", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Cancel(ctx, op.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Process(ctx); err != nil {
		t.Fatal(err)
	}
	loaded, _, _, err := s.runtimeManagementOperation(ctx, op.ID)
	if err != nil || loaded.Status != "cancelled" {
		t.Fatal(loaded, err)
	}
	d, _ := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
	if *d.Spec.Replicas != 0 {
		t.Fatal("cancelled preparation mutated capacity")
	}
	if !strings.Contains(loaded.Kind, "wake") {
		t.Fatal("incorrect operation")
	}
}

func TestRuntimeRolloutPrepareRejectsPendingLivePoolDeletion(t *testing.T) {
	ctx := context.Background()
	sess, s, kube := scaleInMySQLFixture(t)
	d, err := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sess.SQL().Exec("INSERT INTO runtime_pod_deletion_intents(namespace,deployment_name,deployment_uid) VALUES('runtime',?,?)", d.Name, string(d.UID)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PrepareRollout(ctx, "hermes", nil); err == nil {
		t.Fatal("reserved a live pool whose native deletion is already admitted")
	}
	var count int
	row, _ := sess.SQL().QueryRow("SELECT COUNT(*) FROM runtime_management_operations WHERE kind='rollout_wake'")
	if err = row.Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}

func TestRuntimeRolloutPreparationPublicationFailureIsUncertain(t *testing.T) {
	ctx := context.Background()
	sess, s, _ := scaleInMySQLFixture(t)
	if _, err := sess.SQL().Exec("CREATE TRIGGER reject_prepare BEFORE INSERT ON runtime_management_operations FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected publication failure'"); err != nil {
		t.Fatal(err)
	}
	_, err := s.PrepareRollout(ctx, "hermes", nil)
	if !errors.Is(err, ErrRuntimeUpgradeCommitUncertain) {
		t.Fatalf("publication error was presented as a safe-to-retry conflict: %v", err)
	}
}

func TestRuntimeRolloutPreparationCancelPreservesProtectedFailure(t *testing.T) {
	ctx := context.Background()
	_, s, kube, _, _ := completedRolloutSleepFixture(t)
	op, err := s.PrepareRollout(ctx, "hermes", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Process(ctx); err != nil {
		t.Fatal(err)
	}
	d, _ := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
	d.Spec.Template.Spec.Containers[0].Image = "foreign"
	_, _ = kube.AppsV1().Deployments("runtime").Update(ctx, d, metav1.UpdateOptions{})
	if err = s.Process(ctx); err != nil {
		t.Fatal(err)
	}
	before, _, _, err := s.runtimeManagementOperation(ctx, op.ID)
	if err != nil || before.Status != "needs_attention" {
		t.Fatal(before, err)
	}
	if err = s.Cancel(ctx, op.ID); err == nil {
		t.Fatal("stale client cancellation cleared failed native proof protection")
	}
	after, _, _, err := s.runtimeManagementOperation(ctx, op.ID)
	if err != nil || after.Status != "needs_attention" {
		t.Fatal(after, err)
	}
}

type mixedRolloutInventory struct {
	k8s.RuntimeDeploymentService
	k8s.RuntimeRolloutSleepService
	k8s.RuntimeIdlePoolDeploymentService
	inventory k8s.RuntimeRolloutInventory
}

func (d mixedRolloutInventory) RolloutTargets(ctx context.Context, ns, rt string) ([]k8s.RuntimeRolloutTarget, error) {
	live, err := d.inventory.RolloutTargets(ctx, ns, rt)
	return append(live, k8s.RuntimeRolloutTarget{Namespace: ns, Name: "already-live", UID: "live-uid", Image: "registry/live@sha256:" + strings.Repeat("a", 64), Ready: true}), err
}
func TestRuntimeRolloutPreparationFreezesMixedPoolScope(t *testing.T) {
	for _, deleting := range []bool{false, true} {
		t.Run(fmt.Sprint(deleting), func(t *testing.T) {
			ctx := context.Background()
			sess, s, _, _, _ := completedRolloutSleepFixture(t)
			original := s.scheduler.deployments
			s.scheduler.deployments = mixedRolloutInventory{RuntimeDeploymentService: original, RuntimeRolloutSleepService: original.(k8s.RuntimeRolloutSleepService), RuntimeIdlePoolDeploymentService: original.(k8s.RuntimeIdlePoolDeploymentService), inventory: original.(k8s.RuntimeRolloutInventory)}
			if deleting {
				if _, err := sess.SQL().Exec("INSERT INTO runtime_pod_deletion_intents(namespace,deployment_name,deployment_uid) VALUES('runtime','already-live','live-uid')"); err != nil {
					t.Fatal(err)
				}
			}
			op, err := s.PrepareRollout(ctx, "hermes", nil)
			if deleting {
				if err == nil {
					t.Fatal("mixed preparation ignored the live pool deletion intent")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			_, raw, _, err := s.runtimeManagementOperation(ctx, op.ID)
			if err != nil {
				t.Fatal(err)
			}
			var j rolloutWakeJournal
			if err = json.Unmarshal([]byte(raw), &j); err != nil {
				t.Fatal(err)
			}
			if len(j.Scope) != 2 || len(j.Pools) != 1 {
				t.Fatalf("mixed scope was not frozen: %+v", j)
			}
		})
	}
}
