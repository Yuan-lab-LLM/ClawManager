package k8s

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestIdleStorageRejectsEphemeralUnboundAndReplacedPod(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "runtime", UID: types.UID("original")}, Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "workspaces", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}, Containers: []corev1.Container{{Name: "slot-0", VolumeMounts: []corev1.VolumeMount{{Name: "workspaces", MountPath: "/workspaces"}}}}}}
	kube := fake.NewSimpleClientset(pod)
	if _, err := ResolveRuntimePersistentStorage(t.Context(), kube, "runtime", "pool", "original"); err == nil {
		t.Fatal("emptyDir accepted")
	}
	pod.Spec.Volumes[0].VolumeSource = corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}
	if _, err := kube.CoreV1().Pods("runtime").Update(t.Context(), pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "runtime", UID: types.UID("claim")}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "pv"}}
	if _, err := kube.CoreV1().PersistentVolumeClaims("runtime").Create(t.Context(), claim, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveRuntimePersistentStorage(t.Context(), kube, "runtime", "pool", "original"); err == nil {
		t.Fatal("unbound claim accepted")
	}
	claim.Status.Phase = corev1.ClaimBound
	if _, err := kube.CoreV1().PersistentVolumeClaims("runtime").Update(t.Context(), claim, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	identity, err := ResolveRuntimePersistentStorage(t.Context(), kube, "runtime", "pool", "original")
	if err != nil || identity == "" {
		t.Fatalf("bound claim rejected %s %v", identity, err)
	}
	if _, err = ResolveRuntimePersistentStorage(t.Context(), kube, "runtime", "pool", "replacement"); err == nil {
		t.Fatal("replacement Pod accepted")
	}
}

func TestIdleStorageRequiresWorkspaceBackedBySameUnshadowedVolume(t *testing.T) {
	base := func() *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "runtime", UID: types.UID("uid")}, Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "workspaces", VolumeSource: corev1.VolumeSource{NFS: &corev1.NFSVolumeSource{Server: "10.0.0.9", Path: "/export"}}}, {Name: "ephemeral", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}, Containers: []corev1.Container{{Name: "slot-0", VolumeMounts: []corev1.VolumeMount{{Name: "workspaces", MountPath: "/workspaces"}}}}}}
	}
	for _, test := range []struct {
		name   string
		change func(*corev1.Pod)
		valid  bool
	}{
		{"persistent volume elsewhere", func(p *corev1.Pod) { p.Spec.Containers[0].VolumeMounts[0].MountPath = "/archive" }, false},
		{"runtime subtree shadow", func(p *corev1.Pod) {
			p.Spec.Containers[0].VolumeMounts = append(p.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: "ephemeral", MountPath: "/workspaces/openclaw"})
		}, false},
		{"home shadow", func(p *corev1.Pod) {
			p.Spec.Containers[0].VolumeMounts = append(p.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: "ephemeral", MountPath: "/workspaces/openclaw/user-7/instance-1/home"})
		}, false},
		{"inconsistent slot view", func(p *corev1.Pod) {
			p.Spec.Containers = append(p.Spec.Containers, corev1.Container{Name: "slot-1", VolumeMounts: []corev1.VolumeMount{{Name: "workspaces", MountPath: "/workspaces", SubPath: "different"}}})
		}, false},
		{"sibling shadow does not cover this instance", func(p *corev1.Pod) {
			p.Spec.Containers[0].VolumeMounts = append(p.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: "ephemeral", MountPath: "/workspaces/openclaw/user-7/instance-10"})
		}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := base()
			test.change(p)
			_, err := ResolveRuntimePersistentStorage(t.Context(), fake.NewSimpleClientset(p), "runtime", "pool", "uid", "/workspaces/openclaw/user-7/instance-1")
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v error=%v", test.valid, err)
			}
		})
	}
}
