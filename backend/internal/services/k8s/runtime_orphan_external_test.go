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

func TestOrphanExternalGateUsesBackingNotContainerMountPath(t *testing.T) {
	for _, kind := range []string{"distinct", "same-host", "same-nfs-alias", "loopback-alias", "host-nfs-alias", "missing-pvc", "missing-pv", "unsupported-csi", "unsupported-local"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			spec := func(claim string) corev1.PodSpec {
				return corev1.PodSpec{Volumes: []corev1.Volume{{Name: "workspace", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claim}}}}, Containers: []corev1.Container{{Name: "app", VolumeMounts: []corev1.VolumeMount{{Name: "workspace", MountPath: "/workspaces"}}}}}
			}
			source := &RuntimeOrphanSource{Namespace: "runtime", PodUID: "old", WorkspaceRoot: "/workspaces", SourcePod: &corev1.Pod{Spec: spec("source")}}
			sourceClaim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: "runtime"}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "source-pv"}}
			candidateClaim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "ltt"}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "other-pv"}}
			sourcePV := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "source-pv", UID: "source-volume"}, Spec: corev1.PersistentVolumeSpec{PersistentVolumeSource: corev1.PersistentVolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/data/clawmanager/system/workspaces"}}}}
			otherPV := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "other-pv", UID: "other-volume"}, Spec: corev1.PersistentVolumeSpec{PersistentVolumeSource: corev1.PersistentVolumeSource{NFS: &corev1.NFSVolumeSource{Server: "127.0.0.1", Path: "/ltt-test-20261106"}}}}
			if kind == "unsupported-csi" {
				otherPV.Spec.PersistentVolumeSource = corev1.PersistentVolumeSource{CSI: &corev1.CSIPersistentVolumeSource{Driver: "unknown.example", VolumeHandle: "volume"}}
			}
			if kind == "unsupported-local" {
				otherPV.Spec.PersistentVolumeSource = corev1.PersistentVolumeSource{Local: &corev1.LocalVolumeSource{Path: "/unrelated"}}
			}
			if kind == "host-nfs-alias" {
				otherPV.Spec.NFS.Path = "/data/clawmanager/system/workspaces/subdirectory"
			}
			if kind == "same-host" {
				otherPV.Spec.PersistentVolumeSource = corev1.PersistentVolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/data/clawmanager/system/workspaces/subdirectory"}}
			}
			if kind == "same-nfs-alias" || kind == "loopback-alias" {
				sourcePV.Spec.PersistentVolumeSource = corev1.PersistentVolumeSource{NFS: &corev1.NFSVolumeSource{Server: "127.0.0.1", Path: "/workspace-export"}}
				otherPV.Spec.PersistentVolumeSource = corev1.PersistentVolumeSource{NFS: &corev1.NFSVolumeSource{Server: "localhost", Path: "/workspace-export/subdirectory"}}
				if kind == "loopback-alias" {
					otherPV.Spec.NFS.Server = "::1"
				}
			}
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "clawmanager-app-other", Namespace: "ltt", UID: "unrelated"}, Spec: spec("other")}
			client := fake.NewSimpleClientset(sourceClaim, sourcePV, pod)
			if kind != "missing-pvc" {
				client.CoreV1().PersistentVolumeClaims("ltt").Create(ctx, candidateClaim, metav1.CreateOptions{})
			}
			if kind != "missing-pv" {
				client.CoreV1().PersistentVolumes().Create(ctx, otherPV, metav1.CreateOptions{})
			}
			err := CheckRuntimeOrphanExternalWriters(ctx, client, source)
			if kind == "distinct" {
				if err != nil {
					t.Fatalf("distinct NFS test workspace shares only container path: %v", err)
				}
			} else if err == nil {
				t.Fatalf("unproved or shared backing %s allowed", kind)
			}
		})
	}
}
