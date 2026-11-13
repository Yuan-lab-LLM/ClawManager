package services

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"clawreef/internal/repository"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBasicSchedulingIdleRetainsOnePodMySQL(t *testing.T) {
	sess, s, _ := scaleInMySQLFixture(t)
	data, err := os.ReadFile("../db/migrations/097_add_instance_auto_stop_scan.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range strings.Split(string(data), ";") {
		if strings.TrimSpace(query) != "" {
			if _, err = sess.SQL().Exec(query); err != nil {
				t.Fatal(err)
			}
		}
	}
	s.scheduler.autoStopRepo = repository.NewInstanceAutoStopRepository(sess)
	start := time.Now().UTC()
	for tick := 0; tick <= 6; tick++ {
		if err = s.reconcileIdleRuntimePools(t.Context(), start.Add(time.Duration(tick)*10*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	row, err := sess.SQL().QueryRow("SELECT request_json FROM runtime_management_operations WHERE kind='scale_in'")
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	if err = row.Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var j runtimePodScaleInJournal
	if err = json.Unmarshal([]byte(raw), &j); err != nil {
		t.Fatal(err)
	}
	if j.Plan.Replicas != 1 || len(j.Plan.SourcePods) != 1 || len(j.Protection.RetainedPods) != 1 {
		t.Fatalf("automatic reclamation must retain one Pod: %+v", j.Plan)
	}
}

func TestBasicSchedulingRejectsAutomaticZeroMySQL(t *testing.T) {
	for _, stage := range []string{"plan", "submit", "worker"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			sess, s, kube := scaleInMySQLFixture(t)
			pods, err := s.scheduler.RuntimeDeploymentPods(ctx, "hermes")
			if err != nil {
				t.Fatal(err)
			}
			api, _ := s.scaleInKube()
			selected := map[string]bool{stringValue(pods[0].PodUID): true}
			if stage == "plan" {
				selected[stringValue(pods[1].PodUID)] = true
				if _, err = s.idleRuntimeScaleInPlan(ctx, api, pods, selected); err == nil {
					t.Fatal("automatic plan can remove the last Pod")
				}
				return
			}
			j, err := s.idleRuntimeScaleInPlan(ctx, api, pods, selected)
			if err != nil {
				t.Fatal(err)
			}
			if stage == "submit" {
				j.Plan.Replicas, j.Protection.Replicas = 0, 0
				if _, err = s.submitScaleInJournal(ctx, j, nil); err == nil {
					t.Fatal("automatic zero plan was published")
				}
				return
			}
			op, err := s.submitScaleInJournal(ctx, j, nil)
			if err != nil {
				t.Fatal(err)
			}
			j.Plan.Replicas, j.Protection.Replicas = 0, 0
			raw, _ := json.Marshal(j)
			if _, err = sess.SQL().Exec("UPDATE runtime_management_operations SET request_json=? WHERE id=?", string(raw), op.ID); err != nil {
				t.Fatal(err)
			}
			// Simulate an old queued operation surviving an application update.
			_ = s.Process(ctx)
			row, err := sess.SQL().QueryRow("SELECT request_json FROM runtime_management_operations WHERE id=?", op.ID)
			if err != nil {
				t.Fatal(err)
			}
			var updated string
			if err = row.Scan(&updated); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(updated, "retain at least one") {
				t.Fatalf("old automatic zero operation was not rejected by the capacity floor: %s", updated)
			}
			d, err := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if *d.Spec.Replicas != 2 {
				t.Fatalf("legacy zero plan changed replicas to %d", *d.Spec.Replicas)
			}
		})
	}
}
