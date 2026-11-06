package k8s

import (
	"clawreef/internal/models"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
	"time"
)

func TestStoppedRecoveryStorageRequiresRetainedSourceAndSameBacking(t *testing.T) {
	for _, change := range []string{"none", "missing-rs", "source-live", "wrong-node", "recreated-claim", "unknown-node", "wrong-image", "different-mount", "unbound-pv", "ledger-overlay", "ledger-root", "decoy-workspace", "decoy-ledger"} {
		t.Run(change, func(t *testing.T) {
			str := func(s string) *string { return &s }
			yes := true
			before := metav1.NewTime(time.Now().Add(-time.Hour))
			source := models.RuntimePod{Namespace: "runtime", PodName: "pool-abc-old", PodUID: str("old"), PodIP: str("10.1.0.2"), NodeName: str("node"), DeploymentName: "pool", ImageRef: "image@sha256:abc", RuntimeType: "opencode", CreatedAt: time.Now().Add(-time.Minute)}
			observer := models.RuntimePod{Namespace: "runtime", PodName: "pool-new", PodUID: str("new"), NodeName: str("node"), RuntimeType: "opencode"}
			spec := corev1.PodSpec{NodeName: "node", Volumes: []corev1.Volume{{Name: "workspaces", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}}}, Containers: []corev1.Container{{Name: "slot-0", Image: source.ImageRef, Env: []corev1.EnvVar{{Name: "RUNTIME_GATEWAY_PORT_START", Value: "20000"}, {Name: "RUNTIME_WORKSPACE_ROOT", Value: "/workspaces"}}, VolumeMounts: []corev1.VolumeMount{{Name: "workspaces", MountPath: "/workspaces"}}}}}
			rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "pool-abc", Namespace: "runtime", UID: "rs", CreationTimestamp: before, Labels: map[string]string{"pod-template-hash": "abc"}, OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "pool", UID: "dep", Controller: &yes}}}, Spec: appsv1.ReplicaSetSpec{Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"pod-template-hash": "abc"}}, Spec: spec}}}
			hash := retainedRuntimeTemplateHash(&rs.Spec.Template)
			rs.Name = "pool-" + hash
			rs.Labels["pod-template-hash"] = hash
			rs.Spec.Template.Labels["pod-template-hash"] = hash
			source.PodName = rs.Name + "-old"
			dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "runtime", UID: "dep"}}
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: observer.PodName, Namespace: "runtime", UID: types.UID("new")}, Spec: *spec.DeepCopy()}
			claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "runtime", UID: "claim", CreationTimestamp: before}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "pv"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
			pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv", UID: "pv-uid", CreationTimestamp: before}, Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Namespace: "runtime", Name: "data", UID: "claim"}, PersistentVolumeSource: corev1.PersistentVolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/data"}}}}
			node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node", UID: "node-uid", CreationTimestamp: before}, Spec: corev1.NodeSpec{PodCIDR: "10.1.0.0/24"}}
			switch change {
			case "decoy-workspace":
				rs.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "workspaces", MountPath: "/decoy"}, {Name: "workspaces", MountPath: "/workspaces", SubPath: "source"}}
				pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "workspaces", MountPath: "/decoy"}, {Name: "workspaces", MountPath: "/workspaces", SubPath: "observer"}}
			case "decoy-ledger":
				rs.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "workspaces", MountPath: "/decoy"}, {Name: "workspaces", MountPath: "/workspaces/opencode/user-1/instance-155", SubPath: "business"}, {Name: "workspaces", MountPath: "/workspaces/.clawmanager-slot-locks", SubPath: "source-ledger"}}
				pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{Name: "workspaces", MountPath: "/decoy"}, {Name: "workspaces", MountPath: "/workspaces/opencode/user-1/instance-155", SubPath: "business"}, {Name: "workspaces", MountPath: "/workspaces/.clawmanager-slot-locks", SubPath: "observer-ledger"}}
			case "wrong-node":
				pod.Spec.NodeName = "other"
			case "recreated-claim":
				claim.CreationTimestamp = metav1.Now()
			case "unknown-node":
				node.Spec.PodCIDR = "10.2.0.0/24"
			case "wrong-image":
				rs.Spec.Template.Spec.Containers[0].Image = "other"
			case "different-mount":
				pod.Spec.Containers[0].VolumeMounts[0].SubPath = "other"
			case "ledger-overlay":
				pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: "shadow", MountPath: "/workspaces/.clawmanager-slot-locks"})
			case "ledger-root":
				pod.Spec.Containers[0].Env[1].Value = "/different"
			case "unbound-pv":
				pv.Spec.ClaimRef.UID = "other"
			}
			kube := fake.NewSimpleClientset(rs, dep, pod, claim, pv, node)
			if change == "missing-rs" {
				_ = kube.AppsV1().ReplicaSets("runtime").Delete(t.Context(), rs.Name, metav1.DeleteOptions{})
			}
			if change == "source-live" {
				_, _ = kube.CoreV1().Pods("runtime").Create(t.Context(), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: source.PodName, Namespace: "runtime", UID: "old"}}, metav1.CreateOptions{})
			}
			proof, err := resolveStoppedSourceStorage(t.Context(), kube, source, observer, "/workspaces/opencode/user-1/instance-155", 20000, fixtureRuntimeTemplateProof)
			if change != "none" {
				if err == nil {
					t.Fatal("accepted unsafe provenance")
				}
				return
			}
			if err != nil || proof.Identity == "" || proof.Slot != "slot-0" {
				t.Fatalf("valid proof rejected: %+v %v", proof, err)
			}
		})
	}
}
