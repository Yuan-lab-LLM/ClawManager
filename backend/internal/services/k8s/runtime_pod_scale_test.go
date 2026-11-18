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
	ktesting "k8s.io/client-go/testing"
)

func TestDefaultRuntimeScaleInheritsDeploymentAndAddsCount(t *testing.T) {
	ctx := context.Background()
	svc, kube, d, p := maintenanceSourceFixture()
	d.ResourceVersion = "7"
	replicas := int32(3)
	d.Spec.Replicas = &replicas
	d.Spec.Template.Annotations = map[string]string{"keep": "exact"}
	if _, err := kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	target := RuntimePodScaleTarget{RuntimeType: "openclaw", Namespace: "ns", DeploymentName: d.Name, PodName: p.Name, PodUID: string(p.UID)}
	snapshot, err := svc.InspectRuntimeScale(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Option.SlotCount != 2 || snapshot.Option.Replicas != 3 || snapshot.Option.DeploymentUID != string(d.UID) {
		t.Fatalf("bad option: %+v", snapshot.Option)
	}
	result, err := svc.ExpandRuntimeScale(ctx, target, snapshot, 4)
	if err != nil {
		t.Fatal(err)
	}
	current, err := kube.AppsV1().Deployments("ns").Get(ctx, d.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if *current.Spec.Replicas != 7 || result.PreviousReplicas != 3 || result.AddedPods != 4 || result.Replicas != 7 {
		t.Fatalf("wrong counted expansion: %+v deployment=%d", result, *current.Spec.Replicas)
	}
	if !reflect.DeepEqual(current.Spec.Template, d.Spec.Template) {
		t.Fatal("scale changed Deployment template")
	}
}

func TestDefaultRuntimeScaleRejectsCustomAndChangedTemplate(t *testing.T) {
	for _, mode := range []string{"managed", "lab", "standby", "replacement", "template", "image", "wrong-proof"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			svc, kube, d, p := maintenanceSourceFixture()
			d.ResourceVersion = "1"
			replicas := int32(1)
			d.Spec.Replicas = &replicas
			if _, err := kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			target := RuntimePodScaleTarget{RuntimeType: "openclaw", Namespace: "ns", DeploymentName: d.Name, PodName: p.Name, PodUID: string(p.UID)}
			snapshot, err := svc.InspectRuntimeScale(ctx, target)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "managed":
				d.Labels[runtimeManagedPoolLabel] = "true"
			case "lab":
				d.Labels[upgradeLabPurposeLabel] = upgradeLabPurposeValue
			case "standby":
				d.Labels[runtimePoolRoleLabel] = "upgrade-standby"
			case "replacement":
				d.UID = "replacement"
			case "template":
				d.Spec.Template.Annotations = map[string]string{"changed": "yes"}
			case "image":
				d.Spec.Template.Spec.Containers[0].Image = "repo/runtime:mutable"
			case "wrong-proof":
				target.UpgradeID = "5"
				target.SourceDeployment = "old"
			}
			if _, err := kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.ExpandRuntimeScale(ctx, target, snapshot, 1); err == nil {
				t.Fatalf("admitted %s", mode)
			}
			for _, action := range kube.Actions() {
				if action.GetVerb() == "patch" {
					t.Fatal("unsafe scale wrote to Kubernetes")
				}
			}
		})
	}
}

func TestDefaultRuntimeScaleDoesNotRetryUnknownWrite(t *testing.T) {
	ctx := context.Background()
	svc, kube, d, p := maintenanceSourceFixture()
	d.ResourceVersion = "1"
	replicas := int32(1)
	d.Spec.Replicas = &replicas
	_, _ = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{})
	target := RuntimePodScaleTarget{RuntimeType: "openclaw", Namespace: "ns", DeploymentName: d.Name, PodName: p.Name, PodUID: string(p.UID)}
	snapshot, err := svc.InspectRuntimeScale(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	writes := 0
	kube.PrependReactor("patch", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		writes++
		return true, nil, errors.New("connection reset after unknown write")
	})
	if _, err := svc.ExpandRuntimeScale(ctx, target, snapshot, 2); err == nil {
		t.Fatal("lost unknown-write failure")
	}
	if writes != 1 {
		t.Fatalf("retried ambiguous write %d times", writes)
	}
}

