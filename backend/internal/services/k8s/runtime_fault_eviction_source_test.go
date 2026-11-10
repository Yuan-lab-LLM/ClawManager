package k8s

import (
	"clawreef/internal/models"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestFaultEvictionHistoricalPlacementUsesOriginalTemplate(t *testing.T) {
	uid := "old"
	digest := "sha256:old"
	source := models.RuntimePod{Namespace: "ns", PodUID: &uid, ImageRef: "image@sha256:old", ImageDigest: &digest}
	resources := corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("256Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("4Gi")}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", UID: "old"}, Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "workspaces", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}}}, Containers: []corev1.Container{{Name: "runtime", Image: source.ImageRef, VolumeMounts: []corev1.VolumeMount{{Name: "workspaces", MountPath: "/workspaces"}}}, {Name: "slot-0", Image: source.ImageRef, Resources: resources, VolumeMounts: []corev1.VolumeMount{{Name: "workspaces", MountPath: "/workspaces"}}}}}}
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "ns", UID: "claim"}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "pv"}}
	got, err := RuntimeFaultSourcePlacement(pod, claim, source, "slot-0")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Slots) != 1 || *got.Slots[0].CPUMillisLimit != 2000 || *got.Slots[0].MemoryBytesRequest != 256<<20 || got.WorkspaceStorageIdentity == "" {
		t.Fatalf("lost original placement: %+v", got)
	}
	for _, change := range []string{"image", "uid", "resources"} {
		bad := pod.DeepCopy()
		switch change {
		case "image":
			bad.Spec.Containers[1].Image = "other"
		case "uid":
			bad.UID = "replacement"
		case "resources":
			bad.Spec.Containers[1].Resources.Requests = nil
		}
		if _, err = RuntimeFaultSourcePlacement(bad, claim, source, "slot-0"); err == nil {
			t.Fatalf("accepted changed %s", change)
		}
	}
}
