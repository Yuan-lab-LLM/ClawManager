package services

import (
	"clawreef/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
)

type convergenceFlowAgent struct {
	*fallbackLifecycleAgent
	fail    int
	phase   string
	restore bool
}

func (a *convergenceFlowAgent) PreflightUpgradeCompatibility(ctx context.Context, endpoint string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentUpgradeCompatibility, error) {
	if req.InstanceID == a.fail && a.phase == "compatibility_check" {
		return nil, fmt.Errorf("%w: OPENCLAW_PLUGIN_CUSTOM_REVIEW_REQUIRED: user override", ErrRuntimeAgentConflict)
	}
	r, err := a.fallbackLifecycleAgent.PreflightUpgradeCompatibility(ctx, endpoint, req)
	if r != nil {
		r.ManagedPluginPlanSHA256 = strings.Repeat("d", 64)
	}
	return r, err
}
func (a *convergenceFlowAgent) MigrateSessionSQLite(ctx context.Context, endpoint string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentSessionSQLiteMigration, error) {
	if req.ManagedPluginPlanSHA256 != strings.Repeat("d", 64) {
		return nil, fmt.Errorf("compatibility plan was not passed to actual migration")
	}
	if req.InstanceID == a.fail && a.phase == "session_migration" {
		return nil, fmt.Errorf("%w: original data retained, migration refused", ErrRuntimeAgentConflict)
	}
	return a.fallbackLifecycleAgent.MigrateSessionSQLite(ctx, endpoint, req)
}
func (a *convergenceFlowAgent) RestoreSessionSQLite(_ context.Context, _ string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentSessionSQLiteRestore, error) {
	if a.restore {
		return &RuntimeAgentSessionSQLiteRestore{Status: "restored", ConfigRestored: true, StateRestored: true, PreservedSessionSQLite: req.PreserveSessionSQLite}, nil
	}
	return nil, fmt.Errorf("%w: no safe restore available", ErrRuntimeAgentConflict)
}

