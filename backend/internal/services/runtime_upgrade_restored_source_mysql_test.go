package services

import (
	"context"
	"errors"
	"testing"

	"clawreef/internal/models"
)

func TestUpgradeRecoveryMySQLRestoredSourceNeedsFinalizedUnpublishedProof(t *testing.T) {
	for _, name := range []string{"restored", "missing-proof", "wrong-owner", "newer-writer", "wrong-pod", "missing-storage", "not-restored", "target-attempted", "still-bound", "not-rollback"} {
		t.Run(name, func(t *testing.T) {
			sess, u, target := recoveryActionsDB(t)
			ctx := context.Background()
			p, err := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target, CandidateInstanceIDs: []int{481}, RecoveryMode: "retry", AutoRollback: true})
			if err != nil || !p.Passed {
				t.Fatalf("preflight %+v: %v", p, err)
			}
			exec := func(q string, args ...any) {
				t.Helper()
				if _, e := sess.SQL().Exec(q, args...); e != nil {
					t.Fatal(e)
				}
			}
			exec(`UPDATE runtime_rollouts SET status='error',phase='session_migration' WHERE id=?`, p.Rollout.ID)
			exec(`UPDATE runtime_upgrade_items SET state='restart_ready',isolated_at=NOW(),isolation_phase='session_migration',recovery_started_at=NOW(),operation_phase='rollback_restore',recovery_status='needs_attention',target_runtime_version='2026.11.1' WHERE rollout_id=?`, p.Rollout.ID)
			exec(`UPDATE instances SET runtime_generation=9 WHERE id=481`)
			if name != "still-bound" {
				exec(`DELETE FROM instance_runtime_bindings WHERE instance_id=481`)
			}
			var path string
			row, _ := sess.SQL().QueryRow(`SELECT workspace_path FROM instances WHERE id=481`)
			if err := row.Scan(&path); err != nil {
				t.Fatal(err)
			}
			proof := &retainedColdSourceInventory{recoveryInventory: u.deployments.(recoveryInventory), source: models.InstanceIdleStopOperation{InstanceID: 481, UserID: 1, Generation: 8, RuntimePodID: 319, GatewayID: "gw-481-8", PodUID: "source-uid", Endpoint: "http://10.1.2.2:19090", WorkspacePath: path, StorageIdentity: "verified-source-storage", Phase: "completed"}}
			u.deployments = proof
			u.agent = &upgradeFlowAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, states: map[string]*RuntimeAgentGatewayState{"gw-481-8": {InstanceID: 481, Generation: 8, GatewayID: "gw-481-8", State: "stopped"}}}
			switch name {
			case "missing-proof":
				proof.err = errors.New("container proof unavailable")
			case "wrong-owner":
				proof.source.UserID++
			case "newer-writer":
				proof.source.Generation++
			case "wrong-pod":
				proof.source.PodUID = "replacement"
			case "missing-storage":
				proof.source.StorageIdentity = ""
			case "not-restored":
				exec(`UPDATE runtime_upgrade_items SET state='migration_started' WHERE rollout_id=?`, p.Rollout.ID)
			case "target-attempted":
				exec(`UPDATE runtime_upgrade_items SET restart_attempts=1 WHERE rollout_id=?`, p.Rollout.ID)
			case "not-rollback":
				exec(`UPDATE runtime_rollouts SET auto_rollback=FALSE WHERE id=?`, p.Rollout.ID)
			}
			prior, err := u.recoveryStoppedSource(ctx, p.Rollout.ID, 481, 9)
			if name == "restored" {
				if err != nil || intValue(prior.SourceGeneration, 0) != 9 || stringValue(prior.SourceRuntimeVersion) != "2026.10.2" || stringValue(prior.SourceGatewayID) != "gw-481-8" {
					t.Fatalf("restored source lost original version/stop identity: %+v %v", prior, err)
				}
			} else if err == nil {
				t.Fatalf("unproven source admitted: %+v", prior)
			}
			var bindings, generation int
			row, _ = sess.SQL().QueryRow(`SELECT COUNT(*) FROM instance_runtime_bindings WHERE instance_id=481`)
			row.Scan(&bindings)
			row, _ = sess.SQL().QueryRow(`SELECT runtime_generation FROM instances WHERE id=481`)
			row.Scan(&generation)
			if generation != 9 || (bindings != 0 && name != "still-bound") || (bindings != 1 && name == "still-bound") {
				t.Fatal("proof check mutated lifecycle")
			}
		})
	}
}
