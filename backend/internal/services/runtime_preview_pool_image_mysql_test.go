package services

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"clawreef/internal/runtimeauth"
	"clawreef/internal/services/k8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestManagedPreviewPoolImageRequiresCommittedNativeDefaultMySQL(t *testing.T) {
	for _, rt := range []string{"openclaw", "hermes", "opencode", "deepseek-harness"} {
		t.Run(rt, func(t *testing.T) {
			sess := runtimeManagementMySQLDB(t)
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := sess.SQL().Exec(q, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec("CREATE TABLE IF NOT EXISTS runtime_rollout_checkpoints(rollout_id BIGINT PRIMARY KEY,state_json JSON)")
			image := "repo/runtime@sha256:" + strings.Repeat("b", 64)
			name := rt + "-runtime"
			plan := emptyPoolRecoveryPlan{Kind: RuntimeEmptyPoolRecoveryStrategy, Version: 1, SourceUID: "controller-uid", Proof: &k8s.EmptyRuntimePoolProof{Source: k8s.RuntimeRolloutTarget{Namespace: "tenant", Name: name, UID: "controller-uid", Image: "repo/runtime@sha256:" + strings.Repeat("a", 64)}, Configuration: strings.Repeat("c", 64), SlotCount: 2}}
			raw, _ := json.Marshal(plan)
			sources, _ := json.Marshal(map[string]string{replacementKey(plan.Proof.Source): plan.Proof.Source.Image})
			exec("INSERT INTO runtime_rollouts(id,runtime_type,target_image_ref,target_image_digest,status,phase,preflight_json,source_images_json,plan_fingerprint,started_at) VALUES(20,?,?,?,'finished',?,?,?,?,?)", rt, image, imageDigestFromReference(image), RuntimeEmptyPoolRecoveryPhase, string(raw), string(sources), emptyPoolFingerprint(plan, image), time.Now().UTC().Add(-time.Minute))
			exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation) VALUES(101,1,?,'gateway','lite','creating',1)", rt)
			exec("INSERT INTO runtime_pods(id,runtime_type,namespace,deployment_name,pod_name,pod_uid,state,image_ref) VALUES(1,?,'tenant',?,'worker-pod','pod-uid','ready',?)", rt, name, image)
			exec("INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,state,generation,workspace_path) VALUES(101,1,?,'pending-101-1',20000,'creating',1,'/workspaces/test/101')", rt)
			controller := true
			d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "tenant", UID: "controller-uid", Labels: map[string]string{"clawmanager.io/runtime-type": rt}}}
			rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "rs", Namespace: "tenant", UID: "rs-uid", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: name, UID: d.UID, Controller: &controller}}}}
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "worker-pod", Namespace: "tenant", UID: "pod-uid", OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "rs", UID: rs.UID, Controller: &controller}}}}
			kube := fake.NewSimpleClientset(d, rs, pod)
			u := &RuntimeUpgradeService{sess: sess, agent: &slotRuntimeAgentClient{kube: kube}}
			id := runtimeauth.Identity{InstanceID: 101, UserID: 1, Generation: 1, PodUID: "pod-uid"}
			kind, approved, err := u.ManagedPreviewPoolImage(t.Context(), id)
			if err != nil || kind != rt || approved != image {
				t.Fatalf("new committed pool still checked against stale config: %s %s %v", kind, approved, err)
			}
			exec("UPDATE runtime_rollouts SET status='running' WHERE id=20")
			if _, approved, _ = u.ManagedPreviewPoolImage(t.Context(), id); approved != "" {
				t.Fatal("uncommitted image was approved")
			}
			exec("UPDATE runtime_rollouts SET status='finished' WHERE id=20")
			id.Generation++
			if _, approved, _ = u.ManagedPreviewPoolImage(t.Context(), id); approved != "" {
				t.Fatal("wrong generation was approved")
			}
			id.Generation--
			d.UID = "replacement"
			if _, err = kube.AppsV1().Deployments("tenant").Update(t.Context(), d, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, approved, _ = u.ManagedPreviewPoolImage(t.Context(), id); approved != "" {
				t.Fatal("replacement controller inherited image approval")
			}
			d.UID = "controller-uid"
			if _, err = kube.AppsV1().Deployments("tenant").Update(t.Context(), d, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			exec("UPDATE runtime_rollouts SET plan_fingerprint='tampered' WHERE id=20")
			if _, approved, _ = u.ManagedPreviewPoolImage(t.Context(), id); approved != "" {
				t.Fatal("tampered default proof was approved")
			}
		})
	}
}
