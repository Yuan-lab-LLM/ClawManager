package services

import (
	"context"
	"strings"
	"testing"
)

// Use the deployed runtime's literal identity rather than the control-plane
// constants: otherwise an old backend can pass tests against its own old pin.
func TestHermesDesktopAcceptsDeployedV0225Release(t *testing.T) {
	s := desktopFixture(t, "http://127.0.0.1:9000")
	accepted := false
	s.config.Agent.(*desktopAgent).health.Capabilities.HermesDesktopWeb = &HermesDesktopRuntimeCapability{
		ContractVersion: 2, Enabled: true,
		HermesRef: "v0.22.5", HermesCommit: "bf0203a794a07aac3ff764972ab9087baa4e4f4e",
		RPCProtocol: "hermes-jsonrpc-v1", BackendMode: "serve", AuthMode: "password-cookie",
		ArtifactsVerified: true, ReleaseAccepted: &accepted, PayloadSHA256: strings.Repeat("a", 64),
	}
	d, err := s.Describe(context.Background(), 45, 123)
	if err != nil || !d.Available {
		t.Fatalf("deployed v0.22.5 must be supported: descriptor=%+v err=%v", d, err)
	}
}
