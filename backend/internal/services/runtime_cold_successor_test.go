package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/runtimeeviction"
	"clawreef/internal/services/k8s"
	"context"
	"encoding/json"
	"fmt"
	"github.com/upper/db/v4"
	corev1 "k8s.io/api/core/v1"
	"strings"
	"testing"
	"time"
)

type coldClassificationInventory struct{ fakeRuntimeDeploymentService }

func (c *coldClassificationInventory) RuntimeDeploymentPods(context.Context, string) ([]models.RuntimePod, error) {
	return nil, nil
}
func (c *coldClassificationInventory) ColdRuntimeBackend() k8s.RuntimeColdSuccessorService {
	return nil
}

func TestColdClassificationRequiresCompletedRetirementInsteadOfLiveWitness(t *testing.T) {
	sess := upgradeFlowDB(t)
	repo := repository.NewRuntimeRolloutRepository(sess)
	u := NewRuntimeUpgradeService(sess, repo, repository.NewRuntimePodRepository(sess), repository.NewInstanceRuntimeBindingRepository(sess), nil, t.TempDir(), "")
	u.deployments = &coldClassificationInventory{}
	image := "repo@sha256:" + strings.Repeat("a", 64)
	source := `{"tenant/openclaw-runtime":"` + image + `"}`
	preflight := "approved"
	fp := "fingerprint"
	if err := repo.Create(context.Background(), &models.RuntimeRollout{ID: 5, RuntimeType: "openclaw", Status: "finished", Phase: "postflight", TargetImageRef: image, SourceImagesJSON: &source, PreflightID: &preflight, PlanFingerprint: &fp}); err != nil {
		t.Fatal(err)
	}
	_, err := u.ClassifyOpenClawTarget(context.Background(), image)
	if err == nil || !strings.Contains(err.Error(), "retirement") {
		t.Fatalf("cold diagnosis must demand exact completed retirement proof, got %v", err)
	}
}

type coldFlowDeployment struct {
	replacementDeployment
	hash        string
	creates     int
	activations int
	cleanup     int
	target      *k8s.RuntimeRolloutTarget
}

func (d *coldFlowDeployment) InspectRetiredRuntimePool(context.Context, k8s.RuntimeRolloutTarget, string) (string, error) {
	return d.hash, nil
}
func (d *coldFlowDeployment) CheckColdRuntimeExternalWriters(context.Context, *k8s.RuntimeOrphanSource) error {
	return nil
}
func (d *coldFlowDeployment) EnsureColdRuntimeSuccessor(_ context.Context, source k8s.RuntimeRolloutTarget, hash, op, name, id string) (*k8s.RuntimeRolloutTarget, error) {
	if d.target != nil {
		v := *d.target
		return &v, nil
	}
	d.creates++
	d.target = &k8s.RuntimeRolloutTarget{Namespace: source.Namespace, Name: name, UID: "successor-controller", Image: source.Image, Ready: true, Upgrade: true}
	d.targets = []k8s.RuntimeRolloutTarget{*d.target}
	pod, live := ordinaryPlacementFixture()
	pod.Namespace = source.Namespace
	pod.DeploymentName = name
	pod.ImageRef = source.Image
	pod.ImageDigest = ptrString(imageDigestFromReference(source.Image))
	pod.Capacity = 8
	pod.State = "standby"
	pod.CapabilitiesJSON = ptrString(`["openclaw.runtime-standby-v1"]`)
	live.Namespace = source.Namespace
	live.DeploymentName = name
	live.ImageRef = source.Image
	live.ImageDigest = imageDigestFromReference(source.Image)
	live.Capacity = 8
	live.SchedulingEnabled = ptrBool(false)
	live.PoolRole = "upgrade-target"
	live.UpgradeID = id
	live.SourceDeployment = source.Name
	d.pods = []k8s.RuntimeDeploymentPod{live}
	d.agents.pods[pod.ID] = &pod
	v := *d.target
	return &v, nil
}
func (d *coldFlowDeployment) ColdRuntimeStandbyReady(context.Context, k8s.RuntimeRolloutTarget, string) (bool, error) {
	return true, nil
}
func (d *coldFlowDeployment) InspectColdRuntimeSuccessor(_ context.Context, target, source k8s.RuntimeRolloutTarget, hash, id string) error {
	if d.target == nil || d.target.UID != target.UID {
		return fmt.Errorf("target replaced")
	}
	return nil
}
func (d *coldFlowDeployment) RetireColdRuntimeSuccessor(_ context.Context, source k8s.RuntimeRolloutTarget, hash, name, id, uid string) (bool, error) {
	d.cleanup++
	d.target = nil
	d.targets = nil
	d.pods = nil
	return true, nil
}
func (d *coldFlowDeployment) SetUpgradePoolActivePreservingSource(_ context.Context, ns, source, target, id string) error {
	if d.activations == 0 {
		return fmt.Errorf("publication before native activation")
	}
	d.pods[0].SchedulingEnabled = ptrBool(true)
	return nil
}
func ptrBool(v bool) *bool { return &v }

