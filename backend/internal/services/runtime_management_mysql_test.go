package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"github.com/upper/db/v4"
)

// Each test gets a disposable database through the localhost-only upgrade fixture.
// Runtime mutation, queue, and admission state use the production repositories.
func runtimeManagementMySQLDB(t *testing.T) db.Session {
	t.Helper()
	sess := upgradeFlowDB(t)
	exec := func(query string) {
		t.Helper()
		if _, err := sess.SQL().Exec(query); err != nil {
			t.Fatalf("%v: %s", err, query)
		}
	}
	apply := func(file string) {
		t.Helper()
		data, err := os.ReadFile("../db/migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		for _, query := range strings.Split(string(data), ";") {
			if strings.TrimSpace(query) != "" {
				exec(query)
			}
		}
	}
	// ReserveInstanceMutations uses these production lifecycle guard tables.
	data, err := os.ReadFile("../db/migrations/045_add_northbound_api.sql")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(data), "CREATE TABLE IF NOT EXISTS northbound_operations")
	if start < 0 {
		t.Fatal("northbound operation migration table missing")
	}
	table := string(data)[start:]
	exec(table[:strings.Index(table, ";")])
	apply("064_add_instance_lifecycle_batches.sql")
	exec("ALTER TABLE instance_lifecycle_batches ADD COLUMN policy_version INT NOT NULL DEFAULT 1, ADD COLUMN retry_round INT NOT NULL DEFAULT 0")
	var installed int
	row, err := sess.SQL().QueryRow("SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='runtime_management_operations'")
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&installed); err != nil {
		t.Fatal(err)
	}
	if installed == 0 {
		apply("079_add_runtime_management.sql")
	}
	return sess
}

func runtimeManagementMySQLService(sess db.Session) *RuntimeManagementService {
	scheduler := NewRuntimeScheduler(repository.NewInstanceRepository(sess), repository.NewRuntimePodRepository(sess), repository.NewInstanceRuntimeBindingRepository(sess), repository.NewRuntimeRolloutRepository(sess), nil, nil, nil, nil, time.Second)
	return NewRuntimeManagementService(sess, scheduler, repository.NewNorthboundRepository(sess))
}

func runtimeManagementMySQLGuard(t *testing.T, sess db.Session, id int, want string) {
	t.Helper()
	var token string
	row, err := sess.SQL().QueryRow("SELECT token FROM instance_lifecycle_manual_guards WHERE instance_id=?", id)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&token); err != nil {
		t.Fatalf("instance %d durable guard missing: %v", id, err)
	}
	if token != want {
		t.Fatalf("instance %d guard=%q, want %q", id, token, want)
	}
}

func TestRuntimeManagementMySQLQueueRetainsDurableMutationGuard(t *testing.T) {
	sess := runtimeManagementMySQLDB(t)
	ctx := context.Background()
	service := runtimeManagementMySQLService(sess)
	actor := 7
	op, err := service.enqueue(ctx, "migration", RuntimeSchedulingRequest{InstanceIDs: []int{101}, TargetPodID: 2}, []RuntimeSchedulingItem{{InstanceID: 101, SourcePodID: 1, TargetPodID: 2, TargetSlot: 1}}, &actor)
	if err != nil {
		t.Fatal(err)
	}
	if op.Status != "pending" || len(op.Items) != 1 || op.Items[0].State != "pending" {
		t.Fatalf("enqueued operation=%+v", op)
	}
	// enqueue's deferred release owns the old random token, not the durable token.
	runtimeManagementMySQLGuard(t, sess, 101, op.ID)
	recovered := runtimeManagementMySQLService(sess)
	history, err := recovered.History(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].ID != op.ID || history[0].Items[0].InstanceID != 101 {
		t.Fatalf("durable history=%+v", history)
	}
	if release, err := repository.NewNorthboundRepository(sess).ReserveInstanceMutations([]int{101}); err == nil {
		release()
		t.Fatal("queued runtime operation allowed concurrent lifecycle mutation")
	}
}

