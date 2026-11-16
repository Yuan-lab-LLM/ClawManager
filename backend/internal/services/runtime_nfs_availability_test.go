package services

import (
	"clawreef/internal/models"
	cmk8s "clawreef/internal/services/k8s"
	"context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"strconv"
	"strings"
	"testing"
	"time"
)

func nfsAvailabilityPod(pod *corev1.Pod, port, block int) {
	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}
	pod.Labels["clawmanager.io/runtime-protocol"] = "container-slots-v1"
	pod.Labels["clawmanager.io/sandbox-profile"] = cmk8s.RuntimeNFSRootlessProfile
	pod.Status.Phase = corev1.PodRunning
	pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse, Reason: "ContainersNotReady"}}
	pod.Spec.Containers = []corev1.Container{{Name: "runtime", Env: []corev1.EnvVar{{Name: "RUNTIME_AGENT_ROLE", Value: "pool"}}}, {Name: "slot-0", Env: []corev1.EnvVar{{Name: "RUNTIME_AGENT_ROLE", Value: "slot"}, {Name: "RUNTIME_GATEWAY_PORT_START", Value: strconv.Itoa(port)}, {Name: "RUNTIME_GATEWAY_PORT_END", Value: strconv.Itoa(port + block - 1)}}}, {Name: "slot-1", Env: []corev1.EnvVar{{Name: "RUNTIME_AGENT_ROLE", Value: "slot"}, {Name: "RUNTIME_GATEWAY_PORT_START", Value: strconv.Itoa(port + block)}, {Name: "RUNTIME_GATEWAY_PORT_END", Value: strconv.Itoa(port + 2*block - 1)}}}}
	pod.Status.ContainerStatuses = nil
	for _, name := range []string{"runtime", "slot-0"} {
		pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{Name: name, Ready: true, ContainerID: "containerd://" + name, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.Now()}}})
	}
	pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{Name: "slot-1", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}}})
}

func TestNFSHealthyHermesBFFSurvivesPeerResetAndSQLPending(t *testing.T) {
	service, kube, _ := protectedDesktopFixture(t, "http://127.0.0.1:24000")
	service.config.Pods.(*fakeRuntimePodRepo).pods[9].State = "pending"
	pod, err := kube.CoreV1().Pods("test").Get(t.Context(), "pool", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	nfsAvailabilityPod(pod, 24000, 1)
	if _, err = kube.CoreV1().Pods("test").Update(t.Context(), pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	target, reason, err := service.resolve(t.Context(), 45, 123)
	if err != nil || target == nil || reason != "" || !strings.HasPrefix(target.nativeCredential, "ui1_") {
		t.Fatalf("healthy BFF blocked by peer: %s %v", reason, err)
	}
	pod.Status.ContainerStatuses[1].Ready = false
	if _, err = kube.CoreV1().Pods("test").Update(t.Context(), pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if target, _, err = service.resolve(t.Context(), 45, 123); target != nil && err == nil {
		t.Fatal("unready assigned slot retained BFF access")
	}
}

func TestNFSHealthyTeamMemberSurvivesPeerResetAndSQLPending(t *testing.T) {
	for _, healthy := range []bool{true, false} {
		t.Run(strconv.FormatBool(healthy), func(t *testing.T) {
			report, live := ordinaryPlacementFixture()
			report.State = "pending"
			now := time.Now().UTC()
			deployment := cmk8s.BuildRuntimeDeployment(cmk8s.RuntimeDeploymentSpec{Name: "pool", Namespace: "tenant", Replicas: 1, RuntimeType: "openclaw", Image: report.ImageRef, SlotCount: 2, SandboxProfile: cmk8s.RuntimeNFSRootlessProfile, NetworkIsolation: true, AgentTokenSecretName: "agent", GatewayPortStart: 20000, GatewayPortEnd: 20005})
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: live.PodName, Namespace: live.Namespace, UID: types.UID(live.PodUID), Labels: deployment.Spec.Template.Labels}, Status: corev1.PodStatus{PodIP: *live.PodIP}}
			nfsAvailabilityPod(pod, 20000, 3)
			for i := range pod.Spec.Containers {
				pod.Spec.Containers[i].Image = report.ImageRef
			}
			pod.Status.ContainerStatuses[1].Ready = healthy
			client := &cmk8s.Client{Namespace: "tenant", Clientset: fake.NewSimpleClientset(deployment, pod)}
			instance := models.Instance{ID: 9, UserID: 1, Type: "openclaw", InstanceMode: "lite", RuntimeType: "gateway", Status: "running", RuntimeGeneration: 1, RuntimeSecurityProfile: RuntimeSecurityProfile}
			binding := &models.InstanceRuntimeBinding{InstanceID: 9, RuntimePodID: report.ID, RuntimeType: "openclaw", GatewayID: "gw-9-1", GatewayPort: 20000, Generation: 1, State: "running", LastHealthAt: &now}
			bindings := newFakeRuntimeBindingRepo()
			bindings.bindings[9] = binding
			agent := &upgradeFlowAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, states: map[string]*RuntimeAgentGatewayState{"gw-9-1": {GatewayID: "gw-9-1", InstanceID: 9, Generation: 1, State: "running"}}}
			service := &instanceService{bindingRepo: bindings, runtimePodRepo: &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{report.ID: &report}}, agentClient: agent, podService: cmk8s.NewPodServiceWithClient(client)}
			ready, err := service.OwnerTeamRuntimeReady(context.Background(), &instance)
			if err != nil || ready != healthy {
				t.Fatalf("OwnerTeam ready=%t expected=%t err=%v", ready, healthy, err)
			}
			team := NewTeamService(nil, nil, WithTeamKubernetesClient(client)).(*teamService)
			observed := team.observeMemberGateway(t.Context(), models.TeamMemberRuntimeFacts{Instance: instance, Binding: binding, Runtime: &report}, now, nil)
			if (observed.State == "online") != healthy {
				t.Fatalf("Team member observation lost healthy sibling: %+v", observed)
			}
		})
	}
}

