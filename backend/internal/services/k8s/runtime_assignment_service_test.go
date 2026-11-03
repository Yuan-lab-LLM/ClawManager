package k8s

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func assignmentFixture() RuntimeAssignment {
	return RuntimeAssignment{InstanceID: 7, UserID: 4, Generation: 3, RuntimeType: "openclaw",
		Endpoint: "http://pod-a:19091", GatewayID: "gw-7-3",
		Request:     json.RawMessage(`{"InstanceID":7,"Environment":{"API_KEY":"must-not-publish"},"nested":{"eNv":{"TOKEN":"must-not-publish"},"keep":true}}`),
		Environment: map[string]string{"API_KEY": "business-secret", "EMPTY": ""}}
}

func assignmentCM(t *testing.T, client *fake.Clientset) *corev1.ConfigMap {
	t.Helper()
	cm, err := client.CoreV1().ConfigMaps("tenant").Get(context.Background(), "lite-instance-7", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return cm
}

func TestRuntimeAssignmentSeparatesSecretsAndCanonicalizesRetries(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	service := NewRuntimeAssignmentService(client, "tenant")
	a := assignmentFixture()
	if err := service.EnsureDesired(ctx, a); err != nil {
		t.Fatal(err)
	}
	cm := assignmentCM(t, client)
	encoded, _ := json.Marshal(cm)
	for _, forbidden := range []string{"must-not-publish", "business-secret", "Environment", "eNv"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("ConfigMap leaks %q: %s", forbidden, encoded)
		}
	}
	if cm.Data["request"] != `{"InstanceID":7,"nested":{"keep":true}}` || cm.Data["desired_state"] != "running" {
		t.Fatalf("unexpected desired assignment: %#v", cm.Data)
	}
	secret, err := client.CoreV1().Secrets("tenant").Get(ctx, cm.Data["secret_ref"], metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if string(secret.Data["API_KEY"]) != "business-secret" || len(secret.Data) != 2 || secret.Immutable == nil || !*secret.Immutable {
		t.Fatalf("business environment was not stored as an immutable Secret: %#v", secret)
	}
	// Key ordering, whitespace and removed Environment fields do not change meaning.
	a.Request = json.RawMessage(` { "nested": {"keep":true}, "InstanceID":7 } `)
	if err := service.EnsureDesired(ctx, a); err != nil {
		t.Fatalf("canonical retry: %v", err)
	}
}

func TestRuntimeAssignmentRejectsStaleAndChangedIdentity(t *testing.T) {
	changes := map[string]func(*RuntimeAssignment){
		"older generation":                    func(a *RuntimeAssignment) { a.Generation--; a.GatewayID = "gw-7-2" },
		"new generation before same-Pod stop": func(a *RuntimeAssignment) { a.Generation++; a.GatewayID = "gw-7-4" },
		"changed owner":                       func(a *RuntimeAssignment) { a.UserID++ },
		"changed runtime":                     func(a *RuntimeAssignment) { a.RuntimeType = "hermes" },
		"changed owner newer generation":      func(a *RuntimeAssignment) { a.UserID++; a.Generation++; a.GatewayID = "gw-7-4" },
		"changed runtime newer generation":    func(a *RuntimeAssignment) { a.RuntimeType = "hermes"; a.Generation++; a.GatewayID = "gw-7-4" },
		"same generation endpoint":            func(a *RuntimeAssignment) { a.Endpoint = "http://pod-b:19091" },
		"same generation request":             func(a *RuntimeAssignment) { a.Request = json.RawMessage(`{"InstanceID":7,"changed":true}`) },
		"same generation secret":              func(a *RuntimeAssignment) { a.Environment = map[string]string{"API_KEY": "different"} },
		"same generation empty secret":        func(a *RuntimeAssignment) { a.Environment = nil },
		"mismatched gateway":                  func(a *RuntimeAssignment) { a.GatewayID = "gw-7-4" },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			service := NewRuntimeAssignmentService(client, "tenant")
			a := assignmentFixture()
			if err := service.EnsureDesired(context.Background(), a); err != nil {
				t.Fatal(err)
			}
			change(&a)
			if err := service.EnsureDesired(context.Background(), a); err == nil {
				t.Fatal("unsafe assignment was accepted")
			}
			if got := assignmentCM(t, client); got.Data["generation"] != "3" || got.Data["desired_state"] != "running" {
				t.Fatalf("rejected update mutated assignment: %#v", got.Data)
			}
			secrets, _ := client.CoreV1().Secrets("tenant").List(context.Background(), metav1.ListOptions{})
			if len(secrets.Items) != 1 {
				t.Fatalf("rejected assignment created a Secret: %d", len(secrets.Items))
			}
		})
	}
}