type coldFlowAgent struct {
	replacementProbeAgent
	dep *coldFlowDeployment
}

func (a *coldFlowAgent) ActivateUpgrade(context.Context, string, string) error {
	a.dep.activations++
	for _, p := range a.dep.agents.pods {
		p.State = "ready"
	}
	return nil
}

func coldFlowFixture(t *testing.T) (*RuntimeScheduler, *coldFlowDeployment, *models.RuntimeRollout) {
	t.Helper()
	ctx := context.Background()
	sess := upgradeFlowDB(t)
	repo := repository.NewRuntimeRolloutRepository(sess)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := sess.SQL().Exec(q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`CREATE TABLE runtime_rollout_checkpoints(rollout_id BIGINT PRIMARY KEY,state_json MEDIUMTEXT NOT NULL,updated_at DATETIME(6))`)
	exec(`INSERT INTO instances(id,user_id,type,instance_mode,runtime_type,status,runtime_generation,created_at)VALUES(557,1,'openclaw','lite','gateway','error',1,?)`, time.Now().Add(-time.Hour))
	image := "repo@sha256:" + strings.Repeat("a", 64)
	sources := `{"tenant/openclaw-runtime":"` + image + `"}`
	preflight := "approved"
	fingerprint := "approved"
	parent := &models.RuntimeRollout{ID: 5, RuntimeType: "openclaw", Status: "finished", Phase: "postflight", TargetImageRef: image, SourceImagesJSON: &sources, PreflightID: &preflight, PlanFingerprint: &fingerprint, StartedAt: func() *time.Time { v := time.Now().Add(-2 * time.Hour); return &v }()}
	if e := repo.Create(ctx, parent); e != nil {
		t.Fatal(e)
	}
	source := &k8s.RuntimeOrphanSource{Namespace: "tenant", PodName: "old-pod", PodUID: "old-pod-uid", DeploymentName: "openclaw-runtime-u5", DeploymentUID: "source-uid", NodeName: "node", NodeUID: "node-uid", NodeBootID: "boot", RuntimeType: "openclaw", WorkspaceRoot: "/workspaces", StorageDigest: strings.Repeat("a", 64), TerminalResourceVersion: "99"}
	source.SourcePod = &corev1.Pod{}
	scope := source.Scope("retired")
	receipt := &runtimeeviction.PodReceipt{ScopeDigest: runtimeeviction.PodScopeDigest(scope), OperationID: scope.OperationID, PodUID: scope.PodUID, NodeUID: scope.NodeUID, NodeBootID: scope.NodeBootID, InventoryDigest: strings.Repeat("b", 64), LedgerDigest: strings.Repeat("c", 64), FinishedAt: time.Now(), Confirmed: true}
	j := RuntimeOrphanRecoveryJournal{Phase: "completed", SourcePodID: 155, Source: source, Scope: scope, Receipt: receipt, FenceCreated: true}
	items, _ := json.Marshal([]any{map[string]any{"orphan_recovery": j}})
	exec(`INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at)VALUES('retired','orphan_recovery','completed','{}',?,NOW(),NOW())`, string(items))
	exec(`INSERT INTO runtime_pod_deletion_intents(namespace,deployment_name,deployment_uid)VALUES('tenant','openclaw-runtime-u5','source-uid')`)
	exec(`INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,state,capacity,used_slots,scheduling_paused)VALUES(155,'openclaw','tenant','old-pod','old-pod-uid','openclaw-runtime-u5',?,'deleted',8,0,1)`, image)
	dep := &coldFlowDeployment{hash: strings.Repeat("d", 64)}
	dep.agents = &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{}}
	dep.probe = k8s.RuntimeValidationStatus{Ready: true, PodUID: "probe-uid", Endpoint: "http://10.1.2.99:19090"}
	agent := &coldFlowAgent{dep: dep}
	u := NewRuntimeUpgradeService(sess, repo, repository.NewRuntimePodRepository(sess), repository.NewInstanceRuntimeBindingRepository(sess), agent, t.TempDir(), "")
	s := &RuntimeScheduler{rolloutRepo: repo, podRepo: dep.agents, bindingRepo: newFakeRuntimeBindingRepo(), deployments: dep, agentClient: agent, upgrade: u, runtimeNamespace: "tenant", workspaceRoot: u.workspaceRoot, maxGatewaysPerPod: 8}
	u.deployments = s
	classification, e := u.ClassifyOpenClawTarget(ctx, image)
	if e != nil {
		t.Fatal(e)
	}
	if classification.ColdSuccessor == nil {
		t.Fatal("cold proof missing")
	}
	proof := classification.ColdSuccessor
	raw, _ := json.Marshal(map[string]any{"cold_successor": proof, "pool_switch_waiting_instance_ids": classification.WaitingInstanceIDs})
	sourceRaw, _ := json.Marshal(classification.SourceImages)
	fp := proof.Fingerprint()
	plan := string(raw)
	src := string(sourceRaw)
	r := &models.RuntimeRollout{RuntimeType: "openclaw", Status: "pending", Phase: RuntimeUpgradePhaseEmptyPoolReset, TargetImageRef: image, SourceImagesJSON: &src, PreflightJSON: &plan, PlanFingerprint: &fp, AutoRollback: true}
	if e = repo.Create(ctx, r); e != nil {
		t.Fatal(e)
	}
	return s, dep, r
}

