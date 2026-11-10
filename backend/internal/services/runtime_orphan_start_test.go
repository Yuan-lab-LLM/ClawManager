package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/runtimeauth"
	"context"
	"errors"
	"testing"
)

func TestRuntimeOrphanRecoveryBlocksRealInstanceStartMySQL(t *testing.T) {
	s, kube, _ := orphanFlowFixture(t)
	ctx := context.Background()
	if _, e := s.sess.SQL().Exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES(101,1,'opencode','gateway','lite','stopped',4,NOW())"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.sess.SQL().Exec("INSERT INTO instance_desired_state(instance_id,desired_power_state) VALUES(101,'stopped') ON DUPLICATE KEY UPDATE desired_power_state='stopped'"); e != nil {
		t.Fatal(e)
	}
	externalCalls := 0
	s.scheduler.agentClient.(*slotRuntimeAgentClient).workspaceExternalStop = func(context.Context, runtimeauth.Identity) error { externalCalls++; return nil }
	if _, e := s.Evict(ctx, 1, RuntimeSchedulingRequest{Mode: "orphan-recovery", SourcePodUID: "old"}, nil); e != nil {
		t.Fatal(e)
	}
	instances := NewInstanceService(s.scheduler.instanceRepo, nil, nil, nil, WithV2RuntimeLifecycle(s.scheduler.podRepo, s.scheduler.bindingRepo, s.scheduler.agentClient, s.scheduler.workspaceRoot))
	NewInstanceAutoStopService(repository.NewInstanceAutoStopRepository(s.sess), instances, kube)
	if e := instances.Start(101); !errors.Is(e, models.ErrInstanceOperationInProgress) {
		t.Fatalf("real Start did not respect active orphan recovery: %v", e)
	}
	current, e := s.scheduler.instanceRepo.GetByID(101)
	if e != nil || current.Status != "stopped" || current.RuntimeGeneration != 4 {
		t.Fatalf("denied Start changed state %+v %v", current, e)
	}
	var desired string
	row, e := s.sess.SQL().QueryRow("SELECT desired_power_state FROM instance_desired_state WHERE instance_id=101")
	if e != nil {
		t.Fatal(e)
	}
	if e = row.Scan(&desired); e != nil {
		t.Fatal(e)
	}
	if desired != "stopped" || externalCalls != 0 {
		t.Fatalf("denied Start changed desired/external writers: %s %d", desired, externalCalls)
	}
}
func TestRuntimeOrphanSubmitCannotRaceAdmittedLifecycleMySQL(t *testing.T) {
	s, _, backend := orphanFlowFixture(t)
	ctx := context.Background()
	if _, e := s.sess.SQL().Exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES(101,1,'opencode','gateway','lite','stopped',4,NOW())"); e != nil {
		t.Fatal(e)
	}
	locker := s.scheduler.instanceRepo.(interface {
		AcquireLifecycleLock(context.Context, int) (func(), error)
	})
	release, e := locker.AcquireLifecycleLock(ctx, 101)
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	if _, e = s.Evict(ctx, 1, RuntimeSchedulingRequest{Mode: "orphan-recovery", SourcePodUID: "old"}, nil); !errors.Is(e, models.ErrInstanceOperationInProgress) {
		t.Fatalf("orphan submit raced an admitted lifecycle: %v", e)
	}
	var count int
	row, e := s.sess.SQL().QueryRow("SELECT COUNT(*) FROM runtime_pod_deletion_intents")
	if e != nil {
		t.Fatal(e)
	}
	if e = row.Scan(&count); e != nil {
		t.Fatal(e)
	}
	if count != 0 || backend.retireCalls != 0 {
		t.Fatal("failed admission changed source execution/fence")
	}
}

func TestRuntimeOrphanSubmitSerializesExistingWorkspaceWriterMySQL(t *testing.T) {
	s, _, backend := orphanFlowFixture(t)
	ctx := context.Background()
	if _, e := s.sess.SQL().Exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES(101,1,'opencode','gateway','lite','stopped',4,NOW())"); e != nil {
		t.Fatal(e)
	}
	entered := make(chan struct{})
	finish := make(chan struct{})
	finished := make(chan error, 1)
	writes := 0
	go func() {
		finished <- s.scheduler.upgrade.WithWorkspaceMutation(ctx, WorkspaceFileScope{InstanceID: 101}, func() error { close(entered); <-finish; writes++; return nil })
	}()
	select {
	case <-entered:
	case e := <-finished:
		t.Fatal("workspace callback was not admitted", e)
	}
	if _, e := s.Evict(ctx, 1, RuntimeSchedulingRequest{Mode: "orphan-recovery", SourcePodUID: "old"}, nil); !errors.Is(e, ErrWorkspaceUpgradeBusy) {
		close(finish)
		<-finished
		t.Fatalf("orphan reservation bypassed admitted workspace callback: %v", e)
	}
	if backend.retireCalls != 0 {
		t.Fatal("busy writer was interrupted")
	}
	close(finish)
	if e := <-finished; e != nil || writes != 1 {
		t.Fatalf("existing writer did not complete %d %v", writes, e)
	}
	if _, e := s.Evict(ctx, 1, RuntimeSchedulingRequest{Mode: "orphan-recovery", SourcePodUID: "old"}, nil); e != nil {
		t.Fatal(e)
	}
	if e := s.scheduler.upgrade.WithWorkspaceMutation(ctx, WorkspaceFileScope{InstanceID: 101}, func() error { writes++; return nil }); e == nil || writes != 1 {
		t.Fatal("new writer entered after durable orphan reservation", e)
	}
}
func TestRuntimeOrphanSubmitFailureRollsBackGuardAndFenceMySQL(t *testing.T) {
	s, _, backend := orphanFlowFixture(t)
	ctx := context.Background()
	if _, e := s.sess.SQL().Exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES(101,1,'opencode','gateway','lite','stopped',4,NOW())"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.sess.SQL().Exec("CREATE TRIGGER fail_orphan_insert BEFORE INSERT ON runtime_management_operations FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected insert failure'"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Evict(ctx, 1, RuntimeSchedulingRequest{Mode: "orphan-recovery", SourcePodUID: "old"}, nil); e == nil {
		t.Fatal("operation insert failure ignored")
	}
	for _, table := range []string{"instance_lifecycle_manual_guards", "runtime_pod_deletion_intents", "runtime_management_operations"} {
		var count int
		row, e := s.sess.SQL().QueryRow("SELECT COUNT(*) FROM " + table)
		if e != nil {
			t.Fatal(e)
		}
		if e = row.Scan(&count); e != nil {
			t.Fatal(e)
		}
		if count != 0 {
			t.Fatalf("failed operation stranded %s rows=%d", table, count)
		}
	}
	p, e := s.scheduler.podRepo.GetByID(ctx, 1)
	if e != nil || p.SchedulingPaused || p.UsedSlots != 2 || backend.retireCalls != 0 {
		t.Fatalf("failed admission changed source %+v %v", p, e)
	}
}
