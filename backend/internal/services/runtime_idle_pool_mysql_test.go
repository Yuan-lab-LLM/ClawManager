package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"clawreef/internal/backupgate"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type stalledIdlePoolPlacement struct {
	*fakeRuntimeInstanceRepo
	entered chan struct{}
}

func (r *stalledIdlePoolPlacement) GetV2Creating(ctx context.Context, _ int) ([]models.Instance, error) {
	close(r.entered)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestRuntimeIdlePoolMySQLScaleInWhilePlacementBlocked(t *testing.T) {
	sess, service, _ := scaleInMySQLFixture(t)
	enableRuntimeIdlePoolTest(t, service)
	creating := &stalledIdlePoolPlacement{fakeRuntimeInstanceRepo: newFakeRuntimeInstanceRepo(), entered: make(chan struct{})}
	scheduler := service.scheduler
	scheduler.instanceRepo = creating
	now := time.Now()
	service.lastIdlePoolScan = now.Add(-15 * time.Second)
	service.idlePodSince = map[string]time.Time{
		"hermes-runtime-pod-uid":   now.Add(-runtimeIdlePodGrace),
		"hermes-runtime-pod-2-uid": now.Add(-runtimeIdlePodGrace),
	}

	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	var admission sync.Mutex
	var work sync.WaitGroup
	restore := backupgate.ConfigureWorkBoundary(func(ctx context.Context) (func(), error) {
		admission.Lock()
		defer admission.Unlock()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		work.Add(1)
		return work.Done, nil
	})
	defer func() {
		admission.Lock()
		cancel()
		admission.Unlock()
		work.Wait() // Drain scheduler workers before the fixture closes its SQL session.
		restore()
	}()
	scheduler.Start(ctx)
	select {
	case <-creating.entered:
	case <-ctx.Done():
		t.Fatal("placement did not enter the blocked scan")
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var queued int
		row, err := sess.SQL().QueryRow("SELECT COUNT(*) FROM runtime_management_operations WHERE kind='scale_in'")
		if err == nil {
			err = row.Scan(&queued)
		}
		if err != nil {
			t.Fatal(err)
		}
		if queued == 1 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("a blocked placement scan prevented scale-in of a verified empty Pod")
		case <-ticker.C:
		}
	}
}

func TestRuntimeIdlePoolMySQLRemovesUnscheduledReplicaWithEmptySibling(t *testing.T) {
	for _, scenario := range []string{"idle", "fresh pending", "more pending", "new demand", "scheduled", "late registration", "retained Agent stale"} {
		t.Run(scenario, func(t *testing.T) {
			testRuntimeIdlePoolUnscheduledReplica(t, scenario)
		})
	}
}

