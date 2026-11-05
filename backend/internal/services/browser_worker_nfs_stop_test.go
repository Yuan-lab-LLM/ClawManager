package services

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"clawreef/internal/models"
	cmk8s "clawreef/internal/services/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestNFSBrowserStopPersistsTerminationBeforeRemovingFinalizer(t *testing.T) {
	for _, outcome := range []string{"terminated", "missing", "pending"} {
		t.Run(outcome, func(t *testing.T) {
			instance := &models.Instance{ID: 12, UserID: 3}
			dep := desiredBrowserWorkerDeployment("clawbrowser-12", "lab", browserWorkerLabels(instance), "openclaw/user-3/instance-12/home/.openclaw/browser-worker", "workspace", 1, browserWorkerDefaults(12))
			dep.UID = "deployment-12"
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "clawbrowser-12-0", Namespace: "lab", UID: "pod-12", Finalizers: []string{nfsBrowserStopFinalizer}, OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", Name: "clawbrowser-12", UID: "deployment-12", Controller: boolptr(true)}}, Labels: browserWorkerLabels(instance)}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "browser"}}}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "browser", ContainerID: "containerd://old", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}

			kube := fake.NewSimpleClientset(desiredNFSBrowserStatefulSet(dep), pod)
			kube.PrependReactor("delete", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
				object, err := kube.Tracker().Get(corev1.SchemeGroupVersion.WithResource("pods"), "lab", "clawbrowser-12-0")
				if err != nil {
					t.Fatal(err)
				}
				current := object.(*corev1.Pod)
				now := metav1.Now()
				current.DeletionTimestamp = &now
				found := false
				for _, f := range current.Finalizers {
					if f == nfsBrowserStopFinalizer {
						found = true
					}
				}
				if !found {
					t.Fatal("deleted before adding durable finalizer")
				}
				if outcome == "missing" {
					_ = kube.Tracker().Delete(corev1.SchemeGroupVersion.WithResource("pods"), "lab", "clawbrowser-12-0")
				} else if outcome == "terminated" {
					current.Status.Phase = corev1.PodFailed
					current.Status.ContainerStatuses[0].State = corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ContainerID: "containerd://old", ExitCode: 0}}
					_ = kube.Tracker().Update(corev1.SchemeGroupVersion.WithResource("pods"), current, "lab")
				}
				return true, nil, nil
			})
			registry := &memoryBrowserRegistry{}
			admission, _ := json.Marshal(nfsBrowserStopEvidence{ControllerKind: "StatefulSet", Admitted: true, DeploymentUID: string(dep.UID), Pods: map[string]nfsBrowserPodEvidence{string(pod.UID): {Name: pod.Name}}})
			registry.raw, registry.revision = string(admission), 1
			worker := &BrowserWorkerService{repo: registry, client: &cmk8s.Client{Clientset: kube, RuntimeSandboxProfile: cmk8s.RuntimeNFSRootlessProfile}}
			t.Setenv("CLAWMANAGER_BROWSER_WORKER_NAMESPACE", "lab")
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			err := worker.stopNFSBrowserWriter(ctx, 12, false)
			got, _ := kube.AppsV1().StatefulSets("lab").Get(context.Background(), dep.Name, metav1.GetOptions{})
			var receipt nfsBrowserStopEvidence
			if json.Unmarshal([]byte(got.Annotations[nfsBrowserStopAnnotation]), &receipt) != nil {
				t.Fatal("missing durable writer evidence")
			}
			if outcome == "terminated" {
				if err != nil {
					t.Fatal(err)
				}
				var durable nfsBrowserStopEvidence
				if json.Unmarshal([]byte(registry.raw), &durable) != nil || !durable.Stopped || !durable.Pods["pod-12"].Complete {
					t.Fatal("positive proof missing from independent registry")
				}
				if !receipt.Pods["pod-12"].Complete {
					t.Fatal("termination was not durably recorded")
				}
				current, _ := kube.CoreV1().Pods("lab").Get(context.Background(), pod.Name, metav1.GetOptions{})
				if len(current.Finalizers) != 1 || current.Finalizers[0] != nfsBrowserStopFinalizer {
					t.Fatal("completed Pod lost its permanent name tombstone")
				}
			} else {
				if err == nil {
					t.Fatal("unknown writer was released")
				}
				if receipt.Pods["pod-12"].Complete {
					t.Fatal("absence/timeout fabricated termination")
				}
			}
		})
	}
}

