package k8s

import (
	"context"
	"fmt"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestBuildInstanceDeploymentUsesStableIdentityAndPVC(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	client := &Client{Clientset: clientset, Namespace: "clawreef"}
	deployment := BuildInstanceDeployment(client, PodConfig{
		InstanceID:      42,
		InstanceName:    "Pro Desktop",
		UserID:          7,
		Type:            "openclaw",
		RuntimeType:     "desktop",
		CPUCores:        2,
		MemoryGB:        4,
		Image:           "registry/openclaw:pro",
		MountPath:       "/config",
		ContainerPort:   3001,
		ImagePullPolicy: corev1.PullIfNotPresent,
	}, 1)

	if deployment.Name != "clawreef-42-deployment" {
		t.Fatalf("deployment name = %q, want stable instance deployment name", deployment.Name)
	}
	if deployment.Namespace != "clawreef-user-7" {
		t.Fatalf("namespace = %q, want user namespace", deployment.Namespace)
	}
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 {
		t.Fatalf("replicas = %#v, want 1", deployment.Spec.Replicas)
	}
	if got := deployment.Spec.Selector.MatchLabels["instance-id"]; got != "42" {
		t.Fatalf("selector instance-id = %q, want 42", got)
	}
	template := deployment.Spec.Template
	if got := template.Labels["runtime-type"]; got != "desktop" {
		t.Fatalf("template runtime-type = %q, want desktop", got)
	}
	container := template.Spec.Containers[0]
	if container.Name != "desktop" || container.Image != "registry/openclaw:pro" {
		t.Fatalf("unexpected container: %#v", container)
	}
	if template.Spec.RestartPolicy != corev1.RestartPolicyAlways {
		t.Fatalf("restart policy = %q, want Always", template.Spec.RestartPolicy)
	}
	if len(template.Spec.Volumes) == 0 || template.Spec.Volumes[0].PersistentVolumeClaim == nil {
		t.Fatalf("expected PVC data volume, got %#v", template.Spec.Volumes)
	}
	if got := template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName; got != "clawreef-42-pvc" {
		t.Fatalf("PVC name = %q, want clawreef-42-pvc", got)
	}
}

func TestBuildInstanceDeploymentRunsVolumeInitScripts(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	client := &Client{Clientset: clientset, Namespace: "clawreef"}
	deployment := BuildInstanceDeployment(client, PodConfig{
		InstanceID: 42, InstanceName: "OpenCode Pro", UserID: 7,
		Type: "opencode", RuntimeType: "desktop", CPUCores: 2, MemoryGB: 4,
		Image: "registry/opencode:pro", MountPath: "/config", ContainerPort: 3001,
		VolumeInitScripts: []VolumeInitScript{{Name: "data", MountPath: "/config", Script: "echo bootstrap"}},
	}, 1)

	if len(deployment.Spec.Template.Spec.InitContainers) != 1 {
		t.Fatalf("init containers = %#v, want one volume bootstrap container", deployment.Spec.Template.Spec.InitContainers)
	}
	init := deployment.Spec.Template.Spec.InitContainers[0]
	if got := init.Command; len(got) != 3 || got[2] != "echo bootstrap" {
		t.Fatalf("init command = %#v, want volume bootstrap script", got)
	}
	if len(init.VolumeMounts) != 1 || init.VolumeMounts[0].MountPath != "/config" {
		t.Fatalf("init mounts = %#v, want /config data mount", init.VolumeMounts)
	}
}

func TestBuildInstanceDeploymentAppliesNodeSelector(t *testing.T) {
	client := &Client{Clientset: fake.NewSimpleClientset(), Namespace: "clawreef"}
	deployment := BuildInstanceDeployment(client, PodConfig{
		InstanceID:    44,
		InstanceName:  "Pro Desktop",
		UserID:        7,
		Type:          "openclaw",
		RuntimeType:   "desktop",
		CPUCores:      2,
		MemoryGB:      4,
		Image:         "registry/openclaw:pro",
		MountPath:     "/config",
		ContainerPort: 3001,
		NodeSelector: map[string]string{
			"kubernetes.io/hostname": "node125",
		},
	}, 1)

	if got := deployment.Spec.Template.Spec.NodeSelector["kubernetes.io/hostname"]; got != "node125" {
		t.Fatalf("node selector hostname = %q, want node125", got)
	}
}

func TestBuildInstanceDeploymentUsesExplicitPVCName(t *testing.T) {
	client := &Client{Clientset: fake.NewSimpleClientset(), Namespace: "clawreef"}
	deployment := BuildInstanceDeployment(client, PodConfig{
		InstanceID:   45,
		InstanceName: "Prewarmed Windows",
		UserID:       7,
		Type:         "workbuddy",
		RuntimeType:  "desktop",
		CPUCores:     6,
		MemoryGB:     12,
		Image:        "registry/windows-workbuddy:v1",
		PVCName:      "workbuddy-prewarm-abcde",
		MountPath:    "/storage",
	}, 1)

	got := deployment.Spec.Template.Spec.Volumes[0].PersistentVolumeClaim.ClaimName
	if got != "workbuddy-prewarm-abcde" {
		t.Fatalf("PVC name = %q, want explicit prewarm PVC", got)
	}
}

func TestBuildInstanceDeploymentConfiguresWindowsWorkbuddy(t *testing.T) {
	client := &Client{Clientset: fake.NewSimpleClientset(), Namespace: "clawreef"}
	deployment := BuildInstanceDeployment(client, PodConfig{
		InstanceID:           45,
		InstanceName:         "Workbuddy Windows",
		UserID:               7,
		Type:                 "workbuddy",
		RuntimeType:          "desktop",
		CPUCores:             4,
		MemoryGB:             8,
		Image:                "registry/windows-workbuddy:v1",
		MountPath:            "/storage",
		ContainerPort:        8006,
		ProbePort:            3389,
		StartupProbeFailures: 120,
		TerminationGrace:     120,
		SecurityMode:         PodSecurityPrivileged,
	}, 1)

	spec := deployment.Spec.Template.Spec
	if deployment.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType {
		t.Fatalf("Windows deployment strategy = %q, want Recreate", deployment.Spec.Strategy.Type)
	}
	if spec.TerminationGracePeriodSeconds == nil || *spec.TerminationGracePeriodSeconds != 120 {
		t.Fatalf("termination grace = %#v, want 120", spec.TerminationGracePeriodSeconds)
	}
	container := spec.Containers[0]
	if container.SecurityContext == nil || container.SecurityContext.Privileged == nil || !*container.SecurityContext.Privileged {
		t.Fatalf("expected privileged Windows container, got %#v", container.SecurityContext)
	}
	if len(container.Ports) != 2 || container.Ports[0].ContainerPort != 8006 || container.Ports[1].ContainerPort != 3389 {
		t.Fatalf("unexpected Windows ports: %#v", container.Ports)
	}
	if container.StartupProbe == nil || container.StartupProbe.FailureThreshold != 120 || container.StartupProbe.TCPSocket.Port.IntVal != 3389 {
		t.Fatalf("unexpected Windows startup probe: %#v", container.StartupProbe)
	}
	if container.ReadinessProbe == nil || container.ReadinessProbe.TCPSocket.Port.IntVal != 3389 {
		t.Fatalf("unexpected Windows readiness probe: %#v", container.ReadinessProbe)
	}
	if len(container.VolumeMounts) == 0 || container.VolumeMounts[0].MountPath != "/storage" {
		t.Fatalf("unexpected Windows storage mount: %#v", container.VolumeMounts)
	}
}

func TestBuildInstanceDeploymentConfiguresLinuxWorkbuddySecurity(t *testing.T) {
	client := &Client{Clientset: fake.NewSimpleClientset(), Namespace: "clawreef"}
	deployment := BuildInstanceDeployment(client, PodConfig{
		InstanceID:    46,
		InstanceName:  "Workbuddy Linux",
		UserID:        7,
		Type:          "workbuddy",
		RuntimeType:   "desktop",
		CPUCores:      4,
		MemoryGB:      8,
		Image:         "registry/workbuddy-linux:v1",
		MountPath:     "/config",
		ContainerPort: 3001,
		SecurityMode:  PodSecurityWorkbuddyLinux,
	}, 1)

	template := deployment.Spec.Template
	if got := template.Annotations["container.apparmor.security.beta.kubernetes.io/desktop"]; got != "unconfined" {
		t.Fatalf("AppArmor annotation = %q, want unconfined", got)
	}
	securityContext := template.Spec.Containers[0].SecurityContext
	if securityContext == nil || securityContext.Privileged == nil || !*securityContext.Privileged {
		t.Fatalf("expected privileged Linux WorkBuddy container, got %#v", securityContext)
	}
	if securityContext.AllowPrivilegeEscalation == nil || !*securityContext.AllowPrivilegeEscalation {
		t.Fatalf("expected Linux WorkBuddy container to allow privilege escalation")
	}
	if securityContext.SeccompProfile == nil || securityContext.SeccompProfile.Type != corev1.SeccompProfileTypeUnconfined {
		t.Fatalf("expected unconfined Seccomp profile, got %#v", securityContext.SeccompProfile)
	}
	if securityContext.AppArmorProfile == nil || securityContext.AppArmorProfile.Type != corev1.AppArmorProfileTypeUnconfined {
		t.Fatalf("expected unconfined AppArmor profile, got %#v", securityContext.AppArmorProfile)
	}
	if securityContext.Capabilities == nil {
		t.Fatal("expected Linux WorkBuddy sandbox capabilities")
	}
	wantedCapabilities := map[corev1.Capability]bool{"NET_ADMIN": false, "SYS_ADMIN": false}
	for _, capability := range securityContext.Capabilities.Add {
		if _, ok := wantedCapabilities[capability]; ok {
			wantedCapabilities[capability] = true
		}
	}
	for capability, found := range wantedCapabilities {
		if !found {
			t.Fatalf("expected capability %q, got %#v", capability, securityContext.Capabilities.Add)
		}
	}
}

func TestBuildInstanceDeploymentMountsSecretDirectory(t *testing.T) {
	client := &Client{Clientset: fake.NewSimpleClientset(), Namespace: "clawreef"}
	deployment := BuildInstanceDeployment(client, PodConfig{
		InstanceID: 46, InstanceName: "Codex Windows", UserID: 7,
		Type: "codex", RuntimeType: "desktop", CPUCores: 6, MemoryGB: 12,
		Image: "registry/windows-codex:v1", MountPath: "/storage", ContainerPort: 8006,
		SecretDirectoryMounts: []SecretDirectoryMount{{
			Name: "codex-bootstrap", SecretName: "clawreef-46-codex-bootstrap", MountPath: "/shared/.clawmanager",
		}},
	}, 1)

	spec := deployment.Spec.Template.Spec
	foundVolume := false
	for _, volume := range spec.Volumes {
		if volume.Name == "codex-bootstrap" && volume.Secret != nil && volume.Secret.SecretName == "clawreef-46-codex-bootstrap" {
			foundVolume = true
		}
	}
	if !foundVolume {
		t.Fatalf("expected Codex bootstrap Secret volume, got %#v", spec.Volumes)
	}
	foundMount := false
	for _, mount := range spec.Containers[0].VolumeMounts {
		if mount.Name == "codex-bootstrap" && mount.MountPath == "/shared/.clawmanager" && mount.ReadOnly {
			foundMount = true
		}
	}
	if !foundMount {
		t.Fatalf("expected read-only Codex bootstrap mount, got %#v", spec.Containers[0].VolumeMounts)
	}
}

func TestInstanceDeploymentServiceEnsureAndScale(t *testing.T) {
	client := &Client{Clientset: fake.NewSimpleClientset(), Namespace: "clawreef"}
	service := &InstanceDeploymentService{
		client:           client,
		namespaceService: &NamespaceService{client: client},
	}
	config := PodConfig{
		InstanceID:    43,
		InstanceName:  "Pro Desktop",
		UserID:        8,
		Type:          "openclaw",
		RuntimeType:   "desktop",
		CPUCores:      2,
		MemoryGB:      4,
		Image:         "registry/openclaw:v1",
		MountPath:     "/config",
		ContainerPort: 3001,
	}

	if _, err := service.EnsureDeployment(context.Background(), config, 1); err != nil {
		t.Fatalf("EnsureDeployment create returned error: %v", err)
	}
	deployment, err := client.Clientset.AppsV1().Deployments("clawreef-user-8").Get(context.Background(), "clawreef-43-deployment", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 {
		t.Fatalf("created replicas = %#v, want 1", deployment.Spec.Replicas)
	}

	if err := service.ScaleDeployment(context.Background(), 8, 43, 0); err != nil {
		t.Fatalf("ScaleDeployment returned error: %v", err)
	}
	scaled, err := client.Clientset.AppsV1().Deployments("clawreef-user-8").Get(context.Background(), "clawreef-43-deployment", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get scaled deployment: %v", err)
	}
	if scaled.Spec.Replicas == nil || *scaled.Spec.Replicas != 0 {
		t.Fatalf("scaled replicas = %#v, want 0", scaled.Spec.Replicas)
	}
}

func TestInstanceDeploymentServiceEnsureRetriesConflict(t *testing.T) {
	config := PodConfig{
		InstanceID:    46,
		InstanceName:  "OpenCode Pro",
		UserID:        8,
		Type:          "opencode",
		RuntimeType:   "desktop",
		CPUCores:      2,
		MemoryGB:      4,
		Image:         "opencode:local",
		MountPath:     "/config",
		ContainerPort: 3001,
	}
	clientset := fake.NewSimpleClientset()
	client := &Client{Clientset: clientset, Namespace: "clawreef"}
	service := &InstanceDeploymentService{
		client:           client,
		namespaceService: &NamespaceService{client: client},
	}
	ctx := context.Background()
	if _, err := service.EnsureDeployment(ctx, config, 1); err != nil {
		t.Fatalf("initial EnsureDeployment returned error: %v", err)
	}

	updateAttempts := 0
	clientset.Fake.PrependReactor("update", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		updateAttempts++
		if updateAttempts == 1 {
			return true, nil, apierrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments"}, "clawreef-46-deployment", fmt.Errorf("stale resource version"))
		}
		return false, nil, nil
	})

	if _, err := service.EnsureDeployment(ctx, config, 1); err != nil {
		t.Fatalf("EnsureDeployment after conflict returned error: %v", err)
	}
	if updateAttempts < 2 {
		t.Fatalf("update attempts = %d, want retry after conflict", updateAttempts)
	}
}

func TestInstanceDeploymentServiceWaitForDeploymentPodsDeleted(t *testing.T) {
	client := &Client{Clientset: fake.NewSimpleClientset(), Namespace: "clawreef"}
	service := &InstanceDeploymentService{
		client:           client,
		namespaceService: &NamespaceService{client: client},
	}
	ctx := context.Background()

	if err := service.waitForDeploymentPodsDeleted(ctx, 8, 43, 10*time.Millisecond); err != nil {
		t.Fatalf("wait with no pods returned error: %v", err)
	}

	if _, err := client.Clientset.CoreV1().Pods("clawreef-user-8").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "clawreef-43-desktop",
			Namespace: "clawreef-user-8",
			Labels: map[string]string{
				"app":         "clawreef",
				"instance-id": "43",
			},
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod: %v", err)
	}

	if err := service.waitForDeploymentPodsDeleted(ctx, 8, 43, 10*time.Millisecond); err == nil {
		t.Fatalf("expected timeout while pod still exists")
	}
}

