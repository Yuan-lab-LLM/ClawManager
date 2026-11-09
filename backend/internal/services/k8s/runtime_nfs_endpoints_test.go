package k8s

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestRuntimeNFSStorageEndpointsPinBoundServer(t *testing.T) {
	for _, server := range []string{"203.0.113.20", "2001:db8::20", "nfs.example.com", ""} {
		t.Run(server, func(t *testing.T) {
			claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "workspace", Namespace: "lab", UID: "claim"}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "pv"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
			pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv"}, Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Name: "workspace", Namespace: "lab", UID: "claim"}, PersistentVolumeSource: corev1.PersistentVolumeSource{NFS: &corev1.NFSVolumeSource{Server: server, Path: "/"}}}}
			kube := fake.NewSimpleClientset(claim, pv)
			endpoints, err := RuntimeNFSStorageEndpoints(context.Background(), kube, "lab", "workspace")
			valid := server == "203.0.113.20" || server == "2001:db8::20"
			if (err == nil) != valid {
				t.Fatalf("endpoints=%v err=%v", endpoints, err)
			}
			if valid && (len(endpoints) != 1 || endpoints[0] != server) {
				t.Fatalf("unbound endpoints %v", endpoints)
			}
			pv.Spec.ClaimRef.UID = "replacement"
			if _, err = kube.CoreV1().PersistentVolumes().Update(context.Background(), pv, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err = RuntimeNFSStorageEndpoints(context.Background(), kube, "lab", "workspace"); err == nil {
				t.Fatal("stale binding accepted")
			}
		})
	}
}
