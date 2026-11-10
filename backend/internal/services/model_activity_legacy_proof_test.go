package services

import (
	"context"
	"errors"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func legacyProofOwner(kind, name string, uid types.UID) []metav1.OwnerReference {
	yes := true
	return []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: kind, Name: name, UID: uid, Controller: &yes}}
}

func legacyProofObjects() (*appsv1.Deployment, *appsv1.ReplicaSet, *corev1.Pod) {
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: "cm", Name: "manager", UID: "deployment"}}
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Namespace: "cm", Name: "manager-new", UID: "rs-new", OwnerReferences: legacyProofOwner("Deployment", deployment.Name, deployment.UID)}}
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "cm", Name: "manager-new-a", UID: "pod-a", CreationTimestamp: metav1.NewTime(time.Unix(100, 0)), OwnerReferences: legacyProofOwner("ReplicaSet", rs.Name, rs.UID)},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "clawmanager-app", Image: "cm:new"}}},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}, ContainerStatuses: []corev1.ContainerStatus{{Name: "clawmanager-app", Ready: true, ImageID: "sha256:new", ContainerID: "containerd://a", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(time.Unix(200, 0))}}}}},
	}
	return deployment, rs, pod
}

func TestModelActivityLegacyCutoff(t *testing.T) {
	for _, tc := range []struct {
		name     string
		change   func(*appsv1.Deployment, *appsv1.ReplicaSet, *corev1.Pod) []runtime.Object
		wantErr  bool
		wantTime int64
	}{
		{name: "healthy single replica", wantTime: 200},
		{name: "ready restarted process uses container start", wantTime: 200, change: func(_ *appsv1.Deployment, _ *appsv1.ReplicaSet, p *corev1.Pod) []runtime.Object {
			p.Status.ContainerStatuses[0].RestartCount = 1
			return nil
		}},
		{name: "healthy two replicas use earliest process", wantTime: 150, change: func(_ *appsv1.Deployment, _ *appsv1.ReplicaSet, p *corev1.Pod) []runtime.Object {
			other := p.DeepCopy()
			other.Name = "manager-new-b"
			other.UID = "pod-b"
			other.Status.ContainerStatuses[0].ContainerID = "containerd://b"
			other.Status.ContainerStatuses[0].State.Running.StartedAt = metav1.NewTime(time.Unix(150, 0))
			return []runtime.Object{other}
		}},
		{name: "old image in another rollout replica set", wantErr: true, change: legacyProofOldReplica(false)},
		{name: "terminating old image still blocks", wantErr: true, change: legacyProofOldReplica(true)},
		{name: "same tag different running image blocks", wantErr: true, change: func(_ *appsv1.Deployment, _ *appsv1.ReplicaSet, p *corev1.Pod) []runtime.Object {
			other := p.DeepCopy()
			other.Name = "other"
			other.UID = "other"
			other.Status.ContainerStatuses[0].ImageID = "sha256:old"
			return []runtime.Object{other}
		}},
		{name: "unrelated deployment ignored despite same labels", wantTime: 200, change: func(_ *appsv1.Deployment, _ *appsv1.ReplicaSet, p *corev1.Pod) []runtime.Object {
			d, rs, other := legacyProofObjects()
			d.Name = "unrelated"
			d.UID = "unrelated"
			rs.Name = "unrelated-rs"
			rs.UID = "unrelated-rs"
			rs.OwnerReferences = legacyProofOwner("Deployment", d.Name, d.UID)
			other.Name = "unrelated-pod"
			other.UID = "unrelated-pod"
			other.OwnerReferences = legacyProofOwner("ReplicaSet", rs.Name, rs.UID)
			other.Spec.Containers[0].Image = "cm:old"
			other.Status.Phase = corev1.PodPending
			other.Labels = map[string]string{"app": "clawmanager-app"}
			p.Labels = other.Labels
			return []runtime.Object{d, rs, other}
		}},
		{name: "unrelated standalone replica set ignored", wantTime: 200, change: func(_ *appsv1.Deployment, _ *appsv1.ReplicaSet, p *corev1.Pod) []runtime.Object {
			other := p.DeepCopy()
			other.Name, other.UID = "database", "database"
			other.OwnerReferences = legacyProofOwner("ReplicaSet", "database", "database-rs")
			other.Spec.Containers[0].Name = "database"
			other.Status.ContainerStatuses[0].Name = "database"
			set := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Namespace: "cm", Name: "database", UID: "database-rs"}}
			return []runtime.Object{set, other}
		}},
		{name: "orphaned application pod blocks", wantErr: true, change: func(_ *appsv1.Deployment, _ *appsv1.ReplicaSet, p *corev1.Pod) []runtime.Object {
			other := p.DeepCopy()
			other.Name, other.UID = "orphan", "orphan"
			other.OwnerReferences = legacyProofOwner("ReplicaSet", "deleted-old", "deleted-old")
			return []runtime.Object{other}
		}},
		{name: "unready current pod", wantErr: true, change: func(_ *appsv1.Deployment, _ *appsv1.ReplicaSet, p *corev1.Pod) []runtime.Object {
			p.Status.ContainerStatuses[0].Ready = false
			return nil
		}},
		{name: "pending replica blocks", wantErr: true, change: func(_ *appsv1.Deployment, _ *appsv1.ReplicaSet, p *corev1.Pod) []runtime.Object {
			other := p.DeepCopy()
			other.Name = "pending"
			other.UID = "pending"
			other.Status.Phase = corev1.PodPending
			return []runtime.Object{other}
		}},
		{name: "restarting replica blocks", wantErr: true, change: func(_ *appsv1.Deployment, _ *appsv1.ReplicaSet, p *corev1.Pod) []runtime.Object {
			other := p.DeepCopy()
			other.Name = "restarting"
			other.UID = "restarting"
			other.Status.ContainerStatuses[0].State = corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}
			return []runtime.Object{other}
		}},
		{name: "unknown current ownership", wantErr: true, change: func(_ *appsv1.Deployment, _ *appsv1.ReplicaSet, p *corev1.Pod) []runtime.Object {
			p.OwnerReferences = nil
			return nil
		}},
		{name: "mismatched replica set UID", wantErr: true, change: func(_ *appsv1.Deployment, _ *appsv1.ReplicaSet, p *corev1.Pod) []runtime.Object {
			p.OwnerReferences[0].UID = "replaced"
			return nil
		}},
		{name: "mismatched deployment UID", wantErr: true, change: func(_ *appsv1.Deployment, rs *appsv1.ReplicaSet, _ *corev1.Pod) []runtime.Object {
			rs.OwnerReferences[0].UID = "replaced"
			return nil
		}},
		{name: "unknown image identity", wantErr: true, change: func(_ *appsv1.Deployment, _ *appsv1.ReplicaSet, p *corev1.Pod) []runtime.Object {
			p.Status.ContainerStatuses[0].ImageID = ""
			return nil
		}},
		{name: "unknown process start", wantErr: true, change: func(_ *appsv1.Deployment, _ *appsv1.ReplicaSet, p *corev1.Pod) []runtime.Object {
			p.Status.ContainerStatuses[0].State.Running.StartedAt = metav1.Time{}
			return nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, rs, pod := legacyProofObjects()
			objects := []runtime.Object{d, rs, pod}
			if tc.change != nil {
				objects = append(objects, tc.change(d, rs, pod)...)
			}
			cutoff, err := modelActivityLegacyCutoff(context.Background(), fake.NewSimpleClientset(objects...), "cm", pod.Name)
			if tc.wantErr {
				if err == nil || cutoff != nil {
					t.Fatalf("unsafe proof returned cutoff=%v err=%v", cutoff, err)
				}
				return
			}
			if err != nil || cutoff == nil || cutoff.Unix() != tc.wantTime {
				t.Fatalf("cutoff=%v err=%v; want unix %d", cutoff, err, tc.wantTime)
			}
		})
	}
}

