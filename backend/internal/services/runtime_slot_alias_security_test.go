package services

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestProtectedSlotRetiresManagedDashboardCredentialAliases(t *testing.T) {
	a, instances, kube, _, pod := securityFixture(t)
	instances.instance.Type = "hermes"
	client := NewSlotRuntimeAgentClientWithHTTPClient("control", kube, "test", &http.Client{}).(*slotRuntimeAgentClient)
	client.security = a
	before := map[string]string{
		"CLAWMANAGER_INSTANCE_ACCESS_TOKEN":         "raw-identity-secret-sentinel",
		"HERMES_DASHBOARD_BASIC_AUTH_PASSWORD":      "old-dashboard-secret-sentinel",
		"CLAWMANAGER_DASHBOARD_BASIC_AUTH_PASSWORD": "old-dashboard-secret-sentinel",
		"HERMES_DASHBOARD_BASIC_AUTH_SECRET":        "old-session-secret-sentinel",
		"CLAWMANAGER_LLM_RESOLVED_CONFIG_JSON":      `{"api_key":"old-model-secret-sentinel"}`,
		"USER_THIRD_PARTY_KEY":                      "preserved-user-value",
	}
	req, err := client.prepareSecurity(context.Background(), string(pod.UID), RuntimeAgentCreateGatewayRequest{InstanceID: 7, UserID: 3, Generation: 2, AgentType: "hermes", Environment: before})
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range req.Environment {
		if strings.Contains(value, "secret-sentinel") {
			t.Errorf("managed stale secret retained: %s", key)
		}
	}
	for _, key := range []string{"CLAWMANAGER_INSTANCE_ACCESS_TOKEN", "HERMES_DASHBOARD_BASIC_AUTH_PASSWORD", "CLAWMANAGER_DASHBOARD_BASIC_AUTH_PASSWORD"} {
		if req.Environment[key] != req.Environment["CLAWMANAGER_INSTANCE_TOKEN"] {
			t.Errorf("managed credential alias differs from generation password: %s", key)
		}
	}
	if req.Environment["USER_THIRD_PARTY_KEY"] != "preserved-user-value" || before["HERMES_DASHBOARD_BASIC_AUTH_PASSWORD"] != "old-dashboard-secret-sentinel" {
		t.Fatal("unrelated settings or caller map mutated")
	}
}
