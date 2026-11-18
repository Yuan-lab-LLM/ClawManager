package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/services/k8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func validationStopFixture(t *testing.T) (*slotRuntimeAgentClient, *fake.Clientset, k8s.RuntimePoolValidationScope, *corev1.Pod, *atomic.Int32) {
	t.Helper()
	ctx := context.Background()
	image := "repo@sha256:" + strings.Repeat("a", 64)
	source := k8s.BuildRuntimeDeployment(k8s.RuntimeDeploymentSpec{Name: "source", Namespace: "runtime", RuntimeType: "openclaw", Image: image, Replicas: 1, SlotCount: 2, SandboxProfile: k8s.RuntimeNFSRootlessProfile, NetworkIsolation: true, AgentTokenSecretName: "agent", WorkspacePVCClaimName: "business"})
	source.UID = "source-uid"
	kube := fake.NewSimpleClientset(source)
	service := k8s.NewRuntimeDeploymentService(kube).(k8s.RuntimePoolValidationService)
	if err := service.EnsurePoolValidation(ctx, k8s.RuntimeRolloutTarget{Namespace: "runtime", Name: "source", UID: "source-uid"}, "validation", image, "43"); err != nil {
		t.Fatal(err)
	}
	d, _ := kube.AppsV1().Deployments("runtime").Get(ctx, "validation", metav1.GetOptions{})
	d.UID, d.ResourceVersion = "validation-uid", "17"
	kube.AppsV1().Deployments("runtime").Update(ctx, d, metav1.UpdateOptions{})
	yes := true
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "validation-rs", Namespace: "runtime", UID: "rs-uid", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: d.Name, UID: d.UID, Controller: &yes}}}, Spec: appsv1.ReplicaSetSpec{Template: *d.Spec.Template.DeepCopy()}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "validation-pod", Namespace: "runtime", UID: "probe-uid", Labels: d.Spec.Template.Labels, Annotations: d.Spec.Template.Annotations, OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: &yes}}}, Spec: *d.Spec.Template.Spec.DeepCopy(), Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.2"}}
	kube.AppsV1().ReplicaSets("runtime").Create(ctx, rs, metav1.CreateOptions{})
	kube.CoreV1().Pods("runtime").Create(ctx, pod, metav1.CreateOptions{})
	calls := &atomic.Int32{}
	client := NewSlotRuntimeAgentClientWithHTTPClient(strings.Repeat("c", 32), kube, "runtime", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		// A native worker rejects the historical DELETE's absent C1 intent.
		return &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("invalid container stop intent"))}, nil
	})}).(*slotRuntimeAgentClient)
	scope := k8s.RuntimePoolValidationScope{Namespace: "runtime", DeploymentName: d.Name, DeploymentUID: string(d.UID), SourceName: source.Name, SourceUID: string(source.UID), Operation: "43", PodUID: string(pod.UID), Endpoint: "https://10.0.0.2:19090", ImageRef: image, WorkspaceRoot: "/workspaces"}
	return client, kube, scope, pod, calls
}

func TestNFSValidationStopRequiresPhysicalPodRemoval(t *testing.T) {
	client, kube, scope, pod, calls := validationStopFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	scaled := make(chan struct{}, 1)
	kube.PrependReactor("patch", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
		var operations []map[string]any
		if err := json.Unmarshal(action.(ktesting.PatchAction).GetPatch(), &operations); err != nil {
			t.Error(err)
		}
		if len(operations) != 3 || operations[0]["path"] != "/metadata/uid" || operations[0]["value"] != scope.DeploymentUID || operations[1]["path"] != "/metadata/resourceVersion" || operations[1]["value"] != "17" || operations[2]["path"] != "/spec/replicas" || operations[2]["value"] != float64(0) {
			t.Error("validation stop lacks exact controller CAS", operations)
		}
		scaled <- struct{}{}
		return false, nil, nil
	})
	done := make(chan error, 1)
	go func() {
		done <- client.DeletePoolValidationGateway(ctx, scope, fmt.Sprintf("gw-%d-1", poolProbeInstanceID))
	}()
	select {
	case <-scaled:
	case err := <-done:
		t.Fatalf("native stop used legacy teardown before physical stop: %v (HTTP calls=%d)", err, calls.Load())
	case <-ctx.Done():
		t.Fatal("native stop never submitted scoped physical shutdown")
	}
	now := metav1.Now()
	pod.DeletionTimestamp = &now
	pod.Finalizers = []string{"fixture-physical-exit"}
	kube.CoreV1().Pods("runtime").Update(ctx, pod, metav1.UpdateOptions{})
	select {
	case err := <-done:
		t.Fatal("terminating Pod was treated as physical absence", err)
	case <-time.After(80 * time.Millisecond):
	}
	if err := kube.CoreV1().Pods("runtime").Delete(ctx, pod.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("NFS physical stop sent synthetic gateway teardown", calls.Load())
	}
	source, _ := kube.AppsV1().Deployments("runtime").Get(ctx, "source", metav1.GetOptions{})
	if *source.Spec.Replicas != 1 {
		t.Fatal("validation stop touched business source")
	}
}

func TestNFSValidationStopRejectsBusinessAndConcurrentControllerChanges(t *testing.T) {
	for _, change := range []string{"business-pod", "serving-pool", "wrong-uid", "uid-race", "version-race"} {
		t.Run(change, func(t *testing.T) {
			client, kube, scope, pod, calls := validationStopFixture(t)
			ctx := context.Background()
			source, _ := kube.AppsV1().Deployments("runtime").Get(ctx, "source", metav1.GetOptions{})
			switch change {
			case "business-pod":
				pod.Spec.Volumes[0].VolumeSource = corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "business"}}
				kube.CoreV1().Pods("runtime").Update(ctx, pod, metav1.UpdateOptions{})
			case "serving-pool":
				scope.DeploymentName, scope.DeploymentUID = source.Name, string(source.UID)
			case "wrong-uid":
				scope.DeploymentUID = "other"
			case "uid-race", "version-race":
				kube.PrependReactor("patch", "deployments", func(action ktesting.Action) (bool, runtime.Object, error) {
					d, _ := kube.Tracker().Get(appsv1.SchemeGroupVersion.WithResource("deployments"), "runtime", "validation")
					changed := d.(*appsv1.Deployment).DeepCopy()
					if change == "uid-race" {
						changed.UID = "replacement"
					} else {
						changed.ResourceVersion = "18"
					}
					kube.Tracker().Update(appsv1.SchemeGroupVersion.WithResource("deployments"), changed, "runtime")
					return false, nil, nil
				})
			}
			if err := client.DeletePoolValidationGateway(ctx, scope, fmt.Sprintf("gw-%d-1", poolProbeInstanceID)); err == nil {
				t.Fatal("unsafe validation stop succeeded")
			}
			unchanged, _ := kube.AppsV1().Deployments("runtime").Get(ctx, source.Name, metav1.GetOptions{})
			if !reflect.DeepEqual(source, unchanged) || calls.Load() != 0 {
				t.Fatal("rejected stop changed business source or reached worker HTTP")
			}
		})
	}
}
