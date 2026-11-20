package k8s

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func emptyPoolFixture(t *testing.T, rt string, slots int) (*runtimeDeploymentService, *fake.Clientset, *appsv1.Deployment, RuntimePodScaleTarget) {
	t.Helper()
	d := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "source", Namespace: "ns", RuntimeType: rt, Image: "repo/runtime@sha256:" + strings.Repeat("a", 64), SlotCount: slots, AgentTokenSecretName: "agent", WorkspacePVCClaimName: "workspace"})
	d.UID = types.UID("source-uid")
	d.ResourceVersion = "7"
	d.Generation = 4
	zero := int32(0)
	d.Spec.Replicas = &zero
	kube := fake.NewSimpleClientset(d, maintenanceWorkspaceClaim())
	return &runtimeDeploymentService{client: kube}, kube, d, RuntimePodScaleTarget{RuntimeType: rt, Namespace: "ns", DeploymentName: d.Name, DeploymentUID: string(d.UID)}
}

func TestEmptyRuntimeRecoveryPreservesTwoAndEightSlots(t *testing.T) {
	for _, rt := range []string{"openclaw", "opencode", "hermes", "deepseek-harness"} {
		for _, slots := range []int{2, 8} {
			t.Run(fmt.Sprintf("%s/%d", rt, slots), func(t *testing.T) {
				s, kube, _, target := emptyPoolFixture(t, rt, slots)
				proof, err := s.InspectEmptyRuntimePool(t.Context(), target)
				if err != nil {
					t.Fatal(err)
				}
				next := "repo/runtime@sha256:" + strings.Repeat("b", 64)
				checks := 0
				guard := func(context.Context) error { checks++; return nil }
				if err = s.RecoverEmptyRuntimePool(t.Context(), proof, rt, next, "42", guard); err != nil {
					t.Fatal(err)
				}
				current, _ := kube.AppsV1().Deployments("ns").Get(t.Context(), "source", metav1.GetOptions{})
				actual, err := s.emptyRuntimeConfiguration(t.Context(), current, rt)
				if err != nil || actual.Configuration != proof.Configuration || actual.SlotCount != slots || len(current.Spec.Template.Spec.Containers) != slots+1 || *current.Spec.Replicas != 1 {
					t.Fatalf("native template changed: %+v %v", actual, err)
				}
				for _, c := range current.Spec.Template.Spec.Containers {
					if c.Image != next {
						t.Fatal("a worker kept the old image")
					}
				}
				if _, err = kube.CoreV1().PersistentVolumeClaims("ns").Get(t.Context(), "workspace", metav1.GetOptions{}); err != nil {
					t.Fatal("workspace was removed", err)
				}
				if err = s.RecoverEmptyRuntimePool(t.Context(), proof, rt, next, "42", guard); err != nil || checks != 1 {
					t.Fatalf("resume replayed the write: %v checks=%d", err, checks)
				}
			})
		}
	}
}

func TestEmptyRuntimeRecoveryRejectsLiveOrChangedSources(t *testing.T) {
	for _, mode := range []string{"pod", "replicas", "uid", "slot-layout", "storage", "repository", "guard"} {
		t.Run(mode, func(t *testing.T) {
			s, kube, d, target := emptyPoolFixture(t, "openclaw", 2)
			proof, err := s.InspectEmptyRuntimePool(t.Context(), target)
			if err != nil {
				t.Fatal(err)
			}
			image := "repo/runtime@sha256:" + strings.Repeat("b", 64)
			guard := func(context.Context) error { return nil }
			switch mode {
			case "pod":
				_, err = kube.CoreV1().Pods("ns").Create(t.Context(), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "retiring", Namespace: "ns", Labels: map[string]string{"clawmanager.io/runtime-type": "openclaw"}}}, metav1.CreateOptions{})
			case "replicas":
				one := int32(1)
				d.Spec.Replicas = &one
			case "uid":
				d.UID = "replacement"
			case "slot-layout":
				d.Spec.Template.Spec.Containers = d.Spec.Template.Spec.Containers[:2]
			case "storage":
				claim, _ := kube.CoreV1().PersistentVolumeClaims("ns").Get(t.Context(), "workspace", metav1.GetOptions{})
				claim.UID = "new-storage"
				_, err = kube.CoreV1().PersistentVolumeClaims("ns").Update(t.Context(), claim, metav1.UpdateOptions{})
			case "repository":
				image = "foreign/runtime@sha256:" + strings.Repeat("b", 64)
			case "guard":
				guard = func(context.Context) error { return errors.New("binding appeared") }
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = kube.AppsV1().Deployments("ns").Update(t.Context(), d, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			kube.ClearActions()
			if err = s.RecoverEmptyRuntimePool(t.Context(), proof, "openclaw", image, "42", guard); err == nil {
				t.Fatal("unsafe recovery was admitted")
			}
			for _, a := range kube.Actions() {
				if a.GetVerb() == "update" || a.GetVerb() == "patch" || a.GetVerb() == "delete" {
					t.Fatal("rejected recovery changed Kubernetes")
				}
			}
		})
	}
}

func TestEmptyRuntimeRecoveryObservesLostUpdateResponse(t *testing.T) {
	s, kube, _, target := emptyPoolFixture(t, "openclaw", 2)
	proof, err := s.InspectEmptyRuntimePool(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	writes := 0
	kube.PrependReactor("update", "deployments", func(a ktesting.Action) (bool, runtime.Object, error) {
		writes++
		d := a.(ktesting.UpdateAction).GetObject().(*appsv1.Deployment)
		if err := kube.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), d, "ns"); err != nil {
			t.Fatal(err)
		}
		return true, nil, context.DeadlineExceeded
	})
	image := "repo/runtime@sha256:" + strings.Repeat("b", 64)
	guard := func(context.Context) error { return nil }
	if err = s.RecoverEmptyRuntimePool(t.Context(), proof, "openclaw", image, "42", guard); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("lost response was hidden", err)
	}
	if err = s.RecoverEmptyRuntimePool(t.Context(), proof, "openclaw", image, "42", guard); err != nil || writes != 1 {
		t.Fatalf("unknown write was replayed: %v count=%d", err, writes)
	}
}
