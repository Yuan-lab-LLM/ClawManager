package k8s

import (
	"context"
	"errors"
	"fmt"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ktesting "k8s.io/client-go/testing"
	"strings"
	"testing"
)

func TestScaleInRequiresActualAdmissionDenialBeforeResize(t *testing.T) {
	for _, mode := range []string{"missing-policy", "rbac", "policy"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			svc, kube, d, p := maintenanceSourceFixture()
			d.ResourceVersion = "1"
			n := int32(2)
			d.Spec.Replicas = &n
			d.Status.Replicas = 2
			d.Status.ReadyReplicas = 2
			d.Status.UpdatedReplicas = 2
			_, _ = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{})
			p.ResourceVersion = "1"
			_, _ = kube.CoreV1().Pods("ns").Update(ctx, p, metav1.UpdateOptions{})
			retained := p.DeepCopy()
			retained.Name = "retained"
			retained.UID = "retained-uid"
			_, _ = kube.CoreV1().Pods("ns").Create(ctx, retained, metav1.CreateOptions{})
			target := RuntimePodScaleTarget{RuntimeType: "openclaw", Namespace: "ns", DeploymentName: d.Name, PodName: p.Name, PodUID: string(p.UID)}
			snapshot, err := svc.InspectRuntimeScale(ctx, target)
			if err != nil {
				t.Fatal(err)
			}
			proof := RuntimePodScaleInProtection{OperationID: "op", Namespace: "ns", DeploymentName: d.Name, DeploymentUID: string(d.UID), PreviousReplicas: 2, Replicas: 1, SourcePods: []RuntimePodScaleInIdentity{{PodName: p.Name, PodUID: string(p.UID)}}, RetainedPods: []RuntimePodScaleInIdentity{{PodName: retained.Name, PodUID: string(retained.UID)}}}
			kube.PrependReactor("delete", "pods", func(a ktesting.Action) (bool, runtime.Object, error) {
				action := a.(ktesting.DeleteAction)
				if len(action.GetDeleteOptions().DryRun) != 1 {
					t.Fatal("real delete attempted")
				}
				if mode == "missing-policy" {
					return true, nil, nil
				}
				message := "pods is forbidden by RBAC"
				if mode == "policy" {
					message = RuntimeScaleInPolicyDenial
				}
				return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, action.GetName(), &scaleInTestError{message})
			})
			err = svc.ProtectRuntimeScaleIn(ctx, proof)
			if mode != "policy" {
				if err == nil {
					t.Fatal("accepted unproven protection")
				}
				for _, a := range kube.Actions() {
					if a.GetVerb() == "patch" && a.GetResource().Resource == "deployments" && strings.Contains(string(a.(ktesting.PatchAction).GetPatch()), "/spec/replicas") {
						t.Fatal("resized without policy")
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = svc.ResizeRuntimeScaleIn(ctx, target, snapshot, proof); err != nil {
				t.Fatal(err)
			}
			current, _ := kube.AppsV1().Deployments("ns").Get(ctx, d.Name, metav1.GetOptions{})
			if *current.Spec.Replicas != 1 {
				t.Fatal("replicas not reduced")
			}
			protected, _ := kube.CoreV1().Pods("ns").Get(ctx, retained.Name, metav1.GetOptions{})
			if protected.Labels[RuntimeScaleInProtectedLabel] != "op" {
				t.Fatal("retained Pod unprotected")
			}
		})
	}
}

type scaleInTestError struct{ s string }

func (e *scaleInTestError) Error() string { return e.s }

func TestScaleInProtectionRejectsReplacementAndUnexpectedSibling(t *testing.T) {
	ctx := context.Background()
	svc, kube, d, p := maintenanceSourceFixture()
	d.ResourceVersion = "1"
	n := int32(2)
	d.Spec.Replicas = &n
	_, _ = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{})
	proof := RuntimePodScaleInProtection{OperationID: "op", Namespace: "ns", DeploymentName: d.Name, DeploymentUID: string(d.UID), PreviousReplicas: 2, Replicas: 1, SourcePods: []RuntimePodScaleInIdentity{{PodName: p.Name, PodUID: "replaced"}}, RetainedPods: []RuntimePodScaleInIdentity{{PodName: "other", PodUID: "other"}}}
	if err := svc.ProtectRuntimeScaleIn(ctx, proof); err == nil {
		t.Fatal("accepted changed native set")
	}
	for _, a := range kube.Actions() {
		if a.GetVerb() == "patch" {
			t.Fatal("mutated before verifying membership")
		}
	}
}

