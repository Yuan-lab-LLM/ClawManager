package services

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRuntimeIdlePoolMySQLGraceZeroAndColdStart(t *testing.T) {
	ctx := context.Background()
	sess, service, kube := scaleInMySQLFixture(t)
	data, err := os.ReadFile("../db/migrations/097_add_instance_auto_stop_scan.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range strings.Split(string(data), ";") {
		if strings.TrimSpace(q) != "" {
			if _, err = sess.SQL().Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	}
	service.scheduler.autoStopRepo = repository.NewInstanceAutoStopRepository(sess)
	start := time.Now().UTC()
	if err = service.reconcileIdleRuntimePools(ctx, start); err != nil {
		t.Fatal(err)
	}
	var count int
	row, _ := sess.SQL().QueryRow("SELECT COUNT(*) FROM runtime_management_operations")
	_ = row.Scan(&count)
	if count != 0 {
		t.Fatal("removed capacity before full empty grace")
	}
	for n := 1; n <= 6; n++ {
		if err = service.reconcileIdleRuntimePools(ctx, start.Add(time.Duration(n)*10*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	row, _ = sess.SQL().QueryRow("SELECT COUNT(*) FROM runtime_management_operations")
	_ = row.Scan(&count)
	if count != 1 {
		t.Fatal("stable empty pool did not create one durable scale-in")
	}
	if err = service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	d, _ := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
	if *d.Spec.Replicas != 0 {
		t.Fatal("automatic removal did not scale empty pool to zero")
	}
	// Simulate the real Deployment controller's confirmed graceful deletion.
	for _, name := range []string{"hermes-runtime-pod", "hermes-runtime-pod-2"} {
		if err = kube.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), "runtime", name); err != nil {
			t.Fatal(err)
		}
	}
	d.Status.Replicas, d.Status.ReadyReplicas, d.Status.UpdatedReplicas = 0, 0, 0
	d.Generation++
	d.Status.ObservedGeneration = d.Generation
	_, _ = kube.AppsV1().Deployments("runtime").Update(ctx, d, metav1.UpdateOptions{})
	if err = service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	var raw string
	row, _ = sess.SQL().QueryRow("SELECT request_json FROM runtime_management_operations")
	_ = row.Scan(&raw)
	var j runtimePodScaleInJournal
	if err = json.Unmarshal([]byte(raw), &j); err != nil {
		t.Fatal(err)
	}
	if !j.AutomaticIdle || j.Phase != "finished" {
		t.Fatalf("unfinished sleep proof: %s", j.Phase)
	}
	// A manual stop persists intent before contacting Kubernetes. Its receipt
	// can recover the original storage from this completed removal journal.
	if _, err = sess.SQL().Exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES(101,1,'hermes','gateway','lite','stopped',1,NOW(6))"); err != nil {
		t.Fatal(err)
	}
	source := j.Plan.SourcePods[0]
	if _, err = sess.SQL().Exec(`INSERT INTO instance_idle_stop_operations(id,instance_id,user_id,generation,runtime_pod_id,gateway_id,pod_uid,endpoint,workspace_path,storage_identity,phase,stop_kind,created_at,updated_at) VALUES('manual-receipt',101,1,1,?,'gw',?,'https://agent','/workspace','','completed','manual',NOW(6),NOW(6))`, source.ID, source.PodUID); err != nil {
		t.Fatal(err)
	}
	storage, err := service.scheduler.autoStopRepo.PreviousIdleStorage(ctx, 101)
	if err != nil || storage == "" || storage != j.Snapshot.WorkspaceStorageIdentity {
		t.Fatalf("manual stop lost its original persistent backing: %q %v", storage, err)
	}

	if _, err = sess.SQL().Exec(`INSERT INTO instance_idle_stop_operations(id,instance_id,user_id,generation,runtime_pod_id,gateway_id,pod_uid,endpoint,workspace_path,storage_identity,phase,stop_kind,created_at,updated_at) VALUES('unbound-manual-receipt',101,1,2,0,'','','','/workspace','','completed','manual',NOW(6),NOW(6))`); err != nil {
		t.Fatal(err)
	}
	storage, err = service.scheduler.autoStopRepo.PreviousIdleStorage(ctx, 101)
	if err != nil || storage != j.Snapshot.WorkspaceStorageIdentity {
		t.Fatalf("unbound Stop erased original backing: %q %v", storage, err)
	}
	woke, err := service.scheduler.restoreIdleRuntimeCapacity(ctx, models.Instance{ID: 1}, "hermes")
	if err != nil || !woke {
		t.Fatalf("cold start did not restore sleep capacity: %v %v", woke, err)
	}
	d, _ = kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
	if *d.Spec.Replicas != 1 {
		t.Fatal("cold starts must restore only one Pod")
	}
}

func TestRuntimeIdlePoolMySQLUnsettledClaimsRetainCapacity(t *testing.T) {
	ctx := context.Background()
	sess, service, _ := scaleInMySQLFixture(t)
	pods, err := service.scheduler.RuntimeDeploymentPods(ctx, "hermes")
	if err != nil {
		t.Fatal(err)
	}
	selected := map[string]bool{stringValue(pods[0].PodUID): true}
	kube, _ := service.scaleInKube()
	j, err := service.idleRuntimeScaleInPlan(ctx, kube, pods, selected)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sess.SQL().Exec("UPDATE runtime_pods SET used_slots=1 WHERE id=?", j.Plan.SourcePods[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.submitScaleInJournal(ctx, j, nil); err == nil {
		t.Fatal("accepted a new unpublished slot claim")
	}
}

func TestRuntimeIdlePoolMySQLRetainsBusySibling(t *testing.T) {
	ctx := context.Background()
	sess, service, kube := scaleInMySQLFixture(t)
	if _, err := sess.SQL().Exec("UPDATE runtime_pods SET used_slots=1 WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	pods, err := service.scheduler.RuntimeDeploymentPods(ctx, "hermes")
	if err != nil {
		t.Fatal(err)
	}
	var source models.RuntimePod
	for _, p := range pods {
		if p.ID == 1 {
			source = p
		}
	}
	api, _ := service.scaleInKube()
	j, err := service.idleRuntimeScaleInPlan(ctx, api, pods, map[string]bool{stringValue(source.PodUID): true})
	if err != nil {
		t.Fatal(err)
	}
	op, err := service.submitScaleInJournal(ctx, j, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	d, _ := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
	if *d.Spec.Replicas != 1 {
		t.Fatal("empty Pod was not removed from partially occupied pool")
	}
	retained, _ := kube.CoreV1().Pods("runtime").Get(ctx, "hermes-runtime-pod-2", metav1.GetOptions{})
	if retained.Labels["clawmanager.io/scale-in-protected"] != op.ID {
		t.Fatal("busy sibling lacks native deletion protection")
	}
}

func TestRuntimeIdlePoolMySQLManualZeroHasNoWakeAuthority(t *testing.T) {
	ctx := context.Background()
	_, service, kube := scaleInMySQLFixture(t)
	d, _ := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
	zero := int32(0)
	d.Spec.Replicas = &zero
	_, _ = kube.AppsV1().Deployments("runtime").Update(ctx, d, metav1.UpdateOptions{})
	woke, err := service.scheduler.restoreIdleRuntimeCapacity(ctx, models.Instance{ID: 1}, "hermes")
	if err != nil || woke {
		t.Fatalf("manually disabled pool gained wake authority: %v %v", woke, err)
	}
}

type idleWakeRegistrationClient struct {
	k8s.RuntimeDeploymentService
	calls []string
}

func (c *idleWakeRegistrationClient) IdleRuntimePoolOperations(context.Context, string, string) ([]string, error) {
	return []string{"wake-first", "wake-second"}, nil
}
func (c *idleWakeRegistrationClient) WakeIdleRuntimePool(_ context.Context, target k8s.RuntimePodScaleTarget, _ *k8s.RuntimePodScaleSnapshot) error {
	c.calls = append(c.calls, target.DeploymentName)
	if target.DeploymentName == "hermes-runtime" {
		return k8s.ErrRuntimeIdlePoolAlreadyActive
	}
	return nil
}
func TestRuntimeIdlePoolReadyBeforeRegistrationDoesNotWakeSecondPool(t *testing.T) {
	ctx := context.Background()
	sess, service, _ := scaleInMySQLFixture(t)
	client := &idleWakeRegistrationClient{RuntimeDeploymentService: service.scheduler.deployments}
	service.scheduler.deployments = client
	if _, err := sess.SQL().Exec("UPDATE runtime_pods SET state='unhealthy'"); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"hermes-runtime", "zzz-hermes"} {
		j := runtimePodScaleInJournal{AutomaticIdle: true, Phase: "finished", RuntimePodScaleInRequest: RuntimePodScaleInRequest{RuntimeType: "hermes"}, Target: k8s.RuntimePodScaleTarget{Namespace: "runtime", DeploymentName: name}, Snapshot: &k8s.RuntimePodScaleSnapshot{}}
		raw, _ := json.Marshal(j)
		id := []string{"wake-first", "wake-second"}[i]
		if _, err := sess.SQL().Exec("INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES(?,'scale_in','completed',?,'[]',NOW(6),NOW(6))", id, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := service.scheduler.restoreIdleRuntimeCapacity(ctx, models.Instance{ID: 25}, "hermes")
	if err != nil || !pending {
		t.Fatalf("expected to wait for the first pool registration: %v %v", pending, err)
	}
	if len(client.calls) != 1 || client.calls[0] != "hermes-runtime" {
		t.Fatalf("woke a second pool while the first is registering: %v", client.calls)
	}
}
