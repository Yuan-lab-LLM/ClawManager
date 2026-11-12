package k8s

import (
	corev1 "k8s.io/api/core/v1"
	"testing"
)

func TestExplicitSlotAppArmorDoesNotChangeCoordinatorOrDefault(t *testing.T) {
	for _, profile := range []string{"", "hxc-rootless"} {
		d := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "pool", Namespace: "runtime", RuntimeType: "openclaw", Image: "image", Replicas: 1, SlotCount: 2, SlotAppArmorProfile: profile, SandboxProfile: RuntimeNFSRootlessProfile, NetworkIsolation: true})
		for _, c := range d.Spec.Template.Spec.Containers {
			if c.Name == "runtime" {
				if c.SecurityContext.AppArmorProfile != nil {
					t.Fatal("coordinator profile changed")
				}
				continue
			}
			if c.Name != "slot-0" && c.Name != "slot-1" {
				continue
			}
			a := c.SecurityContext.AppArmorProfile
			if profile == "" && a != nil {
				t.Fatal("upstream default changed")
			}
			if profile != "" && (a == nil || a.Type != corev1.AppArmorProfileTypeLocalhost || a.LocalhostProfile == nil || *a.LocalhostProfile != profile) {
				t.Fatal("explicit profile not applied to slot")
			}
		}
	}
}
