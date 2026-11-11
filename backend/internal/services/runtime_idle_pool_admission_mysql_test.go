package services

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"encoding/json"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
)

func TestRuntimeIdlePoolMySQLScopesPendingDemand(t *testing.T) {
	for _, scenario := range []struct {
		name, instanceType, status, stopUID string
		stopPod                             int
		blocked                             bool
	}{
		{"other agent creating", "openclaw", "creating", "", 0, false},
		{"same agent creating", "hermes", "creating", "", 0, false},
		{"same agent unbound running", "hermes", "running", "", 0, false},
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
	for _, scenario := range []string{"reservation", "stop"} {
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
			if scenario == "reservation" {
				if _, err = sess.SQL().Exec("INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,state,generation,workspace_path) VALUES(902,?,'hermes','pending-902-1',20000,'creating',1,'/workspace')", pods[0].ID); err != nil {
					t.Fatal(err)
				}
				if _, err = sess.SQL().Exec("UPDATE runtime_pods SET used_slots=used_slots+1 WHERE id=?", pods[0].ID); err != nil {
					t.Fatal(err)
				}
			}
			_, err = service.submitScaleInJournal(ctx, journal, nil)
			expected := "source occupancy or identity changed"
			if scenario == "stop" {
				expected = "unconfirmed stop"
			}
			if err == nil || !strings.Contains(err.Error(), expected) {
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

func TestRuntimeIdlePoolMySQLRechecksLatestObservation(t *testing.T) {
	for _, stage := range []string{"publication", "execution", "final replica write"} {
		for _, state := range []string{"occupied", "unknown", "stale"} {
			t.Run(stage+"/"+state, func(t *testing.T) {
				sess, s, kube := scaleInMySQLFixture(t)
				ctx := context.Background()
				pods, err := s.scheduler.RuntimeDeploymentPods(ctx, "hermes")
				if err != nil {
					t.Fatal(err)
				}
				control, err := s.scaleInKube()
				if err != nil {
					t.Fatal(err)
				}
				journal, err := s.idleRuntimeScaleInPlan(ctx, control, pods, map[string]bool{stringValue(pods[0].PodUID): true})
				if err != nil {
					t.Fatal(err)
				}
				changed := false
				change := func() {
					if changed {
						return
					}
					changed = true
					p, err := s.scheduler.podRepo.GetByID(ctx, pods[0].ID)
					if err != nil {
						t.Fatal(err)
					}
					var o models.RuntimePodObservation
					if err = json.Unmarshal([]byte(*p.ObservationJSON), &o); err != nil {
						t.Fatal(err)
					}
					if state == "stale" {
						o.ObservedAt = time.Now().Add(-3 * time.Minute).UTC()
					} else {
						o.Slots[0].State = state
						o.Slots[0].Error = "new Runtime state"
					}
					if state == "occupied" {
						o.Slots[0].InstanceID = 25
						o.Slots[0].UserID = 1
						o.Slots[0].GatewayID = "gw-25-14"
						o.Slots[0].Generation = 14
						o.Slots[0].GatewayState = "error"
					}
					data, err := json.Marshal(o)
					if err != nil {
						t.Fatal(err)
					}
					if _, err = sess.SQL().Exec("UPDATE runtime_pods SET observation_json=?,observation_at=? WHERE id=?", string(data), o.ObservedAt, p.ID); err != nil {
						t.Fatal(err)
					}
				}
				if stage == "publication" {
					change()
				}
				op, err := s.submitScaleInJournal(ctx, journal, nil)
				if stage == "publication" {
					if err == nil {
						t.Fatal("published reclamation using superseded empty facts")
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					if stage == "execution" {
						change()
						physicalState := state
						if state == "stale" {
							physicalState = "unknown"
						}
						s.scheduler.agentClient = scaleInPhysicalAgent(t, kube, physicalState)
					} else {
						// The final assignment read follows the ordinary SQL empty check.
						// Inject new facts here; only the locked final guard can reject them.
						kube.PrependReactor("list", "configmaps", func(a ktesting.Action) (bool, runtime.Object, error) {
							var raw string
							row, e := sess.SQL().QueryRow("SELECT request_json FROM runtime_management_operations WHERE id=?", op.ID)
							if e == nil {
								e = row.Scan(&raw)
							}
							if e != nil {
								t.Fatal(e)
							}
							var current runtimePodScaleInJournal
							if e = json.Unmarshal([]byte(raw), &current); e != nil {
								t.Fatal(e)
							}
							observed, e := s.scheduler.podRepo.GetByID(ctx, pods[0].ID)
							if e != nil {
								t.Fatal(e)
							}
							// The dedicated paused-source scan supersedes old cached facts.
							// Inject only after that fresh scan has persisted its result.
							if current.Phase == "migrating" && observed.ObservationAt != nil && observed.ObservationAt.After(op.CreatedAt) {
								change()
							}
							return false, nil, nil
						})
					}
					if err = s.Process(ctx); err != nil {
						t.Fatal(err)
					}
					current, _ := scaleInRead(t, s, op.ID)
					if stage == "execution" {
						if current.Status != "starting" || current.ErrorMessage == "" {
							t.Fatalf("physical source inspection did not hold resize: %+v", current)
						}
					} else if current.Status != "failed" {
						t.Fatalf("changed observation did not stop resize: %+v", current)
					}
				}
				if !changed {
					t.Fatal("race injection did not execute")
				}
				d, err := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
				if err != nil {
					t.Fatal(err)
				}
				if *d.Spec.Replicas != 2 {
					t.Fatal("resized using old observation")
				}
				p, err := s.scheduler.podRepo.GetByID(ctx, pods[0].ID)
				if err != nil {
					t.Fatal(err)
				}
				if p.SchedulingPaused != (stage == "execution") {
					t.Fatalf("source pause does not match held/finished operation: stage=%s paused=%v", stage, p.SchedulingPaused)
				}
				retained, err := kube.CoreV1().Pods("runtime").Get(ctx, "hermes-runtime-pod-2", metav1.GetOptions{})
				if err != nil {
					t.Fatal(err)
				}
				protected := retained.Labels["clawmanager.io/scale-in-protected"] != ""
				if protected != (stage == "execution") {
					t.Fatalf("retained protection does not match held/finished operation: stage=%s protected=%v", stage, protected)
				}
			})
		}
	}
}
