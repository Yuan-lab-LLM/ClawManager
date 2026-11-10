package services

import (
	"clawreef/internal/runtimeauth"
	"clawreef/internal/services/k8s"
	"context"
	"fmt"
	"net/url"
)

type nativePoolValidationClient interface {
	CreatePoolValidationGateway(context.Context, k8s.RuntimePoolValidationScope) (*RuntimeAgentCreateGatewayResponse, error)
	DeletePoolValidationGateway(context.Context, k8s.RuntimePoolValidationScope, string) error
}

func (c *slotRuntimeAgentClient) CreatePoolValidationGateway(ctx context.Context, scope k8s.RuntimePoolValidationScope) (*RuntimeAgentCreateGatewayResponse, error) {
	if scope.Namespace != c.namespace || c.security == nil {
		return nil, fmt.Errorf("isolated validation authority unavailable")
	}
	if _, err := k8s.VerifyPoolValidationScope(ctx, c.kube, scope); err != nil {
		return nil, err
	}
	// This identity has no SQL instance, provider routing or business assignment.
	// The same sanitizer and native slot supervisor enforce its private network.
	req := RuntimeAgentCreateGatewayRequest{InstanceID: poolProbeInstanceID, UserID: 1, Generation: 1, AgentType: RuntimeTypeOpenClaw, PodUID: scope.PodUID, WorkspacePath: RuntimeWorkspacePathWithRoot(scope.WorkspaceRoot, RuntimeTypeOpenClaw, 1, poolProbeInstanceID), GatewayPort: 20000, PortRange: RuntimeAgentPortRange{Start: 20000, End: 20002}, UID: RuntimeLinuxID(1), GID: RuntimeLinuxID(1), MemoryMB: 512, DiskQuotaMB: 64, Environment: map[string]string{"CLAWMANAGER_LLM_BASE_URL": "http://127.0.0.1:9/v1", "CLAWMANAGER_LLM_API_KEY": "isolated-validation-only", "CLAWMANAGER_LLM_MODEL": "[\"auto\"]", "CLAWMANAGER_OPENCLAW_SCHEDULED_TASKS_JSON": "[]"}}
	credential := c.security.nativeCredential(runtimeauth.Identity{InstanceID: req.InstanceID, UserID: req.UserID, Generation: req.Generation, PodUID: scope.PodUID})
	req, err := prepareIsolatedRuntimeEnvironment(req, credential)
	if err != nil {
		return nil, err
	}
	return c.runtimeAgentHTTPClient.createGateway(ctx, scope.Endpoint, req)
}
func (c *slotRuntimeAgentClient) DeletePoolValidationGateway(ctx context.Context, scope k8s.RuntimePoolValidationScope, gatewayID string) error {
	if scope.Namespace != c.namespace || gatewayID != fmt.Sprintf("gw-%d-1", poolProbeInstanceID) {
		return fmt.Errorf("validation stop identity changed")
	}
	if _, err := k8s.VerifyPoolValidationScope(ctx, c.kube, scope); err != nil {
		return err
	}
	var receipt struct {
		GatewayID string `json:"gateway_id"`
		Stopped   bool   `json:"stopped"`
		Confirmed bool   `json:"confirmed"`
		Unmounted bool   `json:"unmounted"`
	}
	if err := c.do(ctx, "DELETE", scope.Endpoint, "/v1/gateways/"+url.PathEscape(gatewayID), nil, &receipt); err != nil {
		return err
	}
	if receipt.GatewayID != gatewayID || !receipt.Stopped || !receipt.Confirmed || !receipt.Unmounted {
		return fmt.Errorf("validation stop lacks matching native teardown receipt")
	}
	return nil
}
