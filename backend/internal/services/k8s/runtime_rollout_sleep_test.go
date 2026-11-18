package k8s

import (
	"context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestRuntimeRolloutSleepProofAndLostWakeResponse(t *testing.T) {
	for _, changed := range []string{"none", "image", "template", "uid", "pvc", "generation", "manual-wake", "foreign-wake"} {
		t.Run(changed, func(t *testing.T) {
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
			snapshot.IdleSleepOperation = "sleep-proof"
			zero := int32(0)
			d.Spec.Replicas = &zero
			d.Generation = snapshot.DeploymentGeneration + 1
			d.Annotations = map[string]string{runtimeIdleSleepAnnotation: "sleep-proof"}
			switch changed {
			case "image":
				d.Spec.Template.Spec.Containers[0].Image = "foreign"
				d.Generation += 3
			case "template":
				d.Spec.Template.Spec.Containers[0].Command = []string{"/foreign-command"}
			case "uid":
				d.UID = "replacement"
			case "generation":
				d.Generation++
			case "pvc":
				claim, _ := kube.CoreV1().PersistentVolumeClaims("ns").Get(ctx, "workspace", metav1.GetOptions{})
				claim.UID = "replacement"
				_, _ = kube.CoreV1().PersistentVolumeClaims("ns").Update(ctx, claim, metav1.UpdateOptions{})
			case "manual-wake", "foreign-wake":
				d.Spec.Replicas = &one
				d.Generation++
				delete(d.Annotations, runtimeIdleSleepAnnotation)
				if changed == "foreign-wake" {
					d.Annotations[runtimeIdleWakeAnnotation] = "foreign"
				}
			}
			_, _ = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{})
			_ = kube.CoreV1().Pods("ns").Delete(ctx, p.Name, metav1.DeleteOptions{})
			service, ok := any(svc).(interface {
				ValidateRuntimeRolloutSleep(context.Context, RuntimePodScaleTarget, *RuntimePodScaleSnapshot) (bool, error)
			})
			if !ok {
				t.Fatal("sleep-aware rollout proof is unavailable")
			}
			active, err := service.ValidateRuntimeRolloutSleep(ctx, target, snapshot)
			if changed != "none" && changed != "image" && changed != "generation" {
				if err == nil {
					t.Fatal("accepted changed sleep identity")
				}
				return
			}
			if err != nil || active {
				t.Fatal(active, err)
			}
			before := len(kube.Actions())
			_, err = service.ValidateRuntimeRolloutSleep(ctx, target, snapshot)
			if err != nil {
				t.Fatal(err)
			}
			for _, a := range kube.Actions()[before:] {
				if a.GetVerb() != "get" && a.GetVerb() != "list" {
					t.Fatal("preparation inspection mutated Kubernetes")
				}
			}
			if err = svc.WakeIdleRuntimePool(ctx, target, snapshot); err != nil {
				t.Fatal(err)
			}
			// Fake API has no generation controller: simulate native accepted replica write.
			d, _ = kube.AppsV1().Deployments("ns").Get(ctx, d.Name, metav1.GetOptions{})
			d.Generation++
			_, _ = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{})
			active, err = service.ValidateRuntimeRolloutSleep(ctx, target, snapshot)
			if err != nil || !active {
				t.Fatal("could not observe the exact accepted wake after response loss", active, err)
			}
		})
	}
}

func TestRuntimeRolloutPreparationDoesNotSilentlySkipManualZero(t *testing.T) {
	ctx := context.Background()
	svc, kube, d, _ := maintenanceSourceFixture()
	zero := int32(0)
	d.Spec.Replicas = &zero
	d.Annotations = nil
	_, _ = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{})
	pools, err := svc.SleepingRuntimePools(ctx, "ns", "openclaw")
	if err != nil {
		t.Fatal(err)
	}
	if len(pools) != 1 || pools[0].DeploymentName != d.Name {
		t.Fatal("enabled zero-replica pool silently omitted from proof assessment")
	}
}

func TestRuntimeCaptureHistoricalExpandedPoolKeepsFrozenPreparationStrict(t *testing.T) {
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
	snapshot.IdleSleepOperation = "sleep-proof"
	d.Annotations = map[string]string{runtimeIdleWakeAnnotation: "sleep-proof"}
	d.Generation = snapshot.DeploymentGeneration + 2
	_, _ = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{})
	frozen, err := svc.CaptureRuntimeRolloutSleep(ctx, target, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	two := int32(2)
	d.Spec.Replicas = &two
	d.Generation++
	_, _ = kube.AppsV1().Deployments("ns").Update(ctx, d, metav1.UpdateOptions{})
	if _, err = svc.ValidateRuntimeRolloutSleep(ctx, target, frozen); err == nil {
		t.Fatal("frozen preparation accepted replica drift")
	}
	current, err := svc.CaptureRuntimeRolloutSleep(ctx, target, snapshot)
	if err != nil || current.Option.Replicas != 2 {
		t.Fatalf("historical ordinary capture rejected legitimate expansion: %+v %v", current, err)
	}
}
