package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
)

// Reproduce 139 -> 140: the first attempt stopped the gateway, the next
// prepared child has no binding/source identity. Exercise production admission,
// Prepare and compatibility dispatch, not just the ancestry helper.
func TestUpgradeFallbackMySQLStoppedAncestorAndPersistentIsolation(t *testing.T) {
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
		t.Fatalf("%+v %v", p, err)
	}
	exec(`UPDATE runtime_rollouts SET status='error',phase='session_migration',finished_at=NOW() WHERE id=?`, p.Rollout.ID)
	exec(`UPDATE runtime_upgrade_items SET state='migration_started',recovery_status='needs_attention',error_message='old image-owned plugin link is missing' WHERE rollout_id=?`, p.Rollout.ID)
	exec(`DELETE FROM instance_runtime_bindings WHERE instance_id=481`)
	u.agent = &upgradeFlowAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, states: map[string]*RuntimeAgentGatewayState{}}
	info, err := u.RecoveryInfo(ctx, 481)
	if err != nil {
		t.Fatal(err)
	}
	child, err := u.RequestRecovery(ctx, 481, 1, true, RuntimeRecoveryRequest{Mode: "rebuild", ParentID: info.ParentID, Generation: 8, TargetImage: target, IdempotencyKey: "rebuild-unbound-child", ConfirmPreserve: true})
	if err != nil || child.Status != "pending" {
		t.Fatalf("admission %+v %v", child, err)
	}
	if err = u.Prepare(ctx, child); err != nil {
		t.Fatal(err)
	}
	items, err := u.listUpgradeItems(ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	c, err := u.candidateForUpgradeItem(ctx, items[0])
	if err != nil || c.Generation != 8 || c.GatewayID != "gw-481-8" {
		t.Fatalf("unbound recovery lost source: %+v %v", c, err)
	}
	// A subsequent failed child with an empty prepared capsule must not hide
	// the original confirmed stop or the quarantine after a process restart.
	exec(`UPDATE runtime_rollouts SET status='running',phase='compatibility_check' WHERE id=?`, child.ID)
	if err = u.holdFailedUpgrade(ctx, child, "deterministic compatibility failure"); err != nil {
		t.Fatal(err)
	}
	healthy, err := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target})
	if err != nil || !healthy.Passed || healthy.InstanceCount != 2 {
		t.Fatalf("held 481 blocked healthy fleet: %+v %v", healthy, err)
	}
	if len(healthy.ExcludedInstanceIDs) != 1 || healthy.ExcludedInstanceIDs[0] != 481 {
		t.Fatalf("quarantine missing %+v", healthy)
	}
	info, err = u.RecoveryInfo(ctx, 481)
	if err != nil {
		t.Fatal(err)
	}
	next, err := u.RequestRecovery(ctx, 481, 1, true, RuntimeRecoveryRequest{Mode: "retry", ParentID: info.ParentID, Generation: 8, TargetImage: target, IdempotencyKey: "third-unbound-recovery"})
	if err != nil {
		t.Fatal(err)
	}
	if err = u.Prepare(ctx, next); err != nil {
		t.Fatal(err)
	}
	items, _ = u.listUpgradeItems(ctx, next.ID)
	if _, err = u.candidateForUpgradeItem(ctx, items[0]); err != nil {
		t.Fatal(err)
	}
	// An old stop never authorizes a different generation, or a live source.
	exec(`UPDATE instances SET runtime_generation=9 WHERE id=481`)
	if _, err = u.candidateForUpgradeItem(ctx, items[0]); !errors.Is(err, ErrUpgradeSourceState) {
		t.Fatalf("generation mismatch permitted: %v", err)
	}
	exec(`UPDATE instances SET runtime_generation=8 WHERE id=481`)
	exec(`UPDATE runtime_rollouts SET status='preflight_blocked',phase='preflight' WHERE id=?`, child.ID)
	if _, err = u.candidateForUpgradeItem(ctx, items[0]); err != nil {
		t.Fatalf("blocked recovery preview hid valid ancestor: %v", err)
	}
}

