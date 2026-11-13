package k8s

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func coordinatorStateSource() *appsv1.Deployment {
	d := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "source", Namespace: "tenant", RuntimeType: "openclaw", Image: "old", Replicas: 1, SlotCount: 2, SandboxProfile: RuntimeNFSRootlessProfile, NetworkIsolation: true, AgentTokenSecretName: "agent", WorkspacePVCClaimName: "business"})
	d.UID = "source-uid"
	return d
}

func requireCoordinatorState(t *testing.T, d *appsv1.Deployment) {
	t.Helper()
	p := d.Spec.Template.Spec
	requireEnv(t, p.Containers[0], "RUNTIME_COORDINATOR_SLOT_STATE_ROOT", "/var/lib/clawmanager-slots")
	for i, slot := range p.Containers[1:] {
		name := fmt.Sprintf("slot-%d-state", i)
		path := "/var/lib/clawmanager-slots/" + slot.Name
		matches := 0
		for _, mount := range p.Containers[0].VolumeMounts {
			if mount.MountPath == path {
				matches++
				if mount.Name != name || mount.ReadOnly || mount.SubPath != "" || mount.SubPathExpr != "" {
					t.Fatalf("coordinator state mapping changed: %#v", mount)
				}
			}
		}
		if matches != 1 {
			t.Fatalf("coordinator has %d mounts for %s", matches, slot.Name)
		}
		requireVolumeMount(t, slot, name, "/var/lib/clawmanager-slot")
		requireEnvAbsent(t, slot, "RUNTIME_COORDINATOR_SLOT_STATE_ROOT")
		for _, mount := range slot.VolumeMounts {
			if mount.MountPath == path || mount.Name != name && mount.MountPath == "/var/lib/clawmanager-slot" {
				t.Fatal("slot inherited another slot's private state")
			}
		}
	}
}

func legacyCoordinatorStateSource() *appsv1.Deployment {
	d := coordinatorStateSource()
	c := &d.Spec.Template.Spec.Containers[0]
	removeEnvVar(c, "RUNTIME_COORDINATOR_SLOT_STATE_ROOT")
	kept := c.VolumeMounts[:0]
	for _, mount := range c.VolumeMounts {
		if mount.MountPath != "/var/lib/clawmanager-slots/slot-0" && mount.MountPath != "/var/lib/clawmanager-slots/slot-1" {
			kept = append(kept, mount)
		}
	}
	c.VolumeMounts = kept
	return d
}

func TestCoordinatorStateOnNewNFSRootlessPools(t *testing.T) {
	requireCoordinatorState(t, coordinatorStateSource())
	d := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "ordinary", RuntimeType: "openclaw", Image: "old", SlotCount: 2})
	requireEnvAbsent(t, d.Spec.Template.Spec.Containers[0], "RUNTIME_COORDINATOR_SLOT_STATE_ROOT")
}

func TestCoordinatorStateNativeTargetsPreserveSource(t *testing.T) {
	source := legacyCoordinatorStateSource()
	before := source.DeepCopy()
	target, err := buildUpgradeRuntimePool(source, "tenant", "source", "target", "new", "42", nil)
	if err != nil {
		t.Fatal(err)
	}
	requireCoordinatorState(t, target)
	validation, err := buildPoolValidationDeployment(source, RuntimeRolloutTarget{Namespace: "tenant", Name: "source", UID: "source-uid"}, "validation", "new", "42")
	if err != nil {
		t.Fatal(err)
	}
	requireCoordinatorState(t, validation)
	if !reflect.DeepEqual(source, before) {
		t.Fatal("native target transformation mutated frozen source")
	}
	if !reflect.DeepEqual(target.Spec.Template.Spec.Volumes, source.Spec.Template.Spec.Volumes) {
		t.Fatal("target changed private state or business storage volumes")
	}
}

func TestCoordinatorStateCMRolloutMigratesOnce(t *testing.T) {
	source := legacyCoordinatorStateSource()
	client := fake.NewSimpleClientset(source)
	service := NewRuntimeDeploymentService(client)
	for i := 0; i < 2; i++ {
		if err := service.RolloutImage(context.Background(), "tenant", "source", "new", "", 0, 1); err != nil {
			t.Fatal(err)
		}
		got, err := client.AppsV1().Deployments("tenant").Get(context.Background(), "source", metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		requireCoordinatorState(t, got)
		for index, slot := range got.Spec.Template.Spec.Containers[1:] {
			if !reflect.DeepEqual(slot.VolumeMounts, source.Spec.Template.Spec.Containers[index+1].VolumeMounts) {
				t.Fatal("rollout changed worker state mounts")
			}
		}
		if !reflect.DeepEqual(got.Spec.Template.Spec.Volumes, source.Spec.Template.Spec.Volumes) {
			t.Fatal("rollout changed storage volumes")
		}
	}
}