func TestDefaultRuntimeScalePreservesTemplateDespitePodAdmissionDefaults(t *testing.T) {
	ctx := context.Background()
	svc, kube, d, p := maintenanceSourceFixture()
	d.ResourceVersion = "1"
	replicas := int32(1)
	d.Spec.Replicas = &replicas
	_, _ = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{})
	p = p.DeepCopy()
	p.Spec.NodeName = "node-added-by-scheduler"
	p.Spec.Volumes = append(p.Spec.Volumes, corev1.Volume{Name: "kube-api-access-abc", VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{}}})
	for i := range p.Spec.Containers {
		p.Spec.Containers[i].VolumeMounts = append(p.Spec.Containers[i].VolumeMounts, corev1.VolumeMount{Name: "kube-api-access-abc", MountPath: "/var/run/secrets/kubernetes.io/serviceaccount", ReadOnly: true})
	}
	_, _ = kube.CoreV1().Pods("ns").Update(ctx, p, metav1.UpdateOptions{})
	target := RuntimePodScaleTarget{RuntimeType: "openclaw", Namespace: "ns", DeploymentName: d.Name, PodName: p.Name, PodUID: string(p.UID)}
	snapshot, err := svc.InspectRuntimeScale(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExpandRuntimeScale(ctx, target, snapshot, 1); err != nil {
		t.Fatal(err)
	}
	current, _ := kube.AppsV1().Deployments("ns").Get(ctx, d.Name, metav1.GetOptions{})
	if !reflect.DeepEqual(d.Spec.Template, current.Spec.Template) {
		t.Fatal("admitted Pod defaults copied to Deployment")
	}
}

func TestDefaultRuntimeScaleCASFencesConcurrentControllerChanges(t *testing.T) {
	for _, change := range []string{"resourceVersion", "uid"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			svc, kube, d, p := maintenanceSourceFixture()
			d.ResourceVersion = "1"
			replicas := int32(2)
			d.Spec.Replicas = &replicas
			_, _ = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{})
			target := RuntimePodScaleTarget{RuntimeType: "openclaw", Namespace: "ns", DeploymentName: d.Name, PodName: p.Name, PodUID: string(p.UID)}
			expected, err := svc.InspectRuntimeScale(ctx, target)
			if err != nil {
				t.Fatal(err)
			}
			writes := 0
			kube.PrependReactor("patch", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
				writes++
				patch := action.(ktesting.PatchAction).GetPatch()
				var ops []map[string]any
				if err := json.Unmarshal(patch, &ops); err != nil {
					t.Fatal(err)
				}
				if len(ops) != 3 || ops[0]["path"] != "/metadata/uid" || ops[1]["path"] != "/metadata/resourceVersion" || ops[2]["path"] != "/spec/replicas" {
					t.Fatalf("unsafe patch: %s", patch)
				}
				changed := d.DeepCopy()
				if change == "uid" {
					changed.UID = "replacement"
				} else {
					changed.ResourceVersion = "2"
					changed.Labels[runtimeSchedulingLabel] = "false"
				}
				if err := kube.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), changed, "ns"); err != nil {
					t.Fatal(err)
				}
				return false, nil, nil
			})
			if _, err := svc.ExpandRuntimeScale(ctx, target, expected, 3); err == nil {
				t.Fatalf("CAS admitted changed %s", change)
			}
			current, _ := kube.AppsV1().Deployments("ns").Get(ctx, d.Name, metav1.GetOptions{})
			if *current.Spec.Replicas != 2 || writes != 1 {
				t.Fatalf("unsafe CAS result replicas=%d writes=%d", *current.Spec.Replicas, writes)
			}
		})
	}
}
