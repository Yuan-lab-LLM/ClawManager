package services

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"clawreef/internal/runtimeauth"
	k8sservice "clawreef/internal/services/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type securitySlotInstances struct {
	securityInstances
	protections    int
	failProtection bool
}

func (s *securitySlotInstances) ProtectRuntimeGeneration(ctx context.Context, id, generation int, profile string) error {
	if s.failProtection {
		return errors.New("durable protection unavailable")
	}
	if s.instance.ID != id || s.instance.RuntimeGeneration != generation || s.instance.Status != "creating" || (s.instance.RuntimeSecurityProfile != "" && s.instance.RuntimeSecurityProfile != profile) {
		return errors.New("CAS failed")
	}
	s.instance.RuntimeSecurityProfile = profile
	s.protections++
	return nil
}

func TestProtectedSlotSanitizesTeamSecretsWithoutTeamID(t *testing.T) {
	for _, teamID := range []string{"", "12"} {
		a, _, kube, _, pod := securityFixture(t)
		client := NewSlotRuntimeAgentClientWithHTTPClient("control", kube, "test", &http.Client{}).(*slotRuntimeAgentClient)
		client.security = a
		env := map[string]string{"CLAWMANAGER_TEAM_ID": teamID, "CLAWMANAGER_TEAM_TOKEN": "team-secret-sentinel", "CLAWMANAGER_TEAM_REDIS_URL": "redis://master-secret-sentinel", "TEAM_REDIS_URL": "redis://master-secret-sentinel", "REDIS_URL": "redis://master-secret-sentinel"}
		req, err := client.prepareSecurity(context.Background(), string(pod.UID), RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 3, Generation: 2, AgentType: "opencode", Environment: env})
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"CLAWMANAGER_TEAM_TOKEN", "CLAWMANAGER_TEAM_REDIS_URL", "TEAM_REDIS_URL", "REDIS_URL"} {
			if strings.Contains(req.Environment[key], "secret-sentinel") {
				t.Fatalf("platform secret retained without Team identity: %s", key)
			}
		}
		if env["CLAWMANAGER_TEAM_TOKEN"] != "team-secret-sentinel" {
			t.Fatal("mutated caller environment")
		}
	}
}

func TestProtectedSlotPreservesRedisDatabaseWithoutCredentials(t *testing.T) {
	a, _, kube, _, pod := securityFixture(t)
	client := NewSlotRuntimeAgentClientWithHTTPClient("control", kube, "test", &http.Client{}).(*slotRuntimeAgentClient)
	client.security = a
	for _, database := range []string{"0", "3", "15"} {
		req, err := client.prepareSecurity(context.Background(), string(pod.UID), RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 3, Generation: 2, AgentType: "opencode", Environment: map[string]string{"CLAWMANAGER_TEAM_ID": "12", "CLAWMANAGER_TEAM_REDIS_URL": "rediss://admin:sentinel@private-redis:6379/" + database}})
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"CLAWMANAGER_TEAM_REDIS_URL", "TEAM_REDIS_URL", "REDIS_URL"} {
			if req.Environment[key] != "redis://clawmanager-local-services.invalid:31283/"+database {
				t.Fatalf("native Redis database lost for %s", key)
			}
		}
	}
}

func TestProtectedSlotPersistenceFailurePreventsLaunch(t *testing.T) {
	a, instances, kube, cm, pod := securityFixture(t)
	instances.instance.Status, instances.instance.RuntimeSecurityProfile = "creating", ""
	source := &securitySlotInstances{securityInstances: *instances, failProtection: true}
	a.instances = source
	if err := kube.CoreV1().ConfigMaps("test").Delete(context.Background(), cm.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := NewSlotRuntimeAgentClientWithHTTPClient("control", kube, "test", &http.Client{Transport: slotRoundTripper(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("must not launch") })})
	if err := ConfigureSlotRuntimeSecurity(client, a); err != nil {
		t.Fatal(err)
	}
	_, err := client.CreateGateway(context.Background(), "https://"+pod.Status.PodIP+":19090", RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 3, Generation: 2, AgentType: "opencode", GatewayPort: 20000})
	if err == nil || calls != 0 || source.instance.RuntimeSecurityProfile != "" {
		t.Fatal("failed durable protection allowed launch")
	}
}

