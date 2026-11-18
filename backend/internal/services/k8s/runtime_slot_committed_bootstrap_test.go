package k8s

import (
	"context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strings"
	"testing"
)

func TestSlotBootstrapCommittedSourceProof(t *testing.T) {
	for _, scenario := range []string{"preserve", "isolated-active", "deleted", "replaced", "image-drift", "enabled", "missing-proof"} {
		t.Run(scenario, func(t *testing.T) {
			cfg, client := slotBootstrapFixture(t)
			ctx := context.Background()
			old := "registry/openclaw@sha256:" + strings.Repeat("a", 64)
			target := "registry/openclaw@sha256:" + strings.Repeat("b", 64)
			source, err := client.AppsV1().Deployments("tenant").Get(ctx, "openclaw-runtime", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			source.UID = "old-source"
			source.Labels[runtimeSchedulingLabel] = "false"
			for i := range source.Spec.Template.Spec.Containers {
				source.Spec.Template.Spec.Containers[i].Image = old
			}
			if scenario == "replaced" {
				source.UID = "foreign-source"
			}
			if scenario == "enabled" || scenario == "isolated-active" {
				source.Labels[runtimeSchedulingLabel] = "true"
			}
			if scenario == "image-drift" {
				source.Spec.Template.Spec.Containers[0].Image = target
			}
			if _, err = client.AppsV1().Deployments("tenant").Update(ctx, source, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if scenario == "deleted" {
				if err = client.AppsV1().Deployments("tenant").Delete(ctx, source.Name, metav1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			proof := RuntimeSlotBootstrapSourceProof{Source: RuntimeRolloutTarget{Namespace: "tenant", Name: source.Name, UID: "old-source", Image: old}, RolloutID: 41}
			proof.AllowServingSource = scenario == "isolated-active" || scenario == "replaced" || scenario == "image-drift" || scenario == "deleted"
			if scenario == "missing-proof" {
				proof.Source.UID = ""
			}
			cfg.OpenClawImage = target
			before := len(client.Actions())
			err = EnsureRuntimeSlotPools(ctx, client, cfg, proof)
			if scenario == "preserve" || scenario == "isolated-active" {
				if err != nil {
					t.Fatal(err)
				}
				got, _ := client.AppsV1().Deployments("tenant").Get(ctx, source.Name, metav1.GetOptions{})
				if runtimeContainerImage(got.Spec.Template.Spec.Containers) != old || *got.Spec.Replicas != 3 {
					t.Fatal("retained source mutated")
				}
			} else {
				if err == nil {
					t.Fatal("unproven source accepted")
				}
				for _, action := range client.Actions()[before:] {
					if action.GetVerb() != "get" && action.GetVerb() != "list" {
						t.Fatalf("failed bootstrap mutated cluster: %s", action.GetVerb())
					}
				}
			}
		})
	}
}
