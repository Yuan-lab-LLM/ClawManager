package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/upper/db/v4"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

type recoveryInventory []models.RuntimePod

func (p recoveryInventory) RuntimeDeploymentPods(context.Context, string) ([]models.RuntimePod, error) {
	return p, nil
}

func recoveryActionsDB(t *testing.T) (db.Session, *RuntimeUpgradeService, string) {
	t.Helper()
	sess := upgradeFlowDB(t)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`ALTER TABLE instances ADD COLUMN description TEXT NULL`)
	exec(`INSERT INTO users(id) VALUES (1)`)
	exec(`ALTER TABLE team_members ADD COLUMN instance_id INT,ADD COLUMN team_id INT,ADD COLUMN member_key VARCHAR(64),ADD COLUMN role VARCHAR(32),ADD COLUMN runtime_type VARCHAR(32),ADD COLUMN availability VARCHAR(32),ADD COLUMN status VARCHAR(32)`)
	for _, name := range []string{"team_tasks", "team_work_items", "team_event_outbox"} {
		exec("CREATE TABLE " + name + " (id INT PRIMARY KEY,team_id INT,status VARCHAR(32))")
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/manifests/") {
			w.Header().Set("Docker-Content-Digest", digest)
			w.Write([]byte(`{"schemaVersion":2,"config":{"digest":"sha256:` + strings.Repeat("b", 64) + `"}}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"config": map[string]any{"Labels": map[string]string{"io.clawmanager.runtime.type": "openclaw", "io.clawmanager.openclaw.version": "2026.10.2", "io.clawmanager.upgrade.strategy": openClawDataSafeImageStrategy, "io.clawmanager.upgrade.protocol": openClawDataSafeProtocol}}})
	}))
	t.Cleanup(server.Close)
	host := strings.TrimPrefix(server.URL, "http://")
	source := host + "/agentsruntime/openclaw-lite@sha256:" + strings.Repeat("c", 64)
	root := t.TempDir()
	caps, _ := json.Marshal(openClawUpgradeRequiredCapabilities)
	exec(`INSERT INTO runtime_pods (id,runtime_type,namespace,pod_name,pod_uid,pod_ip,deployment_name,image_ref,image_digest,agent_endpoint,state,capacity,openclaw_version,capabilities_json,last_seen_at) VALUES (319,'openclaw','tenant','source-pod','source-uid','10.1.2.2','source',?,?,'http://10.1.2.2:19090','ready',100,'2026.10.2',?,NOW())`, source, "sha256:"+strings.Repeat("c", 64), string(caps))
	for _, id := range []int{481, 482, 484} {
		path := RuntimeWorkspacePathWithRoot(root, "openclaw", 1, id)
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		state := "running"
		if id == 481 {
			state = "error"
		}
		exec(`INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,workspace_path,updated_at)VALUES (?,1,'openclaw','gateway','lite',?,8,?,NOW())`, id, state, path)
		exec(`INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,state,generation,workspace_path)VALUES (?,319,'openclaw',?,?,?,8,?)`, id, fmt.Sprintf("gw-%d-8", id), 20000+(id-481)*3, state, path)
	}
	repo := repository.NewRuntimeRolloutRepository(sess)
	u := NewRuntimeUpgradeService(sess, repo, repository.NewRuntimePodRepository(sess), repository.NewInstanceRuntimeBindingRepository(sess), nil, root, "")
	ip, endpoint, version := "10.1.2.2", "http://10.1.2.2:19090", "2026.10.2"
	u.deployments = recoveryInventory{{ID: 319, RuntimeType: "openclaw", Namespace: "tenant", DeploymentName: "source", PodName: "source-pod", PodUID: upgradeStringPtrOrNil("source-uid"), PodIP: &ip, AgentEndpoint: &endpoint, State: "ready", ImageRef: source, ImageDigest: upgradeStringPtrOrNil("sha256:" + strings.Repeat("c", 64)), OpenClawVersion: &version}}
	return sess, u, host + "/agentsruntime/openclaw-lite:repair"
}

func TestRecoveryActionsMySQLAdmissionAndIndependentReset(t *testing.T) {
	sess, u, target := recoveryActionsDB(t)
	ctx := context.Background()
	actor := 1
	ordinary, err := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target, ActorUserID: &actor})
	if err != nil {
		t.Fatal(err)
	}
	if ordinary.Passed || len(ordinary.Unavailable) != 1 || ordinary.Unavailable[0].InstanceID != 481 {
		t.Fatalf("unavailable contract: %+v", ordinary)
	}
	healthy, err := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target, ExcludeInstanceIDs: []int{481}})
	if err != nil {
		t.Fatal(err)
	}
	if !healthy.Passed || healthy.InstanceCount != 2 {
		t.Fatalf("healthy members still globally blocked: %+v", healthy)
	}
	info, err := u.RecoveryInfo(ctx, 481)
	if err != nil || !info.CanRetry {
		t.Fatalf("info: %+v %v", info, err)
	}
	req := RuntimeRecoveryRequest{Mode: "retry", ParentID: info.ParentID, Generation: 8, TargetImage: target, IdempotencyKey: "click-retry-1"}
	child, err := u.RequestRecovery(ctx, 481, 1, true, req)
	if err != nil {
		t.Fatal(err)
	}
	if child.Status != "pending" || runtimeUpgradeScopeFromRollout(child).RecoveryMode != "retry" {
		t.Fatalf("not queued: %+v", child)
	}
	items, err := u.listUpgradeItems(ctx, child.ID)
	if err != nil || len(items) != 1 || items[0].InstanceID != 481 {
		t.Fatalf("not a single-instance recovery: %+v %v", items, err)
	}
	repeat, err := u.RequestRecovery(ctx, 481, 1, true, req)
	if err != nil || repeat.ID != child.ID {
		t.Fatalf("duplicate click: %+v %v", repeat, err)
	}
	altered := req
	altered.Mode = "rebuild"
	altered.ConfirmPreserve = true
	if _, err := u.RequestRecovery(ctx, 481, 1, true, altered); err == nil {
		t.Fatal("idempotency key reused for a different action")
	}
	info, err = u.RecoveryInfo(ctx, 481)
	if err != nil || info.CanRetry || info.CanRebuild || info.ParentID != child.ID {
		t.Fatalf("active recovery lost ownership: %+v %v", info, err)
	}
	var writes bool
	if err := u.WithInstanceMutation(ctx, 481, func() error { writes = true; return nil }); !errors.Is(err, ErrWorkspaceUpgradeBusy) || writes {
		t.Fatalf("editor bypassed active recovery: %v", err)
	}
	if err := u.WithInstanceMutation(ctx, 482, func() error { return nil }); err != nil {
		t.Fatal("unselected instance blocked", err)
	}
	// Overlapping scope denied without consuming the executable preflight.
	overlap, err := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target, CandidateInstanceIDs: []int{481}, RecoveryMode: "retry"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := u.ConfirmPreflight(ctx, stringValue(overlap.Rollout.PreflightID), overlap.TargetImageRef, &actor); err == nil {
		t.Fatal("overlapping upgrade admitted")
	}
	if _, err := sess.SQL().Exec(`UPDATE runtime_rollouts SET status='error',finished_at=NOW() WHERE id=?`, child.ID); err != nil {
		t.Fatal(err)
	}
	info, err = u.RecoveryInfo(ctx, 481)
	if err != nil || !info.CanRebuild || info.ParentID != child.ID {
		t.Fatalf("failed retry lacks rebuild: %+v %v", info, err)
	}
	rebuild := RuntimeRecoveryRequest{Mode: "rebuild", ParentID: child.ID, Generation: 8, TargetImage: target, IdempotencyKey: "click-rebuild-2", ConfirmPreserve: true}
	next, err := u.RequestRecovery(ctx, 481, 1, true, rebuild)
	if err != nil || next.Status != "pending" {
		t.Fatalf("rebuild admission %+v %v", next, err)
	}
	var status string
	var generation int
	row, _ := sess.SQL().QueryRow(`SELECT status,runtime_generation FROM instances WHERE id=481`)
	row.Scan(&status, &generation)
	if status != "error" || generation != 8 {
		t.Fatal("HTTP admission mutated instance data/lifecycle")
	}
}

func TestUpgradeHeldFailureMySQLHasBoundedStateAndRetainsBindings(t *testing.T) {
	sess, u, target := recoveryActionsDB(t)
	ctx := context.Background()
	p, err := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target, CandidateInstanceIDs: []int{481}, RecoveryMode: "retry"})
	if err != nil {
		t.Fatal(err)
	}
	id := p.Rollout.ID
	for _, q := range []string{`UPDATE runtime_rollouts SET status='running',phase='gateway_restart'`, `UPDATE runtime_upgrade_items SET state='restart_ready',recovery_status='needs_attention',source_generation=7`} {
		if _, err := sess.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	reason, err := u.upgradeWaitingFailure(ctx, p.Rollout)
	if err != nil || reason == "" {
		t.Fatalf("held forever: %q %v", reason, err)
	}
	if err := u.holdFailedUpgrade(ctx, p.Rollout, reason); err != nil {
		t.Fatal(err)
	}
	r, err := u.rollouts.GetByID(ctx, id)
	if err != nil || r.Status != "error" || r.FinishedAt == nil {
		t.Fatalf("unbounded rollout: %+v %v", r, err)
	}
	b, err := u.bindings.GetByInstanceID(ctx, 481)
	if err != nil || b == nil || b.RuntimePodID != 319 {
		t.Fatal("failure destroyed writer evidence")
	}
	// Only actual item progress resets the budget, not repeated controller claims.
	sess.SQL().Exec(`UPDATE runtime_upgrade_items SET recovery_status=NULL,updated_at=NOW()-INTERVAL 21 MINUTE`)
	reason, err = u.upgradeWaitingFailure(ctx, r)
	if err != nil || reason == "" {
		t.Fatal("progress deadline missing", err)
	}
	sess.SQL().Exec(`UPDATE runtime_upgrade_items SET updated_at=?`, time.Now())
	reason, err = u.upgradeWaitingFailure(ctx, r)
	if err != nil || reason != "" {
		t.Fatal("progressing batch incorrectly timed out", err)
	}
}

func TestRecoveryPlacementMySQLDoesNotUpgradeExcludedInstancesOnRestart(t *testing.T) {
	sess, u, target := recoveryActionsDB(t)
	ctx := context.Background()
	p, err := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target, ExcludeInstanceIDs: []int{481}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.SQL().Exec(`UPDATE runtime_rollouts SET status='finished' WHERE id=?`, p.Rollout.ID); err != nil {
		t.Fatal(err)
	}
	source := u.deployments.(recoveryInventory)[0]
	newPod := source
	newPod.ID = 320
	newPod.DeploymentName = runtimeUpgradeTargetDeploymentName("source", p.Rollout.ID)
	newPod.ImageRef = p.Rollout.TargetImageRef
	newPod.ImageDigest = p.Rollout.TargetImageDigest
	for _, tc := range []struct {
		id   int
		want int64
	}{{481, 319}, {482, 320}, {9999, 320}} {
		result, err := u.filterUpgradePlacement(ctx, tc.id, []models.RuntimePod{source, newPod})
		if err != nil || len(result) != 1 || result[0].ID != tc.want {
			t.Fatalf("instance %d wrong core placement %+v %v", tc.id, result, err)
		}
	}
}

func TestRecoveryAdmissionMySQLProtectsTeamButDoesNotReplaceTeammates(t *testing.T) {
	sess, u, target := recoveryActionsDB(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO teams(id)VALUES(154)`)
	exec(`INSERT INTO team_members(id,instance_id,team_id,member_key,role,runtime_type,availability,status)VALUES(1,481,154,'worker','worker','openclaw','idle','idle'),(2,482,154,'leader','leader','openclaw','idle','idle')`)
	ordinary, err := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target})
	if err != nil {
		t.Fatal(err)
	}
	if len(ordinary.Unavailable) != 2 {
		t.Fatalf("partial Team exclusion exposed: %+v", ordinary)
	}
	partial, err := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target, ExcludeInstanceIDs: []int{481}})
	if err != nil {
		t.Fatal(err)
	}
	if partial.Passed {
		t.Fatal("half a Team was excluded")
	}
	healthy, err := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target, ExcludeInstanceIDs: []int{481, 482}})
	if err != nil || !healthy.Passed || healthy.InstanceCount != 1 {
		t.Fatalf("Team exclusion blocks unrelated instance: %+v %v", healthy, err)
	}
	info, err := u.RecoveryInfo(ctx, 481)
	if err != nil {
		t.Fatal(err)
	}
	r, err := u.RequestRecovery(ctx, 481, 1, false, RuntimeRecoveryRequest{Mode: "retry", Generation: 8, ParentID: info.ParentID, TargetImage: info.TargetImage, IdempotencyKey: "team-single-repair"})
	if err != nil || r.Status != "pending" {
		t.Fatalf("scoped Team member failed %+v %v", r, err)
	}
	items, err := u.listUpgradeItems(ctx, r.ID)
	if err != nil || len(items) != 1 || items[0].InstanceID != 481 || items[0].TeamID == nil {
		t.Fatalf("healthy teammate was included %+v %v", items, err)
	}
	if err := u.WithWorkspaceMutation(ctx, WorkspaceFileScope{TeamID: 154}, func() error { t.Fatal("Team editor bypassed recovery fence"); return nil }); !errors.Is(err, ErrWorkspaceUpgradeBusy) {
		t.Fatal(err)
	}
}

