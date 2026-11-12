package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawreef/internal/repository"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

type idleFlowAgent struct {
	*fakeRuntimeAgentClient
	store                       *repository.InstanceAutoStopRepository
	fail, preflightFail         bool
	stops, confirms, preflights int
}

func (a *idleFlowAgent) IdlePreflight(_ context.Context, _ string, id string, req RuntimeIdleStopRequest) (*RuntimeIdlePreflight, error) {
	a.preflights++
	if a.preflightFail {
		return nil, fmt.Errorf("graceful stop endpoint is unavailable")
	}
	return &RuntimeIdlePreflight{GatewayID: id, InstanceID: req.InstanceID, Generation: req.Generation, PodUID: req.PodUID, ExecutionState: "unsupported", ExecutionProofSupported: false}, nil
}
func (a *idleFlowAgent) IdleStop(ctx context.Context, _ string, gatewayID string, req RuntimeIdleStopRequest) error {
	a.stops++
	activity, err := a.store.GetActivity(ctx, req.InstanceID)
	if err != nil {
		return err
	}
	if activity == nil || activity.StopOperationID == "" || activity.Generation != req.Generation {
		return fmt.Errorf("Runtime signaled before durable fence")
	}
	if a.fail {
		return context.DeadlineExceeded
	}
	return nil
}
func (a *idleFlowAgent) ConfirmIdleStop(ctx context.Context, _ string, _ string, req RuntimeIdleStopRequest) error {
	activity, err := a.store.GetActivity(ctx, req.InstanceID)
	if err != nil {
		return err
	}
	if activity == nil || activity.StopPhase != "teardown_confirmed" {
		return fmt.Errorf("slot release before durable receipt")
	}
	a.confirms++
	return nil
}

func TestAutoStopMySQLControllerFailureResumeAndDataRetention(t *testing.T) {
	for _, runtimeType := range []string{"openclaw", "hermes", "opencode", "deepseek-harness"} {
		t.Run(runtimeType, func(t *testing.T) { testAutoStopControllerFailureResume(t, runtimeType) })
	}
}