func TestRuntimeAssignmentHandoverRequiresConfirmedStop(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	service := NewRuntimeAssignmentService(client, "tenant")
	old := assignmentFixture()
	if err := service.EnsureDesired(ctx, old); err != nil {
		t.Fatal(err)
	}
	next := old
	next.Generation++
	next.GatewayID = "gw-7-4"
	next.Endpoint = "http://pod-b:19091"
	if err := service.EnsureDesired(ctx, next); err == nil {
		t.Fatal("running source authorized a second endpoint")
	}
	if err := service.MarkDraining(ctx, old.Endpoint, old.GatewayID); err != nil {
		t.Fatal(err)
	}
	if err := service.MarkDraining(ctx, old.Endpoint, old.GatewayID); err != nil {
		t.Fatal(err)
	}
	if err := service.EnsureDesired(ctx, next); err == nil {
		t.Fatal("draining without stop evidence authorized handover")
	}
	if err := service.ConfirmStopped(ctx, old.Endpoint, old.GatewayID); err != nil {
		t.Fatal(err)
	}
	if err := service.ConfirmStopped(ctx, old.Endpoint, old.GatewayID); err != nil {
		t.Fatal(err)
	}
	if err := service.EnsureDesired(ctx, old); err != nil {
		t.Fatal(err)
	}
	if got := assignmentCM(t, client).Data["desired_state"]; got != "stopped" {
		t.Fatalf("retry resurrected a stopped writer: %s", got)
	}
	if err := service.EnsureDesired(ctx, next); err != nil {
		t.Fatalf("confirmed handover: %v", err)
	}
	if err := service.EnsureDesired(ctx, next); err != nil {
		t.Fatalf("handover retry: %v", err)
	}
	if err := service.ConfirmStopped(ctx, old.Endpoint, old.GatewayID); err == nil {
		t.Fatal("old stop changed the new owner")
	}
	if err := service.MarkDraining(ctx, old.Endpoint, old.GatewayID); err == nil {
		t.Fatal("old delete changed the new owner")
	}
	cm := assignmentCM(t, client)
	if cm.Data["generation"] != "4" || cm.Data["endpoint"] != next.Endpoint || cm.Data["desired_state"] != "running" {
		t.Fatalf("bad handover: %#v", cm.Data)
	}
	secrets, _ := client.CoreV1().Secrets("tenant").List(ctx, metav1.ListOptions{})
	if len(secrets.Items) != 2 {
		t.Fatalf("generation secrets were overwritten: %d", len(secrets.Items))
	}
}