func TestProtectedSlotPublishesSafeEnvironmentBeforeStarting(t *testing.T) {
	a, instances, kube, cm, pod := securityFixture(t)
	instances.instance.Status = "creating"
	instances.instance.RuntimeSecurityProfile = ""
	source := &securitySlotInstances{securityInstances: *instances}
	a.instances = source
	if err := kube.CoreV1().ConfigMaps("test").Delete(context.Background(), cm.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	var observed RuntimeAgentCreateGatewayRequest
	client := NewSlotRuntimeAgentClientWithHTTPClient("control", kube, "test", &http.Client{Transport: slotRoundTripper(func(r *http.Request) (*http.Response, error) {
		if source.instance.RuntimeSecurityProfile != RuntimeSecurityProfile {
			t.Error("native launch preceded sticky protection")
		}
		if err := jsonDecodeSlot(r, &observed); err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"gateway_id":"gw-7-2","gateway_port":20000}`))}, nil
	})})
	if err := ConfigureSlotRuntimeSecurity(client, a); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"CLAWMANAGER_LLM_API_KEY": "long-lived-instance-secret", "OPENAI_API_KEY": "long-lived-instance-secret", "CLAWMANAGER_INSTANCE_TOKEN": "long-lived-instance-secret", "OPENCODE_SERVER_PASSWORD": "long-lived-instance-secret", "CLAWMANAGER_AGENT_BOOTSTRAP_TOKEN": "bootstrap-secret", "PROJECT_VALUE": "keep"}
	req := RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 3, Generation: 2, AgentType: "opencode", GatewayPort: 20000, Environment: env}
	if _, err := client.CreateGateway(context.Background(), "https://"+pod.Status.PodIP+":19090", req); err != nil {
		t.Fatal(err)
	}
	if observed.SecurityProfile != RuntimeSecurityProfile || source.protections != 1 || observed.PodUID != string(pod.UID) {
		t.Fatal("protected identity not published")
	}
	if observed.Environment["CLAWMANAGER_LLM_API_KEY"] != "cm-local-model" || observed.Environment["PROJECT_VALUE"] != "keep" || observed.Environment["CLAWMANAGER_AGENT_BOOTSTRAP_TOKEN"] != "" {
		t.Fatal("unsafe native bootstrap")
	}
	if observed.Environment["CLAWMANAGER_LLM_BASE_URL"] != "http://clawmanager-local-model.invalid:31281/api/v1/gateway/llm" {
		t.Fatal("model endpoint does not reach the fixed parent broker port")
	}
	for _, key := range []string{"NO_PROXY", "no_proxy"} {
		for _, host := range []string{"clawmanager-local-model.invalid", "clawmanager-local-services.invalid", "clawmanager-local-egress.invalid"} {
			if !strings.Contains(","+observed.Environment[key]+",", ","+host+",") {
				t.Fatal("managed local service would be forwarded through public proxy")
			}
		}
	}
	credential := a.nativeCredential(runtimeauth.Identity{InstanceID: 7, UserID: 3, Generation: 2, PodUID: string(pod.UID)})
	if observed.Environment["CLAWMANAGER_INSTANCE_TOKEN"] != credential || observed.Environment["OPENCODE_SERVER_PASSWORD"] != credential {
		t.Fatal("native credential not separated")
	}
	if env["OPENAI_API_KEY"] != "long-lived-instance-secret" {
		t.Fatal("mutated caller environment")
	}
	cm, err := kube.CoreV1().ConfigMaps("test").Get(context.Background(), "lite-instance-7", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cm.Data["request"], `"security_profile":"isolated-network-v1"`) {
		t.Fatal("profile absent from authority")
	}
}

func TestLegacyHTTPClientCannotLaunchProtectedGeneration(t *testing.T) {
	calls := 0
	client := NewRuntimeAgentClientWithHTTPClient("control", &http.Client{Transport: slotRoundTripper(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("must not dial") })})
	if _, err := client.CreateGateway(context.Background(), "https://10.2.0.3:19090", RuntimeAgentCreateGatewayRequest{SecurityProfile: RuntimeSecurityProfile}); err == nil || calls != 0 {
		t.Fatal("protected generation reached legacy launcher")
	}
}

func TestProtectedSlotCannotDowngradeOrRevokeBeforeSourceStop(t *testing.T) {
	for _, scenario := range []string{"legacy-target", "source-still-running"} {
		t.Run(scenario, func(t *testing.T) {
			a, instances, kube, cm, pod := securityFixture(t)
			instances.instance.Status = "creating"
			if scenario == "source-still-running" {
				instances.instance.RuntimeSecurityProfile = ""
			}
			source := &securitySlotInstances{securityInstances: *instances}
			a.instances = source
			if err := kube.CoreV1().ConfigMaps("test").Delete(context.Background(), cm.Name, metav1.DeleteOptions{}); err != nil {
				t.Fatal(err)
			}
			if scenario == "legacy-target" {
				delete(pod.Labels, "clawmanager.io/runtime-security")
				if _, err := kube.CoreV1().Pods("test").Update(context.Background(), pod, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			client := NewSlotRuntimeAgentClientWithHTTPClient("control", kube, "test", &http.Client{Transport: slotRoundTripper(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("must not start") })})
			if err := ConfigureSlotRuntimeSecurity(client, a); err != nil {
				t.Fatal(err)
			}
			if scenario == "source-still-running" {
				old := RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 3, Generation: 1, AgentType: "opencode", PodUID: "old-pod", GatewayPort: 20000}
				data, _ := json.Marshal(old)
				if err := client.(*slotRuntimeAgentClient).assignments.EnsureDesired(context.Background(), k8sservice.RuntimeAssignment{InstanceID: 7, UserID: 3, Generation: 1, RuntimeType: "opencode", PodUID: "old-pod", Endpoint: "https://10.1.0.3:19090", GatewayID: "gw-7-1", Request: data}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := client.CreateGateway(context.Background(), "https://10.2.0.3:19090", RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 3, Generation: 2, AgentType: "opencode", GatewayPort: 20000}); err == nil {
				t.Fatal("accepted unsafe transition")
			}
			if calls != 0 || source.protections != 0 {
				t.Fatal("launched or disabled legacy credentials before stop authority")
			}
		})
	}
}

// Keep transport decoding in this test without exposing production env values.
func jsonDecodeSlot(r *http.Request, out *RuntimeAgentCreateGatewayRequest) error {
	return json.NewDecoder(r.Body).Decode(out)
}