func TestRuntimeManagementMySQLSchedulingPauseSurvivesHeartbeat(t *testing.T) {
	sess := runtimeManagementMySQLDB(t)
	ctx := context.Background()
	service := runtimeManagementMySQLService(sess)
	if _, err := sess.SQL().Exec("INSERT INTO runtime_pods (id,runtime_type,namespace,pod_name,deployment_name,image_ref,state,capacity) VALUES (9,'openclaw','runtime','runtime-a','runtime','image','ready',2)"); err != nil {
		t.Fatal(err)
	}
	pods := repository.NewRuntimePodRepository(sess)
	if err := service.Scheduling(ctx, 9, false); err != nil {
		t.Fatal(err)
	}
	if claimed, err := pods.TryClaimSlot(ctx, 9); err != nil || claimed {
		t.Fatalf("paused claim=%v err=%v", claimed, err)
	}
	if err := pods.UpdateHeartbeat(ctx, 9, "ready", 0, 2, false, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	pod, err := pods.GetByID(ctx, 9)
	if err != nil || pod == nil || !pod.SchedulingPaused {
		t.Fatalf("heartbeat lost pause: %+v %v", pod, err)
	}
	if claimed, err := pods.TryClaimSlot(ctx, 9); err != nil || claimed {
		t.Fatalf("heartbeat reopened paused admission: %v %v", claimed, err)
	}
	if err := service.Scheduling(ctx, 9, true); err != nil {
		t.Fatal(err)
	}
	if claimed, err := pods.TryClaimSlot(ctx, 9); err != nil || !claimed {
		t.Fatalf("resumed claim=%v err=%v", claimed, err)
	}
}

func TestRuntimeManagementMySQLUnknownOwnerHoldsGuardAndRejectsLateSave(t *testing.T) {
	sess := runtimeManagementMySQLDB(t)
	ctx := context.Background()
	original := runtimeManagementMySQLService(sess)
	op, err := original.enqueue(ctx, "migration", RuntimeSchedulingRequest{InstanceIDs: []int{101}}, []RuntimeSchedulingItem{{InstanceID: 101, TargetPodID: 2}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sess.SQL().Exec("UPDATE runtime_management_operations SET status='processing',owner_token=? WHERE id=?", original.token, op.ID); err != nil {
		t.Fatal(err)
	}
	replacement := runtimeManagementMySQLService(sess)
	if err = replacement.Process(ctx); err != nil {
		t.Fatal(err)
	}
	history, err := replacement.History(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Status != "needs_attention" {
		t.Fatalf("interrupted owner replayed: %+v", history)
	}
	runtimeManagementMySQLGuard(t, sess, 101, op.ID)
	op.Status = "completed"
	if err = original.save(ctx, op); err == nil {
		t.Fatal("late original worker overwrote held operation")
	}
	runtimeManagementMySQLGuard(t, sess, 101, op.ID)
}

func TestRuntimeManagementMySQLPreflightFailureReleasesGuard(t *testing.T) {
	sess := runtimeManagementMySQLDB(t)
	ctx := context.Background()
	service := runtimeManagementMySQLService(sess)
	op, err := service.enqueue(ctx, "migration", RuntimeSchedulingRequest{InstanceIDs: []int{101}}, []RuntimeSchedulingItem{{InstanceID: 101, SourcePodID: 1, TargetPodID: 2}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// No Kubernetes inventory exists: processing must fail before a runtime action.
	if err = service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	history, err := service.History(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].ID != op.ID || history[0].Status != "failed" || history[0].Items[0].State != "failed" {
		t.Fatalf("terminal result=%+v", history)
	}
	release, err := repository.NewNorthboundRepository(sess).ReserveInstanceMutations([]int{101})
	if err != nil {
		t.Fatalf("safe terminal failure retained guard: %v", err)
	}
	release()
}

func TestRuntimeManagementMySQLCompletedHealthReleasesGuard(t *testing.T) {
	sess := runtimeManagementMySQLDB(t)
	ctx := context.Background()
	service := runtimeManagementMySQLService(sess)
	if _, err := sess.SQL().Exec("INSERT INTO instances (id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES (101,1,'opencode','gateway','lite','running',2,NOW())"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.SQL().Exec("INSERT INTO runtime_pods (id,runtime_type,namespace,pod_name,deployment_name,image_ref,state,capacity) VALUES (2,'opencode','runtime','runtime-b','runtime','image','ready',2)"); err != nil {
		t.Fatal(err)
	}
	op, err := service.enqueue(ctx, "migration", RuntimeSchedulingRequest{InstanceIDs: []int{101}}, []RuntimeSchedulingItem{{InstanceID: 101, TargetPodID: 2, SourceGeneration: 1}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	op.Items[0].State = "starting"
	op.Status = "starting"
	if _, err = sess.SQL().Exec("UPDATE runtime_management_operations SET status='processing',owner_token=? WHERE id=?", service.token, op.ID); err != nil {
		t.Fatal(err)
	}
	if err = service.save(ctx, op); err != nil {
		t.Fatal(err)
	}
	binding := &models.InstanceRuntimeBinding{InstanceID: 101, RuntimePodID: 2, RuntimeType: "opencode", GatewayID: "gw-101-2", GatewayPort: 20000, WorkspacePath: "/workspaces/101", State: "running", Generation: 2}
	if err = repository.NewInstanceRuntimeBindingRepository(sess).Create(ctx, binding); err != nil {
		t.Fatal(err)
	}
	if err = service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	history, err := service.History(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Status != "completed" {
		t.Fatalf("healthy operation=%+v", history)
	}
	release, err := repository.NewNorthboundRepository(sess).ReserveInstanceMutations([]int{101})
	if err != nil {
		t.Fatalf("healthy completed operation retained guard: %v", err)
	}
	release()
}

func TestRuntimeManagementMySQLPendingCancellationReleasesGuards(t *testing.T) {
	sess := runtimeManagementMySQLDB(t)
	ctx := context.Background()
	service := runtimeManagementMySQLService(sess)
	ids := []int{101, 102}
	op, err := service.enqueue(ctx, "migration", RuntimeSchedulingRequest{InstanceIDs: ids, Concurrency: 2}, []RuntimeSchedulingItem{{InstanceID: 101, TargetPodID: 2}, {InstanceID: 102, TargetPodID: 2}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Cancel(ctx, op.ID); err != nil {
		t.Fatal(err)
	}
	if err = service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	history, err := service.History(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Status != "cancelled" {
		t.Fatalf("cancelled operation=%+v", history)
	}
	for _, item := range history[0].Items {
		if item.State != "cancelled" {
			t.Fatalf("cancelled pending item reached Runtime move: %+v", item)
		}
	}
	release, err := repository.NewNorthboundRepository(sess).ReserveInstanceMutations(ids)
	if err != nil {
		t.Fatalf("cancelled operation retained guards: %v", err)
	}
	release()
}

func TestRuntimeManagementMySQLRolloutAdmissionIsBidirectional(t *testing.T) {
	t.Run("queued-management-blocks-rollout", func(t *testing.T) {
		sess := runtimeManagementMySQLDB(t)
		ctx := context.Background()
		service := runtimeManagementMySQLService(sess)
		op, err := service.enqueue(ctx, "migration", RuntimeSchedulingRequest{InstanceIDs: []int{101}}, []RuntimeSchedulingItem{{InstanceID: 101, TargetPodID: 2}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		rollout := &models.RuntimeRollout{RuntimeType: "openclaw", TargetImageRef: "runtime:next", Status: "pending", Phase: "requested", BatchSize: 1, MaxUnavailable: 1}
		if err = repository.NewRuntimeRolloutRepository(sess).Create(ctx, rollout); err == nil {
			t.Fatal("rollout admitted while Runtime management owns lifecycle guards")
		}
		history, err := service.History(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(history) != 1 || history[0].Status != "pending" {
			t.Fatalf("rollout changed queued operation: %+v", history)
		}
		runtimeManagementMySQLGuard(t, sess, 101, op.ID)
		active, err := repository.NewRuntimeRolloutRepository(sess).ListActive(ctx, "")
		if err != nil || len(active) != 0 {
			t.Fatalf("blocked rollout was inserted: %+v %v", active, err)
		}
	})
	t.Run("active-rollout-blocks-management", func(t *testing.T) {
		sess := runtimeManagementMySQLDB(t)
		ctx := context.Background()
		service := runtimeManagementMySQLService(sess)
		rollout := &models.RuntimeRollout{RuntimeType: "openclaw", TargetImageRef: "runtime:next", Status: "pending", Phase: "requested", BatchSize: 1, MaxUnavailable: 1}
		if err := repository.NewRuntimeRolloutRepository(sess).Create(ctx, rollout); err != nil {
			t.Fatal(err)
		}
		if _, err := service.enqueue(ctx, "migration", RuntimeSchedulingRequest{InstanceIDs: []int{101}}, []RuntimeSchedulingItem{{InstanceID: 101, TargetPodID: 2}}, nil); err == nil {
			t.Fatal("Runtime management admitted during active rollout")
		}
		history, err := service.History(ctx)
		if err != nil || len(history) != 0 {
			t.Fatalf("blocked operation persisted: %+v %v", history, err)
		}
		release, err := repository.NewNorthboundRepository(sess).ReserveInstanceMutations([]int{101})
		if err != nil {
			t.Fatalf("rejected operation leaked transient guard: %v", err)
		}
		release()
	})
}

func runtimeManagementMySQLMoveFixture(t *testing.T) (db.Session, *RuntimeManagementService, *strictStopRuntimeAgent) {
	t.Helper()
	sess := runtimeManagementMySQLDB(t)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("ALTER TABLE instances ADD COLUMN cpu_cores DOUBLE NOT NULL DEFAULT 1, ADD COLUMN memory_gb INT NOT NULL DEFAULT 1, ADD COLUMN disk_gb INT NOT NULL DEFAULT 1, ADD COLUMN runtime_security_profile VARCHAR(64) NOT NULL DEFAULT ''")
	exec("ALTER TABLE team_members ADD COLUMN instance_id INT, ADD COLUMN team_id INT, ADD COLUMN status VARCHAR(32)")
	exec("CREATE TABLE llm_models (id INT PRIMARY KEY,is_active BOOLEAN NOT NULL)")
	exec("INSERT INTO llm_models VALUES (1,1)")
	root := canonicalCompatibilityTestDir(t)
	workspace := RuntimeWorkspacePathWithRoot(root, "opencode", 1, 101)
	exec("INSERT INTO instances (id,user_id,type,runtime_type,instance_mode,status,runtime_generation,workspace_path,updated_at) VALUES (101,1,'opencode','gateway','lite','running',1,?,NOW())", workspace)
	digest := "sha256:" + strings.Repeat("a", 64)
	for _, pod := range []struct {
		id        int
		name, uid string
		used      int
	}{{1, "source", "source-uid", 1}, {2, "target", "target-uid", 0}} {
		ip := fmt.Sprintf("10.30.0.%d", pod.id)
		exec("INSERT INTO runtime_pods (id,runtime_type,namespace,pod_name,pod_uid,pod_ip,deployment_name,image_ref,image_digest,agent_endpoint,state,capacity,used_slots,last_seen_at) VALUES (?,'opencode','runtime',?,?,?,'runtime','image',?,?,'ready',2,?,NOW())", pod.id, pod.name, pod.uid, ip, digest, "https://"+ip+":19090", pod.used)
	}
	exec("INSERT INTO instance_runtime_bindings (instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES (101,1,'opencode','gw-101-1',20000,?,'running',1)", workspace)
	totalCPU, totalMemory, coordCPU, coordMemory, slotCPU, slotMemory := int64(4000), int64(16*1024*1024*1024), int64(100), int64(128*1024*1024), int64(1000), int64(4*1024*1024*1024)
	requestCPU, requestMemory := int64(100), int64(1<<30)
	port1, port2 := 20000, 20003
	deployments := &fakeRuntimeDeploymentService{}
	for index, pod := range []struct{ name, uid string }{{"source", "source-uid"}, {"target", "target-uid"}} {
		ip := fmt.Sprintf("10.30.0.%d", index+1)
		deployments.pods = append(deployments.pods, k8s.RuntimeDeploymentPod{PodIP: &ip, RuntimeType: "opencode", Namespace: "runtime", DeploymentName: "runtime", PodName: pod.name, PodUID: pod.uid, State: "ready", ImageRef: "image", ImageDigest: digest, Capacity: 2, WorkspaceStorageIdentity: "pvc:runtime/shared:uid:pv", CPUMillisLimit: &totalCPU, MemoryBytesLimit: &totalMemory, CoordinatorCPUMillis: &coordCPU, CoordinatorMemoryBytes: &coordMemory, Slots: []models.RuntimeSlot{{Index: 1, ContainerName: "slot-0", Ready: true, GatewayPort: &port1, CPUMillisLimit: &slotCPU, MemoryBytesLimit: &slotMemory}, {Index: 2, ContainerName: "slot-1", Ready: true, GatewayPort: &port2, CPUMillisLimit: &slotCPU, MemoryBytesLimit: &slotMemory}}})
	}
	for i := range deployments.pods {
		p := &deployments.pods[i]
		p.CPUMillisRequest, p.MemoryBytesRequest = &totalCPU, &totalMemory
		p.CoordinatorCPUMillisRequest, p.CoordinatorMemoryBytesRequest = &coordCPU, &coordMemory
		for j := range p.Slots {
			p.Slots[j].CPUMillisRequest = &requestCPU
			p.Slots[j].MemoryBytesRequest = &requestMemory
		}
	}
	agent := &strictStopRuntimeAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{createResponse: &RuntimeAgentCreateGatewayResponse{GatewayID: "gw-101-2", Port: 20003, Status: "running"}}, strict: true}
	scheduler := NewRuntimeScheduler(repository.NewInstanceRepository(sess), repository.NewRuntimePodRepository(sess), repository.NewInstanceRuntimeBindingRepository(sess), repository.NewRuntimeRolloutRepository(sess), agent, nil, nil, deployments, time.Second, WithRuntimeSchedulerNamespace("runtime"), WithRuntimeSchedulerWorkspaceRoot(root), WithRuntimeSchedulerGatewayEnvBuilder(func(*models.Instance) (map[string]string, error) { return map[string]string{}, nil }))
	scheduler.upgrade = NewRuntimeUpgradeService(sess, repository.NewRuntimeRolloutRepository(sess), repository.NewRuntimePodRepository(sess), repository.NewInstanceRuntimeBindingRepository(sess), agent, root, "")
	return sess, NewRuntimeManagementService(sess, scheduler, repository.NewNorthboundRepository(sess)), agent
}

func TestRuntimeManagementMySQLBindingCreateKeepsFenceOnOneConnection(t *testing.T) {
	sess, _, _ := runtimeManagementMySQLMoveFixture(t)
	sess.SetMaxOpenConns(1)
	repo := repository.NewInstanceRuntimeBindingRepository(sess)
	binding, err := repo.GetByInstanceID(context.Background(), 101)
	if err != nil || binding == nil {
		t.Fatalf("read source binding: %+v %v", binding, err)
	}
	if _, err := sess.SQL().Exec("DELETE FROM instance_runtime_bindings WHERE instance_id=101"); err != nil {
		t.Fatal(err)
	}
	binding.ID = 0
	if _, err := sess.SQL().Exec("INSERT INTO gateway_runtime_upgrades(instance_id,owner_user_id,generation,operation_id,state,created_at,updated_at) VALUES(101,1,1,?,'reserved',NOW(),NOW())", strings.Repeat("a", 32)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := repo.Create(ctx, binding); !errors.Is(err, models.ErrRuntimeUpgradeInProgress) {
		t.Fatalf("active upgrade fence bypassed: %v", err)
	}
	if _, err := sess.SQL().Exec("DELETE FROM gateway_runtime_upgrades WHERE instance_id=101"); err != nil {
		t.Fatal(err)
	}
	ctx, cancelCreate := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelCreate()
	if err := repo.Create(ctx, binding); err != nil {
		t.Fatalf("binding create needs a second connection while holding its FK parent: %v", err)
	}
	created, err := repo.GetByInstanceID(context.Background(), 101)
	if err != nil || created == nil || created.GatewayID != binding.GatewayID {
		t.Fatalf("binding not committed: %+v %v", created, err)
	}
}

func TestRuntimeManagementMySQLSuccessfulMoveUsesExactSlotAndNewGeneration(t *testing.T) {
	sess, service, agent := runtimeManagementMySQLMoveFixture(t)
	ctx := context.Background()
	op, err := service.Submit(ctx, RuntimeSchedulingRequest{InstanceIDs: []int{101}, TargetPodID: 2, TargetSlot: 2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	if len(agent.deleteRequests) != 1 || agent.deleteRequests[0].gatewayID != "gw-101-1" || len(agent.createRequests) != 1 {
		t.Fatalf("Runtime calls deletes=%+v creates=%+v", agent.deleteRequests, agent.createRequests)
	}
	request := agent.createRequests[0].req
	if request.Generation != 2 || request.GatewayPort != 20003 || request.PodUID != "target-uid" {
		t.Fatalf("incorrect target creation=%+v", request)
	}
	binding, err := repository.NewInstanceRuntimeBindingRepository(sess).GetByInstanceID(ctx, 101)
	if err != nil || binding == nil || binding.RuntimePodID != 2 || binding.Generation != 2 || binding.GatewayPort != 20003 || binding.State != "running" {
		t.Fatalf("target binding=%+v %v", binding, err)
	}
	instance, err := repository.NewInstanceRepository(sess).GetByID(101)
	if err != nil || instance.RuntimeGeneration != 2 || instance.Status != "running" {
		t.Fatalf("moved instance=%+v %v", instance, err)
	}
	for id, want := range map[int64]int{1: 0, 2: 1} {
		pod, err := repository.NewRuntimePodRepository(sess).GetByID(ctx, id)
		if err != nil || pod.UsedSlots != want {
			t.Fatalf("pod%d occupancy=%+v %v", id, pod, err)
		}
	}
	if err = service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	history, err := service.History(ctx)
	if err != nil || len(history) != 1 || history[0].ID != op.ID || history[0].Status != "completed" {
		t.Fatalf("move history=%+v %v", history, err)
	}
	if len(agent.deleteRequests) != 1 || len(agent.createRequests) != 1 {
		t.Fatal("healthy refresh repeated Runtime mutation")
	}
}

func TestRuntimeManagementMySQLUnresolvedRecoveryBlocksPreflightAndWorker(t *testing.T) {
	sess, service, agent := runtimeManagementMySQLMoveFixture(t)
	ctx := context.Background()
	if _, err := sess.SQL().Exec("INSERT INTO runtime_rollouts (id,runtime_type,target_image_ref,status,phase) VALUES (55,'opencode','image','error','failed')"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.SQL().Exec("INSERT INTO runtime_upgrade_items (rollout_id,instance_id,state,source_generation,recovery_status,created_at,updated_at) VALUES (55,101,'failed',1,'needs_attention',NOW(),NOW())"); err != nil {
		t.Fatal(err)
	}
	plan, err := service.Preflight(ctx, RuntimeSchedulingRequest{InstanceIDs: []int{101}, TargetPodID: 2, TargetSlot: 2})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Allowed || len(plan.Blockers) == 0 || !strings.Contains(strings.Join(plan.Blockers, " "), ErrWorkspaceUpgradeBusy.Error()) {
		t.Fatalf("unresolved recovery admitted by preflight=%+v", plan)
	}
	op, err := service.enqueue(ctx, "migration", RuntimeSchedulingRequest{InstanceIDs: []int{101}, TargetPodID: 2}, []RuntimeSchedulingItem{{InstanceID: 101, SourcePodID: 1, TargetPodID: 2, TargetSlot: 2}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	history, err := service.History(ctx)
	if err != nil || len(history) != 1 || history[0].ID != op.ID || history[0].Status != "failed" || !strings.Contains(history[0].Items[0].ErrorMessage, ErrWorkspaceUpgradeBusy.Error()) {
		t.Fatalf("worker bypassed shared mutation fence: %+v %v", history, err)
	}
	if len(agent.deleteRequests) != 0 || len(agent.createRequests) != 0 {
		t.Fatalf("held recovery reached Runtime calls: delete=%+v create=%+v", agent.deleteRequests, agent.createRequests)
	}
	binding, err := repository.NewInstanceRuntimeBindingRepository(sess).GetByInstanceID(ctx, 101)
	if err != nil || binding == nil || binding.RuntimePodID != 1 || binding.Generation != 1 {
		t.Fatalf("recovery source changed: %+v %v", binding, err)
	}
}

func TestRuntimeManagementMySQLIsolatedSourceCannotMoveToSiblingPod(t *testing.T) {
	sess, service, agent := runtimeManagementMySQLMoveFixture(t)
	ctx := context.Background()
	for _, query := range []string{"UPDATE instances SET type='openclaw'", "UPDATE runtime_pods SET runtime_type='openclaw'", "UPDATE instance_runtime_bindings SET runtime_type='openclaw'"} {
		if _, err := sess.SQL().Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	deployments := service.scheduler.deployments.(*fakeRuntimeDeploymentService)
	for i := range deployments.pods {
		deployments.pods[i].RuntimeType = "openclaw"
	}
	if _, err := sess.SQL().Exec("INSERT INTO runtime_rollouts (id,runtime_type,target_image_ref,status,phase,preflight_id,preflight_json) VALUES (55,'openclaw','image','finished','committed','isolated','{}')"); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.SQL().Exec("INSERT INTO runtime_upgrade_items (rollout_id,instance_id,runtime_pod_id,state,source_generation,source_pod_uid,source_deployment_name,source_image_digest,isolated_at,created_at,updated_at) VALUES (55,101,1,'failed',1,'source-uid','runtime',?,NOW(),NOW(),NOW())", "sha256:"+strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	plan, err := service.Preflight(ctx, RuntimeSchedulingRequest{InstanceIDs: []int{101}, TargetPodID: 2, TargetSlot: 2})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Allowed || !strings.Contains(strings.Join(plan.Blockers, " "), "placement policy") {
		t.Fatalf("isolated source admitted to sibling Pod: %+v", plan)
	}
	if len(agent.deleteRequests) != 0 || len(agent.createRequests) != 0 {
		t.Fatal("placement rejection reached Runtime boundary")
	}
}

func TestRuntimeManagementMySQLWorkerHonorsWorkspaceMutationFence(t *testing.T) {
	_, service, agent := runtimeManagementMySQLMoveFixture(t)
	ctx := context.Background()
	op, err := service.Submit(ctx, RuntimeSchedulingRequest{InstanceIDs: []int{101}, TargetPodID: 2, TargetSlot: 2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The SQL preflight passed. Contention on the shared mutation fence alone
	// must stop the queued worker, proving it uses WithInstanceMutation.
	release, err := service.scheduler.upgrade.lockWorkspaceOperations([]int{101})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err = service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	history, err := service.History(ctx)
	if err != nil || len(history) != 1 || history[0].ID != op.ID || history[0].Status != "failed" || !strings.Contains(history[0].Items[0].ErrorMessage, ErrWorkspaceUpgradeBusy.Error()) {
		t.Fatalf("worker bypassed workspace mutation fence: %+v %v", history, err)
	}
	if len(agent.deleteRequests) != 0 || len(agent.createRequests) != 0 {
		t.Fatal("contended worker reached Runtime boundary")
	}
}

func TestRuntimeManagementMySQLElasticEvictionUsesPhysicalSlots(t *testing.T) {
	sess, service, agent := runtimeManagementMySQLMoveFixture(t)
	deployments := &managementInventoryDeployments{fakeRuntimeDeploymentService: service.scheduler.deployments.(*fakeRuntimeDeploymentService)}
	service.scheduler.deployments = deployments
	if _, err := sess.SQL().Exec("UPDATE instances SET cpu_cores=2,memory_gb=4 WHERE id=101"); err != nil {
		t.Fatal(err)
	}
	agent.createResponse.Port = 20000
	operation, err := service.Evict(context.Background(), 1, RuntimeSchedulingRequest{Mode: "graceful"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(operation.Items) != 1 || operation.Items[0].TargetPodID != 2 || operation.Items[0].TargetSlot != 1 {
		t.Fatalf("eviction plan=%+v", operation)
	}
	if deployments.listCalls != 1 {
		t.Fatalf("graceful planning fetched live inventory %d times, want once", deployments.listCalls)
	}
	// Worker execution must refresh planning's snapshot before any gateway stop.
	beforeWorker := deployments.listCalls
	if err = service.Process(context.Background()); err != nil {
		t.Fatal(err)
	}
	if deployments.listCalls <= beforeWorker {
		t.Fatal("worker reused the HTTP request's planning inventory")
	}
	binding, err := service.scheduler.bindingRepo.GetByInstanceID(context.Background(), 101)
	if err != nil || binding == nil || binding.RuntimePodID != 2 || binding.GatewayPort != 20000 || binding.Generation != 2 {
		t.Fatalf("binding=%+v err=%v", binding, err)
	}
}

func TestRuntimeManagementMySQLGracefulWorkerRejectsReplacedTarget(t *testing.T) {
	_, service, agent := runtimeManagementMySQLMoveFixture(t)
	ctx := context.Background()
	if _, err := service.Evict(ctx, 1, RuntimeSchedulingRequest{Mode: "graceful"}, nil); err != nil {
		t.Fatal(err)
	}
	service.scheduler.deployments.(*fakeRuntimeDeploymentService).pods[1].PodUID = "replacement-uid"
	if err := service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	instance, err := service.scheduler.instanceRepo.GetByID(101)
	if err != nil || instance.Status != "running" || instance.RuntimeGeneration != 1 {
		t.Fatalf("stale planning changed source intent: %+v %v", instance, err)
	}
	binding, err := service.scheduler.bindingRepo.GetByInstanceID(ctx, 101)
	if err != nil || binding == nil || binding.RuntimePodID != 1 || binding.Generation != 1 {
		t.Fatalf("stale planning released source binding: %+v %v", binding, err)
	}
	if len(agent.deleteRequests) != 0 || len(agent.createRequests) != 0 {
		t.Fatal("replaced target reached gateway mutation boundary")
	}
}

func TestRuntimeManagementMySQLForceEvictionWaitsAndRecovers(t *testing.T) {
	_, service, agent := runtimeManagementMySQLMoveFixture(t)
	ctx := context.Background()
	if err := service.Scheduling(ctx, 2, false); err != nil {
		t.Fatal(err)
	}
	op, err := service.Evict(ctx, 1, RuntimeSchedulingRequest{Mode: "force"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(op.Items) != 1 || op.Items[0].TargetPodID != 0 {
		t.Fatalf("force plan=%+v", op)
	}
	if err = service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	binding, err := service.scheduler.bindingRepo.GetByInstanceID(ctx, 101)
	if err != nil || binding != nil {
		t.Fatalf("source binding retained after confirmed stop: %+v %v", binding, err)
	}
	instance, err := service.scheduler.instanceRepo.GetByID(101)
	if err != nil || instance.Status != "creating" || instance.RuntimeGeneration != 2 {
		t.Fatalf("pending intent=%+v %v", instance, err)
	}
	source, err := service.scheduler.podRepo.GetByID(ctx, 1)
	if err != nil || source.UsedSlots != 0 || !source.SchedulingPaused {
		t.Fatalf("source=%+v %v", source, err)
	}
	if len(agent.deleteRequests) != 1 || len(agent.createRequests) != 0 {
		t.Fatalf("calls delete=%+v create=%+v", agent.deleteRequests, agent.createRequests)
	}
	if err = service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	history, err := service.History(ctx)
	if err != nil || len(history) != 1 || history[0].Status != "starting" || history[0].Items[0].State != "waiting_capacity" {
		t.Fatalf("waiting history=%+v %v", history, err)
	}
	// The durable creating intent survives service replacement and uses ordinary scheduling.
	recovered := NewRuntimeManagementService(service.sess, service.scheduler, service.guards)
	if err = recovered.Process(ctx); err != nil {
		t.Fatal(err)
	}
	if err = recovered.Scheduling(ctx, 2, true); err != nil {
		t.Fatal(err)
	}
	agent.createResponse.Port = 20000
	if err = service.scheduler.assignInstance(ctx, *instance); err != nil {
		t.Fatal(err)
	}
	if err = recovered.Process(ctx); err != nil {
		t.Fatal(err)
	}
	history, err = recovered.History(ctx)
	if err != nil || len(history) != 1 || history[0].Status != "completed" || history[0].Items[0].TargetPodID != 2 {
		t.Fatalf("recovered history=%+v %v", history, err)
	}
}

func TestRuntimeManagementMySQLForceEvictionRetainsUnconfirmedSource(t *testing.T) {
	_, service, agent := runtimeManagementMySQLMoveFixture(t)
	agent.deleteErr = errors.New("source teardown unconfirmed")
	ctx := context.Background()
	if _, err := service.Evict(ctx, 1, RuntimeSchedulingRequest{Mode: "force"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	binding, err := service.scheduler.bindingRepo.GetByInstanceID(ctx, 101)
	if err != nil || binding == nil || binding.RuntimePodID != 1 {
		t.Fatalf("unconfirmed binding=%+v %v", binding, err)
	}
	instance, err := service.scheduler.instanceRepo.GetByID(101)
	if err != nil || instance.Status == "creating" {
		t.Fatalf("unconfirmed instance became schedulable: %+v %v", instance, err)
	}
	source, err := service.scheduler.podRepo.GetByID(ctx, 1)
	if err != nil || source.UsedSlots != 1 || len(agent.createRequests) != 0 {
		t.Fatalf("unconfirmed occupancy=%+v %v", source, err)
	}
}

type runtimeManagementAfterStopAgent struct {
	*strictStopRuntimeAgent
	afterStop func()
}

func (a *runtimeManagementAfterStopAgent) DeleteGateway(ctx context.Context, endpoint, gatewayID string) error {
	if err := a.strictStopRuntimeAgent.DeleteGateway(ctx, endpoint, gatewayID); err != nil {
		return err
	}
	a.afterStop()
	return nil
}

func TestRuntimeManagementMySQLForceEvictionReconcilesReportedOccupancy(t *testing.T) {
	for _, tc := range []struct {
		name         string
		heartbeat    bool
		otherBinding bool
	}{
		{name: "before-heartbeat"},
		{name: "heartbeat-already-released", heartbeat: true},
		{name: "other-binding-before-heartbeat", otherBinding: true},
		{name: "heartbeat-keeps-other-binding", heartbeat: true, otherBinding: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sess, service, agent := runtimeManagementMySQLMoveFixture(t)
			ctx := context.Background()
			if err := service.Scheduling(ctx, 2, false); err != nil {
				t.Fatal(err)
			}
			service.scheduler.agentClient = &runtimeManagementAfterStopAgent{strictStopRuntimeAgent: agent, afterStop: func() {
				// Another assignment may finish while source teardown is in flight.
				if tc.otherBinding {
					for _, query := range []string{
						"INSERT INTO instances (id,user_id,type,runtime_type,instance_mode,status,runtime_generation) VALUES (102,2,'opencode','gateway','lite','running',1)",
						"INSERT INTO instance_runtime_bindings (instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES (102,1,'opencode','gw-102-1',20003,'/workspaces/other','running',1)",
						"UPDATE runtime_pods SET used_slots=2 WHERE id=1",
					} {
						if _, err := sess.SQL().Exec(query); err != nil {
							t.Fatal(err)
						}
					}
				}
				if tc.heartbeat {
					used := 0
					if tc.otherBinding {
						used = 1
					}
					if err := service.scheduler.bindingRepo.UpdateState(ctx, 101, 1, "stopped", nil); err != nil {
						t.Fatal(err)
					}
					if err := service.scheduler.podRepo.UpdateHeartbeat(ctx, 1, "ready", used, 2, false, time.Now().UTC()); err != nil {
						t.Fatal(err)
					}
				}
			}}
			if _, err := service.Evict(ctx, 1, RuntimeSchedulingRequest{Mode: "force"}, nil); err != nil {
				t.Fatal(err)
			}
			if err := service.Process(ctx); err != nil {
				t.Fatal(err)
			}
			binding, err := service.scheduler.bindingRepo.GetByInstanceID(ctx, 101)
			if err != nil || binding != nil {
				t.Fatalf("confirmed source was not queued: binding=%+v err=%v", binding, err)
			}
			instance, err := service.scheduler.instanceRepo.GetByID(101)
			if err != nil || instance.Status != "creating" || instance.RuntimeGeneration != 2 {
				t.Fatalf("creating intent=%+v err=%v", instance, err)
			}
			// Reprocessing must not release the source twice.
			if err := service.Process(ctx); err != nil {
				t.Fatal(err)
			}
			wantUsed := 0
			if tc.otherBinding {
				wantUsed = 1
				other, err := service.scheduler.bindingRepo.GetByInstanceID(ctx, 102)
				if err != nil || other == nil || other.State != "running" || other.Generation != 1 {
					t.Fatalf("other source occupant changed: %+v %v", other, err)
				}
			}
			source, err := service.scheduler.podRepo.GetByID(ctx, 1)
			if err != nil || source.UsedSlots != wantUsed || !source.SchedulingPaused {
				t.Fatalf("source occupancy=%+v err=%v, want %d used slots and paused", source, err, wantUsed)
			}
			if len(agent.deleteRequests) != 1 || len(agent.createRequests) != 0 {
				t.Fatalf("calls delete=%+v create=%+v", agent.deleteRequests, agent.createRequests)
			}
		})
	}
}

func TestRuntimeManagementMySQLForceEvictionRetainsReplacedSource(t *testing.T) {
	sess, service, agent := runtimeManagementMySQLMoveFixture(t)
	ctx := context.Background()
	service.scheduler.agentClient = &runtimeManagementAfterStopAgent{strictStopRuntimeAgent: agent, afterStop: func() {
		if _, err := sess.SQL().Exec("UPDATE runtime_pods SET pod_uid='replacement-uid' WHERE id=1"); err != nil {
			t.Fatal(err)
		}
	}}
	if _, err := service.Evict(ctx, 1, RuntimeSchedulingRequest{Mode: "force"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	binding, err := service.scheduler.bindingRepo.GetByInstanceID(ctx, 101)
	if err != nil || binding == nil || binding.Generation != 1 {
		t.Fatalf("replacement source binding was released: %+v %v", binding, err)
	}
	instance, err := service.scheduler.instanceRepo.GetByID(101)
	if err != nil || instance.Status != "stopped" || instance.RuntimeGeneration != 2 {
		t.Fatalf("replacement source became schedulable: %+v %v", instance, err)
	}
	source, err := service.scheduler.podRepo.GetByID(ctx, 1)
	if err != nil || source.UsedSlots != 1 || !source.SchedulingPaused || len(agent.createRequests) != 0 {
		t.Fatalf("replacement source occupancy changed: %+v %v", source, err)
	}
}

func TestRuntimeManagementMySQLForceQueueDoesNotLockConcurrentRelease(t *testing.T) {
	sess, service, _ := runtimeManagementMySQLMoveFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	op, err := service.Evict(ctx, 1, RuntimeSchedulingRequest{Mode: "force"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := service.scheduler.bindingRepo.GetByInstanceID(ctx, 101)
	if err != nil {
		t.Fatal(err)
	}
	fence := service.scheduler.instanceRepo.(interface {
		FenceRuntimeRestart(context.Context, int, int) (bool, error)
	})
	if fenced, err := fence.FenceRuntimeRestart(ctx, 101, 1); err != nil || !fenced {
		t.Fatalf("fence=%v err=%v", fenced, err)
	}
	if _, err = sess.SQL().ExecContext(ctx, "UPDATE runtime_management_operations SET status='processing',owner_token=? WHERE id=?", service.token, op.ID); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		"INSERT INTO instances (id,user_id,type,runtime_type,instance_mode,status,runtime_generation) VALUES (102,2,'opencode','gateway','lite','running',1)",
		"INSERT INTO instance_runtime_bindings (instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES (102,1,'opencode','gw-102-1',20003,'/workspaces/other','running',1)",
		"UPDATE runtime_pods SET used_slots=2 WHERE id=1",
	} {
		if _, err = sess.SQL().ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	// A second release holds its binding lock before it updates the Pod counter.
	// Queueing the first source must not wait for that binding while holding the
	// Pod lock, or the two ordinary binding -> Pod releases would deadlock.
	other, err := sess.Driver().(*sql.DB).BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Rollback()
	if _, err = other.ExecContext(ctx, "DELETE FROM instance_runtime_bindings WHERE instance_id=102"); err != nil {
		t.Fatal(err)
	}
	queued := make(chan error, 1)
	go func() { queued <- service.queueEvictedSource(ctx, op.Items[0], binding, op.ID) }()
	select {
	case err = <-queued:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		// Release the fixture lock before waiting, including on the RED path.
		_ = other.Rollback()
		queueErr := <-queued
		t.Fatalf("force queue waited for another binding release while holding the Pod lock: %v", queueErr)
	}
	source, err := service.scheduler.podRepo.GetByID(ctx, 1)
	if err != nil || source.UsedSlots != 1 {
		t.Fatalf("uncommitted concurrent release was not reserved: %+v %v", source, err)
	}
	if _, err = other.ExecContext(ctx, "UPDATE runtime_pods SET used_slots=GREATEST(used_slots-1,0) WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	if err = other.Commit(); err != nil {
		t.Fatal(err)
	}
	source, err = service.scheduler.podRepo.GetByID(ctx, 1)
	if err != nil || source.UsedSlots != 0 || !source.SchedulingPaused {
		t.Fatalf("concurrent releases left incorrect source occupancy: %+v %v", source, err)
	}
	instance, err := service.scheduler.instanceRepo.GetByID(101)
	if err != nil || instance.Status != "creating" || instance.RuntimeGeneration != 2 {
		t.Fatalf("concurrent release lost creating intent: %+v %v", instance, err)
	}
}

func TestRuntimeManagementMySQLForceEvictionWaitsForEquivalentSlot(t *testing.T) {
	_, service, agent := runtimeManagementMySQLMoveFixture(t)
	ctx := context.Background()
	if _, err := service.Evict(ctx, 1, RuntimeSchedulingRequest{Mode: "force"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	deployments := service.scheduler.deployments.(*fakeRuntimeDeploymentService)
	// Constraints must survive more than the UI's latest 100 operation rows.
	values := ""
	for n := 0; n < 101; n++ {
		if n > 0 {
			values += ","
		}
		values += fmt.Sprintf("('unrelated-%d','migration','completed','{}','[]',DATE_ADD(NOW(6),INTERVAL 1 SECOND),NOW(6))", n)
	}
	if _, err := service.sess.SQL().Exec("INSERT INTO runtime_management_operations (id,kind,status,request_json,items_json,created_at,updated_at) VALUES " + values); err != nil {
		t.Fatal(err)
	}
	weakCPU, weakMemory := int64(500), int64(2<<30)
	for i := range deployments.pods[1].Slots {
		deployments.pods[1].Slots[i].CPUMillisLimit = &weakCPU
		deployments.pods[1].Slots[i].MemoryBytesLimit = &weakMemory
	}
	instance, err := service.scheduler.instanceRepo.GetByID(101)
	if err != nil {
		t.Fatal(err)
	}
	agent.createResponse.Port = 20000
	if err = service.scheduler.assignInstance(ctx, *instance); !isRuntimeAssignmentPending(err) || len(agent.createRequests) != 0 {
		t.Fatalf("weaker slot was admitted: %v creates=%+v", err, agent.createRequests)
	}
	cpu, memory := int64(1000), int64(4<<30)
	deployments.pods[1].Slots[1].CPUMillisLimit = &cpu
	deployments.pods[1].Slots[1].MemoryBytesLimit = &memory
	agent.createResponse.Port = 20003
	if err = service.scheduler.assignInstance(ctx, *instance); err != nil {
		t.Fatal(err)
	}
	if err = service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	row, err := service.sess.SQL().QueryRow("SELECT status FROM runtime_management_operations WHERE kind='eviction'")
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&status); err != nil || status != "completed" {
		t.Fatalf("equivalent destination did not recover: %s %v", status, err)
	}
	if len(agent.createRequests) != 1 || agent.createRequests[0].req.GatewayPort != 20003 {
		t.Fatalf("incompatible first port admitted: %+v", agent.createRequests)
	}
}

func TestRuntimeManagementMySQLForceQueueFailureRetainsBinding(t *testing.T) {
	sess, service, _ := runtimeManagementMySQLMoveFixture(t)
	_, err := sess.SQL().Exec("CREATE TRIGGER fail_creating BEFORE UPDATE ON instances FOR EACH ROW BEGIN IF NEW.status='creating' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture queue failure'; END IF; END")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err = service.Evict(ctx, 1, RuntimeSchedulingRequest{Mode: "force"}, nil); err != nil {
		t.Fatal(err)
	}
	if err = service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	binding, err := service.scheduler.bindingRepo.GetByInstanceID(ctx, 101)
	if err != nil || binding == nil || binding.RuntimePodID != 1 {
		t.Fatalf("queue failure lost binding: %+v %v", binding, err)
	}
	source, err := service.scheduler.podRepo.GetByID(ctx, 1)
	if err != nil || source.UsedSlots != 1 {
		t.Fatalf("queue failure released occupancy: %+v %v", source, err)
	}
}

func TestRuntimeManagementMySQLForceReservationRechecksBeforeCreate(t *testing.T) {
	_, service, agent := runtimeManagementMySQLMoveFixture(t)
	ctx := context.Background()
	if _, err := service.Evict(ctx, 1, RuntimeSchedulingRequest{Mode: "force"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	instance, err := service.scheduler.instanceRepo.GetByID(101)
	if err != nil {
		t.Fatal(err)
	}
	target, err := service.scheduler.podRepo.GetByID(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := service.scheduler.podRepo.TryClaimSlot(ctx, 2); err != nil || !claimed {
		t.Fatalf("claim=%v %v", claimed, err)
	}
	start, err := service.scheduler.prepareGatewayStart(ctx, *instance, "opencode", *target)
	if err != nil {
		t.Fatal(err)
	}
	deployments := service.scheduler.deployments.(*fakeRuntimeDeploymentService)
	old := deployments.pods[1].Slots[0].MemoryBytesLimit
	weak := int64(2 << 30)
	deployments.pods[1].Slots[0].MemoryBytesLimit = &weak
	agent.createResponse.Port = 20000
	if err = service.scheduler.createGatewayOnPod(ctx, *instance, "opencode", *target, start); !isRuntimeAssignmentPending(err) || len(agent.createRequests) != 0 {
		t.Fatalf("reused reservation bypassed physical policy: %v %+v", err, agent.createRequests)
	}
	deployments.pods[1].Slots[0].MemoryBytesLimit = old
	if err = service.scheduler.createGatewayOnPod(ctx, *instance, "opencode", *target, start); err != nil {
		t.Fatal(err)
	}
}
