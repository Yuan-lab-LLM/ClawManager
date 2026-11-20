package services

import (
	"context"
	"strings"
	"testing"

	"clawreef/internal/runtimeauth"
)

func TestUpgradeMySQLPreviewRetainedImageRequiresConfirmedStopLineage(t *testing.T) {
	for _, name := range []string{"confirmed-stop", "missing-stop", "unconfirmed-stop", "wrong-owner", "wrong-workspace", "wrong-source-pod", "wrong-source-image", "wrong-target-image", "wrong-generation", "newer-upgrade"} {
		t.Run(name, func(t *testing.T) {
			sess, u, target := recoveryActionsDB(t)
			ctx := context.Background()
			p, err := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target, CandidateInstanceIDs: []int{481}, RecoveryMode: "retry"})
			if err != nil || !p.Passed {
				t.Fatalf("preflight %+v %v", p, err)
			}
			exec := func(q string, args ...any) {
				t.Helper()
				if _, e := sess.SQL().Exec(q, args...); e != nil {
					t.Fatal(e)
				}
			}
			exec(`UPDATE runtime_rollouts SET status='finished',phase='postflight',finished_at=UTC_TIMESTAMP()-INTERVAL 2 MINUTE WHERE id=?`, p.Rollout.ID)
			exec(`UPDATE runtime_upgrade_items SET state='verified',recovery_status='verified',restart_target_pod_uid='migration-target',postflight_json=? WHERE rollout_id=?`, `{"migration":{"instance_id":481,"status":"validated","output_sha256":"verified-output","session_catalog_sha256":"verified-catalog"}}`, p.Rollout.ID)
			exec(`UPDATE runtime_pods SET pod_uid='migration-target',image_ref=?,image_digest=? WHERE id=319`, p.Rollout.TargetImageRef, *p.Rollout.TargetImageDigest)
			exec(`INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,deployment_name,pod_uid,pod_ip,agent_endpoint,state,image_ref,image_digest) VALUES(320,'openclaw','tenant','next-pod','next-pool','next-uid','10.1.2.3','http://10.1.2.3:19090','ready',?,?)`, p.Rollout.TargetImageRef, *p.Rollout.TargetImageDigest)
			b, _ := u.bindings.GetByInstanceID(ctx, 481)
			exec(`INSERT INTO instance_idle_stop_operations(id,instance_id,user_id,generation,runtime_pod_id,gateway_id,pod_uid,endpoint,workspace_path,storage_identity,phase,created_at,updated_at) VALUES('real-stop',481,1,9,319,'gw-481-9','migration-target','http://10.1.2.2:19090',?,'same-nfs','completed',UTC_TIMESTAMP()-INTERVAL 1 MINUTE,UTC_TIMESTAMP())`, b.WorkspacePath)
			exec(`UPDATE instances SET status='creating',runtime_generation=10 WHERE id=481`)
			exec(`UPDATE instance_runtime_bindings SET runtime_pod_id=320,generation=10,gateway_id='pending-481-10',state='creating',gateway_pid=NULL WHERE instance_id=481`)
			id := runtimeauth.Identity{InstanceID: 481, UserID: 1, Generation: 10, PodUID: "next-uid"}
			switch name {
			case "missing-stop":
				exec(`DELETE FROM instance_idle_stop_operations`)
			case "unconfirmed-stop":
				exec(`UPDATE instance_idle_stop_operations SET phase='error'`)
			case "wrong-owner":
				exec(`UPDATE instance_idle_stop_operations SET user_id=2`)
			case "wrong-workspace":
				exec(`UPDATE instance_idle_stop_operations SET workspace_path='/other'`)
			case "wrong-source-pod":
				exec(`UPDATE instance_idle_stop_operations SET pod_uid='unknown'`)
			case "wrong-source-image":
				exec(`UPDATE runtime_pods SET image_digest=? WHERE id=319`, "sha256:"+strings.Repeat("d", 64))
			case "wrong-target-image":
				exec(`UPDATE runtime_pods SET image_digest=? WHERE id=320`, "sha256:"+strings.Repeat("d", 64))
			case "wrong-generation":
				id.Generation++
			case "newer-upgrade":
				exec(`INSERT INTO runtime_rollouts(runtime_type,target_image_ref,status,phase,preflight_id,created_at,updated_at) VALUES('openclaw',?,'error','gateway_restart','newer-failed',UTC_TIMESTAMP(),UTC_TIMESTAMP())`, target)
				exec(`INSERT INTO runtime_upgrade_items(rollout_id,instance_id,member_order,state,created_at,updated_at) SELECT MAX(id),481,481,'restart_ready',UTC_TIMESTAMP(),UTC_TIMESTAMP() FROM runtime_rollouts`)
			}
			image, err := u.ManagedPreviewUpgradeImage(ctx, id)
			if name == "confirmed-stop" {
				if err != nil || image != p.Rollout.TargetImageRef {
					t.Fatalf("retained image %q %v", image, err)
				}
			} else if image != "" {
				t.Fatalf("unconfirmed stop approved %q", image)
			}
		})
	}
}

