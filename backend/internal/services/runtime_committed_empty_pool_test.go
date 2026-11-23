package services

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"clawreef/internal/config"
	"clawreef/internal/services/k8s"
)

func TestCommittedEmptyPoolSurvivesBackendRestartMySQL(t *testing.T) {
	for _, rt := range []string{"openclaw", "hermes", "opencode", "deepseek-harness"} {
		for _, slots := range []int{2, 8} {
			t.Run(fmt.Sprintf("%s/%d", rt, slots), func(t *testing.T) {
				sess := runtimeManagementMySQLDB(t)
				exec := func(q string, args ...any) {
					t.Helper()
					if _, err := sess.SQL().Exec(q, args...); err != nil {
						t.Fatal(err)
					}
				}
				exec("CREATE TABLE IF NOT EXISTS runtime_rollout_checkpoints(rollout_id BIGINT PRIMARY KEY,state_json JSON)")
				old := "repo/runtime@sha256:" + strings.Repeat("a", 64)
				image := "repo/runtime@sha256:" + strings.Repeat("b", 64)
				plan := emptyPoolRecoveryPlan{Kind: RuntimeEmptyPoolRecoveryStrategy, Version: 1, SourceUID: "controller-uid", Proof: &k8s.EmptyRuntimePoolProof{Source: k8s.RuntimeRolloutTarget{Namespace: "tenant", Name: rt + "-runtime", UID: "controller-uid", Image: old}, Configuration: strings.Repeat("c", 64), SlotCount: slots}}
				raw, _ := json.Marshal(plan)
				sources, _ := json.Marshal(map[string]string{replacementKey(plan.Proof.Source): old})
				// An older retained-source proof must not resurrect the retired
				// image or demand isolated bindings after this empty epoch.
				exec("INSERT INTO runtime_rollouts(id,runtime_type,target_image_ref,status,phase,preflight_id,preflight_json,source_images_json) VALUES(10,?,?,'finished','postflight','old-proof',?,?)", rt, old, `{"source_deployment_proofs":[{"namespace":"tenant","name":"`+rt+`-runtime","uid":"retired-controller","image":"`+old+`"}]}`, string(sources))
				exec("INSERT INTO runtime_rollouts(id,runtime_type,target_image_ref,status,phase,preflight_json,source_images_json,plan_fingerprint) VALUES(20,?,?,'finished',?,?,?,?)", rt, image, RuntimeEmptyPoolRecoveryPhase, string(raw), string(sources), emptyPoolFingerprint(plan, image))
				got, err := LoadCommittedRuntimeImages(t.Context(), sess)
				if err != nil {
					t.Fatal(err)
				}
				if got.Images[rt] != image || len(got.BootstrapSources) != 0 {
					t.Fatalf("restart lost committed empty-pool default: %+v", got)
				}
				cfg := got.Apply(config.RuntimePoolConfig{ContainerSlots: slots})
				images := map[string]string{"openclaw": cfg.OpenClawImage, "hermes": cfg.HermesImage, "opencode": cfg.OpenCodeImage, "deepseek-harness": cfg.DeepSeekHarnessImage}
				if images[rt] != image || cfg.ContainerSlots != slots {
					t.Fatal("bootstrap did not retain the committed image and slot count")
				}
				exec("UPDATE runtime_rollouts SET plan_fingerprint='tampered' WHERE id=20")
				if _, err = LoadCommittedRuntimeImages(t.Context(), sess); err == nil {
					t.Fatal("invalid recovery proof became a restart default")
				}
			})
		}
	}
}
