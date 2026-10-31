package k8s

import (
	"context"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"strings"
	"testing"
)

func TestPoolValidationNeverMountsBusinessDataOrChangesSource(t *testing.T) {
	ctx := context.Background()
	source := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "source", Namespace: "tenant", RuntimeType: "openclaw", Image: "old", Replicas: 1, WorkspaceNFSServer: "10.0.0.1", WorkspaceNFSPath: "/business", AgentControlToken: "fixture", AgentReportToken: "fixture"})
	source.UID = "source-uid"
	source.Spec.Template.Spec.Volumes = append(source.Spec.Template.Spec.Volumes, corev1.Volume{Name: "another", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "business-pvc"}}})
	client := fake.NewSimpleClientset(source)
	service := &runtimeDeploymentService{client: client}
	target := RuntimeRolloutTarget{Namespace: "tenant", Name: "source", UID: "source-uid", Image: "old"}
	image := "repo@sha256:" + strings.Repeat("a", 64)
	if err := service.EnsurePoolValidation(ctx, target, "validation", image, "123"); err != nil {
		t.Fatal(err)
	}
	candidate, err := client.AppsV1().Deployments("tenant").Get(ctx, "validation", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ordinaryRuntimeDeployment(*candidate) {
		t.Fatal("validation pool became schedulable")
	}
	for _, v := range candidate.Spec.Template.Spec.Volumes {
		if v.NFS != nil || v.PersistentVolumeClaim != nil || v.HostPath != nil || v.CSI != nil {
			t.Fatal("business storage leaked into probe")
		}
	}
	original, _ := client.AppsV1().Deployments("tenant").Get(ctx, "source", metav1.GetOptions{})
	if runtimeContainerImage(original.Spec.Template.Spec.Containers) != "old" {
		t.Fatal("source mutated before validation")
	}
	if err = service.RolloutPoolImage(ctx, RuntimeRolloutTarget{Namespace: "tenant", Name: "source", UID: "stale"}, image); err == nil {
		t.Fatal("stale deployment identity accepted")
	}
	if err = service.StopPoolValidation(ctx, "tenant", "source", "123"); err == nil {
		t.Fatal("unrelated source stopped")
	}
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "source-old-rs", Namespace: "tenant", UID: "old-rs", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", UID: source.UID}}}}
	if _, err = client.AppsV1().ReplicaSets("tenant").Create(ctx, rs, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	now := metav1.Now()
	old := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "terminating-writer", Namespace: "tenant", DeletionTimestamp: &now, Finalizers: []string{"fixture"}, Labels: source.Spec.Selector.MatchLabels, OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID}}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "runtime", Image: "old"}}}}
	if _, err = client.CoreV1().Pods("tenant").Create(ctx, old, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if reason, err := service.PoolRetirementReason(ctx, target, "old"); err != nil || reason == "" {
		t.Fatal("terminating writer was ignored", reason, err)
	}
}

func TestPoolValidationObservesCrashAndStopsOnlyItsPool(t *testing.T) {
	ctx := context.Background()
	one := int32(1)
	uid := types.UID("candidate-uid")
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "probe", Namespace: "tenant", UID: uid, Labels: map[string]string{runtimePoolRoleLabel: poolValidationRole, runtimeUpgradeIDLabel: "150"}}, Spec: appsv1.DeploymentSpec{Replicas: &one}}
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "probe-rs", Namespace: "tenant", UID: "rs-uid", OwnerReferences: []metav1.OwnerReference{{UID: uid, Kind: "Deployment"}}}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "probe-pod", Namespace: "tenant", UID: "pod-uid", Labels: map[string]string{"app": "probe"}, OwnerReferences: []metav1.OwnerReference{{Name: "probe-rs", UID: "rs-uid", Kind: "ReplicaSet"}}}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "runtime", RestartCount: 2, LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1}}}}}}
	client := fake.NewSimpleClientset(deployment, rs, pod)
	service := &runtimeDeploymentService{client: client}
	status, err := service.ObservePoolValidation(ctx, "tenant", "probe", "150")
	if err != nil || !status.Failed || status.Ready {
		t.Fatal(status, err)
	}
	if err = service.StopPoolValidation(ctx, "tenant", "probe", "wrong"); err == nil {
		t.Fatal("ownership ignored")
	}
	if err = service.StopPoolValidation(ctx, "tenant", "probe", "150"); err != nil {
		t.Fatal(err)
	}
	d, _ := client.AppsV1().Deployments("tenant").Get(ctx, "probe", metav1.GetOptions{})
	if *d.Spec.Replicas != 0 {
		t.Fatal("probe kept running")
	}
}
