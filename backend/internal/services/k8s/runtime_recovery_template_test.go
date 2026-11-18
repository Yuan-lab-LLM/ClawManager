package k8s

import (
	"context"
	"fmt"
	"hash/fnv"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/dump"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"testing"
)

func TestStoppedRecoveryRequiresOriginalTemplateHash(t *testing.T) {
	for _, change := range []string{"none", "mount", "volume", "root", "collision"} {
		t.Run(change, func(t *testing.T) {
			template := corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "pool"}}, Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "workspaces", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "original"}}}}, Containers: []corev1.Container{{Name: "runtime", Env: []corev1.EnvVar{{Name: "RUNTIME_WORKSPACE_ROOT", Value: "/workspaces"}}, VolumeMounts: []corev1.VolumeMount{{Name: "workspaces", MountPath: "/workspaces"}}}}}}
			hash := retainedRuntimeTemplateHash(&template)
			template.Labels["pod-template-hash"] = hash
			rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "pool-" + hash, Labels: map[string]string{"pod-template-hash": hash}}, Spec: appsv1.ReplicaSetSpec{Template: template}}
			dep := &appsv1.Deployment{}
			switch change {
			case "mount":
				rs.Spec.Template.Spec.Containers[0].VolumeMounts[0].SubPath = "changed"
			case "volume":
				rs.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName = "changed"
			case "root":
				rs.Spec.Template.Spec.Containers[0].Env[0].Value = "/other"
			case "collision":
				n := int32(1)
				dep.Status.CollisionCount = &n
			}
			err := verifyRuntimeTemplateHash(rs, dep, retainedRuntimeTemplateHash(&rs.Spec.Template))
			if (err == nil) != (change == "none") {
				t.Fatalf("change %s: %v", change, err)
			}
		})
	}
}

func retainedRuntimeTemplateHash(template *corev1.PodTemplateSpec) string {
	clean := template.DeepCopy()
	delete(clean.Labels, "pod-template-hash")
	hasher := fnv.New32a()
	fmt.Fprint(hasher, dump.ForHash(*clean))
	return rand.SafeEncodeString(fmt.Sprint(hasher.Sum32()))
}
func fixtureRuntimeTemplateProof(_ context.Context, _ kubernetes.Interface, rs *appsv1.ReplicaSet, dep *appsv1.Deployment) error {
	return verifyRuntimeTemplateHash(rs, dep, retainedRuntimeTemplateHash(&rs.Spec.Template))
}