func testAutoStopControllerFailureResume(t *testing.T, runtimeType string) {
	s := upgradeFlowDB(t)
	ctx := t.Context()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`ALTER TABLE instances ADD access_token VARCHAR(255) NULL,ADD agent_bootstrap_token VARCHAR(255) NULL`)
	exec(`ALTER TABLE instances ADD provisioning_operation_id VARCHAR(64) NULL`)
	exec(`ALTER TABLE teams ADD status VARCHAR(32), ADD resource_scope VARCHAR(32)`)
	exec(`ALTER TABLE team_members ADD team_id INT,ADD instance_id INT,ADD status VARCHAR(32),ADD member_key VARCHAR(64)`)

	for _, name := range []string{"007_add_instance_agent_control_plane.sql", "096_add_instance_auto_stop.sql", "097_add_instance_auto_stop_scan.sql", "098_add_instance_auto_stop_policy_status.sql"} {
		raw, err := os.ReadFile("../db/migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if strings.HasPrefix(name, "007") {
			text = text[strings.Index(text, "CREATE TABLE IF NOT EXISTS instance_agents"):]
		}
		for _, q := range strings.Split(text, ";") {
			if strings.TrimSpace(q) != "" {
				exec(q)
			}
		}
	}
	exec(`CREATE TABLE gateway_runtime_tasks (instance_id INT,generation INT,finished BOOLEAN)`)
	exec(`CREATE TABLE instance_workspace_write_intents (instance_id INT)`)
	root := canonicalCompatibilityTestDir(t)
	workspace := RuntimeWorkspacePathWithRoot(root, runtimeType, 7, 1)
	if err := os.MkdirAll(filepath.Join(workspace, "home"), 0750); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(workspace, "home", "keep.txt")
	if err := os.WriteFile(data, []byte("preserved"), 0600); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,workspace_path,started_at,updated_at) VALUES(1,7,?,'gateway','lite','running',2,?,UTC_TIMESTAMP()-INTERVAL 3 DAY,UTC_TIMESTAMP())`, runtimeType, workspace)
	exec(`INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,agent_endpoint,state,capacity,used_slots) VALUES(11,?,'runtime','pool','source-uid','pool','test','https://10.0.0.2:19090','ready',1,1)`, runtimeType)
	exec(`INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES(1,11,?,'gw-1-2',20000,?,'running',2)`, runtimeType, workspace)
	store := repository.NewInstanceAutoStopRepository(s)
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE instance_model_activity SET monitoring_since=UTC_TIMESTAMP()-INTERVAL 3 DAY`)
	instances := repository.NewInstanceRepository(s)
	pods := repository.NewRuntimePodRepository(s)
	bindings := repository.NewInstanceRuntimeBindingRepository(s)
	agent := &idleFlowAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, store: store}
	lifecycle := &instanceService{instanceRepo: instances, runtimePodRepo: pods, bindingRepo: bindings, agentClient: agent, workspaceRoot: root, autoStopRepo: store}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "runtime", UID: types.UID("source-uid")}, Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "workspaces", VolumeSource: corev1.VolumeSource{NFS: &corev1.NFSVolumeSource{Server: "10.0.0.9", Path: "/export"}}}}, Containers: []corev1.Container{{Name: "slot-0", VolumeMounts: []corev1.VolumeMount{{Name: "workspaces", MountPath: root}}}}}}
	controller := &InstanceAutoStopService{store: store, lifecycle: lifecycle, kube: fake.NewSimpleClientset(pod)}
	cfg, err := store.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	activity, err := store.GetActivity(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	agent.preflightFail = true
	if err = controller.tryStop(ctx, *cfg, *activity); err == nil {
		t.Fatal("missing stop protocol changed the running instance")
	}
	stillRunning, readErr := instances.GetByID(1)
	if readErr != nil || stillRunning.Status != "running" || agent.stops != 0 {
		t.Fatalf("protocol failure changed desired state: %+v %v", stillRunning, readErr)
	}
	agent.preflightFail = false
	agent.fail = true
	if err = controller.tryStop(ctx, *cfg, *activity); err == nil {
		t.Fatal("lost teardown was reported complete")
	}
	binding, err := bindings.GetByInstanceID(ctx, 1)
	if err != nil || binding == nil {
		t.Fatalf("binding released without receipt: %v", err)
	}
	pending, err := store.PendingStops(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("stop not recoverable: %+v %v", pending, err)
	}
	if err = store.BeginManualStart(ctx, 1, time.Now().Add(time.Minute)); err == nil {
		t.Fatal("unresolved stop allowed Start")
	}
	agent.fail = false
	controller.resume(ctx, pending[0])
	binding, err = bindings.GetByInstanceID(ctx, 1)
	if err != nil || binding != nil {
		t.Fatalf("binding not released: %+v %v", binding, err)
	}
	activity, err = store.GetActivity(ctx, 1)
	if err != nil || activity.StoppedReason != "idle_timeout" || activity.StopOperationID != "" {
		t.Fatalf("stop did not complete: %+v %v", activity, err)
	}
	if agent.stops != 2 || agent.confirms != 1 || agent.preflights != 2 {
		t.Fatalf("wrong retry flow: stops=%d confirms=%d native_preflights=%d", agent.stops, agent.confirms, agent.preflights)
	}
	bytes, err := os.ReadFile(data)
	if err != nil || string(bytes) != "preserved" {
		t.Fatal("workspace data was lost")
	}
	p, err := pods.GetByID(ctx, 11)
	if err != nil || p.UsedSlots != 0 {
		t.Fatalf("slot not returned: %+v %v", p, err)
	}
}

func TestAutoStopMySQLPollingInitializesAtStartupAndOnlyRescansWhenDue(t *testing.T) {
	s := idleLifecycleDB(t)
	store := repository.NewInstanceAutoStopRepository(s)
	ctx := t.Context()
	cfg, err := store.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = false
	cfg, err = store.SaveSettings(ctx, *cfg)
	if err != nil {
		t.Fatal(err)
	}
	insert := func(id int) {
		t.Helper()
		if _, err := s.SQL().Exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation) VALUES(?,7,'openclaw','gateway','lite','stopped',1)", id); err != nil {
			t.Fatal(err)
		}
	}
	controller := &InstanceAutoStopService{store: store, lifecycle: &instanceService{}}
	insert(99)
	if err := controller.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if activity, err := store.GetActivity(ctx, 99); err != nil || activity == nil {
		t.Fatalf("startup did not establish observation window: %+v %v", activity, err)
	}
	insert(100)
	if err := controller.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if activity, err := store.GetActivity(ctx, 100); err != nil || activity != nil {
		t.Fatalf("poll without due scan traversed all instances: %+v %v", activity, err)
	}
	cfg.Enabled = true
	if _, err := store.SaveSettings(ctx, *cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SQL().Exec("UPDATE instance_auto_stop_settings SET next_check_at=UTC_TIMESTAMP()-INTERVAL 1 SECOND"); err != nil {
		t.Fatal(err)
	}
	if err := controller.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if activity, err := store.GetActivity(ctx, 100); err != nil || activity == nil {
		t.Fatalf("due scan did not initialize newly observed instance: %+v %v", activity, err)
	}
}
