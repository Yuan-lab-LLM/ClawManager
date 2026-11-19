package k8s

import (
	"context"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"strings"
	"testing"
)

func openCodeSchedulingFixture(t *testing.T) (*runtimeDeploymentService, *fake.Clientset, *OpenCodeUpgradeSource, *OpenCodeUpgradeSource) {
	t.Helper()
	ctx := context.Background()
	image := "registry.local/opencode@sha256:" + strings.Repeat("a", 64)
	source := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "opencode-runtime", Namespace: "tenant", RuntimeType: "opencode", SandboxProfile: RuntimeNFSRootlessProfile, Image: image, Replicas: 1, SlotCount: 8, AgentTokenSecretName: "agent", WorkspacePVCClaimName: "workspaces", WorkspaceMountPath: "/workspaces", PodUserNamespaces: true, NetworkIsolation: true})
	source.UID = "source-uid"
	source.ResourceVersion = "1"
	source.Labels[runtimeSchedulingLabel] = "true"
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "workspaces", Namespace: "tenant", UID: "claim-uid"}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "pv"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
	pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv", UID: "pv-uid"}, Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Namespace: "tenant", Name: "workspaces", UID: "claim-uid"}, PersistentVolumeSource: corev1.PersistentVolumeSource{NFS: &corev1.NFSVolumeSource{Server: "10.0.0.9", Path: "/workspaces"}}}}
	controller := true
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "source-rs", Namespace: "tenant", UID: "source-rs-uid", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: source.Name, UID: source.UID, Controller: &controller}}}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "source-pod", Namespace: "tenant", UID: "source-pod-uid", OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: &controller}}}, Spec: *source.Spec.Template.Spec.DeepCopy()}
	client := fake.NewSimpleClientset(source, claim, pv, rs, pod)
	service := &runtimeDeploymentService{client: client}
	frozen, err := service.CaptureOpenCodeUpgradeSource(ctx, RuntimeRolloutTarget{Namespace: "tenant", Name: source.Name, UID: string(source.UID), Image: image})
	if err != nil {
		t.Fatal(err)
	}
	target, err := expectedOpenCodeUpgradePool(frozen, "opencode-runtime-u41", "registry.local/opencode@sha256:"+strings.Repeat("b", 64), "41")
	if err != nil {
		t.Fatal(err)
	}
	target.UID = "target-uid"
	target.ResourceVersion = "2"
	if _, err = client.AppsV1().Deployments("tenant").Create(ctx, target, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	targetRS := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "target-rs", Namespace: "tenant", UID: "target-rs-uid", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: target.Name, UID: target.UID, Controller: &controller}}}}
	if _, err = client.AppsV1().ReplicaSets("tenant").Create(ctx, targetRS, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	targetPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "target-pod", Namespace: "tenant", UID: "target-pod-uid", OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: targetRS.Name, UID: targetRS.UID, Controller: &controller}}}, Spec: *target.Spec.Template.Spec.DeepCopy()}
	if _, err = client.CoreV1().Pods("tenant").Create(ctx, targetPod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	targetProof, err := service.CaptureOpenCodeUpgradeSource(ctx, RuntimeRolloutTarget{Namespace: "tenant", Name: target.Name, UID: string(target.UID), Image: target.Spec.Template.Spec.Containers[0].Image})
	if err != nil {
		t.Fatal(err)
	}
	return service, client, frozen, targetProof
}
func TestOpenCodeSchedulingCASUsesFrozenUIDAndResumesOwnedTransitions(t *testing.T) {
	ctx := context.Background()
	service, client, source, target := openCodeSchedulingFixture(t)
	if err := service.ValidateOpenCodeUpgradeScheduling(ctx, source, target, "41", "preserve"); err != nil {
		t.Fatal(err)
	}
	actual, err := client.AppsV1().Deployments("tenant").Get(ctx, target.Target.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	actual.Labels[runtimeSchedulingLabel] = "true"
	if _, err = client.AppsV1().Deployments("tenant").Update(ctx, actual, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err = service.ValidateOpenCodeUpgradeScheduling(ctx, source, target, "41", "preserve"); err == nil {
		t.Fatal("early target scheduling accepted before quiesce")
	}
	if err = service.SetOpenCodeUpgradePoolActive(ctx, source, target, "41", true); err != nil {
		t.Fatal("owned half-switch did not resume", err)
	}
	if err = service.ValidateOpenCodeUpgradeScheduling(ctx, source, target, "41", "retire"); err != nil {
		t.Fatal(err)
	}
	if err = service.SetOpenCodeUpgradePoolActive(ctx, source, target, "41", false); err != nil {
		t.Fatal(err)
	}
	if err = service.RetireOpenCodeUpgradeSource(ctx, target, "rollback-41"); err != nil {
		t.Fatal(err)
	}
	if err = service.SetOpenCodeUpgradePoolActive(ctx, source, target, "41", false); err != nil {
		t.Fatal("owned zero-replica rollback did not replay", err)
	}
	for _, which := range []string{source.Target.Name, target.Target.Name} {
		actual, err = client.AppsV1().Deployments("tenant").Get(ctx, which, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		original := actual.DeepCopy()
		actual.UID = "replacement-uid"
		if _, err = client.AppsV1().Deployments("tenant").Update(ctx, actual, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		client.ClearActions()
		if err = service.SetOpenCodeUpgradePoolActive(ctx, source, target, "41", false); err == nil {
			t.Fatal("same-name replacement accepted")
		}
		for _, action := range client.Actions() {
			if action.GetVerb() == "update" || action.GetVerb() == "patch" {
				t.Fatal("wrote before rejecting peer UID")
			}
		}
		if _, err = client.AppsV1().Deployments("tenant").Update(ctx, original, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
}
func TestOpenCodeSchedulingRevalidatesTargetLineageBeforeEveryWrite(t *testing.T) {
	for _, change := range []string{"uid", "lineage", "selector", "storage"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			service, client, source, target := openCodeSchedulingFixture(t)
			actual, err := client.AppsV1().Deployments("tenant").Get(ctx, target.Target.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "uid":
				actual.UID = "replacement"
			case "lineage":
				actual.Annotations["clawmanager.io/opencode-source-uid"] = "other-source"
			case "selector":
				actual.Spec.Selector.MatchLabels["app"] = "other-pool"
			case "storage":
				actual.Spec.Template.Spec.Containers[1].VolumeMounts[0].SubPath = "other-owner"
			}
			if _, err = client.AppsV1().Deployments("tenant").Update(ctx, actual, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			client.ClearActions()
			if err = service.SetOpenCodeUpgradePoolActive(ctx, source, target, "41", true); err == nil {
				t.Fatal("target identity drift accepted")
			}
			for _, action := range client.Actions() {
				if action.GetVerb() == "update" || action.GetVerb() == "patch" {
					t.Fatal("wrote before rejecting target drift")
				}
			}
		})
	}
}

func TestOpenCodeSchedulingRollbackRejectsBothDisabledWithoutWriting(t *testing.T) {
	ctx := context.Background()
	service, client, source, target := openCodeSchedulingFixture(t)
	for _, name := range []string{source.Target.Name, target.Target.Name} {
		actual, err := client.AppsV1().Deployments("tenant").Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		actual.Labels[runtimeSchedulingLabel] = "false"
		if _, err = client.AppsV1().Deployments("tenant").Update(ctx, actual, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	client.ClearActions()
	if err := service.SetOpenCodeUpgradePoolActive(ctx, source, target, "41", false); err == nil {
		t.Fatal("rollback reopened an independently disabled source")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "update" || action.GetVerb() == "patch" {
			t.Fatal("rollback wrote after impossible double-disabled scheduling state")
		}
	}
	for _, name := range []string{source.Target.Name, target.Target.Name} {
		actual, err := client.AppsV1().Deployments("tenant").Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if actual.Labels[runtimeSchedulingLabel] != "false" {
			t.Fatal("rollback changed original disabled scheduling")
		}
	}
}
