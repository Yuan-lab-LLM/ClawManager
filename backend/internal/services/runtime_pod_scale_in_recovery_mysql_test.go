package services

import (
	"context"
	"encoding/json"
	ktesting "k8s.io/client-go/testing"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestScaleInMySQLInterruptedEarlierPhaseUsesNativeResizeProof(t *testing.T) {
	for _, scenario := range []string{"same-operation", "foreign-marker-starting", "same-operation-resizing", "same-operation-retry", "missing-marker-retry", "missing-marker-resizing", "missing-marker-observing", "legacy-automatic", "controller-pending", "cancel-after-resize", "runtime-type", "foreign-controller-owner", "destination-healthy", "destination-missing", "destination-generation", "destination-unhealthy", "destination-stopped", "foreign-operation", "missing-marker", "deployment-uid", "template", "pvc", "unexpected-pod", "source-binding", "source-claim", "native-assignment", "missing-snapshot", "generation"} {
		t.Run(scenario, func(t *testing.T) {
			sess, s, kube := scaleInMySQLFixture(t)
			ctx := context.Background()
			op := scaleInQueue(t, s)
			if err := s.Process(ctx); err != nil {
				t.Fatal(err)
			}
			_, j := scaleInRead(t, s, op.ID)
			d, err := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			d.Generation = j.Snapshot.DeploymentGeneration + 1
			d.Status.ObservedGeneration = d.Generation
			d.Status.Replicas, d.Status.UpdatedReplicas, d.Status.ReadyReplicas = 1, 1, 1
			if d.Annotations == nil {
				d.Annotations = map[string]string{}
			}
			if scenario == "legacy-automatic" {
				j.AutomaticIdle, j.Protection.AutomaticIdle = true, true
				j.Plan.Replicas, j.Protection.Replicas = 0, 0
				zero := int32(0)
				d.Spec.Replicas = &zero
				d.Status.Replicas, d.Status.UpdatedReplicas, d.Status.ReadyReplicas = 0, 0, 0
				j.Protection.SourcePods = append(j.Protection.SourcePods, j.Protection.RetainedPods...)
				j.Protection.RetainedPods = nil
				j.Plan.SourcePods = append(j.Plan.SourcePods, RuntimePodScaleInSource{ID: 2, PodName: "hermes-runtime-pod-2", PodUID: "hermes-runtime-pod-2-uid"})
				if _, err = sess.SQL().Exec("UPDATE runtime_pods SET scheduling_paused=1 WHERE id=2"); err != nil {
					t.Fatal(err)
				}
				d.Annotations["clawmanager.io/idle-sleep-operation"] = op.ID
				delete(d.Annotations, "clawmanager.io/scale-in-resize-operation")
				if err = kube.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), "runtime", "hermes-runtime-pod-2"); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "runtime-type":
				d.Labels["clawmanager.io/runtime-type"] = "opencode"
			case "foreign-controller-owner":
				d.Labels["clawmanager.io/scale-in-operation"] = "foreign"
			case "foreign-operation", "foreign-marker-starting":
				d.Annotations["clawmanager.io/scale-in-resize-operation"] = "foreign"
			case "missing-marker", "missing-marker-resizing", "missing-marker-observing", "missing-marker-retry":
				delete(d.Annotations, "clawmanager.io/scale-in-resize-operation")
			case "deployment-uid":
				d.UID = "replacement"
			case "template":
				d.Spec.Template.Spec.Containers[0].Image = "foreign-image"
			case "pvc":
				claim, e := kube.CoreV1().PersistentVolumeClaims("runtime").Get(ctx, "workspace", metav1.GetOptions{})
				if e != nil {
					t.Fatal(e)
				}
				claim.UID = "replacement"
				if _, e = kube.CoreV1().PersistentVolumeClaims("runtime").Update(ctx, claim, metav1.UpdateOptions{}); e != nil {
					t.Fatal(e)
				}
			case "unexpected-pod":
				p, e := kube.CoreV1().Pods("runtime").Get(ctx, "hermes-runtime-pod-2", metav1.GetOptions{})
				if e != nil {
					t.Fatal(e)
				}
				p.Name = "foreign"
				p.UID = "foreign"
				if _, e = kube.CoreV1().Pods("runtime").Create(ctx, p, metav1.CreateOptions{}); e != nil {
					t.Fatal(e)
				}
			case "source-binding":
				if _, err = sess.SQL().Exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES(101,1,'hermes','hermes','lite','running',1,NOW(6))"); err != nil {
					t.Fatal(err)
				}
				if _, err = sess.SQL().Exec("INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,state,generation,workspace_path) VALUES(101,1,'hermes','gw',20000,'running',1,'/workspace')"); err != nil {
					t.Fatal(err)
				}
			case "source-claim":
				if _, err = sess.SQL().Exec("UPDATE runtime_pods SET used_slots=1 WHERE id=1"); err != nil {
					t.Fatal(err)
				}
			case "native-assignment":
				cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "foreign-assignment", Namespace: "runtime", Labels: map[string]string{"clawmanager.io/purpose": "lite-instance"}}, Data: map[string]string{"assignment": "invalid"}}
				if _, err = kube.CoreV1().ConfigMaps("runtime").Create(ctx, cm, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
			case "missing-snapshot":
				j.Snapshot = nil
			case "generation":
				d.Generation++
				d.Status.ObservedGeneration = d.Generation
			}
			if _, err = kube.AppsV1().Deployments("runtime").Update(ctx, d, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if scenario != "controller-pending" {
				if err = kube.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), "runtime", "hermes-runtime-pod"); err != nil {
					t.Fatal(err)
				}
			}
			if strings.HasPrefix(scenario, "destination-") {
				status := "running"
				if scenario == "destination-stopped" {
					status = "stopped"
				}
				if _, err = sess.SQL().Exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES(101,1,'hermes','hermes','lite',?,1,NOW(6))", status); err != nil {
					t.Fatal(err)
				}
				if scenario != "destination-missing" {
					state := "running"
					generation := 1
					if scenario == "destination-unhealthy" {
						state = "error"
					}
					if scenario == "destination-generation" {
						generation = 2
					}
					if _, err = sess.SQL().Exec("INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,state,generation,workspace_path) VALUES(101,2,'hermes','gw',20000,?,?,'/workspace')", state, generation); err != nil {
						t.Fatal(err)
					}
				}
				items, _ := json.Marshal([]RuntimeSchedulingItem{{InstanceID: 101, SourcePodID: 1, TargetPodID: 2, TargetSlot: 1, State: "starting", SourcePodUID: "hermes-runtime-pod-uid", TargetPodUID: "hermes-runtime-pod-2-uid"}})
				if _, err = sess.SQL().Exec("UPDATE runtime_management_operations SET items_json=? WHERE id=?", string(items), op.ID); err != nil {
					t.Fatal(err)
				}
			}
			j.Phase = "migrating"
			if strings.HasSuffix(scenario, "retry") {
				j.Phase = "resizing"
			}
			if strings.HasSuffix(scenario, "resizing") {
				j.Phase = "resizing"
			}
			if strings.HasSuffix(scenario, "observing") {
				j.Phase = "observing"
			}
			raw, _ := json.Marshal(j)
			if _, err = sess.SQL().Exec("UPDATE runtime_management_operations SET status='needs_attention',request_json=? WHERE id=?", string(raw), op.ID); err != nil {
				t.Fatal(err)
			}
			if scenario == "foreign-marker-starting" {
				j.Phase = "observing"
				raw, _ = json.Marshal(j)
				if _, err = sess.SQL().Exec("UPDATE runtime_management_operations SET status='starting',request_json=? WHERE id=?", string(raw), op.ID); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "cancel-after-resize" {
				if _, err = sess.SQL().Exec("UPDATE runtime_management_operations SET cancel_requested=1 WHERE id=?", op.ID); err != nil {
					t.Fatal(err)
				}
			}
			kube.ClearActions()
			if strings.HasSuffix(scenario, "retry") {
				if _, err = s.RetryOperation(ctx, op.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err = s.Process(ctx); err != nil {
				t.Fatal(err)
			}
			for _, action := range kube.Actions() {
				if action.GetVerb() == "patch" && action.GetResource().Resource == "deployments" && strings.Contains(string(action.(ktesting.PatchAction).GetPatch()), "/spec/replicas") {
					t.Fatal("recovery replayed replicas")
				}
			}
			current, _ := scaleInRead(t, s, op.ID)
			want := "needs_attention"
			if scenario == "same-operation" || scenario == "same-operation-resizing" || scenario == "same-operation-retry" || scenario == "legacy-automatic" || scenario == "cancel-after-resize" || scenario == "destination-healthy" {
				want = "completed"
			}
			if scenario == "controller-pending" {
				want = "starting"
			}
			if current.Status != want {
				t.Fatalf("status=%s want %s: %s", current.Status, want, current.ErrorMessage)
			}
			if scenario == "destination-healthy" && current.Items[0].State != "completed" {
				t.Fatalf("verified migration item still %s", current.Items[0].State)
			}
			live, _ := kube.AppsV1().Deployments("runtime").Get(ctx, d.Name, metav1.GetOptions{})
			if want == "needs_attention" && live.Labels["clawmanager.io/scale-in-operation"] != d.Labels["clawmanager.io/scale-in-operation"] {
				t.Fatal("unproved resize lost protection")
			}
		})
	}
}

