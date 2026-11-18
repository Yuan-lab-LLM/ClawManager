package k8s

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"
)

func TestRuntimePodSchedulingRestoresOnlyDeploymentMetadata(t *testing.T) {
	ctx := context.Background()
	svc, kube, d, p := maintenanceSourceFixture()
	one := int32(1)
	d.Spec.Replicas = &one
	d.ResourceVersion = "7"
	d.Labels[runtimeSchedulingLabel] = "false"
	d.Spec.Template.Labels[runtimeSchedulingLabel] = "false"
	if _, err := kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	target := RuntimePodControllerIdentity{Namespace: "ns", PodName: p.Name, PodUID: string(p.UID), DeploymentName: d.Name, RuntimeType: "openclaw"}
	if err := svc.EnableRuntimePodScheduling(ctx, target); err != nil {
		t.Fatal(err)
	}
	after, err := kube.AppsV1().Deployments("ns").Get(ctx, d.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if after.Labels[runtimeSchedulingLabel] != "true" || !reflect.DeepEqual(d.Spec, after.Spec) {
		t.Fatal("resume must enable only Deployment metadata without restarting/changing replicas or template")
	}
	patches := 0
	for _, a := range kube.Actions() {
		if a.GetVerb() == "patch" {
			patches++
			var ops []map[string]any
			if err := json.Unmarshal(a.(clienttesting.PatchAction).GetPatch(), &ops); err != nil {
				t.Fatal(err)
			}
			if len(ops) != 3 || ops[0]["path"] != "/metadata/uid" || ops[1]["path"] != "/metadata/resourceVersion" || ops[2]["path"] != "/metadata/labels/clawmanager.io~1scheduling-enabled" {
				t.Fatalf("unsafe patch: %v", ops)
			}
		}
	}
	if patches != 1 {
		t.Fatalf("patches=%d", patches)
	}
	// Resuming an already-enabled pool does no Kubernetes mutation.
	if err := svc.EnableRuntimePodScheduling(ctx, target); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, a := range kube.Actions() {
		if a.GetVerb() == "patch" {
			n++
		}
	}
	if n != 1 {
		t.Fatal("repeated resume changed controller")
	}
}
func TestRuntimePodSchedulingRejectsStaleAndProtectedPools(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*appsv1.Deployment, *corev1.Pod, *RuntimePodControllerIdentity)
	}{
		{"wrong Pod UID", func(d *appsv1.Deployment, p *corev1.Pod, v *RuntimePodControllerIdentity) { v.PodUID = "old" }},
		{"wrong deployment", func(d *appsv1.Deployment, p *corev1.Pod, v *RuntimePodControllerIdentity) { v.DeploymentName = "other" }},
		{"wrong runtime", func(d *appsv1.Deployment, p *corev1.Pod, v *RuntimePodControllerIdentity) { v.RuntimeType = "hermes" }},
		{"Pod deleting", func(d *appsv1.Deployment, p *corev1.Pod, v *RuntimePodControllerIdentity) {
			now := metav1.Now()
			p.DeletionTimestamp = &now
		}},
		{"Pod not ready", func(d *appsv1.Deployment, p *corev1.Pod, v *RuntimePodControllerIdentity) { p.Status.Conditions = nil }},
		{"Deployment deleting", func(d *appsv1.Deployment, p *corev1.Pod, v *RuntimePodControllerIdentity) {
			now := metav1.Now()
			d.DeletionTimestamp = &now
		}},
		{"zero replicas", func(d *appsv1.Deployment, p *corev1.Pod, v *RuntimePodControllerIdentity) {
			n := int32(0)
			d.Spec.Replicas = &n
		}},
		{"standby", func(d *appsv1.Deployment, p *corev1.Pod, v *RuntimePodControllerIdentity) {
			d.Labels[runtimePoolRoleLabel] = "upgrade-standby"
		}},
		{"inactive upgrade target", func(d *appsv1.Deployment, p *corev1.Pod, v *RuntimePodControllerIdentity) {
			d.Labels[runtimePoolRoleLabel] = "upgrade-target"
		}},
		{"lab", func(d *appsv1.Deployment, p *corev1.Pod, v *RuntimePodControllerIdentity) {
			d.Labels[upgradeLabPurposeLabel] = upgradeLabPurposeValue
		}},
		{"template lab", func(d *appsv1.Deployment, p *corev1.Pod, v *RuntimePodControllerIdentity) {
			d.Spec.Template.Labels[runtimePoolRoleLabel] = "upgrade-lab"
		}},
		{"scale in", func(d *appsv1.Deployment, p *corev1.Pod, v *RuntimePodControllerIdentity) {
			d.Labels[RuntimeScaleInOperationLabel] = "operation"
		}},
		{"sleep", func(d *appsv1.Deployment, p *corev1.Pod, v *RuntimePodControllerIdentity) {
			d.Annotations = map[string]string{runtimeIdleSleepAnnotation: "operation"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			svc, kube, d, p := maintenanceSourceFixture()
			one := int32(1)
			d.Spec.Replicas = &one
			d.ResourceVersion = "7"
			d.Labels[runtimeSchedulingLabel] = "false"
			target := RuntimePodControllerIdentity{Namespace: "ns", PodName: p.Name, PodUID: string(p.UID), DeploymentName: d.Name, RuntimeType: "openclaw"}
			tc.mutate(d, p, &target)
			if _, err := kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := kube.CoreV1().Pods("ns").Update(ctx, p, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := svc.EnableRuntimePodScheduling(ctx, target); err == nil {
				t.Fatal("unsafe resume accepted")
			}
			for _, a := range kube.Actions() {
				if a.GetVerb() == "patch" {
					t.Fatal("rejected target was mutated")
				}
			}
		})
	}
}
func TestRuntimePodSchedulingPropagatesPatchConflict(t *testing.T) {
	ctx := context.Background()
	svc, kube, d, p := maintenanceSourceFixture()
	one := int32(1)
	d.Spec.Replicas = &one
	d.ResourceVersion = "7"
	d.Labels[runtimeSchedulingLabel] = "false"
	if _, err := kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	conflict := errors.New("resourceVersion conflict")
	kube.PrependReactor("patch", "deployments", func(clienttesting.Action) (bool, runtime.Object, error) { return true, nil, conflict })
	err := svc.EnableRuntimePodScheduling(ctx, RuntimePodControllerIdentity{Namespace: "ns", PodName: p.Name, PodUID: string(p.UID), DeploymentName: d.Name, RuntimeType: "openclaw"})
	if !errors.Is(err, conflict) {
		t.Fatalf("conflict not returned: %v", err)
	}
}

func TestRuntimePodSchedulingResumesWokenAndNoncanonicalDisabledPools(t *testing.T) {
	for _, tc := range []struct {
		label string
		woken bool
	}{{"false", true}, {"False", false}, {"FALSE", false}, {"0", false}} {
		t.Run(tc.label+map[bool]string{true: " after wake", false: ""}[tc.woken], func(t *testing.T) {
			ctx := context.Background()
			svc, kube, d, p := maintenanceSourceFixture()
			one := int32(1)
			d.Spec.Replicas = &one
			d.ResourceVersion = "7"
			d.Labels[runtimeSchedulingLabel] = tc.label
			if tc.woken {
				d.Annotations = map[string]string{runtimeIdleWakeAnnotation: "completed-sleep-journal"}
			}
			if _, err := kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := svc.EnableRuntimePodScheduling(ctx, RuntimePodControllerIdentity{Namespace: "ns", PodName: p.Name, PodUID: string(p.UID), DeploymentName: d.Name, RuntimeType: "openclaw"}); err != nil {
				t.Fatal(err)
			}
			after, err := kube.AppsV1().Deployments("ns").Get(ctx, d.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if after.Labels[runtimeSchedulingLabel] != "true" {
				t.Fatal("successful resume left live gate disabled")
			}
			if !reflect.DeepEqual(d.Annotations, after.Annotations) {
				t.Fatal("resume removed the historical wake journal")
			}
		})
	}
}
