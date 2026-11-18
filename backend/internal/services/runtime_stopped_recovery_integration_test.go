package services

import (
	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	"clawreef/internal/services/k8s"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/dump"
	krand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRecoverStoppedSourceJoinsProofsWithoutLaunchingSuccessor(t *testing.T) {
	for _, change := range []string{"none", "fence-failed", "external-failed", "running-writer", "nonce-mismatch", "assignment-changed", "successor-published", "observer-changed", "target-changed", "foreign-storage", "absence-unavailable", "late-external-writer"} {
		t.Run(change, func(t *testing.T) {
			ctx := t.Context()
			yes := true
			before := metav1.NewTime(time.Now().Add(-time.Hour))
			now := time.Now()
			str := stringPtrOrNil
			source := &models.RuntimePod{ID: 151, Namespace: "runtime", PodName: "pool-abc-old", PodUID: str("old"), PodIP: str("10.1.0.2"), NodeName: str("node"), DeploymentName: "pool", ImageRef: "image@sha256:abc", RuntimeType: "opencode", AgentEndpoint: str("https://10.1.0.2:19090"), CreatedAt: now.Add(-time.Minute)}
			target := &models.RuntimePod{ID: 153, Namespace: "runtime", PodName: "pool-new", PodUID: str("new"), PodIP: str("10.1.0.3"), NodeName: str("node"), DeploymentName: "pool", RuntimeType: "opencode", AgentEndpoint: str("https://10.1.0.3:19090"), State: "ready", LastSeenAt: &now}
			observerCopy := *target
			observer := &observerCopy
			observer.ID = 154
			observer.PodName = "pool-observer"
			observer.PodUID = str("observer")
			observer.PodIP = str("10.1.0.4")
			observer.AgentEndpoint = str("https://10.1.0.4:19090")
			spec := corev1.PodSpec{NodeName: "node", Volumes: []corev1.Volume{{Name: "workspaces", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}}}, Containers: []corev1.Container{{Name: "slot-0", Image: source.ImageRef, Env: []corev1.EnvVar{{Name: "RUNTIME_GATEWAY_PORT_START", Value: "20000"}, {Name: "RUNTIME_WORKSPACE_ROOT", Value: "/workspaces"}}, VolumeMounts: []corev1.VolumeMount{{Name: "workspaces", MountPath: "/workspaces"}}}}}
			rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "pool-abc", Namespace: "runtime", UID: "rs", CreationTimestamp: before, Labels: map[string]string{"pod-template-hash": "abc"}, OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: "pool", UID: "dep", Controller: &yes}}}, Spec: appsv1.ReplicaSetSpec{Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"pod-template-hash": "abc"}}, Spec: spec}}}
			original := rs.Spec.Template.DeepCopy()
			delete(original.Labels, "pod-template-hash")
			hasher := fnv.New32a()
			fmt.Fprint(hasher, dump.ForHash(*original))
			hash := krand.SafeEncodeString(fmt.Sprint(hasher.Sum32()))
			rs.Name = "pool-" + hash
			rs.Labels["pod-template-hash"] = hash
			rs.Spec.Template.Labels["pod-template-hash"] = hash
			source.PodName = rs.Name + "-old"
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pool-new", Namespace: "runtime", UID: "new", Labels: map[string]string{"clawmanager.io/runtime-protocol": "container-slots-v1"}}, Spec: spec, Status: corev1.PodStatus{PodIP: "10.1.0.3", Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}
			claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "runtime", UID: "claim", CreationTimestamp: before}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "pv"}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
			pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv", UID: "pv-uid", CreationTimestamp: before}, Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Namespace: "runtime", Name: "data", UID: "claim"}, PersistentVolumeSource: corev1.PersistentVolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/data"}}}}
			observerPod := pod.DeepCopy()
			observerPod.Name = observer.PodName
			observerPod.UID = "observer"
			observerPod.Status.PodIP = "10.1.0.4"
			coordinator := *observerPod.Spec.Containers[0].DeepCopy()
			coordinator.Name = "runtime"
			observerPod.Spec.Containers = append(observerPod.Spec.Containers, coordinator)
			if change == "foreign-storage" {
				observerPod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName = "foreign"
			}
			kube := fake.NewSimpleClientset(rs, pod, observerPod, claim, pv, &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "runtime", UID: "dep"}}, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node", UID: "node-uid", CreationTimestamp: before}, Spec: corev1.NodeSpec{PodCIDR: "10.1.0.0/24"}})
			workspace := "/workspaces/opencode/user-1/instance-155"
			request := RuntimeAgentCreateGatewayRequest{InstanceID: 155, UserID: 1, Generation: 7, PodUID: "old", AgentType: "opencode", GatewayPort: 20000, WorkspacePath: workspace}
			raw, _ := json.Marshal(request)
			assignments := k8s.NewRuntimeAssignmentService(kube, "runtime")
			if err := assignments.EnsureDesired(ctx, k8s.RuntimeAssignment{InstanceID: 155, UserID: 1, Generation: 7, PodUID: "old", RuntimeType: "opencode", Endpoint: *source.AgentEndpoint, GatewayID: "gw-155-7", Request: raw}); err != nil {
				t.Fatal(err)
			}
			cm, _ := kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-155", metav1.GetOptions{})
			cm.ResourceVersion = "17"
			_, _ = kube.CoreV1().ConfigMaps("runtime").Update(ctx, cm, metav1.UpdateOptions{})
			calls := 0
			fenced := false
			external := false
			transport := slotRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path != "/v1/source-writer/stopped-proof" || r.Header.Get("X-ClawManager-Control-Token") != "control" || !fenced || !external {
					t.Fatal("proof request missing auth or earlier fences")
				}
				var scope stoppedSourceProofScope
				if err := json.NewDecoder(r.Body).Decode(&scope); err != nil {
					t.Fatal(err)
				}
				response := stoppedSourceProof{stoppedSourceProofScope: scope, Confirmed: true, ObserverPodUID: "observer", SourceRecordDigest: strings.Repeat("b", 64)}
				switch change {
				case "running-writer":
					return &http.Response{StatusCode: 409, Body: io.NopCloser(strings.NewReader(`{"error":"writer running"}`)), Header: http.Header{}}, nil
				case "nonce-mismatch":
					response.Nonce = strings.Repeat("c", 64)
				case "assignment-changed":
					cm.Data["container_stop_intent"] = "pending"
					_, _ = kube.CoreV1().ConfigMaps("runtime").Update(ctx, cm, metav1.UpdateOptions{})
				case "successor-published":
					_, _ = kube.CoreV1().Secrets("runtime").Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "lite-instance-155-g8"}}, metav1.CreateOptions{})
				case "observer-changed":
					observerPod.UID = "replacement"
					_, _ = kube.CoreV1().Pods("runtime").Update(ctx, observerPod, metav1.UpdateOptions{})
				case "target-changed":
					pod.UID = "replacement"
					_, _ = kube.CoreV1().Pods("runtime").Update(ctx, pod, metav1.UpdateOptions{})
				}
				body, _ := json.Marshal(response)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: http.Header{}}, nil
			})
			client := NewSlotRuntimeAgentClientWithHTTPClient("control", kube, "runtime", &http.Client{Transport: transport}).(*slotRuntimeAgentClient)
			client.recoveryStorageProof = func(context.Context, kubernetes.Interface, models.RuntimePod, models.RuntimePod, string, int) (k8s.StoppedSourceStorage, error) {
				if change == "foreign-storage" {
					return k8s.StoppedSourceStorage{}, errors.New("foreign storage")
				}
				return k8s.StoppedSourceStorage{Identity: "verified-storage", Slot: "slot-0"}, nil
			}
			absenceCalls := 0
			if change != "absence-unavailable" {
				client.workspaceExternalAbsence = func(context.Context, runtimeauth.Identity) error {
					absenceCalls++
					if change == "late-external-writer" && absenceCalls > 1 {
						return errors.New("external writer appeared")
					}
					return nil
				}
			}
			client.workspaceWriteFence = func(context.Context, runtimeauth.Identity) error {
				if change == "fence-failed" {
					return errors.New("fence failed")
				}
				fenced = true
				return nil
			}
			client.workspaceExternalStop = func(context.Context, runtimeauth.Identity) error {
				if change == "external-failed" {
					return errors.New("external failed")
				}
				external = true
				return nil
			}
			instances := newFakeRuntimeInstanceRepo()
			instances.byID[155] = &models.Instance{ID: 155, UserID: 1, Type: "opencode", RuntimeType: "gateway", InstanceMode: "lite", Status: "creating", RuntimeGeneration: 8}
			bindings := newFakeRuntimeBindingRepo()
			bindings.bindings[155] = &models.InstanceRuntimeBinding{InstanceID: 155, RuntimeType: "opencode", Generation: 8, State: "creating", GatewayID: "pending-155-8", RuntimePodID: 153, WorkspacePath: workspace, GatewayPort: 20000}
			pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{151: source, 153: target, 154: observer}}
			scheduler := NewRuntimeScheduler(instances, pods, bindings, nil, client, nil, nil, nil, time.Second, WithRuntimeSchedulerWorkspaceRoot("/workspaces"))
			err := scheduler.RecoverStoppedSource(ctx, 155, StoppedSourceRecoveryRequest{SuccessorGeneration: 8, SourcePodUID: "old", ObserverRuntimePodID: 154}, 1)
			cm, _ = kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-155", metav1.GetOptions{})
			if change == "none" {
				if err != nil || cm.Data["desired_state"] != "stopped" || cm.Annotations[k8s.StoppedSourceRecoveryAnnotation] == "" {
					t.Fatalf("valid recovery failed: %v", err)
				}
			} else {
				if err == nil || cm.Data["desired_state"] == "stopped" {
					t.Fatalf("unsafe recovery accepted: %v", err)
				}
			}
			if instances.byID[155].RuntimeGeneration != 8 || len(instances.runtimeStates) != 0 || bindings.bindings[155].GatewayID != "pending-155-8" || calls > 1 {
				t.Fatal("recovery modified SQL or launched successor")
			}
		})
	}
}