func TestNFSBrowserReenableMustReconcilePendingStop(t *testing.T) {
	instance := &models.Instance{ID: 12, UserID: 3}
	dep := desiredBrowserWorkerDeployment("clawbrowser-12", "lab", browserWorkerLabels(instance), "old", "workspace", 0, browserWorkerDefaults(12))
	dep.UID = "deployment-12"
	if err := configureNFSBrowserWorker(dep, instance); err != nil {
		t.Fatal(err)
	}
	pending, _ := json.Marshal(nfsBrowserStopEvidence{ControllerKind: "StatefulSet", DeploymentUID: string(dep.UID), Pods: map[string]nfsBrowserPodEvidence{"missing-pod": {Name: "clawbrowser-12-0"}}})
	dep.Annotations = map[string]string{nfsBrowserStopAnnotation: string(pending)}
	kube := fake.NewSimpleClientset(desiredNFSBrowserStatefulSet(dep))
	kube.PrependReactor("delete", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		name := action.(ktesting.DeleteAction).GetName()
		obj, err := kube.Tracker().Get(corev1.SchemeGroupVersion.WithResource("pods"), "lab", name)
		if err != nil {
			return true, nil, err
		}
		pod := obj.(*corev1.Pod)
		now := metav1.Now()
		pod.DeletionTimestamp = &now
		return true, nil, kube.Tracker().Update(corev1.SchemeGroupVersion.WithResource("pods"), pod, "lab")
	})
	registry := &memoryBrowserRegistry{raw: `{"ControllerKind":"StatefulSet","Admitted":true,"DeploymentUID":"deployment-12","Pods":{}}`, revision: 1}
	worker := &BrowserWorkerService{repo: registry, client: &cmk8s.Client{Clientset: kube, RuntimeSandboxProfile: cmk8s.RuntimeNFSRootlessProfile}}
	t.Setenv("CLAWMANAGER_BROWSER_WORKER_NAMESPACE", "lab")
	if err := worker.stopNFSBrowserWriter(t.Context(), 12, true); err == nil {
		t.Fatal("re-enable bypassed pending writer stop")
	}
	got, _ := kube.AppsV1().StatefulSets("lab").Get(t.Context(), dep.Name, metav1.GetOptions{})
	if *got.Spec.Replicas != 0 || got.Annotations[nfsBrowserStopAnnotation] == "" {
		t.Fatal("pending stop was discarded")
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "clawbrowser-12-0", Namespace: "lab", UID: "missing-pod", Finalizers: []string{nfsBrowserStopFinalizer}, OwnerReferences: []metav1.OwnerReference{{Kind: "StatefulSet", Name: "clawbrowser-12", UID: "deployment-12", Controller: boolptr(true)}}, Labels: browserWorkerLabels(instance)}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "browser"}}}, Status: corev1.PodStatus{Phase: corev1.PodSucceeded, ContainerStatuses: []corev1.ContainerStatus{{Name: "browser", ContainerID: "containerd://prior", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ContainerID: "containerd://prior"}}}}}}
	if _, err := kube.CoreV1().Pods("lab").Create(t.Context(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := worker.stopNFSBrowserWriter(t.Context(), 12, true); err != nil {
		t.Fatal("positive prior proof failed reconciliation:", err)
	}
	var complete nfsBrowserStopEvidence
	if json.Unmarshal([]byte(registry.raw), &complete) != nil || !complete.Stopped {
		t.Fatal("completed stop not persisted")
	}

}

type memoryBrowserRegistry struct {
	memoryBrowserWorkerRepo
	raw      string
	revision int
}

func (r *memoryBrowserRegistry) LoadBrowserWriter(context.Context, int) (string, int, error) {
	return r.raw, r.revision, nil
}
func (r *memoryBrowserRegistry) StoreBrowserWriter(_ context.Context, _ int, revision int, raw string) error {
	if revision != r.revision {
		return fmt.Errorf("registry conflict")
	}
	r.raw = raw
	r.revision++
	return nil
}
func TestNFSBrowserAdmittedDisappearanceRemainsUnknownAfterRestart(t *testing.T) {
	repo := &memoryBrowserRegistry{raw: `{"Admitted":true,"DeploymentUID":"prior-deployment","Pods":{"prior-pod":{"Name":"prior"}}}`, revision: 1}
	kube := fake.NewSimpleClientset()
	t.Setenv("CLAWMANAGER_BROWSER_WORKER_NAMESPACE", "lab")
	for i := 0; i < 2; i++ {
		worker := &BrowserWorkerService{repo: repo, client: &cmk8s.Client{Clientset: kube, RuntimeSandboxProfile: cmk8s.RuntimeNFSRootlessProfile}}
		if err := worker.DeleteForInstance(t.Context(), 12); err == nil {
			t.Fatal("missing admitted writer treated as never created")
		}
	}
}

func TestNFSBrowserNeverEnabledCreatesExplicitRegistry(t *testing.T) {
	repo := &memoryBrowserRegistry{}
	worker := &BrowserWorkerService{repo: repo, client: &cmk8s.Client{Clientset: fake.NewSimpleClientset()}}
	if err := worker.stopNFSBrowserWriter(t.Context(), 12, false); err != nil {
		t.Fatal(err)
	}
	if repo.revision != 1 || repo.raw == "" {
		t.Fatal("never-admitted state not durable")
	}
}
