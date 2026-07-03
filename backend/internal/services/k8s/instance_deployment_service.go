package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const instanceDeploymentRestartedAtAnnotation = "clawmanager.io/restarted-at"

type InstanceDeploymentService struct {
	client           *Client
	namespaceService *NamespaceService
}

func NewInstanceDeploymentService() *InstanceDeploymentService {
	return &InstanceDeploymentService{
		client:           globalClient,
		namespaceService: NewNamespaceService(),
	}
}

func InstanceDeploymentName(instanceID int) string {
	return sanitizeK8sName(fmt.Sprintf("clawreef-%d-deployment", instanceID))
}

func (s *InstanceDeploymentService) EnsureDeployment(ctx context.Context, config PodConfig, replicas int32) (*appsv1.Deployment, error) {
	if s.client == nil {
		return nil, fmt.Errorf("k8s client not initialized")
	}
	if _, err := s.namespaceService.EnsureNamespace(ctx, config.UserID); err != nil {
		return nil, fmt.Errorf("failed to ensure namespace: %w", err)
	}

	desired := BuildInstanceDeployment(s.client, config, replicas)
	deployments := s.client.Clientset.AppsV1().Deployments(desired.Namespace)
	if err := s.deleteLegacyDeployments(ctx, desired.Namespace, config.InstanceID, desired.Name); err != nil {
		return nil, err
	}
	existing, err := deployments.Get(ctx, desired.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		created, createErr := deployments.Create(ctx, desired, metav1.CreateOptions{})
		if createErr != nil {
			return nil, fmt.Errorf("failed to create instance deployment %s/%s: %w", desired.Namespace, desired.Name, createErr)
		}
		return created, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get instance deployment %s/%s: %w", desired.Namespace, desired.Name, err)
	}
	if !reflect.DeepEqual(existing.Spec.Selector, desired.Spec.Selector) {
		return nil, fmt.Errorf("instance deployment %s/%s selector mismatch; delete and recreate the deployment to change immutable selector", desired.Namespace, desired.Name)
	}

	updated := existing.DeepCopy()
	if updated.Labels == nil {
		updated.Labels = map[string]string{}
	}
	for key, value := range desired.Labels {
		updated.Labels[key] = value
	}
	updated.Spec.Replicas = desired.Spec.Replicas
	updated.Spec.Template = desired.Spec.Template

	result, updateErr := deployments.Update(ctx, updated, metav1.UpdateOptions{})
	if updateErr != nil {
		return nil, fmt.Errorf("failed to update instance deployment %s/%s: %w", desired.Namespace, desired.Name, updateErr)
	}
	return result, nil
}

func (s *InstanceDeploymentService) deleteLegacyDeployments(ctx context.Context, namespace string, instanceID int, expectedName string) error {
	deployments := s.client.Clientset.AppsV1().Deployments(namespace)
	items, err := deployments.List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("instance-id=%d", instanceID),
	})
	if err != nil {
		return fmt.Errorf("failed to list instance deployments %s/instance-id=%d: %w", namespace, instanceID, err)
	}

	for _, item := range items.Items {
		if item.Name == expectedName {
			continue
		}
		propagation := metav1.DeletePropagationForeground
		if err := deployments.Delete(ctx, item.Name, metav1.DeleteOptions{PropagationPolicy: &propagation}); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete legacy instance deployment %s/%s: %w", namespace, item.Name, err)
		}
	}
	return nil
}

func (s *InstanceDeploymentService) ScaleDeployment(ctx context.Context, userID, instanceID int, replicas int32) error {
	if s.client == nil {
		return fmt.Errorf("k8s client not initialized")
	}
	namespace := s.client.GetNamespace(userID)
	name := InstanceDeploymentName(instanceID)
	patch, err := json.Marshal(map[string]any{
		"spec": map[string]any{"replicas": replicas},
	})
	if err != nil {
		return fmt.Errorf("failed to build instance deployment scale patch: %w", err)
	}
	if _, err := s.client.Clientset.AppsV1().Deployments(namespace).Patch(ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("failed to scale instance deployment %s/%s: %w", namespace, name, err)
	}
	return nil
}

func (s *InstanceDeploymentService) WaitForDeploymentPodsDeleted(ctx context.Context, userID, instanceID int) error {
	return s.waitForDeploymentPodsDeleted(ctx, userID, instanceID, podDeletionTimeout)
}