func TestNFSPeerResetKeepsProxyPolicyRenewalForHealthyExistingGateway(t *testing.T) {
	for _, healthy := range []bool{true, false} {
		t.Run(strconv.FormatBool(healthy), func(t *testing.T) {
			report, live := ordinaryPlacementFixture()
			report.State = "pending"
			report.UsedSlots = 2
			deployment := cmk8s.BuildRuntimeDeployment(cmk8s.RuntimeDeploymentSpec{Name: "pool", Namespace: "tenant", Replicas: 1, RuntimeType: "openclaw", Image: report.ImageRef, SlotCount: 2, SandboxProfile: cmk8s.RuntimeNFSRootlessProfile, NetworkIsolation: true, AgentTokenSecretName: "agent", GatewayPortStart: 20000, GatewayPortEnd: 20005})
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: live.PodName, Namespace: live.Namespace, UID: types.UID(live.PodUID), Labels: deployment.Spec.Template.Labels}, Status: corev1.PodStatus{PodIP: *live.PodIP}}
			nfsAvailabilityPod(pod, 20000, 3)
			for i := range pod.Spec.Containers {
				pod.Spec.Containers[i].Image = report.ImageRef
			}
			pod.Status.ContainerStatuses[1].Ready = healthy
			kube := fake.NewSimpleClientset(deployment, pod, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "tenant"}, Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "manager"}}}, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "manager", Namespace: "tenant", UID: "manager-uid", Labels: map[string]string{"app": "manager"}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.1.2.4"}})
			agent := &policyAgentStub{}
			coordinator := NewRuntimeProxyPolicyCoordinator(kube, agent, "tenant", "frontend")
			scheduler := &RuntimeScheduler{podRepo: &fakeRuntimePodRepo{pods: map[int64]*models.RuntimePod{report.ID: &report}}, proxyPolicy: coordinator, runtimeNamespace: "tenant"}
			if err := scheduler.renewRuntimeProxyPolicies(t.Context()); err != nil {
				t.Fatal(err)
			}
			if (agent.writes == 1) != healthy {
				t.Fatalf("healthy existing gateway lost managed proxy renewal: writes=%d healthy=%t", agent.writes, healthy)
			}
		})
	}
}