func TestUpgradeMySQLPreviewApprovesOnlyFrozenMigratedSuccessor(t *testing.T) {
	for _, name := range []string{"reserved-target", "retrying", "held", "wrong-user", "wrong-pod", "wrong-generation", "not-activated", "failed", "isolated", "bad-receipt", "wrong-migration-owner", "wrong-digest"} {
		t.Run(name, func(t *testing.T) {
			sess, u, target := recoveryActionsDB(t)
			ctx := context.Background()
			p, err := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target, CandidateInstanceIDs: []int{481}, RecoveryMode: "retry"})
			if err != nil || !p.Passed {
				t.Fatalf("preflight %+v %v", p, err)
			}
			exec := func(q string, args ...any) {
				t.Helper()
				if _, e := sess.SQL().Exec(q, args...); e != nil {
					t.Fatal(e)
				}
			}
			exec(`UPDATE runtime_rollouts SET status='running',phase='gateway_restart' WHERE id=?`, p.Rollout.ID)
			exec(`UPDATE runtime_upgrade_items SET state='restart_ready',recovery_status='starting',restart_target_pod_uid='target-uid',postflight_json=? WHERE rollout_id=?`, `{"migration":{"instance_id":481,"status":"validated","output_sha256":"verified-output","session_catalog_sha256":"verified-catalog","session_count":0}}`, p.Rollout.ID)
			exec(`UPDATE runtime_pods SET pod_uid='target-uid' WHERE id=319`)
			exec(`UPDATE instances SET runtime_generation=9 WHERE id=481`)
			exec(`UPDATE instance_runtime_bindings SET generation=9,gateway_id='pending-481-9' WHERE instance_id=481`)
			id := runtimeauth.Identity{InstanceID: 481, UserID: 1, Generation: 9, PodUID: "target-uid"}
			switch name {
			case "retrying":
				exec(`UPDATE runtime_upgrade_items SET recovery_status='retrying' WHERE rollout_id=?`, p.Rollout.ID)
			case "held":
				exec(`UPDATE runtime_upgrade_items SET recovery_status='needs_attention' WHERE rollout_id=?`, p.Rollout.ID)
			case "wrong-user":
				id.UserID++
			case "wrong-pod":
				id.PodUID = "other"
			case "wrong-generation":
				id.Generation++
			case "not-activated":
				exec(`UPDATE runtime_rollouts SET phase='session_migration' WHERE id=?`, p.Rollout.ID)
			case "failed":
				exec(`UPDATE runtime_rollouts SET status='error' WHERE id=?`, p.Rollout.ID)
			case "isolated":
				exec(`UPDATE runtime_upgrade_items SET isolated_at=NOW() WHERE rollout_id=?`, p.Rollout.ID)
			case "bad-receipt":
				exec(`UPDATE runtime_upgrade_items SET postflight_json='{}' WHERE rollout_id=?`, p.Rollout.ID)
			case "wrong-migration-owner":
				exec(`UPDATE runtime_upgrade_items SET postflight_json=JSON_SET(postflight_json,'$.migration.instance_id',482) WHERE rollout_id=?`, p.Rollout.ID)
			case "wrong-digest":
				exec(`UPDATE runtime_rollouts SET target_image_digest='sha256:wrong' WHERE id=?`, p.Rollout.ID)
			}
			image, err := u.ManagedPreviewUpgradeImage(ctx, id)
			if name == "reserved-target" || name == "retrying" {
				if err != nil || image != p.Rollout.TargetImageRef {
					t.Fatalf("admitted successor not approved: %q %v", image, err)
				}
			} else if image != "" {
				t.Fatalf("unproven target approved: %q", image)
			}
		})
	}
}
