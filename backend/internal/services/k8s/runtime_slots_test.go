package k8s

import (
	"clawreef/internal/config"
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"strings"
	"testing"
)

func TestBuildRuntimeContainerSlots(t *testing.T) {
	for _, rt := range []string{"openclaw", "hermes", "opencode", "deepseek-harness"} {
		t.Run(rt, func(t *testing.T) {
			d := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "pool", Namespace: "tenant", RuntimeType: rt, Image: "image", Replicas: 1, SlotCount: 2, AgentControlToken: "management", AgentReportToken: "report", SlotResources: corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("2Gi")}}})
			p := d.Spec.Template.Spec
			if len(p.Containers) != 3 {
				t.Fatalf("containers=%d want coordinator+2 slots", len(p.Containers))
			}
			requireEnv(t, p.Containers[0], "RUNTIME_AGENT_ROLE", "pool")
			requireEnv(t, p.Containers[0], "RUNTIME_SLOT_COUNT", "2")
			requireEnv(t, p.Containers[0], "RUNTIME_SLOT_SOCKET_DIR", "/run/clawmanager-slots")
			if probe := p.Containers[0].ReadinessProbe; probe == nil || probe.HTTPGet == nil || probe.HTTPGet.Path != "/readyz" {
				t.Fatal("coordinator must gate Pod readiness on healthy slots and upgrade activation")
			}
			for i, c := range p.Containers[1:] {
				if c.ReadinessProbe != nil {
					t.Fatal("slot must not inherit the coordinator TCP probe")
				}
				requireEnv(t, c, "RUNTIME_AGENT_ROLE", "slot")
				requireEnv(t, c, "RUNTIME_GATEWAY_CAPACITY", "1")
				requireEnvAbsent(t, c, "RUNTIME_AGENT_REPORT_TOKEN")
				requireEnvAbsent(t, c, "CLAWMANAGER_AGENT_REPORT_TOKEN")
				if c.Name != fmt.Sprintf("slot-%d", i) {
					t.Fatalf("wrong slot name %s", c.Name)
				}
				if len(c.Ports) != 0 {
					t.Fatal("slot must not expose duplicate control port")
				}
				if c.SecurityContext == nil || c.SecurityContext.Privileged == nil || *c.SecurityContext.Privileged {
					t.Fatal("slot requires explicit nonprivileged context")
				}
				caps := map[corev1.Capability]bool{}
				for _, capability := range c.SecurityContext.Capabilities.Add {
					caps[capability] = true
				}
				if !caps["KILL"] || !caps["SYS_PTRACE"] || caps["SYS_ADMIN"] {
					t.Fatal("supervisor capabilities do not support namespace attribution and stop safely")
				}
				if c.Resources.Limits.Cpu().IsZero() {
					t.Fatal("slot CPU limit missing")
				}
				if c.Resources.Limits.Memory().Cmp(resource.MustParse("2Gi")) != 0 {
					t.Fatal("slot memory budget missing")
				}
				requireVolumeMount(t, c, "runtime-slot-sockets", "/run/clawmanager-slots")
			}
			block := 1
			if rt == "openclaw" || rt == "opencode" {
				block = 3
			}
			requireEnv(t, p.Containers[1], "RUNTIME_GATEWAY_PORT_START", "20000")
			requireEnv(t, p.Containers[2], "RUNTIME_GATEWAY_PORT_START", fmt.Sprint(20000+block))
			requireEnvFieldRef(t, p.Containers[0], "POD_UID", "metadata.uid")
			if p.AutomountServiceAccountToken == nil || *p.AutomountServiceAccountToken {
				t.Fatal("slot pod must not mount Kubernetes credentials")
			}
		})
	}
}

