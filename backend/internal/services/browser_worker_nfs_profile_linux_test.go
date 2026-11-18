//go:build linux

package services

import (
	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	cmk8s "clawreef/internal/services/k8s"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestNFSBrowserProfileMigratesAtomicallyWithoutChangingPrivateData(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires credential switching")
	}
	base, err := os.MkdirTemp("/tmp", "cm-browser-grant-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	os.Chown(base, 10001, 10001)
	os.Chmod(base, 0711)
	workspace := RuntimeWorkspacePathWithRoot(base, RuntimeTypeOpenClaw, 3, 12)
	oldRoot := filepath.Join(workspace, "home", ".openclaw", "browser-worker")
	old := filepath.Join(oldRoot, "chromium")
	os.MkdirAll(old, 0755)
	for _, p := range []string{workspace, filepath.Join(workspace, "home"), filepath.Join(workspace, "home", ".openclaw"), oldRoot, old} {
		os.Chown(p, 200012, 200012)
		os.Chmod(p, 0700)
	}
	file := filepath.Join(old, "Preferences")
	os.WriteFile(file, []byte("saved-browser"), 0600)
	os.Chown(file, 200012, 200012)
	before, err := os.Stat(old)
	if err != nil {
		t.Fatal(err)
	}
	instance := &models.Instance{ID: 12, UserID: 3, RuntimeGeneration: 4, WorkspacePath: &workspace}
	ended, err := PrepareNFSBrowserProfile(t.Context(), base, instance)
	if err != nil || !ended {
		t.Fatalf("prepare: %v %v", ended, err)
	}
	target := filepath.Join(base, "browsers", "user-3", "instance-12", "profile")
	after, err := os.Stat(target)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("profile was not moved as the same directory inode", err)
	}
	info, err := os.Stat(filepath.Join(target, "Preferences"))
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	if info.Mode().Perm() != 0600 || st.Uid != 200012 || st.Gid != 200012 {
		t.Fatal("private browser metadata changed")
	}
	info, err = os.Stat(filepath.Dir(target))
	if err != nil || info.Mode().Perm() != 0771 || info.Sys().(*syscall.Stat_t).Uid != 10001 || info.Sys().(*syscall.Stat_t).Gid != 200012 {
		t.Fatalf("invalid new traversal parent: %+v %v", info, err)
	}
	info, _ = os.Stat(filepath.Join(workspace, "home"))
	if info.Mode().Perm() != 0700 {
		t.Fatal("private native home permissions broadened")
	}
	if _, err = PrepareNFSBrowserProfile(t.Context(), base, instance); err != nil {
		t.Fatal("idempotent profile preparation:", err)
	}
	os.Mkdir(old, 0700)
	os.Chown(old, 200012, 200012)
	if _, err = PrepareNFSBrowserProfile(t.Context(), base, instance); err == nil {
		t.Fatal("conflicting source/destination accepted")
	}
	os.Chown(filepath.Dir(workspace), 10001, 10001)
	writes := &workspaceWriteFake{}
	if err = EraseNFSInstanceWorkspace(t.Context(), base, RuntimeTypeOpenClaw, instance, writes); err != nil {
		t.Fatal("reset did not erase relocated browser profile:", err)
	}
	if _, err = os.Stat(filepath.Dir(target)); !os.IsNotExist(err) {
		t.Fatal("relocated browser directory survived reset")
	}
	if writes.active || writes.begins != 1 || writes.completes != 1 {
		t.Fatalf("relocated erase intent not settled: %+v", writes)
	}
}

func TestNFSBrowserProfileRealRootSquash(t *testing.T) {
	base := os.Getenv("CM_NFS_BROWSER_PROFILE_ROOT")
	if base == "" {
		t.Skip("requires isolated root-squash browser profile fixture")
	}
	workspace := RuntimeWorkspacePathWithRoot(base, RuntimeTypeOpenClaw, 3, 12)
	instance := &models.Instance{ID: 12, UserID: 3, RuntimeGeneration: 4, WorkspacePath: &workspace}
	if ended, err := PrepareNFSBrowserProfile(t.Context(), base, instance); err != nil || !ended {
		t.Fatalf("NFS atomic profile migration: ended=%v err=%v", ended, err)
	}
	if ended, err := PrepareNFSBrowserProfile(t.Context(), base, instance); err != nil || !ended {
		t.Fatalf("NFS profile migration retry: ended=%v err=%v", ended, err)
	}
	t.Log("root-squash atomic browser profile rename accepted; private native ancestors unchanged")
}

