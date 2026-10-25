package k8s

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestBuildContainerSecurityContext(t *testing.T) {
	if got := buildContainerSecurityContext(PodSecurityDefault); got != nil {
		t.Fatalf("expected default security mode to leave securityContext unset, got %#v", got)
	}

	chromium := buildContainerSecurityContext(PodSecurityChromiumCompat)
	if chromium == nil {
		t.Fatalf("expected chromium compat security context")
	}
	if chromium.Privileged != nil && *chromium.Privileged {
		t.Fatalf("chromium compat mode must not enable privileged")
	}
	if chromium.AllowPrivilegeEscalation == nil || !*chromium.AllowPrivilegeEscalation {
		t.Fatalf("expected chromium compat mode to allow privilege escalation for browser sandbox helpers")
	}
	if chromium.SeccompProfile == nil || chromium.SeccompProfile.Type != "Unconfined" {
		t.Fatalf("expected chromium compat mode to use unconfined seccomp profile, got %#v", chromium.SeccompProfile)
	}

	workbuddy := buildContainerSecurityContext(PodSecurityWorkbuddyLinux)
	if workbuddy == nil || workbuddy.Privileged == nil || !*workbuddy.Privileged {
		t.Fatalf("expected Linux WorkBuddy mode to enable privileged security context")
	}
	if workbuddy.AllowPrivilegeEscalation == nil || !*workbuddy.AllowPrivilegeEscalation {
		t.Fatalf("expected Linux WorkBuddy mode to allow the setuid bubblewrap helper")
	}
	if workbuddy.SeccompProfile == nil || workbuddy.SeccompProfile.Type != corev1.SeccompProfileTypeUnconfined {
		t.Fatalf("expected Linux WorkBuddy mode to use unconfined seccomp, got %#v", workbuddy.SeccompProfile)
	}
	if workbuddy.AppArmorProfile == nil || workbuddy.AppArmorProfile.Type != corev1.AppArmorProfileTypeUnconfined {
		t.Fatalf("expected Linux WorkBuddy mode to use unconfined AppArmor, got %#v", workbuddy.AppArmorProfile)
	}
	if workbuddy.Capabilities == nil || len(workbuddy.Capabilities.Add) != 2 {
		t.Fatalf("expected Linux WorkBuddy mode to add sandbox capabilities, got %#v", workbuddy.Capabilities)
	}
	if !requiresUnconfinedAppArmor(PodSecurityWorkbuddyLinux) {
		t.Fatalf("expected Linux WorkBuddy mode to request the compatibility AppArmor annotation")
	}

	privileged := buildContainerSecurityContext(PodSecurityPrivileged)
	if privileged == nil || privileged.Privileged == nil || !*privileged.Privileged {
		t.Fatalf("expected privileged mode to enable privileged security context")
	}
}

func TestCreatePodAppliesSecurityModes(t *testing.T) {
	previousClient := globalClient
	t.Cleanup(func() {
		globalClient = previousClient
	})

	globalClient = &Client{
		Clientset:    fake.NewSimpleClientset(),
		Namespace:    "clawreef",
		StorageClass: "standard",
	}

	service := NewPodService()
	pod, err := service.CreatePod(context.Background(), PodConfig{
		InstanceID:    42,
		InstanceName:  "openclaw-test",
		UserID:        7,
		Type:          "openclaw",
		CPUCores:      1,
		MemoryGB:      2,
		Image:         "openclaw:test",
		MountPath:     "/config",
		ContainerPort: 3001,
		SHMSizeGB:     1,
		SecurityMode:  PodSecurityChromiumCompat,
	})
	if err != nil {
		t.Fatalf("CreatePod returned error: %v", err)
	}

	deployment := mustGetDeployment(t, service, "clawreef-user-7", "clawreef-42-openclaw-test")
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 {
		t.Fatalf("expected single-replica deployment, got %#v", deployment.Spec.Replicas)
	}
	if deployment.Spec.Template.Spec.RestartPolicy != corev1.RestartPolicyAlways {
		t.Fatalf("expected deployment-managed pod restartPolicy Always, got %s", deployment.Spec.Template.Spec.RestartPolicy)
	}

	container := pod.Spec.Containers[0]
	if container.SecurityContext == nil {
		t.Fatalf("expected chromium compat security context")
	}
	if container.SecurityContext.Privileged != nil && *container.SecurityContext.Privileged {
		t.Fatalf("chromium compat pod must not be privileged")
	}
	if pod.Annotations["container.apparmor.security.beta.kubernetes.io/desktop"] != "unconfined" {
		t.Fatalf("expected apparmor unconfined annotation for chromium compat mode, got %#v", pod.Annotations)
	}
	if len(pod.Spec.Volumes) != 2 {
		t.Fatalf("expected data and shm volumes, got %d", len(pod.Spec.Volumes))
	}
}

