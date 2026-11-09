package services

import (
	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
	"context"
	"fmt"
	"strings"
	"testing"
)

type stoppedSourceInventory struct {
	assignment k8s.RuntimeAssignment
	err        error
}

func (p stoppedSourceInventory) RuntimeDeploymentPods(context.Context, string) ([]models.RuntimePod, error) {
	return nil, nil
}
func (p stoppedSourceInventory) StoppedRuntimeAssignment(context.Context, int) (*k8s.RuntimeAssignment, error) {
	return &p.assignment, p.err
}
func stoppedSourceString(v string) *string { return &v }
func TestStoppedSourceVersionRequiresMatchingPersistedReceipt(t *testing.T) {
	image := "registry/source@sha256:" + strings.Repeat("a", 64)
	pod := models.RuntimePod{ID: 42, RuntimeType: "openclaw", Namespace: "ns", DeploymentName: "source", PodUID: stoppedSourceString("source-uid"), AgentEndpoint: stoppedSourceString("https://10.0.0.1:19090"), ImageRef: image, ImageDigest: stoppedSourceString(imageDigestFromReference(image)), OpenClawVersion: stoppedSourceString("2026.7.1-2")}
	for _, tc := range []struct {
		name   string
		change func(*stoppedSourceInventory, map[string]string)
		bad    bool
	}{
		{"confirmed receipt", func(*stoppedSourceInventory, map[string]string) {}, false},
		{"missing receipt", func(p *stoppedSourceInventory, _ map[string]string) { p.err = fmt.Errorf("missing") }, true},
		{"wrong owner", func(p *stoppedSourceInventory, _ map[string]string) { p.assignment.UserID++ }, true},
		{"stale generation", func(p *stoppedSourceInventory, _ map[string]string) { p.assignment.Generation-- }, true},
		{"unknown source uid", func(p *stoppedSourceInventory, _ map[string]string) { p.assignment.PodUID = "other" }, true},
		{"replaced rollback image", func(_ *stoppedSourceInventory, live map[string]string) {
			live["ns/source"] = "registry/new@sha256:" + strings.Repeat("b", 64)
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inventory := stoppedSourceInventory{assignment: k8s.RuntimeAssignment{InstanceID: 7, UserID: 2, Generation: 3, RuntimeType: "openclaw", PodUID: "source-uid", GatewayID: "gw-7-3", Endpoint: "https://10.0.0.1:19090"}}
			live := map[string]string{"ns/source": image}
			tc.change(&inventory, live)
			service := RuntimeUpgradeService{deployments: inventory}
			candidates := []runtimeUpgradeCandidate{{InstanceID: 7, UserID: 2, RuntimeGeneration: 3, Generation: 3, InstanceStatus: "stopped"}}
			sources := map[string]string{"ns/running-other": "another-image"}
			inspect := func(_ context.Context, ref string, refs map[string]string, _ bool) (*OpenClawTargetClassification, error) {
				if ref != image || refs["ns/source"] != image {
					t.Fatal("inspected target or guessed pool instead of original source")
				}
				return &OpenClawTargetClassification{RuntimeVersion: "2026.7.1-2", ImageRef: image, ImageDigest: imageDigestFromReference(image)}, nil
			}
			err := service.fillStoppedSourceVersions(context.Background(), candidates, sources, live, []models.RuntimePod{pod}, inspect)
			if (err != nil) != tc.bad {
				t.Fatalf("err=%v", err)
			}
			if !tc.bad && (candidates[0].SourceVersion != "2026.7.1-2" || candidates[0].RuntimePodID == nil || *candidates[0].RuntimePodID != 42 || sources["ns/source"] != image || sources["ns/running-other"] == "") {
				t.Fatalf("lost mixed source evidence: %+v %+v", candidates, sources)
			}
		})
	}
}
func TestUpgradeFingerprintBindsStoppedSourceVersion(t *testing.T) {
	a := []runtimeUpgradeCandidate{{InstanceID: 1, InstanceStatus: "stopped", SourceVersion: "2026.10.2"}}
	before := runtimeUpgradeFingerprint("target", a, map[string]string{"source": "image"})
	a[0].SourceVersion = "2026.7.1-2"
	if before == runtimeUpgradeFingerprint("target", a, map[string]string{"source": "image"}) {
		t.Fatal("source rollback format changed without invalidating preflight")
	}
}
