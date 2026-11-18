package k8s

import (
	"context"
	"errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
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
	if !errors.Is(e, ErrRuntimeFaultEvictionPending) || terminal {
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

func TestOrphanRetirementWaitsForObservedZeroReplicaSetsBeforePodDelete(t *testing.T) {
	ctx := context.Background()
	one := int32(1)
	zero := int32(0)
	yes := true
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "legacy", Namespace: "ns", UID: "dep", ResourceVersion: "1", Generation: 1, Labels: map[string]string{"clawmanager.io/runtime-type": "opencode"}}, Spec: appsv1.DeploymentSpec{Replicas: &one}, Status: appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: 1}}
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "legacy-rs", Namespace: "ns", UID: "rs", Generation: 1, OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: d.Name, UID: d.UID, Controller: &yes}}}, Spec: appsv1.ReplicaSetSpec{Replicas: &one}, Status: appsv1.ReplicaSetStatus{ObservedGeneration: 1, Replicas: 1}}
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "old", Namespace: "ns", UID: "uid", ResourceVersion: "2", OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: &yes}}}, Spec: corev1.PodSpec{NodeName: "node"}}
	client := fake.NewSimpleClientset(d, rs, p)
	client.PrependReactor("update", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		updated := action.(ktesting.UpdateAction).GetObject().(*appsv1.Deployment).DeepCopy()
		updated.Generation = 2
		e := client.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), updated, "ns")
		return true, updated, e
	})
	svc := &runtimeDeploymentService{client: client}
	source := &RuntimeOrphanSource{Namespace: "ns", PodName: "old", PodUID: "uid", DeploymentName: d.Name, DeploymentUID: "dep", ReplicaSetName: rs.Name, ReplicaSetUID: "rs", NodeName: "node"}
	assertNoDelete := func() {
		t.Helper()
		for _, a := range client.Actions() {
			if a.GetVerb() == "delete" && a.GetResource().Resource == "pods" {
				t.Fatal("deleted old Pod while ReplicaSet could still replace it")
			}
		}
	}
	if terminal, e := svc.RetireRuntimeOrphanController(ctx, source, "op"); terminal || !errors.Is(e, ErrRuntimeFaultEvictionPending) {
		t.Fatalf("unobserved scale must stay pending: %v %v", terminal, e)
	}
	assertNoDelete()
	pod, _ := client.CoreV1().Pods("ns").Get(ctx, "old", metav1.GetOptions{})
	if len(pod.Finalizers) != 1 || pod.Finalizers[0] != orphanFinalizer {
		t.Fatal("proof finalizer was not persisted first")
	}
	current, _ := client.AppsV1().Deployments("ns").Get(ctx, "legacy", metav1.GetOptions{})
	current.Status.ObservedGeneration = 2
	client.AppsV1().Deployments("ns").Update(ctx, current, metav1.UpdateOptions{})
	rs.Spec.Replicas = &zero
	rs.Generation = 2
	client.AppsV1().ReplicaSets("ns").Update(ctx, rs, metav1.UpdateOptions{})
	if _, e := svc.RetireRuntimeOrphanController(ctx, source, "op"); !errors.Is(e, ErrRuntimeFaultEvictionPending) {
		t.Fatalf("unobserved RS scale must stay pending: %v", e)
	}
	assertNoDelete()
	rs.Status.ObservedGeneration = 2
	client.AppsV1().ReplicaSets("ns").Update(ctx, rs, metav1.UpdateOptions{})
	if terminal, e := svc.RetireRuntimeOrphanController(ctx, source, "op"); e != nil || terminal {
		t.Fatalf("observed spec0 should request exact deletion, not synthesize terminal: %v %v", terminal, e)
	}
	deletes := 0
	for _, a := range client.Actions() {
		if a.GetVerb() == "delete" && a.GetResource().Resource == "pods" {
			deletes++
			options := a.(ktesting.DeleteAction).GetDeleteOptions()
			if options.Preconditions == nil || options.Preconditions.UID == nil || string(*options.Preconditions.UID) != "uid" {
				t.Fatal("deletion lost exact UID binding")
			}
		}
	}
	if deletes != 1 {
		t.Fatalf("expected one exact Pod delete, got %d", deletes)
	}
}
