package services

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// HermesDesktopRuntimeCapability is an additive Runtime Agent V2 health
// contract. An absent or unknown contract is unsupported, never inferred from
// image names or from a successful TCP connection.
type HermesDesktopRuntimeCapability struct {
	ContractVersion   int    `json:"contract_version"`
	Enabled           bool   `json:"enabled"`
	HermesRef         string `json:"hermes_ref"`
	HermesCommit      string `json:"hermes_commit"`
	RPCProtocol       string `json:"rpc_protocol"`
	BackendMode       string `json:"backend_mode"`
	AuthMode          string `json:"auth_mode"`
	ArtifactsVerified bool   `json:"artifacts_verified"`
	ReleaseAccepted   *bool  `json:"release_accepted,omitempty"`
	PayloadSHA256     string `json:"payload_sha256,omitempty"`
}

// Version 1 was emitted only by signed, accepted releases. Version 2 reports
// verified protocol support separately from signed release acceptance, so the
// deployed UI can be exercised before an integration release is signed.
// Both versions still require normal control authentication and gateway login.
func (c *HermesDesktopRuntimeCapability) compatible() bool {
	if c == nil || !c.Enabled || c.HermesRef != HermesDesktopRef || c.HermesCommit != HermesDesktopCommit || c.RPCProtocol != "hermes-jsonrpc-v1" || c.BackendMode != "dashboard" || c.AuthMode != "password-cookie" {
		return false
	}
	switch c.ContractVersion {
	case 1:
		return true
	case 2:
		digest, err := hex.DecodeString(c.PayloadSHA256)
		return c.ArtifactsVerified && c.ReleaseAccepted != nil && err == nil && len(digest) == 32 && c.PayloadSHA256 == strings.ToLower(c.PayloadSHA256)
	default:
		return false
	}
}

type RuntimeAgentHealthCapabilities struct {
	Capabilities struct {
		HermesDesktopWeb *HermesDesktopRuntimeCapability `json:"hermes_desktop_web,omitempty"`
	} `json:"capabilities"`
}

// Kept optional so older runtime clients and existing control-plane mocks
// continue implementing RuntimeAgentClient unchanged.
type HermesDesktopCapabilityClient interface {
	HealthCapabilities(context.Context, string) (*RuntimeAgentHealthCapabilities, error)
}

type hermesDesktopRedisReadiness interface {
	Ping(context.Context) error
}

type hermesDesktopLeaseStore interface {
	Get(context.Context, string) (string, bool, error)
	Set(context.Context, string, string, time.Duration) error
	SetPersistentNX(context.Context, string, string) (bool, error)
}

func (b *redisBus) SetPersistentNX(ctx context.Context, key, value string) (bool, error) {
	reply, err := b.do(ctx, "SET", key, value, "NX")
	if err != nil {
		return false, err
	}
	if reply == nil {
		return false, nil
	}
	if reply != "OK" {
		return false, fmt.Errorf("unexpected Redis SET NX response")
	}
	return true, nil
}

// Optional read-only probe: feature discovery must not create Redis keys.
func (b *redisBus) Ping(ctx context.Context) error {
	reply, err := b.do(ctx, "PING")
	if err != nil {
		return err
	}
	if reply != "PONG" {
		return fmt.Errorf("unexpected Redis PING response")
	}
	return nil
}

func (c *runtimeAgentHTTPClient) HealthCapabilities(ctx context.Context, endpoint string) (*RuntimeAgentHealthCapabilities, error) {
	var response RuntimeAgentHealthCapabilities
	if err := c.do(ctx, "GET", endpoint, "/v1/health", nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}
