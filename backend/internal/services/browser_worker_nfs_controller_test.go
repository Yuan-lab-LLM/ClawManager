package services

import (
	"clawreef/internal/models"
	cmk8s "clawreef/internal/services/k8s"
	"context"
	"encoding/json"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"strings"
	"testing"
)

func TestNFSBrowserRecordsLegacyExposureWithoutAuthorizingMigration(t *testing.T) {
	for _, scenario := range []string{"valid", "missing-owner", "missing-pod", "wrong-owner"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("CLAWMANAGER_BROWSER_WORKER_NAMESPACE", "lab")
			instance := &models.Instance{ID: 12, UserID: 3}
			dep := desiredBrowserWorkerDeployment("clawbrowser-12", "lab", browserWorkerLabels(instance), "old", "workspace", 1, browserWorkerDefaults(12))
			dep.UID = "old-deployment"
			rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "old-rs", Namespace: "lab", UID: "old-rs", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: dep.Name, UID: dep.UID, Controller: boolptr(true)}}}}
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "old-pod", Namespace: "lab", UID: "old-pod", Labels: browserWorkerLabels(instance), OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: boolptr(true)}}}}
			objects := []runtime.Object{dep, rs, pod}
			if scenario == "missing-owner" {
				objects = []runtime.Object{rs, pod}
			}
			if scenario == "missing-pod" {
				objects = []runtime.Object{dep, rs}
			}
			if scenario == "wrong-owner" {
				rs.OwnerReferences[0].UID = "another-deployment"
			}
			repo := &memoryBrowserRegistry{memoryBrowserWorkerRepo: memoryBrowserWorkerRepo{config: browserWorkerDefaults(12)}}
			worker := &BrowserWorkerService{repo: repo, client: &cmk8s.Client{Clientset: fake.NewSimpleClientset(objects...), RuntimeSandboxProfile: cmk8s.RuntimeNFSRootlessProfile}}
			proof, _, err := worker.loadNFSBrowserRegistry(t.Context(), 12, true)
			if scenario != "valid" {
				if err == nil || repo.revision != 0 {
					t.Fatal("incomplete history was adopted")
				}
				return
			}
			if err != errNFSBrowserLegacyFence || !proof.Admitted || !proof.Stopping || proof.Stopped || proof.DeploymentUID != string(dep.UID) || proof.Pods[string(pod.UID)].Name != pod.Name {
				t.Fatalf("adoption did not durably fence live writer: %+v %v", proof, err)
			}
			restarted := &BrowserWorkerService{repo: repo, client: worker.client}
			if _, _, err = restarted.loadNFSBrowserRegistry(t.Context(), 12, false); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNFSBrowserOrdinalRejectsReplacedUIDAndLostFinalizer(t *testing.T) {
	t.Setenv("CLAWMANAGER_BROWSER_WORKER_NAMESPACE", "lab")
	instance := &models.Instance{ID: 12, UserID: 3}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "clawbrowser-12-0", Namespace: "lab", UID: "new-pod", Labels: browserWorkerLabels(instance), Annotations: map[string]string{nfsBrowserEpochAnnotation: "epoch"}, Finalizers: []string{nfsBrowserStopFinalizer}, OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", Name: "clawbrowser-12", UID: "set", Controller: boolptr(true)}}}}
	evidence := nfsBrowserStopEvidence{ControllerKind: "StatefulSet", Admitted: true, Epoch: "epoch", DeploymentUID: "set", Pods: map[string]nfsBrowserPodEvidence{"old-pod": {Name: pod.Name}}}
	raw, _ := json.Marshal(evidence)
	repo := &memoryBrowserRegistry{raw: string(raw), revision: 1}
	worker := &BrowserWorkerService{repo: repo, client: &cmk8s.Client{Clientset: fake.NewSimpleClientset(pod), RuntimeSandboxProfile: cmk8s.RuntimeNFSRootlessProfile}}
	for i := 0; i < 2; i++ {
		if err := worker.observeNFSBrowserPods(context.Background(), 12); err == nil {
			t.Fatal("replacement admitted without old UID proof")
		}
	}
	evidence.Pods = map[string]nfsBrowserPodEvidence{}
	pod.Finalizers = nil
	if validateNFSBrowserOrdinal(evidence, pod, "clawbrowser-12") == nil {
		t.Fatal("unretained writer accepted")
	}
}

