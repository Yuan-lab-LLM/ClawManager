package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

type securityInstances struct {
	instance *models.Instance
	err      error
}

func (s *securityInstances) GetByIDContext(ctx context.Context, id int) (*models.Instance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.err != nil {
		return nil, s.err
	}
	if s.instance != nil && s.instance.ID == id {
		return s.instance, nil
	}
	return nil, errors.New("absent")
}

func securityFixture(t *testing.T) (*RuntimeSecurityAuthority, *securityInstances, *fake.Clientset, *corev1.ConfigMap, *corev1.Pod) {
	t.Helper()
	instances := &securityInstances{instance: &models.Instance{ID: 7, UserID: 3, Type: "opencode", InstanceMode: "lite", RuntimeType: "gateway", Status: "running", RuntimeGeneration: 2, RuntimeSecurityProfile: RuntimeSecurityProfile}}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "lite-instance-7", Namespace: "test"}, Data: map[string]string{
		"instance_id": "7", "user_id": "3", "generation": "2", "pod_uid": "pod-7", "runtime_type": "opencode", "desired_state": "running", "endpoint": "https://10.2.0.3:19090",
		"request": `{"instance_id":7,"user_id":3,"generation":2,"pod_uid":"pod-7","agent_type":"opencode","security_profile":"isolated-network-v1","gateway_port":24000}`,
	}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "test", UID: types.UID("pod-7"), Labels: map[string]string{"clawmanager.io/runtime-protocol": "container-slots-v1", "clawmanager.io/runtime-security": RuntimeSecurityProfile}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.2.0.3", Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}
	kube := fake.NewSimpleClientset(cm, pod)
	a, err := NewRuntimeSecurityAuthority(kube, "test", instances, []byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	return a, instances, kube, cm, pod
}