func TestScaleInResizeUsesControllerCASAndNeverRetriesUnknownWrite(t *testing.T) {
	ctx := context.Background()
	svc, kube, d, p := maintenanceSourceFixture()
	d.ResourceVersion = "1"
	n := int32(2)
	d.Spec.Replicas = &n
	_, _ = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{})
	p.ResourceVersion = "1"
	_, _ = kube.CoreV1().Pods("ns").Update(ctx, p, metav1.UpdateOptions{})
	q := p.DeepCopy()
	q.Name = "retained"
	q.UID = "retained"
	_, _ = kube.CoreV1().Pods("ns").Create(ctx, q, metav1.CreateOptions{})
	target := RuntimePodScaleTarget{RuntimeType: "openclaw", Namespace: "ns", DeploymentName: d.Name, PodName: p.Name, PodUID: string(p.UID)}
	snapshot, err := svc.InspectRuntimeScale(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	proof := RuntimePodScaleInProtection{OperationID: "op", Namespace: "ns", DeploymentName: d.Name, DeploymentUID: string(d.UID), PreviousReplicas: 2, Replicas: 1, SourcePods: []RuntimePodScaleInIdentity{{PodName: p.Name, PodUID: string(p.UID)}}, RetainedPods: []RuntimePodScaleInIdentity{{PodName: q.Name, PodUID: string(q.UID)}}}
	kube.PrependReactor("delete", "pods", func(a ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, q.Name, &scaleInTestError{RuntimeScaleInPolicyDenial})
	})
	if err = svc.ProtectRuntimeScaleIn(ctx, proof); err != nil {
		t.Fatal(err)
	}
	writes := 0
	kube.PrependReactor("patch", "deployments", func(a ktesting.Action) (bool, runtime.Object, error) {
		writes++
		patch := string(a.(ktesting.PatchAction).GetPatch())
		if !strings.Contains(patch, "/metadata/resourceVersion") || !strings.Contains(patch, "/metadata/uid") {
			t.Fatal(patch)
		}
		return true, nil, &scaleInTestError{"ambiguous write"}
	})
	if svc.ResizeRuntimeScaleIn(ctx, target, snapshot, proof) == nil || writes != 1 {
		t.Fatalf("unknown write replayed: %d", writes)
	}
}

func TestScaleInRollbackBarrierPrecedesAnyUnprotect(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(fmt.Sprint(reject), func(t *testing.T) {
			ctx := context.Background()
			svc, kube, d, p := maintenanceSourceFixture()
			d.ResourceVersion = "1"
			n := int32(2)
			d.Spec.Replicas = &n
			_, _ = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{})
			p.ResourceVersion = "1"
			_, _ = kube.CoreV1().Pods("ns").Update(ctx, p, metav1.UpdateOptions{})
			q := p.DeepCopy()
			q.Name = "retained"
			q.UID = "retained"
			_, _ = kube.CoreV1().Pods("ns").Create(ctx, q, metav1.CreateOptions{})
			proof := RuntimePodScaleInProtection{OperationID: "op", Namespace: "ns", DeploymentName: d.Name, DeploymentUID: string(d.UID), PreviousReplicas: 2, Replicas: 1, SourcePods: []RuntimePodScaleInIdentity{{PodName: p.Name, PodUID: string(p.UID)}}, RetainedPods: []RuntimePodScaleInIdentity{{PodName: q.Name, PodUID: string(q.UID)}}}
			kube.PrependReactor("delete", "pods", func(a ktesting.Action) (bool, runtime.Object, error) {
				return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, q.Name, &scaleInTestError{RuntimeScaleInPolicyDenial})
			})
			if err := svc.ProtectRuntimeScaleIn(ctx, proof); err != nil {
				t.Fatal(err)
			}
			barrier := false
			unprotected := false
			kube.PrependReactor("patch", "deployments", func(a ktesting.Action) (bool, runtime.Object, error) {
				patch := string(a.(ktesting.PatchAction).GetPatch())
				if strings.Contains(patch, runtimeScaleInCleanupBarrierAnnotation) {
					if !strings.Contains(patch, "/metadata/resourceVersion") || !strings.Contains(patch, "/spec/replicas") {
						t.Fatalf("unsafe rollback barrier: %s", patch)
					}
					barrier = true
					if reject {
						return true, nil, errors.New("barrier response lost")
					}
				}
				return false, nil, nil
			})
			kube.PrependReactor("patch", "pods", func(a ktesting.Action) (bool, runtime.Object, error) {
				if !barrier {
					t.Fatal("unprotected before pending replica CAS was fenced")
				}
				unprotected = true
				return false, nil, nil
			})
			err := svc.ReleaseRuntimeScaleIn(ctx, proof, true)
			if reject {
				if err == nil || unprotected {
					t.Fatal("unprotected after unconfirmed barrier")
				}
			} else if err != nil || !unprotected {
				t.Fatalf("safe rollback failed: %v", err)
			}
		})
	}
}