func TestEnsureContainerSlotsValidatesStorageBeforeMutation(t *testing.T) {
	for _, tc := range []struct {
		name                string
		nodes               int
		provisioner, driver string
		mode                corev1.PersistentVolumeAccessMode
		bound               bool
		wantOK              bool
	}{
		{"longhorn rwx", 2, "driver.longhorn.io", "driver.longhorn.io", corev1.ReadWriteMany, true, true},
		{"name spoof", 2, "rancher.io/local-path", "driver.longhorn.io", corev1.ReadWriteMany, true, false},
		{"pv spoof", 2, "driver.longhorn.io", "other.csi", corev1.ReadWriteMany, true, false},
		{"rwo", 2, "driver.longhorn.io", "driver.longhorn.io", corev1.ReadWriteOnce, true, false},
		{"pending", 2, "driver.longhorn.io", "driver.longhorn.io", corev1.ReadWriteMany, false, false},
		{"missing node inventory", 0, "driver.longhorn.io", "driver.longhorn.io", corev1.ReadWriteMany, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sc := "longhorn"
			phase := corev1.ClaimPending
			if tc.bound {
				phase = corev1.ClaimBound
			}
			objects := []runtime.Object{&storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: sc}, Provisioner: tc.provisioner}, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "workspace", Namespace: "tenant"}, Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &sc, VolumeName: "pv", AccessModes: []corev1.PersistentVolumeAccessMode{tc.mode}}, Status: corev1.PersistentVolumeClaimStatus{Phase: phase}}, &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv"}, Spec: corev1.PersistentVolumeSpec{StorageClassName: sc, AccessModes: []corev1.PersistentVolumeAccessMode{tc.mode}, ClaimRef: &corev1.ObjectReference{Namespace: "tenant", Name: "workspace"}, PersistentVolumeSource: corev1.PersistentVolumeSource{CSI: &corev1.CSIPersistentVolumeSource{Driver: tc.driver}}}, Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound}}}
			for i := 0; i < tc.nodes; i++ {
				objects = append(objects, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("node-%d", i)}})
			}
			client := fake.NewSimpleClientset(objects...)
			err := NewRuntimeDeploymentService(client).Ensure(context.Background(), RuntimeDeploymentSpec{Name: "pool", Namespace: "tenant", RuntimeType: "openclaw", SlotCount: 2, AgentTokenSecretName: "pool-agent", WorkspacePVCClaimName: "workspace"})
			if (err == nil) != tc.wantOK {
				t.Fatalf("error=%v wantOK=%t", err, tc.wantOK)
			}
			deployments, _ := client.AppsV1().Deployments("tenant").List(context.Background(), metav1.ListOptions{})
			if !tc.wantOK && len(deployments.Items) != 0 {
				t.Fatal("invalid pool persisted")
			}
		})
	}
}

func TestContainerSlotRolloutUpdatesAllRuntimeContainers(t *testing.T) {
	d := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "pool", Namespace: "tenant", RuntimeType: "openclaw", Image: "old", SlotCount: 2, AgentTokenSecretName: "pool-agent"})
	client := fake.NewSimpleClientset(d)
	if err := NewRuntimeDeploymentService(client).RolloutImage(context.Background(), "tenant", "pool", "new", "", 1, 1); err != nil {
		t.Fatal(err)
	}
	got, err := client.AppsV1().Deployments("tenant").Get(context.Background(), "pool", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range got.Spec.Template.Spec.Containers {
		if c.Image != "new" {
			t.Fatalf("%s retained old image", c.Name)
		}
		requireEnv(t, c, "CLAWMANAGER_RUNTIME_IMAGE_REF", "new")
		for _, e := range c.Env {
			if e.Name == "RUNTIME_AGENT_CONTROL_TOKEN" && (e.Value != "" || e.ValueFrom == nil || e.ValueFrom.SecretKeyRef == nil || e.ValueFrom.SecretKeyRef.Name != "pool-agent") {
				t.Fatal("management credential is not Secret-backed")
			}
		}
	}
}