func (s *InstanceDeploymentService) waitForDeploymentPodsDeleted(ctx context.Context, userID, instanceID int, timeout time.Duration) error {
	if s.client == nil {
		return fmt.Errorf("k8s client not initialized")
	}
	if timeout <= 0 {
		timeout = podDeletionTimeout
	}

	namespace := s.client.GetNamespace(userID)
	selector := fmt.Sprintf("instance-id=%d,app=clawreef", instanceID)
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	ticker := time.NewTicker(podDeletionPollInterval)
	defer ticker.Stop()

	for {
		pods, err := s.client.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return fmt.Errorf("failed to list instance deployment pods: %w", err)
		}
		if len(pods.Items) == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for instance deployment pods to be deleted")
		case <-ticker.C:
		}
	}
}

func (s *InstanceDeploymentService) RestartDeployment(ctx context.Context, userID, instanceID int) error {
	if s.client == nil {
		return fmt.Errorf("k8s client not initialized")
	}
	namespace := s.client.GetNamespace(userID)
	name := InstanceDeploymentName(instanceID)
	patch, err := json.Marshal(map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]string{
						instanceDeploymentRestartedAtAnnotation: time.Now().UTC().Format(time.RFC3339Nano),
					},
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to build instance deployment restart patch: %w", err)
	}
	if _, err := s.client.Clientset.AppsV1().Deployments(namespace).Patch(ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("failed to restart instance deployment %s/%s: %w", namespace, name, err)
	}
	return nil
}

func (s *InstanceDeploymentService) DeleteDeployment(ctx context.Context, userID, instanceID int) error {
	if s.client == nil {
		return fmt.Errorf("k8s client not initialized")
	}
	namespace := s.client.GetNamespace(userID)
	name := InstanceDeploymentName(instanceID)
	err := s.client.Clientset.AppsV1().Deployments(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete instance deployment %s/%s: %w", namespace, name, err)
	}
	return nil
}

func (s *InstanceDeploymentService) GetDeployment(ctx context.Context, userID, instanceID int) (*appsv1.Deployment, error) {
	if s.client == nil {
		return nil, fmt.Errorf("k8s client not initialized")
	}
	namespace := s.client.GetNamespace(userID)
	name := InstanceDeploymentName(instanceID)
	deployment, err := s.client.Clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get instance deployment %s/%s: %w", namespace, name, err)
	}
	return deployment, nil
}

func (s *InstanceDeploymentService) GetActivePod(ctx context.Context, userID, instanceID int) (*corev1.Pod, error) {
	if s.client == nil {
		return nil, fmt.Errorf("k8s client not initialized")
	}
	namespace := s.client.GetNamespace(userID)
	selector := fmt.Sprintf("instance-id=%d,app=clawreef", instanceID)
	pods, err := s.client.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("failed to list instance deployment pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("pod not found for instance %d", instanceID)
	}
	for _, pod := range pods.Items {
		if pod.DeletionTimestamp == nil && pod.Status.Phase == corev1.PodRunning && podReady(&pod) {
			selected := pod
			return &selected, nil
		}
	}
	selected := pods.Items[0]
	return &selected, nil
}

func BuildInstanceDeployment(client *Client, config PodConfig, replicas int32) *appsv1.Deployment {
	namespace := client.GetNamespace(config.UserID)
	name := InstanceDeploymentName(config.InstanceID)
	runtimeType := normalizePodRuntimeType(config.RuntimeType)
	labels := map[string]string{
		"app":           "clawreef",
		"instance-id":   fmt.Sprintf("%d", config.InstanceID),
		"instance-name": config.InstanceName,
		"user-id":       fmt.Sprintf("%d", config.UserID),
		"instance-type": config.Type,
		"runtime-type":  runtimeType,
		"managed-by":    "clawreef",
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    copyStringMap(labels),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":         "clawreef",
					"instance-id": fmt.Sprintf("%d", config.InstanceID),
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: instanceDeploymentAnnotations(config),
					Labels:      labels,
				},
				Spec: buildInstanceDeploymentPodSpec(client, config, runtimeType),
			},
		},
	}
}

func instanceDeploymentAnnotations(config PodConfig) map[string]string {
	if config.SecurityMode == PodSecurityChromiumCompat {
		return map[string]string{"container.apparmor.security.beta.kubernetes.io/desktop": "unconfined"}
	}
	return nil
}