type fallbackLifecycleAgent struct{ *standbyFlowAgent }

func (a *fallbackLifecycleAgent) MigrateSessionSQLite(ctx context.Context, endpoint string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentSessionSQLiteMigration, error) {
	r, err := a.standbyFlowAgent.MigrateSessionSQLite(ctx, endpoint, req)
	if r != nil && req.RecoveryMode == "rebuild" {
		r.DataPreserved = true
		r.PreservationSHA256 = strings.Repeat("a", 64)
	}
	return r, err
}

func TestUpgradeFallbackMySQLRealSchedulerRebuildAndHealthyFleet(t *testing.T) {
	for _, scenario := range []string{"rebuild-unbound", "upgrade-healthy-after-isolation"} {
		t.Run(scenario, func(t *testing.T) {
			sess, u, target := recoveryActionsDB(t)
			ctx := context.Background()
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := sess.SQL().Exec(q, args...); err != nil {
					t.Fatal(err)
				}
			}
			parent, err := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target, CandidateInstanceIDs: []int{481}, RecoveryMode: "retry"})
			if err != nil {
				t.Fatal(err)
			}
			exec(`UPDATE runtime_rollouts SET status='error',phase='session_migration',finished_at=NOW() WHERE id=?`, parent.Rollout.ID)
			exec(`UPDATE runtime_upgrade_items SET state='migration_started',recovery_status='needs_attention',error_message='broken image dependency' WHERE rollout_id=?`, parent.Rollout.ID)
			exec(`DELETE FROM instance_runtime_bindings WHERE instance_id=481`)
			var r *models.RuntimeRollout
			expected := 1
			if scenario == "rebuild-unbound" {
				r, err = u.RequestRecovery(ctx, 481, 1, true, RuntimeRecoveryRequest{Mode: "rebuild", ParentID: parent.Rollout.ID, Generation: 8, TargetImage: target, IdempotencyKey: "lifecycle-rebuild-481", ConfirmPreserve: true})
			} else {
				p, e := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target})
				if e != nil || !p.Passed || p.InstanceCount != 2 {
					t.Fatalf("healthy plan %+v %v", p, e)
				}
				r, err = u.ConfirmPreflight(ctx, stringValue(p.Rollout.PreflightID), p.TargetImageRef, nil)
				expected = 2
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = u.Prepare(ctx, r); err != nil {
				t.Fatal(err)
			}
			exec(`UPDATE runtime_rollouts SET status='running',started_at=NOW() WHERE id=?`, r.ID)
			name := runtimeUpgradeTargetDeploymentName("source", r.ID)
			ip, sourceIP := "10.1.2.3", "10.1.2.2"
			disabled := false
			caps, _ := json.Marshal(append(append([]string{}, openClawUpgradeRequiredCapabilities...), "openclaw.rebuild-preserve-v2", "openclaw.upgrade-operation-fence-v1"))
			exec(`INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,pod_ip,deployment_name,image_ref,image_digest,agent_endpoint,state,capacity,openclaw_version,session_store,agent_protocol_version,capabilities_json,last_seen_at)VALUES(320,'openclaw','tenant','target','target-uid','10.1.2.3',?,?,?,'http://10.1.2.3:19090','standby',100,'2026.10.2','sqlite','openclaw-upgrade-v3',?,NOW())`, name, r.TargetImageRef, stringValue(r.TargetImageDigest), string(caps))
			source := u.deployments.(recoveryInventory)[0]
			deployments := &fakeRuntimeDeploymentService{pods: []k8s.RuntimeDeploymentPod{
				{Namespace: "tenant", RuntimeType: "openclaw", DeploymentName: "source", PodName: "source-pod", PodUID: "source-uid", PodIP: &sourceIP, ImageRef: source.ImageRef, ImageDigest: stringValue(source.ImageDigest), State: "ready"},
				{Namespace: "tenant", RuntimeType: "openclaw", DeploymentName: name, PodName: "target", PodUID: "target-uid", PodIP: &ip, ImageRef: r.TargetImageRef, ImageDigest: stringValue(r.TargetImageDigest), State: "unhealthy", PoolRole: "upgrade", UpgradeID: fmt.Sprint(r.ID), SourceDeployment: "source", SchedulingEnabled: &disabled},
			}}
			a := &fallbackLifecycleAgent{&standbyFlowAgent{upgradeFlowAgent: &upgradeFlowAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, states: map[string]*RuntimeAgentGatewayState{"gw-482-8": {State: "running"}, "gw-484-8": {State: "running"}}}, s: sess, deployments: deployments, expectedItems: expected}}
			u.agent = a
			scheduler := NewRuntimeScheduler(repository.NewInstanceRepository(sess), u.pods, u.bindings, u.rollouts, a, nil, nil, deployments, time.Second, WithRuntimeSchedulerWorkspaceRoot(u.workspaceRoot), WithRuntimeSchedulerNamespace("tenant"))
			u.gatewayRestarter = scheduler
			u.deployments = scheduler
			scheduler.upgrade = u
			for round := 0; round < 40; round++ {
				fresh, e := u.rollouts.GetByID(ctx, r.ID)
				if e != nil {
					t.Fatal(e)
				}
				if fresh.Status == "finished" {
					break
				}
				if e = scheduler.finishRolloutIfReady(ctx, *fresh); e != nil {
					t.Fatal(e)
				}
			}
			fresh, err := u.rollouts.GetByID(ctx, r.ID)
			if err != nil || fresh.Status != "finished" || a.creates != expected {
				t.Fatalf("full lifecycle failed %+v creates=%d %v", fresh, a.creates, err)
			}
			if scenario == "upgrade-healthy-after-isolation" {
				b, e := u.bindings.GetByInstanceID(ctx, 481)
				if e != nil || b != nil {
					t.Fatal("isolated 481 was restarted")
				}
				var status string
				row, _ := sess.SQL().QueryRow(`SELECT status FROM instances WHERE id=481`)
				row.Scan(&status)
				if status != "error" {
					t.Fatal("isolated data/lifecycle changed")
				}
			}
			if deployments.scaleCalls != 0 {
				t.Fatal("source pool needed by excluded instances was removed")
			}
		})
	}
}

