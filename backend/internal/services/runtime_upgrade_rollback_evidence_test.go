package services

import (
	"context"
	"strings"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

type rollbackEvidenceInventory struct{ pods []models.RuntimePod }

func (p *rollbackEvidenceInventory) RuntimeDeploymentPods(context.Context, string) ([]models.RuntimePod, error) {
	return p.pods, nil
}

func TestUpgradeRollbackRequiresLiveSourceAndFreshBindingMySQL(t *testing.T) {
	db := upgradeFlowDB(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	image := "repo@" + digest
	sourceJSON := `{"tenant/source":"` + image + `"}`
	exec(`INSERT INTO runtime_rollouts(id,runtime_type,target_image_ref,status,phase,source_images_json,rollback_status) VALUES(1,'openclaw','target','error','rollback_restore',?,'waiting')`, sourceJSON)
	exec(`INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,image_digest,state,last_seen_at) VALUES(1,'openclaw','tenant','source-pod','uid-source','source',?,?,'ready',UTC_TIMESTAMP())`, image, digest)
	exec(`INSERT INTO instances(id,user_id,type,status,runtime_generation) VALUES(1,1,'openclaw','running',3)`)
	exec(`INSERT INTO runtime_upgrade_items(rollout_id,instance_id,state,created_at,updated_at) VALUES(1,1,'restart_ready',UTC_TIMESTAMP(),UTC_TIMESTAMP())`)
	binding := repository.NewInstanceRuntimeBindingRepository(db)
	if err := binding.Create(ctx, &models.InstanceRuntimeBinding{InstanceID: 1, RuntimePodID: 1, RuntimeType: "openclaw", GatewayPort: 20000, GatewayID: "gw", State: "running", Generation: 3, WorkspacePath: "/fixture"}); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE instance_runtime_bindings SET last_health_at=UTC_TIMESTAMP() WHERE instance_id=1`)
	inventory := &rollbackEvidenceInventory{}
	svc := &RuntimeUpgradeService{sess: db, deployments: inventory}
	rollout := &models.RuntimeRollout{ID: 1, SourceImagesJSON: &sourceJSON}
	check := func(want bool) {
		t.Helper()
		got, err := svc.RollbackRecoveryVerified(ctx, rollout)
		if err != nil || got != want {
			t.Fatalf("verified=%v want=%v err=%v", got, want, err)
		}
	}
	check(false) // Historical ready registration cannot prove recovery.
	uid := "uid-replacement"
	inventory.pods = []models.RuntimePod{{Namespace: "tenant", DeploymentName: "source", PodUID: &uid, State: "ready", ImageRef: image, ImageDigest: &digest}}
	check(false)
	uid = "uid-source"
	exec(`UPDATE instance_runtime_bindings SET last_health_at=DATE_SUB(UTC_TIMESTAMP(),INTERVAL 5 MINUTE) WHERE instance_id=1`)
	check(false)
	exec(`UPDATE instance_runtime_bindings SET last_health_at=UTC_TIMESTAMP(),generation=2 WHERE instance_id=1`)
	check(false)
	exec(`UPDATE instance_runtime_bindings SET generation=3 WHERE instance_id=1`)
	check(true)
	// The production scoped inventory deliberately keeps an Agent-independent
	// fallback state unhealthy. KubernetesReady is its separate traffic proof.
	inventory.pods[0].State = "unhealthy"
	inventory.pods[0].KubernetesReady = true
	check(true)
	inventory.pods[0].KubernetesReady = false
	check(false)
	inventory.pods[0].State = "ready"
	// Legacy Agents omitted UID. Only an exact live Pod + endpoint/IP match can
	// supply it; a missing UID must neither falsely fail nor trust history alone.
	exec(`UPDATE runtime_pods SET pod_uid=NULL,pod_ip='10.1.2.2',agent_endpoint='http://10.1.2.2:19090' WHERE id=1`)
	ip := "10.1.2.2"
	inventory.pods[0].PodName = "source-pod"
	inventory.pods[0].PodIP = &ip
	check(true)
	exec(`UPDATE runtime_pods SET agent_endpoint='http://10.1.2.99:19090' WHERE id=1`)
	check(false)
	exec(`UPDATE runtime_pods SET agent_endpoint='http://10.1.2.2:19090' WHERE id=1`)
	check(true)
	var state string
	row, err := db.SQL().QueryRow(`SELECT rollback_status FROM runtime_rollouts WHERE id=1`)
	if err != nil {
		t.Fatal(err)
	}
	if err := row.Scan(&state); err != nil || state != "restored" {
		t.Fatalf("state=%s err=%v", state, err)
	}
	exec(`UPDATE runtime_upgrade_items SET state='prepared' WHERE rollout_id=1`)
	check(true)
	row, _ = db.SQL().QueryRow(`SELECT rollback_status FROM runtime_rollouts WHERE id=1`)
	if err := row.Scan(&state); err != nil || state != "not_needed" {
		t.Fatalf("untouched preflight misleadingly waits for restore: %s %v", state, err)
	}
}