func TestRuntimeAssignmentStopIdentityFailsClosed(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	service := NewRuntimeAssignmentService(client, "tenant")
	if err := service.MarkDraining(ctx, "http://pod-a:19091", "gw-7-3"); err == nil {
		t.Fatal("unknown delete accepted")
	}
	if err := service.ConfirmStopped(ctx, "http://pod-a:19091", "gw-7-3"); err == nil {
		t.Fatal("unknown stop accepted")
	}
	a := assignmentFixture()
	if err := service.EnsureDesired(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := service.ConfirmStopped(ctx, a.Endpoint, a.GatewayID); err == nil {
		t.Fatal("running assignment skipped draining")
	}
	for _, id := range []string{"gw-7-2", "gw-7-03", "gw-7-3-extra", "gw-0-3", "invalid"} {
		if err := service.MarkDraining(ctx, a.Endpoint, id); err == nil {
			t.Fatalf("invalid stop identity %q accepted", id)
		}
	}
	if err := service.MarkDraining(ctx, "http://wrong:19091", a.GatewayID); err == nil {
		t.Fatal("wrong endpoint accepted")
	}
	if err := service.MarkDraining(ctx, a.Endpoint, a.GatewayID); err != nil {
		t.Fatal(err)
	}
	if err := service.ConfirmStopped(ctx, a.Endpoint, a.GatewayID); err != nil {
		t.Fatal(err)
	}
	if err := service.MarkDraining(ctx, a.Endpoint, a.GatewayID); err != nil {
		t.Fatal(err)
	}
	if got := assignmentCM(t, client).Data["desired_state"]; got != "stopped" {
		t.Fatalf("drain retry reversed stopped state: %s", got)
	}
}

func TestRuntimeAssignmentRetriesConflictAndRevalidates(t *testing.T) {
	ctx := context.Background()
	for _, replaceOwner := range []bool{false, true} {
		client := fake.NewSimpleClientset()
		service := NewRuntimeAssignmentService(client, "tenant")
		a := assignmentFixture()
		if err := service.EnsureDesired(ctx, a); err != nil {
			t.Fatal(err)
		}
		attempts := 0
		client.PrependReactor("update", "configmaps", func(action clienttesting.Action) (bool, runtime.Object, error) {
			attempts++
			if attempts > 1 {
				return false, nil, nil
			}
			if replaceOwner {
				object, err := client.Tracker().Get(corev1.SchemeGroupVersion.WithResource("configmaps"), "tenant", "lite-instance-7")
				if err != nil {
					t.Fatal(err)
				}
				cm := object.(*corev1.ConfigMap).DeepCopy()
				cm.Data["generation"] = "4"
				cm.Data["gateway_id"] = "gw-7-4"
				cm.Data["secret_ref"] = "lite-instance-7-g4"
				if err := client.Tracker().Update(corev1.SchemeGroupVersion.WithResource("configmaps"), cm, "tenant"); err != nil {
					t.Fatal(err)
				}
			}
			return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, "lite-instance-7", errors.New("test conflict"))
		})
		err := service.MarkDraining(ctx, a.Endpoint, a.GatewayID)
		if replaceOwner {
			if err == nil || attempts != 1 {
				t.Fatalf("conflicting generation not rejected: attempts=%d err=%v", attempts, err)
			}
			if assignmentCM(t, client).Data["desired_state"] != "running" {
				t.Fatal("stale update drained new generation")
			}
		} else if err != nil || attempts != 2 {
			t.Fatalf("conflict not retried: attempts=%d err=%v", attempts, err)
		}
	}
}

func TestRuntimeAssignmentEnsureConflictRechecksStop(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	service := NewRuntimeAssignmentService(client, "tenant")
	a := assignmentFixture()
	if err := service.EnsureDesired(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := service.MarkDraining(ctx, a.Endpoint, a.GatewayID); err != nil {
		t.Fatal(err)
	}
	if err := service.ConfirmStopped(ctx, a.Endpoint, a.GatewayID); err != nil {
		t.Fatal(err)
	}
	client.PrependReactor("update", "configmaps", func(action clienttesting.Action) (bool, runtime.Object, error) {
		object, err := client.Tracker().Get(corev1.SchemeGroupVersion.WithResource("configmaps"), "tenant", "lite-instance-7")
		if err != nil {
			t.Fatal(err)
		}
		cm := object.(*corev1.ConfigMap).DeepCopy()
		cm.Data["desired_state"] = "running"
		if err := client.Tracker().Update(corev1.SchemeGroupVersion.WithResource("configmaps"), cm, "tenant"); err != nil {
			t.Fatal(err)
		}
		return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, cm.Name, errors.New("changed stop authority"))
	})
	a.Generation++
	a.GatewayID = "gw-7-4"
	a.Endpoint = "http://pod-b:19091"
	if err := service.EnsureDesired(ctx, a); err == nil {
		t.Fatal("retry reused obsolete source-stop confirmation")
	}
	cm := assignmentCM(t, client)
	if cm.Data["generation"] != "3" || cm.Data["endpoint"] != "http://pod-a:19091" {
		t.Fatalf("failed CAS published handover: %#v", cm.Data)
	}
}

