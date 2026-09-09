package services

import (
	"context"
	"strings"
	"testing"
)

func TestHermesDesktopVerifiedProtocolContract(t *testing.T) {
	for _, tc := range []struct {
		name      string
		modify    func(*HermesDesktopRuntimeCapability)
		available bool
	}{
		{"verified unsigned release", nil, true},
		{"verified accepted release", func(c *HermesDesktopRuntimeCapability) { accepted := true; c.ReleaseAccepted = &accepted }, true},
		{"legacy accepted contract", func(c *HermesDesktopRuntimeCapability) {
			c.ContractVersion = 1
			c.ReleaseAccepted = nil
			c.ArtifactsVerified = false
			c.PayloadSHA256 = ""
		}, true},
		{"missing artifact verification", func(c *HermesDesktopRuntimeCapability) { c.ArtifactsVerified = false }, false},
		{"missing acceptance status", func(c *HermesDesktopRuntimeCapability) { c.ReleaseAccepted = nil }, false},
		{"missing payload", func(c *HermesDesktopRuntimeCapability) { c.PayloadSHA256 = "" }, false},
		{"invalid payload", func(c *HermesDesktopRuntimeCapability) { c.PayloadSHA256 = strings.Repeat("z", 64) }, false},
		{"uppercase payload", func(c *HermesDesktopRuntimeCapability) { c.PayloadSHA256 = strings.Repeat("A", 64) }, false},
		{"unknown contract", func(c *HermesDesktopRuntimeCapability) { c.ContractVersion = 3 }, false},
		{"wrong revision", func(c *HermesDesktopRuntimeCapability) { c.HermesCommit = "other" }, false},
		{"no authentication", func(c *HermesDesktopRuntimeCapability) { c.AuthMode = "none" }, false},
		{"disabled", func(c *HermesDesktopRuntimeCapability) { c.Enabled = false }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := desktopFixture(t, "http://127.0.0.1:9000")
			c := s.config.Agent.(*desktopAgent).health.Capabilities.HermesDesktopWeb
			accepted := false
			c.ContractVersion, c.ArtifactsVerified, c.ReleaseAccepted, c.PayloadSHA256 = 2, true, &accepted, strings.Repeat("a", 64)
			if tc.modify != nil {
				tc.modify(c)
			}
			d, err := s.Describe(context.Background(), 45, 123)
			if err != nil || d.Available != tc.available {
				t.Fatalf("descriptor=%+v err=%v", d, err)
			}
			if !tc.available && d.Reason != "runtime_capability_unsupported" {
				t.Fatalf("reason=%s", d.Reason)
			}
		})
	}
}