func TestInstanceDeploymentServiceEnsureDeletesLegacyDeployments(t *testing.T) {
	client := &Client{Clientset: fake.NewSimpleClientset(), Namespace: "clawreef"}
	service := &InstanceDeploymentService{
		client:           client,
		namespaceService: &NamespaceService{client: client},
	}
	ctx := context.Background()
	namespace := "clawreef-user-9"
	if _, err := client.Clientset.AppsV1().Deployments(namespace).Create(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "clawreef-44-old-name",
			Namespace: namespace,
			Labels: map[string]string{
				"app":         "clawreef",
				"instance-id": "44",
				"managed-by":  "clawreef",
			},
		},
	}, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create legacy deployment: %v", err)
	}

	if _, err := service.EnsureDeployment(ctx, PodConfig{
		InstanceID:    44,
		InstanceName:  "Pro Desktop",
		UserID:        9,
		Type:          "openclaw",
		RuntimeType:   "desktop",
		CPUCores:      2,
		MemoryGB:      4,
		Image:         "registry/openclaw:v2",
		MountPath:     "/config",
		ContainerPort: 3001,
	}, 1); err != nil {
		t.Fatalf("EnsureDeployment returned error: %v", err)
	}

	deployments, err := client.Clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "instance-id=44",
	})
	if err != nil {
		t.Fatalf("list deployments: %v", err)
	}
	if len(deployments.Items) != 1 {
		t.Fatalf("deployment count = %d, want 1: %#v", len(deployments.Items), deployments.Items)
	}
	if got := deployments.Items[0].Name; got != "clawreef-44-deployment" {
		t.Fatalf("remaining deployment = %q, want stable deployment", got)
	}
}
