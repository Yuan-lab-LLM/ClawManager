package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"clawreef/internal/runtimeauth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConfigureSlotRuntimeSecurity must run before the client is published to the
// scheduler/reconciler. Protection cannot be hot-swapped on a serving client.
func ConfigureSlotRuntimeSecurity(client RuntimeAgentClient, authority *RuntimeSecurityAuthority) error {
	c, ok := client.(*slotRuntimeAgentClient)
	if !ok || authority == nil || c.security != nil {
		return errors.New("invalid slot security authority")
	}
	if _, ok := authority.instances.(interface {
		ProtectRuntimeGeneration(context.Context, int, int, string) error
	}); !ok {
		return errors.New("slot security requires durable generation protection")
	}
	c.security = authority
	return nil
}

// ConfigureSlotTeamPreviewSecurity runs before any assignment reconciliation.
func ConfigureSlotTeamPreviewSecurity(client RuntimeAgentClient, preview *ManagedTeamPreview) error {
	c, ok := client.(*slotRuntimeAgentClient)
	if !ok || c.security == nil || preview == nil || c.protectTeamPreview != nil {
		return errors.New("invalid slot Team preview protection")
	}
	c.protectTeamPreview = preview.Protect
	return nil
}

// prepareSecurity checks the actual target Pod, then removes platform secrets
// from immutable business bootstrap data. The supervisor resolves local broker
// placeholders after its namespace is ready and before native config rendering.
func (c *slotRuntimeAgentClient) prepareSecurity(ctx context.Context, podUID string, req RuntimeAgentCreateGatewayRequest) (RuntimeAgentCreateGatewayRequest, error) {
	fail := errors.New("runtime security target or generation is unavailable")
	pods, err := c.kube.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{LabelSelector: "clawmanager.io/runtime-protocol=container-slots-v1"})
	if err != nil {
		return req, fail
	}
	profile := ""
	found := false
	for _, pod := range pods.Items {
		if string(pod.UID) == podUID && pod.Namespace == c.namespace {
			if found {
				return req, fail
			}
			found = true
			profile = pod.Labels["clawmanager.io/runtime-security"]
		}
	}
	if !found || (profile != "" && profile != RuntimeSecurityProfile) {
		return req, fail
	}
	if c.security == nil {
		if profile != "" || req.SecurityProfile != "" {
			return req, fail
		}
		return req, nil
	}
	instance, err := c.security.instances.GetByIDContext(ctx, req.InstanceID)
	if err != nil || instance == nil || instance.ID != req.InstanceID || instance.UserID != req.UserID || instance.RuntimeGeneration != req.Generation || instance.Type != req.AgentType {
		return req, fail
	}
	if profile == "" {
		if instance.RuntimeSecurityProfile != "" || req.SecurityProfile != "" {
			return req, fail
		}
		return req, nil
	}
	if instance.InstanceMode != "lite" || instance.RuntimeType != "gateway" || (instance.Status != "creating" && instance.Status != "running") || (instance.RuntimeSecurityProfile != "" && instance.RuntimeSecurityProfile != RuntimeSecurityProfile) || (req.SecurityProfile != "" && req.SecurityProfile != RuntimeSecurityProfile) {
		return req, fail
	}
	if instance.RuntimeSecurityProfile == "" && instance.Status != "creating" {
		return req, fail
	}
	req.SecurityProfile = RuntimeSecurityProfile
	identity := runtimeauth.Identity{InstanceID: req.InstanceID, UserID: req.UserID, Generation: req.Generation, PodUID: podUID}
	credential := c.security.nativeCredential(identity)
	env := make(map[string]string, len(req.Environment)+12)
	for key, value := range req.Environment {
		if (strings.HasPrefix(key, "CLAWMANAGER_AGENT_") && key != "CLAWMANAGER_AGENT_PERSISTENT_DIR") || strings.HasPrefix(key, "RUNTIME_AGENT_") || strings.HasPrefix(key, "RUNTIME_SLOT_") || strings.HasPrefix(key, "KUBERNETES_") || strings.HasPrefix(key, "POD_") {
			continue
		}
		env[key] = value
	}
	for _, key := range []string{"CLAWMANAGER_LLM_API_KEY", "OPENAI_API_KEY"} {
		env[key] = "cm-local-model"
	}
	for _, key := range []string{"CLAWMANAGER_LLM_BASE_URL", "OPENAI_BASE_URL", "OPENAI_API_BASE"} {
		env[key] = "http://clawmanager-local-model.invalid:31281/api/v1/gateway/llm"
	}
	for _, key := range []string{"CLAWMANAGER_INSTANCE_TOKEN", "CLAWMANAGER_INSTANCE_ACCESS_TOKEN", "OPENCLAW_GATEWAY_TOKEN", "CLAWMANAGER_GATEWAY_TOKEN", "OPENCLAW_GATEWAY_PASSWORD"} {
		env[key] = credential
	}
	for _, key := range []string{"HERMES_DASHBOARD_BASIC_AUTH_PASSWORD", "CLAWMANAGER_DASHBOARD_BASIC_AUTH_PASSWORD"} {
		if _, present := env[key]; present || req.AgentType == RuntimeTypeHermes {
			env[key] = credential
		}
	}
	// The native adapters derive session signing and canonical model metadata
	// from this generation's sanitized inputs; never retain stale derived blobs.
	delete(env, "HERMES_DASHBOARD_BASIC_AUTH_SECRET")
	delete(env, "CLAWMANAGER_LLM_RESOLVED_CONFIG_JSON")
	if req.AgentType == RuntimeTypeOpenCode {
		env["OPENCODE_SERVER_PASSWORD"] = credential
	}
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		env[key] = "http://clawmanager-local-egress.invalid:31280"
	}
	localHosts := "localhost,127.0.0.1,::1,clawmanager-local-model.invalid,clawmanager-local-services.invalid,clawmanager-local-egress.invalid"
	env["NO_PROXY"], env["no_proxy"] = localHosts, localHosts
	// Team adapters receive public IDs and fixed local endpoints. Their actual
	// Redis and signing credentials remain in the backend managed-service path.
	if env["CLAWMANAGER_TEAM_ID"] != "" || env["CLAWMANAGER_TEAM_TOKEN"] != "" {
		env["CLAWMANAGER_TEAM_TOKEN"] = "cm-local-team"
	}
	databasePath := ""
	for _, key := range []string{"CLAWMANAGER_TEAM_REDIS_URL", "TEAM_REDIS_URL", "REDIS_URL"} {
		if raw := env[key]; raw != "" {
			parsed, err := url.Parse(raw)
			if err != nil {
				return req, fail
			}
			if parsed.Path != "" && parsed.Path != "/" {
				if !strings.HasPrefix(parsed.Path, "/") {
					return req, fail
				}
				database, err := strconv.ParseUint(strings.TrimPrefix(parsed.Path, "/"), 10, 31)
				if err != nil {
					return req, fail
				}
				candidate := "/" + strconv.FormatUint(database, 10)
				if databasePath != "" && databasePath != candidate {
					return req, fail
				}
				databasePath = candidate
			}
		}
	}
	for _, key := range []string{"CLAWMANAGER_TEAM_REDIS_URL", "TEAM_REDIS_URL", "REDIS_URL"} {
		if env["CLAWMANAGER_TEAM_ID"] != "" || env[key] != "" {
			env[key] = "redis://clawmanager-local-services.invalid:31283" + databasePath
		}
	}
	env["CLAWMANAGER_MANAGED_SERVICES_URL"] = "http://clawmanager-local-services.invalid:31282"
	req.Environment = env
	return req, nil
}

