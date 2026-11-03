package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

type replacementDeployment struct {
	completionDeployment
	probe          k8s.RuntimeValidationStatus
	originalWrites int
	stopped        bool
	cleanupStuck   bool
	agents         *fakeRuntimePodRepo
}

func (d *replacementDeployment) EnsurePoolValidation(context.Context, k8s.RuntimeRolloutTarget, string, string, string) error {
	return nil
}
func (d *replacementDeployment) ObservePoolValidation(context.Context, string, string, string) (*k8s.RuntimeValidationStatus, error) {
	v := d.probe
	v.Gone = d.stopped && !d.cleanupStuck
	return &v, nil
}
func (d *replacementDeployment) StopPoolValidation(context.Context, string, string, string) error {
	d.stopped = true
	return nil
}
func (d *replacementDeployment) PoolRetirementReason(context.Context, k8s.RuntimeRolloutTarget, string) (string, error) {
	return "", nil
}
func (d *replacementDeployment) RolloutPoolImage(_ context.Context, target k8s.RuntimeRolloutTarget, image string) error {
	d.originalWrites++
	for i := range d.targets {
		if d.targets[i].Name == target.Name {
			d.targets[i].Image = image
			d.targets[i].Ready = true
		}
	}
	for i := range d.pods {
		if d.pods[i].DeploymentName == target.Name {
			d.pods[i].ImageRef = image
		}
	}
	for _, a := range d.agents.pods {
		if a.DeploymentName == target.Name {
			a.ImageRef = image
			now := time.Now()
			a.LastSeenAt = &now
		}
	}
	return nil
}

type replacementProbeAgent struct {
	fakeRuntimeAgentClient
	fail    bool
	creates int
}

func (a *replacementProbeAgent) CreateGateway(_ context.Context, _ string, req RuntimeAgentCreateGatewayRequest) (*RuntimeAgentCreateGatewayResponse, error) {
	a.creates++
	if a.fail {
		return nil, fmt.Errorf("incompatible startup")
	}
	if req.InstanceID != poolProbeInstanceID || req.Environment["CLAWMANAGER_LLM_BASE_URL"] != "http://127.0.0.1:9/v1" {
		return nil, fmt.Errorf("probe escaped isolation")
	}
	return &RuntimeAgentCreateGatewayResponse{GatewayID: fmt.Sprintf("gw-%d-1", poolProbeInstanceID), Port: 20000, Status: "running"}, nil
}

