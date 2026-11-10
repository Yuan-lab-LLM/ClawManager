package k8s

import (
	"context"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestOrphanControllerRetirementPreservesUIDAndTerminalBarrier(t *testing.T) {
	ctx := context.Background()
	one := int32(1)
	yes := true
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "legacy", Namespace: "ns", UID: "dep", ResourceVersion: "1", Labels: map[string]string{"clawmanager.io/runtime-type": "opencode"}}, Spec: appsv1.DeploymentSpec{Replicas: &one, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "legacy"}}}}
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "legacy-rs", Namespace: "ns", UID: "rs", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: d.Name, UID: d.UID, Controller: &yes}}}}
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "old", Namespace: "ns", UID: "uid", ResourceVersion: "2", Labels: map[string]string{"app": "legacy"}, OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: &yes}}}, Spec: corev1.PodSpec{NodeName: "node"}}
	client := fake.NewSimpleClientset(d, rs, p)
	svc := &runtimeDeploymentService{client: client}
	source := &RuntimeOrphanSource{Namespace: "ns", PodName: "old", PodUID: "uid", DeploymentName: d.Name, DeploymentUID: "dep", ReplicaSetName: rs.Name, ReplicaSetUID: "rs", NodeName: "node"}
	terminal, e := svc.RetireRuntimeOrphanController(ctx, source, "operation")
	if e != nil || terminal {
		t.Fatalf("unproved terminal accepted %v %v", terminal, e)
	}
	got, _ := client.AppsV1().Deployments("ns").Get(ctx, d.Name, metav1.GetOptions{})
	if *got.Spec.Replicas != 0 {
		t.Fatal("controller not fenced")
	}
	gotPod, _ := client.CoreV1().Pods("ns").Get(ctx, p.Name, metav1.GetOptions{})
	if gotPod == nil {
		t.Fatal("deleted before terminal barrier could be retained")
	}
}
