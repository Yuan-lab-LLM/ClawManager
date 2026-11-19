package services

import (
	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	cmk8s "clawreef/internal/services/k8s"
	"context"
	"encoding/json"
	"errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"strings"
	"testing"
)

func TestStoppedRecoveryAcceptsOnlyInitializedNeverAdmittedBrowserRegistry(t *testing.T) {
	for _, change := range []string{"empty-initialized", "admitted", "stopping", "stopped", "controller-name", "controller-uid", "controller-kind", "epoch", "pods", "retired", "missing-pods", "unknown-field", "missing-fields", "null-boolean", "lowercase-key", "retired-array", "duplicate-key", "bad-json", "later-empty", "raw-no-revision", "orphan", "config", "team"} {
		t.Run(change, func(t *testing.T) {
			instances := newFakeRuntimeInstanceRepo()
			instances.byID[155] = &models.Instance{ID: 155, UserID: 1, Type: "opencode", RuntimeType: "gateway", InstanceMode: "lite", RuntimeGeneration: 8, WorkspacePath: stringPtrOrNil("/workspaces/opencode/user-1/instance-155")}
			evidence := nfsBrowserStopEvidence{Pods: map[string]nfsBrowserPodEvidence{}}
			switch change {
			case "admitted":
				evidence.Admitted = true
			case "stopping":
				evidence.Stopping = true
			case "stopped":
				evidence.Stopped = true
			case "controller-name":
				evidence.ControllerName = "prior"
			case "controller-uid":
				evidence.DeploymentUID = "prior"
			case "controller-kind":
				evidence.ControllerKind = "StatefulSet"
			case "epoch":
				evidence.Epoch = "prior"
			case "pods":
				evidence.Pods["prior"] = nfsBrowserPodEvidence{Name: "prior", Complete: true}
			case "retired":
				evidence.Retired = []nfsBrowserRetiredEpoch{{ControllerName: "prior"}}
			case "missing-pods":
				evidence.Pods = nil
			}
			data, err := json.Marshal(evidence)
			if err != nil {
				t.Fatal(err)
			}
			repo := &recoveryBrowserRepo{memoryBrowserRegistry: memoryBrowserRegistry{raw: string(data), revision: 1}}
			if change == "unknown-field" {
				repo.raw = `{"Pods":{},"UnknownWriter":true}`
			}
			if change == "missing-fields" {
				repo.raw = `{"Pods":{}}`
			}
			if change == "null-boolean" {
				repo.raw = strings.Replace(repo.raw, `"Admitted":false`, `"Admitted":null`, 1)
			}
			if change == "lowercase-key" {
				repo.raw = strings.Replace(repo.raw, `"Admitted":false`, `"admitted":false`, 1)
			}
			if change == "retired-array" {
				repo.raw = strings.Replace(repo.raw, `"Retired":null`, `"Retired":[]`, 1)
			}
			if change == "duplicate-key" {
				repo.raw = strings.Replace(repo.raw, `"Admitted":false`, `"Admitted":true,"Admitted":false`, 1)
			}
			if change == "bad-json" {
				repo.raw = "invalid"
			}
			if change == "later-empty" {
				repo.revision = 2
			}
			if change == "raw-no-revision" {
				repo.revision = 0
			}
			if change == "config" {
				repo.config = &models.InstanceBrowserWorker{InstanceID: 155, Enabled: false}
			}
			if change == "team" {
				repo.isolationErr = errors.New("shared reference")
			}
			kube := fake.NewSimpleClientset()
			if change == "orphan" {
				_, err = kube.CoreV1().Pods("other").Create(t.Context(), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "clawbrowser-155-orphan", Namespace: "other"}}, metav1.CreateOptions{})
				if err != nil {
					t.Fatal(err)
				}
			}
			service := NewBrowserWorkerService(repo, instances, &cmk8s.Client{Clientset: kube})
			before, revision := repo.raw, repo.revision
			kube.ClearActions()
			err = service.VerifyNoBrowserWriterHistory(t.Context(), runtimeauth.Identity{InstanceID: 155, UserID: 1, Generation: 7, PodUID: "old"})
			if (err == nil) != (change == "empty-initialized") {
				t.Fatalf("change %s: %v", change, err)
			}
			if repo.raw != before || repo.revision != revision {
				t.Fatal("read-only proof altered registry")
			}
			for _, a := range kube.Actions() {
				if a.GetVerb() != "get" && a.GetVerb() != "list" {
					t.Fatal("proof mutated Kubernetes")
				}
			}
		})
	}
}

type recoveryBrowserRepo struct {
	memoryBrowserRegistry
	isolationErr error
}

func (r *recoveryBrowserRepo) VerifyRecoveryWorkspaceIsolation(context.Context, int) error {
	return r.isolationErr
}