func TestPoolReplacementMySQLRecoversBrokenPoolAndKeepsWaitingIdentity(t *testing.T) {
	for _, scenario := range []string{"success", "bad-agent", "bad-gateway", "rollback", "checkpoint-cas", "legacy-success", "legacy-bad", "legacy-cleanup-stuck"} {
		t.Run(scenario, func(t *testing.T) {
			sess := upgradeFlowDB(t)
			ctx := context.Background()
			if _, err := sess.SQL().Exec(`CREATE TABLE runtime_rollout_checkpoints(rollout_id BIGINT PRIMARY KEY,state_json MEDIUMTEXT NOT NULL,updated_at DATETIME(6))`); err != nil {
				t.Fatal(err)
			}
			if _, err := sess.SQL().Exec(`INSERT INTO instances(id,user_id,type,instance_mode,runtime_type,status,runtime_generation,created_at)VALUES(557,1,'openclaw','lite','gateway','error',1,?)`, time.Now().Add(-time.Hour)); err != nil {
				t.Fatal(err)
			}
			repo := repository.NewRuntimeRolloutRepository(sess)
			u := NewRuntimeUpgradeService(sess, repo, repository.NewRuntimePodRepository(sess), repository.NewInstanceRuntimeBindingRepository(sess), nil, t.TempDir(), "")
			pod, live := ordinaryPlacementFixture()
			pod.Namespace = "tenant"
			pod.DeploymentName = "pool"
			pod.ImageRef = "registry/old"
			live.ImageRef = "registry/old"
			agents := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{pod.ID: &pod}}
			source := `{"tenant/pool":"registry/old"}`
			plan := `{"pool_switch_waiting_instance_ids":[557]}`
			r := &models.RuntimeRollout{RuntimeType: "openclaw", Status: "pending", Phase: RuntimeUpgradePhaseEmptyPoolReset, TargetImageRef: "registry/new", SourceImagesJSON: &source, PreflightJSON: &plan, AutoRollback: true}
			if strings.HasPrefix(scenario, "legacy-") {
				r.Phase = "requested"
			}
			if err := repo.Create(ctx, r); err != nil {
				t.Fatal(err)
			}
			dep := &replacementDeployment{agents: agents, probe: k8s.RuntimeValidationStatus{Ready: true, PodUID: "validation-uid", Endpoint: "http://10.1.2.99:19090"}}
			dep.targets = []k8s.RuntimeRolloutTarget{{Namespace: "tenant", Name: "pool", UID: "source-uid", Image: "registry/old"}}
			dep.pods = []k8s.RuntimeDeploymentPod{live}
			agent := &replacementProbeAgent{}
			dep.cleanupStuck = scenario == "legacy-cleanup-stuck"
			scheduler := &RuntimeScheduler{rolloutRepo: repo, podRepo: agents, bindingRepo: newFakeRuntimeBindingRepo(), deployments: dep, agentClient: agent, upgrade: u, runtimeNamespace: "tenant", workspaceRoot: u.workspaceRoot}
			if scenario == "bad-agent" {
				dep.probe.Failed = true
				dep.probe.Ready = false
				dep.probe.Reason = "Runtime 连续启动失败"
			}
			if scenario == "bad-gateway" || scenario == "legacy-bad" {
				agent.fail = true
			}
			if err := scheduler.StartRollout(ctx, r.ID); err != nil {
				t.Fatal(err)
			}
			p, err := scheduler.loadPoolReplacement(ctx, r.ID)
			if err != nil {
				t.Fatal(err)
			}
			if dep.originalWrites != 0 {
				t.Fatal("source changed before validation")
			}
			if scenario == "checkpoint-cas" {
				stale, _ := scheduler.loadPoolReplacement(ctx, r.ID)
				p.Stage = "switch"
				if err = scheduler.savePoolReplacement(ctx, r.ID, p); err != nil {
					t.Fatal(err)
				}
				stale.Stage = "rollback"
				if err = scheduler.savePoolReplacement(ctx, r.ID, stale); err == nil {
					t.Fatal("stale controller overwrote current plan")
				}
				now := time.Now()
				if err = scheduler.setPoolReplacementStatus(ctx, *r, p, "error", r.StartedAt, &now, ptrString("finished")); err != nil {
					t.Fatal(err)
				}
				if err = scheduler.setPoolReplacementStatus(ctx, *r, stale, "running", r.StartedAt, nil, nil); err == nil {
					t.Fatal("late status revived terminal operation")
				}
				return
			}
			rollbackInjected := false
			for step := 0; step < 30; step++ {
				if scenario == "legacy-success" && len(dep.rolloutImageCalls) > 0 {
					dep.targets[0].Image = r.TargetImageRef
					dep.targets[0].Ready = true
					dep.pods[0].ImageRef = r.TargetImageRef
					agents.pods[pod.ID].ImageRef = r.TargetImageRef
					agents.pods[pod.ID].State = "ready"
					agents.pods[pod.ID].Draining = false
				}
				current, _ := repo.GetByID(ctx, r.ID)
				if current.Status == "finished" || current.Status == "error" {
					r = current
					break
				}
				// Recreate scheduler state on every observation, proving persisted resume.
				p, err = scheduler.loadPoolReplacement(ctx, r.ID)
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "rollback" && p.Stage == "switch" && p.Submitted && !rollbackInjected {
					dep.targets[0].Ready = false
					p.StageStarted = time.Now().Add(-11 * time.Minute)
					if err = scheduler.savePoolReplacement(ctx, r.ID, p); err != nil {
						t.Fatal(err)
					}
					rollbackInjected = true
				}
				if scenario == "legacy-cleanup-stuck" && (p.Stage == "legacy_apply" || p.Stage == "cleanup_failed") {
					p.StageStarted = time.Now().Add(-6 * time.Minute)
					if err = scheduler.savePoolReplacement(ctx, r.ID, p); err != nil {
						t.Fatal(err)
					}
				}
				if err = scheduler.finishRolloutIfReady(ctx, *current); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "legacy-success" {
				if r.Status != "finished" || len(dep.rolloutImageCalls) != 1 || agent.creates != 1 {
					t.Fatal("legacy update failed", r, len(dep.rolloutImageCalls), agent.creates)
				}
			} else if scenario == "success" {
				if r.Status != "finished" || dep.originalWrites != 1 || agent.creates != 1 {
					t.Fatalf("%+v writes=%d creates=%d", r, dep.originalWrites, agent.creates)
				}
				// The pending instance was created before the switch, but has a durable
				// no-old-data certificate; unrelated historical IDs retain their fence.
				if _, err = sess.SQL().Exec(`UPDATE instances SET runtime_generation=2,status='creating' WHERE id=557`); err != nil {
					t.Fatal(err)
				}
				got, err := u.filterUpgradePlacement(ctx, 557, []models.RuntimePod{*agents.pods[pod.ID]})
				if err != nil || len(got) != 1 {
					t.Fatal("waiting instance lost admission after restart", got, err)
				}
			} else {
				if r.Status != "error" {
					t.Fatalf("did not converge: %+v", r)
				}
				if scenario != "rollback" && dep.originalWrites != 0 {
					t.Fatal("failed candidate changed source")
				}
				if scenario == "rollback" && (dep.originalWrites != 2 || !strings.Contains(stringValue(r.ErrorMessage), "已恢复并验证")) {
					t.Fatal("rollback not verified", r, dep.originalWrites)
				}
			}
			var count int
			row, _ := sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instances WHERE id=557`)
			if err = row.Scan(&count); err != nil || count != 1 {
				t.Fatal("waiting instance deleted", err)
			}
			if !dep.stopped {
				t.Fatal("probe cleanup missing")
			}
			if view, err := LoadRuntimePoolOperation(ctx, repo, r.ID); err != nil || view == nil {
				t.Fatal("public progress unavailable", err)
			}
			if _, err := LoadRuntimePoolProgress(ctx, repo, r.ID); err != nil {
				t.Fatal("legacy endpoint failed on new checkpoint", err)
			}
			raw, _ := json.Marshal(p)
			if strings.Contains(string(raw), "token") {
				t.Fatal("checkpoint leaked credentials")
			}
		})
	}
}
