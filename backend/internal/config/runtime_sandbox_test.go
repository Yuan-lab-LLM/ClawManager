package config

import (
	"strings"
	"testing"
)

func TestNFSRootlessProfileRequiresCompleteIsolation(t *testing.T) {
	for _, scenario := range []string{"valid", "unknown", "pod-userns", "no-network", "no-slots", "weak-control"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("RUNTIME_SANDBOX_PROFILE", "nfs-rootless-v1")
			t.Setenv("RUNTIME_CONTAINER_SLOTS", "2")
			t.Setenv("RUNTIME_POD_USER_NAMESPACES", "false")
			t.Setenv("RUNTIME_NETWORK_ISOLATION", "true")
			t.Setenv("RUNTIME_NETWORK_POD_CIDRS", "10.244.0.0/16")
			t.Setenv("RUNTIME_AGENT_CONTROL_TOKEN", strings.Repeat("c", 32))
			switch scenario {
			case "unknown":
				t.Setenv("RUNTIME_SANDBOX_PROFILE", "unknown")
			case "pod-userns":
				t.Setenv("RUNTIME_POD_USER_NAMESPACES", "true")
			case "no-network":
				t.Setenv("RUNTIME_NETWORK_ISOLATION", "false")
			case "no-slots":
				t.Setenv("RUNTIME_CONTAINER_SLOTS", "0")
			case "weak-control":
				t.Setenv("RUNTIME_AGENT_CONTROL_TOKEN", "weak")
			}
			cfg, err := Load()
			if scenario != "valid" {
				if err == nil {
					t.Fatal("accepted incomplete NFS isolation")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Runtime.SandboxProfile != "nfs-rootless-v1" {
				t.Fatal("lost sandbox profile")
			}
		})
	}
}
