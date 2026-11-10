package services

import (
	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNativeValidationUsesProtectedSupervisorWithoutBusinessPublication(t *testing.T) {
	ctx := context.Background()
	image := "repo@sha256:" + strings.Repeat("a", 64)
	d := k8s.BuildRuntimeDeployment(k8s.RuntimeDeploymentSpec{Name: "source", Namespace: "runtime", RuntimeType: "openclaw", Image: image, Replicas: 1, SlotCount: 8, AgentTokenSecretName: "clawmanager-lite-agent", WorkspaceMountPath: "/workspaces", PodUserNamespaces: true, NetworkIsolation: true})
	d.UID = "source-uid"
	kube := fake.NewSimpleClientset(d)
	validation := k8s.NewRuntimeDeploymentService(kube).(k8s.RuntimePoolValidationService)
	if e := validation.EnsurePoolValidation(ctx, k8s.RuntimeRolloutTarget{Namespace: "runtime", Name: "source", UID: "source-uid"}, replacementProbeName(21, 0), image, "21"); e != nil {
		t.Fatal(e)
	}
	d, _ = kube.AppsV1().Deployments("runtime").Get(ctx, replacementProbeName(21, 0), metav1.GetOptions{})
	d.UID = "validation-uid"
	kube.AppsV1().Deployments("runtime").Update(ctx, d, metav1.UpdateOptions{})
	yes := true
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "validation-rs", Namespace: "runtime", UID: "rs-uid", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: d.Name, UID: d.UID, Controller: &yes}}}, Spec: appsv1.ReplicaSetSpec{Template: *d.Spec.Template.DeepCopy()}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "probe", Namespace: "runtime", UID: "probe-uid", Labels: d.Spec.Template.Labels, Annotations: d.Spec.Template.Annotations, OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: &yes}}}, Spec: *d.Spec.Template.Spec.DeepCopy(), Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.2", Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}
	kube.AppsV1().ReplicaSets("runtime").Create(ctx, rs, metav1.CreateOptions{})
	kube.CoreV1().Pods("runtime").Create(ctx, pod, metav1.CreateOptions{})
	calls := 0
	creates := 0
	slotState := `{"pod_uid":"probe-uid","worker_epoch":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee","idle_confirmed":true,"quarantined":false,"gateways":[]}`
	badSlotState, badSlot, retrySlotState := "", "", ""
	if e := k8s.EnsureRuntimeSlotTLS(ctx, kube, "runtime"); e != nil {
		t.Fatal(e)
	}
	fixtureTLS, _ := kube.CoreV1().Secrets("runtime").Get(ctx, "clawmanager-lite-agent-tls", metav1.GetOptions{})
	pair, e := tls.X509KeyPair(fixtureTLS.Data[corev1.TLSCertKey], fixtureTLS.Data[corev1.TLSPrivateKeyKey])
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		calls++
		if r.TLS == nil || r.TLS.Version < tls.VersionTLS13 || r.Header.Get("X-ClawManager-Control-Token") == "" {
			t.Fatal("native validation lost authenticated transport")
		}
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/slots/") {
			body := slotState
			if r.URL.Path == "/v1/slots/slot-0/state" && retrySlotState != "" {
				body = retrySlotState
			}
			if r.URL.Path == "/v1/slots/"+badSlot+"/state" {
				body = badSlotState
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, body)
			return
		}
		body := fmt.Sprintf(`{"gateway_id":"gw-%d-1","port":20000,"status":"running"}`, poolProbeInstanceID)
		if r.Method == http.MethodPost {
			creates++
			var req RuntimeAgentCreateGatewayRequest
			if e := json.NewDecoder(r.Body).Decode(&req); e != nil {
				t.Fatal(e)
			}
			if req.SecurityProfile != RuntimeSecurityProfile || req.PodUID != "probe-uid" || req.UID < 200000 || !strings.HasPrefix(req.Environment["CLAWMANAGER_INSTANCE_TOKEN"], "ui1_") {
				t.Fatal("native security request missing")
			}
			if req.Environment[OpenClawScheduledTasksEnv] != `{"schemaVersion":1,"items":[]}` {
				t.Error("probe scheduled tasks did not use the canonical empty envelope")
			}
			if req.Environment["CLAWMANAGER_LLM_API_KEY"] != "cm-local-model" {
				t.Fatal("shared native sanitizer was bypassed")
			}
		}
		if r.Method == http.MethodDelete {
			body = fmt.Sprintf(`{"gateway_id":"gw-%d-1","stopped":true,"confirmed":true,"unmounted":true}`, poolProbeInstanceID)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, body)
	}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}}
	server.StartTLS()
	defer server.Close()
	trust, e := k8s.RuntimeSlotTLSConfig(ctx, kube, "runtime")
	if e != nil {
		t.Fatal(e)
	}
	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: trust, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != "10.0.0.2:19090" {
			return nil, fmt.Errorf("unexpected validation endpoint")
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}}}
	client := NewSlotRuntimeAgentClientWithHTTPClient(strings.Repeat("c", 32), kube, "runtime", httpClient).(*slotRuntimeAgentClient)
	client.security = &RuntimeSecurityAuthority{key: []byte(strings.Repeat("k", 32))}
	client.assignmentAdmission = func(context.Context, string, string, int, int, func() error) error {
		t.Fatal("validation entered business SQL admission")
		return nil
	}
	adapter, ok := any(client).(interface {
		CreatePoolValidationGateway(context.Context, k8s.RuntimePoolValidationScope) (*RuntimeAgentCreateGatewayResponse, error)
		DeletePoolValidationGateway(context.Context, k8s.RuntimePoolValidationScope, string) error
	})
	if !ok {
		t.Fatal("native probe still requires a business SQL instance")
	}
	scope := k8s.RuntimePoolValidationScope{Namespace: "runtime", DeploymentName: d.Name, DeploymentUID: string(d.UID), SourceName: "source", SourceUID: "source-uid", Operation: "21", PodUID: string(pod.UID), Endpoint: "https://10.0.0.2:19090", ImageRef: image, WorkspaceRoot: "/workspaces"}
	ownedSlotState := fmt.Sprintf(`{"pod_uid":"probe-uid","worker_epoch":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee","idle_confirmed":false,"quarantined":false,"gateways":[{"gateway_id":"gw-%d-1","state":"starting"}],"assignment":{"pod_uid":"probe-uid","worker_epoch":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee","gateway_id":"gw-%d-1","instance_id":%d,"user_id":1,"generation":1,"state":"starting"}}`, poolProbeInstanceID, poolProbeInstanceID, poolProbeInstanceID)
	for _, test := range []struct{ name, body string }{
		{"live-old-agent", `{"gateways":[],"quarantined":false,"skills":{}}`},
		{"wrong-pod", strings.Replace(slotState, "probe-uid", "other-pod", 1)},
		{"missing-pod", strings.Replace(slotState, `"pod_uid":"probe-uid",`, "", 1)},
		{"invalid-epoch", strings.Replace(slotState, strings.Repeat("e", 32), "invalid", 1)},
		{"missing-idle", strings.Replace(slotState, `"idle_confirmed":true,`, "", 1)},
		{"missing-quarantine", strings.Replace(slotState, `"quarantined":false,`, "", 1)},
		{"null-gateways", strings.Replace(slotState, `"gateways":[]`, `"gateways":null`, 1)},
		{"not-idle-without-owner", strings.Replace(slotState, `"idle_confirmed":true`, `"idle_confirmed":false`, 1)},
		{"quarantined", strings.Replace(slotState, `"quarantined":false`, `"quarantined":true`, 1)},
		{"malformed", `{`},
		{"startup-on-wrong-slot", ownedSlotState},
		{"owner-foreign-user", strings.Replace(ownedSlotState, `"user_id":1`, `"user_id":2`, 1)},
		{"owner-foreign-pod", strings.Replace(ownedSlotState, `"assignment":{"pod_uid":"probe-uid"`, `"assignment":{"pod_uid":"other-pod"`, 1)},
		{"owner-different-epoch", strings.Replace(ownedSlotState, strings.Repeat("e", 32), strings.Repeat("f", 32), 1)},
		{"owner-ambiguous-gateways", strings.Replace(ownedSlotState, `"gateways":[`, `"gateways":[{"gateway_id":"gw-foreign","state":"running"},`, 1)},
	} {
		t.Run("slot-proof-"+test.name, func(t *testing.T) {
			// A compatible first slot cannot hide an incompatible later worker.
			badSlot, badSlotState = "slot-7", test.body
			if strings.HasPrefix(test.name, "owner-") {
				badSlot = "slot-0"
			}
			before := creates
			if _, err := adapter.CreatePoolValidationGateway(ctx, scope); err == nil || creates != before {
				t.Fatal("incompatible physical proof reached Gateway creation", err)
			}
		})
	}
	badSlot, badSlotState = "", ""
	beforeValid := calls
	response, e := adapter.CreatePoolValidationGateway(ctx, scope)
	if e != nil {
		t.Fatal(e)
	}
	// Starting probes are polled through the same method. Their exact existing
	// native assignment must remain admissible without a false idle assertion.
	for _, state := range []string{"starting", "running"} {
		retrySlotState = strings.ReplaceAll(ownedSlotState, "starting", state)
		if _, e = adapter.CreatePoolValidationGateway(ctx, scope); e != nil {
			t.Fatal("same owned probe could not resume startup", state, e)
		}
	}
	retrySlotState = ""
	if e = adapter.DeletePoolValidationGateway(ctx, scope, response.GatewayID); e != nil {
		t.Fatal(e)
	}
	if calls != beforeValid+28 {
		t.Fatal(calls)
	}
	cms, _ := kube.CoreV1().ConfigMaps("runtime").List(ctx, metav1.ListOptions{})
	secrets, _ := kube.CoreV1().Secrets("runtime").List(ctx, metav1.ListOptions{})
	if len(cms.Items) != 0 || (len(secrets.Items) != 1 || secrets.Items[0].Name != "clawmanager-lite-agent-tls") {
		t.Fatal("probe published business assignments or credentials")
	}

	// Exercise the actual rollout dispatch, including authenticated health,
	// scoped create and teardown, rather than testing the adapter in isolation.
	beforeController := calls
	dep := &replacementDeployment{probe: k8s.RuntimeValidationStatus{Ready: true, UID: string(d.UID), PodUID: string(pod.UID), Endpoint: scope.Endpoint, Protocol: "container-slots-v1"}}
	r := models.RuntimeRollout{ID: 21, RuntimeType: RuntimeTypeOpenClaw, Status: "running", TargetImageRef: image}
	repo := &checkpointRepo{fakeRuntimeRolloutRepo: fakeRuntimeRolloutRepo{rollouts: map[int64]*models.RuntimeRollout{21: &r}}}
	scheduler := &RuntimeScheduler{rolloutRepo: repo, agentClient: client, deployments: dep, workspaceRoot: "/workspaces"}
	p := &poolReplacementProgress{Kind: "openclaw-pool-replacement", Version: 2, Stage: "validate", StageStarted: time.Now(), Targets: []k8s.RuntimeRolloutTarget{{Namespace: "runtime", Name: "source", UID: "source-uid", Image: image}}}
	if e = scheduler.advanceOpenClawPoolReplacement(ctx, r, p); e != nil || p.ProbePodUID != scope.PodUID || p.ProbeDeploymentUID != scope.DeploymentUID || calls != beforeController {
		t.Fatal("probe identity was not frozen before native startup", e)
	}
	if e = scheduler.advanceOpenClawPoolReplacement(ctx, r, p); e != nil || p.ProbeIndex != 1 || calls != beforeController+11 || !dep.stopped {
		t.Fatal("native rollout validation dispatch failed", p, e)
	}
	protectedCalls := calls
	// Normal scheduler admission fields are tolerated, while every execution and
	// storage mutation is rejected before authenticated HTTP reaches a worker.
	pristine := pod.DeepCopy()
	zero := int32(0)
	policy := corev1.PreemptLowerPriority
	seconds := int64(300)
	pod.Spec.NodeName = "assigned-node"
	defaultLinks := true
	pod.Spec.EnableServiceLinks = &defaultLinks
	pod.Spec.DeprecatedServiceAccount = pod.Spec.ServiceAccountName
	pod.Spec.Priority = &zero
	pod.Spec.PreemptionPolicy = &policy
	pod.Spec.Tolerations = []corev1.Toleration{{Key: "node.kubernetes.io/not-ready", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute, TolerationSeconds: &seconds}}
	kube.CoreV1().Pods("runtime").Update(ctx, pod, metav1.UpdateOptions{})
	if _, e = k8s.VerifyPoolValidationScope(ctx, kube, scope); e != nil {
		t.Fatal("normal API-assigned fields were rejected", e)
	}
	// Kubernetes converts the trusted legacy beta annotations into typed
	// profiles on real Pods, without changing the original Deployment template.
	for i := 1; i < len(pod.Spec.Containers); i++ {
		pod.Spec.Containers[i].SecurityContext.AppArmorProfile = &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeUnconfined}
	}
	kube.CoreV1().Pods("runtime").Update(ctx, pod, metav1.UpdateOptions{})
	if _, e = k8s.VerifyPoolValidationScope(ctx, kube, scope); e != nil {
		t.Fatal("Kubernetes materialized AppArmor directive was rejected", e)
	}
	for _, kind := range []string{"business-volume", "host-users", "shared-processes", "command", "args", "init", "env-from", "volume-device", "pod-security", "security-annotation", "wrong-pod-uid", "wrong-source-uid", "serving-pool", "apparmor-profile-conflict", "apparmor-annotation-conflict"} {
		t.Run(kind, func(t *testing.T) {
			candidate := pristine.DeepCopy()
			candidateScope := scope
			yes := true
			switch kind {
			case "business-volume":
				candidate.Spec.Volumes[0].VolumeSource = corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "business"}}
			case "host-users":
				candidate.Spec.HostUsers = &yes
			case "shared-processes":
				candidate.Spec.ShareProcessNamespace = &yes
			case "command":
				candidate.Spec.Containers[1].Command = []string{"/bin/sh", "-c", "unapproved"}
			case "args":
				candidate.Spec.Containers[1].Args = []string{"unapproved"}
			case "init":
				candidate.Spec.InitContainers = append(candidate.Spec.InitContainers, corev1.Container{Name: "unapproved", Image: image})
			case "env-from":
				candidate.Spec.Containers[1].EnvFrom = []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "business"}}}}
			case "volume-device":
				candidate.Spec.Containers[1].VolumeDevices = []corev1.VolumeDevice{{Name: "business", DevicePath: "/business"}}
			case "pod-security":
				candidate.Spec.SecurityContext = &corev1.PodSecurityContext{RunAsNonRoot: &yes}
			case "security-annotation":
				candidate.Annotations["container.apparmor.security.beta.kubernetes.io/runtime"] = "unconfined"
			case "wrong-pod-uid":
				candidateScope.PodUID = "wrong"
			case "wrong-source-uid":
				candidateScope.SourceUID = "wrong"
			case "apparmor-profile-conflict":
				candidate.Spec.Containers[1].SecurityContext.AppArmorProfile = &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeRuntimeDefault}
			case "apparmor-annotation-conflict":
				candidate.Annotations["container.apparmor.security.beta.kubernetes.io/slot-0"] = "runtime/default"
			case "serving-pool":
				changed := d.DeepCopy()
				changed.Labels["clawmanager.io/scheduling-enabled"] = "true"
				kube.AppsV1().Deployments("runtime").Update(ctx, changed, metav1.UpdateOptions{})
			}
			kube.CoreV1().Pods("runtime").Update(ctx, candidate, metav1.UpdateOptions{})
			if _, e = adapter.CreatePoolValidationGateway(ctx, candidateScope); e == nil || calls != protectedCalls {
				t.Fatal("unapproved execution reached native validation", e)
			}
			kube.AppsV1().Deployments("runtime").Update(ctx, d, metav1.UpdateOptions{})
		})
	}
}
