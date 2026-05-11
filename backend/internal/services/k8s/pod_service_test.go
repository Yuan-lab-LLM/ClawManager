package k8s

import (
	"context"
	"testing"

	"k8s.io/client-go/kubernetes/fake"
)

func newTestPodService() *PodService {
	return &PodService{
		client: &Client{
			Clientset: fake.NewSimpleClientset(),
			Namespace: "clawmanager",
		},
	}
}

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

func TestCreateOpenClawPodUsesShortRuntimeHostnameForLongUserFacingName(t *testing.T) {
	ctx := context.Background()
	podService := newTestPodService()
	userFacingName := "gtclaw-fresh-bind-lan-auth-20260505-175724"

	pod, err := podService.CreatePod(ctx, PodConfig{
		InstanceID:    9,
		InstanceName:  userFacingName,
		UserID:        1,
		Type:          "openclaw",
		CPUCores:      1,
		MemoryGB:      2,
		Image:         "openclaw:test",
		MountPath:     "/data",
		ContainerPort: 3001,
	})
	if err != nil {
		t.Fatalf("CreatePod() error = %v", err)
	}

	if pod.Spec.Hostname == "" {
		t.Fatalf("Hostname was empty, want short OpenClaw runtime hostname")
	}
	if pod.Spec.Hostname != "clawreef-9" {
		t.Fatalf("Hostname = %q, want instance-id scoped runtime hostname clawreef-9", pod.Spec.Hostname)
	}
	decoratedLabel := pod.Spec.Hostname + " (OpenClaw)"
	if got := len([]byte(decoratedLabel)); got > 63 {
		t.Fatalf("decorated OpenClaw label byte length = %d for %q, want <= 63 bytes", got, decoratedLabel)
	}
	if got := len([]byte(decoratedLabel)); got != 21 {
		t.Fatalf("decorated OpenClaw label byte length = %d for %q, want exact proof length 21", got, decoratedLabel)
	}
	if pod.Spec.Hostname == pod.Name {
		t.Fatalf("Hostname = pod name %q, want runtime-facing hostname independent from long user-facing pod name", pod.Name)
	}
	if got := pod.Labels["instance-name"]; got != userFacingName {
		t.Fatalf("instance-name label = %q, want user-facing name preserved as %q", got, userFacingName)
	}
	if pod.Labels["instance-id"] != "9" || pod.Labels["app"] != "clawreef" {
		t.Fatalf("identity labels = %#v, want instance-id/app selector labels preserved", pod.Labels)
	}
}

func TestCreateNonOpenClawPodDoesNotSetRuntimeHostname(t *testing.T) {
	ctx := context.Background()
	podService := newTestPodService()

	pod, err := podService.CreatePod(ctx, PodConfig{
		InstanceID:    10,
		InstanceName:  "ubuntu-desktop-long-name-that-should-not-change-hostname",
		UserID:        1,
		Type:          "ubuntu",
		CPUCores:      1,
		MemoryGB:      2,
		Image:         "ubuntu:test",
		MountPath:     "/data",
		ContainerPort: 3001,
	})
	if err != nil {
		t.Fatalf("CreatePod() error = %v", err)
	}

	if pod.Spec.Hostname != "" {
		t.Fatalf("Hostname = %q, want empty hostname for non-OpenClaw pod", pod.Spec.Hostname)
	}
	if pod.Labels["instance-id"] != "10" || pod.Labels["app"] != "clawreef" {
		t.Fatalf("identity labels = %#v, want instance-id/app selector labels preserved", pod.Labels)
	}
}
