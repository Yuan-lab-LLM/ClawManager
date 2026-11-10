package k8s

import (
	"context"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestOrphanExternalGatePreservesSupportedDSHControllers(t *testing.T) {
	ctx := context.Background()
	yes := true
	spec := corev1.PodSpec{Volumes: []corev1.Volume{{Name: "workspaces", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}}}, Containers: []corev1.Container{{Name: "slot-0", VolumeMounts: []corev1.VolumeMount{{Name: "workspaces", MountPath: "/workspaces"}}}}}
	source := &RuntimeOrphanSource{Namespace: "runtime", PodUID: "old", WorkspaceRoot: "/workspaces", SourcePod: &corev1.Pod{Spec: spec}}
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "runtime"}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "pv"}}
	pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv"}, Spec: corev1.PersistentVolumeSpec{PersistentVolumeSource: corev1.PersistentVolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/data"}}}}
	client := fake.NewSimpleClientset(claim, pv)
	for _, name := range []string{"selected-dsh", "healthy-dsh-neighbor"} {
		d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "runtime", UID: types.UID(name), Labels: map[string]string{"clawmanager.io/runtime-type": "deepseek-harness"}}, Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: spec}}}
		rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: name + "-rs", Namespace: "runtime", UID: types.UID(name + "-rs"), OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: name, UID: d.UID, Controller: &yes}}}}
		p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name + "-pod", Namespace: "runtime", UID: types.UID(name + "-pod"), OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: &yes}}}, Spec: spec}
		if name == "selected-dsh" {
			p.UID = "old"
		}
		client.AppsV1().Deployments("runtime").Create(ctx, d, metav1.CreateOptions{})
		client.AppsV1().ReplicaSets("runtime").Create(ctx, rs, metav1.CreateOptions{})
		client.CoreV1().Pods("runtime").Create(ctx, p, metav1.CreateOptions{})
	}
	if e := CheckRuntimeOrphanExternalWriters(ctx, client, source); e != nil {
		t.Fatalf("supported DSH source or healthy neighbor rejected: %v", e)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "update" || action.GetVerb() == "delete" {
			t.Fatal("readonly external gate mutated neighbor")
		}
	}
}
