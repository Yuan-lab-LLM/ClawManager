package services

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"clawreef/internal/models"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

type policyAgentStub struct {
	RuntimeAgentClient
	readErr   error
	writes    int
	conflict  bool
	addresses []string
}

func (a *policyAgentStub) ReadProxyPolicy(context.Context, string) (*RuntimeProxyPolicy, error) {
	return &RuntimeProxyPolicy{Contract: 1, Namespace: "tenant", Revision: "old"}, a.readErr
}
func (a *policyAgentStub) WriteProxyPolicy(_ context.Context, _, _, ns string, addresses []string) (*RuntimeProxyPolicy, error) {
	a.writes++
	if a.conflict && a.writes == 1 {
		return nil, ErrRuntimeAgentConflict
	}
	a.addresses = append([]string(nil), addresses...)
	return &RuntimeProxyPolicy{Contract: 1, Namespace: ns, Revision: "new", Proxies: addresses, ExpiresAtMS: time.Now().Add(time.Minute).UnixMilli()}, nil
}

func TestProxyPolicyDiscoversOnlyTenantServicePodsAndRetriesCAS(t *testing.T) {
	pod := func(ns, name, ip string, phase corev1.PodPhase) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, UID: types.UID(name), Labels: map[string]string{"app": "manager"}}, Status: corev1.PodStatus{Phase: phase, PodIP: ip}}
	}
	client := fake.NewSimpleClientset(
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant", Name: "frontend"}, Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "manager"}}},
		pod("tenant", "old", "10.244.236.50", corev1.PodRunning),
		pod("tenant", "new", "10.244.166.131", corev1.PodRunning),
		pod("tenant", "pending", "10.244.1.2", corev1.PodPending),
		pod("other-tenant", "other", "10.244.1.3", corev1.PodRunning),
	)
	agent := &policyAgentStub{conflict: true}
	c := NewRuntimeProxyPolicyCoordinator(client, agent, "tenant", "frontend")
	endpoint := "http://runtime:19090"
	p := &models.RuntimePod{ID: 1, Namespace: "tenant", AgentEndpoint: &endpoint}
	if err := c.Ensure(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if agent.writes != 2 || !reflect.DeepEqual(agent.addresses, []string{"10.244.166.131", "10.244.236.50"}) {
		t.Fatalf("writes=%d addresses=%v", agent.writes, agent.addresses)
	}
	if err := c.Ensure(context.Background(), p); err != nil || agent.writes != 2 {
		t.Fatal("valid policy was not cached")
	}
	p.Namespace = "other-tenant"
	if err := c.Ensure(context.Background(), p); err == nil {
		t.Fatal("cross-tenant Runtime accepted")
	}
}

func TestProxyPolicyLegacyAndDiscoveryFailure(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		agent := &policyAgentStub{}
		if legacy {
			agent.readErr = ErrRuntimeAgentNotFound
		}
		c := NewRuntimeProxyPolicyCoordinator(fake.NewSimpleClientset(), agent, "tenant", "missing-service")
		endpoint := "http://runtime:19090"
		err := c.Ensure(context.Background(), &models.RuntimePod{ID: 1, Namespace: "tenant", AgentEndpoint: &endpoint})
		if legacy && err != nil {
			t.Fatal(err)
		}
		if !legacy && err == nil {
			t.Fatal("discovery failure must block, not publish empty trust")
		}
		if agent.writes != 0 {
			t.Fatal("invalid discovery or legacy capability received update")
		}
	}
}

func TestModelConfigurationErrorRetainsCodeThroughWrapping(t *testing.T) {
	err := errors.Join(errors.New("startup validation"), ErrModelConfigurationRequired)
	if runtimeFailureCode(err) != "MODEL_CONFIGURATION_REQUIRED" || !errors.Is(err, ErrModelConfigurationRequired) {
		t.Fatal(err)
	}
}

func TestStartupPolicyPrecedesWorkspaceAndReservationOnBothPaths(t *testing.T) {
	endpoint := "http://runtime:19090"
	pod := models.RuntimePod{ID: 1, Namespace: "tenant", AgentEndpoint: &endpoint}
	for _, upgrade := range []bool{false, true} {
		binding := newFakeRuntimeBindingRepo()
		agent := &policyAgentStub{readErr: errors.New("policy unavailable")}
		coordinator := NewRuntimeProxyPolicyCoordinator(fake.NewSimpleClientset(), agent, "tenant", "frontend")
		s := NewRuntimeScheduler(newFakeRuntimeInstanceRepo(), &fakeRuntimePodRepo{}, binding, nil, &fakeRuntimeAgentClient{}, nil, nil, nil, time.Second,
			WithRuntimeSchedulerProxyPolicyCoordinator(coordinator),
			WithRuntimeSchedulerGatewayEnvBuilder(func(*models.Instance) (map[string]string, error) {
				t.Fatal("workspace/environment prepared before policy")
				return nil, nil
			}))
		var rollout *models.RuntimeRollout
		if upgrade {
			rollout = &models.RuntimeRollout{ID: 123}
		}
		_, err := s.prepareGatewayStartOwned(context.Background(), models.Instance{ID: 42}, RuntimeTypeOpenClaw, pod, nil, rollout)
		if runtimeFailureCode(err) != "PROXY_POLICY_NOT_READY" {
			t.Fatalf("upgrade=%v: %v", upgrade, err)
		}
		if code, hold := upgradeRecoveryError(err); code != "PROXY_POLICY_NOT_READY" || hold {
			t.Fatalf("wrong recovery classification: %s %v", code, hold)
		}
		got, _ := binding.GetByInstanceID(context.Background(), 42)
		if got != nil {
			t.Fatal("failed policy reserved a binding")
		}
	}
}

