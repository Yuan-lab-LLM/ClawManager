package services

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

type RuntimeIdleStopRequest struct {
	InstanceID int    `json:"instance_id"`
	Generation int    `json:"generation"`
	PodUID     string `json:"pod_uid"`
}
type RuntimeIdlePreflight struct {
	DefaultsPolicyVersion string `json:"defaults_policy_version"`
	DefaultsPolicyStatus  string `json:"defaults_policy_status"`

	GatewayID               string `json:"gateway_id"`
	InstanceID              int    `json:"instance_id"`
	Generation              int    `json:"generation"`
	PodUID                  string `json:"pod_uid"`
	ExecutionState          string `json:"execution_state"`
	ExecutionProofSupported bool   `json:"execution_proof_supported"`
	Blocker                 string `json:"blocker"`
}
type RuntimeIdleStopClient interface {
	IdlePreflight(context.Context, string, string, RuntimeIdleStopRequest) (*RuntimeIdlePreflight, error)
	IdleStop(context.Context, string, string, RuntimeIdleStopRequest) error
	ConfirmIdleStop(context.Context, string, string, RuntimeIdleStopRequest) error
}

func (c *slotRuntimeAgentClient) verifyIdleTarget(ctx context.Context, endpoint, gatewayID string, req RuntimeIdleStopRequest) error {
	if c.legacy != nil {
		protected, err := c.protectedInstance(ctx, req.InstanceID)
		if err != nil {
			return err
		}
		if !protected {
			return ErrRuntimeAgentUnsupported
		}
	}

	if req.InstanceID <= 0 || req.Generation <= 0 || req.PodUID == "" || gatewayID != fmt.Sprintf("gw-%d-%d", req.InstanceID, req.Generation) {
		return fmt.Errorf("invalid idle-stop identity")
	}
	if pod, err := c.containerStopPod(ctx, endpoint, req.PodUID); err == nil && pod.Labels["clawmanager.io/sandbox-profile"] == "nfs-rootless-v1" {
		return nil
	}
	uid, err := c.targetPodUID(ctx, endpoint)
	if err != nil {
		return err
	}
	if uid != req.PodUID {
		return fmt.Errorf("idle-stop Pod identity changed")
	}
	return nil
}
func (c *slotRuntimeAgentClient) IdlePreflight(ctx context.Context, endpoint, gatewayID string, req RuntimeIdleStopRequest) (*RuntimeIdlePreflight, error) {
	if err := c.verifyIdleTarget(ctx, endpoint, gatewayID, req); err != nil {
		return nil, err
	}
	var result RuntimeIdlePreflight
	if err := c.do(ctx, http.MethodPost, endpoint, "/v1/gateways/"+url.PathEscape(gatewayID)+"/idle-preflight", req, &result); err != nil {
		return nil, err
	}
	if result.GatewayID != gatewayID || result.InstanceID != req.InstanceID || result.Generation != req.Generation || result.PodUID != req.PodUID {
		return nil, fmt.Errorf("idle preflight identity mismatch")
	}
	return &result, nil
}
func (c *slotRuntimeAgentClient) IdleStop(ctx context.Context, endpoint, gatewayID string, req RuntimeIdleStopRequest) error {
	if err := c.verifyIdleTarget(ctx, endpoint, gatewayID, req); err != nil {
		return err
	}
	if pod, err := c.containerStopPod(ctx, endpoint, req.PodUID); err == nil && pod.Labels["clawmanager.io/sandbox-profile"] == "nfs-rootless-v1" {
		if err = c.assignments.MarkDraining(ctx, endpoint, gatewayID); err != nil {
			return err
		}
		return c.stopContainerGateway(ctx, endpoint, gatewayID, true)
	}
	var receipt struct {
		RuntimeIdleStopRequest
		GatewayID string `json:"gateway_id"`
		Stopped   bool   `json:"stopped"`
		Confirmed bool   `json:"confirmed"`
		Unmounted bool   `json:"unmounted"`
		Graceful  bool   `json:"graceful"`
	}
	if err := c.do(ctx, http.MethodPost, endpoint, "/v1/gateways/"+url.PathEscape(gatewayID)+"/idle-stop", req, &receipt); err != nil {
		return err
	}
	if receipt.GatewayID != gatewayID || receipt.InstanceID != req.InstanceID || receipt.Generation != req.Generation || receipt.PodUID != req.PodUID || !receipt.Stopped || !receipt.Confirmed || !receipt.Unmounted || !receipt.Graceful {
		return fmt.Errorf("idle stop lacks matching graceful teardown receipt")
	}
	return nil
}

// Called only after the matching receipt has been durably stored. Assignment
// reconciliation must never invoke forceful DELETE before that proof exists.
func (c *slotRuntimeAgentClient) ConfirmIdleStop(ctx context.Context, endpoint, gatewayID string, req RuntimeIdleStopRequest) error {
	if err := c.verifyIdleTarget(ctx, endpoint, gatewayID, req); err != nil {
		return err
	}
	if pod, err := c.containerStopPod(ctx, endpoint, req.PodUID); err == nil && pod.Labels["clawmanager.io/sandbox-profile"] == "nfs-rootless-v1" {
		return c.stopContainerGateway(ctx, endpoint, gatewayID, true)
	}
	if err := c.assignments.MarkDraining(ctx, endpoint, gatewayID); err != nil {
		return err
	}
	return c.assignments.ConfirmStopped(ctx, endpoint, gatewayID)
}