func TestNFSBrowserResourceAdmissionIntentSpansKubernetesApply(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires credential switching")
	}
	for _, uncertain := range []bool{false, true} {
		t.Run(fmt.Sprint(uncertain), func(t *testing.T) {
			base, err := os.MkdirTemp("/tmp", "cm-browser-resource-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(base)
			os.Chown(base, 10001, 10001)
			os.Chmod(base, 0711)
			workspace := RuntimeWorkspacePathWithRoot(base, RuntimeTypeOpenClaw, 3, 12)
			os.MkdirAll(workspace, 0755)
			instance := &models.Instance{ID: 12, UserID: 3, RuntimeGeneration: 4, WorkspacePath: &workspace, Status: "running", RuntimeSecurityProfile: RuntimeSecurityProfile}
			writes := &workspaceWriteFake{}
			kube := fake.NewSimpleClientset()
			kube.PrependReactor("delete", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
				name := action.(ktesting.DeleteAction).GetName()
				obj, err := kube.Tracker().Get(corev1.SchemeGroupVersion.WithResource("pods"), action.GetNamespace(), name)
				if err != nil {
					return true, nil, err
				}
				pod := obj.(*corev1.Pod)
				now := metav1.Now()
				pod.DeletionTimestamp = &now
				return true, nil, kube.Tracker().Update(corev1.SchemeGroupVersion.WithResource("pods"), pod, pod.Namespace)
			})
			registry := &memoryBrowserRegistry{}
			kube.PrependReactor("create", "*", func(action ktesting.Action) (bool, runtime.Object, error) {
				if !writes.active {
					t.Fatal("Kubernetes create escaped durable admission intent")
				}
				if action.GetResource().Resource == "statefulsets" {
					if !strings.Contains(registry.raw, `"Admitted":true`) {
						t.Fatal("Deployment creation preceded durable admission")
					}
					object := action.(ktesting.CreateAction).GetObject().(*appsv1.StatefulSet)
					object.UID = "created-deployment"
					if object.Spec.UpdateStrategy.Type != appsv1.OnDeleteStatefulSetStrategyType {
						t.Fatal("NFS controller must not autonomously roll writers")
					}
					if len(object.Spec.Template.Finalizers) == 0 {
						t.Fatal("Pod template omitted writer finalizer")
					}
				}
				if uncertain && action.GetResource().Resource == "statefulsets" {
					return true, nil, context.DeadlineExceeded
				}
				return false, nil, nil
			})
			worker := &BrowserWorkerService{repo: registry, client: &cmk8s.Client{Clientset: kube, WorkspaceRoot: base, WorkspacePVCClaimName: "workspace", RuntimeSandboxProfile: cmk8s.RuntimeNFSRootlessProfile}}
			ConfigureBrowserWorkspaceNFSIO(worker, "nfs-rootless-v1", base, writes, nfsTeamGuard{})
			if err = ConfigureBrowserRuntimeSecurity(worker, func(context.Context, int) (runtimeauth.Identity, error) {
				return runtimeauth.Identity{InstanceID: 12, UserID: 3, Generation: 4, PodUID: "runtime-pod"}, nil
			}); err != nil {
				t.Fatal(err)
			}
			err = worker.ensureResources(t.Context(), instance, browserWorkerDefaults(12))
			if uncertain {
				if !errors.Is(err, ErrWorkspaceNFSUncertain) || !writes.active || writes.completes != 0 {
					t.Fatalf("lost Kubernetes apply released admission: %v %+v", err, writes)
				}
			} else {
				if err != nil || writes.active || writes.begins != 1 || writes.completes != 1 {
					t.Fatalf("resource admission not settled after apply: %v %+v", err, writes)
				}
				beforeRevision := registry.revision
				if err = worker.ensureResources(t.Context(), instance, browserWorkerDefaults(12)); err != nil {
					t.Fatal("unchanged browser reconcile restarted admission:", err)
				}
				if registry.revision != beforeRevision {
					t.Fatal("unchanged browser created a new writer epoch")
				}
				changed := browserWorkerDefaults(12)
				changed.DisplayWidth = 1600
				if err = worker.ensureResources(t.Context(), instance, changed); err == nil {
					t.Fatal("template change replaced writer without positive stop")
				}
				var pending nfsBrowserStopEvidence
				if json.Unmarshal([]byte(registry.raw), &pending) != nil || !pending.Stopping {
					t.Fatal("unfinished stop was not durable")
				}
				if err = worker.ensureResources(t.Context(), instance, browserWorkerDefaults(12)); err == nil {
					t.Fatal("re-enable bypassed unfinished template stop")
				}
				pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: pending.ControllerName + "-0", Namespace: worker.namespace(), UID: "prior-pod", Labels: browserWorkerLabels(instance)}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "browser"}}}, Status: corev1.PodStatus{Phase: corev1.PodSucceeded, ContainerStatuses: []corev1.ContainerStatus{{Name: "browser", ContainerID: "containerd://prior", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ContainerID: "containerd://prior"}}}}}}
				pod.Annotations = map[string]string{nfsBrowserEpochAnnotation: pending.Epoch}
				pod.Finalizers = []string{nfsBrowserStopFinalizer}
				pod.OwnerReferences = []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "StatefulSet", Name: pending.ControllerName, UID: "created-deployment", Controller: boolptr(true)}}
				if err = kube.Tracker().Add(pod); err != nil {
					t.Fatal(err)
				}
				if err = worker.ensureResources(t.Context(), instance, changed); err != nil {
					t.Fatal("positive termination did not permit next admission:", err)
				}
				var next nfsBrowserStopEvidence
				if json.Unmarshal([]byte(registry.raw), &next) != nil || !next.Admitted || next.Stopping || next.Stopped {
					t.Fatal("next browser epoch did not open explicitly")
				}
				if err = worker.stopNFSBrowserWriter(t.Context(), instance.ID, false); err == nil {
					t.Fatal("old terminal Pod falsely proved new execution stopped")
				}

			}
		})
	}
}