func TestIdlePolicyRenewalKeepsTenantAndRuntimeBoundaries(t *testing.T) {
	endpoint := "http://runtime:19090"
	client := fake.NewSimpleClientset(
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant", Name: "frontend"}, Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "manager"}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant", Name: "app", UID: "app-uid", Labels: map[string]string{"app": "manager"}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.1.1.2"}},
	)
	agent := &policyAgentStub{}
	coordinator := NewRuntimeProxyPolicyCoordinator(client, agent, "tenant", "frontend")
	pods := &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{
		1: {ID: 1, RuntimeType: RuntimeTypeOpenClaw, Namespace: "tenant", AgentEndpoint: &endpoint, State: "ready"},
		2: {ID: 2, RuntimeType: RuntimeTypeOpenClaw, Namespace: "other", AgentEndpoint: &endpoint, State: "ready"},
		3: {ID: 3, RuntimeType: RuntimeTypeOpenClaw, Namespace: "tenant", AgentEndpoint: &endpoint, State: "draining", Draining: true, UsedSlots: 0},
		4: {ID: 4, RuntimeType: RuntimeTypeHermes, Namespace: "tenant", AgentEndpoint: &endpoint, State: "ready"},
	}}
	s := &RuntimeScheduler{podRepo: pods, proxyPolicy: coordinator, runtimeNamespace: "tenant"}
	if err := s.renewRuntimeProxyPolicies(context.Background()); err != nil {
		t.Fatal(err)
	}
	if agent.writes != 2 {
		t.Fatalf("writes=%d", agent.writes)
	}
	// Renewal works without any instance/page access, and doesn't reuse an
	// expired cache entry when App addresses rotate.
	coordinator.validUntil = map[string]time.Time{}
	p, _ := client.CoreV1().Pods("tenant").Get(context.Background(), "app", metav1.GetOptions{})
	p.Status.PodIP = "10.1.1.3"
	_, _ = client.CoreV1().Pods("tenant").Update(context.Background(), p, metav1.UpdateOptions{})
	if err := s.renewRuntimeProxyPolicies(context.Background()); err != nil {
		t.Fatal(err)
	}
	if agent.writes != 4 || !reflect.DeepEqual(agent.addresses, []string{"10.1.1.3"}) {
		t.Fatalf("renewal=%v", agent.addresses)
	}
}

func TestHermesStartupPolicyPrecedesWorkspaceReservation(t *testing.T) {
	endpoint := "http://hermes:19090"
	binding := newFakeRuntimeBindingRepo()
	coordinator := NewRuntimeProxyPolicyCoordinator(fake.NewSimpleClientset(), &policyAgentStub{readErr: errors.New("unavailable")}, "tenant", "frontend")
	s := NewRuntimeScheduler(newFakeRuntimeInstanceRepo(), &fakeRuntimePodRepo{}, binding, nil, &fakeRuntimeAgentClient{}, nil, nil, nil, time.Second,
		WithRuntimeSchedulerProxyPolicyCoordinator(coordinator),
		WithRuntimeSchedulerGatewayEnvBuilder(func(*models.Instance) (map[string]string, error) {
			t.Fatal("Hermes workspace/environment prepared before policy")
			return nil, nil
		}))
	_, err := s.prepareGatewayStartOwned(context.Background(), models.Instance{ID: 42}, RuntimeTypeHermes, models.RuntimePod{ID: 1, Namespace: "tenant", AgentEndpoint: &endpoint}, nil, nil)
	if runtimeFailureCode(err) != "PROXY_POLICY_NOT_READY" || len(binding.bindings) != 0 {
		t.Fatalf("Hermes policy failure was not ownership-safe: %v", err)
	}
}

func TestProxyPolicyDropsTerminatedPodsAndHonorsDrainDeadline(t *testing.T) {
	pod := func(name, ip string, deadline time.Time) *corev1.Pod {
		stamp := metav1.NewTime(deadline)
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant", Name: name, UID: types.UID(name), Labels: map[string]string{"app": "manager"}, DeletionTimestamp: &stamp}, Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: ip}}
	}
	client := fake.NewSimpleClientset(
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: "tenant", Name: "frontend"}, Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "manager"}}},
		pod("expired", "10.244.1.1", time.Now().Add(-time.Second)),
		pod("draining", "10.244.1.2", time.Now().Add(time.Minute)),
	)
	c := NewRuntimeProxyPolicyCoordinator(client, &policyAgentStub{}, "tenant", "frontend")
	addresses, err := c.discover(context.Background())
	if err != nil || !reflect.DeepEqual(addresses, []string{"10.244.1.2"}) {
		t.Fatalf("addresses=%v err=%v", addresses, err)
	}
}