func TestColdSuccessorMySQLNativeCommitAndFailureIsolation(t *testing.T) {
	for _, scenario := range []string{"success", "bad-probe", "cancelled", "activation-timeout"} {
		t.Run(scenario, func(t *testing.T) {
			s, dep, r := coldFlowFixture(t)
			ctx := context.Background()
			agent := s.agentClient.(*coldFlowAgent)
			if scenario == "bad-probe" {
				agent.fail = true
			}
			if e := s.rollbackOpenClawRollout(ctx, r, fmt.Errorf("failure")); e == nil {
				t.Fatal("cold operation entered generic rollback")
			}
			if e := s.StartRollout(ctx, r.ID); e != nil {
				t.Fatal(e)
			}
			p, e := s.loadPoolReplacement(ctx, r.ID)
			if e != nil || p.SuccessorName == "" || p.Mode != "cold-successor" {
				t.Fatal(p, e)
			}
			if scenario == "cancelled" {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				_ = s.finishRolloutIfReady(cancelled, *r)
				if dep.creates != 0 || dep.originalWrites != 0 || dep.scaleCalls != 0 {
					t.Fatal("cancelled request changed capacity")
				}
				return
			}
			checkedAuthentication := false
			for step := 0; step < 25; step++ {
				current, e := s.rolloutRepo.GetByID(ctx, r.ID)
				if e != nil {
					t.Fatal(e)
				}
				if current.Status == "finished" || current.Status == "error" {
					r = current
					break
				}
				p, e = s.loadPoolReplacement(ctx, r.ID)
				if e != nil {
					t.Fatal(e)
				}
				if scenario == "success" && p.Stage == "cold_activate" && !checkedAuthentication {
					for _, reported := range dep.agents.pods {
						saved := reported.PodUID
						reported.PodUID = ptrString("wrong-registered-uid")
						if e = s.finishRolloutIfReady(ctx, *current); e != nil {
							t.Fatal(e)
						}
						reported.PodUID = saved
					}
					if dep.activations != 0 || *dep.pods[0].SchedulingEnabled {
						t.Fatal("unconfirmed registration published native target")
					}
					checkedAuthentication = true
				}
				if scenario == "activation-timeout" && p.Stage == "cold_activate" {
					p.StageStarted = time.Now().Add(-11 * time.Minute)
					if e = s.savePoolReplacement(ctx, r.ID, p); e != nil {
						t.Fatal(e)
					}
				}
				if e = s.finishRolloutIfReady(ctx, *current); e != nil {
					t.Fatalf("step %d stage %s: %v", step, p.Stage, e)
				}
			}
			if dep.originalWrites != 0 || dep.scaleCalls != 0 || len(dep.rolloutImageCalls) != 0 {
				t.Fatal("cold failure restored retired source")
			}
			if scenario != "success" {
				if r.Status != "error" {
					t.Fatalf("failed operation remained %s", r.Status)
				}
				if scenario == "bad-probe" && dep.creates != 0 {
					t.Fatal("bad probe published target")
				}
				if dep.cleanup == 0 {
					t.Fatal("owned cleanup skipped")
				}
				return
			}
			if r.Status != "finished" || dep.creates != 1 || dep.activations == 0 {
				t.Fatalf("native commit failed: %s creates=%d activations=%d", r.Status, dep.creates, dep.activations)
			}
			proof, e := coldSuccessorProof(r)
			if e != nil || proof == nil || proof.SuccessorUID == "" {
				t.Fatal("completion lost target identity", proof, e)
			}
			var finished []models.RuntimeRollout
			if e = s.upgrade.sess.Collection("runtime_rollouts").Find(db.Cond{"status": "finished"}).All(&finished); e != nil {
				t.Fatal(e)
			}
			targets, e := runtimeDefaultScaleTargets("tenant", "openclaw", finished)
			if e != nil || len(targets) != 1 || targets[0].DeploymentName != p.SuccessorName || targets[0].DeploymentUID != proof.SuccessorUID {
				t.Fatal("new canonical lineage not reusable", targets, e)
			}
			live, e := s.RuntimeDeploymentPods(ctx, "openclaw")
			if e != nil {
				t.Fatal(e)
			}
			placed, e := s.upgrade.filterUpgradePlacementWithInventory(ctx, 557, live, live)
			if e != nil || len(placed) != 1 {
				t.Fatal("waiting identity was not released", placed, e)
			}
			if !s.upgrade.poolSwitchWaitingInstance(ctx, r, 557) {
				t.Fatal("waiting birth proof lost")
			}
		})
	}
}

