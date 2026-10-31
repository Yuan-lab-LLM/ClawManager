package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"github.com/upper/db/v4"
)

// Only the remote Agent/Kubernetes boundary is simulated. Drive the real
// scheduler entry and MySQL repositories, not ValidateTargetRuntime directly.
type standbyFlowAgent struct {
	*upgradeFlowAgent
	RuntimeUpgradeAgentClient
	s                                             db.Session
	deployments                                   *fakeRuntimeDeploymentService
	mu                                            sync.Mutex
	compatibility, migrations, activations, stops int
	deferReadiness                                bool
	expectedItems                                 int
}

func (a *standbyFlowAgent) CreateGateway(ctx context.Context, endpoint string, req RuntimeAgentCreateGatewayRequest) (*RuntimeAgentCreateGatewayResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.upgradeFlowAgent.CreateGateway(ctx, endpoint, req)
}

func (a *standbyFlowAgent) GatewayState(ctx context.Context, endpoint, id string) (*RuntimeAgentGatewayState, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.upgradeFlowAgent.GatewayState(ctx, endpoint, id)
}
func (a *standbyFlowAgent) DeleteGateway(ctx context.Context, endpoint, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stops++
	delete(a.states, id)
	return nil
}
func (a *standbyFlowAgent) PreflightUpgradeCompatibility(ctx context.Context, endpoint string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentUpgradeCompatibility, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.deployments.pods[1].State == "ready" || a.activations != 0 {
		return nil, fmt.Errorf("compatibility ran after premature activation")
	}
	a.compatibility++
	return &RuntimeAgentUpgradeCompatibility{Status: "compatible", ConfigOriginalSHA256: "original", ConfigTargetSHA256: "target", ConfigValidated: true, DoctorValidated: true, SessionDryRunValid: true, AvailableBytes: 10 << 30}, nil
}
func (a *standbyFlowAgent) AcquireWriterLease(context.Context, string, RuntimeAgentWriterLeaseRequest) error {
	return nil
}
func (a *standbyFlowAgent) ReleaseWriterLease(context.Context, string, RuntimeAgentWriterLeaseRequest) error {
	return nil
}
func (a *standbyFlowAgent) PreflightWorkspace(context.Context, string, RuntimeAgentWorkspaceRequest) (*RuntimeAgentWorkspacePreflight, error) {
	return &RuntimeAgentWorkspacePreflight{}, nil
}
func (a *standbyFlowAgent) MigrateSessionSQLite(ctx context.Context, endpoint string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentSessionSQLiteMigration, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.activations != 0 {
		return nil, fmt.Errorf("migration after activation")
	}
	a.migrations++
	return &RuntimeAgentSessionSQLiteMigration{Status: "validated", OutputSHA256: "result", SessionCatalogSHA256: "catalog"}, nil
}
func (a *standbyFlowAgent) ActivateUpgrade(ctx context.Context, endpoint, id string) error {
	if a.migrations != a.expectedItems || a.compatibility != a.expectedItems {
		return fmt.Errorf("activation before all migrations")
	}
	a.activations++
	if a.deferReadiness {
		return nil
	}
	for index := 1; index < len(a.deployments.pods); index++ {
		p := &a.deployments.pods[index]
		if p.PodIP != nil && endpoint == "http://"+*p.PodIP+":19090" {
			p.State = "ready"
		}
	}
	_, err := a.s.SQL().ExecContext(ctx, "UPDATE runtime_pods SET state='ready',last_seen_at=NOW() WHERE agent_endpoint=?", endpoint)
	return err
}

func TestUpgradeStandbyMySQLSchedulerLifecycle(t *testing.T) {
	for _, delayed := range []bool{false, true} {
		t.Run(fmt.Sprintf("delayed-readiness-%v", delayed), func(t *testing.T) {
			testUpgradeStandbyMySQLSchedulerLifecycle(t, delayed, 3, false)
		})
	}
}

func TestUpgradeParallelMySQLSchedulerScale(t *testing.T) {
	counts := []int{32}
	if os.Getenv("TEST_UPGRADE_LARGE_SCALE") == "1" {
		counts = []int{200, 500}
	}
	for _, count := range counts {
		t.Run(fmt.Sprint(count), func(t *testing.T) { testUpgradeStandbyMySQLSchedulerLifecycle(t, false, count, true) })
	}
}

