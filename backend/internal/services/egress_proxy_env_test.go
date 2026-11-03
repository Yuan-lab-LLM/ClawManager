package services

import (
	"net/url"
	"strings"
	"testing"

	"clawreef/internal/egresspolicy"
	"clawreef/internal/models"
)

func TestEgressProxyEnvAuthenticatesPodAndPooledGatewayClients(t *testing.T) {
	t.Setenv("CLAWMANAGER_EGRESS_PROXY_URL", "http://egress.example:3128")
	t.Setenv("CLAWMANAGER_NO_PROXY", "localhost,gateway.example")
	token := "gateway-token"
	instance := &models.Instance{ID: 8, Type: "openclaw", AccessToken: &token}
	for _, name := range []string{"pod", "pooled gateway"} {
		t.Run(name, func(t *testing.T) {
			var env map[string]string
			var err error
			if name == "pod" {
				env, err = buildInstancePodEnv(instance, nil, nil, nil)
			} else {
				env, err = buildInstanceGatewayEnv(instance, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
				u, err := url.Parse(env[key])
				if err != nil || u.User == nil {
					t.Fatalf("%s must provide proxy credentials", key)
				}
				password, ok := u.User.Password()
				if !ok || u.User.Username() != "8" || password != egresspolicy.InstanceProxyPassword(8, token) {
					t.Fatalf("%s credentials do not identify this gateway", key)
				}
				if strings.Contains(env[key], token) || u.Host != "egress.example:3128" {
					t.Fatalf("%s must use a proxy-only credential and preserve destination", key)
				}
			}
			if env["NO_PROXY"] != "localhost,gateway.example" || env["no_proxy"] != env["NO_PROXY"] || env["CLAWMANAGER_EGRESS_INSTANCE_ID"] != "8" {
				t.Fatal("proxy bypass or identity environment changed")
			}
		})
	}
}

func TestEgressProxyEnvOverridesRetainManagedAuthentication(t *testing.T) {
	t.Setenv("CLAWMANAGER_EGRESS_PROXY_URL", "http://egress.example:3128")
	token := "gateway-token"
	for _, builder := range []string{"pod", "pooled gateway"} {
		for _, value := range []string{"http://egress.example:3128", "http://EGRESS.EXAMPLE.:3128", "http://9:stale-credential@egress.example:3128"} {
			t.Run(builder+"/"+value, func(t *testing.T) {
				overrides := map[string]string{"NO_PROXY": "custom.example", "no_proxy": "custom.example"}
				for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
					overrides[key] = value
				}
				raw, err := marshalEnvironmentOverrides(overrides)
				if err != nil {
					t.Fatal(err)
				}
				instance := &models.Instance{ID: 8, Type: "openclaw", AccessToken: &token, EnvironmentOverridesJSON: raw}
				var env map[string]string
				if builder == "pod" {
					env, err = buildInstancePodEnv(instance, nil, nil, nil)
				} else {
					env, err = buildInstanceGatewayEnv(instance, nil)
				}
				if err != nil {
					t.Fatal(err)
				}
				for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
					u, err := url.Parse(env[key])
					if err != nil || u.User == nil {
						t.Fatalf("%s lost proxy identity after overrides", key)
					}
					password, _ := u.User.Password()
					if u.User.Username() != "8" || password != egresspolicy.InstanceProxyPassword(8, token) {
						t.Fatalf("%s retained wrong gateway credentials", key)
					}
				}
				if env["NO_PROXY"] != "custom.example" || env["no_proxy"] != "custom.example" {
					t.Fatal("unrelated proxy override changed")
				}
			})
		}
	}
}

func TestEgressProxyEnvDoesNotAttachCredentialsToOtherEndpoints(t *testing.T) {
	t.Setenv("CLAWMANAGER_EGRESS_PROXY_URL", "http://egress.example:3128")
	token := "gateway-token"
	for _, value := range []string{"http://external-user:external-password@external.example:3128", "http://egress.example.attacker:3128", "http://egress.example:3129", "https://egress.example:3128"} {
		t.Run(value, func(t *testing.T) {
			raw, err := marshalEnvironmentOverrides(map[string]string{"HTTP_PROXY": value, "http_proxy": value})
			if err != nil {
				t.Fatal(err)
			}
			env, err := buildInstanceGatewayEnv(&models.Instance{ID: 8, Type: "openclaw", AccessToken: &token, EnvironmentOverridesJSON: raw}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if env["HTTP_PROXY"] != value || env["http_proxy"] != value {
				t.Fatal("credentials were attached to another endpoint")
			}
		})
	}
}

func TestEgressProxyEnvPreservesExplicitUpstreamCredentials(t *testing.T) {
	t.Setenv("CLAWMANAGER_EGRESS_PROXY_URL", "http://external-user:external-password@proxy.example:3128")
	token := "gateway-token"
	env := withInstanceProxyEnv(&models.Instance{ID: 8, Type: "openclaw", AccessToken: &token}, nil)
	if env["HTTP_PROXY"] != "http://external-user:external-password@proxy.example:3128" {
		t.Fatal("explicit upstream credentials were replaced")
	}
}
