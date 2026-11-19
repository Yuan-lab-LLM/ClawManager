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
	targets, err := runtimeDefaultScaleTargets("ns", "openclaw", rollouts, nil)
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
		if _, err := runtimeDefaultScaleTargets("ns", "openclaw", append(rollouts, r), nil); err == nil {
			t.Fatal("fell back to retained/default pool without latest canonical proof")
		}
	}
	defaults, err := runtimeDefaultScaleTargets("ns", "openclaw", nil, nil)
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
			targets, err := runtimeDefaultScaleTargets("ns", RuntimeTypeOpenClaw, []models.RuntimeRollout{r}, map[int64]string{r.ID: string(raw)})
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