func TestUpgradeScopedMySQLSchedulerRetainsExcludedSource(t *testing.T) {
	testUpgradeStandbyMySQLSchedulerLifecycle(t, false, 3, true, true)
}

func testUpgradeStandbyMySQLSchedulerLifecycle(t *testing.T, delayed bool, count int, parallel bool, scoped ...bool) {
	s := upgradeFlowDB(t)
	ctx := context.Background()
	root := t.TempDir()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	digest := "sha256:" + strings.Repeat("b", 64)
	image := "repo@" + digest
	capabilities := append([]string{}, openClawUpgradeRequiredCapabilities...)
	preflight := `{"target_runtime_version":"2026.8.1","target_upgrade_protocol":"openclaw-upgrade-v3"}`
	batchSize := 2
	if parallel {
		capabilities = append(capabilities, "openclaw.upgrade-operation-fence-v1")
		preflight = `{"target_runtime_version":"2026.8.1","target_upgrade_protocol":"openclaw-upgrade-v3","execution":{"version":1,"check":4,"migrate":2,"start":2,"restore":2}}`
		batchSize = 8
	}
	if len(scoped) > 0 && scoped[0] {
		var frozen map[string]any
		json.Unmarshal([]byte(preflight), &frozen)
		frozen["exclude_instance_ids"] = []int{490}
		raw, _ := json.Marshal(frozen)
		preflight = string(raw)
	}
	caps, _ := json.Marshal(capabilities)
	exec(`INSERT INTO runtime_rollouts (id,runtime_type,target_image_ref,target_image_digest,status,phase,preflight_id,source_images_json,preflight_json,batch_size,started_at,updated_at) VALUES (124,'openclaw',?,?,'running','image_rollout','p124','{"tenant/source":"old"}',?,?,NOW(),NOW())`, image, digest, preflight, batchSize)
	exec(`INSERT INTO runtime_pods (id,runtime_type,namespace,pod_name,pod_ip,deployment_name,image_ref,image_digest,agent_endpoint,state,capacity,openclaw_version,session_store,agent_protocol_version,capabilities_json,last_seen_at) VALUES (308,'openclaw','tenant','target','10.1.2.3','source-u124',?,?,'http://10.1.2.3:19090','standby',100,'2026.8.1','sqlite','openclaw-upgrade-v3',?,NOW())`, image, digest, string(caps))
	exec(`INSERT INTO runtime_pods (id,runtime_type,namespace,pod_name,pod_uid,pod_ip,deployment_name,image_ref,agent_endpoint,state,capacity,used_slots,capabilities_json,last_seen_at) VALUES (307,'openclaw','tenant','source-pod','source-uid','10.1.2.2','source','old','http://10.1.2.2:19090','ready',100,2,?,NOW())`, string(caps))
	ids := []int{481, 482, 484}
	for len(ids) < count {
		ids = append(ids, 1000+len(ids))
	}
	for _, id := range ids {
		exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,workspace_path,runtime_error_message,runtime_error_code,updated_at) VALUES (?,1,'openclaw','gateway','lite','stopped',2,?,NULL,NULL,NOW())", id, RuntimeWorkspacePathWithRoot(root, "openclaw", 1, id))
		exec(`INSERT INTO runtime_upgrade_items (rollout_id,instance_id,state,source_generation,created_at,updated_at) VALUES (124,?,'prepared',2,NOW(),NOW())`, id)
	}
	ip := "10.1.2.3"
	sourceIP := "10.1.2.2"
	disabled := false
	deployments := &fakeRuntimeDeploymentService{pods: []k8s.RuntimeDeploymentPod{
		{Namespace: "tenant", RuntimeType: "openclaw", DeploymentName: "source", PodName: "source-pod", PodUID: "source-uid", PodIP: &sourceIP, ImageRef: "old", State: "ready"},
		{Namespace: "tenant", RuntimeType: "openclaw", DeploymentName: "source-u124", PodName: "target", PodUID: "target-uid", PodIP: &ip, ImageRef: image, ImageDigest: digest, State: "unhealthy", PoolRole: "upgrade", UpgradeID: "124", SourceDeployment: "source", SchedulingEnabled: &disabled},
	}}
	targetCount := (count + 99) / 100
	for index := 1; index < targetCount; index++ {
		name := fmt.Sprintf("target-%d", index)
		extraIP := fmt.Sprintf("10.1.2.%d", index+3)
		exec(`INSERT INTO runtime_pods (id,runtime_type,namespace,pod_name,pod_uid,pod_ip,deployment_name,image_ref,image_digest,agent_endpoint,state,capacity,openclaw_version,session_store,agent_protocol_version,capabilities_json,last_seen_at) VALUES (?,'openclaw','tenant',?,?,?,'source-u124',?,? ,?,'standby',100,'2026.8.1','sqlite','openclaw-upgrade-v3',?,NOW())`, 308+index, name, name+"-uid", extraIP, image, digest, "http://"+extraIP+":19090", string(caps))
		deployments.pods = append(deployments.pods, k8s.RuntimeDeploymentPod{Namespace: "tenant", RuntimeType: "openclaw", DeploymentName: "source-u124", PodName: name, PodUID: name + "-uid", PodIP: &extraIP, ImageRef: image, ImageDigest: digest, State: "unhealthy", PoolRole: "upgrade", UpgradeID: "124", SourceDeployment: "source", SchedulingEnabled: &disabled})
	}
	pods := repository.NewRuntimePodRepository(s)
	bindings := repository.NewInstanceRuntimeBindingRepository(s)
	rollouts := repository.NewRuntimeRolloutRepository(s)
	agent := &standbyFlowAgent{upgradeFlowAgent: &upgradeFlowAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, states: map[string]*RuntimeAgentGatewayState{}}, s: s, deployments: deployments, deferReadiness: delayed, expectedItems: count}
	// Two live source Gateways and one stopped instance exercise both paths.
	for index, id := range []int{481, 482} {
		gid := fmt.Sprintf("gw-%d-2", id)
		exec("UPDATE instances SET status='running' WHERE id=?", id)
		if err := bindings.Create(ctx, &models.InstanceRuntimeBinding{InstanceID: id, RuntimePodID: 307, RuntimeType: "openclaw", GatewayID: gid, GatewayPort: 20000 + index*3, State: "running", Generation: 2, WorkspacePath: RuntimeWorkspacePathWithRoot(root, "openclaw", 1, id)}); err != nil {
			t.Fatal(err)
		}
		agent.states[gid] = &RuntimeAgentGatewayState{GatewayID: gid, InstanceID: id, Generation: 2, State: "running"}
	}
	if len(scoped) > 0 && scoped[0] {
		exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,workspace_path,runtime_error_message,runtime_error_code,updated_at) VALUES (490,1,'openclaw','gateway','lite','error',2,?,NULL,NULL,NOW())", RuntimeWorkspacePathWithRoot(root, "openclaw", 1, 490))
		if err := bindings.Create(ctx, &models.InstanceRuntimeBinding{InstanceID: 490, RuntimePodID: 307, RuntimeType: "openclaw", GatewayID: "gw-490-2", GatewayPort: 20100, State: "error", Generation: 2, WorkspacePath: RuntimeWorkspacePathWithRoot(root, "openclaw", 1, 490)}); err != nil {
			t.Fatal(err)
		}
	}
	scheduler := NewRuntimeScheduler(repository.NewInstanceRepository(s), pods, bindings, rollouts, agent, nil, nil, deployments, time.Second, WithRuntimeSchedulerWorkspaceRoot(root), WithRuntimeSchedulerNamespace("tenant"))
	upgrade := NewRuntimeUpgradeService(s, rollouts, pods, bindings, agent, root, "")
	upgrade.gatewayRestarter = scheduler
	upgrade.deployments = scheduler
	scheduler.upgrade = upgrade
	// The real #134 target heartbeated under its source deployment because
	// an old Agent inferred a truncated Pod name. Diagnose, preserve sources,
	// then allow the normal pipeline once the Agent reports the correct owner.
	exec("UPDATE runtime_pods SET deployment_name='source' WHERE id=308")
	r, err := rollouts.GetByID(ctx, 124)
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduler.finishRolloutIfReady(ctx, *r); err != nil {
		t.Fatal(err)
	}
	r, err = rollouts.GetByID(ctx, 124)
	if err != nil || !strings.HasPrefix(stringValue(r.ErrorMessage), upgradeTargetIdentityMismatch) || r.Phase != "image_rollout" || agent.compatibility != 0 || agent.creates != 0 {
		t.Fatalf("identity mismatch was not safely reported: %+v %v", r, err)
	}
	exec("UPDATE runtime_pods SET deployment_name='source-u124' WHERE id=308")
	deadline := time.Now().Add(5 * time.Minute)
	waitRounds := 0
	var recoveryStarted time.Time
	for n := 0; time.Now().Before(deadline); n++ {
		r, err := rollouts.GetByID(ctx, 124)
		if err != nil {
			t.Fatal(err)
		}
		if r.Status == "finished" {
			break
		}
		if err := scheduler.finishRolloutIfReady(ctx, *r); err != nil {
			t.Fatal(err)
		}
		if n == 0 && agent.compatibility != minInt(batchSize, count) {
			t.Fatal("standby was filtered before compatibility")
		}
		if agent.activations == 0 && agent.creates != 0 {
			t.Fatal("Gateway created on standby")
		}
		if agent.activations > 0 && agent.creates == 0 {
			var errors int
			row, err := s.SQL().QueryRow(`SELECT COUNT(*) FROM runtime_upgrade_items WHERE rollout_id=124 AND (error_message IS NOT NULL OR restart_next_retry_at IS NOT NULL OR restart_attempts <> 0)`)
			if err != nil {
				t.Fatal(err)
			}
			if err := row.Scan(&errors); err != nil || errors != 0 {
				t.Fatalf("normal activation wait became a failed startup: count=%d err=%v", errors, err)
			}
			var fresh models.RuntimeRollout
			if err := s.Collection("runtime_rollouts").Find("id", 124).One(&fresh); err != nil || fresh.ErrorMessage != nil {
				t.Fatalf("activation wrote a rollout error: %+v %v", fresh, err)
			}
			if delayed {
				waitRounds++
				if waitRounds > 1 {
					var item models.RuntimeUpgradeItem
					if err := s.Collection("runtime_upgrade_items").Find("instance_id", 481).One(&item); err != nil || item.RecoveryStartedAt == nil {
						t.Fatalf("activation wait lacks a bounded deadline: %+v %v", item, err)
					}
					if recoveryStarted.IsZero() {
						recoveryStarted = *item.RecoveryStartedAt
					} else if !recoveryStarted.Equal(*item.RecoveryStartedAt) {
						t.Fatal("activation wait reset its recovery deadline")
					}
				}
				if waitRounds == 4 {
					deployments.pods[1].State = "ready"
					exec("UPDATE runtime_pods SET state='ready',last_seen_at=NOW() WHERE id=308")
				}
			}
		}
		current, err := rollouts.GetByID(ctx, 124)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status != "finished" {
			if allowed, err := pods.TryClaimSlot(ctx, 308); err != nil || allowed {
				t.Fatalf("ordinary scheduling fence lost: %v %v", allowed, err)
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	r, err = rollouts.GetByID(ctx, 124)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "finished" || r.Phase != "postflight" || agent.creates != count || agent.activations != targetCount || agent.stops != 2 {
		var items []models.RuntimeUpgradeItem
		_ = s.Collection("runtime_upgrade_items").Find("rollout_id", 124).All(&items)
		for _, item := range items {
			t.Logf("instance=%d state=%s code=%s error=%s", item.InstanceID, item.State, stringValue(item.RecoveryErrorCode), stringValue(item.ErrorMessage))
		}
		t.Fatalf("incomplete lifecycle: %+v creates=%d activations=%d", r, agent.creates, agent.activations)
	}
	if len(scoped) > 0 && scoped[0] {
		b, err := bindings.GetByInstanceID(ctx, 490)
		if err != nil || b == nil || b.RuntimePodID != 307 || b.Generation != 2 || b.State != "error" || deployments.scaleCalls != 0 {
			t.Fatalf("excluded source changed %+v %v scale=%d", b, err, deployments.scaleCalls)
		}
	}
	for _, id := range ids {
		b, err := bindings.GetRunningByInstanceID(ctx, id)
		if err != nil || b == nil || b.Generation != 3 {
			t.Fatalf("bad binding for %d: %+v %v", id, b, err)
		}
		var item models.RuntimeUpgradeItem
		if err := s.Collection("runtime_upgrade_items").Find("instance_id", id).One(&item); err != nil {
			t.Fatal(err)
		}
		if item.State != "verified" || item.RestartAttempts != 1 {
			t.Fatalf("bad item: %+v", item)
		}
	}
}