func testRuntimeIdlePoolUnscheduledReplica(t *testing.T, scenario string) {
	t.Helper()
	ctx := context.Background()
	sess, service, client := scaleInMySQLFixture(t)
	deployment := client.AppsV1().Deployments("runtime")
	d, err := deployment.Get(ctx, "hermes-runtime", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	before, after, readyAfter, sourceCount, registeredCount := int32(3), int32(1), int32(1), 2, 1
	if scenario == "more pending" {
		before, after, readyAfter, sourceCount, registeredCount = 13, 3, 2, 10, 0
	}
	*d.Spec.Replicas, d.Status.Replicas, d.Status.UpdatedReplicas = before, before, before
	d.Annotations = map[string]string{"clawmanager.io/runtime-expansion": `{"previous":2,"replicas":3}`}
	if _, err = deployment.Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	pods := client.CoreV1().Pods("runtime")
	p, err := pods.Get(ctx, "hermes-runtime-pod", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p.Name, p.UID, p.Spec.NodeName = "hermes-pending", "hermes-pending-uid", ""
	p.Status = corev1.PodStatus{Phase: corev1.PodPending, Conditions: []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable}}}
	if scenario != "fresh pending" {
		if _, err = pods.Create(ctx, p, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	for i := int32(0); i < before-3; i++ {
		extra := p.DeepCopy()
		extra.Name = fmt.Sprintf("hermes-pending-%02d", i)
		extra.UID = types.UID(extra.Name + "-uid")
		if _, err = pods.Create(ctx, extra, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
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
	for tick := 0; tick <= 6; tick++ {
		if scenario == "fresh pending" && tick == 6 {
			if _, err = pods.Create(ctx, p, metav1.CreateOptions{}); err != nil {
				t.Fatal(err)
			}
		}
		if err = service.reconcileIdleRuntimePools(ctx, start.Add(time.Duration(tick)*10*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	var raw string
	row, err := sess.SQL().QueryRow("SELECT request_json FROM runtime_management_operations")
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var journal runtimePodScaleInJournal
	if err = json.Unmarshal([]byte(raw), &journal); err != nil {
		t.Fatal(err)
	}
	if len(journal.Protection.SourcePods) != sourceCount || !journal.Protection.SourcePods[0].Unscheduled || journal.Protection.SourcePods[0].PodUID != string(p.UID) || len(journal.Plan.SourcePods) != registeredCount {
		t.Fatalf("unexpected replica selection: %+v", journal.Protection.SourcePods)
	}
	switch scenario {
	case "new demand":
		seedRuntimeScaleDemand(t, sess, 1)
	case "scheduled":
		p.Spec.NodeName = "worker"
		if _, err = pods.Update(ctx, p, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
	case "late registration":
		if _, err = sess.SQL().Exec(`INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,state,capacity,used_slots)
			VALUES(3,'hermes','runtime',?,?,'hermes-runtime','image','ready',8,0)`, p.Name, string(p.UID)); err != nil {
			t.Fatal(err)
		}
		if claimed, claimErr := service.scheduler.podRepo.TryClaimSlot(ctx, 3); claimErr == nil || claimed {
			t.Fatalf("late registered source was allowed to claim a Slot: claimed=%v err=%v", claimed, claimErr)
		}
	case "retained Agent stale":
		if _, err = sess.SQL().Exec("UPDATE runtime_pods SET last_seen_at=? WHERE pod_uid=?", start.Add(-5*time.Minute), journal.Protection.RetainedPods[0].PodUID); err != nil {
			t.Fatal(err)
		}
	}
	if err = service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	d, err = deployment.Get(ctx, d.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if scenario != "idle" && scenario != "fresh pending" && scenario != "more pending" {
		if *d.Spec.Replicas != 3 || d.Labels["clawmanager.io/scale-in-operation"] != "" {
			t.Fatalf("%s must cancel scale-in and release protection: replicas=%d, labels=%v", scenario, *d.Spec.Replicas, d.Labels)
		}
		return
	}
	if *d.Spec.Replicas != after {
		t.Fatalf("replicas=%d, want %d with unscheduled and idle Pods selected", *d.Spec.Replicas, after)
	}
	if d.Annotations["clawmanager.io/runtime-expansion"] != "" {
		t.Fatal("superseded expansion remained active")
	}
	for _, source := range journal.Protection.SourcePods {
		if err = client.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), "runtime", source.PodName); err != nil {
			t.Fatal(err)
		}
	}
	d.Status.Replicas, d.Status.ReadyReplicas, d.Status.UpdatedReplicas = after, readyAfter, after
	d.Generation++
	d.Status.ObservedGeneration = d.Generation
	if _, err = deployment.Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err = service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	row, err = sess.SQL().QueryRow("SELECT status FROM runtime_management_operations")
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&status); err != nil || status != "completed" {
		t.Fatalf("mixed scale-in did not complete: status=%q err=%v", status, err)
	}
}

func enableRuntimeIdlePoolTest(t *testing.T, service *RuntimeManagementService) {
	t.Helper()
	sess := service.sess
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
}

func TestRuntimeIdlePoolMySQLGraceAndMinimumOne(t *testing.T) {
	ctx := context.Background()
	sess, service, kube := scaleInMySQLFixture(t)
	enableRuntimeIdlePoolTest(t, service)
	start := time.Now().UTC()
	if err := service.reconcileIdleRuntimePools(ctx, start); err != nil {
		t.Fatal(err)
	}
	var count int
	row, _ := sess.SQL().QueryRow("SELECT COUNT(*) FROM runtime_management_operations")
	_ = row.Scan(&count)
	if count != 0 {
		t.Fatal("removed capacity before full empty grace")
	}
	for n := 1; n <= 6; n++ {
		if err := service.reconcileIdleRuntimePools(ctx, start.Add(time.Duration(n)*10*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	row, _ = sess.SQL().QueryRow("SELECT COUNT(*) FROM runtime_management_operations")
	_ = row.Scan(&count)
	if count != 1 {
		t.Fatal("stable empty pool did not create one durable scale-in")
	}
	if err := service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	d, _ := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
	if *d.Spec.Replicas != 1 {
		t.Fatal("automatic reclamation must retain one Pod")
	}
	// Complete the actual selected deletion, preserving the retained sibling.
	var raw string
	row, _ = sess.SQL().QueryRow("SELECT request_json FROM runtime_management_operations")
	if err := row.Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var j runtimePodScaleInJournal
	if err := json.Unmarshal([]byte(raw), &j); err != nil {
		t.Fatal(err)
	}
	if len(j.Plan.SourcePods) != 1 || len(j.Protection.RetainedPods) != 1 {
		t.Fatal("automatic reclamation selected the last Pod")
	}
	if err := kube.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), "runtime", j.Plan.SourcePods[0].PodName); err != nil {
		t.Fatal(err)
	}
	d.Status.Replicas, d.Status.ReadyReplicas, d.Status.UpdatedReplicas = 1, 1, 1
	d.Generation++
	d.Status.ObservedGeneration = d.Generation
	if _, err := kube.AppsV1().Deployments("runtime").Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	row, _ = sess.SQL().QueryRow("SELECT request_json FROM runtime_management_operations")
	if err := row.Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(raw), &j); err != nil || j.Phase != "finished" {
		t.Fatalf("reclamation did not complete: phase=%s err=%v", j.Phase, err)
	}
	// The retained Pod is not removed by a later idle scan either.
	for n := 7; n <= 14; n++ {
		if err := service.reconcileIdleRuntimePools(ctx, start.Add(time.Duration(n)*10*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	row, _ = sess.SQL().QueryRow("SELECT COUNT(*) FROM runtime_management_operations")
	if err := row.Scan(&count); err != nil || count != 1 {
		t.Fatalf("last Pod was queued for removal: operations=%d err=%v", count, err)
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
