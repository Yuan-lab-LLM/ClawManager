package k8s

import (
	"fmt"
	"strings"
	"testing"

	"clawreef/internal/config"
	corev1 "k8s.io/api/core/v1"
)

func TestNetworkIsolatedSlotsKeepConfigurableCapacityAndTrustedCapabilities(t *testing.T) {
	for _, count := range []int{1, 4, 8, 16} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			cfg := config.RuntimePoolConfig{ContainerSlots: count, NetworkIsolation: true, PodUserNamespaces: true, OpenClawImage: "oc", HermesImage: "hm", OpenCodeImage: "code", DeepSeekHarnessImage: "ds"}
			specs, err := RuntimeSlotDeploymentSpecs(cfg, "management")
			if err != nil {
				t.Fatal(err)
			}
			for _, spec := range specs {
				d := BuildRuntimeDeployment(spec)
				pod := d.Spec.Template.Spec
				if len(pod.Containers) != count+1 || d.Spec.Template.Labels["clawmanager.io/runtime-security"] != "isolated-network-v1" {
					t.Fatal("capacity or profile lost")
				}
				for i, c := range pod.Containers {
					caps := map[corev1.Capability]bool{}
					for _, cap := range c.SecurityContext.Capabilities.Add {
						caps[cap] = true
					}
					if i == 0 {
						if caps["SYS_ADMIN"] || caps["NET_ADMIN"] {
							t.Fatal("coordinator received unnecessary network capabilities")
						}
						continue
					}
					if !caps["SYS_ADMIN"] || !caps["NET_ADMIN"] || caps["NET_RAW"] {
						t.Fatal("trusted slot network capability mismatch")
					}
					requireEnv(t, c, "RUNTIME_NETWORK_SECURITY_PROFILE", "isolated-network-v1")
					requireEnv(t, c, "RUNTIME_NETWORK_SLOT_COUNT", fmt.Sprint(count))
					for _, mount := range c.VolumeMounts {
						if strings.Contains(mount.Name, "capabilities") {
							t.Fatal("signing key mounted in runtime")
						}
					}
				}
				if pod.HostNetwork || pod.HostPID || pod.HostIPC || pod.HostUsers == nil || *pod.HostUsers {
					t.Fatal("network isolation requires scoped Pod user namespace")
				}
			}
		})
	}
}

func TestNetworkIsolationRejectsUnscopedOrMissingSlots(t *testing.T) {
	for _, cfg := range []config.RuntimePoolConfig{{NetworkIsolation: true}, {NetworkIsolation: true, ContainerSlots: 4, OpenClawImage: "oc", HermesImage: "hm", OpenCodeImage: "code", DeepSeekHarnessImage: "ds"}} {
		if _, err := RuntimeSlotDeploymentSpecs(cfg, "management"); err == nil {
			t.Fatal("accepted invalid protected pool")
		}
	}
}
