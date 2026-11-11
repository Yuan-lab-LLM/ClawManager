package k8s

import (
	"context"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"strings"
	"testing"
	"time"
)

func TestColdSuccessorPreservesRetirementFenceAndRetriesSameIdentity(t *testing.T) {
	ctx := context.Background()
	image := "repo@sha256:" + strings.Repeat("a", 64)
	zero := int32(0)
	source := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "source", Namespace: "tenant", RuntimeType: "openclaw", Image: image, Replicas: 0, SlotCount: 8, AgentTokenSecretName: "agent", WorkspacePVCClaimName: "workspaces", WorkspaceMountPath: "/workspaces", PodUserNamespaces: true, NetworkIsolation: true})
	source.UID = "source-uid"
	source.ResourceVersion = "10"
	source.Generation = 1
	source.Status.ObservedGeneration = 1
	source.Spec.Replicas = &zero
	source.Labels[runtimeSchedulingLabel] = "false"
	source.Labels[runtimePoolRoleLabel] = "upgrade-target"
	source.Annotations = map[string]string{orphanOperation: "retirement"}
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "workspaces", Namespace: "tenant", UID: "claim-uid"}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "pv"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
	pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv", UID: "pv-uid"}, Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Namespace: "tenant", Name: "workspaces", UID: "claim-uid"}, PersistentVolumeSource: corev1.PersistentVolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/workspace"}}}}
	client := fake.NewSimpleClientset(source, claim, pv)
	service := &runtimeDeploymentService{client: client}
	cold, ok := any(service).(interface {
		InspectRetiredRuntimePool(context.Context, RuntimeRolloutTarget, string) (string, error)
		EnsureColdRuntimeSuccessor(context.Context, RuntimeRolloutTarget, string, string, string, string) (*RuntimeRolloutTarget, error)
	})
	if !ok {
		t.Fatal("fully retired default has no safe successor creation path")
	}
	original := RuntimeRolloutTarget{Namespace: "tenant", Name: "source", UID: "source-uid", Image: image}
	hash, err := cold.InspectRetiredRuntimePool(ctx, original, "retirement")
	if err != nil {
		t.Fatal(err)
	}
	target, err := cold.EnsureColdRuntimeSuccessor(ctx, original, hash, "retirement", "source-u20", "20")
	if err != nil {
		t.Fatal(err)
	}
	again, err := cold.EnsureColdRuntimeSuccessor(ctx, original, hash, "retirement", "source-u20", "20")
	if err != nil || again.Name != target.Name {
		t.Fatal(again, err)
	}
	d, _ := client.AppsV1().Deployments("tenant").Get(ctx, target.Name, metav1.GetOptions{})
	if *d.Spec.Replicas != 1 || d.Labels[runtimeSchedulingLabel] != "false" || len(d.Spec.Template.Spec.Containers) != 9 {
		t.Fatal("successor lost native capacity or started admitted")
	}
	originalD, _ := client.AppsV1().Deployments("tenant").Get(ctx, "source", metav1.GetOptions{})
	if *originalD.Spec.Replicas != 0 || originalD.Labels[runtimeSchedulingLabel] != "false" || originalD.Annotations[orphanOperation] != "retirement" {
		t.Fatal("retired source was changed")
	}
	source.Spec.Template.Spec.Containers[0].Image = "changed"
	client.AppsV1().Deployments("tenant").Update(ctx, source, metav1.UpdateOptions{})
	if _, err = cold.EnsureColdRuntimeSuccessor(ctx, original, hash, "retirement", "source-u21", "21"); err == nil {
		t.Fatal("changed retired source authorized a clone")
	}
}

