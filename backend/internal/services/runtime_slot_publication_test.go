package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"clawreef/internal/runtimeinventory"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
)

func TestProtectedSlotWaitsForExactPublishedAssignment(t *testing.T) {
	for _, scenario := range []string{"g1-timely", "unobserved", "healthy-missing", "old-version", "old-version-different-request", "uid-replaced", "caught-up-request-changed", "draining", "deleting", "pod-replaced", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			a, instances, kube, cm, pod := securityFixture(t)
			instances.instance.Status = "creating"
			generation := 2
			if scenario == "g1-timely" {
				generation = 1
				instances.instance.RuntimeGeneration = 1
				instances.instance.CreatedAt = time.Now().Add(-time.Minute)
			}
			a.instances = &securitySlotInstances{securityInstances: *instances}
			if err := kube.CoreV1().ConfigMaps("test").Delete(context.Background(), cm.Name, metav1.DeleteOptions{}); err != nil {
				t.Fatal(err)
			}
			kube.PrependReactor("create", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
				created := action.(ktesting.CreateAction).GetObject().(*corev1.ConfigMap)
				created.UID, created.ResourceVersion = "assignment-uid", "10"
				return false, nil, nil
			})
			ready := false
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			a.assignments = securityAssignmentFunc(func(context.Context, int) (*corev1.ConfigMap, []corev1.Pod, error) {
				if !ready && (scenario == "unobserved" || scenario == "g1-timely") {
					return nil, nil, unavailableInventoryError()
				}
				if !ready && scenario == "healthy-missing" {
					return nil, nil, runtimeinventory.ErrAssignmentNotFound
				}
				observed, err := kube.CoreV1().ConfigMaps("test").Get(context.Background(), cm.Name, metav1.GetOptions{})
				if err != nil {
					return nil, nil, err
				}
				observedPods := []corev1.Pod{*pod.DeepCopy()}
				if !ready {
					switch scenario {
					case "old-version":
						observed.ResourceVersion = "9"
					case "old-version-different-request":
						observed.ResourceVersion = "9"
						observed.Data["request"] = `{}`
					case "uid-replaced":
						observed.UID = "replacement"
					case "caught-up-request-changed":
						observed.Data["request"] = `{}`
					case "draining":
						observed.Data["desired_state"] = "draining"
					case "deleting":
						now := metav1.Now()
						observed.DeletionTimestamp = &now
					case "pod-replaced":
						observedPods[0].UID = "replacement"
					case "cancel":
						cancel()
					}
				}
				return observed, observedPods, nil
			})
			posts := 0
			client := NewSlotRuntimeAgentClientWithHTTPClient("control", kube, "test", &http.Client{Transport: slotRoundTripper(func(*http.Request) (*http.Response, error) {
				posts++
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"gateway_id":"gw-7-%d","gateway_port":20000}`, generation)))}, nil
			})}).(*slotRuntimeAgentClient)
			if err := ConfigureSlotRuntimeSecurity(client, a); err != nil {
				t.Fatal(err)
			}
			_, err := client.CreateGateway(ctx, "https://10.2.0.3:19090", RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 3, Generation: generation, AgentType: "opencode", GatewayPort: 20000})
			if err == nil || posts != 0 {
				t.Fatalf("Agent POST preceded exact cache confirmation: posts=%d err=%v", posts, err)
			}
			pending := scenario == "g1-timely" || scenario == "unobserved" || scenario == "healthy-missing" || scenario == "old-version" || scenario == "old-version-different-request"
			if errors.Is(err, errRuntimeGatewayStartPending) != pending {
				t.Fatalf("wrong pending classification: %v", err)
			}
			if pending {
				if !runtimeGatewayCreateOutcomeUncertain(err) {
					t.Fatal("scheduler would release published reservation")
				}
				ready = true
				if err := client.ReconcileAssignments(context.Background()); err != nil {
					t.Fatal(err)
				}
				if posts != 1 {
					t.Fatalf("did not resume exact assignment: posts=%d", posts)
				}
				actual, _ := kube.CoreV1().ConfigMaps("test").Get(context.Background(), cm.Name, metav1.GetOptions{})
				if actual.UID != "assignment-uid" || actual.Data["generation"] != fmt.Sprint(generation) || actual.Data["applied_generation"] != fmt.Sprint(generation) {
					t.Fatal("reconciliation replaced assignment")
				}
			}
		})
	}
}

func TestPublicationPendingPreservesSQLReservationAndTimeoutOrigin(t *testing.T) {
	f := newPendingSlotReplayFixture(t)
	f.kube.PrependReactor("create", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
		cm := action.(ktesting.CreateAction).GetObject().(*corev1.ConfigMap)
		cm.UID, cm.ResourceVersion = "assignment-uid", "10"
		return false, nil, nil
	})
	f.seed(t, 1, "stopped")
	pod, err := f.kube.CoreV1().Pods("runtime").Get(context.Background(), "pool", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pod.Labels["clawmanager.io/runtime-security"] = RuntimeSecurityProfile
	pod.Status.Phase = corev1.PodRunning
	if _, err = f.kube.CoreV1().Pods("runtime").Update(context.Background(), pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	f.instance.RuntimeSecurityProfile = RuntimeSecurityProfile
	instances := &securitySlotInstances{securityInstances: securityInstances{instance: &f.instance}}
	a, err := NewRuntimeSecurityAuthority(f.kube, "runtime", instances, []byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	ready := false
	a.assignments = securityAssignmentFunc(func(ctx context.Context, _ int) (*corev1.ConfigMap, []corev1.Pod, error) {
		if !ready {
			return nil, nil, unavailableInventoryError()
		}
		cm, err := f.kube.CoreV1().ConfigMaps("runtime").Get(ctx, "lite-instance-7", metav1.GetOptions{})
		return cm, []corev1.Pod{*pod}, err
	})
	if err := ConfigureSlotRuntimeSecurity(f.client, a); err != nil {
		t.Fatal(err)
	}
	origin := time.Now().UTC().Add(-time.Minute)
	f.binding.UpdatedAt = origin
	start := &runtimeGatewayStart{endpoint: *f.pod.AgentEndpoint, workspacePath: f.binding.WorkspacePath, environment: map[string]string{"BUSINESS_TOKEN": "original-issued-once"}, uid: 200007, gid: 200007, reservedBinding: f.binding}
	err = f.scheduler.createGatewayOnPod(context.Background(), f.instance, RuntimeTypeOpenClaw, *f.pod, start)
	if !errors.Is(err, errRuntimeGatewayStartPending) || len(f.requests) != 0 {
		t.Fatalf("pending publication was terminal or called Agent: %v", err)
	}
	for range 3 {
		if errs := f.scheduler.reconcileCreatingInstance(context.Background(), f.instance); len(errs) != 0 {
			t.Fatal(errs)
		}
		if err := f.client.ReconcileAssignments(context.Background()); !errors.Is(err, errRuntimeGatewayStartPending) {
			t.Fatalf("missing observation did not remain pending: %v", err)
		}
	}
	f.assertReservationUnchanged(t)
	if !f.binding.UpdatedAt.Equal(origin) || f.envCalls != 0 || len(f.requests) != 0 {
		t.Fatal("pending observation renewed timeout or regenerated inputs")
	}
	ready = true
	if err := f.client.ReconcileAssignments(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.assertReservationUnchanged(t)
	if len(f.requests) != 1 || f.requests[0].GatewayPort != f.binding.GatewayPort || f.requests[0].Environment["BUSINESS_TOKEN"] != "original-issued-once" {
		t.Fatal("did not resume original reservation and Secret")
	}
	if !f.binding.UpdatedAt.Equal(origin) {
		t.Fatal("assignment replay renewed timeout")
	}
}

func TestPublicationKnownInstanceStopIsHardBeforeCacheObservation(t *testing.T) {
	for _, status := range []string{"stopping", "stopped", "deleting"} {
		t.Run(status, func(t *testing.T) {
			a, instances, _, cm, _ := securityFixture(t)
			cm.UID, cm.ResourceVersion = "assignment-uid", "10"
			instances.instance.Status = status
			reads := 0
			a.assignments = securityAssignmentFunc(func(context.Context, int) (*corev1.ConfigMap, []corev1.Pod, error) {
				reads++
				return nil, nil, unavailableInventoryError()
			})
			client := &slotRuntimeAgentClient{security: a, namespace: "test"}
			err := client.confirmPublishedSecurity(context.Background(), cm, RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 3, Generation: 2, AgentType: "opencode", PodUID: "pod-7", SecurityProfile: RuntimeSecurityProfile})
			if err == nil || errors.Is(err, errRuntimeGatewayStartPending) || reads != 0 {
				t.Fatalf("known stop became cache pending: reads=%d err=%v", reads, err)
			}
		})
	}
}
