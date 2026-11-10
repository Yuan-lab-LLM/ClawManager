package services

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
	"github.com/upper/db/v4"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestRuntimeIdlePoolFullServingPoolDoesNotWakeHistoricalImage(t *testing.T) {
	ctx := context.Background()
	sess, service, _ := scaleInMySQLFixture(t)
	// A burst needs another replica of the proven serving pool, not an old binary.
	if _, err := sess.SQL().Exec("UPDATE runtime_pods SET used_slots=8,pod_ip=IF(id=1,'10.0.0.1','10.0.0.2')"); err != nil {
		t.Fatal(err)
	}
	client := &idleWakeRegistrationClient{RuntimeDeploymentService: service.scheduler.deployments}
	service.scheduler.deployments = client
	j := runtimePodScaleInJournal{AutomaticIdle: true, Phase: "finished", RuntimePodScaleInRequest: RuntimePodScaleInRequest{RuntimeType: "hermes"}, Target: k8s.RuntimePodScaleTarget{Namespace: "runtime", DeploymentName: "zzz-hermes"}, Snapshot: &k8s.RuntimePodScaleSnapshot{Option: k8s.RuntimePodScaleOption{ImageRef: "repo/runtime@sha256:" + strings.Repeat("b", 64)}}}
	raw, _ := json.Marshal(j)
	if _, err := sess.SQL().Exec("INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES('wake-second','scale_in','completed',?,'[]',NOW(6),NOW(6))", string(raw)); err != nil {
		t.Fatal(err)
	}
	pending, err := service.scheduler.restoreIdleRuntimeCapacity(ctx, models.Instance{ID: 25}, "hermes")
	if err != nil || pending || len(client.calls) != 0 {
		t.Fatalf("full serving pool woke incompatible history instead of returning to expansion: calls=%v pending=%v err=%v", client.calls, pending, err)
	}
}

func TestRuntimeIdlePoolHistoricalServingImageDoesNotSuppressApprovedWake(t *testing.T) {
	sess, service, _ := scaleInMySQLFixture(t)
	if _, err := sess.SQL().Exec("UPDATE runtime_pods SET used_slots=8,pod_ip=IF(id=1,'10.0.0.1','10.0.0.2')"); err != nil {
		t.Fatal(err)
	}
	approved := "repo/runtime@sha256:" + strings.Repeat("b", 64)
	if _, err := sess.SQL().Exec("INSERT INTO runtime_rollouts(runtime_type,target_image_ref,status) VALUES('hermes',?,'finished')", approved); err != nil {
		t.Fatal(err)
	}
	client := &idleWakeRegistrationClient{RuntimeDeploymentService: service.scheduler.deployments}
	service.scheduler.deployments = client
	idleCompatibilityJournal(t, sess, "wake-second", "zzz-approved", approved)
	pending, err := service.scheduler.restoreIdleRuntimeCapacity(t.Context(), models.Instance{ID: 25}, "hermes")
	if err != nil || !pending || len(client.calls) != 1 || client.calls[0] != "zzz-approved" {
		t.Fatalf("historical live image suppressed the approved sleeping pool: calls=%v pending=%v err=%v", client.calls, pending, err)
	}
}