func TestColdSuccessorRejectsAdoptedTemplateDriftAndPinsDeletion(t *testing.T) {
	ctx := context.Background()
	image := "repo@sha256:" + strings.Repeat("a", 64)
	zero := int32(0)
	d := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "source", Namespace: "tenant", RuntimeType: "openclaw", Image: image, SlotCount: 8, AgentTokenSecretName: "agent", WorkspacePVCClaimName: "workspaces", WorkspaceMountPath: "/workspaces", PodUserNamespaces: true, NetworkIsolation: true})
	d.UID = "source-uid"
	d.ResourceVersion = "10"
	d.Spec.Replicas = &zero
	d.Labels[runtimeSchedulingLabel] = "false"
	d.Annotations = map[string]string{orphanOperation: "retirement"}
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "workspaces", Namespace: "tenant", UID: "claim"}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "pv"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
	pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv", UID: "pv-uid"}, Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Namespace: "tenant", Name: "workspaces", UID: "claim"}, PersistentVolumeSource: corev1.PersistentVolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/workspace"}}}}
	client := fake.NewSimpleClientset(d, claim, pv)
	s := &runtimeDeploymentService{client: client}
	source := RuntimeRolloutTarget{Namespace: "tenant", Name: "source", UID: "source-uid", Image: image}
	hash, e := s.InspectRetiredRuntimePool(ctx, source, "retirement")
	if e != nil {
		t.Fatal(e)
	}
	target, e := s.EnsureColdRuntimeSuccessor(ctx, source, hash, "retirement", "source-u20", "20")
	if e != nil {
		t.Fatal(e)
	}
	good, _ := client.AppsV1().Deployments("tenant").Get(ctx, target.Name, metav1.GetOptions{})
	good.UID = "target-uid"
	good.ResourceVersion = "33"
	client.AppsV1().Deployments("tenant").Update(ctx, good, metav1.UpdateOptions{})
	target.UID = "target-uid"
	changed := good.DeepCopy()
	changed.Spec.Template.Spec.Containers[1].Image = "unapproved-slot"
	client.AppsV1().Deployments("tenant").Update(ctx, changed, metav1.UpdateOptions{})
	if _, e = s.EnsureColdRuntimeSuccessor(ctx, source, hash, "retirement", target.Name, "20"); e == nil {
		t.Fatal("slot image drift was adopted")
	}
	if e = s.InspectColdRuntimeSuccessor(ctx, *target, source, hash, "20"); e == nil {
		t.Fatal("slot image drift was activated")
	}
	client.AppsV1().Deployments("tenant").Update(ctx, good, metav1.UpdateOptions{})
	gets := 0
	client.PrependReactor("get", "deployments", func(a ktesting.Action) (bool, runtime.Object, error) {
		if a.(ktesting.GetAction).GetName() != "source" {
			return false, nil, nil
		}
		gets++
		if gets != 2 {
			return false, nil, nil
		}
		changed := d.DeepCopy()
		changed.Spec.Template.Spec.Containers[0].Env = append(changed.Spec.Template.Spec.Containers[0].Env, corev1.EnvVar{Name: "UNAPPROVED", Value: "changed"})
		return true, changed, nil
	})
	if _, e = s.EnsureColdRuntimeSuccessor(ctx, source, hash, "retirement", "source-u21", "21"); e == nil {
		t.Fatal("second source read copied a changed same-UID template")
	}
	// A zero-replica target must be deleted with both immutable and version proof.
	good.Spec.Replicas = &zero
	client.AppsV1().Deployments("tenant").Update(ctx, good, metav1.UpdateOptions{})
	checked := false
	client.PrependReactor("delete", "deployments", func(a ktesting.Action) (bool, runtime.Object, error) {
		opts := a.(ktesting.DeleteAction).GetDeleteOptions()
		if opts.Preconditions == nil || opts.Preconditions.UID == nil || *opts.Preconditions.UID != "target-uid" || opts.Preconditions.ResourceVersion == nil || *opts.Preconditions.ResourceVersion != "33" {
			t.Fatal("cold delete is not tied to frozen UID and version")
		}
		checked = true
		return false, nil, nil
	})
	if gone, e := s.RetireColdRuntimeSuccessor(ctx, source, hash, target.Name, "20", target.UID); e != nil || !gone || !checked {
		t.Fatal(gone, e)
	}
}

