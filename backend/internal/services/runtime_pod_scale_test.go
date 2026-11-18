package services

import (
	"clawreef/internal/models"
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
	targets, err := runtimeDefaultScaleTargets("ns", "openclaw", rollouts)
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
		if _, err := runtimeDefaultScaleTargets("ns", "openclaw", append(rollouts, r)); err == nil {
			t.Fatal("fell back to retained/default pool without latest canonical proof")
		}
	}
	defaults, err := runtimeDefaultScaleTargets("ns", "openclaw", nil)
	if err != nil || len(defaults) != 1 || defaults[0].DeploymentName != "openclaw-runtime" {
		t.Fatalf("initial default unavailable: %+v %v", defaults, err)
	}
}
