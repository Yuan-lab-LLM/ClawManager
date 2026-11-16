package k8s

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestColdNFSPVCWrapperRequiresExactLiveExportAndMapping(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*corev1.Pod, *corev1.Pod, *corev1.PersistentVolumeClaim, *corev1.PersistentVolume)
		good   bool
	}{
		{"same export", func(*corev1.Pod, *corev1.Pod, *corev1.PersistentVolumeClaim, *corev1.PersistentVolume) {}, true},
		{"different export", func(_, _ *corev1.Pod, _ *corev1.PersistentVolumeClaim, v *corev1.PersistentVolume) {
			v.Spec.NFS.Path = "/other"
		}, false},
		{"replaced source", func(s, _ *corev1.Pod, _ *corev1.PersistentVolumeClaim, _ *corev1.PersistentVolume) { s.UID = "other" }, false},
		{"wrong claim identity", func(_, _ *corev1.Pod, _ *corev1.PersistentVolumeClaim, v *corev1.PersistentVolume) {
			v.Spec.ClaimRef.UID = "other"
		}, false},
		{"target subpath", func(_, p *corev1.Pod, _ *corev1.PersistentVolumeClaim, _ *corev1.PersistentVolume) {
			p.Spec.Containers[0].VolumeMounts[0].SubPath = "other"
		}, false},
		{"delete policy", func(_, _ *corev1.Pod, _ *corev1.PersistentVolumeClaim, v *corev1.PersistentVolume) {
			v.Spec.PersistentVolumeReclaimPolicy = corev1.PersistentVolumeReclaimDelete
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "old", Namespace: "ns", UID: types.UID("old-uid")}, Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "workspaces", VolumeSource: corev1.VolumeSource{NFS: &corev1.NFSVolumeSource{Server: "10.0.0.1", Path: "/"}}}}, Containers: []corev1.Container{{Name: "runtime", VolumeMounts: []corev1.VolumeMount{{Name: "workspaces", MountPath: "/workspaces"}}}}}}
			target := old.DeepCopy()
			target.Name = "new"
			target.UID = "new-uid"
			target.Spec.Volumes[0].VolumeSource = corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}
			claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "ns", UID: "claim"}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "pv", AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
			pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv", UID: "pv-uid"}, Spec: corev1.PersistentVolumeSpec{PersistentVolumeSource: corev1.PersistentVolumeSource{NFS: &corev1.NFSVolumeSource{Server: "10.0.0.1", Path: "/"}}, ClaimRef: &corev1.ObjectReference{Namespace: "ns", Name: "data", UID: "claim"}, AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}, PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain}}
			tc.change(old, target, claim, pv)
			ok, e := VerifyColdNFSWorkspace(t.Context(), fake.NewSimpleClientset(old, target, claim, pv), "ns", "old", "old-uid", "new", "new-uid", "/workspaces/openclaw/user-1/instance-7")
			if e != nil || ok != tc.good {
				t.Fatalf("accepted=%v err=%v", ok, e)
			}
		})
	}
}
