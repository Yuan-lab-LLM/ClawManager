package services

import (
	"context"
	"errors"
	"testing"

	"clawreef/internal/models"
)

type retainedStopMigrationAgent struct {
	*standbyFlowAgent
	deleteCalls int
}

func (*retainedStopMigrationAgent) RequiresConfirmedGatewayStop() bool { return true }
func (a *retainedStopMigrationAgent) DeleteGateway(context.Context, string, string) error {
	a.deleteCalls++
	return models.ErrRuntimeUpgradeConflict
}

// Exercise the production migration entry after a rollback consumed a SQL
// generation without publishing a new writer. A second Delete cannot authorize
// the old physical generation; only its independently retained stop proof can.
func TestUpgradeRecoveryMySQLRetainedStopDoesNotDeleteOldGenerationAgain(t *testing.T) {
	sess, u, target := recoveryActionsDB(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	p, err := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target, CandidateInstanceIDs: []int{481}, RecoveryMode: "retry", AutoRollback: true})
	if err != nil || !p.Passed {
		t.Fatalf("parent preflight: %+v %v", p, err)
	}
	exec(`UPDATE runtime_rollouts SET status='error',phase='session_migration' WHERE id=?`, p.Rollout.ID)
	exec(`UPDATE runtime_upgrade_items SET state='restart_ready',isolated_at=NOW(),isolation_phase='session_migration',recovery_started_at=NOW(),operation_phase='rollback_restore',recovery_status='needs_attention',target_runtime_version='2026.11.1' WHERE rollout_id=?`, p.Rollout.ID)
	exec(`UPDATE instances SET runtime_generation=9 WHERE id=481`)
	exec(`DELETE FROM instance_runtime_bindings WHERE instance_id=481`)
	var path string
	row, _ := sess.SQL().QueryRow(`SELECT workspace_path FROM instances WHERE id=481`)
	if err := row.Scan(&path); err != nil {
		t.Fatal(err)
	}
	original := models.InstanceIdleStopOperation{InstanceID: 481, UserID: 1, Generation: 8, RuntimePodID: 319, GatewayID: "gw-481-8", PodUID: "source-uid", Endpoint: "http://10.1.2.2:19090", WorkspacePath: path, StorageIdentity: "verified-source-storage", Phase: "completed"}
	proof := &retainedColdSourceInventory{recoveryInventory: u.deployments.(recoveryInventory), source: original}
	u.deployments = proof
	a := &retainedStopMigrationAgent{standbyFlowAgent: &standbyFlowAgent{upgradeFlowAgent: &upgradeFlowAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, states: map[string]*RuntimeAgentGatewayState{"gw-481-8": {InstanceID: 481, Generation: 8, GatewayID: "gw-481-8", State: "stopped"}}}}}
	u.agent = a
	child, err := u.RequestRecovery(ctx, 481, 1, true, RuntimeRecoveryRequest{Mode: "retry", ParentID: p.Rollout.ID, Generation: 9, TargetImage: target, IdempotencyKey: "retained-stop-regression"})
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE runtime_rollouts SET status='running',phase='session_migration' WHERE id=?`, child.ID)
	run := func(claim bool) error {
		exec(`UPDATE runtime_upgrade_items SET state='compatibility_checked' WHERE rollout_id=?`, child.ID)
		items, err := u.listUpgradeItems(ctx, child.ID)
		if err != nil || len(items) != 1 {
			t.Fatalf("child items: %+v %v", items, err)
		}
		call := func(c context.Context) error { return u.migrateUpgradeItem(c, child, items[0], "http://target", a) }
		if !claim {
			return call(ctx)
		}
		return u.withUpgradeClaim(ctx, child.ID, 481, "session_migration", call)
	}
	if err := run(true); err != nil {
		t.Fatalf("retained stopped writer rejected instead of migrating: %v", err)
	}
	if a.deleteCalls != 0 || a.migrations != 1 {
		t.Fatalf("old gateway deleted again or migration missing: deletes=%d migrations=%d", a.deleteCalls, a.migrations)
	}
	for _, name := range []string{"missing-proof", "wrong-owner", "wrong-gateway", "newer-writer", "missing-storage", "missing-claim"} {
		t.Run(name, func(t *testing.T) {
			proof.source, proof.err = original, nil
			switch name {
			case "missing-proof":
				proof.err = errors.New("termination proof unavailable")
			case "wrong-owner":
				proof.source.UserID++
			case "wrong-gateway":
				proof.source.GatewayID = "different-writer"
			case "newer-writer":
				proof.err = errors.New("newer publication is unconfirmed")
			case "missing-storage":
				proof.source.StorageIdentity = ""
			}
			if err := run(name != "missing-claim"); err == nil || a.migrations != 1 {
				t.Fatalf("unproven writer allowed migration: %v count=%d", err, a.migrations)
			}
		})
	}
	var generation, bindings int
	row, _ = sess.SQL().QueryRow(`SELECT runtime_generation FROM instances WHERE id=481`)
	row.Scan(&generation)
	row, _ = sess.SQL().QueryRow(`SELECT COUNT(*) FROM instance_runtime_bindings WHERE instance_id=481`)
	row.Scan(&bindings)
	if generation != 9 || bindings != 0 {
		t.Fatal("stop proof reuse mutated SQL generation or fabricated a binding")
	}
}