func TestRuntimeAssignmentInvalidRequestsHaveNoSideEffects(t *testing.T) {
	for _, request := range []string{"", "null", "[]", "true", "{} {}", "{"} {
		client := fake.NewSimpleClientset()
		a := assignmentFixture()
		a.Request = json.RawMessage(request)
		if err := NewRuntimeAssignmentService(client, "tenant").EnsureDesired(context.Background(), a); err == nil {
			t.Fatalf("invalid request accepted: %q", request)
		}
		if len(client.Actions()) != 0 {
			t.Fatalf("invalid request performed Kubernetes operations: %q", request)
		}
	}
}

func TestRuntimeAssignmentConcurrentCreateDoesNotResurrectStopped(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	service := NewRuntimeAssignmentService(client, "tenant")
	client.PrependReactor("create", "configmaps", func(action clienttesting.Action) (bool, runtime.Object, error) {
		cm := action.(clienttesting.CreateAction).GetObject().(*corev1.ConfigMap).DeepCopy()
		cm.Data["desired_state"] = "stopped"
		if err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("configmaps"), cm, "tenant"); err != nil {
			t.Fatal(err)
		}
		return true, nil, apierrors.NewAlreadyExists(schema.GroupResource{Resource: "configmaps"}, cm.Name)
	})
	if err := service.EnsureDesired(ctx, assignmentFixture()); err != nil {
		t.Fatal(err)
	}
	if assignmentCM(t, client).Data["desired_state"] != "stopped" {
		t.Fatal("conflicting create resurrected stopped assignment")
	}
}

func TestRuntimeAssignmentStripsEnvironmentInsideArrays(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	a := assignmentFixture()
	a.Request = json.RawMessage(`{"nested":[{"ENV":{"API_KEY":"nested-secret"},"keep":9007199254740993}]}`)
	if err := NewRuntimeAssignmentService(client, "tenant").EnsureDesired(ctx, a); err != nil {
		t.Fatal(err)
	}
	if request := assignmentCM(t, client).Data["request"]; request != `{"nested":[{"keep":9007199254740993}]}` {
		t.Fatalf("environment leaked or integer precision changed: %s", request)
	}
}

