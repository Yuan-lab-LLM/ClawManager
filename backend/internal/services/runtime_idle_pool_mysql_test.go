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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRuntimeIdlePoolMySQLGraceAndMinimumOne(t *testing.T) {
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
	if *d.Spec.Replicas != 1 {
		t.Fatal("automatic reclamation must retain one Pod")
	}
	// Complete the actual selected deletion, preserving the retained sibling.
	var raw string
	row, _ = sess.SQL().QueryRow("SELECT request_json FROM runtime_management_operations")
	if err = row.Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var j runtimePodScaleInJournal
	if err = json.Unmarshal([]byte(raw), &j); err != nil {
		t.Fatal(err)
	}
	if len(j.Plan.SourcePods) != 1 || len(j.Protection.RetainedPods) != 1 {
		t.Fatal("automatic reclamation selected the last Pod")
	}
	if err = kube.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), "runtime", j.Plan.SourcePods[0].PodName); err != nil {
		t.Fatal(err)
	}
	d.Status.Replicas, d.Status.ReadyReplicas, d.Status.UpdatedReplicas = 1, 1, 1
	d.Generation++
	d.Status.ObservedGeneration = d.Generation
	if _, err = kube.AppsV1().Deployments("runtime").Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err = service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	row, _ = sess.SQL().QueryRow("SELECT request_json FROM runtime_management_operations")
	if err = row.Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(raw), &j); err != nil || j.Phase != "finished" {
		t.Fatalf("reclamation did not complete: phase=%s err=%v", j.Phase, err)
	}
	// The retained Pod is not removed by a later idle scan either.
	for n := 7; n <= 14; n++ {
		if err = service.reconcileIdleRuntimePools(ctx, start.Add(time.Duration(n)*10*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	row, _ = sess.SQL().QueryRow("SELECT COUNT(*) FROM runtime_management_operations")
	if err = row.Scan(&count); err != nil || count != 1 {
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