func TestNFSBrowserMissingRegistryDoesNotInventNeverAdmittedForController(t *testing.T) {
	t.Setenv("CLAWMANAGER_BROWSER_WORKER_NAMESPACE", "lab")
	set := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "clawbrowser-12", Namespace: "lab", UID: "old-set"}}
	repo := &memoryBrowserRegistry{}
	worker := &BrowserWorkerService{repo: repo, client: &cmk8s.Client{Clientset: fake.NewSimpleClientset(set), RuntimeSandboxProfile: cmk8s.RuntimeNFSRootlessProfile}}
	if _, _, err := worker.loadNFSBrowserRegistry(t.Context(), 12, true); err == nil || repo.revision != 0 {
		t.Fatal("existing controller was declared never-admitted")
	}
}

func TestNFSBrowserLegacyHistoryAlwaysBlocksAutomaticCutover(t *testing.T) {
	t.Setenv("CLAWMANAGER_BROWSER_WORKER_NAMESPACE", "lab")
	for _, completed := range []bool{false, true} {
		t.Run(fmt.Sprint(completed), func(t *testing.T) {
			instance := &models.Instance{ID: 12, UserID: 3}
			dep := desiredBrowserWorkerDeployment("clawbrowser-12", "lab", browserWorkerLabels(instance), "old", "workspace", 0, browserWorkerDefaults(12))
			dep.UID = "legacy"
			dep.Spec.Template.Finalizers = []string{nfsBrowserStopFinalizer}
			dep.Generation = 4
			dep.Status.ObservedGeneration = 4
			proof := nfsBrowserStopEvidence{Admitted: true, Stopped: completed, DeploymentUID: "legacy", Pods: map[string]nfsBrowserPodEvidence{"old": {Name: "old", Complete: completed}}}
			if completed {
				p := proof.Pods["old"]
				p.ContainersSHA256 = strings.Repeat("a", 64)
				proof.Pods["old"] = p
			}
			raw, _ := json.Marshal(proof)
			repo := &memoryBrowserRegistry{raw: string(raw), revision: 1}
			kube := fake.NewSimpleClientset(dep)
			worker := &BrowserWorkerService{repo: repo, client: &cmk8s.Client{Clientset: kube, RuntimeSandboxProfile: cmk8s.RuntimeNFSRootlessProfile}}
			for i := 0; i < 2; i++ {
				if err := worker.stopNFSBrowserWriter(t.Context(), 12, false); err != errNFSBrowserLegacyFence {
					t.Fatal("historical controller stop accepted", err)
				}
			}
			if err := worker.applyNFSBrowserWorkload(t.Context(), instance, dep.DeepCopy()); err != errNFSBrowserLegacyFence {
				t.Fatal("historical controller cutover accepted", err)
			}
			if repo.raw != string(raw) || repo.revision != 1 {
				t.Fatal("legacy receipts were overwritten")
			}
			for _, a := range kube.Actions() {
				if a.GetVerb() != "get" && a.GetVerb() != "list" {
					t.Fatal("legacy history triggered Kubernetes mutation", a)
				}
			}
		})
	}
}