func TestUpgradeFallbackMySQLRetryBudgetAndFirstCause(t *testing.T) {
	sess, u, target := recoveryActionsDB(t)
	ctx := context.Background()
	p, err := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target, CandidateInstanceIDs: []int{481}, RecoveryMode: "retry"})
	if err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 5; n++ {
		sess.SQL().Exec(`UPDATE runtime_upgrade_items SET operation_next_retry_at=NULL WHERE rollout_id=?`, p.Rollout.ID)
		err = u.withUpgradeClaim(ctx, p.Rollout.ID, 481, "compatibility_check", func(context.Context) error { return fmt.Errorf("temporary connection refused attempt %d", n) })
		if n < 5 && !runtimeUpgradeInfrastructureRetryable(err) {
			t.Fatal(err)
		}
		if n == 5 && (!errors.Is(err, ErrUpgradeSourceState) || runtimeUpgradeInfrastructureRetryable(err)) {
			t.Fatalf("unbounded transient retries: %v", err)
		}
	}
	items, err := u.listUpgradeItems(ctx, p.Rollout.ID)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].OperationFailures != 5 || stringValue(items[0].ErrorMessage) != "temporary connection refused attempt 1" || stringValue(items[0].OperationLastError) != "temporary connection refused attempt 5" || items[0].OperationNextRetryAt == nil {
		t.Fatalf("missing diagnostics %+v", items[0])
	}
	called := false
	err = u.withUpgradeClaim(ctx, p.Rollout.ID, 481, "compatibility_check", func(context.Context) error { called = true; return nil })
	if !errors.Is(err, ErrRuntimeUpgradeBusy) || called {
		t.Fatal("backoff ignored", err)
	}
	// Forward retry backoff must never prevent an admitted rollback phase.
	called = false
	err = u.withUpgradeClaim(ctx, p.Rollout.ID, 481, "rollback_restore", func(context.Context) error { called = true; return nil })
	if err != nil || !called {
		t.Fatal("forward backoff blocked rollback", err)
	}
}