func TestCreatePodShellRuntimeSkipsDesktopNetworkProbes(t *testing.T) {
	previousClient := globalClient
	t.Cleanup(func() {
		globalClient = previousClient
	})

	globalClient = &Client{
		Clientset:    fake.NewSimpleClientset(),
		Namespace:    "clawreef",
		StorageClass: "standard",
	}

	service := NewPodService()
	pod, err := service.CreatePod(context.Background(), PodConfig{
		InstanceID:    43,
		InstanceName:  "shell-test",
		UserID:        7,
		Type:          "openclaw",
		RuntimeType:   "shell",
		CPUCores:      1,
		MemoryGB:      2,
		Image:         "openclaw:test",
		MountPath:     "/config",
		ContainerPort: 3001,
	})
	if err != nil {
		t.Fatalf("CreatePod returned error: %v", err)
	}

	deployment := mustGetDeployment(t, service, "clawreef-user-7", "clawreef-43-shell-test")
	if len(deployment.Spec.Template.Spec.Containers[0].Ports) != 0 {
		t.Fatalf("expected shell deployment template to skip desktop ports")
	}

	container := pod.Spec.Containers[0]
	if got := pod.Labels["runtime-type"]; got != "shell" {
		t.Fatalf("expected runtime-type shell label, got %q", got)
	}
	if len(container.Ports) != 0 {
		t.Fatalf("expected shell runtime to skip desktop ports, got %d", len(container.Ports))
	}
	if container.StartupProbe != nil || container.ReadinessProbe != nil || container.LivenessProbe != nil {
		t.Fatalf("expected shell runtime to skip desktop TCP probes")
	}
}

