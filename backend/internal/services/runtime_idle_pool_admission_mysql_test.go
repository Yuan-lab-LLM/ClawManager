package services

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"clawreef/internal/repository"
)

func TestRuntimeIdlePoolMySQLScopesPendingDemand(t *testing.T) {
	for _, scenario := range []struct {
		name, instanceType, status, stopUID string
		stopPod                             int
		blocked                             bool
	}{
		{"other agent creating", "openclaw", "creating", "", 0, false},
		{"same agent creating", "hermes", "creating", "", 0, true},
		{"same agent unbound running", "hermes", "running", "", 0, true},
		{"other pool stop", "hermes", "stopped", "other-pod-uid", 999, false},
		{"current source stop", "hermes", "stopped", "hermes-runtime-pod-uid", 1, true},
		{"replaced source stop", "hermes", "stopped", "previous-pod-uid", 1, false},
		{"source stop missing UID", "hermes", "stopped", "", 1, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			sess, service, _ := scaleInMySQLFixture(t)
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
			service.scheduler.autoStopRepo = repository.NewInstanceAutoStopRepository(sess)
			if _, err = sess.SQL().Exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES(901,1,?,'gateway','lite',?,1,NOW(6))", scenario.instanceType, scenario.status); err != nil {
				t.Fatal(err)
			}
			if scenario.stopPod != 0 {
				if _, err = sess.SQL().Exec("INSERT INTO instance_idle_stop_operations(id,instance_id,user_id,generation,runtime_pod_id,gateway_id,pod_uid,endpoint,workspace_path,storage_identity,phase,stop_kind,created_at,updated_at) VALUES('old-stop',901,1,1,?,'gw-901-1',?,'https://agent','/workspace','','error','manual',NOW(6),NOW(6))", scenario.stopPod, scenario.stopUID); err != nil {
					t.Fatal(err)
				}
			}
			start := time.Now().UTC()
			for tick := 0; tick <= 6; tick++ {
				if err = service.reconcileIdleRuntimePools(context.Background(), start.Add(time.Duration(tick)*10*time.Second)); err != nil {
					t.Fatal(err)
				}
			}
			var count int
			row, err := sess.SQL().QueryRow("SELECT COUNT(*) FROM runtime_management_operations WHERE kind='scale_in'")
			if err != nil {
				t.Fatal(err)
			}
			if err = row.Scan(&count); err != nil {
				t.Fatal(err)
			}
			if (count == 0) != scenario.blocked {
				t.Fatalf("queued=%d, blocked=%v; unrelated pending work must not prevent empty pool reclamation", count, scenario.blocked)
			}
		})
	}
}

func TestRuntimeIdlePoolMySQLRechecksDemandBeforePublishing(t *testing.T) {
	for _, scenario := range []string{"start", "stop"} {
		t.Run(scenario, func(t *testing.T) {
			sess, service, _ := scaleInMySQLFixture(t)
			ctx := context.Background()
			pods, err := service.scheduler.RuntimeDeploymentPods(ctx, "hermes")
			if err != nil {
				t.Fatal(err)
			}
			kube, err := service.scaleInKube()
			if err != nil {
				t.Fatal(err)
			}
			journal, err := service.idleRuntimeScaleInPlan(ctx, kube, pods, map[string]bool{stringValue(pods[0].PodUID): true})
			if err != nil {
				t.Fatal(err)
			}
			status := "creating"
			if scenario == "stop" {
				status = "stopped"
			}
			if _, err = sess.SQL().Exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES(902,1,'hermes','gateway','lite',?,1,NOW(6))", status); err != nil {
				t.Fatal(err)
			}
			if scenario == "stop" {
				if _, err = sess.SQL().Exec("INSERT INTO instance_idle_stop_operations(id,instance_id,user_id,generation,runtime_pod_id,gateway_id,pod_uid,endpoint,workspace_path,storage_identity,phase,stop_kind,created_at,updated_at) VALUES('late-stop',902,1,1,?,'gw-902-1',?,'https://agent','/workspace','','error','manual',NOW(6),NOW(6))", pods[0].ID, stringValue(pods[0].PodUID)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = service.submitScaleInJournal(ctx, journal, nil); err == nil || !strings.Contains(err.Error(), "instance start or unconfirmed stop exists") {
				t.Fatalf("related demand was not rechecked before publishing: %v", err)
			}
			pod, err := service.scheduler.podRepo.GetByID(ctx, pods[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			if pod.SchedulingPaused {
				t.Fatal("denied idle reclamation changed source admission")
			}
		})
	}
}
