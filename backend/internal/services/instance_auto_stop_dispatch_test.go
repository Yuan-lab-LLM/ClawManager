package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/backupgate"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

type dispatchedIdleAgent struct {
	*fakeRuntimeAgentClient
	entered chan int
	blocked chan struct{}
}

func (a *dispatchedIdleAgent) IdlePreflight(_ context.Context, _, _ string, req RuntimeIdleStopRequest) (*RuntimeIdlePreflight, error) {
	return &RuntimeIdlePreflight{InstanceID: req.InstanceID, Generation: req.Generation, PodUID: req.PodUID, ExecutionState: "idle", ExecutionProofSupported: true}, nil
}
func (a *dispatchedIdleAgent) IdleStop(ctx context.Context, _, _ string, req RuntimeIdleStopRequest) error {
	a.entered <- req.InstanceID
	if req.InstanceID == 1 {
		select {
		case <-a.blocked:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
func (a *dispatchedIdleAgent) ConfirmIdleStop(context.Context, string, string, RuntimeIdleStopRequest) error {
	return nil
}

func TestAutoStopScanDoesNotWaitForAnotherRuntimeType(t *testing.T) {
	testAutoStopDispatch(t, false)
}

func TestAutoStopRetriesBriefLifecycleLockContention(t *testing.T) {
	testAutoStopDispatch(t, true)
}

func testAutoStopDispatch(t *testing.T, holdLock bool) {
	sess := idleLifecycleDB(t)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`ALTER TABLE instance_desired_state ADD desired_config_revision_id INT NULL`)
	policy, err := os.ReadFile("../db/migrations/098_add_instance_auto_stop_policy_status.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(policy))
	root := canonicalCompatibilityTestDir(t)
	kube := fake.NewSimpleClientset()
	for n, kind := range []string{"openclaw", "hermes", "opencode", "deepseek-harness"} {
		id := n + 1
		workspace := RuntimeWorkspacePathWithRoot(root, kind, 7, id)
		if err := os.MkdirAll(filepath.Join(workspace, "home"), 0750); err != nil {
			t.Fatal(err)
		}
		name := "pool-" + kind
		exec(`INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,workspace_path,started_at,updated_at) VALUES(?,7,?,'gateway','lite','running',2,?,UTC_TIMESTAMP()-INTERVAL 3 DAY,UTC_TIMESTAMP())`, id, kind, workspace)
		exec(`INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,agent_endpoint,state,capacity,used_slots) VALUES(?,?,'runtime',?,?,?,'test','https://10.0.0.2:19090','ready',1,1)`, id, kind, name, name, name)
		exec(`INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES(?,?,?, ?,20000,?,'running',2)`, id, id, kind, fmt.Sprintf("gw-%d-2", id), workspace)
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "runtime", UID: types.UID(name)}, Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "workspaces", VolumeSource: corev1.VolumeSource{NFS: &corev1.NFSVolumeSource{Server: "10.0.0.9", Path: "/export"}}}}, Containers: []corev1.Container{{Name: "slot-0", VolumeMounts: []corev1.VolumeMount{{Name: "workspaces", MountPath: root}}}}}}
		if _, err := kube.CoreV1().Pods("runtime").Create(t.Context(), pod, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	store := repository.NewInstanceAutoStopRepository(sess)
	if err := store.Initialize(t.Context()); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE instance_model_activity SET monitoring_since=UTC_TIMESTAMP()-INTERVAL 3 DAY`)
	exec(`UPDATE instance_auto_stop_settings SET enabled=TRUE,check_interval_seconds=60,idle_timeout_seconds=120,next_check_at=UTC_TIMESTAMP()-INTERVAL 1 SECOND`)
	agent := &dispatchedIdleAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, entered: make(chan int, 16), blocked: make(chan struct{})}
	lifecycle := &instanceService{instanceRepo: repository.NewInstanceRepository(sess), runtimePodRepo: repository.NewRuntimePodRepository(sess), bindingRepo: repository.NewInstanceRuntimeBindingRepository(sess), agentClient: agent, workspaceRoot: root, autoStopRepo: store}
	controller := &InstanceAutoStopService{store: store, lifecycle: lifecycle, kube: kube}
	if holdLock {
		release, err := acquireInstanceLifecycleOperation(t.Context(), lifecycle.instanceRepo, 1)
		if err != nil {
			t.Fatal(err)
		}
		var once sync.Once
		unlock := func() { once.Do(release) }
		t.Cleanup(unlock)
		time.AfterFunc(200*time.Millisecond, unlock)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer func() { cancel(); controller.workers.Wait() }()
	done := make(chan error, 1)
	go func() { done <- controller.Tick(ctx) }()
	seen := map[int]bool{}
	deadline := time.After(2 * time.Second)
	for len(seen) < 4 {
		select {
		case id := <-agent.entered:
			seen[id] = true
		case <-deadline:
			close(agent.blocked)
			<-done
			t.Fatalf("slow OpenClaw stop blocked independent runtime types: entered=%v", seen)
		}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(agent.blocked)
		t.Fatal("scan waits for native stop")
	}
	cfg, err := store.Settings(ctx)
	if err != nil || !cfg.NextCheckAt.After(time.Now()) {
		close(agent.blocked)
		t.Fatalf("next scan not advanced while stop blocked: %+v %v", cfg, err)
	}
	close(agent.blocked)
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		completed := 0
		for id := 1; id <= 4; id++ {
			a, e := store.GetActivity(ctx, id)
			if e == nil && a.StopPhase == "completed" {
				completed++
			}
		}
		if completed == 4 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("dispatched stops did not release bindings and finish")
}

func TestAutoStopPendingQueueIncludesEveryRuntimeType(t *testing.T) {
	sess := idleLifecycleDB(t)
	for id := 1; id <= 12; id++ {
		kind := "openclaw"
		if id == 12 {
			kind = "deepseek-harness"
		}
		if _, err := sess.SQL().Exec(`INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation) VALUES(?,7,?,'gateway','lite','stopped',2)`, id, kind); err != nil {
			t.Fatal(err)
		}
		if _, err := sess.SQL().Exec(`INSERT INTO instance_idle_stop_operations(id,instance_id,user_id,generation,runtime_pod_id,gateway_id,pod_uid,endpoint,workspace_path,storage_identity,phase,created_at,updated_at) VALUES(?,?,7,2,1,'gateway','pod','endpoint','/workspace','storage','error',DATE_ADD('2026-01-01',INTERVAL ? SECOND),DATE_ADD('2026-01-01',INTERVAL ? SECOND))`, fmt.Sprint(id), id, id, id); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := repository.NewInstanceAutoStopRepository(sess).PendingStops(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range pending {
		if op.InstanceID == 12 {
			return
		}
	}
	t.Fatal("one runtime type filled global pending-stop limit and hid another type")
}

func TestAutoStopDispatchBoundsDeduplicatesAndKeepsBackupLease(t *testing.T) {
	repo := newFakeRuntimeInstanceRepo()
	for id := 1; id <= 5; id++ {
		kind := "openclaw"
		if id == 5 {
			kind = "hermes"
		}
		repo.byID[id] = &models.Instance{ID: id, Type: kind, InstanceMode: "lite", RuntimeType: "gateway", Status: "running"}
	}
	service := &InstanceAutoStopService{lifecycle: &instanceService{instanceRepo: repo}}
	var leases atomic.Int32
	restore := backupgate.ConfigureWorkBoundary(func(context.Context) (func(), error) { leases.Add(1); return func() { leases.Add(-1) }, nil })
	defer restore()
	blocked := make(chan struct{})
	defer func() {
		close(blocked)
		service.workers.Wait()
		if leases.Load() != 0 {
			t.Error("backup lease leaked")
		}
	}()
	entered := make(chan int, 10)
	for _, id := range []int{1, 1, 1, 2, 3, 4, 5} {
		if err := service.dispatchStop(t.Context(), id, func() { entered <- id; <-blocked }); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[int]bool{}
	for len(seen) < 4 {
		select {
		case id := <-entered:
			if seen[id] {
				t.Fatalf("duplicate worker for %d", id)
			}
			seen[id] = true
		case <-time.After(time.Second):
			t.Fatalf("type capacity coupled: %v", seen)
		}
	}
	if seen[4] || !seen[5] || leases.Load() != 4 {
		t.Fatalf("unbounded dispatch or missing backup fence: seen=%v leases=%d", seen, leases.Load())
	}
}

func TestAutoStopDispatchRetainsSameTypeOverflow(t *testing.T) {
	repo := newFakeRuntimeInstanceRepo()
	service := &InstanceAutoStopService{lifecycle: &instanceService{instanceRepo: repo}}
	blocked := make(chan struct{})
	last := make(chan struct{}, 1)
	for id := 1; id <= 4; id++ {
		repo.byID[id] = &models.Instance{ID: id, Type: "openclaw", InstanceMode: "lite", RuntimeType: "gateway", Status: "running"}
		if err := service.dispatchStop(t.Context(), id, func() {
			if id < 4 {
				<-blocked
			} else {
				last <- struct{}{}
			}
		}); err != nil {
			t.Fatal(err)
		}
	}
	close(blocked)
	service.workers.Wait()
	select {
	case <-last:
	default:
		t.Fatal("same-type overflow was dropped and must wait for another scan")
	}
}
