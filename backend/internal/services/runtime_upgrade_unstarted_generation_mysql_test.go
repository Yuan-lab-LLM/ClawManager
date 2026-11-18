package services

import (
	"clawreef/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestRecoveryMySQLMigratedUnstartedGenerationCanRetryWithoutRewritingState(t *testing.T) {
	sess, u, target := recoveryActionsDB(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	p, err := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target, CandidateInstanceIDs: []int{481}, RecoveryMode: "retry"})
	if err != nil || !p.Passed {
		t.Fatalf("preflight %+v %v", p, err)
	}
	proof, _ := json.Marshal(map[string]any{"migration": RuntimeAgentSessionSQLiteMigration{InstanceID: 481, Status: "validated", OutputSHA256: "archive", SessionCatalogSHA256: "catalog"}})
	exec(`UPDATE runtime_rollouts SET status='error',phase='gateway_restart',finished_at=NOW() WHERE id=?`, p.Rollout.ID)
	exec(`UPDATE runtime_upgrade_items SET state='restart_ready',source_generation=8,snapshot_ref='official-session-archive',postflight_json=?,recovery_started_at=NOW(),isolated_at=NOW(),recovery_status='needs_attention' WHERE rollout_id=?`, string(proof), p.Rollout.ID)
	exec(`DELETE FROM instance_runtime_bindings WHERE instance_id=481`)
	exec(`UPDATE instances SET status='creating',runtime_generation=9 WHERE id=481`)
	endpoint, uid := "http://target-agent", "target-uid"
	now := time.Now().UTC()
	live := models.RuntimePod{ID: 320, RuntimeType: "openclaw", Namespace: "tenant", PodName: "target-pod", DeploymentName: fmt.Sprintf("source-u%d", p.Rollout.ID), PodUID: &uid, KubernetesPodUID: &uid, KubernetesReady: true, AgentEndpoint: &endpoint, State: "ready", ImageRef: p.TargetImageRef, ImageDigest: p.Rollout.TargetImageDigest, LastSeenAt: &now}
	inventory := append(u.deployments.(recoveryInventory), live)
	u.deployments = inventory
	agent := &upgradeFlowAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, states: map[string]*RuntimeAgentGatewayState{"gw-481-8": {InstanceID: 481, Generation: 8, GatewayID: "gw-481-8", State: "stopped"}}}
	u.agent = agent
	assertBlocked := func(reason string) {
		t.Helper()
		info, e := u.RecoveryInfo(ctx, 481)
		if e != nil || info.CanRetry {
			t.Fatalf("%s allowed unsafe retry: %+v %v", reason, info, e)
		}
	}
	// Neither an old registration nor a live target can be treated as absence.
	u.deployments = inventory[:1]
	assertBlocked("target missing from Kubernetes")
	u.deployments = inventory
	agent.states["gw-481-9"] = &RuntimeAgentGatewayState{InstanceID: 481, Generation: 9, GatewayID: "gw-481-9", State: "running"}
	assertBlocked("target running")
	delete(agent.states, "gw-481-9")
	exec(`UPDATE runtime_upgrade_items SET restart_attempts=1 WHERE rollout_id=?`, p.Rollout.ID)
	assertBlocked("lost reserved attempt")
	exec(`UPDATE runtime_upgrade_items SET restart_attempts=0 WHERE rollout_id=?`, p.Rollout.ID)
	info, err := u.RecoveryInfo(ctx, 481)
	if err != nil || !info.CanRetry {
		t.Fatalf("proven unstarted recovery blocked: %+v %v", info, err)
	}
	child, err := u.RequestRecovery(ctx, 481, 1, true, RuntimeRecoveryRequest{Mode: "retry", ParentID: p.Rollout.ID, Generation: 9, TargetImage: target, IdempotencyKey: "unstarted-migration-retry", ConfirmPreserve: true})
	if err != nil || child.Status != "pending" {
		t.Fatalf("child admission %+v %v", child, err)
	}
	if err = u.Prepare(ctx, child); err != nil {
		t.Fatal(err)
	}
	items, err := u.listUpgradeItems(ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := u.candidateForUpgradeItem(ctx, items[0])
	if err != nil || candidate.Generation != 9 || candidate.GatewayID != "gw-481-8" || candidate.SourceVersion != "2026.10.2" {
		t.Fatalf("lost data generation/stop proof: %+v %v", candidate, err)
	}
	req := RuntimeAgentWorkspaceRequest{Generation: 9}
	u.attachRecoveryMigrationReceipt(ctx, child, items[0], candidate, &req)
	if req.ResumeRolloutID != fmt.Sprint(p.Rollout.ID) || req.ResumeGeneration != 8 {
		t.Fatal("validated migration was lost during startup-only recovery")
	}
	var status string
	var gen int
	row, _ := sess.SQL().QueryRow(`SELECT status,runtime_generation FROM instances WHERE id=481`)
	row.Scan(&status, &gen)
	if status != "creating" || gen != 9 {
		t.Fatalf("admission rewrote lifecycle: %s/%d", status, gen)
	}
}
