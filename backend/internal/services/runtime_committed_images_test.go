package services

import (
	"clawreef/internal/config"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCommittedRuntimeImagesMySQLFinishedDefaultsOnly(t *testing.T) {
	sess := upgradeFlowDB(t)
	migration, err := os.ReadFile("../db/migrations/068_add_runtime_rollout_checkpoints.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sess.SQL().Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewRuntimeRolloutRepository(sess)
	now := time.Now().UTC()
	image := "registry/hermes@sha256:" + strings.Repeat("a", 64)
	source := `{"tenant/hermes-runtime":"old"}`
	for _, entry := range []struct{ status, meta, image string }{
		{"finished", `{}`, image}, {"error", `{}`, "failed"}, {"preflight_passed", `{}`, "preflight"},
		{"finished", `{"recovery_mode":"retry"}`, "recovery"}, {"finished", `{"upgrade_lab_run_id":9}`, "lab"},
	} {
		meta := entry.meta
		r := &models.RuntimeRollout{RuntimeType: RuntimeTypeHermes, TargetImageRef: entry.image, Status: entry.status, Phase: "requested", BatchSize: 1, MaxUnavailable: 1, StartedAt: &now, FinishedAt: &now, PreflightJSON: &meta, SourceImagesJSON: &source}
		if err := repo.Create(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := sess.SQL().Exec(`INSERT INTO runtime_rollouts(runtime_type,target_image_ref,status,phase,batch_size,max_unavailable,created_at,updated_at) VALUES('hermes','pending-image','pending','requested',1,1,NOW(),NOW())`); err != nil {
		t.Fatal(err)
	}
	got, err := LoadCommittedRuntimeImages(context.Background(), sess)
	if err != nil {
		t.Fatal(err)
	}
	if got.Images[RuntimeTypeHermes] != image || len(got.Images) != 1 {
		t.Fatalf("committed images=%+v", got.Images)
	}
}

func TestCommittedRuntimeImagesApplyPreservesUnrelatedConfig(t *testing.T) {
	images := &RuntimeCommittedImages{Images: map[string]string{RuntimeTypeHermes: "committed-hermes", RuntimeTypeOpenClaw: "committed-openclaw"}}
	got := images.Apply(config.RuntimePoolConfig{HermesImage: "old", OpenClawImage: "old", OpenCodeImage: "unchanged-opencode", DeepSeekHarnessImage: "unchanged-dsh", ContainerSlots: 8, AgentControlToken: "unchanged-token"})
	if got.HermesImage != "committed-hermes" || got.OpenClawImage != "committed-openclaw" || got.OpenCodeImage != "unchanged-opencode" || got.DeepSeekHarnessImage != "unchanged-dsh" || got.ContainerSlots != 8 || got.AgentControlToken != "unchanged-token" {
		t.Fatal("committed defaults changed unrelated bootstrap settings")
	}
}

func TestCommittedRuntimeGatewayOverlayPreservesOtherCards(t *testing.T) {
	image := "registry/hermes@sha256:" + strings.Repeat("b", 64)
	repo := &stubSystemImageSettingRepository{items: []models.SystemImageSetting{
		{ID: 1, InstanceType: RuntimeTypeHermes, RuntimeType: "desktop", Image: "pro", IsEnabled: true},
		{ID: 2, InstanceType: RuntimeTypeHermes, RuntimeType: "gateway", Image: "old-default", IsEnabled: true},
		{ID: 3, InstanceType: RuntimeTypeHermes, RuntimeType: "gateway", Image: "custom", IsEnabled: true},
	}}
	service := NewSystemImageSettingService(repo).(*systemImageSettingService)
	service.committedImages = func() (*RuntimeCommittedImages, error) {
		return &RuntimeCommittedImages{Images: map[string]string{RuntimeTypeHermes: image}}, nil
	}
	items, err := service.List()
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]string{1: "pro", 2: image, 3: "custom"}
	for _, item := range items {
		if expected, ok := want[item.ID]; ok && item.Image != expected {
			t.Fatalf("card=%+v want=%s", item, expected)
		}
	}
	got, ok := service.GetRuntimeImageForRuntimeType(RuntimeTypeHermes, "gateway")
	if !ok || got.Image != image {
		t.Fatalf("gateway=%+v %v", got, ok)
	}
	got, ok = service.GetRuntimeImageForImage(RuntimeTypeHermes, image)
	if !ok || got.RuntimeType != "gateway" {
		t.Fatalf("exact image=%+v %v", got, ok)
	}
	if repo.items[1].Image != "old-default" {
		t.Fatal("read overlay wrote settings")
	}
}

func TestCommittedRuntimeSourceProofIsFrozen(t *testing.T) {
	sess := upgradeFlowDB(t)
	migration, err := os.ReadFile("../db/migrations/068_add_runtime_rollout_checkpoints.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sess.SQL().Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	image := "registry/openclaw@sha256:" + strings.Repeat("a", 64)
	old := "registry/openclaw@sha256:" + strings.Repeat("b", 64)
	proof := []k8s.RuntimeRolloutTarget{{Namespace: "tenant", Name: "openclaw-runtime", UID: "frozen-source", Image: old}}
	raw, _ := json.Marshal(map[string]any{"source_deployment_proofs": proof})
	meta, source, preflight := string(raw), `{"tenant/openclaw-runtime":"`+old+`"}`, "preflight"
	r := &models.RuntimeRollout{RuntimeType: RuntimeTypeOpenClaw, TargetImageRef: image, Status: "finished", Phase: "postflight", BatchSize: 1, StartedAt: &now, FinishedAt: &now, PreflightJSON: &meta, SourceImagesJSON: &source, PreflightID: &preflight}
	if err := repository.NewRuntimeRolloutRepository(sess).Create(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	got, err := LoadCommittedRuntimeImages(context.Background(), sess)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.BootstrapSources) != 1 || got.BootstrapSources[0].Source.UID != "frozen-source" {
		t.Fatalf("proof=%+v", got.BootstrapSources)
	}
}

func TestCommittedRuntimeActiveSourceRequiresEveryIsolatedBinding(t *testing.T) {
	for _, scenario := range []string{"isolated", "inherited-isolate", "unknown-instance", "new-generation", "pod-uid-drift", "newer-rollout", "restored"} {
		t.Run(scenario, func(t *testing.T) {
			sess := upgradeFlowDB(t)
			migration, err := os.ReadFile("../db/migrations/068_add_runtime_rollout_checkpoints.sql")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = sess.SQL().Exec(string(migration)); err != nil {
				t.Fatal(err)
			}
			exec := func(q string, args ...any) {
				t.Helper()
				if _, e := sess.SQL().Exec(q, args...); e != nil {
					t.Fatal(e)
				}
			}
			image := "registry/openclaw@sha256:" + strings.Repeat("a", 64)
			old := "registry/openclaw@sha256:" + strings.Repeat("b", 64)
			sourceProof := []k8s.RuntimeRolloutTarget{{Namespace: "tenant", Name: "openclaw-runtime", UID: "source-controller", Image: old}}
			raw, _ := json.Marshal(map[string]any{"source_deployment_proofs": sourceProof})
			meta, source, preflight := string(raw), `{"tenant/openclaw-runtime":"`+old+`"}`, "test-proof"
			now := time.Now().UTC()
			rollout := &models.RuntimeRollout{RuntimeType: RuntimeTypeOpenClaw, TargetImageRef: image, Status: "finished", Phase: "postflight", BatchSize: 1, StartedAt: &now, FinishedAt: &now, PreflightJSON: &meta, SourceImagesJSON: &source, PreflightID: &preflight}
			if err = repository.NewRuntimeRolloutRepository(sess).Create(context.Background(), rollout); err != nil {
				t.Fatal(err)
			}
			exec(`INSERT INTO instances(id,user_id,type,runtime_type,status,runtime_generation,workspace_path,updated_at) VALUES(101,1,'openclaw','gateway','error',1,'/workspaces/101',NOW())`)
			exec(`INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,image_digest,state,capacity) VALUES(1,'openclaw','tenant','source-pod','source-pod-uid','openclaw-runtime',?,?,'ready',8)`, old, "sha256:"+strings.Repeat("b", 64))
			exec(`INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation,created_at,updated_at) VALUES(101,1,'openclaw','gw-101-1',20000,'/workspaces/101','running',1,NOW(),NOW())`)
			exec(`INSERT INTO runtime_upgrade_items(rollout_id,instance_id,runtime_pod_id,source_gateway_id,source_generation,source_pod_uid,source_deployment_name,source_image_digest,state,isolated_at,created_at,updated_at) VALUES(?,101,1,'gw-101-1',1,'source-pod-uid','openclaw-runtime',?,'pending',NOW(),NOW(),NOW())`, rollout.ID, "sha256:"+strings.Repeat("b", 64))
			switch scenario {
			case "unknown-instance":
				exec(`INSERT INTO instances(id,user_id,type,runtime_type,status,runtime_generation,workspace_path,updated_at) VALUES(102,1,'openclaw','gateway','running',1,'/workspaces/102',NOW())`)
				exec(`INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation,created_at,updated_at) VALUES(102,1,'openclaw','gw-102-1',20001,'/workspaces/102','running',1,NOW(),NOW())`)
			case "new-generation":
				exec(`UPDATE instances SET runtime_generation=2 WHERE id=101`)
				exec(`UPDATE instance_runtime_bindings SET generation=2 WHERE instance_id=101`)
			case "pod-uid-drift":
				exec(`UPDATE runtime_pods SET pod_uid='foreign-pod' WHERE id=1`)
			case "newer-rollout":
				newer := &models.RuntimeRollout{RuntimeType: RuntimeTypeOpenClaw, TargetImageRef: image, Status: "error", Phase: "postflight", BatchSize: 1}
				if err = repository.NewRuntimeRolloutRepository(sess).Create(context.Background(), newer); err != nil {
					t.Fatal(err)
				}
				exec(`INSERT INTO runtime_upgrade_items(rollout_id,instance_id,state,created_at,updated_at) VALUES(?,101,'pending',NOW(),NOW())`, newer.ID)
			case "inherited-isolate":
				inheritedMeta, _ := json.Marshal(map[string]any{"source_deployment_proofs": sourceProof, "exclude_instance_ids": []int{101}})
				inherited := *rollout
				inherited.ID = 0
				meta, inheritedPreflight := string(inheritedMeta), "inherited-proof"
				inherited.PreflightJSON, inherited.PreflightID = &meta, &inheritedPreflight
				if err = repository.NewRuntimeRolloutRepository(sess).Create(context.Background(), &inherited); err != nil {
					t.Fatal(err)
				}
			case "restored":
				exec(`UPDATE runtime_upgrade_items SET state='restored' WHERE rollout_id=?`, rollout.ID)
				exec(`UPDATE instances SET runtime_generation=2 WHERE id=101`)
				exec(`UPDATE instance_runtime_bindings SET generation=2,gateway_id='gw-101-2' WHERE instance_id=101`)
			}
			got, err := LoadCommittedRuntimeImages(context.Background(), sess)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.BootstrapSources) != 1 {
				t.Fatalf("source proof=%+v", got.BootstrapSources)
			}
			want := scenario == "isolated" || scenario == "restored" || scenario == "inherited-isolate"
			if got.BootstrapSources[0].AllowServingSource != want {
				t.Fatalf("serving authorization=%v want=%v", got.BootstrapSources[0].AllowServingSource, want)
			}
			if got.Images[RuntimeTypeOpenClaw] != image {
				t.Fatal("retained source replaced new-instance default")
			}
		})
	}
}

func TestCommittedRuntimeLegacyLatestTagPreservesConfiguredType(t *testing.T) {
	sess := upgradeFlowDB(t)
	migration, err := os.ReadFile("../db/migrations/068_add_runtime_rollout_checkpoints.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sess.SQL().Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	old := "registry/openclaw@sha256:" + strings.Repeat("b", 64)
	olderDigest := "registry/openclaw@sha256:" + strings.Repeat("a", 64)
	hermes := "registry/hermes@sha256:" + strings.Repeat("c", 64)
	proofs := []k8s.RuntimeRolloutTarget{{Namespace: "tenant", Name: "openclaw-runtime", UID: "old-source", Image: old}}
	raw, _ := json.Marshal(map[string]any{"source_deployment_proofs": proofs})
	meta, source, preflight := string(raw), `{"tenant/openclaw-runtime":"`+old+`"}`, "old-proof"
	repo := repository.NewRuntimeRolloutRepository(sess)
	for _, r := range []*models.RuntimeRollout{
		{RuntimeType: RuntimeTypeOpenClaw, TargetImageRef: olderDigest, Status: "finished", Phase: "postflight", BatchSize: 1, StartedAt: &now, FinishedAt: &now, PreflightJSON: &meta, SourceImagesJSON: &source, PreflightID: &preflight},
		{RuntimeType: RuntimeTypeOpenClaw, TargetImageRef: "registry/openclaw:legacy", Status: "finished", Phase: "requested", BatchSize: 1, StartedAt: &now, FinishedAt: &now},
		{RuntimeType: RuntimeTypeHermes, TargetImageRef: hermes, Status: "finished", Phase: "requested", BatchSize: 1, StartedAt: &now, FinishedAt: &now},
	} {
		if err = repo.Create(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := LoadCommittedRuntimeImages(context.Background(), sess)
	if err != nil {
		t.Fatal(err)
	}
	if got.Images[RuntimeTypeOpenClaw] != "" || got.Images[RuntimeTypeHermes] != hermes || len(got.BootstrapSources) != 0 {
		t.Fatalf("legacy type adopted history: %+v", got)
	}
	cfg := got.Apply(config.RuntimePoolConfig{OpenClawImage: "configured-bootstrap", HermesImage: "old-hermes"})
	if cfg.OpenClawImage != "configured-bootstrap" || cfg.HermesImage != hermes {
		t.Fatalf("bootstrap defaults=%+v", cfg)
	}
	service := NewSystemImageSettingService(&stubSystemImageSettingRepository{items: []models.SystemImageSetting{{InstanceType: RuntimeTypeOpenClaw, RuntimeType: "gateway", Image: "configured-card", IsEnabled: true}}}).(*systemImageSettingService)
	service.committedImages = func() (*RuntimeCommittedImages, error) { return got, nil }
	selection, ok := service.GetRuntimeImageForRuntimeType(RuntimeTypeOpenClaw, "gateway")
	if !ok || selection.Image != "configured-card" {
		t.Fatalf("legacy card=%+v %v", selection, ok)
	}
}

func TestIsImmutableRuntimeImageRejectsMalformedReferences(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	for _, image := range []string{"registry/runtime:latest", "@" + digest, "registry/runtime@sha256:abc", "registry/runtime@sha256:" + strings.Repeat("A", 64), "registry/runtime@sha256:" + strings.Repeat("g", 64), "registry/runtime\n@" + digest, "registry/runtime@other@" + digest} {
		if IsImmutableRuntimeImage(image) {
			t.Errorf("accepted malformed immutable reference %q", image)
		}
	}
	if !IsImmutableRuntimeImage("registry:5000/runtime:tag@" + digest) {
		t.Fatal("valid pinned reference rejected")
	}
}