func buildInstanceDeploymentPodSpec(client *Client, config PodConfig, runtimeType string) corev1.PodSpec {
	pvcName := client.GetPVCName(config.InstanceID)
	if config.ContainerPort == 0 {
		config.ContainerPort = 3001
	}
	pullPolicy := config.ImagePullPolicy
	if pullPolicy == "" {
		pullPolicy = corev1.PullIfNotPresent
	}

	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%g", config.CPUCores)),
			corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dGi", config.MemoryGB)),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%g", config.CPUCores)),
			corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dGi", config.MemoryGB)),
		},
	}
	if config.GPUEnabled && config.GPUCount > 0 {
		resources.Limits["nvidia.com/gpu"] = resource.MustParse(fmt.Sprintf("%d", config.GPUCount))
		resources.Requests["nvidia.com/gpu"] = resource.MustParse(fmt.Sprintf("%d", config.GPUCount))
	}

	container := corev1.Container{
		Name:            "desktop",
		Image:           config.Image,
		ImagePullPolicy: pullPolicy,
		Ports: []corev1.ContainerPort{{
			ContainerPort: config.ContainerPort,
			Name:          "http",
		}},
		StartupProbe: &corev1.Probe{
			ProbeHandler:     corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstrFromInt32(config.ContainerPort)}},
			FailureThreshold: 30,
			PeriodSeconds:    5,
			TimeoutSeconds:   2,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler:        corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstrFromInt32(config.ContainerPort)}},
			InitialDelaySeconds: 3,
			PeriodSeconds:       5,
			TimeoutSeconds:      2,
			FailureThreshold:    6,
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler:        corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstrFromInt32(config.ContainerPort)}},
			InitialDelaySeconds: 15,
			PeriodSeconds:       10,
			TimeoutSeconds:      2,
			FailureThreshold:    3,
		},
		SecurityContext: buildContainerSecurityContext(config.SecurityMode),
		Resources:       resources,
		VolumeMounts: []corev1.VolumeMount{{
			Name:      "data",
			MountPath: config.MountPath,
		}},
		Env: []corev1.EnvVar{
			{Name: "INSTANCE_ID", Value: fmt.Sprintf("%d", config.InstanceID)},
			{Name: "USER_ID", Value: fmt.Sprintf("%d", config.UserID)},
		},
	}
	if runtimeType == "shell" {
		container.Ports = nil
		container.StartupProbe = nil
		container.ReadinessProbe = nil
		container.LivenessProbe = nil
	}
	for key, value := range config.ExtraEnv {
		container.Env = append(container.Env, corev1.EnvVar{Name: key, Value: value})
	}
	for _, secretName := range config.EnvFromSecretNames {
		if secretName == "" {
			continue
		}
		container.EnvFrom = append(container.EnvFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: secretName}},
		})
	}

	spec := corev1.PodSpec{
		RestartPolicy:   corev1.RestartPolicyAlways,
		SecurityContext: buildPodSecurityContext(config.FSGroup),
		NodeSelector:    copyStringMap(config.NodeSelector),
		Containers:      []corev1.Container{container},
		Volumes: []corev1.Volume{{
			Name: "data",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvcName},
			},
		}},
	}

	for _, mount := range config.ExtraPVCMounts {
		if mount.Name == "" || mount.ClaimName == "" || mount.MountPath == "" {
			continue
		}
		spec.Volumes = append(spec.Volumes, corev1.Volume{
			Name: mount.Name,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: mount.ClaimName, ReadOnly: mount.ReadOnly},
			},
		})
		spec.Containers[0].VolumeMounts = append(spec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name:      mount.Name,
			MountPath: mount.MountPath,
			ReadOnly:  mount.ReadOnly,
		})
	}

	for index, fix := range config.VolumeOwnershipFixes {
		if fix.Name == "" || fix.MountPath == "" || fix.UID < 0 || fix.GID < 0 {
			continue
		}
		spec.InitContainers = append(spec.InitContainers, buildVolumeOwnershipInitContainer(index, config.Image, pullPolicy, fix))
	}

	for _, mount := range config.ConfigMapFileMounts {
		if mount.Name == "" || mount.ConfigMapName == "" || mount.Key == "" || mount.MountPath == "" {
			continue
		}
		spec.Volumes = append(spec.Volumes, corev1.Volume{
			Name: mount.Name,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: mount.ConfigMapName},
					Items:                []corev1.KeyToPath{{Key: mount.Key, Path: mount.Key}},
				},
			},
		})
		volumeMount := corev1.VolumeMount{Name: mount.Name, MountPath: mount.MountPath, ReadOnly: true}
		if !mount.AsDirectory {
			volumeMount.SubPath = mount.Key
		}
		spec.Containers[0].VolumeMounts = append(spec.Containers[0].VolumeMounts, volumeMount)
	}

	if config.SHMSizeGB > 0 {
		shmLimit := resource.MustParse(fmt.Sprintf("%dGi", config.SHMSizeGB))
		spec.Volumes = append(spec.Volumes, corev1.Volume{
			Name: "shm",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory, SizeLimit: &shmLimit},
			},
		})
		spec.Containers[0].VolumeMounts = append(spec.Containers[0].VolumeMounts, corev1.VolumeMount{Name: "shm", MountPath: "/dev/shm"})
	}

	return spec
}

func podReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}
