package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestConfirmedColdTeamSourceRequiresCompleteGenerationChain(t *testing.T) {
	s := models.InstanceIdleStopOperation{InstanceID: 7, UserID: 2, Generation: 1, WorkspacePath: "/workspaces/openclaw/user-2/instance-7", Phase: "completed", RuntimePodID: 42, PodUID: "source", GatewayID: "gw-7-1", Endpoint: "http://10.0.0.1:19090"}
	zero := s
	zero.Generation = 2
	zero.RuntimePodID = 0
	zero.PodUID = ""
	zero.GatewayID = ""
	zero.Endpoint = ""
	for _, tc := range []struct {
		name string
		rows []models.InstanceIdleStopOperation
		good bool
	}{
		{"confirmed source", []models.InstanceIdleStopOperation{zero, s}, true},
		{"missing generation", []models.InstanceIdleStopOperation{s}, false},
		{"pending stop", []models.InstanceIdleStopOperation{func() models.InstanceIdleStopOperation { v := zero; v.Phase = "stopping"; return v }(), s}, false},
		{"wrong workspace", []models.InstanceIdleStopOperation{func() models.InstanceIdleStopOperation { v := zero; v.WorkspacePath = "other"; return v }(), s}, false},
		{"partial source identity", []models.InstanceIdleStopOperation{func() models.InstanceIdleStopOperation { v := zero; v.PodUID = "orphan"; return v }(), s}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := confirmedColdTeamSource(7, 2, 3, s.WorkspacePath, tc.rows)
			if ok != tc.good {
				t.Fatalf("accepted=%v", ok)
			}
		})
	}
}

func TestWholeTeamStopCoversUnpublishedStartTimeoutWithoutInventingSource(t *testing.T) {
	now := time.Now().UTC()
	s := models.InstanceIdleStopOperation{InstanceID: 7, UserID: 2, Generation: 1, WorkspacePath: "/workspace", Phase: "completed", RuntimePodID: 42, PodUID: "pod", GatewayID: "gw-7-1", Endpoint: "http://10.0.0.1:19090", CreatedAt: now.Add(-time.Hour)}
	if _, ok := confirmedColdTeamSourceAfterWholeStop(7, 2, 4, "/workspace", now, []models.InstanceIdleStopOperation{s}); !ok {
		t.Fatal("real whole-Team stop lost")
	}
	for _, bad := range []models.InstanceIdleStopOperation{func() models.InstanceIdleStopOperation { v := s; v.Phase = "stopping"; return v }(), func() models.InstanceIdleStopOperation { v := s; v.CreatedAt = now.Add(time.Minute); return v }(), func() models.InstanceIdleStopOperation { v := s; v.UserID++; return v }()} {
		if _, ok := confirmedColdTeamSourceAfterWholeStop(7, 2, 4, "/workspace", now, []models.InstanceIdleStopOperation{bad}); ok {
			t.Fatal("uncertain source accepted")
		}
	}
}

type verifiedColdNFSInventory struct {
	recoveryInventory
	allowed bool
	calls   int
}

type retainedColdSourceInventory struct {
	recoveryInventory
	source models.InstanceIdleStopOperation
	err    error
}

func (v *retainedColdSourceInventory) ConfirmedTeamStoppedSource(context.Context, int, int, int, string) (models.InstanceIdleStopOperation, error) {
	return v.source, v.err
}

func (v *verifiedColdNFSInventory) ColdTeamNFSWorkspace(_ context.Context, _, _ models.RuntimePod, _ string) (bool, error) {
	v.calls++
	return v.allowed, nil
}

