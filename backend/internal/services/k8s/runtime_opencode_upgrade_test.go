package k8s

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestOpenCodeUpgradeSourceSecretInspectionIsReadOnly(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	service := NewRuntimeAssignmentService(client, "tenant")
	assignment := assignmentFixture()
	assignment.RuntimeType = "opencode"
	assignment.PodUID = "source-pod-uid"
	if err := service.EnsureDesired(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	client.ClearActions()
	actual, state, err := service.OpenCodeUpgradeSource(ctx, assignment.InstanceID)
	if err != nil || state != "running" || actual.Environment["API_KEY"] != "business-secret" {
		t.Fatal(state, err)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" {
			t.Fatalf("source freeze wrote %s %s", action.GetVerb(), action.GetResource().Resource)
		}
	}
	secret, err := client.CoreV1().Secrets("tenant").Get(ctx, runtimeAssignmentSecretName(7, 3), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	secret.Labels["clawmanager.io/generation"] = "4"
	if _, err = client.CoreV1().Secrets("tenant").Update(ctx, secret, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = service.OpenCodeUpgradeSource(ctx, 7); err == nil {
		t.Fatal("source freeze accepted a different generation Secret")
	}
}
func TestOpenCodeSourceRetirementRequiresFrozenStorageTemplateUIDAndPodExit(t *testing.T) {
	ctx := context.Background()
	image := "registry.local/opencode@sha256:" + strings.Repeat("a", 64)
	d := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "opencode-runtime", Namespace: "tenant", RuntimeType: "opencode", SandboxProfile: RuntimeNFSRootlessProfile, Image: image, Replicas: 1, SlotCount: 8, AgentTokenSecretName: "agent", WorkspacePVCClaimName: "workspaces", WorkspaceMountPath: "/workspaces", PodUserNamespaces: true, NetworkIsolation: true})
	d.UID = "source-deployment-uid"
	d.Generation = 1
	d.ResourceVersion = "1"
	d.Status = appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 1}
	d.Labels[runtimeSchedulingLabel] = "true"
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "workspaces", Namespace: "tenant", UID: "claim-uid"}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "pv"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
	pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv", UID: "pv-uid"}, Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Namespace: "tenant", Name: "workspaces", UID: "claim-uid"}, PersistentVolumeSource: corev1.PersistentVolumeSource{NFS: &corev1.NFSVolumeSource{Server: "10.0.0.9", Path: "/workspaces"}}}}
	controller := true
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "source-rs", Namespace: "tenant", UID: "rs-uid", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: d.Name, UID: d.UID, Controller: &controller}}}}
	pod := &corev1.Pod{Spec: *d.Spec.Template.Spec.DeepCopy(), ObjectMeta: metav1.ObjectMeta{Name: "source-pod", Namespace: "tenant", UID: "source-pod-uid", Labels: d.Spec.Selector.MatchLabels, OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: &controller}}}}
	client := fake.NewSimpleClientset(d, claim, pv, rs, pod)
	service := &runtimeDeploymentService{client: client}
	target := RuntimeRolloutTarget{Namespace: "tenant", Name: d.Name, UID: string(d.UID), Image: image}
	proof, err := service.CaptureOpenCodeUpgradeSource(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	targetImage := "registry.local/opencode@sha256:" + strings.Repeat("b", 64)
	expected, err := expectedOpenCodeUpgradePool(proof, "opencode-runtime-u41", targetImage, "41")
	if err != nil {
		t.Fatal(err)
	}
	expected.UID = "target-controller-uid"
	expected.ResourceVersion = "2"
	if _, err = client.AppsV1().Deployments("tenant").Create(ctx, expected, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	targetProof := RuntimeRolloutTarget{Namespace: "tenant", Name: expected.Name, UID: string(expected.UID), Image: targetImage}
	if err = service.ValidateOpenCodeUpgradeTarget(ctx, proof, targetProof, "41"); err != nil {
		t.Fatal(err)
	}
	for _, drift := range []string{"worker-env", "worker-mount", "scheduling"} {
		changed := expected.DeepCopy()
		switch drift {
		case "worker-env":
			changed.Spec.Template.Spec.Containers[1].Env = append(changed.Spec.Template.Spec.Containers[1].Env, corev1.EnvVar{Name: "UNREVIEWED", Value: "ordinary-business-mode"})
		case "worker-mount":
			for i := range changed.Spec.Template.Spec.Containers[1].VolumeMounts {
				if changed.Spec.Template.Spec.Containers[1].VolumeMounts[i].Name == runtimeWorkspaceVolume {
					changed.Spec.Template.Spec.Containers[1].VolumeMounts[i].SubPath = "other-owner"
				}
			}
		case "scheduling":
			changed.Labels[runtimeSchedulingLabel] = "true"
		}
		if _, err = client.AppsV1().Deployments("tenant").Update(ctx, changed, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		if err = service.ValidateOpenCodeUpgradeTarget(ctx, proof, targetProof, "41"); err == nil {
			t.Fatalf("target %s drift accepted", drift)
		}
	}
	if _, err = client.AppsV1().Deployments("tenant").Update(ctx, expected, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err = service.RetireOpenCodeUpgradeSource(ctx, proof, "41"); err == nil {
		t.Fatal("retired a schedulable source")
	}
	d.Labels[runtimeSchedulingLabel] = "false"
	d.Spec.Template.Spec.Containers[0].Env = append(d.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "UNREVIEWED", Value: "drift"})
	if _, err = client.AppsV1().Deployments("tenant").Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err = service.RetireOpenCodeUpgradeSource(ctx, proof, "41"); err == nil {
		t.Fatal("retired changed template")
	}
	d.Spec.Template = *proof.Template.DeepCopy()
	if _, err = client.AppsV1().Deployments("tenant").Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err = service.RetireOpenCodeUpgradeSource(ctx, proof, "41"); err != nil {
		t.Fatal(err)
	}
	if retired, err := service.OpenCodeUpgradeSourceRetired(ctx, proof, "41"); err != nil || retired {
		t.Fatal("missing physical source Pod exit proof", retired, err)
	}
	if err = client.CoreV1().Pods("tenant").Delete(ctx, pod.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	d, err = client.AppsV1().Deployments("tenant").Get(ctx, d.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	d.Status.Replicas = 0
	if _, err = client.AppsV1().Deployments("tenant").Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if retired, err := service.OpenCodeUpgradeSourceRetired(ctx, proof, "41"); err != nil || !retired {
		t.Fatal(retired, err)
	}
	if _, err = service.OpenCodeUpgradeSourceRetired(ctx, proof, "other-operation"); err == nil {
		t.Fatal("adopted another operation retirement")
	}
	claim.UID = "replacement-claim"
	if _, err = client.CoreV1().PersistentVolumeClaims("tenant").Update(ctx, claim, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err = service.OpenCodeUpgradeSourceRetired(ctx, proof, "41"); err == nil {
		t.Fatal("accepted replacement workspace claim")
	}
}
