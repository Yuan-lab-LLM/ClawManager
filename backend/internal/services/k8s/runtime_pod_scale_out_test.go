package k8s

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
)

func TestRuntimeScaleOutKeepsManualExpansionAboveStaleFloor(t *testing.T) {
	ctx := context.Background()
	svc, kube, d, p := maintenanceSourceFixture()
	d.ResourceVersion = "1"
	replicas := int32(2)
	d.Spec.Replicas = &replicas
	_, _ = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{})
	target := RuntimePodScaleTarget{RuntimeType: "openclaw", Namespace: "ns", DeploymentName: d.Name, PodName: p.Name, PodUID: string(p.UID)}
	snapshot, err := svc.InspectRuntimeScale(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExpandRuntimeScale(ctx, target, snapshot, 4); err != nil {
		t.Fatal(err)
	}
	before := len(kube.Actions())
	if err := svc.ScaleOut(ctx, "ns", d.Name, 3); err != nil {
		t.Fatal(err)
	}
	current, _ := kube.AppsV1().Deployments("ns").Get(ctx, d.Name, metav1.GetOptions{})
	if *current.Spec.Replicas != 6 || !reflect.DeepEqual(current.Spec.Template, d.Spec.Template) {
		t.Fatalf("stale floor undid manual expansion: replicas=%d", *current.Spec.Replicas)
	}
	for _, action := range kube.Actions()[before:] {
		if action.GetVerb() == "patch" {
			t.Fatal("satisfied floor performed a write")
		}
	}
}

func TestRuntimeScaleOutOnlyRaisesFloorWithUIDAndVersionCAS(t *testing.T) {
	ctx := context.Background()
	svc, kube, d, _ := maintenanceSourceFixture()
	d.ResourceVersion = "1"
	replicas := int32(2)
	d.Spec.Replicas = &replicas
	_, _ = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{})
	if err := svc.ScaleOut(ctx, "ns", d.Name, 3); err != nil {
		t.Fatal(err)
	}
	current, _ := kube.AppsV1().Deployments("ns").Get(ctx, d.Name, metav1.GetOptions{})
	if *current.Spec.Replicas != 3 || !reflect.DeepEqual(current.Spec.Template, d.Spec.Template) {
		t.Fatal("scale-out failed to preserve template")
	}
	patches := 0
	for _, action := range kube.Actions() {
		if action.GetVerb() == "patch" {
			patches++
			var ops []map[string]any
			if err := json.Unmarshal(action.(ktesting.PatchAction).GetPatch(), &ops); err != nil {
				t.Fatal(err)
			}
			if len(ops) != 3 || ops[0]["path"] != "/metadata/uid" || ops[1]["path"] != "/metadata/resourceVersion" || ops[2]["path"] != "/spec/replicas" || ops[2]["value"] != float64(3) {
				t.Fatalf("unfenced scale-out patch: %+v", ops)
			}
		}
	}
	if patches != 1 {
		t.Fatalf("writes=%d", patches)
	}
}

func TestRuntimeScaleOutFencesConcurrentChangesAndUnknownWrites(t *testing.T) {
	for _, change := range []string{"uid", "resourceVersion", "unknown-write"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			svc, kube, d, _ := maintenanceSourceFixture()
			d.ResourceVersion = "1"
			replicas := int32(2)
			d.Spec.Replicas = &replicas
			_, _ = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{})
			writes := 0
			kube.PrependReactor("patch", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
				writes++
				changed := d.DeepCopy()
				six := int32(6)
				changed.Spec.Replicas = &six
				if change == "uid" {
					changed.UID = "replacement"
				} else {
					changed.ResourceVersion = "2"
				}
				if err := kube.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), changed, "ns"); err != nil {
					t.Fatal(err)
				}
				if change == "unknown-write" {
					return true, nil, errors.New("connection reset after accepted write")
				}
				return false, nil, nil
			})
			if err := svc.ScaleOut(ctx, "ns", d.Name, 3); err == nil {
				t.Fatal("lost controller race/unknown write error")
			}
			current, _ := kube.AppsV1().Deployments("ns").Get(ctx, d.Name, metav1.GetOptions{})
			if *current.Spec.Replicas != 6 || writes != 1 {
				t.Fatalf("scale-out downgraded/retried: replicas=%d writes=%d", *current.Spec.Replicas, writes)
			}
		})
	}
}

func TestRuntimeScaleOutDoesNotResurrectDrainedDeployment(t *testing.T) {
	ctx := context.Background()
	svc, kube, d, _ := maintenanceSourceFixture()
	d.ResourceVersion = "1"
	zero := int32(0)
	d.Spec.Replicas = &zero
	_, _ = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{})
	if err := svc.ScaleOut(ctx, "ns", d.Name, 3); err == nil {
		t.Fatal("stale automatic inventory resurrected drained Deployment")
	}
	for _, action := range kube.Actions() {
		if action.GetVerb() == "patch" {
			t.Fatal("drained Deployment changed")
		}
	}
	// Rollout cleanup retains exact Scale(0) behavior.
	if err := svc.Scale(ctx, "ns", d.Name, 0); err != nil {
		t.Fatal(err)
	}
	current, _ := kube.AppsV1().Deployments("ns").Get(ctx, d.Name, metav1.GetOptions{})
	if *current.Spec.Replicas != 0 {
		t.Fatal("exact rollout drain semantics changed")
	}
}

func TestRuntimeScaleMutationsBlockedDuringScaleIn(t *testing.T) {
	for _, mode := range []string{"automatic", "manual", "exact"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			svc, kube, d, p := maintenanceSourceFixture()
			d.ResourceVersion = "1"
			replicas := int32(2)
			d.Spec.Replicas = &replicas
			d.Labels[RuntimeScaleInOperationLabel] = "active-scale-in"
			_, _ = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{})
			target := RuntimePodScaleTarget{RuntimeType: "openclaw", Namespace: "ns", DeploymentName: d.Name, PodName: p.Name, PodUID: string(p.UID)}
			snapshot, err := svc.InspectRuntimeScale(ctx, target)
			if err != nil {
				t.Fatal(err)
			}
			kube.ClearActions()
			switch mode {
			case "automatic":
				err = svc.ScaleOut(ctx, "ns", d.Name, 3)
			case "manual":
				_, err = svc.ExpandRuntimeScale(ctx, target, snapshot, 1)
			case "exact":
				err = svc.Scale(ctx, "ns", d.Name, 0)
			}
			if err == nil {
				t.Fatal("changed replicas while retained Pods are protected by scale-in")
			}
			for _, a := range kube.Actions() {
				if a.GetVerb() == "patch" {
					t.Fatal("blocked scale wrote to Kubernetes")
				}
			}
		})
	}
}
