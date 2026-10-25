package services

import (
	"context"
	"os"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

// Actual repositories/checkpoint persistence and the elected scheduler entry;
// remote Kubernetes observations are controlled, never a tenant's live cluster.
func TestPoolCompletionMySQLSchedulerLifecycle(t *testing.T) {
	sess := upgradeFlowDB(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	migration, err := os.ReadFile("../db/migrations/068_add_runtime_rollout_checkpoints.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(migration))
	_, _, dep, r := completionFixture(t)
	dep.targets[0].Image = "repo:old"
	r.ID = 1
	r.Status = "pending"
	r.StartedAt = nil
	rollouts := repository.NewRuntimeRolloutRepository(sess)
	if err := rollouts.Create(ctx, r); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO runtime_pods (id,runtime_type,namespace,pod_name,pod_uid,pod_ip,deployment_name,image_ref,agent_endpoint,state,draining,last_seen_at) VALUES (306,'hermes','tenant','old','old-uid','10.1.2.2','hermes-runtime','repo:old','http://10.1.2.2:19090','draining',1,NOW())`)
	// Existing stopped/error instances and bindings are unrelated to rollout
	// completion. They must survive intact; do not manufacture instance recovery.
	exec(`INSERT INTO instances VALUES (483,1,'hermes','gateway','lite','stopped',3,'/workspace/483',NULL,NULL,NOW()),(494,1,'hermes','gateway','lite','error',1,'/workspace/494','unsupported_hermes_protocol',NULL,NOW())`)
	bindings := repository.NewInstanceRuntimeBindingRepository(sess)
	if err := bindings.Create(ctx, &models.InstanceRuntimeBinding{InstanceID: 483, RuntimePodID: 306, RuntimeType: "hermes", GatewayID: "gw-483-3", GatewayPort: 20000, State: "stopped", Generation: 3, WorkspacePath: "/workspace/483"}); err != nil {
		t.Fatal(err)
	}
	pods := repository.NewRuntimePodRepository(sess)
	agent := &fakeRuntimeAgentClient{}
	newScheduler := func() *RuntimeScheduler {
		return NewRuntimeScheduler(repository.NewInstanceRepository(sess), pods, bindings, rollouts, agent, nil, nil, dep, time.Second, WithRuntimeSchedulerNamespace("tenant"), WithRuntimeSchedulerHeartbeatTimeout(6*time.Minute))
	}
	scheduler := newScheduler()
	step := func() {
		t.Helper()
		if err := scheduler.reconcileRollouts(ctx); err != nil {
			t.Fatal(err)
		}
	}
	step()                     // pending -> running; frozen inventory persisted
	step()                     // mutation intent persisted
	scheduler = newScheduler() // leader/App replacement resumes from MySQL only
	step()                     // exactly one image submission
	if len(dep.rolloutImageCalls) != 1 {
		t.Fatal("expected one image update")
	}
	step() // old image is not Ready at target yet
	dep.targets[0].Image, dep.targets[0].Ready = "repo:new", true
	step() // completed deployment checkpoint, Agent still missing
	scheduler = newScheduler()
	step()
	current, err := rollouts.GetByID(ctx, r.ID)
	if err != nil || current.Status != "running" {
		t.Fatalf("premature completion: %+v %v", current, err)
	}
	p, err := LoadRuntimePoolProgress(ctx, rollouts, r.ID)
	if err != nil || p.Completed != 1 || p.VerificationStarted.IsZero() {
		t.Fatalf("invalid checkpoint: %+v %v", p, err)
	}
	// Reproduce #126: total duration exceeds15m; source registration is younger
	// than18m. Only the target's live identity/health may complete this operation.
	exec(`UPDATE runtime_rollouts SET started_at=NOW()-INTERVAL 16 MINUTE WHERE id=?`, r.ID)
	exec(`UPDATE runtime_pods SET last_seen_at=NOW()-INTERVAL 16 MINUTE WHERE id=306`)
	exec(`INSERT INTO runtime_pods (id,runtime_type,namespace,pod_name,pod_uid,pod_ip,deployment_name,image_ref,agent_endpoint,state,draining,last_seen_at) VALUES (310,'hermes','tenant','new','new-uid','10.1.2.3','hermes-runtime','repo:new','http://10.1.2.3:19090','ready',0,NOW())`)
	step()
	current, err = rollouts.GetByID(ctx, r.ID)
	if err != nil || current.Status != "finished" || current.FinishedAt == nil || current.ErrorMessage != nil {
		t.Fatalf("did not finish: %+v %v", current, err)
	}
	step() // terminal operations are not retried
	if len(dep.rolloutImageCalls) != 1 || dep.scaleCalls != 0 || len(agent.drainEndpoints) != 0 || len(agent.createRequests) != 0 || len(agent.deleteRequests) != 0 {
		t.Fatal("completion mutated runtime/instances")
	}
	var instances []models.Instance
	if err := sess.Collection("instances").Find().OrderBy("id").All(&instances); err != nil {
		t.Fatal(err)
	}
	if len(instances) != 2 || instances[0].Status != "stopped" || instances[0].RuntimeGeneration != 3 || instances[1].Status != "error" || instances[1].RuntimeGeneration != 1 || stringValue(instances[1].RuntimeErrorMessage) != "unsupported_hermes_protocol" {
		t.Fatalf("instance state changed: %+v", instances)
	}
	var binding models.InstanceRuntimeBinding
	if err := sess.Collection("instance_runtime_bindings").Find("instance_id", 483).One(&binding); err != nil {
		t.Fatal(err)
	}
	if binding.State != "stopped" || binding.RuntimePodID != 306 || binding.Generation != 3 {
		t.Fatalf("binding changed: %+v", binding)
	}
}