func TestRuntimeAssignmentCanStartRequiresCurrentRunningIdentity(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	service := NewRuntimeAssignmentService(client, "tenant")
	a := assignmentFixture()
	if err := service.CanStart(ctx, a.Endpoint, a.GatewayID); err == nil {
		t.Fatal("unknown assignment authorized startup")
	}
	if err := service.EnsureDesired(ctx, a); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := service.EnsureDesired(ctx, a); err != nil {
			t.Fatal(err)
		}
		if err := service.CanStart(ctx, a.Endpoint, a.GatewayID); err != nil {
			t.Fatalf("running retry denied: %v", err)
		}
	}
	if err := service.CanStart(ctx, "http://wrong:19091", a.GatewayID); err == nil {
		t.Fatal("wrong endpoint authorized startup")
	}
	if err := service.CanStart(ctx, a.Endpoint, "gw-7-2"); err == nil {
		t.Fatal("old generation authorized startup")
	}
	if err := service.MarkDraining(ctx, a.Endpoint, a.GatewayID); err != nil {
		t.Fatal(err)
	}
	if err := service.EnsureDesired(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := service.CanStart(ctx, a.Endpoint, a.GatewayID); err == nil {
		t.Fatal("draining retry authorized startup")
	}
	if err := service.ConfirmStopped(ctx, a.Endpoint, a.GatewayID); err != nil {
		t.Fatal(err)
	}
	if err := service.EnsureDesired(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := service.CanStart(ctx, a.Endpoint, a.GatewayID); err == nil {
		t.Fatal("stopped retry authorized startup")
	}
}

func TestRuntimeAssignmentPodIdentityFencesReusedEndpoint(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	service := NewRuntimeAssignmentService(client, "tenant")
	a := assignmentFixture()
	a.PodUID = "pod-a-uid"
	a.Request = json.RawMessage(`{"pod_uid":"pod-a-uid","Environment":{"TOKEN":"do-not-publish"}}`)
	if err := service.EnsureDesired(ctx, a); err != nil {
		t.Fatal(err)
	}
	cm := assignmentCM(t, client)
	if cm.Data["pod_uid"] != a.PodUID || cm.Data["request"] != `{"pod_uid":"pod-a-uid"}` {
		t.Fatalf("Pod identity not persisted: %#v", cm.Data)
	}
	next := a
	next.PodUID = "pod-b-uid"
	if err := service.EnsureDesired(ctx, next); err == nil {
		t.Fatal("same generation changed Pod UID at reused endpoint")
	}
	next.Generation++
	next.GatewayID = "gw-7-4"
	next.Request = json.RawMessage(`{"pod_uid":"pod-b-uid"}`)
	if err := service.EnsureDesired(ctx, next); err == nil {
		t.Fatal("new Pod at reused endpoint bypassed confirmed stop")
	}
	if err := service.MarkDraining(ctx, a.Endpoint, a.GatewayID); err != nil {
		t.Fatal(err)
	}
	if err := service.ConfirmStopped(ctx, a.Endpoint, a.GatewayID); err != nil {
		t.Fatal(err)
	}
	if err := service.EnsureDesired(ctx, next); err != nil {
		t.Fatal(err)
	}
	if err := service.CanStart(ctx, a.Endpoint, a.GatewayID); err == nil {
		t.Fatal("previous Pod generation authorized startup")
	}
	if err := service.CanStart(ctx, next.Endpoint, next.GatewayID); err != nil {
		t.Fatal(err)
	}
}

func TestStoppedAssignmentRequiresReceiptAndReturnsOnlyIdentity(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	service := NewRuntimeAssignmentService(client, "tenant")
	a := assignmentFixture()
	a.PodUID = "source-uid"
	a.Endpoint = "https://10.0.0.1:19090"
	if err := service.EnsureDesired(ctx, a); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StoppedAssignment(ctx, 7); err == nil {
		t.Fatal("running assignment used as stop evidence")
	}
	if err := service.MarkDraining(ctx, a.Endpoint, a.GatewayID); err != nil {
		t.Fatal(err)
	}
	if err := service.ConfirmStopped(ctx, a.Endpoint, a.GatewayID); err != nil {
		t.Fatal(err)
	}
	client.ClearActions()
	got, err := service.StoppedAssignment(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if got.PodUID != a.PodUID || got.Generation != a.Generation || got.Request != nil || got.Environment != nil {
		t.Fatalf("bad stopped identity: %+v", got)
	}
	for _, action := range client.Actions() {
		if action.GetResource().Resource != "configmaps" || action.GetVerb() != "get" {
			t.Fatal("stop evidence touched a Secret or mutated state")
		}
	}
	cm := assignmentCM(t, client)
	cm.Data["secret_ref"] = "wrong-generation"
	if _, err := client.CoreV1().ConfigMaps("tenant").Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StoppedAssignment(ctx, 7); err == nil {
		t.Fatal("tampered source reference accepted")
	}
}