func (a *convergenceFlowAgent) ActivateUpgrade(ctx context.Context, endpoint, id string) error {
	var eligible, validated int
	row, err := a.s.SQL().QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(state='migrated'),0) FROM runtime_upgrade_items WHERE rollout_id=? AND isolated_at IS NULL`, id)
	if err != nil {
		return err
	}
	if err = row.Scan(&eligible, &validated); err != nil {
		return err
	}
	if eligible == 0 || eligible != validated {
		return fmt.Errorf("activation before eligible migrations completed")
	}
	a.activations++
	for index := 1; index < len(a.deployments.pods); index++ {
		p := &a.deployments.pods[index]
		if p.PodIP != nil && endpoint == "http://"+*p.PodIP+":19090" {
			p.State = "ready"
		}
	}
	_, err = a.s.SQL().ExecContext(ctx, "UPDATE runtime_pods SET state='ready',last_seen_at=NOW() WHERE agent_endpoint=?", endpoint)
	return err
}

// Real admission -> controller -> MySQL reservations -> commit. Only external
// Runtime/Kubernetes are simulated. A sibling failure must not skip good work.
func TestUpgradeConvergenceMySQLPartialScheduler(t *testing.T) {
	for _, phase := range []string{"compatibility_check", "session_migration", "restore_source"} {
		t.Run(phase, func(t *testing.T) {
			sess, u, target := recoveryActionsDB(t)
			ctx := context.Background()
			exec := func(q string, args ...any) {
				t.Helper()
				if _, e := sess.SQL().Exec(q, args...); e != nil {
					t.Fatal(e)
				}
			}
			p, err := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target, ExcludeInstanceIDs: []int{481}, BatchSize: 2, AutoRollback: true})
			if err != nil || !p.Passed {
				t.Fatalf("%+v %v", p, err)
			}
			r, err := u.ConfirmPreflight(ctx, stringValue(p.Rollout.PreflightID), p.TargetImageRef, nil)
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
			caps, _ := json.Marshal(append(append([]string{}, openClawUpgradeRequiredCapabilities...), "openclaw.upgrade-operation-fence-v1", "openclaw.managed-plugin-convergence-v1"))
			exec(`INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,pod_ip,deployment_name,image_ref,image_digest,agent_endpoint,state,capacity,openclaw_version,session_store,agent_protocol_version,capabilities_json,last_seen_at)VALUES(320,'openclaw','tenant','target','target-uid','10.1.2.3',?,?,?,'http://10.1.2.3:19090','standby',100,'2026.10.2','sqlite','openclaw-upgrade-v3',?,NOW())`, name, r.TargetImageRef, stringValue(r.TargetImageDigest), string(caps))
			source := u.deployments.(recoveryInventory)[0]
			deployments := &fakeRuntimeDeploymentService{pods: []k8s.RuntimeDeploymentPod{{Namespace: "tenant", RuntimeType: "openclaw", DeploymentName: "source", PodName: "source-pod", PodUID: "source-uid", PodIP: &sourceIP, ImageRef: source.ImageRef, ImageDigest: stringValue(source.ImageDigest), State: "ready"}, {Namespace: "tenant", RuntimeType: "openclaw", DeploymentName: name, PodName: "target", PodUID: "target-uid", PodIP: &ip, ImageRef: r.TargetImageRef, ImageDigest: stringValue(r.TargetImageDigest), State: "unhealthy", PoolRole: "upgrade", UpgradeID: fmt.Sprint(r.ID), SourceDeployment: "source", SchedulingEnabled: &disabled}}}
			base := &standbyFlowAgent{upgradeFlowAgent: &upgradeFlowAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, states: map[string]*RuntimeAgentGatewayState{"gw-482-8": {GatewayID: "gw-482-8", InstanceID: 482, Generation: 8, State: "running"}, "gw-484-8": {GatewayID: "gw-484-8", InstanceID: 484, Generation: 8, State: "running"}}}, s: sess, deployments: deployments, expectedItems: 1}
			a := &convergenceFlowAgent{fallbackLifecycleAgent: &fallbackLifecycleAgent{base}, fail: 482, phase: phase}
			if phase == "restore_source" {
				a.phase = "session_migration"
				a.restore = true
			}
			u.agent = a
			scheduler := NewRuntimeScheduler(repository.NewInstanceRepository(sess), u.pods, u.bindings, u.rollouts, a, nil, nil, deployments, time.Second, WithRuntimeSchedulerWorkspaceRoot(u.workspaceRoot), WithRuntimeSchedulerNamespace("tenant"))
			u.gatewayRestarter = scheduler
			u.deployments = scheduler
			scheduler.upgrade = u
			for round := 0; round < 35; round++ {
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
			fresh, _ := u.rollouts.GetByID(ctx, r.ID)
			if fresh.Status != "finished" || fresh.FinishedAt == nil || !strings.Contains(stringValue(fresh.ErrorMessage), "隔离") {
				t.Fatalf("batch never converged %+v", fresh)
			}
			items, e := u.listUpgradeItems(ctx, r.ID)
			if e != nil {
				t.Fatal(e)
			}
			for _, i := range items {
				if i.InstanceID == 482 {
					if i.IsolatedAt == nil || i.State == "verified" {
						t.Fatalf("failed item mislabeled %+v", i)
					}
				} else if i.State != "verified" {
					t.Fatalf("healthy item lost %+v", i)
				}
			}
			b, _ := u.bindings.GetByInstanceID(ctx, 484)
			if b == nil || b.RuntimePodID != 320 {
				t.Fatal("healthy instance did not reach target")
			}
			isolated, e := u.isolatedUpgradeInstances(ctx)
			if e != nil || len(isolated) != 1 || isolated[0].InstanceID != 482 {
				t.Fatalf("quarantine lost %+v %v", isolated, e)
			}
			info, e := u.RecoveryInfo(ctx, 482)
			if e != nil || !info.Isolated || (phase != "restore_source" && !info.CanRebuild) {
				t.Fatalf("no repair option %+v %v", info, e)
			}
			if deployments.scaleCalls != 0 {
				t.Fatal("source needed by failed instance was removed")
			}
			if phase == "restore_source" {
				if u.InstanceBlocked(ctx, "openclaw", 482) {
					t.Fatal("restored source cannot be restarted independently")
				}
				pool, e := u.filterUpgradePlacement(ctx, 482, []models.RuntimePod{source, {ID: 320, ImageRef: r.TargetImageRef, ImageDigest: r.TargetImageDigest}})
				if e != nil || len(pool) != 1 || pool[0].ID != 319 {
					t.Fatalf("restored source affinity lost %+v %v", pool, e)
				}
			}
		})
	}
}

func TestUpgradeConvergenceMySQLGroupAndDeadline(t *testing.T) {
	sess, u, target := recoveryActionsDB(t)
	ctx := context.Background()
	p, e := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target, ExcludeInstanceIDs: []int{481}})
	if e != nil {
		t.Fatal(e)
	}
	sess.SQL().Exec(`UPDATE runtime_upgrade_items SET team_id=7,state='prepared' WHERE rollout_id=?`, p.Rollout.ID)
	items, _ := u.listUpgradeItems(ctx, p.Rollout.ID)
	if e = u.runConvergentUpgradeItems(ctx, p.Rollout, "compatibility_check", items, 1, func(context.Context, int) error { return fmt.Errorf("%w: custom plugin", ErrRuntimeAgentConflict) }); e != nil {
		t.Fatal(e)
	}
	items, _ = u.listUpgradeItems(ctx, p.Rollout.ID)
	if len(activeUpgradeItems(items)) != 0 {
		t.Fatal("Team was only half isolated")
	}
	if _, e = u.finishConvergentUpgrade(ctx, p.Rollout); e != nil {
		t.Fatal(e)
	}
	// A renewed scheduling claim is not real progress and cannot reset expiry.
	sess.SQL().Exec(`UPDATE runtime_upgrade_items SET isolated_at=NULL,team_id=NULL,operation_phase='compatibility_check',phase_entered_at=NOW()-INTERVAL 21 MINUTE WHERE rollout_id=?`, p.Rollout.ID)
	items, _ = u.listUpgradeItems(ctx, p.Rollout.ID)
	called := false
	e = u.runConvergentUpgradeItems(ctx, p.Rollout, "compatibility_check", items, 1, func(context.Context, int) error { called = true; return nil })
	if e != nil || called {
		t.Fatalf("expired work was dispatched: %v", e)
	}
	items, _ = u.listUpgradeItems(ctx, p.Rollout.ID)
	if len(activeUpgradeItems(items)) != 0 {
		t.Fatal("expired items still hold the fleet")
	}
}

func TestUpgradeConvergenceMySQLImageCircuit(t *testing.T) {
	_, u, target := recoveryActionsDB(t)
	ctx := context.Background()
	p, e := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target, ExcludeInstanceIDs: []int{481}})
	if e != nil {
		t.Fatal(e)
	}
	fingerprint := upgradeFailureFingerprint(fmt.Errorf("%w: OPENCLAW_UPGRADE_DOCTOR_FAILED: incompatible image", ErrRuntimeAgentConflict))
	if fingerprint == "" {
		t.Fatal("image signature missing")
	}
	for n := 0; n < 3; n++ {
		if e = u.audit(ctx, &p.Rollout.ID, nil, "upgrade_group_isolated", "compatibility_check", "needs_attention", map[string]any{"group": "instance:482", "fingerprint": fingerprint}); e != nil {
			t.Fatal(e)
		}
	}
	if open, e := u.upgradeImageCircuitOpen(ctx, p.Rollout); e != nil || open {
		t.Fatal("one bad instance tripped image circuit", e)
	}
	for _, id := range []int{484, 522} {
		u.audit(ctx, &p.Rollout.ID, nil, "upgrade_group_isolated", "compatibility_check", "needs_attention", map[string]any{"group": fmt.Sprintf("instance:%d", id), "fingerprint": fingerprint})
	}
	if open, e := u.upgradeImageCircuitOpen(ctx, p.Rollout); e != nil || !open {
		t.Fatal("common bad image keeps touching more instances", e)
	}
	if fp := upgradeFailureFingerprint(fmt.Errorf("%w: OPENCLAW_PLUGIN_CUSTOM_REVIEW_REQUIRED: user override", ErrRuntimeAgentConflict)); fp != "" {
		t.Fatal("custom plugin conflict blamed on image")
	}
}
