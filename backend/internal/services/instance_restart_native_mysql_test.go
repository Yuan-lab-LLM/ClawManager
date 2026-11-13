package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

type nativeRestartAgent struct {
	*fakeRuntimeAgentClient
	beforeDelete func(context.Context) error
}

func (a *nativeRestartAgent) DeleteGateway(ctx context.Context, endpoint, id string) error {
	if err := a.beforeDelete(ctx); err != nil {
		return err
	}
	return a.fakeRuntimeAgentClient.DeleteGateway(ctx, endpoint, id)
}

func TestNativeRestartDoesNotStopPendingGeneration(t *testing.T) {
	repo := &deniedRestartFenceRepo{v2LifecycleInstanceRepo: newV2LifecycleInstanceRepo()}
	repo.byID[1] = &models.Instance{ID: 1, UserID: 7, Type: "openclaw", RuntimeType: "gateway", InstanceMode: "lite", Status: "creating", RuntimeGeneration: 2}
	// A pending start must be rejected before any durable stop dependency is used.
	service := &instanceService{instanceRepo: repo, autoStopRepo: &repository.InstanceAutoStopRepository{}}
	configureLifecycleModels(t, service)
	if err := service.Restart(1); err == nil {
		t.Fatal("restart accepted a pending generation")
	}
	if instance := repo.byID[1]; instance.Status != "creating" || instance.RuntimeGeneration != 2 {
		t.Fatalf("restart changed pending generation: %+v", instance)
	}
}