func idleCompatibilityJournal(t *testing.T, sess db.Session, id, deployment, image string) {
	t.Helper()
	j := runtimePodScaleInJournal{AutomaticIdle: true, Phase: "finished", RuntimePodScaleInRequest: RuntimePodScaleInRequest{RuntimeType: "hermes"}, Target: k8s.RuntimePodScaleTarget{Namespace: "runtime", DeploymentName: deployment}, Snapshot: &k8s.RuntimePodScaleSnapshot{Option: k8s.RuntimePodScaleOption{ImageRef: image}}}
	raw, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sess.SQL().Exec("INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES(?,'scale_in','completed',?,'[]',NOW(6),NOW(6))", id, string(raw)); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeIdlePoolPinnedRestartKeepsHistoricalSource(t *testing.T) {
	sess, service, _ := scaleInMySQLFixture(t)
	image := "repo/runtime@sha256:" + strings.Repeat("b", 64)
	for _, query := range []string{
		"INSERT INTO instances(id,user_id,type,status,runtime_generation,updated_at) VALUES(25,1,'hermes','creating',2,NOW(6))",
		"INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,state,capacity,used_slots) VALUES(3,'hermes','runtime','old-pod','old-uid','zzz-hermes','old','unhealthy',8,0)",
		"INSERT INTO instance_idle_stop_operations(id,instance_id,user_id,generation,runtime_pod_id,gateway_id,pod_uid,endpoint,workspace_path,storage_identity,phase,created_at,updated_at) VALUES('old-stop',25,1,1,3,'gw-25-1','old-uid','https://old','/workspace','original','completed',NOW(6),NOW(6))",
	} {
		if _, err := sess.SQL().Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := sess.SQL().Exec("UPDATE runtime_pods SET image_ref=? WHERE id=3", image); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.SQL().Exec("INSERT INTO runtime_rollouts(runtime_type,target_image_ref,status) VALUES('hermes',?,'finished')", "repo/runtime@sha256:"+strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	client := &idleWakeRegistrationClient{RuntimeDeploymentService: service.scheduler.deployments}
	service.scheduler.deployments = client
	idleCompatibilityJournal(t, sess, "wake-second", "zzz-hermes", image)
	pending, err := service.scheduler.restoreIdleRuntimeCapacity(t.Context(), models.Instance{ID: 25}, "hermes")
	if err != nil || !pending || len(client.calls) != 1 || client.calls[0] != "zzz-hermes" {
		t.Fatalf("restart lost source ownership: calls=%v pending=%v err=%v", client.calls, pending, err)
	}
	// The same controller name cannot authorize a different binary for old data.
	client.calls = nil
	if _, err = sess.SQL().Exec("UPDATE runtime_pods SET image_ref=? WHERE id=3", "repo/runtime@sha256:"+strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	pending, err = service.scheduler.restoreIdleRuntimeCapacity(t.Context(), models.Instance{ID: 25}, "hermes")
	if err != nil || pending || len(client.calls) != 0 {
		t.Fatalf("restart accepted changed source binary: calls=%v pending=%v err=%v", client.calls, pending, err)
	}
}

func TestRuntimeIdlePoolColdStartUsesApprovedImage(t *testing.T) {
	sess, service, _ := scaleInMySQLFixture(t)
	if _, err := sess.SQL().Exec("UPDATE runtime_pods SET state='unhealthy'"); err != nil {
		t.Fatal(err)
	}
	image := "repo/runtime@sha256:" + strings.Repeat("a", 64)
	if _, err := sess.SQL().Exec("INSERT INTO runtime_rollouts(runtime_type,target_image_ref,status) VALUES('hermes',?,'finished')", image); err != nil {
		t.Fatal(err)
	}
	client := &idleWakeRegistrationClient{RuntimeDeploymentService: service.scheduler.deployments}
	service.scheduler.deployments = client
	idleCompatibilityJournal(t, sess, "wake-first", "aaa-old", "repo/runtime@sha256:"+strings.Repeat("b", 64))
	idleCompatibilityJournal(t, sess, "wake-second", "zzz-current", image)
	pending, err := service.scheduler.restoreIdleRuntimeCapacity(t.Context(), models.Instance{ID: 25}, "hermes")
	if err != nil || !pending || len(client.calls) != 1 || client.calls[0] != "zzz-current" {
		t.Fatalf("cold request selected unapproved historical image: calls=%v pending=%v err=%v", client.calls, pending, err)
	}
}

func TestRuntimeIdlePoolColdConflictingContractsRequireDefaultProof(t *testing.T) {
	sess, service, _ := scaleInMySQLFixture(t)
	if _, err := sess.SQL().Exec("UPDATE runtime_pods SET state='unhealthy'"); err != nil {
		t.Fatal(err)
	}
	client := &idleWakeRegistrationClient{RuntimeDeploymentService: service.scheduler.deployments}
	service.scheduler.deployments = client
	idleCompatibilityJournal(t, sess, "wake-first", "aaa-old", "repo/runtime@sha256:"+strings.Repeat("b", 64))
	idleCompatibilityJournal(t, sess, "wake-second", "zzz-current", "repo/runtime@sha256:"+strings.Repeat("a", 64))
	pending, err := service.scheduler.restoreIdleRuntimeCapacity(t.Context(), models.Instance{ID: 25}, "hermes")
	if err == nil || pending || len(client.calls) != 0 {
		t.Fatalf("ambiguous cold defaults woke arbitrary history: calls=%v pending=%v err=%v", client.calls, pending, err)
	}
}

func TestIdleRuntimeContractIncludesNativeSecurityResourcesAndSlots(t *testing.T) {
	base := &k8s.RuntimePodScaleSnapshot{Option: k8s.RuntimePodScaleOption{ImageRef: "digest", SlotCount: 8}}
	base.Template.Labels = map[string]string{"clawmanager.io/runtime-protocol": "container-slots-v1", "clawmanager.io/runtime-security": "isolated", "clawmanager.io/sandbox-profile": "userns"}
	for _, change := range []string{"image", "slots", "protocol", "security", "sandbox", "resources", "native", "storage"} {
		t.Run(change, func(t *testing.T) {
			other := *base
			other.Template = *base.Template.DeepCopy()
			switch change {
			case "image":
				other.Option.ImageRef = "older"
			case "slots":
				other.Option.SlotCount = 4
			case "protocol":
				other.Template.Labels["clawmanager.io/runtime-protocol"] = "legacy"
			case "security":
				other.Template.Labels["clawmanager.io/runtime-security"] = ""
			case "sandbox":
				other.Template.Labels["clawmanager.io/sandbox-profile"] = ""
			case "resources":
				other.Template.Spec.Resources = &corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4")}}
			case "native":
				other.Template.Spec.Containers = []corev1.Container{{Name: "runtime", Command: []string{"legacy"}}}
			case "storage":
				other.WorkspaceStorageIdentity = "different-pvc"
			}
			if idleRuntimeContractMatches(base, &other) {
				t.Fatal("incompatible historical native contract accepted")
			}
		})
	}
}