func TestColdSuccessorRejectsUnapprovedImageAndIncompleteRetirement(t *testing.T) {
	s, dep, r := coldFlowFixture(t)
	ctx := context.Background()
	if _, e := s.upgrade.ClassifyOpenClawTarget(ctx, "repo@sha256:"+strings.Repeat("b", 64)); e == nil || !strings.Contains(e.Error(), "exact approved") {
		t.Fatal("cold arbitrary upgrade was admitted", e)
	}
	if _, e := s.upgrade.sess.SQL().Exec(`UPDATE runtime_management_operations SET status='processing' WHERE id='retired'`); e != nil {
		t.Fatal(e)
	}
	if e := s.StartRollout(ctx, r.ID); e == nil {
		t.Fatal("incomplete retirement authorized cold creation")
	}
	if dep.creates != 0 || dep.originalWrites != 0 || dep.scaleCalls != 0 {
		t.Fatal("retirement rejection mutated serving resources")
	}
}

func TestColdSuccessorMySQLActivatesStandbyBeforePublicReadiness(t *testing.T) {
	s, dep, r := coldFlowFixture(t)
	ctx := context.Background()
	if e := s.StartRollout(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	for step := 0; step < 12; step++ {
		current, e := s.rolloutRepo.GetByID(ctx, r.ID)
		if e != nil {
			t.Fatal(e)
		}
		p, e := s.loadPoolReplacement(ctx, r.ID)
		if e != nil {
			t.Fatal(e)
		}
		if p.Stage == "cold_activate" {
			r = current
			break
		}
		if e = s.finishRolloutIfReady(ctx, *current); e != nil {
			t.Fatal(e)
		}
	}
	if len(dep.pods) != 1 {
		t.Fatal("successor was not created")
	}
	dep.pods[0].State = "starting" // Eight native workers are healthy; /readyz remains 503 in standby.
	agent := s.agentClient.(*coldFlowAgent)
	agent.healthErr = fmt.Errorf("management health unavailable")
	if e := s.finishRolloutIfReady(ctx, *r); e != nil {
		t.Fatal(e)
	}
	if dep.activations != 0 {
		t.Fatal("unauthenticated management health admitted activation")
	}
	agent.healthErr = nil
	if e := s.finishRolloutIfReady(ctx, *r); e != nil {
		t.Fatal(e)
	}
	if dep.activations != 1 {
		t.Fatal("healthy standby waits for readiness that requires ActivateUpgrade")
	}
	if dep.pods[0].SchedulingEnabled == nil || *dep.pods[0].SchedulingEnabled {
		t.Fatal("standby activation published ordinary capacity before readiness")
	}
	p, e := s.loadPoolReplacement(ctx, r.ID)
	if e != nil || p.Stage != "cold_activate" {
		t.Fatal("cold activation must observe post-activation readiness", p, e)
	}
	if e = s.finishRolloutIfReady(ctx, *r); e != nil {
		t.Fatal(e)
	}
	if dep.activations != 1 || *dep.pods[0].SchedulingEnabled {
		t.Fatal("agent-ready heartbeat bypassed public Kubernetes readiness")
	}
	dep.pods[0].State = "ready"
	if e = s.finishRolloutIfReady(ctx, *r); e != nil {
		t.Fatal(e)
	}
	if !*dep.pods[0].SchedulingEnabled {
		t.Fatal("post-activation ready successor was not released")
	}
}
