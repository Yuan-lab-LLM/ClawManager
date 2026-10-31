package services

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
)

func ordinaryPlacementFixture() (models.RuntimePod, k8s.RuntimeDeploymentPod) {
	now := time.Now().UTC()
	ip, endpoint, uid := "10.1.2.3", "http://10.1.2.3:19090", "current-uid"
	image := "registry/openclaw@sha256:" + strings.Repeat("b", 64)
	return models.RuntimePod{ID: 12, RuntimeType: "openclaw", Namespace: "tenant", DeploymentName: "pool", PodName: "current-pod", PodUID: &uid, PodIP: &ip, AgentEndpoint: &endpoint, ImageRef: image, State: "ready", LastSeenAt: &now, Capacity: 100},
		k8s.RuntimeDeploymentPod{RuntimeType: "openclaw", Namespace: "tenant", DeploymentName: "pool", PodName: "current-pod", PodUID: uid, PodIP: &ip, ImageRef: image, ImageDigest: "sha256:" + strings.Repeat("b", 64), State: "ready", DesiredReplicas: 1}
}

func TestOrdinaryPlacementMySQLDefaultScopeAndBirthFence(t *testing.T) {
	sess := upgradeFlowDB(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	start := now.Add(-time.Minute)
	exec(`INSERT INTO instances(id,type,runtime_generation,created_at)VALUES(901,'openclaw',1,?),(902,'openclaw',9,?),(903,'openclaw',2,?)`, now, now.Add(-time.Hour), now.Add(-30*time.Second))
	repo := repository.NewRuntimeRolloutRepository(sess)
	u := NewRuntimeUpgradeService(sess, repo, repository.NewRuntimePodRepository(sess), repository.NewInstanceRuntimeBindingRepository(sess), nil, t.TempDir(), "")
	image := "registry/openclaw@sha256:" + strings.Repeat("b", 64)
	source := `{"tenant/pool":"registry/old"}`
	r := models.RuntimeRollout{RuntimeType: "openclaw", TargetImageRef: image, SourceImagesJSON: &source, Phase: RuntimeUpgradePhaseEmptyPoolReset, Status: "finished", StartedAt: &start, FinishedAt: &now}
	if err := repo.Create(ctx, &r); err != nil {
		t.Fatal(err)
	}
	pod, _ := ordinaryPlacementFixture()
	for _, scenario := range []string{"running", "error", "preflight", "lab", "recovery", "scoped", "legacy-followup", "second-switch"} {
		t.Run(scenario, func(t *testing.T) {
			later := r
			later.ID = 0
			later.TargetImageRef = "registry/openclaw@sha256:" + strings.Repeat("c", 64)
			switch scenario {
			case "running":
				later.Status = "running"
			case "error":
				later.Status = "error"
			case "preflight":
				later.Status = "preflight_passed"
			case "lab":
				later.PreflightID = ptrString("lab")
				later.Phase = "postflight"
				later.PreflightJSON = ptrString(`{"upgrade_lab_run_id":12}`)
			case "recovery":
				later.PreflightID = ptrString("recovery")
				later.Phase = "postflight"
				later.PreflightJSON = ptrString(`{"recovery_mode":"retry","candidate_instance_ids":[12]}`)
			case "scoped":
				later.PreflightID = ptrString("scoped")
				later.Phase = "postflight"
				later.PreflightJSON = ptrString(`{"candidate_instance_ids":[12]}`)
				started := now.Add(-10 * time.Second)
				later.StartedAt = &started
				later.SourceImagesJSON = ptrString(`{"tenant/pool":"` + image + `"}`)
			case "legacy-followup":
				later.Phase = "requested"
				later.SourceImagesJSON = nil // Production #148: BOTH fields are NULL.
			}
			if err := repo.Create(ctx, &later); err != nil {
				t.Fatal(err)
			}
			defer exec(`DELETE FROM runtime_rollouts WHERE id=?`, later.ID)
			expected := pod
			if scenario == "second-switch" || scenario == "legacy-followup" || scenario == "scoped" {
				expected.ImageRef = later.TargetImageRef
			}
			if scenario == "scoped" {
				expected.DeploymentName = runtimeUpgradeTargetDeploymentName("pool", later.ID)
			}
			if scenario == "legacy-followup" {
				expected.KubernetesReady = true
				expected.KubernetesPodUID = expected.PodUID
			}
			got, err := u.filterUpgradePlacement(ctx, 901, []models.RuntimePod{pod, expected})
			if err != nil || len(got) == 0 {
				t.Fatalf("wrong default: %+v %v", got, err)
			}
			for _, p := range got {
				if p.ImageRef != expected.ImageRef {
					t.Fatal("old or unrelated rollout became default")
				}
			}
			if scenario == "legacy-followup" {
				unverified := expected
				unverified.KubernetesReady = false
				unrelated := expected
				unrelated.DeploymentName = "unrelated-pool"
				if rejected, err := u.filterUpgradePlacement(ctx, 901, []models.RuntimePod{unverified, unrelated}); err != nil || len(rejected) != 0 {
					t.Fatalf("legacy fallback widened scope or trusted registry: %+v %v", rejected, err)
				}
				if old, err := u.filterUpgradePlacement(ctx, 902, []models.RuntimePod{expected}); err == nil || len(old) != 0 {
					t.Fatal("legacy fallback lost original birth fence")
				}
			}
			if scenario == "scoped" {
				old, err := u.filterUpgradePlacement(ctx, 903, []models.RuntimePod{pod, expected})
				if err != nil || len(old) != 1 || old[0].ImageRef != image {
					t.Fatalf("unselected existing instance skipped migration: %+v %v", old, err)
				}
				if got, err := u.filterUpgradePlacement(ctx, 902, []models.RuntimePod{pod, expected}); err == nil || len(got) != 0 {
					t.Fatal("pre-reset data bypassed birth fence")
				}
			}
		})
	}
	if got, err := u.filterUpgradePlacement(ctx, 902, []models.RuntimePod{pod}); err == nil || len(got) != 0 {
		t.Fatal("old unproven data bypassed compatibility fence")
	}
	// No target is a bounded wait/no-candidate result, never a legacy-image fallback.
	pod.ImageRef = "registry/old"
	if got, err := u.filterUpgradePlacement(ctx, 901, []models.RuntimePod{pod}); err != nil || len(got) != 0 {
		t.Fatalf("wrong-image fallback: %+v %v", got, err)
	}
}

func TestOrdinaryPlacementMySQLFreshAtomicCapacity(t *testing.T) {
	sess := upgradeFlowDB(t)
	ctx := context.Background()
	repo := repository.NewRuntimePodRepository(sess)
	pod, _ := ordinaryPlacementFixture()
	pod.Capacity = 2
	if err := repo.UpsertFromAgent(ctx, &pod); err != nil {
		t.Fatal(err)
	}
	claim := repo.(freshRuntimePodClaimer)
	cutoff := time.Now().Add(-time.Minute)
	if _, err := sess.SQL().Exec(`UPDATE runtime_pods SET last_seen_at=? WHERE id=?`, cutoff.Add(-time.Second), pod.ID); err != nil {
		t.Fatal(err)
	}
	if ok, err := claim.TryClaimSlotSince(ctx, pod.ID, cutoff); err != nil || ok {
		t.Fatal("stale SELECT snapshot claimed a slot", err)
	}
	if err := repo.UpdateHeartbeat(ctx, pod.ID, "ready", 0, 2, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := claim.TryClaimSlotSince(ctx, pod.ID, cutoff)
			if err != nil {
				t.Error(err)
			}
			if ok {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 2 {
		t.Fatalf("over/under capacity: %d", accepted.Load())
	}
}

func TestOrdinaryPlacementCompletionChecksActualRoute(t *testing.T) {
	pod, live := ordinaryPlacementFixture()
	source := `{"tenant/pool":"registry/old"}`
	r := models.RuntimeRollout{RuntimeType: "openclaw", TargetImageRef: pod.ImageRef, SourceImagesJSON: &source, Phase: RuntimeUpgradePhaseEmptyPoolReset}
	repo := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{pod.ID: &pod}}
	dep := &fakeRuntimeDeploymentService{pods: []k8s.RuntimeDeploymentPod{live}}
	s := NewRuntimeScheduler(nil, repo, nil, nil, nil, nil, nil, dep, time.Second, WithRuntimeSchedulerNamespace("tenant"))
	if reason, err := s.emptyPoolPlacementCompletionReason(context.Background(), r); err != nil || reason != "" {
		t.Fatalf("valid route: %s %v", reason, err)
	}
	pod.AgentEndpoint = ptrString("http://10.1.2.99:19090")
	if reason, err := s.emptyPoolPlacementCompletionReason(context.Background(), r); err != nil || reason == "" {
		t.Fatal("ready image with broken route was accepted")
	}
}

type changingOrdinaryInventory struct {
	fakeRuntimeDeploymentService
	calls int
}

func (d *changingOrdinaryInventory) ListPods(ctx context.Context, namespace, runtimeType string) ([]k8s.RuntimeDeploymentPod, error) {
	d.calls++
	if d.calls == 3 {
		d.pods[0].PodUID = "replacement-uid"
	}
	return d.fakeRuntimeDeploymentService.ListPods(ctx, namespace, runtimeType)
}

func TestOrdinaryPlacementPodChangesAfterReservation(t *testing.T) {
	pod, live := ordinaryPlacementFixture()
	r := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{pod.ID: &pod}, schedulable: []models.RuntimePod{pod}}
	bindings := newFakeRuntimeBindingRepo()
	agent := &fakeRuntimeAgentClient{}
	dep := &changingOrdinaryInventory{fakeRuntimeDeploymentService: fakeRuntimeDeploymentService{pods: []k8s.RuntimeDeploymentPod{live}}}
	root := canonicalCompatibilityTestDir(t)
	path := RuntimeWorkspacePathWithRoot(root, "openclaw", 1, 901)
	i := models.Instance{ID: 901, UserID: 1, Type: "openclaw", RuntimeType: "gateway", InstanceMode: "lite", RuntimeGeneration: 1, WorkspacePath: &path}
	s := NewRuntimeScheduler(newFakeRuntimeInstanceRepo(), r, bindings, nil, agent, nil, nil, dep, time.Second, WithRuntimeSchedulerNamespace("tenant"), WithRuntimeSchedulerWorkspaceRoot(root), WithRuntimeSchedulerGatewayEnvBuilder(func(*models.Instance) (map[string]string, error) { return map[string]string{}, nil }))
	err := s.assignInstance(context.Background(), i)
	if !isRuntimeAssignmentPending(err) || len(agent.createRequests) != 0 || bindings.bindings[i.ID] == nil {
		t.Fatalf("unsafe replacement: err=%v creates=%v binding=%v", err, agent.createRequests, bindings.bindings[i.ID])
	}
	if err := s.assignInstance(context.Background(), i); err != nil || len(agent.createRequests) != 0 {
		t.Fatal("pending reservation launched a duplicate", err)
	}
}

func TestOrdinaryPlacementRequiresCurrentLiveIdentity(t *testing.T) {
	for _, scenario := range []string{"current", "legacy", "stale", "no-heartbeat", "deleted", "changed-uid", "changed-ip", "wrong-endpoint", "wrong-image", "wrong-namespace", "not-ready", "disabled", "inventory-error"} {
		t.Run(scenario, func(t *testing.T) {
			pod, live := ordinaryPlacementFixture()
			deployment := &fakeRuntimeDeploymentService{pods: []k8s.RuntimeDeploymentPod{live}}
			want := scenario == "current" || scenario == "legacy"
			switch scenario {
			case "legacy":
				pod.PodUID = nil
			case "stale":
				old := time.Now().Add(-time.Hour)
				pod.LastSeenAt = &old
			case "no-heartbeat":
				pod.LastSeenAt = nil
			case "deleted":
				deployment.pods = nil
			case "changed-uid":
				pod.PodUID = ptrString("deleted-uid")
			case "changed-ip":
				pod.PodIP = ptrString("10.1.2.4")
			case "wrong-endpoint":
				pod.AgentEndpoint = ptrString("http://10.1.2.4:19090")
			case "wrong-image":
				pod.ImageRef = "registry/openclaw:old"
			case "wrong-namespace":
				pod.Namespace = "other"
			case "not-ready":
				deployment.pods[0].State = "unhealthy"
			case "disabled":
				disabled := false
				deployment.pods[0].SchedulingEnabled = &disabled
			case "inventory-error":
				deployment.listErr = errors.New("inventory unavailable")
			}
			s := NewRuntimeScheduler(nil, &fakeRuntimePodRepo{}, nil, nil, nil, nil, nil, deployment, time.Second, WithRuntimeSchedulerNamespace("tenant"))
			got, err := s.verifiedOrdinaryRuntimePods(context.Background(), "openclaw", []models.RuntimePod{pod})
			if scenario == "inventory-error" {
				if err == nil {
					t.Fatal("inventory error hidden")
				}
				return
			}
			if err != nil || (len(got) == 1) != want {
				t.Fatalf("candidates=%+v err=%v want=%v", got, err, want)
			}
		})
	}
}

// Regression for a data-safe upgrade followed by an empty-pool image change.
// Real SQL candidate selection and upgrade placement feed the ordinary scheduler
// and its real binding repository; only Kubernetes/Agent execution is simulated.
func TestOrdinaryPlacementMySQLAfterEmptyPoolSwitch(t *testing.T) {
	sess := upgradeFlowDB(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO users(id)VALUES(1)`)
	root := canonicalCompatibilityTestDir(t)
	pods := repository.NewRuntimePodRepository(sess)
	bindings := repository.NewInstanceRuntimeBindingRepository(sess)
	rollouts := repository.NewRuntimeRolloutRepository(sess)
	u := NewRuntimeUpgradeService(sess, rollouts, pods, bindings, nil, root, "")
	oldImage := "registry/openclaw@sha256:" + strings.Repeat("a", 64)
	for _, target := range []string{"7", "8", "b"} {
		t.Run("target-"+target, func(t *testing.T) {
			exec(`DELETE FROM instance_runtime_bindings`)
			exec(`DELETE FROM runtime_pods`)
			exec(`DELETE FROM instances`)
			exec(`DELETE FROM runtime_rollouts`)
			now := time.Now().UTC()
			start, finish := now.Add(-time.Minute), now.Add(-30*time.Second)
			source := `{"tenant/pool":"` + oldImage + `"}`
			old := models.RuntimeRollout{RuntimeType: "openclaw", TargetImageRef: oldImage, Status: "finished", Phase: "postflight", PreflightID: ptrString("historical"), SourceImagesJSON: &source, StartedAt: &start, FinishedAt: &finish}
			if err := rollouts.Create(ctx, &old); err != nil {
				t.Fatal(err)
			}
			pool := runtimeUpgradeTargetDeploymentName("pool", old.ID)
			image := "registry/openclaw@sha256:" + strings.Repeat(target, 64)
			source = `{"tenant/` + pool + `":"` + oldImage + `"}`
			reset := models.RuntimeRollout{RuntimeType: "openclaw", TargetImageRef: image, Status: "finished", Phase: RuntimeUpgradePhaseEmptyPoolReset, SourceImagesJSON: &source, StartedAt: &start, FinishedAt: &finish}
			if err := rollouts.Create(ctx, &reset); err != nil {
				t.Fatal(err)
			}
			// The real regression: a later successful ordinary rollout stores no
			// preflight or inventory, and changes the serving image in place.
			legacy := models.RuntimeRollout{RuntimeType: "openclaw", TargetImageRef: "registry/openclaw:legacy-" + target, Status: "finished", Phase: "pending", StartedAt: &start, FinishedAt: &finish}
			if err := rollouts.Create(ctx, &legacy); err != nil {
				t.Fatal(err)
			}
			image = legacy.TargetImageRef
			path := RuntimeWorkspacePathWithRoot(root, "openclaw", 1, 901)
			exec(`INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,workspace_path,created_at,updated_at)VALUES(901,1,'openclaw','gateway','lite','creating',1,?,?,?)`, path, now, now)
			pod, live := ordinaryPlacementFixture()
			pod.DeploymentName, live.DeploymentName = pool, pool
			pod.ImageRef, live.ImageRef, live.ImageDigest = image, image, "sha256:"+strings.Repeat(target, 64)
			pod.ImageDigest = &live.ImageDigest
			if err := pods.UpsertFromAgent(ctx, &pod); err != nil {
				t.Fatal(err)
			}
			stale := pod
			stale.ID = 0
			stale.PodName = "old-pod"
			stale.PodUID = ptrString("old-uid")
			stale.AgentEndpoint = ptrString("http://10.1.2.99:19090")
			stale.PodIP = ptrString("10.1.2.99")
			stale.ImageRef = oldImage
			if err := pods.UpsertFromAgent(ctx, &stale); err != nil {
				t.Fatal(err)
			}
			instance := models.Instance{ID: 901, UserID: 1, Type: "openclaw", InstanceMode: "lite", RuntimeType: "gateway", RuntimeGeneration: 1, Status: "creating", WorkspacePath: &path}
			instances := newFakeRuntimeInstanceRepo()
			instances.byID[901] = &instance
			agent := &fakeRuntimeAgentClient{createResponse: &RuntimeAgentCreateGatewayResponse{GatewayID: "gw-901", Port: 20000, Status: "running"}}
			s := NewRuntimeScheduler(instances, pods, bindings, rollouts, agent, nil, nil, &fakeRuntimeDeploymentService{pods: []k8s.RuntimeDeploymentPod{live}}, time.Second, WithRuntimeSchedulerNamespace("tenant"), WithRuntimeSchedulerWorkspaceRoot(root), WithRuntimeUpgradeService(u), WithRuntimeSchedulerGatewayEnvBuilder(func(*models.Instance) (map[string]string, error) { return map[string]string{}, nil }))
			if err := s.assignInstance(ctx, instance); err != nil {
				t.Fatalf("new instance cannot start after switch: %v", err)
			}
			b, err := bindings.GetByInstanceID(ctx, 901)
			if err != nil || b == nil || b.RuntimePodID != pod.ID || len(agent.createRequests) != 1 || agent.createRequests[0].endpoint != *pod.AgentEndpoint {
				t.Fatalf("wrong target binding=%+v creates=%+v err=%v", b, agent.createRequests, err)
			}
			if err := s.assignInstance(ctx, instance); err != nil || len(agent.createRequests) != 1 {
				t.Fatal("retry duplicated Gateway", err)
			}
		})
	}
}
