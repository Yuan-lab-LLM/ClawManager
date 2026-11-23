package services

import (
	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
	"encoding/json"
	"strings"
	"testing"
)

func TestDefaultRuntimeScaleCanonicalRolloutLineage(t *testing.T) {
	ptr := func(s string) *string { return &s }
	approved := func(id int64, sources string) models.RuntimeRollout {
		return models.RuntimeRollout{ID: id, RuntimeType: "openclaw", Status: "finished", SourceImagesJSON: ptr(sources), TargetImageRef: "image", PreflightID: ptr("approved"), PlanFingerprint: ptr("fingerprint")}
	}
	rollouts := []models.RuntimeRollout{
		approved(5, `{"ns/openclaw-runtime":"old","ns/runtime-openclaw-custom":"other"}`),
		approved(6, `{"ns/openclaw-runtime":"old","ns/openclaw-runtime-u5":"previous","ns/runtime-openclaw-custom":"custom"}`),
	}
	targets, err := runtimeDefaultScaleTargets("ns", "openclaw", "openclaw-runtime", rollouts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[0].DeploymentName != "openclaw-runtime-u5-u6" || targets[1].DeploymentName != "openclaw-runtime-u6" || targets[0].UpgradeID != "6" || targets[0].SourceDeployment != "openclaw-runtime-u5" {
		t.Fatalf("wrong current default candidates: %+v", targets)
	}
	for _, r := range []models.RuntimeRollout{
		{ID: 7, RuntimeType: "openclaw", Status: "finished", TargetImageRef: "image"},
		approved(7, `{"ns/runtime-openclaw-custom":"custom"}`),
	} {
		if _, err := runtimeDefaultScaleTargets("ns", "openclaw", "openclaw-runtime", append(rollouts, r), nil); err == nil {
			t.Fatal("fell back to retained/default pool without latest canonical proof")
		}
	}
	defaults, err := runtimeDefaultScaleTargets("ns", "openclaw", "openclaw-runtime", nil, nil)
	if err != nil || len(defaults) != 1 || defaults[0].DeploymentName != "openclaw-runtime" {
		t.Fatalf("initial default unavailable: %+v %v", defaults, err)
	}
}

func TestOpenClawInPlaceScaleRejectsUnprovenSources(t *testing.T) {
	old := "repo/runtime@sha256:" + strings.Repeat("a", 64)
	next := "repo/runtime@sha256:" + strings.Repeat("b", 64)
	sources := `{"ns/openclaw-runtime":"` + old + `"}`
	r := models.RuntimeRollout{
		ID: 13, RuntimeType: RuntimeTypeOpenClaw, Status: "finished",
		Phase: RuntimeUpgradePhaseEmptyPoolReset, TargetImageRef: next, SourceImagesJSON: &sources,
	}
	source := k8s.RuntimeRolloutTarget{Namespace: "ns", Name: "openclaw-runtime", UID: "default-uid", Image: old}
	for _, tc := range []struct {
		name string
		edit func(*poolReplacementProgress)
	}{
		{"complete", func(*poolReplacementProgress) {}},
		{"wrong namespace", func(p *poolReplacementProgress) { p.Targets[0].Namespace = "other" }},
		{"custom deployment", func(p *poolReplacementProgress) { p.Targets[0].Name = "custom" }},
		{"missing UID", func(p *poolReplacementProgress) { p.Targets[0].UID = "" }},
		{"wrong source image", func(p *poolReplacementProgress) { p.Targets[0].Image = next }},
		{"unfinished", func(p *poolReplacementProgress) { p.PoolIndex = 0 }},
		{"restored", func(p *poolReplacementProgress) { p.Restored = true }},
		{"wrong mode", func(p *poolReplacementProgress) { p.Mode = "legacy" }},
		{"missing checkpoint", func(p *poolReplacementProgress) { p.Kind = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := poolReplacementProgress{Kind: "openclaw-pool-replacement", Stage: "cleanup_success", Version: 2,
				Targets: []k8s.RuntimeRolloutTarget{source}, PoolIndex: 1, ProbeIndex: 1, Changed: true}
			tc.edit(&p)
			raw, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			targets, err := runtimeDefaultScaleTargets("ns", RuntimeTypeOpenClaw, "openclaw-runtime", []models.RuntimeRollout{r}, map[int64]string{r.ID: string(raw)})
			if tc.name != "complete" {
				if err == nil {
					t.Fatalf("unverified checkpoint selected targets: %+v", targets)
				}
				return
			}
			if err != nil || len(targets) != 1 || targets[0].DeploymentName != source.Name ||
				targets[0].DeploymentUID != source.UID || targets[0].ImageRef != next || targets[0].UpgradeID != "" {
				t.Fatalf("in-place reset selected wrong default: %+v (%v)", targets, err)
			}
		})
	}
}

func TestDefaultRuntimeScaleUsesConfiguredPoolRoot(t *testing.T) {
	scheduler := &RuntimeScheduler{}
	WithRuntimeSchedulerPoolPrefix("site-")(scheduler)
	source, preflight, fingerprint := `{"tenant/site-openclaw-runtime":"old"}`, "approved", "fingerprint"
	history := []models.RuntimeRollout{{ID: 9, RuntimeType: "openclaw", Status: "finished", SourceImagesJSON: &source, PreflightID: &preflight, PlanFingerprint: &fingerprint, TargetImageRef: "new"}}
	targets, err := runtimeDefaultScaleTargets("tenant", RuntimeTypeOpenClaw, scheduler.DefaultRuntimePoolName(RuntimeTypeOpenClaw), history, nil)
	if err != nil || len(targets) != 1 || targets[0].DeploymentName != "site-openclaw-runtime-u9" {
		t.Fatalf("configured OpenClaw lineage was not found: %+v %v", targets, err)
	}
	targets, err = runtimeDefaultScaleTargets("tenant", RuntimeTypeOpenCode, scheduler.DefaultRuntimePoolName(RuntimeTypeOpenCode), nil, nil)
	if err != nil || len(targets) != 1 || targets[0].DeploymentName != "site-opencode-runtime" {
		t.Fatalf("configured OpenCode default was not found: %+v %v", targets, err)
	}
}

func TestDefaultRuntimeScaleIgnoresRecoveryAndLabRollouts(t *testing.T) {
	base := "team-openclaw-runtime"
	preflight, fingerprint := "approved", "fingerprint"
	ordinarySource := `{"tenant/team-openclaw-runtime":"old"}`
	ordinary := models.RuntimeRollout{ID: 10, RuntimeType: RuntimeTypeOpenClaw, Status: "finished", SourceImagesJSON: &ordinarySource, PreflightID: &preflight, PlanFingerprint: &fingerprint, TargetImageRef: "new"}
	for _, scope := range []string{`{"recovery_mode":"retry"}`, `{"recovery_mode":"rebuild"}`, `{"upgrade_lab_run_id":42}`} {
		t.Run(scope, func(t *testing.T) {
			unrelatedSource := `{"tenant/other-runtime":"old"}`
			recovery := models.RuntimeRollout{ID: 11, RuntimeType: RuntimeTypeOpenClaw, Status: "finished", SourceImagesJSON: &unrelatedSource, PreflightJSON: &scope, PreflightID: &preflight, PlanFingerprint: &fingerprint, TargetImageRef: "recovery-image"}
			for _, test := range []struct {
				history []models.RuntimeRollout
				want    string
			}{
				{[]models.RuntimeRollout{recovery}, base},
				{[]models.RuntimeRollout{ordinary, recovery}, base + "-u10"},
			} {
				targets, err := runtimeDefaultScaleTargets("tenant", RuntimeTypeOpenClaw, base, test.history, nil)
				if err != nil || len(targets) != 1 || targets[0].DeploymentName != test.want {
					t.Fatalf("scoped rollout changed the ordinary default: got %+v, %v; want %s", targets, err, test.want)
				}
			}
		})
	}
}

// Combining site-specific pool names with new upstream approval evidence must
// retain the configured lineage for both in-place and native successor upgrades.
func TestDefaultRuntimeScaleConfiguredApprovalEvidence(t *testing.T) {
	old := "repo/runtime@sha256:" + strings.Repeat("a", 64)
	next := "repo/runtime@sha256:" + strings.Repeat("b", 64)
	for _, runtime := range []string{RuntimeTypeOpenClaw, RuntimeTypeOpenCode} {
		t.Run(runtime, func(t *testing.T) {
			base := "site-" + runtime + "-runtime"
			sources := `{"tenant/` + base + `":"` + old + `"}`
			r := models.RuntimeRollout{ID: 21, RuntimeType: runtime, Status: "finished", TargetImageRef: next, SourceImagesJSON: &sources}
			checkpoints := map[int64]string{}
			want := base
			if runtime == RuntimeTypeOpenClaw {
				r.Phase = RuntimeUpgradePhaseEmptyPoolReset
				raw, err := json.Marshal(poolReplacementProgress{Kind: "openclaw-pool-replacement", Stage: "cleanup_success", Version: 2, Changed: true, PoolIndex: 1, ProbeIndex: 1, Targets: []k8s.RuntimeRolloutTarget{{Namespace: "tenant", Name: base, UID: "source-uid", Image: old}}})
				if err != nil {
					t.Fatal(err)
				}
				checkpoints[r.ID] = string(raw)
			} else {
				preflight := `{"strategy":"` + RuntimeUpgradeStrategyOpenCodePreserve + `"}`
				proof, fingerprint := "approved", "fingerprint"
				r.PreflightJSON, r.PreflightID, r.PlanFingerprint = &preflight, &proof, &fingerprint
				want = runtimeUpgradeTargetDeploymentName(base, r.ID)
			}
			targets, err := runtimeDefaultScaleTargets("tenant", runtime, base, []models.RuntimeRollout{r}, checkpoints)
			if err != nil || len(targets) != 1 || targets[0].DeploymentName != want || targets[0].ImageRef != next || targets[0].ApprovalSource == "" {
				t.Fatalf("configured approved lineage lost: %+v %v", targets, err)
			}
		})
	}
}