func (c *slotRuntimeAgentClient) protectPublishedGeneration(ctx context.Context, req RuntimeAgentCreateGatewayRequest) error {
	if req.SecurityProfile == "" {
		return nil
	}
	if c.security == nil || req.SecurityProfile != RuntimeSecurityProfile {
		return errors.New("protected slot authority unavailable")
	}
	instance, err := c.security.instances.GetByIDContext(ctx, req.InstanceID)
	if err != nil || instance == nil {
		return errors.New("protected generation unavailable")
	}
	if instance.RuntimeGeneration != req.Generation || instance.UserID != req.UserID {
		return errors.New("protected generation changed")
	}
	if instance.RuntimeSecurityProfile != RuntimeSecurityProfile {
		protector, ok := c.security.instances.(interface {
			ProtectRuntimeGeneration(context.Context, int, int, string) error
		})
		if !ok {
			return errors.New("durable slot protection unavailable")
		}
		if err := protector.ProtectRuntimeGeneration(ctx, req.InstanceID, req.Generation, RuntimeSecurityProfile); err != nil {
			return err
		}
	}
	var environment map[string]string
	if instance.EnvironmentOverridesJSON != nil {
		if len(*instance.EnvironmentOverridesJSON) > 1<<20 || json.Unmarshal([]byte(*instance.EnvironmentOverridesJSON), &environment) != nil {
			return errors.New("protected Team identity unavailable")
		}
	}
	teamID := environment["CLAWMANAGER_TEAM_ID"]
	if teamID != req.Environment["CLAWMANAGER_TEAM_ID"] {
		return errors.New("protected Team identity changed")
	}
	if err := c.security.checkTeamNetwork(ctx, instance); err != nil {
		return errors.New("protected Team network unavailable")
	}
	if teamID != "" {
		if c.protectTeamPreview == nil {
			return errors.New("protected Team preview authority unavailable")
		}
		return c.protectTeamPreview(ctx, runtimeauth.Identity{InstanceID: req.InstanceID, UserID: req.UserID, Generation: req.Generation, PodUID: req.PodUID})
	}
	return nil
}
