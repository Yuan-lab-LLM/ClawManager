package k8s

import (
	"clawreef/internal/config"
	"context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestNFSRootlessSlotsUseOrdinaryPodAndContainerResetBoundary(t *testing.T) {
	cfg := config.RuntimePoolConfig{SandboxProfile: "nfs-rootless-v1", ContainerSlots: 2, NetworkIsolation: true, OpenClawImage: "oc", HermesImage: "hm", OpenCodeImage: "code", DeepSeekHarnessImage: "ds"}
	specs, err := RuntimeSlotDeploymentSpecs(cfg, "management")
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range specs {
		d := BuildRuntimeDeployment(spec)
		if err := validateRuntimeSlotTLSDeployment(d); err != nil {
			t.Fatalf("NFS pool must survive backend restart: %v", err)
		}
		p := d.Spec.Template.Spec
		if p.HostUsers != nil && !*p.HostUsers {
			t.Fatal("NFS requires no Pod idmapped mounts")
		}
		if p.ShareProcessNamespace != nil && *p.ShareProcessNamespace {
			t.Fatal("slots share PID namespace")
		}
		if d.Spec.Template.Labels["clawmanager.io/sandbox-profile"] != "nfs-rootless-v1" {
			t.Fatal("missing trusted profile identity")
		}
		for _, c := range p.Containers[1:] {
			requireEnv(t, c, "RUNTIME_SANDBOX_PROFILE", "nfs-rootless-v1")
			requireEnv(t, c, "RUNTIME_SLOT_TERMINATION_MESSAGE_PATH", "/dev/termination-log")
			if c.TerminationMessagePath != "/dev/termination-log" || c.TerminationMessagePolicy != corev1.TerminationMessageReadFile {
				t.Fatal("reset proof must be explicit file evidence")
			}
			s := c.SecurityContext
			if s.RunAsUser == nil || *s.RunAsUser != 0 || len(c.Command) < 2 || c.Command[0] != "/usr/bin/setpriv" {
				t.Fatal("management identity requires the capability-preserving startup wrapper")
			}
			for _, arg := range []string{"--reuid=10001", "--regid=10001", "--clear-groups", "--inh-caps=+setuid,+setgid", "--ambient-caps=+setuid,+setgid"} {
				found := false
				for _, actual := range c.Command {
					if actual == arg {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing trusted management startup argument %s", arg)
				}
			}
			if s.ProcMount != nil && *s.ProcMount == corev1.UnmaskedProcMount {
				t.Fatal("ordinary Pod cannot request Unmasked proc")
			}
			for _, cap := range s.Capabilities.Add {
				if cap == "SYS_ADMIN" || cap == "NET_ADMIN" {
					t.Fatal("host network administration granted")
				}
			}
		}
	}
}

func TestNFSRootlessStorageChecksActualBoundExport(t *testing.T) {
	for _, scenario := range []string{"single-node", "multi-node", "rwo", "readonly", "wrong-claim", "no-export"} {
		t.Run(scenario, func(t *testing.T) {
			empty := ""
			pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "workspace", Namespace: "tenant", UID: "claim-uid"}, Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &empty, VolumeName: "pv", AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
			pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv"}, Spec: corev1.PersistentVolumeSpec{AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}, ClaimRef: &corev1.ObjectReference{Namespace: "tenant", Name: "workspace", UID: "claim-uid"}, PersistentVolumeSource: corev1.PersistentVolumeSource{NFS: &corev1.NFSVolumeSource{Server: "10.0.0.9", Path: "/workspaces"}}}, Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound}}
			switch scenario {
			case "rwo":
				pvc.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
			case "readonly":
				pv.Spec.NFS.ReadOnly = true
			case "wrong-claim":
				pv.Spec.ClaimRef.UID = "other-claim"
			case "no-export":
				pv.Spec.NFS.Path = ""
			}
			objects := []runtime.Object{pvc, pv, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-0"}}}
			if scenario == "multi-node" {
				objects = append(objects, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}})
			}
			s := &runtimeDeploymentService{client: fake.NewSimpleClientset(objects...)}
			err := s.validateRuntimeSlotStorage(context.Background(), RuntimeDeploymentSpec{Namespace: "tenant", RuntimeType: "openclaw", SlotCount: 2, SandboxProfile: "nfs-rootless-v1", NetworkIsolation: true, WorkspacePVCClaimName: "workspace"})
			wantOK := scenario == "single-node" || scenario == "multi-node"
			if (err == nil) != wantOK {
				t.Fatalf("err=%v wantOK=%v", err, wantOK)
			}
		})
	}
}

func TestNFSCoordinatorTLSCopyRequiresPrivateSourceAndMemoryTarget(t *testing.T) {
	for _, scenario := range []string{"missing-init-source", "disk-target", "slot-secret"} {
		t.Run(scenario, func(t *testing.T) {
			d := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "runtime", Namespace: "lab", RuntimeType: "openclaw", Image: "image", SlotCount: 2, SandboxProfile: RuntimeNFSRootlessProfile, NetworkIsolation: true, AgentTokenSecretName: "agent"})
			pod := &d.Spec.Template.Spec
			switch scenario {
			case "missing-init-source":
				for i := range pod.InitContainers {
					mounts := pod.InitContainers[i].VolumeMounts[:0]
					for _, mount := range pod.InitContainers[i].VolumeMounts {
						if mount.Name != "runtime-agent-tls-source" {
							mounts = append(mounts, mount)
						}
					}
					pod.InitContainers[i].VolumeMounts = mounts
				}
			case "disk-target":
				for i := range pod.Volumes {
					if pod.Volumes[i].Name == "runtime-agent-tls" {
						pod.Volumes[i].EmptyDir.Medium = ""
					}
				}
			case "slot-secret":
				pod.Containers[1].VolumeMounts = append(pod.Containers[1].VolumeMounts, corev1.VolumeMount{Name: "runtime-agent-tls-source", MountPath: "/steal", ReadOnly: true})
			}
			if err := validateRuntimeSlotTLSDeployment(d); err == nil {
				t.Fatal("untrusted TLS copy layout admitted")
			}
		})
	}
}

func TestNFSInventoryKeepsExistingHealthyGatewayWithoutOpeningScheduling(t *testing.T) {
	deployment := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "runtime", Namespace: "lab", RuntimeType: "openclaw", Image: "image", SlotCount: 2, SandboxProfile: RuntimeNFSRootlessProfile, NetworkIsolation: true, AgentTokenSecretName: "agent", GatewayPortStart: 24000, GatewayPortEnd: 24005})
	pod := corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: "lab", UID: "uid", Labels: deployment.Spec.Template.Labels}, Spec: *deployment.Spec.Template.Spec.DeepCopy(), Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.0.0.2", Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse, Reason: "ContainersNotReady"}}}}
	for _, name := range []string{"runtime", "slot-0"} {
		pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{Name: name, Ready: true, ContainerID: "containerd://" + name, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.Now()}}})
	}
	pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{Name: "slot-1", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ContainerCreating"}}})
	service := &runtimeDeploymentService{client: fake.NewSimpleClientset()}
	inventory := service.renderDeploymentPods(context.Background(), deployment, []corev1.Pod{pod}, map[string]*corev1.PersistentVolumeClaim{})
	if len(inventory) != 1 || inventory[0].State == "ready" || !inventory[0].ReadyForGateway(24000) || inventory[0].ReadyForGateway(24003) {
		t.Fatalf("existing gateway and scheduling mixed: %+v", inventory)
	}
}