type recoveryMaintenanceSpy struct{ released []int }

func (s *recoveryMaintenanceSpy) SetRuntimeUpgradeMaintenance(_ context.Context, ids []int, _ int64, enabled bool) error {
	if !enabled {
		s.released = append(s.released, ids...)
	}
	return nil
}

func TestRecoveryPostflightMySQLDoesNotReleaseAnotherFailedMember(t *testing.T) {
	sess, u, target := recoveryActionsDB(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO teams(id)VALUES(154)`)
	exec(`INSERT INTO team_members(id,instance_id,team_id,member_key,role,runtime_type,availability,status)VALUES(1,481,154,'worker','worker','openclaw','idle','idle'),(2,482,154,'leader','leader','openclaw','idle','idle')`)
	p, err := u.Preflight(ctx, RuntimeUpgradePreflightRequest{TargetImageRef: target, CandidateInstanceIDs: []int{481}, RecoveryMode: "retry"})
	if err != nil || !p.Passed {
		t.Fatalf("%+v %v", p, err)
	}
	r := p.Rollout
	r.Status = "running"
	r.Phase = "postflight"
	exec(`UPDATE runtime_rollouts SET status='running',phase='postflight' WHERE id=?`, r.ID)
	exec(`UPDATE runtime_upgrade_items SET state='gateway_verified' WHERE rollout_id=?`, r.ID)
	exec(`UPDATE instances SET status=CASE WHEN id=481 THEN 'running' ELSE 'error' END WHERE id IN(481,482)`)
	spy := &recoveryMaintenanceSpy{}
	u.teamMaintenance = spy
	version, protocol, store := "2026.10.2", openClawDataSafeProtocol, "sqlite"
	caps, _ := json.Marshal(openClawUpgradeRequiredCapabilities)
	raw := string(caps)
	pods := []models.RuntimePod{{RuntimeType: "openclaw", Namespace: "tenant", DeploymentName: runtimeUpgradeTargetDeploymentName("source", r.ID), ImageRef: r.TargetImageRef, ImageDigest: r.TargetImageDigest, State: "ready", Capacity: 100, OpenClawVersion: &version, AgentProtocolVersion: &protocol, SessionStore: &store, CapabilitiesJSON: &raw}}
	done, err := u.ValidateTargetRuntime(ctx, r, pods)
	if err != nil || !done || len(spy.released) != 0 {
		t.Fatalf("released failed teammate: done=%v released=%v err=%v", done, spy.released, err)
	}
	exec(`UPDATE instances SET status='running' WHERE id=482`)
	done, err = u.ValidateTargetRuntime(ctx, r, pods)
	if err != nil || !done || len(spy.released) != 1 || spy.released[0] != 154 {
		t.Fatalf("healthy Team not released: %v %v %v", done, spy.released, err)
	}
}
