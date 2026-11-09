package services

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"
)

var ErrRuntimeStopUnconfirmed = errors.New("runtime stop is unsupported or unconfirmed; replacement and backup are not permitted")
var ErrRuntimeStopReceiptMismatch = errors.New("runtime stop receipt does not match the pinned process identity")

// Additive interface: old agent clients and ordinary lifecycle calls are not
// upgraded implicitly. An orchestrator must fence placement and drain first.
type RuntimeAgentStopClient interface {
	StopCapabilities(context.Context, string) (*RuntimeAgentStopCapabilities, error)
	ConfirmGatewayStop(context.Context, string, RuntimeAgentStopRequest) (*RuntimeAgentStopReceipt, error)
}

type RuntimeAgentStopCapabilities struct {
	Protocol          int    `json:"protocol"`
	PodUID            string `json:"pod_uid"`
	AgentEpoch        string `json:"agent_epoch"`
	GracefulStop      bool   `json:"graceful_stop"`
	ConfirmationScope string `json:"confirmation_scope"`
	AutomaticUpgrade  bool   `json:"automatic_upgrade"`
	ExpectedPIDGuard  bool   `json:"expected_pid_guard"`
}

type RuntimeAgentStopRequest struct {
	InstanceID     int    `json:"instance_id"`
	UserID         int    `json:"user_id"`
	Generation     int    `json:"generation"`
	GatewayID      string `json:"gateway_id"`
	OperationID    string `json:"operation_id"`
	PodUID         string `json:"pod_uid"`
	AgentEpoch     string `json:"agent_epoch"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	ExpectedPID    int    `json:"expected_pid,omitempty"`
}

type RuntimeAgentStopReceipt struct {
	InstanceID        int       `json:"instance_id"`
	Generation        int       `json:"generation"`
	GatewayID         string    `json:"gateway_id"`
	OperationID       string    `json:"operation_id"`
	PodUID            string    `json:"pod_uid"`
	AgentEpoch        string    `json:"agent_epoch"`
	PID               int       `json:"pid"`
	Confirmed         bool      `json:"confirmed"`
	ConfirmationScope string    `json:"confirmation_scope"`
	ConfirmedAt       time.Time `json:"confirmed_at"`
}

func stopTokenValid(value string) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == 16 && value == hex.EncodeToString(b)
}

func (c *runtimeAgentHTTPClient) StopCapabilities(ctx context.Context, endpoint string) (*RuntimeAgentStopCapabilities, error) {
	var caps RuntimeAgentStopCapabilities
	if err := c.do(ctx, http.MethodGet, endpoint, "/v1/stop-capabilities", nil, &caps); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRuntimeStopUnconfirmed, err)
	}
	if caps.Protocol != 1 || !caps.GracefulStop || caps.PodUID == "" || !stopTokenValid(caps.AgentEpoch) || caps.ConfirmationScope != "process_group" {
		return nil, ErrRuntimeStopUnconfirmed
	}
	return &caps, nil
}

func (c *runtimeAgentHTTPClient) ConfirmGatewayStop(ctx context.Context, endpoint string, req RuntimeAgentStopRequest) (*RuntimeAgentStopReceipt, error) {
	if req.InstanceID <= 0 || req.UserID <= 0 || req.Generation < 0 || req.GatewayID != fmt.Sprintf("gw-%d-%d", req.InstanceID, req.Generation) ||
		!stopTokenValid(req.OperationID) || !stopTokenValid(req.AgentEpoch) || req.PodUID == "" || req.TimeoutSeconds < 1 || req.TimeoutSeconds > 20 || req.ExpectedPID < 0 {
		return nil, ErrRuntimeStopUnconfirmed
	}
	var receipt RuntimeAgentStopReceipt
	if err := c.do(ctx, http.MethodPost, endpoint, "/v1/gateway-stop", req, &receipt); err != nil {
		// Do not fall back to DELETE or pod deletion, even for 404/501.
		return nil, fmt.Errorf("%w: %w", ErrRuntimeStopUnconfirmed, err)
	}
	if !validRuntimeStopReceipt(req, &receipt) {
		return nil, errors.Join(ErrRuntimeStopUnconfirmed, ErrRuntimeStopReceiptMismatch)
	}
	return &receipt, nil
}

func validRuntimeStopReceipt(req RuntimeAgentStopRequest, receipt *RuntimeAgentStopReceipt) bool {
	return receipt != nil && receipt.Confirmed && receipt.PID > 0 && (req.ExpectedPID == 0 || receipt.PID == req.ExpectedPID) && receipt.InstanceID == req.InstanceID && receipt.Generation == req.Generation &&
		receipt.GatewayID == req.GatewayID && receipt.OperationID == req.OperationID && receipt.PodUID == req.PodUID &&
		receipt.AgentEpoch == req.AgentEpoch && receipt.ConfirmationScope == "process_group" && !receipt.ConfirmedAt.IsZero()
}
