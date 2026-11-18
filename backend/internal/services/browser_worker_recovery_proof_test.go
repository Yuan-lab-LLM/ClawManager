package services

import (
	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	cmk8s "clawreef/internal/services/k8s"
	"context"
	"errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

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
