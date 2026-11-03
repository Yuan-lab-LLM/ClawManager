package services

import (
	"strings"
	"testing"
)

func TestRuntimeServiceNamespaceResolution(t *testing.T) {
	for _, key := range []string{
		"CLAWMANAGER_SYSTEM_NAMESPACE", "K8S_NAMESPACE", "CLAWMANAGER_LLM_GATEWAY_BASE_URL",
		"CLAWMANAGER_EGRESS_PROXY_URL", "CLAWMANAGER_AGENT_CONTROL_BASE_URL", "CLAWMANAGER_NO_PROXY",
		"CLAWMANAGER_TEAM_PREVIEW_ORIGIN", "CLAWMANAGER_LLM_GATEWAY_SERVICE_NAME", "CLAWMANAGER_LLM_GATEWAY_SERVICE",
		"CLAWMANAGER_LLM_GATEWAY_SERVICE_PORT", "CLAWMANAGER_LLM_GATEWAY_PORT",
		"CLAWMANAGER_EGRESS_PROXY_SERVICE_NAME", "CLAWMANAGER_EGRESS_PROXY_SERVICE", "CLAWMANAGER_EGRESS_PROXY_SERVICE_PORT", "CLAWMANAGER_EGRESS_PROXY_PORT",
		"CLAWMANAGER_AGENT_CONTROL_SERVICE_NAME", "CLAWMANAGER_AGENT_CONTROL_SERVICE_PORT",
		"CLAWMANAGER_TEAM_REDIS_SERVICE_NAME", "CLAWMANAGER_TEAM_REDIS_SERVICE", "CLAWMANAGER_TEAM_REDIS_SERVICE_PORT", "CLAWMANAGER_TEAM_REDIS_PORT", "CLAWMANAGER_TEAM_REDIS_DB", "TEAM_REDIS_DB",
	} {
		t.Setenv(key, "")
	}
	for _, tc := range []struct{ name, configured, explicit, want string }{
		{"legacy prefix", "clawreef", "", "clawreef-system"},
		{"full system namespace", "clawmanager-system", "", "clawmanager-system"},
		{"custom full namespace", "tenant-system", "", "tenant-system"},
		{"explicit override", "clawmanager-system", "custom-control", "custom-control"},
		{"explicit suffix preserved", "clawmanager", "custom-system-system", "custom-system-system"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("K8S_NAMESPACE", tc.configured)
			t.Setenv("CLAWMANAGER_SYSTEM_NAMESPACE", tc.explicit)
			gateway := "http://clawmanager-gateway." + tc.want + ".svc.cluster.local:9001"
			proxy := "http://clawmanager-egress-proxy." + tc.want + ".svc.cluster.local:3128"
			for _, check := range []struct {
				name    string
				resolve func() (string, bool)
				want    string
			}{
				{"model gateway", defaultGatewayBaseURL, gateway + "/api/v1/gateway/llm"},
				{"agent control", defaultAgentControlBaseURL, gateway},
				{"egress proxy", defaultEgressProxyURL, proxy},
				{"team preview", defaultTeamPreviewOrigin, proxy},
				{"team redis", defaultTeamRedisServiceURL, "redis://clawmanager-team-redis." + tc.want + ".svc.cluster.local:6379/0"},
			} {
				if got, ok := check.resolve(); !ok || got != check.want {
					t.Errorf("%s = %q (%t), want %q", check.name, got, ok, check.want)
				}
			}
			for _, service := range []string{"clawmanager-frontend", "clawmanager-gateway", "clawmanager-egress-proxy"} {
				want := service + "." + tc.want + ".svc.cluster.local"
				if !strings.Contains(","+defaultNoProxyList()+",", ","+want+",") {
					t.Errorf("NO_PROXY is missing %q", want)
				}
			}
		})
	}
}
