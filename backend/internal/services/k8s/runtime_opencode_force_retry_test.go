package k8s

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
)

type forceTargetTestAPI interface {
	ScaleOpenCodeForceTargetZero(context.Context, *OpenCodeUpgradeSource, *OpenCodeUpgradeSource, string, string) error
	OpenCodeForceOldPodsGone(context.Context, *OpenCodeUpgradeSource, string) (bool, error)
	ReplaceOpenCodeForceTarget(context.Context, *OpenCodeUpgradeSource, *OpenCodeUpgradeSource, string, string, string) error
}

func TestOpenCodeForceTargetExactZeroImageOneAndOrphanPodBarrier(t *testing.T) {
	ctx := context.Background()
	svc, client, source, old := openCodeSchedulingFixture(t)
	backend, ok := any(svc).(forceTargetTestAPI)
	if !ok {
		t.Fatal("CM native force target transition unavailable")
	}
	request := "81e247de-ef46-41ad-853e-0bb20524303e"
	image := "registry.local/opencode@sha256:" + strings.Repeat("c", 64)
	if err := backend.ScaleOpenCodeForceTargetZero(ctx, source, old, "41", request); err != nil {
		t.Fatal(err)
	}
	if err := backend.ScaleOpenCodeForceTargetZero(ctx, source, old, "41", request); err != nil {
		t.Fatal(err)
	}
	pod, _ := client.CoreV1().Pods("tenant").Get(ctx, "target-pod", metav1.GetOptions{})
	pod.OwnerReferences = nil
	pod.Labels = nil
	if _, err := client.CoreV1().Pods("tenant").Update(ctx, pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if gone, err := backend.OpenCodeForceOldPodsGone(ctx, old, request); err != nil || gone {
		t.Fatalf("orphaned frozen Pod forgotten: %v %v", gone, err)
	}
	client.ClearActions()
	if err := backend.ReplaceOpenCodeForceTarget(ctx, source, old, image, "41", request); err == nil {
		t.Fatal("new image applied while old Pod survives")
	}
	for _, a := range client.Actions() {
		if a.GetVerb() == "update" || a.GetVerb() == "patch" {
			t.Fatal("mutation before physical Pod exit")
		}
	}
	if err := client.CoreV1().Pods("tenant").Delete(ctx, "target-pod", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	rs, _ := client.AppsV1().ReplicaSets("tenant").Get(ctx, "target-rs", metav1.GetOptions{})
	zero := int32(0)
	rs.Spec.Replicas = &zero
	if _, err := client.AppsV1().ReplicaSets("tenant").Update(ctx, rs, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if gone, err := backend.OpenCodeForceOldPodsGone(ctx, old, request); err != nil || !gone {
		t.Fatalf("zero did not converge: %v %v", gone, err)
	}
	if err := backend.ReplaceOpenCodeForceTarget(ctx, source, old, image, "41", request); err != nil {
		t.Fatal(err)
	}
	if err := backend.ReplaceOpenCodeForceTarget(ctx, source, old, image, "41", request); err != nil {
		t.Fatal(err)
	}
	if ready, err := svc.ObserveOpenCodeForceTarget(ctx, source, old, image, "41", request); err != nil || !ready {
		t.Fatalf("new1 replay observed as old0: %v %v", ready, err)
	}
	actual, _ := client.AppsV1().Deployments("tenant").Get(ctx, old.Target.Name, metav1.GetOptions{})
	expected, err := expectedOpenCodeUpgradePool(source, old.Target.Name, image, "41")
	if err != nil {
		t.Fatal(err)
	}
	if string(actual.UID) != old.Target.UID || !apiequality.Semantic.DeepEqual(actual.Spec, expected.Spec) || len(actual.Spec.Template.Spec.Containers) != 9 {
		t.Fatal("replacement changed non-image scope")
	}
	for _, a := range client.Actions() {
		if a.GetVerb() == "update" {
			if obj, ok := a.(ktesting.UpdateAction).GetObject().(*appsv1.Deployment); ok && obj.Name != old.Target.Name {
				t.Fatal("force changed source controller")
			}
		}
	}
}
func TestOpenCodeForceTargetRefusesDriftAndResumesLostScaleReply(t *testing.T) {
	for _, drift := range []string{"uid", "worker-env", "storage", "scheduling", "source-scheduling", "source-owner", "lost-reply"} {
		t.Run(drift, func(t *testing.T) {
			ctx := context.Background()
			svc, client, source, old := openCodeSchedulingFixture(t)
			backend, ok := any(svc).(forceTargetTestAPI)
			if !ok {
				t.Fatal("CM native force target transition unavailable")
			}
			request := "81e247de-ef46-41ad-853e-0bb20524303e"
			d, _ := client.AppsV1().Deployments("tenant").Get(ctx, old.Target.Name, metav1.GetOptions{})
			switch drift {
			case "uid":
				d.UID = "replacement"
			case "worker-env":
				d.Spec.Template.Spec.Containers[1].Env = append(d.Spec.Template.Spec.Containers[1].Env, corev1.EnvVar{Name: "FOREIGN", Value: "drift"})
			case "storage":
				d.Spec.Template.Spec.Containers[1].VolumeMounts[0].SubPath = "foreign"
			case "scheduling":
				d.Labels[runtimeSchedulingLabel] = "true"
			case "source-scheduling", "source-owner":
				src, _ := client.AppsV1().Deployments("tenant").Get(ctx, source.Target.Name, metav1.GetOptions{})
				if drift == "source-scheduling" {
					src.Labels[runtimeSchedulingLabel] = "false"
				} else {
					src.OwnerReferences = []metav1.OwnerReference{{Kind: "foreign", Name: "foreign", UID: "foreign"}}
				}
				if _, err := client.AppsV1().Deployments("tenant").Update(ctx, src, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			case "lost-reply":
				once := false
				client.PrependReactor("update", "deployments", func(a ktesting.Action) (bool, runtime.Object, error) {
					if once {
						return false, nil, nil
					}
					once = true
					obj := a.(ktesting.UpdateAction).GetObject().(*appsv1.Deployment)
					if err := client.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), obj, "tenant"); err != nil {
						return true, nil, err
					}
					return true, nil, context.DeadlineExceeded
				})
			}
			if drift != "lost-reply" {
				if _, err := client.AppsV1().Deployments("tenant").Update(ctx, d, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			client.ClearActions()
			err := backend.ScaleOpenCodeForceTargetZero(ctx, source, old, "41", request)
			if err == nil {
				t.Fatal("drift/unknown scale outcome accepted without observation")
			}
			if drift == "lost-reply" {
				client.ClearActions()
				if err := backend.ScaleOpenCodeForceTargetZero(ctx, source, old, "41", request); err != nil {
					t.Fatal(err)
				}
			}
			for _, a := range client.Actions() {
				if a.GetVerb() == "update" || a.GetVerb() == "patch" {
					t.Fatal("drift/idempotent observation wrote controller")
				}
			}
		})
	}
}

func TestOpenCodeForceTargetLostReplaceReplyObservesNewOneWithoutWriting(t *testing.T) {
	ctx := context.Background()
	svc, client, source, old := openCodeSchedulingFixture(t)
	request := "81e247de-ef46-41ad-853e-0bb20524303e"
	image := "registry.local/opencode@sha256:" + strings.Repeat("c", 64)
	if err := svc.ScaleOpenCodeForceTargetZero(ctx, source, old, "41", request); err != nil {
		t.Fatal(err)
	}
	if err := client.CoreV1().Pods("tenant").Delete(ctx, "target-pod", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	rs, _ := client.AppsV1().ReplicaSets("tenant").Get(ctx, "target-rs", metav1.GetOptions{})
	zero := int32(0)
	rs.Spec.Replicas = &zero
	if _, err := client.AppsV1().ReplicaSets("tenant").Update(ctx, rs, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	once := false
	client.PrependReactor("update", "deployments", func(a ktesting.Action) (bool, runtime.Object, error) {
		if once {
			return false, nil, nil
		}
		once = true
		obj := a.(ktesting.UpdateAction).GetObject().(*appsv1.Deployment)
		if err := client.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), obj, "tenant"); err != nil {
			return true, nil, err
		}
		return true, nil, context.DeadlineExceeded
	})
	if err := svc.ReplaceOpenCodeForceTarget(ctx, source, old, image, "41", request); err != context.DeadlineExceeded {
		t.Fatal("unknown write not reported", err)
	}
	client.ClearActions()
	if ready, err := svc.ObserveOpenCodeForceTarget(ctx, source, old, image, "41", request); err != nil || !ready {
		t.Fatalf("new1 lost reply not observable: %v %v", ready, err)
	}
	if err := svc.ReplaceOpenCodeForceTarget(ctx, source, old, image, "41", request); err != nil {
		t.Fatal(err)
	}
	for _, a := range client.Actions() {
		if a.GetVerb() == "update" || a.GetVerb() == "patch" {
			t.Fatal("lost reply recovery rewrote exact new1")
		}
	}
}