func TestNativeRestartStopsOriginalGenerationBeforeFencing(t *testing.T) {
	for _, typ := range []string{"openclaw", "hermes", "opencode", "deepseek-harness"} {
		for _, editEnvironment := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/environment=%t", typ, editEnvironment), func(t *testing.T) {
				session := idleLifecycleDB(t)
				ctx := t.Context()
				for _, q := range []string{
					`ALTER TABLE instances
 ADD owner VARCHAR(255) NULL, ADD name VARCHAR(255) NOT NULL DEFAULT '', ADD alias VARCHAR(255) NULL,
 ADD description TEXT NULL, ADD runtime_variant VARCHAR(32) NOT NULL DEFAULT '',
 ADD cpu_cores DOUBLE NOT NULL DEFAULT 0, ADD memory_gb INT NOT NULL DEFAULT 0, ADD disk_gb INT NOT NULL DEFAULT 0,
 ADD gpu_enabled BOOLEAN NOT NULL DEFAULT FALSE, ADD gpu_type VARCHAR(255) NULL, ADD gpu_count INT NOT NULL DEFAULT 0,
 ADD os_type VARCHAR(32) NOT NULL DEFAULT '', ADD os_version VARCHAR(32) NOT NULL DEFAULT '',
 ADD image_registry TEXT NULL, ADD image_tag TEXT NULL, ADD environment_overrides_json TEXT NULL,
 ADD storage_class VARCHAR(255) NOT NULL DEFAULT '', ADD pvc_name VARCHAR(255) NULL, ADD mount_path VARCHAR(1024) NOT NULL DEFAULT '',
 ADD provisioning_operation_id VARCHAR(255) NULL, ADD pod_name VARCHAR(255) NULL, ADD pod_namespace VARCHAR(255) NULL,
 ADD pod_ip VARCHAR(255) NULL, ADD access_url TEXT NULL, ADD openclaw_config_snapshot_id INT NULL`,
					`CREATE TABLE llm_models(id INT PRIMARY KEY,is_active BOOLEAN)`,
					`INSERT INTO llm_models VALUES(1,TRUE)`,
				} {
					if _, err := session.SQL().Exec(q); err != nil {
						t.Fatal(err)
					}
				}
				workspace := "/workspaces/" + typ + "/user-7/instance-1"
				if _, err := session.SQL().Exec(`INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,workspace_path,updated_at) VALUES(1,7,?,'gateway','lite','running',2,?,UTC_TIMESTAMP())`, typ, workspace); err != nil {
					t.Fatal(err)
				}
				if _, err := session.SQL().Exec(`INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,agent_endpoint,state,capacity,used_slots) VALUES(11,?,'runtime','pool','source-uid','pool','test','https://10.0.0.2:19090','ready',1,1)`, typ); err != nil {
					t.Fatal(err)
				}
				if _, err := session.SQL().Exec(`INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES(1,11,?,'gw-1-2',20000,?,'running',2)`, typ, workspace); err != nil {
					t.Fatal(err)
				}
				store := repository.NewInstanceAutoStopRepository(session)
				if err := store.Initialize(ctx); err != nil {
					t.Fatal(err)
				}
				instances := repository.NewInstanceRepository(session)
				bindings := repository.NewInstanceRuntimeBindingRepository(session)
				deleteCalls := 0
				operationID := ""
				agent := &nativeRestartAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}}
				agent.beforeDelete = func(ctx context.Context) error {
					deleteCalls++
					activity, err := store.GetActivity(ctx, 1)
					if err != nil {
						return err
					}
					if activity == nil || activity.StopOperationID == "" || activity.StoppedReason != "manual" {
						t.Fatal("runtime contacted before durable manual stop intent")
					}
					if operationID == "" {
						operationID = activity.StopOperationID
					} else if operationID != activity.StopOperationID {
						t.Fatal("restart retry replaced the original stop operation")
					}
					instance, err := instances.GetByID(1)
					if err != nil || instance == nil || instance.RuntimeGeneration != 2 || instance.Status != "stopped" {
						t.Fatalf("source generation changed before teardown: %+v %v", instance, err)
					}
					if err := instances.UpdateRuntimeState(ctx, 1, "running", 2, nil); !errors.Is(err, models.ErrInstanceStopping) {
						t.Fatalf("old heartbeat accepted during native stop: %v", err)
					}
					if deleteCalls == 1 {
						return errors.New("native teardown receipt unavailable")
					}
					return nil
				}
				service := &instanceService{instanceRepo: instances, bindingRepo: bindings, runtimePodRepo: repository.NewRuntimePodRepository(session), autoStopRepo: store, agentClient: agent}
				configureLifecycleModels(t, service)
				rechecked := 0
				service.SetRuntimeSlotRechecker(func(podID int64) {
					rechecked++
					activity, err := store.GetActivity(ctx, 1)
					if err != nil || activity.StopPhase != "teardown_confirmed" || podID != 11 {
						t.Fatalf("source released before durable native proof: %+v %v", activity, err)
					}
				})
				restart := func() error {
					if editEnvironment {
						return service.RestartWithEnvironment(1, map[string]string{"LOG_LEVEL": "debug"}, nil)
					}
					return service.Restart(1)
				}
				if err := restart(); err == nil || !strings.Contains(err.Error(), "native teardown receipt unavailable") {
					t.Fatalf("restart must reach original generation teardown: %v", err)
				}
				binding, err := bindings.GetByInstanceID(ctx, 1)
				if err != nil || binding == nil || binding.Generation != 2 || rechecked != 0 {
					t.Fatalf("failed teardown released original binding: %+v %v rechecked=%d", binding, err, rechecked)
				}
				if err := restart(); err != nil {
					t.Fatalf("restart retry failed: %v", err)
				}
				instance, err := instances.GetByID(1)
				if err != nil || instance == nil || instance.Status != "creating" || instance.RuntimeGeneration != 4 || stringValue(instance.WorkspacePath) != workspace {
					t.Fatalf("restart did not preserve home and fence old generation: %+v %v", instance, err)
				}
				if err := instances.UpdateRuntimeState(ctx, 1, "running", 2, nil); !errors.Is(err, repository.ErrStaleRuntimeGeneration) {
					t.Fatalf("old heartbeat accepted after restart: %v", err)
				}
				binding, err = bindings.GetByInstanceID(ctx, 1)
				if err != nil || binding != nil || deleteCalls != 2 || rechecked != 1 {
					t.Fatalf("source binding was not released exactly once: %+v %v deletes=%d rechecked=%d", binding, err, deleteCalls, rechecked)
				}
				var phase string
				var generation, usedSlots int
				row, err := session.SQL().QueryRow(`SELECT o.phase,o.generation,p.used_slots FROM instance_idle_stop_operations o JOIN runtime_pods p ON p.id=o.runtime_pod_id WHERE o.id=?`, operationID)
				if err != nil {
					t.Fatal(err)
				}
				if err := row.Scan(&phase, &generation, &usedSlots); err != nil || phase != "completed" || generation != 2 || usedSlots != 0 {
					t.Fatalf("native stop receipt/slot lost: phase=%s generation=%d slots=%d err=%v", phase, generation, usedSlots, err)
				}
			})
		}
	}
}
