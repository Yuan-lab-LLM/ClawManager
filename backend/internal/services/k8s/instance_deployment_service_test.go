package k8s

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestBuildInstanceDeploymentUsesStableIdentityAndPVC(t *testing.T) {
	client := &Client{Clientset: fake.NewSimpleClientset(), Namespace: "clawreef"}
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