func TestNFSBrowserDelayedCreateCannotReuseRetiredOrdinal(t *testing.T) {
	t.Setenv("CLAWMANAGER_BROWSER_WORKER_NAMESPACE", "lab")
	instance := &models.Instance{ID: 12, UserID: 3, Status: "running"}
	repo := &memoryBrowserRegistry{raw: `{"Pods":{}}`, revision: 1}
	kube := fake.NewSimpleClientset()
	worker := &BrowserWorkerService{repo: repo, client: &cmk8s.Client{Clientset: kube, RuntimeSandboxProfile: cmk8s.RuntimeNFSRootlessProfile}}
	kube.PrependReactor("create", "statefulsets", func(action ktesting.Action) (bool, runtime.Object, error) {
		set := action.(ktesting.CreateAction).GetObject().(*appsv1.StatefulSet)
		set.UID = types.UID("uid-" + set.Name)
		return false, nil, nil
	})
	kube.PrependReactor("delete", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		name := action.(ktesting.DeleteAction).GetName()
		obj, err := kube.Tracker().Get(corev1.SchemeGroupVersion.WithResource("pods"), "lab", name)
		if err != nil {
			return true, nil, err
		}
		pod := obj.(*corev1.Pod)
		now := metav1.Now()
		pod.DeletionTimestamp = &now
		pod.Status.Phase = corev1.PodSucceeded
		pod.Status.ContainerStatuses[0].State = corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ContainerID: pod.Status.ContainerStatuses[0].ContainerID}}
		return true, nil, kube.Tracker().Update(corev1.SchemeGroupVersion.WithResource("pods"), pod, "lab")
	})
	kube.PrependReactor("update", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		pod := action.(ktesting.UpdateAction).GetObject().(*corev1.Pod)
		if pod.DeletionTimestamp != nil && len(pod.Finalizers) == 0 {
			return true, pod, kube.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), "lab", pod.Name)
		}
		return false, nil, nil
	})
	desired := desiredBrowserWorkerDeployment("clawbrowser-12", "lab", browserWorkerLabels(instance), "old", "workspace", 1, browserWorkerDefaults(12))
	if err := configureNFSBrowserWorker(desired, instance); err != nil {
		t.Fatal(err)
	}
	if err := worker.applyNFSBrowserWorkload(t.Context(), instance, desired.DeepCopy()); err != nil {
		t.Fatal(err)
	}
	first, err := worker.getBrowserWorkload(t.Context(), 12, "StatefulSet")
	if err != nil {
		t.Fatal(err)
	}
	makePod := func(dep *appsv1.Deployment, uid string) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: dep.Name + "-0", Namespace: "lab", UID: types.UID(uid), Labels: dep.Spec.Template.Labels, Annotations: dep.Spec.Template.Annotations, Finalizers: []string{nfsBrowserStopFinalizer}, OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", Name: dep.Name, UID: dep.UID, Controller: boolptr(true)}}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "browser"}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: "browser", ContainerID: "containerd://" + uid, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
	}
	delayed := makePod(first, "delayed-A")
	ambiguous := true
	kube.PrependReactor("create", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		if ambiguous {
			ambiguous = false
			return true, nil, apierrors.NewTimeoutError("Create A may still commit", 1)
		}
		return false, nil, nil
	})
	if _, err := kube.CoreV1().Pods("lab").Create(t.Context(), delayed, metav1.CreateOptions{}); !apierrors.IsTimeout(err) {
		t.Fatal("fault did not return ambiguous timeout", err)
	}
	retryPod := makePod(first, "retry-B")
	if _, err := kube.CoreV1().Pods("lab").Create(t.Context(), retryPod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := worker.observeNFSBrowserPods(t.Context(), 12); err != nil {
		t.Fatal(err)
	}
	if err := worker.stopNFSBrowserWriter(t.Context(), 12, false); err != nil {
		t.Fatal(err)
	}
	tombstone, err := kube.CoreV1().Pods("lab").Get(t.Context(), retryPod.Name, metav1.GetOptions{})
	if err != nil || len(tombstone.Finalizers) == 0 {
		t.Fatal("retirement freed the name for delayed Create A", err)
	}
	worker = &BrowserWorkerService{repo: repo, client: worker.client}
	if err := worker.applyNFSBrowserWorkload(t.Context(), instance, desired.DeepCopy()); err != nil {
		t.Fatal(err)
	}
	second, err := worker.getBrowserWorkload(t.Context(), 12, "StatefulSet")
	if err != nil || second.Name == first.Name {
		t.Fatal("new epoch reused retired controller name", err)
	}
	if err := kube.Tracker().Create(corev1.SchemeGroupVersion.WithResource("pods"), delayed, "lab"); !apierrors.IsAlreadyExists(err) {
		t.Fatal("late Create A could become a writer", err)
	}
	nextPod := makePod(second, "next-C")
	if _, err := kube.CoreV1().Pods("lab").Create(t.Context(), nextPod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := worker.deleteResources(t.Context(), 12); err != nil {
		t.Fatal(err)
	}
	sets, _ := kube.AppsV1().StatefulSets("lab").List(t.Context(), metav1.ListOptions{})
	pods, _ := kube.CoreV1().Pods("lab").List(t.Context(), metav1.ListOptions{})
	if len(sets.Items) != 2 || len(pods.Items) != 2 {
		t.Fatal("configuration deletion garbage-collected an execution name")
	}
	if err := kube.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), "lab", retryPod.Name); err != nil {
		t.Fatal(err)
	}
	if err := worker.applyNFSBrowserWorkload(t.Context(), instance, desired.DeepCopy()); err == nil {
		t.Fatal("lost historical tombstone allowed another admission")
	}
}