func TestRuntimeSecurityAuthorityRejectsStaleAndUnprotectedAssignments(t *testing.T) {
	for _, scenario := range []string{"running", "creating", "stopped", "wrong-generation", "wrong-user", "missing-assignment", "draining", "unprotected-request", "unprotected-instance", "replaced-pod", "unready-pod", "unprotected-pod", "wrong-endpoint", "repository-error", "kube-error"} {
		t.Run(scenario, func(t *testing.T) {
			a, instances, kube, cm, pod := securityFixture(t)
			switch scenario {
			case "creating":
				instances.instance.Status = "creating"
			case "stopped":
				instances.instance.Status = "stopped"
			case "wrong-generation":
				instances.instance.RuntimeGeneration++
			case "wrong-user":
				instances.instance.UserID++
			case "missing-assignment":
				if err := kube.CoreV1().ConfigMaps("test").Delete(context.Background(), cm.Name, metav1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
			case "draining":
				cm.Data["desired_state"] = "draining"
			case "unprotected-request":
				cm.Data["request"] = strings.ReplaceAll(cm.Data["request"], RuntimeSecurityProfile, "legacy")
			case "unprotected-instance":
				instances.instance.RuntimeSecurityProfile = ""
			case "replaced-pod":
				pod.UID = "replacement"
			case "unready-pod":
				pod.Status.Conditions = nil
			case "unprotected-pod":
				delete(pod.Labels, "clawmanager.io/runtime-security")
			case "wrong-endpoint":
				cm.Data["endpoint"] = "https://10.2.0.4:19090"
			case "repository-error":
				instances.err = errors.New("unavailable")
			case "kube-error":
				kube.PrependReactor("get", "configmaps", func(action ktesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("unavailable")
				})
			}
			if scenario != "missing-assignment" {
				if _, err := kube.CoreV1().ConfigMaps("test").Update(context.Background(), cm, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := kube.CoreV1().Pods("test").Update(context.Background(), pod, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			identity, err := a.CurrentIdentity(context.Background(), 7)
			if scenario != "running" && scenario != "creating" {
				if err == nil {
					t.Fatalf("unsafe %s accepted: %+v", scenario, identity)
				}
				return
			}
			if err != nil || identity != (runtimeauth.Identity{InstanceID: 7, UserID: 3, Generation: 2, PodUID: "pod-7"}) {
				t.Fatalf("identity %+v: %v", identity, err)
			}
		})
	}
}

func TestProtectedRuntimeIdentityCannotDowngradeWhenAssignmentDisappears(t *testing.T) {
	a, _, kube, cm, _ := securityFixture(t)
	if err := kube.CoreV1().ConfigMaps("test").Delete(context.Background(), cm.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	protected, err := a.ProtectedInstance(context.Background(), 7)
	if err != nil || !protected {
		t.Fatalf("lost sticky protection: %v %v", protected, err)
	}
	if _, err := a.CurrentIdentity(context.Background(), 7); err == nil {
		t.Fatal("absent assignment authorized")
	}
}

func TestRuntimeSecurityCapabilityRevokesOnDesiredStop(t *testing.T) {
	a, _, kube, cm, _ := securityFixture(t)
	identity, err := a.CurrentIdentity(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	token, err := a.Capabilities().Issue(context.Background(), identity, runtimeauth.AudienceEgress)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Capabilities().Verify(context.Background(), token, runtimeauth.AudienceEgress); err != nil {
		t.Fatal(err)
	}
	cm.Data["desired_state"] = "draining"
	if _, err := kube.CoreV1().ConfigMaps("test").Update(context.Background(), cm, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Capabilities().Verify(context.Background(), token, runtimeauth.AudienceEgress); err == nil {
		t.Fatal("stopped assignment retained capability")
	}
}

type securityInstanceSourceFunc func(context.Context, int) (*models.Instance, error)

func (f securityInstanceSourceFunc) GetByIDContext(ctx context.Context, id int) (*models.Instance, error) {
	return f(ctx, id)
}

func TestProtectedInstanceOnlyExactEmptyProfileIsLegacy(t *testing.T) {
	for _, profile := range []string{"", RuntimeSecurityProfile, "future-profile", " ", "\t", "\r\n"} {
		t.Run(fmt.Sprintf("%q", profile), func(t *testing.T) {
			a, instances, _, _, _ := securityFixture(t)
			instances.instance.RuntimeSecurityProfile = profile
			protected, err := a.ProtectedInstance(context.Background(), 7)
			if err != nil || protected != (profile != "") {
				t.Fatalf("profile %q: protected=%v err=%v", profile, protected, err)
			}
		})
	}
}

func TestProtectedInstanceRejectsCancellationDuringLookup(t *testing.T) {
	for _, profile := range []string{"", RuntimeSecurityProfile} {
		t.Run(profile, func(t *testing.T) {
			a, instances, _, _, _ := securityFixture(t)
			instances.instance.RuntimeSecurityProfile = profile
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			a.instances = securityInstanceSourceFunc(func(context.Context, int) (*models.Instance, error) { cancel(); return instances.instance, nil })
			protected, err := a.ProtectedInstance(ctx, 7)
			if !errors.Is(err, context.Canceled) || protected {
				t.Fatalf("canceled lookup returned a decision: protected=%v err=%v", protected, err)
			}
		})
	}
}

func TestNativeGatewayCredentialIdentityBindingAndPurpose(t *testing.T) {
	a, _, _, _, _ := securityFixture(t)
	original, err := a.NativeGatewayCredential(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := a.NativeGatewayCredential(context.Background(), 7)
	if err != nil || original != repeated || !strings.HasPrefix(original, "ui1_") {
		t.Fatalf("UI credential changed within one identity: %v", err)
	}
	identity, err := a.CurrentIdentity(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if a.nativeCredential(identity) != original {
		t.Fatal("native credential differs from current authoritative identity")
	}
	for name, change := range map[string]func(*runtimeauth.Identity){"user": func(i *runtimeauth.Identity) { i.UserID++ }, "instance": func(i *runtimeauth.Identity) { i.InstanceID++ }, "generation": func(i *runtimeauth.Identity) { i.Generation++ }, "pod": func(i *runtimeauth.Identity) { i.PodUID += "-replacement" }} {
		t.Run(name, func(t *testing.T) {
			changed := identity
			change(&changed)
			if a.nativeCredential(changed) == original {
				t.Fatal("UI credential failed to rotate on identity change")
			}
		})
	}
	audiences := []string{runtimeauth.AudienceModel, runtimeauth.AudienceEgress, runtimeauth.AudienceServices}
	for _, issuedAudience := range audiences {
		if _, err := a.Capabilities().Verify(context.Background(), original, issuedAudience); err == nil {
			t.Fatalf("native UI credential accepted as %s capability", issuedAudience)
		}
		token, err := a.Capabilities().Issue(context.Background(), identity, issuedAudience)
		if err != nil {
			t.Fatal(err)
		}
		if token == original {
			t.Fatal("capability is interchangeable with native UI credential")
		}
		for _, requestedAudience := range audiences {
			_, err = a.Capabilities().Verify(context.Background(), token, requestedAudience)
			if (err == nil) != (issuedAudience == requestedAudience) {
				t.Fatalf("purpose confusion: issued=%s requested=%s err=%v", issuedAudience, requestedAudience, err)
			}
		}
	}
}
