package services

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/repository"
)

func TestRuntimeBalanceMySQLCompletedAffinityAdmitsProvenCopies(t *testing.T) {
	sess := upgradeFlowDB(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	oldDigest, newDigest := "sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("b", 64)
	oldImage, newImage := "repo@"+oldDigest, "repo@"+newDigest
	source := elasticManagementPod(1)
	source.RuntimeType = "openclaw"
	source.Namespace = "tenant"
	source.DeploymentName = "source"
	source.ImageRef = oldImage
	source.ImageDigest = &oldDigest
	source.OpenClawVersion = stringPtrOrNil("28")
	source.AgentProtocolVersion = stringPtrOrNil("v3")
	source.SessionStore = stringPtrOrNil("sqlite")
	source.CapabilitiesJSON = stringPtrOrNil(`["container-slots-v1","writer-lease"]`)
	source.WorkspaceStorageIdentity = runtimeStorageIdentityForTest(8, "pvc:tenant/shared:pvc-uid:pv-name", "/data", "", false, false)
	source.RegisteredPodUID = source.PodUID
	source.KubernetesPodUID = source.PodUID
	target := source
	target.ID = 2
	target.PodUID = stringPtrOrNil("target-uid")
	target.RegisteredPodUID = target.PodUID
	target.KubernetesPodUID = target.PodUID
	target.DeploymentName = runtimeUpgradeTargetDeploymentName("source", 7)
	target.ImageRef = newImage
	target.ImageDigest = &newDigest
	copy := target
	copy.ID = 3
	copy.PodUID = stringPtrOrNil("copy-uid")
	copy.RegisteredPodUID = copy.PodUID
	copy.KubernetesPodUID = copy.PodUID
	copy.DeploymentName = "managed-copy"
	copy.Capacity = 2
	copy.Slots = copy.Slots[:2]
	copy.WorkspaceStorageIdentity = runtimeStorageIdentityForTest(2, "pvc:tenant/shared:pvc-uid:pv-name", "/data", "", false, false)
	oldCopy := source
	oldCopy.ID = 4
	oldCopy.PodUID = stringPtrOrNil("old-copy-uid")
	oldCopy.RegisteredPodUID = oldCopy.PodUID
	oldCopy.KubernetesPodUID = oldCopy.PodUID
	oldCopy.DeploymentName = "managed-old-copy"
	oldCopy.Capacity = 2
	oldCopy.Slots = oldCopy.Slots[:2]
	oldCopy.WorkspaceStorageIdentity = copy.WorkspaceStorageIdentity
	raw, _ := json.Marshal(map[string]string{"tenant/source": oldImage})
	exec(`INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation) VALUES(101,1,'openclaw','gateway','lite','running',1),(202,1,'openclaw','gateway','lite','running',1),(303,1,'openclaw','gateway','lite','creating',1)`)
	exec(`INSERT INTO runtime_rollouts(id,runtime_type,target_image_ref,target_image_digest,status,phase,preflight_id,source_images_json,preflight_json,batch_size,started_at) VALUES (7,'openclaw',?,?,'finished','completed','p7',?,'{"exclude_instance_ids":[202]}',1,NOW()-INTERVAL 1 MINUTE)`, newImage, newDigest, string(raw))
	exec(`INSERT INTO runtime_upgrade_items(rollout_id,instance_id,state,created_at,updated_at) VALUES(7,101,'verified',NOW(),NOW())`)
	service := NewRuntimeUpgradeService(sess, repository.NewRuntimeRolloutRepository(sess), repository.NewRuntimePodRepository(sess), repository.NewInstanceRuntimeBindingRepository(sess), nil, "", "")
	service.deployments = recoveryInventory{source, target, copy, oldCopy}
	got, err := service.filterUpgradePlacement(ctx, 101, []models.RuntimePod{copy, oldCopy})
	if err != nil || len(got) != 1 || got[0].ID != copy.ID {
		t.Fatalf("verified copy blocked or changed core admitted: %+v %v", got, err)
	}
	got, err = service.filterUpgradePlacement(ctx, 202, []models.RuntimePod{copy, oldCopy})
	if err != nil || len(got) != 1 || got[0].ID != oldCopy.ID {
		t.Fatalf("excluded changed-image policy bypassed: %+v %v", got, err)
	}
	got, err = service.filterUpgradePlacement(ctx, 303, []models.RuntimePod{copy, oldCopy})
	if err != nil || len(got) != 1 || got[0].ID != copy.ID {
		t.Fatalf("new instance cannot use proven managed copy: %+v %v", got, err)
	}
	service.deployments = recoveryInventory{copy, oldCopy}
	got, err = service.filterUpgradePlacement(ctx, 101, []models.RuntimePod{copy})
	if err != nil || len(got) != 0 {
		t.Fatalf("copy accepted without approved witness: %+v %v", got, err)
	}
	service.deployments = recoveryInventory{source, target, copy, oldCopy}
	exec(`INSERT INTO runtime_rollouts(id,runtime_type,target_image_ref,target_image_digest,status,phase,preflight_id,source_images_json,preflight_json,batch_size) VALUES (8,'openclaw',?,?,'finished','completed','p8',?,'{}',1)`, newImage, newDigest, string(raw))
	exec(`INSERT INTO runtime_upgrade_items(rollout_id,instance_id,state,created_at,updated_at) VALUES(8,202,'verified',NOW(),NOW())`)
	recoveredTarget := target
	recoveredTarget.ID = 5
	recoveredTarget.DeploymentName = runtimeUpgradeTargetDeploymentName("source", 8)
	recoveredTarget.PodUID = stringPtrOrNil("recovery-target-uid")
	recoveredTarget.RegisteredPodUID = recoveredTarget.PodUID
	recoveredTarget.KubernetesPodUID = recoveredTarget.PodUID
	service.deployments = recoveryInventory{source, target, recoveredTarget, copy, oldCopy}
	got, err = service.filterUpgradePlacement(ctx, 202, []models.RuntimePod{copy, oldCopy})
	if err != nil || len(got) != 1 || got[0].ID != copy.ID {
		t.Fatalf("verified recovery did not supersede exclusion: %+v %v", got, err)
	}
	exec(`INSERT INTO runtime_rollouts(id,runtime_type,target_image_ref,status,phase,preflight_id,batch_size) VALUES (9,'openclaw',?,'error','held','p9',1)`, newImage)
	exec(`INSERT INTO runtime_upgrade_items(rollout_id,instance_id,state,recovery_status,created_at,updated_at) VALUES(9,202,'failed','needs_attention',NOW(),NOW())`)
	got, err = service.filterUpgradePlacement(ctx, 202, []models.RuntimePod{copy})
	if err != nil || len(got) != 0 {
		t.Fatalf("unresolved recovery widened placement: %+v %v", got, err)
	}
	wrongStorage := copy
	wrongStorage.WorkspaceStorageIdentity = "another-pvc"
	service.deployments = recoveryInventory{source, target, wrongStorage}
	got, err = service.filterUpgradePlacement(ctx, 101, []models.RuntimePod{wrongStorage})
	if err != nil || len(got) != 0 {
		t.Fatalf("foreign storage accepted: %+v %v", got, err)
	}
	exec(`UPDATE runtime_upgrade_items SET isolated_at=NOW(),runtime_pod_id=1,source_pod_uid=?,source_deployment_name='source',source_image_digest=? WHERE instance_id=101`, *source.PodUID, oldDigest)
	got, err = service.filterUpgradePlacement(ctx, 101, []models.RuntimePod{copy, source, oldCopy})
	if err != nil || len(got) != 1 || got[0].ID != source.ID {
		t.Fatalf("isolation widened: %+v %v", got, err)
	}
	replaced := source
	replaced.PodUID = stringPtrOrNil("replacement-uid")
	got, err = service.filterUpgradePlacement(ctx, 101, []models.RuntimePod{replaced, oldCopy})
	if err != nil || len(got) != 0 {
		t.Fatalf("isolated replacement UID admitted: %+v %v", got, err)
	}
}

func TestRuntimeBalanceMySQLForcePendingUsesPersistedCopyProof(t *testing.T) {
	for _, test := range []struct {
		name                   string
		legacy, sameDeployment bool
	}{{"new-proof", false, false}, {"legacy-same-deployment-only", true, false}, {"legacy-within-deployment", true, true}} {
		t.Run(test.name, func(t *testing.T) {
			sess, service, agent := runtimeManagementMySQLMoveFixture(t)
			ctx := context.Background()
			if _, err := sess.SQL().Exec(`UPDATE runtime_pods SET capabilities_json='["container-slots-v1"]'`); err != nil {
				t.Fatal(err)
			}
			deployments := service.scheduler.deployments.(*fakeRuntimeDeploymentService)
			targetDeployment := "managed-copy"
			if test.sameDeployment {
				targetDeployment = "runtime"
			}
			deployments.pods[0].WorkspaceStorageIdentity = runtimeStorageIdentityForTest(2, "pvc:runtime/shared:pvc-uid:pv-name", "/data", "", false, false)
			deployments.pods[1].WorkspaceStorageIdentity = runtimeStorageIdentityForTest(1, "pvc:runtime/shared:pvc-uid:pv-name", "/data", "", false, false)
			deployments.pods[1].Capacity = 1
			deployments.pods[1].Slots = deployments.pods[1].Slots[:1]
			deployments.pods[1].DeploymentName = targetDeployment
			if _, err := sess.SQL().Exec(`UPDATE runtime_pods SET deployment_name=?,capacity=1 WHERE id=2`, targetDeployment); err != nil {
				t.Fatal(err)
			}
			op, err := service.Evict(ctx, 1, RuntimeSchedulingRequest{Mode: "force"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if op.Items[0].SourcePlacement.StorageIdentity != deployments.pods[0].WorkspaceStorageIdentity {
				t.Fatal("persisted source storage proof was rewritten")
			}
			if test.legacy {
				op.Items[0].SourcePlacement.Capabilities = nil
				raw, _ := json.Marshal(op.Items)
				if _, err := sess.SQL().Exec(`UPDATE runtime_management_operations SET items_json=? WHERE id=?`, string(raw), op.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err = service.Process(ctx); err != nil {
				t.Fatal(err)
			}
			instance, err := service.scheduler.instanceRepo.GetByID(101)
			if err != nil {
				t.Fatal(err)
			}
			agent.createResponse.Port = 20000
			err = service.scheduler.assignInstance(ctx, *instance)
			if test.legacy && !test.sameDeployment {
				if !isRuntimeAssignmentPending(err) || len(agent.createRequests) != 0 {
					t.Fatalf("legacy policy escaped source deployment: %v %+v", err, agent.createRequests)
				}
			} else {
				if err != nil || len(agent.createRequests) != 1 {
					t.Fatalf("proven managed copy unavailable: %v %+v", err, agent.createRequests)
				}
			}
		})
	}
}

func TestRuntimeBalanceMySQLConfirmationQueueAndExecution(t *testing.T) {
	sess, service, agent := runtimeManagementMySQLMoveFixture(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,workspace_path,updated_at) SELECT 102,user_id,type,runtime_type,instance_mode,status,runtime_generation,REPLACE(workspace_path,'101','102'),NOW() FROM instances WHERE id=101`)
	exec(`INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) SELECT 102,1,runtime_type,'gw-102-1',20003,REPLACE(workspace_path,'101','102'),'running',1 FROM instance_runtime_bindings WHERE instance_id=101`)
	exec(`UPDATE runtime_pods SET used_slots=2 WHERE id=1`)
	req := RuntimeBalanceRequest{RuntimeType: "opencode", PodIDs: []int64{1, 2}}
	plan, err := service.BalancePlan(ctx, req)
	if err != nil || !plan.Allowed || len(plan.Moves) != 1 {
		t.Fatalf("balance plan=%+v %v", plan, err)
	}
	exec(`UPDATE runtime_pods SET scheduling_paused=1 WHERE id=2`)
	req.PlanFingerprint = plan.PlanFingerprint
	if _, err = service.Balance(ctx, RuntimeBalanceRequest{RuntimeType: req.RuntimeType, PlanFingerprint: req.PlanFingerprint}, nil); err == nil {
		t.Fatal("execution omitted the confirmed Pod scope")
	}
	if _, err = service.Balance(ctx, req, nil); err == nil {
		t.Fatal("changed admission accepted")
	}
	exec(`UPDATE runtime_pods SET scheduling_paused=0 WHERE id=2`)
	plan, err = service.BalancePlan(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	req.PlanFingerprint = plan.PlanFingerprint
	op, err := service.Balance(ctx, req, nil)
	if err != nil || op.Kind != "balance" || op.Status != "pending" {
		t.Fatalf("queue=%+v %v", op, err)
	}
	runtimeManagementMySQLGuard(t, sess, plan.Moves[0].InstanceID, op.ID)
	if len(agent.deleteRequests) > 0 || len(agent.createRequests) > 0 {
		t.Fatal("plan/enqueue executed Runtime calls")
	}
	agent.createResponse.Port = 20000
	if err = service.Process(ctx); err != nil {
		t.Fatal(err)
	}
	if len(agent.deleteRequests) != 1 || len(agent.createRequests) != 1 {
		t.Fatalf("balance lifecycle calls deletes=%d creates=%d", len(agent.deleteRequests), len(agent.createRequests))
	}
	binding, err := service.scheduler.bindingRepo.GetByInstanceID(ctx, plan.Moves[0].InstanceID)
	if err != nil || binding == nil || binding.RuntimePodID != 2 || binding.Generation != 2 || binding.GatewayPort != 20000 {
		t.Fatalf("balance placement=%+v %v", binding, err)
	}
}