func TestScaleInMySQLInterruptedProtectionCancelsOnlyUnchangedOriginalSet(t *testing.T) {
	sess, s, kube := scaleInMySQLFixture(t)
	ctx := context.Background()
	op := scaleInQueue(t, s)
	_, j := scaleInRead(t, s, op.ID)
	j.Phase = "acquiring_protection"
	raw, _ := json.Marshal(j)
	if _, err := sess.SQL().Exec("UPDATE runtime_management_operations SET status='processing',owner_token=?,request_json=? WHERE id=?", s.token, string(raw), op.ID); err != nil {
		t.Fatal(err)
	}
	native, err := s.scaleInKube()
	if err != nil {
		t.Fatal(err)
	}
	if err = native.ProtectRuntimeScaleIn(ctx, j.Protection); err != nil {
		t.Fatal(err)
	}
	j.Phase = "migrating"
	raw, _ = json.Marshal(j)
	if _, err = sess.SQL().Exec("UPDATE runtime_management_operations SET status='needs_attention',cancel_requested=1,request_json=? WHERE id=?", string(raw), op.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Process(ctx); err != nil {
		t.Fatal(err)
	}
	current, _ := scaleInRead(t, s, op.ID)
	if current.Status != "cancelled" {
		t.Fatalf("unchanged set cancellation status=%s: %s", current.Status, current.ErrorMessage)
	}
	d, err := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if *d.Spec.Replicas != 2 || d.Labels["clawmanager.io/scale-in-operation"] != "" {
		t.Fatal("original capacity or protection not restored")
	}
	source, err := s.scheduler.podRepo.GetByID(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if source.SchedulingPaused {
		t.Fatal("cancelled original source remains paused")
	}
}
