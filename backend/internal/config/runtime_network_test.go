package config

import (
	"strings"
	"testing"
)

func TestLoadRuntimeNetworkIsolationRequiresScopedConfiguration(t *testing.T) {
	for _, scenario := range []string{"valid", "no-slots", "no-userns", "weak-control", "invalid-flag", "no-pod-cidrs", "invalid-pod-cidrs", "ambiguous-pod-cidrs"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("RUNTIME_NETWORK_ISOLATION", "true")
			t.Setenv("RUNTIME_NETWORK_POD_CIDRS", "10.244.0.0/16,fd00:244::/48")
			t.Setenv("RUNTIME_CONTAINER_SLOTS", "4")
			t.Setenv("RUNTIME_POD_USER_NAMESPACES", "true")
			t.Setenv("RUNTIME_AGENT_CONTROL_TOKEN", strings.Repeat("c", 32))
			switch scenario {
			case "no-slots":
				t.Setenv("RUNTIME_CONTAINER_SLOTS", "0")
			case "no-userns":
				t.Setenv("RUNTIME_POD_USER_NAMESPACES", "false")
			case "weak-control":
				t.Setenv("RUNTIME_AGENT_CONTROL_TOKEN", "weak")
			case "invalid-flag":
				t.Setenv("RUNTIME_NETWORK_ISOLATION", "typo")
			case "no-pod-cidrs":
				t.Setenv("RUNTIME_NETWORK_POD_CIDRS", "")
			case "invalid-pod-cidrs":
				t.Setenv("RUNTIME_NETWORK_POD_CIDRS", "typo")
			case "ambiguous-pod-cidrs":
				t.Setenv("RUNTIME_NETWORK_POD_CIDRS", "10.244.0.1/16")
			}
			cfg, err := Load()
			if scenario != "valid" {
				if err == nil {
					t.Fatal("accepted invalid network isolation config")
				}
				return
			}
			if err != nil || !cfg.Runtime.NetworkIsolation || cfg.Runtime.ContainerSlots != 4 {
				t.Fatalf("config lost: %v", err)
			}
		})
	}
}