func TestColdTeamNativePlacementMySQLPreservesVersionAndUpgradeFences(t *testing.T) {
	sess := upgradeFlowDB(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := sess.SQL().Exec(q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`ALTER TABLE instances ADD runtime_security_profile VARCHAR(64) NOT NULL DEFAULT ''`)
	exec(`ALTER TABLE teams ADD preview_security_profile VARCHAR(64) NOT NULL DEFAULT ''`)
	exec(`ALTER TABLE team_members ADD instance_id INT, ADD team_id INT, ADD status VARCHAR(32)`)
	exec(`CREATE TABLE team_runtime_lifecycle(team_id INT PRIMARY KEY,state VARCHAR(32),desired_state VARCHAR(32),members_json LONGTEXT)`)
	workspace := "/workspaces/openclaw/user-2/instance-7"
	exec(`INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,workspace_path,runtime_security_profile) VALUES(7,2,'openclaw','gateway','lite','creating',3,?,?)`, workspace, RuntimeSecurityProfile)
	exec(`INSERT INTO teams(id,preview_security_profile) VALUES(9,?)`, models.TeamPreviewSecurityProfile)
	exec(`INSERT INTO team_members(id,instance_id,team_id,status) VALUES(8,7,9,'offline')`)
	exec(`INSERT INTO team_runtime_lifecycle VALUES(9,'starting','running',?)`, `[{"instance_id":7,"member_id":8,"generation":2,"workspace":"`+workspace+`"}]`)
	exec(`INSERT INTO instance_idle_stop_operations(id,instance_id,user_id,generation,runtime_pod_id,gateway_id,pod_uid,endpoint,workspace_path,storage_identity,phase,created_at,updated_at) VALUES('source',7,2,1,42,'gw-7-1','source','http://10.0.0.1:19090',?,'','completed',NOW()-INTERVAL 1 MINUTE,NOW()),('unbound',7,2,2,0,'','','',?,'','completed',NOW(),NOW())`, workspace, workspace)
	oldDigest, newDigest := "sha256:"+strings.Repeat("a", 64), "sha256:"+strings.Repeat("b", 64)
	oldImage, newImage := "repo@"+oldDigest, "repo@"+newDigest
	exec(`INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,deployment_name,image_ref,image_digest,pod_uid,agent_endpoint,state,capacity,used_slots,created_at,updated_at) VALUES(42,'openclaw','tenant','source-pod','source',?,?,'source','http://10.0.0.1:19090','ready',2,0,NOW(),NOW())`, oldImage, oldDigest)
	old := elasticManagementPod(42)
	old.RuntimeType = "openclaw"
	old.Namespace = "tenant"
	old.DeploymentName = "source"
	old.ImageRef = oldImage
	old.ImageDigest = &oldDigest
	old.PodUID = stringPtrOrNil("source")
	old.RegisteredPodUID = old.PodUID
	old.KubernetesPodUID = old.PodUID
	old.KubernetesReady = true
	old.WorkspaceStorageIdentity = runtimeStorageIdentityForTest(2, "pvc:tenant/shared:pvc-uid:pv-name", "/data", "", false, false)
	target := old
	target.ID = 43
	target.ImageRef = newImage
	target.ImageDigest = &newDigest
	target.PodUID = stringPtrOrNil("target")
	target.RegisteredPodUID = target.PodUID
	target.KubernetesPodUID = target.PodUID
	inventory := recoveryInventory{old, target}
	service := NewRuntimeUpgradeService(sess, repository.NewRuntimeRolloutRepository(sess), repository.NewRuntimePodRepository(sess), repository.NewInstanceRuntimeBindingRepository(sess), nil, "", "")
	service.deployments = inventory
	version := "2026.10.2"
	inspect := func(_ context.Context, image string, _ map[string]string, _ bool) (*OpenClawTargetClassification, error) {
		v := "2026.10.2"
		if image == newImage {
			v = version
		}
		return &OpenClawTargetClassification{ImageDigest: imageDigestFromReference(image), RuntimeVersion: v}, nil
	}
	check := func(good bool) {
		t.Helper()
		got, ok, e := service.coldTeamNativePlacement(ctx, 7, []models.RuntimePod{target}, inspect)
		if e != nil || ok != good || (good && (len(got) != 1 || got[0].ID != 43)) {
			t.Fatalf("got=%v ok=%v err=%v", got, ok, e)
		}
	}
	check(true)
	// Exercise the production ordering: ordinary storage filtering would remove
	// the inline-NFS candidate before its retained-PVC proof could run.
	inline := target
	inline.WorkspaceStorageIdentity = runtimeStorageIdentityForTest(2, "nfs:10.0.0.8:/export", "/data", "", false, false)
	verifier := &verifiedColdNFSInventory{recoveryInventory: recoveryInventory{old, inline}, allowed: true}
	service.deployments = verifier
	scheduler := &RuntimeScheduler{upgrade: service}
	instance := models.Instance{ID: 7, Type: RuntimeTypeOpenClaw, RuntimeSecurityProfile: RuntimeSecurityProfile}
	got, cold, err := scheduler.filterResumeWorkspace(ctx, instance, []models.RuntimePod{inline}, old.WorkspaceStorageIdentity, inspect)
	if err != nil || !cold || len(got) != 1 || verifier.calls != 1 {
		t.Fatalf("retained NFS resume: pods=%v cold=%v calls=%d err=%v", got, cold, verifier.calls, err)
	}
	verifier.allowed = false
	got, cold, err = scheduler.filterResumeWorkspace(ctx, instance, []models.RuntimePod{inline}, old.WorkspaceStorageIdentity, inspect)
	if err != nil || cold || len(got) != 0 {
		t.Fatalf("unproved NFS accepted: pods=%v cold=%v err=%v", got, cold, err)
	}
	verifier.allowed = true
	version = "2026.11.1"
	got, cold, err = scheduler.filterResumeWorkspace(ctx, instance, []models.RuntimePod{inline}, old.WorkspaceStorageIdentity, inspect)
	if err != nil || cold || len(got) != 0 {
		t.Fatalf("version fence bypassed: pods=%v cold=%v err=%v", got, cold, err)
	}
	version = "2026.10.2"
	instance.RuntimeSecurityProfile = ""
	got, cold, err = scheduler.filterResumeWorkspace(ctx, instance, []models.RuntimePod{inline}, old.WorkspaceStorageIdentity, inspect)
	if err != nil || cold || len(got) != 0 {
		t.Fatalf("ordinary storage fence bypassed: pods=%v cold=%v err=%v", got, cold, err)
	}
	service.deployments = inventory
	// A retry that never obtained a binding advances the instance generation,
	// but remains under the original completed whole-Team stop provenance.
	stamp := time.Now().UTC().Add(time.Second)
	frozen, marshalErr := json.Marshal([]models.TeamLifecycleMember{{InstanceID: 7, MemberID: 8, Generation: 2, Workspace: workspace, ColdStopOperationID: "whole-stop", ColdStoppedAt: &stamp}})
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	exec(`UPDATE team_runtime_lifecycle SET members_json=?`, string(frozen))
	exec(`UPDATE instances SET runtime_generation=4 WHERE id=7`)
	check(true)
	exec(`INSERT INTO instance_idle_stop_operations(id,instance_id,user_id,generation,runtime_pod_id,gateway_id,pod_uid,endpoint,workspace_path,storage_identity,phase,created_at,updated_at) VALUES('later',7,2,3,42,'gw-7-3','later','http://10.0.0.1:19090',?,'','completed',?,NOW())`, workspace, stamp.Add(time.Second))
	check(false)
	exec(`DELETE FROM instance_idle_stop_operations WHERE id='later'`)
	exec(`UPDATE team_runtime_lifecycle SET members_json=?`, `[{"instance_id":7,"member_id":8,"generation":2,"workspace":"`+workspace+`"}]`)
	check(false)
	// A finalized container stop is independent evidence when failure cleanup
	// bypassed idle-stop SQL. It grants no version, owner or storage override.
	retained := &retainedColdSourceInventory{recoveryInventory: inventory, source: models.InstanceIdleStopOperation{InstanceID: 7, UserID: 2, Generation: 2, RuntimePodID: 42, GatewayID: "gw-7-2", PodUID: "source", Endpoint: "http://10.0.0.1:19090", WorkspacePath: workspace, Phase: "completed", StorageIdentity: old.WorkspaceStorageIdentity}}
	service.deployments = retained
	check(true)
	retained.source.UserID = 3
	check(false)
	retained.source.UserID = 2
	retained.source.Phase = "stopping"
	check(false)
	retained.source.Phase = "completed"
	version = "2026.11.1"
	check(false)
	version = "2026.10.2"
	service.deployments = inventory
	exec(`UPDATE instances SET runtime_generation=3 WHERE id=7`)
	exec(`INSERT INTO runtime_rollouts(id,runtime_type,target_image_ref,status,phase,batch_size,preflight_id,preflight_json) VALUES(3,'openclaw',?,'preflight_blocked','preflight',1,'blocked','{}')`, newImage)
	exec(`INSERT INTO runtime_upgrade_items(rollout_id,instance_id,state,created_at,updated_at) VALUES(3,7,'pending',NOW(),NOW())`)
	check(true)
	// A real native stop/scale-in receipt must survive removal of the source Pod.
	exec(`UPDATE instance_idle_stop_operations SET storage_identity=? WHERE id='source'`, old.WorkspaceStorageIdentity)
	service.deployments = recoveryInventory{target}
	check(true)
	exec(`UPDATE instance_idle_stop_operations SET storage_identity='' WHERE id='source'`)
	check(false)
	service.deployments = inventory
	version = "2026.11.1"
	check(false)
	version = "2026.10.2"
	target.WorkspaceStorageIdentity = runtimeStorageIdentityForTest(2, "pvc:tenant/other:other:other", "/data", "", false, false)
	service.deployments = recoveryInventory{old, target}
	check(false)
	target.WorkspaceStorageIdentity = old.WorkspaceStorageIdentity
	service.deployments = inventory
	exec(`UPDATE instance_idle_stop_operations SET phase='stopping' WHERE id='unbound'`)
	check(false)
	exec(`UPDATE instance_idle_stop_operations SET phase='completed' WHERE id='unbound'`)
	exec(`UPDATE team_runtime_lifecycle SET desired_state='stopped'`)
	check(false)
	exec(`UPDATE team_runtime_lifecycle SET desired_state='running'`)
	exec(`INSERT INTO runtime_rollouts(id,runtime_type,target_image_ref,status,phase,batch_size,preflight_id,preflight_json) VALUES(2,'openclaw',?,'finished','completed',1,'excluded','{"exclude_instance_ids":[7]}')`, newImage)
	check(false)
	exec(`DELETE FROM runtime_rollouts WHERE id=2`)
	exec(`INSERT INTO runtime_rollouts(id,runtime_type,target_image_ref,status,phase,batch_size,preflight_id,preflight_json) VALUES(1,'openclaw',?,'finished','completed',1,'committed','{}')`, newImage)
	exec(`INSERT INTO runtime_upgrade_items(rollout_id,instance_id,state,created_at,updated_at) VALUES(1,7,'verified',NOW(),NOW())`)
	check(false)
}