func TestCreatePodAppliesExtraPVCMountsAndSecretEnv(t *testing.T) {
	previousClient := globalClient
	t.Cleanup(func() {
		globalClient = previousClient
	})

	globalClient = &Client{
		Clientset:    fake.NewSimpleClientset(),
		Namespace:    "clawreef",
		StorageClass: "standard",
	}

	service := NewPodService()
	pod, err := service.CreatePod(context.Background(), PodConfig{
		InstanceID:         43,
		InstanceName:       "openclaw-team",
		UserID:             7,
		Type:               "openclaw",
		CPUCores:           1,
		MemoryGB:           2,
		Image:              "openclaw:test",
		MountPath:          "/config",
		ContainerPort:      3001,
		EnvFromSecretNames: []string{"clawreef-team-1-bus"},
		ExtraPVCMounts: []PVCMount{
			{Name: "team-shared", ClaimName: "clawreef-team-1-shared", MountPath: "/team"},
		},
		ConfigMapFileMounts: []ConfigMapFileMount{
			{Name: "team-config", ConfigMapName: "clawreef-team-1-config", Key: "team.json", MountPath: "/etc/clawmanager/team", ReadOnly: true, AsDirectory: true},
		},
		FSGroup: int64Ptr(1000),
		VolumeOwnershipFixes: []VolumeOwnershipFix{
			{Name: "team-shared", MountPath: "/team", UID: 1000, GID: 1000},
		},
	})
	if err != nil {
		t.Fatalf("CreatePod returned error: %v", err)
	}

	deployment := mustGetDeployment(t, service, "clawreef-user-7", "clawreef-43-openclaw-team")
	if deployment.Spec.Template.Labels["instance-id"] != "43" {
		t.Fatalf("expected deployment template instance-id label, got %#v", deployment.Spec.Template.Labels)
	}

	container := pod.Spec.Containers[0]
	if len(container.EnvFrom) != 1 || container.EnvFrom[0].SecretRef == nil || container.EnvFrom[0].SecretRef.Name != "clawreef-team-1-bus" {
		t.Fatalf("expected Team secret envFrom, got %#v", container.EnvFrom)
	}

	foundMount := false
	for _, mount := range container.VolumeMounts {
		if mount.Name == "team-shared" && mount.MountPath == "/team" {
			foundMount = true
		}
	}
	if !foundMount {
		t.Fatalf("expected /team shared PVC mount, got %#v", container.VolumeMounts)
	}

	if pod.Spec.SecurityContext == nil || pod.Spec.SecurityContext.FSGroup == nil || *pod.Spec.SecurityContext.FSGroup != 1000 {
		t.Fatalf("expected pod fsGroup 1000, got %#v", pod.Spec.SecurityContext)
	}
	if pod.Spec.SecurityContext.FSGroupChangePolicy == nil || *pod.Spec.SecurityContext.FSGroupChangePolicy != corev1.FSGroupChangeOnRootMismatch {
		t.Fatalf("expected OnRootMismatch fsGroup change policy, got %#v", pod.Spec.SecurityContext)
	}

	if len(pod.Spec.InitContainers) != 1 {
		t.Fatalf("expected one Team shared permission initContainer, got %#v", pod.Spec.InitContainers)
	}
	initContainer := pod.Spec.InitContainers[0]
	if initContainer.SecurityContext == nil || initContainer.SecurityContext.RunAsUser == nil || *initContainer.SecurityContext.RunAsUser != 0 {
		t.Fatalf("expected permission initContainer to run as root, got %#v", initContainer.SecurityContext)
	}
	foundInitMount := false
	for _, mount := range initContainer.VolumeMounts {
		if mount.Name == "team-shared" && mount.MountPath == "/team" {
			foundInitMount = true
		}
	}
	if !foundInitMount {
		t.Fatalf("expected permission initContainer to mount /team, got %#v", initContainer.VolumeMounts)
	}

	foundConfig := false
	for _, mount := range container.VolumeMounts {
		if mount.Name == "team-config" && mount.MountPath == "/etc/clawmanager/team" && mount.SubPath == "" && mount.ReadOnly {
			foundConfig = true
		}
	}
	if !foundConfig {
		t.Fatalf("expected Team ConfigMap directory mount, got %#v", container.VolumeMounts)
	}
}

func TestSelectCurrentPodPrefersRecoveringDeploymentPod(t *testing.T) {
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "old-evicted"},
			Status:     corev1.PodStatus{Phase: corev1.PodFailed},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "new-pending"},
			Status:     corev1.PodStatus{Phase: corev1.PodPending},
		},
	}

	selected := selectCurrentPod(pods)
	if selected == nil || selected.Name != "new-pending" {
		t.Fatalf("expected recovering pending pod to win over failed pod, got %#v", selected)
	}

	pods = append(pods, corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "ready"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	})

	selected = selectCurrentPod(pods)
	if selected == nil || selected.Name != "ready" {
		t.Fatalf("expected ready running pod to win, got %#v", selected)
	}
}

func int64Ptr(value int64) *int64 {
	return &value
}

func mustGetDeployment(t *testing.T, service *PodService, namespace, name string) *appsv1.Deployment {
	t.Helper()
	deployment, err := service.GetClient().Clientset.AppsV1().Deployments(namespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected deployment %s/%s to exist: %v", namespace, name, err)
	}
	return deployment
}
