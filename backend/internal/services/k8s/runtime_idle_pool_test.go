package k8s

import (
	"context"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRuntimeIdlePoolCanRemoveFinalEmptyPod(t *testing.T) {
	ctx := context.Background()
	svc, kube, d, p := maintenanceSourceFixture()
	d.ResourceVersion = "1"
	one := int32(1)
	d.Spec.Replicas = &one
	_, _ = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{})
	p.ResourceVersion = "1"
	_, _ = kube.CoreV1().Pods("ns").Update(ctx, p, metav1.UpdateOptions{})
	proof := RuntimePodScaleInProtection{OperationID: "idle-op", Namespace: "ns", DeploymentName: d.Name, DeploymentUID: string(d.UID), PreviousReplicas: 1, Replicas: 0, SourcePods: []RuntimePodScaleInIdentity{{PodName: p.Name, PodUID: string(p.UID)}}}
	if err := svc.ProtectRuntimeScaleIn(ctx, proof); err != nil {
		t.Fatalf("final empty Pod cannot be protected for scale to zero: %v", err)
	}
	if err := svc.ResizeRuntimeScaleIn(ctx, RuntimePodScaleTarget{}, &RuntimePodScaleSnapshot{Option: RuntimePodScaleOption{DeploymentUID: string(d.UID)}, Template: d.Spec.Template}, proof); err != nil {
		t.Fatal(err)
	}
	current, _ := kube.AppsV1().Deployments("ns").Get(ctx, d.Name, metav1.GetOptions{})
	if *current.Spec.Replicas != 0 || !reflect.DeepEqual(current.Spec.Template, d.Spec.Template) {
		t.Fatal("idle scale-in did not preserve template at zero")
	}
}

func TestRuntimeIdlePoolWakeRequiresUnchangedAutomaticSleepProof(t *testing.T) {
	for _, change := range []string{"none", "uid", "template", "disabled", "scale-in", "no-proof", "manual-rescale", "pvc-replaced", "consumed"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			svc, kube, d, p := maintenanceSourceFixture()
			d.ResourceVersion = "1"
			one := int32(1)
			d.Spec.Replicas = &one
			_, _ = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{})
			target := RuntimePodScaleTarget{RuntimeType: "openclaw", Namespace: "ns", DeploymentName: d.Name, PodName: p.Name, PodUID: string(p.UID)}
			snapshot, err := svc.InspectRuntimeScale(ctx, target)
			if err != nil {
				t.Fatal(err)
			}
			snapshot.IdleSleepOperation = "sleep-operation"
			d.Generation = snapshot.DeploymentGeneration + 1
			d.Annotations = map[string]string{runtimeIdleSleepAnnotation: snapshot.IdleSleepOperation}
			zero := int32(0)
			d.Spec.Replicas = &zero
			switch change {
			case "manual-rescale":
				d.Generation += 2
			case "consumed":
				delete(d.Annotations, runtimeIdleSleepAnnotation)
			case "pvc-replaced":
				claim, _ := kube.CoreV1().PersistentVolumeClaims("ns").Get(ctx, "workspace", metav1.GetOptions{})
				claim.UID = "new-pvc"
				_, _ = kube.CoreV1().PersistentVolumeClaims("ns").Update(ctx, claim, metav1.UpdateOptions{})
			case "uid":
				d.UID = "replacement"
			case "template":
				d.Spec.Template.Spec.Containers[0].Image = "repo/other:latest"
			case "disabled":
				d.Labels[runtimeSchedulingLabel] = "false"
			case "scale-in":
				d.Labels[RuntimeScaleInOperationLabel] = "other"
			case "no-proof":
				snapshot = nil
			}
			_, _ = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{})
			_ = kube.CoreV1().Pods("ns").Delete(ctx, p.Name, metav1.DeleteOptions{})
			wake, ok := any(svc).(interface {
				WakeIdleRuntimePool(context.Context, RuntimePodScaleTarget, *RuntimePodScaleSnapshot) error
			})
			if !ok {
				t.Fatal("automatic sleep capacity restoration is not implemented")
			}
			err = wake.WakeIdleRuntimePool(ctx, target, snapshot)
			if change == "none" && err != nil {
				t.Fatal(err)
			}
			if change != "none" && err == nil {
				t.Fatal("accepted changed or absent sleep proof")
			}
			current, _ := kube.AppsV1().Deployments("ns").Get(ctx, d.Name, metav1.GetOptions{})
			want := int32(0)
			if change == "none" {
				want = 1
			}
			if *current.Spec.Replicas != want {
				t.Fatal("incorrect capacity after wake")
			}
			for _, action := range kube.Actions() {
				if action.GetVerb() == "delete" && action.GetResource().Resource == "persistentvolumeclaims" {
					t.Fatal("workspace PVC deleted")
				}
			}
		})
	}
}