func TestStoppedRecoveryRejectsExternalBrowserHistoryAndOrphans(t *testing.T) {
	for _, change := range []string{"none", "config-disabled", "history", "missing-registry", "openclaw", "orphan-pod", "deleting-pod", "unlabelled-mount", "controller", "retained-secret", "team-reference"} {
		t.Run(change, func(t *testing.T) {
			instances := newFakeRuntimeInstanceRepo()
			instances.byID[155] = &models.Instance{ID: 155, UserID: 1, Type: "opencode", RuntimeType: "gateway", InstanceMode: "lite", RuntimeGeneration: 8, WorkspacePath: stringPtrOrNil("/workspaces/opencode/user-1/instance-155")}
			repo := &recoveryBrowserRepo{}
			kube := fake.NewSimpleClientset()
			service := NewBrowserWorkerService(repo, instances, &cmk8s.Client{Clientset: kube})
			switch change {
			case "team-reference":
				repo.isolationErr = errors.New("shared reference")
			case "config-disabled":
				repo.config = &models.InstanceBrowserWorker{InstanceID: 155, Enabled: false}
			case "history":
				repo.raw = "old"
				repo.revision = 1
			case "missing-registry":
				service.repo = &memoryBrowserWorkerRepo{}
			case "openclaw":
				instances.byID[155].Type = "openclaw"
			case "orphan-pod", "deleting-pod", "unlabelled-mount":
				pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "clawbrowser-155-orphan", Namespace: "other"}}
				if change == "deleting-pod" {
					now := metav1.Now()
					pod.DeletionTimestamp = &now
				}
				if change == "unlabelled-mount" {
					pod.Name = "orphan"
					pod.Spec.Containers = []corev1.Container{{Name: "unknown", VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: "/data", SubPath: "opencode/user-1/instance-155/home/browser"}}}}
				}
				_, _ = kube.CoreV1().Pods("other").Create(t.Context(), pod, metav1.CreateOptions{})
			case "controller":
				_, _ = kube.AppsV1().Deployments("other").Create(t.Context(), &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "clawbrowser-155", Namespace: "other"}}, metav1.CreateOptions{})
			case "retained-secret":
				_, _ = kube.CoreV1().Secrets("other").Create(t.Context(), &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "clawbrowser-155", Namespace: "other"}}, metav1.CreateOptions{})
			}
			err := service.VerifyNoBrowserWriterHistory(t.Context(), runtimeauth.Identity{InstanceID: 155, UserID: 1, Generation: 7, PodUID: "old"})
			if (err == nil) != (change == "none") {
				t.Fatalf("change %s: %v", change, err)
			}
			for _, action := range kube.Actions() {
				if action.GetVerb() != "get" && action.GetVerb() != "list" && action.GetVerb() != "create" {
					t.Fatal("absence proof mutated Kubernetes")
				}
			}
			if repo.revision != 0 && change != "history" {
				t.Fatal("absence proof wrote admission history")
			}
		})
	}
}

func TestSlotResetExternalBrowserCleanup(t *testing.T) {
	for _, change := range []string{"exact", "advanced-clean", "hermes-clean", "deepseek-harness-clean", "opencode-empty-history", "hermes-empty-history", "deepseek-harness-empty-history", "advanced-config", "advanced-history", "advanced-policy", "same-generation-unbound", "wrong-owner", "wrong-workspace", "future-source"} {
		t.Run(change, func(t *testing.T) {
			const id = 155
			instances := newFakeRuntimeInstanceRepo()
			instance := &models.Instance{ID: id, UserID: 1, Type: "openclaw", RuntimeType: "gateway", InstanceMode: "lite", RuntimeGeneration: 8, WorkspacePath: stringPtrOrNil("/workspaces/openclaw/user-1/instance-155")}
			instances.byID[id] = instance
			repo := &recoveryBrowserRepo{}
			kube := fake.NewSimpleClientset()
			service := NewBrowserWorkerService(repo, instances, &cmk8s.Client{Clientset: kube})
			identity := runtimeauth.Identity{InstanceID: id, UserID: 1, Generation: 7, PodUID: "source"}
			bound := true
			switch change {
			case "exact":
				identity.Generation = 8
				repo.config = &models.InstanceBrowserWorker{InstanceID: id, Enabled: true}
			case "hermes-clean", "deepseek-harness-clean":
				instance.Type = change[:len(change)-len("-clean")]
				instance.WorkspacePath = stringPtrOrNil("/workspaces/" + instance.Type + "/user-1/instance-155")
			case "advanced-config", "same-generation-unbound":
				repo.config = &models.InstanceBrowserWorker{InstanceID: id, Enabled: true}
				if change == "same-generation-unbound" {
					identity.Generation = 8
					bound = false
				}
			case "advanced-history":
				repo.revision = 1
				repo.raw = "existing"
			case "opencode-empty-history", "hermes-empty-history", "deepseek-harness-empty-history":
				instance.Type = strings.TrimSuffix(change, "-empty-history")
				instance.WorkspacePath = stringPtrOrNil("/workspaces/" + instance.Type + "/user-1/instance-155")
				data, _ := json.Marshal(nfsBrowserStopEvidence{Pods: map[string]nfsBrowserPodEvidence{}})
				repo.revision = 1
				repo.raw = string(data)
			case "advanced-policy":
				_, err := kube.NetworkingV1().NetworkPolicies("other").Create(t.Context(), &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "clawbrowser-155", Namespace: "other"}}, metav1.CreateOptions{})
				if err != nil {
					t.Fatal(err)
				}
			case "wrong-owner":
				identity.UserID = 2
			case "wrong-workspace":
				instance.WorkspacePath = stringPtrOrNil("/workspaces/shared")
			case "future-source":
				identity.Generation = 9
			}
			kube.ClearActions()
			err := service.CleanupSlotResetExternalWriter(t.Context(), identity, bound)
			if (err == nil) != (change == "exact" || change == "advanced-clean" || change == "hermes-clean" || change == "deepseek-harness-clean" || change == "opencode-empty-history") {
				t.Fatalf("cleanup: %v", err)
			}
			mutated := false
			for _, action := range kube.Actions() {
				if action.GetVerb() != "list" && action.GetVerb() != "get" {
					mutated = true
				}
			}
			if mutated != (change == "exact") {
				t.Fatalf("unexpected mutation: %v", kube.Actions())
			}
		})
	}
}