func TestUpgradeFallbackMySQLHealthyRollbackSiblingIsNotQuarantined(t *testing.T) {
	sess, u, target := recoveryActionsDB(t)
	ctx := context.Background()
	p, err := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target, CandidateInstanceIDs: []int{481, 482}, RecoveryMode: "retry"})
	if err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE runtime_rollouts SET status='error',phase='rollback_restore',rollback_status='needs_attention' WHERE id=?`, p.Rollout.ID)
	exec(`UPDATE runtime_upgrade_items SET state='restart_ready',recovery_status='needs_attention',source_generation=7 WHERE rollout_id=?`, p.Rollout.ID)
	exec(`UPDATE instance_runtime_bindings SET last_health_at=NOW() WHERE instance_id=482`)
	u.agent = &upgradeFlowAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, states: map[string]*RuntimeAgentGatewayState{"gw-482-8": {GatewayID: "gw-482-8", InstanceID: 482, Generation: 8, State: "running"}}}
	healthy, err := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target})
	if err != nil || !healthy.Passed || healthy.InstanceCount != 2 || len(healthy.Isolated) != 1 || healthy.Isolated[0].InstanceID != 481 {
		t.Fatalf("restored sibling blocked by old batch: %+v %v", healthy, err)
	}
	exec(`UPDATE instance_runtime_bindings SET last_health_at=NOW()-INTERVAL 10 MINUTE WHERE instance_id=482`)
	held, err := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target})
	if err != nil || held.InstanceCount != 1 || len(held.Isolated) != 2 {
		t.Fatalf("unverified sibling was allowed: %+v %v", held, err)
	}
}

func TestUpgradeFallbackMySQLLiveSourceAndMissingProofFailImmediately(t *testing.T) {
	sess, u, target := recoveryActionsDB(t)
	ctx := context.Background()
	p, err := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target, CandidateInstanceIDs: []int{481}, RecoveryMode: "retry"})
	if err != nil {
		t.Fatal(err)
	}
	sess.SQL().Exec(`UPDATE runtime_rollouts SET status='error' WHERE id=?`, p.Rollout.ID)
	sess.SQL().Exec(`UPDATE runtime_upgrade_items SET state='migration_started' WHERE rollout_id=?`, p.Rollout.ID)
	sess.SQL().Exec(`DELETE FROM instance_runtime_bindings WHERE instance_id=481`)
	u.agent = &upgradeFlowAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, states: map[string]*RuntimeAgentGatewayState{"gw-481-8": {State: "running"}}}
	raw, _ := json.Marshal(map[string]any{"recovery_mode": "rebuild", "recovery_parent_id": p.Rollout.ID, "candidate_instance_ids": []int{481}})
	r := &models.RuntimeRollout{RuntimeType: "openclaw", Status: "running", PreflightJSON: upgradeStringPtrOrNil(string(raw))}
	if err := u.rollouts.Create(ctx, r); err != nil {
		t.Fatal(err)
	}
	_, err = u.candidateForUpgradeItem(ctx, models.RuntimeUpgradeItem{RolloutID: r.ID, InstanceID: 481, State: "prepared"})
	if !errors.Is(err, ErrUpgradeSourceState) || runtimeUpgradeInfrastructureRetryable(err) {
		t.Fatalf("live source not held: %v", err)
	}
}