func TestContainerSlotDeploymentSpecsFromConfig(t *testing.T) {
	cfg := config.RuntimePoolConfig{Namespace: "tenant", ContainerSlots: 2, WorkspacePVCClaimName: "workspace", OpenClawImage: "openclaw", HermesImage: "hermes", OpenCodeImage: "opencode", DeepSeekHarnessImage: "dsh", SlotResources: config.RuntimeSlotResources{CPURequest: "100m", MemoryRequest: "256Mi", CPULimit: "2", MemoryLimit: "4Gi"}}
	specs, err := RuntimeSlotDeploymentSpecs(cfg, "pool-agent")
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 4 {
		t.Fatalf("spec count=%d", len(specs))
	}
	for _, s := range specs {
		if s.SlotCount != 2 || s.AgentTokenSecretName != "pool-agent" || s.Namespace != "tenant" || s.WorkspacePVCClaimName != "workspace" || s.SlotResources.Limits.Memory().Cmp(resource.MustParse("4Gi")) != 0 {
			t.Fatalf("bad spec: %#v", s)
		}
	}
	cfg.ContainerSlots = 0
	if specs, err := RuntimeSlotDeploymentSpecs(cfg, ""); err != nil || len(specs) != 0 {
		t.Fatalf("legacy auto-deployed slot pools: %v %v", specs, err)
	}
	cfg.ContainerSlots = 2
	cfg.SlotResources.CPULimit = "invalid"
	if _, err := RuntimeSlotDeploymentSpecs(cfg, "pool-agent"); err == nil {
		t.Fatal("invalid budget accepted")
	}
}

func TestEnsureContainerSlotsRequiresCredentialSecret(t *testing.T) {
	err := NewRuntimeDeploymentService(fake.NewSimpleClientset()).Ensure(context.Background(), RuntimeDeploymentSpec{SlotCount: 1, RuntimeType: "openclaw"})
	if err == nil || !strings.Contains(err.Error(), "Secret") {
		t.Fatalf("missing credential Secret accepted: %v", err)
	}
}

func TestEnsureDoesNotConvertExistingSlotProtocol(t *testing.T) {
	d := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "pool", Namespace: "tenant", RuntimeType: "openclaw", SlotCount: 1})
	client := fake.NewSimpleClientset(d)
	err := NewRuntimeDeploymentService(client).Ensure(context.Background(), RuntimeDeploymentSpec{Name: "pool", Namespace: "tenant", RuntimeType: "openclaw"})
	if err == nil {
		t.Fatal("container-slot deployment silently converted to legacy process pool")
	}
	got, _ := client.AppsV1().Deployments("tenant").Get(context.Background(), "pool", metav1.GetOptions{})
	if len(got.Spec.Template.Spec.Containers) != 2 {
		t.Fatal("live pool mutated before explicit migration")
	}
}

func TestContainerSlotsPermitUserNamespaceMountsWithoutChangingCoordinatorAppArmor(t *testing.T) {
	for _, kind := range []string{"openclaw", "hermes", "opencode", "deepseek-harness"} {
		t.Run(kind, func(t *testing.T) {
			deployment := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "pool", RuntimeType: kind, Image: "image", Replicas: 1, SlotCount: 2})
			if deployment.Spec.Template.Spec.ServiceAccountName != "clawmanager-lite-runtime" {
				t.Fatal("runtime pool must use its dedicated service account")
			}
			for _, container := range deployment.Spec.Template.Spec.Containers[1:] {
				key := "container.apparmor.security.beta.kubernetes.io/" + container.Name
				if deployment.Spec.Template.Annotations[key] != "unconfined" {
					t.Fatal("slot supervisor must permit bubblewrap mounts inside its unprivileged user namespace")
				}
				if container.SecurityContext.ProcMount == nil || *container.SecurityContext.ProcMount != corev1.UnmaskedProcMount {
					t.Fatal("outer proc masks prevent bubblewrap from creating the isolated PID namespace proc")
				}
			}
			key := "container.apparmor.security.beta.kubernetes.io/" + deployment.Spec.Template.Spec.Containers[0].Name
			if deployment.Spec.Template.Annotations[key] == "unconfined" {
				t.Fatal("coordinator does not create sandboxes and must retain the default AppArmor policy")
			}
			coordinator := deployment.Spec.Template.Spec.Containers[0]
			if coordinator.SecurityContext == nil || coordinator.SecurityContext.Capabilities == nil || len(coordinator.SecurityContext.Capabilities.Drop) != 1 || coordinator.SecurityContext.Capabilities.Drop[0] != "ALL" {
				t.Fatal("coordinator must drop default capabilities to satisfy the restricted runtime policy")
			}
		})
	}
}
