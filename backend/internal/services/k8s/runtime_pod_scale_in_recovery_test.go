package k8s

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
	"testing"
)

func TestInterruptedScaleInRequiresCompleteNativeIdentityProof(t *testing.T) {
	for _, scenario := range []string{"complete", "missing-source-proof", "source-owner-changed", "source-replaced", "paused"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			svc, kube, d, pod := maintenanceSourceFixture()
			d.ResourceVersion = "1"
			one := int32(1)
			d.Spec.Replicas = &one
			if _, err := kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			snapshot, err := svc.InspectRuntimeScale(ctx, RuntimePodScaleTarget{RuntimeType: "openclaw", Namespace: "ns", DeploymentName: d.Name, PodName: pod.Name, PodUID: string(pod.UID)})
			if err != nil {
				t.Fatal(err)
			}
			proof := RuntimePodScaleInProtection{OperationID: "op", Namespace: "ns", DeploymentName: d.Name, DeploymentUID: string(d.UID), PreviousReplicas: 1, Replicas: 0, SourcePods: []RuntimePodScaleInIdentity{{PodName: pod.Name, PodUID: string(pod.UID)}}}
			zero := int32(0)
			d.Spec.Replicas = &zero
			d.Generation = snapshot.DeploymentGeneration + 1
			d.Status.ObservedGeneration = d.Generation
			d.Labels[RuntimeScaleInOperationLabel] = "op"
			d.Annotations = map[string]string{runtimeScaleInResizeOperationAnnotation: "op"}
			if scenario == "paused" {
				d.Spec.Paused = true
			}
			if _, err = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if scenario == "source-owner-changed" || scenario == "source-replaced" {
				pod.OwnerReferences = nil
				if scenario == "source-replaced" {
					pod.UID = "replacement"
				}
				if _, err = kube.CoreV1().Pods("ns").Update(ctx, pod, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			} else if err = kube.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), "ns", pod.Name); err != nil {
				t.Fatal(err)
			}
			if scenario == "missing-source-proof" {
				proof.SourcePods = nil
			}
			ready, err := svc.ObserveInterruptedRuntimeScaleIn(ctx, proof, snapshot)
			if scenario == "complete" {
				if err != nil || !ready {
					t.Fatalf("ready=%v err=%v", ready, err)
				}
			} else if err == nil {
				t.Fatalf("accepted changed native proof, ready=%v", ready)
			}
		})
	}
}

func TestScaleInCleanupRetainsGuardWhenRetainedIdentityChangesAfterObservation(t *testing.T) {
	ctx := context.Background()
	svc, kube, d, pod := maintenanceSourceFixture()
	d.ResourceVersion = "1"
	one := int32(1)
	d.Spec.Replicas = &one
	d.Status.Replicas, d.Status.UpdatedReplicas, d.Status.ReadyReplicas = 1, 1, 1
	d.Labels[RuntimeScaleInOperationLabel] = "op"
	if _, err := kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	q := pod.DeepCopy()
	q.Name = "retained"
	q.UID = "retained-uid"
	q.ResourceVersion = "1"
	q.Labels = map[string]string{RuntimeScaleInProtectedLabel: "op"}
	if _, err := kube.CoreV1().Pods("ns").Create(ctx, q, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := kube.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), "ns", pod.Name); err != nil {
		t.Fatal(err)
	}
	proof := RuntimePodScaleInProtection{OperationID: "op", Namespace: "ns", DeploymentName: d.Name, DeploymentUID: string(d.UID), PreviousReplicas: 2, Replicas: 1, SourcePods: []RuntimePodScaleInIdentity{{PodName: pod.Name, PodUID: string(pod.UID)}}, RetainedPods: []RuntimePodScaleInIdentity{{PodName: q.Name, PodUID: string(q.UID)}}}
	kube.PrependReactor("get", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		if action.(ktesting.GetAction).GetName() == q.Name {
			replacement := q.DeepCopy()
			replacement.UID = "replacement"
			return true, replacement, nil
		}
		return false, nil, nil
	})
	if err := svc.ReleaseRuntimeScaleIn(ctx, proof, false); err == nil {
		t.Fatal("released after retained Pod identity changed")
	}
	live, err := kube.AppsV1().Deployments("ns").Get(ctx, d.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if live.Labels[RuntimeScaleInOperationLabel] != "op" {
		t.Fatal("cleared guard after retained identity changed")
	}
}

func TestScaleInCleanupRejectsForeignResizeMarker(t *testing.T) {
	ctx := context.Background()
	svc, kube, d, pod := maintenanceSourceFixture()
	zero := int32(0)
	d.Spec.Replicas = &zero
	d.ResourceVersion = "1"
	d.Labels[RuntimeScaleInOperationLabel] = "op"
	d.Annotations = map[string]string{runtimeScaleInResizeOperationAnnotation: "foreign"}
	if _, err := kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := kube.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), "ns", pod.Name); err != nil {
		t.Fatal(err)
	}
	proof := RuntimePodScaleInProtection{OperationID: "op", Namespace: "ns", DeploymentName: d.Name, DeploymentUID: string(d.UID), PreviousReplicas: 1, Replicas: 0, SourcePods: []RuntimePodScaleInIdentity{{PodName: pod.Name, PodUID: string(pod.UID)}}}
	if err := svc.ReleaseRuntimeScaleIn(ctx, proof, false); err == nil {
		t.Fatal("cleared a foreign resize operation during cleanup")
	}
	live, err := kube.AppsV1().Deployments("ns").Get(ctx, d.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if live.Labels[RuntimeScaleInOperationLabel] != "op" || live.Annotations[runtimeScaleInResizeOperationAnnotation] != "foreign" {
		t.Fatal("foreign resize evidence changed")
	}
}