func legacyProofOldReplica(terminating bool) func(*appsv1.Deployment, *appsv1.ReplicaSet, *corev1.Pod) []runtime.Object {
	return func(d *appsv1.Deployment, _ *appsv1.ReplicaSet, p *corev1.Pod) []runtime.Object {
		rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Namespace: "cm", Name: "manager-old", UID: "rs-old", OwnerReferences: legacyProofOwner("Deployment", d.Name, d.UID)}}
		old := p.DeepCopy()
		old.Name = "manager-old-a"
		old.UID = "pod-old"
		old.OwnerReferences = legacyProofOwner("ReplicaSet", rs.Name, rs.UID)
		old.Spec.Containers[0].Image = "cm:old"
		old.Status.ContainerStatuses[0].ImageID = "sha256:old"
		if terminating {
			now := metav1.Now()
			old.DeletionTimestamp = &now
			old.Finalizers = []string{"keep"}
		}
		return []runtime.Object{rs, old}
	}
}

func TestModelActivityLegacyRejectsUnstableProof(t *testing.T) {
	for _, scenario := range []string{"API failure", "replaced own UID", "restarted own process", "old replica appears on relist"} {
		t.Run(scenario, func(t *testing.T) {
			d, rs, pod := legacyProofObjects()
			kube := fake.NewSimpleClientset(d, rs, pod)
			gets, lists := 0, 0
			kube.PrependReactor("get", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
				gets++
				if scenario == "API failure" {
					return true, nil, errors.New("API unavailable")
				}
				if gets > 1 && (scenario == "replaced own UID" || scenario == "restarted own process") {
					copy := pod.DeepCopy()
					if scenario == "replaced own UID" {
						copy.UID = "replacement"
					} else {
						copy.Status.ContainerStatuses[0].ContainerID = "containerd://replacement"
					}
					return true, copy, nil
				}
				return false, nil, nil
			})
			kube.PrependReactor("list", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
				lists++
				if scenario == "old replica appears on relist" && lists > 1 {
					old := pod.DeepCopy()
					old.Name = "old"
					old.UID = "old"
					old.Status.ContainerStatuses[0].ImageID = "sha256:old"
					return true, &corev1.PodList{Items: []corev1.Pod{*pod, *old}}, nil
				}
				return false, nil, nil
			})
			cutoff, err := modelActivityLegacyCutoff(context.Background(), kube, "cm", pod.Name)
			if err == nil || cutoff != nil {
				t.Fatalf("unstable proof returned cutoff=%v err=%v", cutoff, err)
			}
		})
	}
}