func TestColdSourceRequiresOriginalRetirementTemplateAndStorage(t *testing.T) {
	ctx := context.Background()
	image := "repo@sha256:" + strings.Repeat("a", 64)
	zero := int32(0)
	yes := true
	d := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "source", Namespace: "tenant", RuntimeType: "openclaw", Image: image, SlotCount: 8, AgentTokenSecretName: "agent", WorkspacePVCClaimName: "workspaces", WorkspaceMountPath: "/workspaces", PodUserNamespaces: true, NetworkIsolation: true})
	d.UID = "source-uid"
	d.Spec.Replicas = &zero
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "original-rs", Namespace: "tenant", UID: "rs-uid", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", UID: d.UID, Controller: &yes}}}, Spec: appsv1.ReplicaSetSpec{Replicas: &zero, Template: *d.Spec.Template.DeepCopy()}}
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "workspaces", Namespace: "tenant", UID: "claim"}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "pv"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
	pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv", UID: "pv-uid"}, Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Namespace: "tenant", Name: "workspaces", UID: "claim"}, PersistentVolumeSource: corev1.PersistentVolumeSource{NFS: &corev1.NFSVolumeSource{Server: "10.0.0.1", Path: "/business"}}}}
	client := fake.NewSimpleClientset(d, rs, claim, pv)
	s := &runtimeDeploymentService{client: client}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "old", Namespace: "tenant", UID: "old-uid"}, Spec: *d.Spec.Template.Spec.DeepCopy()}
	digest, e := RuntimeFaultStorageIdentity(ctx, client, pod, "/workspaces", time.Time{})
	if e != nil {
		t.Fatal(e)
	}
	source := &RuntimeOrphanSource{Namespace: "tenant", PodUID: "old-uid", DeploymentName: d.Name, DeploymentUID: string(d.UID), ReplicaSetName: rs.Name, ReplicaSetUID: string(rs.UID), SourcePod: pod, WorkspaceRoot: "/workspaces", StorageDigest: digest}
	if e = s.CheckColdRuntimeExternalWriters(ctx, source); e != nil {
		t.Fatal(e)
	}
	d.Spec.Template.Spec.Containers[1].Env = append(d.Spec.Template.Spec.Containers[1].Env, corev1.EnvVar{Name: "UNAPPROVED", Value: "changed"})
	client.AppsV1().Deployments("tenant").Update(ctx, d, metav1.UpdateOptions{})
	if e = s.CheckColdRuntimeExternalWriters(ctx, source); e == nil {
		t.Fatal("same-UID edit made before cold classification became approved")
	}
	d.Spec.Template = *rs.Spec.Template.DeepCopy()
	client.AppsV1().Deployments("tenant").Update(ctx, d, metav1.UpdateOptions{})
	pv.Spec.NFS.Path = "/replaced"
	client.CoreV1().PersistentVolumes().Update(ctx, pv, metav1.UpdateOptions{})
	if e = s.CheckColdRuntimeExternalWriters(ctx, source); e == nil {
		t.Fatal("changed PV backing accepted as retired storage")
	}
}

func TestColdStandbyRequiresEveryWorkerButNotPublicReadiness(t *testing.T) {
	ctx := context.Background()
	image := "repo@sha256:" + strings.Repeat("a", 64)
	yes := true
	d := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "target", Namespace: "tenant", RuntimeType: "openclaw", Image: image, Replicas: 1, SlotCount: 8, AgentTokenSecretName: "agent", WorkspaceMountPath: "/workspaces", PodUserNamespaces: true, NetworkIsolation: true})
	d.UID = "target-uid"
	d.Labels[runtimePoolRoleLabel] = "upgrade-target"
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "target-rs", Namespace: "tenant", UID: "rs-uid", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: d.Name, UID: d.UID, Controller: &yes}}}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "target-pod", Namespace: "tenant", UID: "pod-uid", OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: &yes}}}, Spec: *d.Spec.Template.Spec.DeepCopy(), Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	for i, c := range pod.Spec.Containers {
		pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{Name: c.Name, Ready: i > 0, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}})
	}
	client := fake.NewSimpleClientset(d, rs, pod)
	s := &runtimeDeploymentService{client: client}
	target := RuntimeRolloutTarget{Namespace: d.Namespace, Name: d.Name, UID: string(d.UID), Image: image}
	if ready, err := s.ColdRuntimeStandbyReady(ctx, target, "pod-uid"); err != nil || !ready {
		t.Fatal("public coordinator readiness blocked native activation", ready, err)
	}
	pod.Status.ContainerStatuses[1].Ready = false
	client.CoreV1().Pods("tenant").Update(ctx, pod, metav1.UpdateOptions{})
	if ready, err := s.ColdRuntimeStandbyReady(ctx, target, "pod-uid"); err != nil || ready {
		t.Fatal("unhealthy native worker admitted activation", ready, err)
	}
	if ready, err := s.ColdRuntimeStandbyReady(ctx, target, "wrong-pod-uid"); err != nil || ready {
		t.Fatal("unconfirmed Pod identity admitted activation", ready, err)
	}
}
