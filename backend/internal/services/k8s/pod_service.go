package k8s

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// PodService handles Pod operations
type PodService struct {
	client *Client
}

type PodSecurityMode string

const (
	podDeletionPollInterval   = 500 * time.Millisecond
	podDeletionTimeout        = 60 * time.Second
	deploymentDeletionTimeout = 60 * time.Second

	PodSecurityDefault        PodSecurityMode = "default"
	PodSecurityChromiumCompat PodSecurityMode = "chromium-compat"
	PodSecurityPrivileged     PodSecurityMode = "privileged"
)

// NewPodService creates a new Pod service
func NewPodService() *PodService {
	return &PodService{
		client: globalClient,
	}
}

// GetClient returns the k8s client
func (s *PodService) GetClient() *Client {
	return s.client
}

// PodConfig holds configuration for creating a pod
type PodConfig struct {
	InstanceID           int
	InstanceName         string
	UserID               int
	Type                 string
	RuntimeType          string
	CPUCores             float64
	MemoryGB             int
	GPUEnabled           bool
	GPUCount             int
	Image                string
	MountPath            string
	ContainerPort        int32
	ImagePullPolicy      corev1.PullPolicy
	ExtraEnv             map[string]string
	EnvFromSecretNames   []string
	ExtraPVCMounts       []PVCMount
	ConfigMapFileMounts  []ConfigMapFileMount
	VolumeInitScripts    []VolumeInitScript
	FSGroup              *int64
	NodeSelector         map[string]string
	VolumeOwnershipFixes []VolumeOwnershipFix
	SHMSizeGB            int
	SecurityMode         PodSecurityMode
}

type PVCMount struct {
	Name      string
	ClaimName string
	MountPath string
	ReadOnly  bool
}

type ConfigMapFileMount struct {
	Name          string
	ConfigMapName string
	Key           string
	MountPath     string
	ReadOnly      bool
	AsDirectory   bool
}

type VolumeOwnershipFix struct {
	Name      string
	MountPath string
	UID       int64
	GID       int64
}

type VolumeInitScript struct {
	Name      string
	MountPath string
	Script    string
}

// CreatePod creates a single-replica Deployment for an instance and returns the
// pod template that will be used for managed Pods. Callers should use GetPod to
// resolve the current real Pod after the Deployment controller creates it.
func (s *PodService) CreatePod(ctx context.Context, config PodConfig) (*corev1.Pod, error) {
	if s.client == nil {
		return nil, fmt.Errorf("k8s client not initialized")
	}

	deploymentName := s.client.GetDeploymentName(config.InstanceID, config.InstanceName)
	namespace := s.client.GetNamespace(config.UserID)
	pvcName := s.client.GetPVCName(config.InstanceID)
	runtimeType := normalizePodRuntimeType(config.RuntimeType)

	// Build resource requirements
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

	// Add GPU resources if enabled
	if config.GPUEnabled && config.GPUCount > 0 {
		resources.Limits["nvidia.com/gpu"] = resource.MustParse(fmt.Sprintf("%d", config.GPUCount))
		resources.Requests["nvidia.com/gpu"] = resource.MustParse(fmt.Sprintf("%d", config.GPUCount))
	}

	// Default container port
	if config.ContainerPort == 0 {
		config.ContainerPort = 3001
	}

	// Default image pull policy to IfNotPresent so that air-gapped and
	// enterprise environments can use locally cached images without being
	// forced to pull from a remote registry (fixes #94).
	pullPolicy := config.ImagePullPolicy
	if pullPolicy == "" {
		pullPolicy = corev1.PullIfNotPresent
	}

	annotations := map[string]string{}
	if config.SecurityMode == PodSecurityChromiumCompat {
		annotations["container.apparmor.security.beta.kubernetes.io/desktop"] = "unconfined"
	}

	labels := map[string]string{
		"app":           "clawreef",
		"instance-id":   fmt.Sprintf("%d", config.InstanceID),
		"instance-name": config.InstanceName,
		"user-id":       fmt.Sprintf("%d", config.UserID),
		"instance-type": config.Type,
		"runtime-type":  runtimeType,
		"managed-by":    "clawreef",
	}
	appendManagedRuntimeLabels(config.Type, labels)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        deploymentName,
			Namespace:   namespace,
			Annotations: copyStringMap(annotations),
			Labels:      copyStringMap(labels),
		},
		Spec: corev1.PodSpec{
			RestartPolicy:   corev1.RestartPolicyAlways,
			SecurityContext: buildPodSecurityContext(config.FSGroup),
			NodeSelector:    copyStringMap(config.NodeSelector),
			Containers: []corev1.Container{
				{
					Name:            "desktop",
					Image:           config.Image,
					ImagePullPolicy: pullPolicy,
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: config.ContainerPort,
							Name:          "http",
						},
					},
					StartupProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							TCPSocket: &corev1.TCPSocketAction{
								Port: intstrFromInt32(config.ContainerPort),
							},
						},
						FailureThreshold: 30,
						PeriodSeconds:    5,
						TimeoutSeconds:   2,
					},
					ReadinessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							TCPSocket: &corev1.TCPSocketAction{
								Port: intstrFromInt32(config.ContainerPort),
							},
						},
						InitialDelaySeconds: 3,
						PeriodSeconds:       5,
						TimeoutSeconds:      2,
						FailureThreshold:    6,
					},
					LivenessProbe: &corev1.Probe{
						ProbeHandler: corev1.ProbeHandler{
							TCPSocket: &corev1.TCPSocketAction{
								Port: intstrFromInt32(config.ContainerPort),
							},
						},
						InitialDelaySeconds: 15,
						PeriodSeconds:       10,
						TimeoutSeconds:      2,
						FailureThreshold:    3,
					},
					SecurityContext: buildContainerSecurityContext(config.SecurityMode),
					Resources:       resources,
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "data",
							MountPath: config.MountPath,
						},
					},
					Env: []corev1.EnvVar{
						{
							Name:  "INSTANCE_ID",
							Value: fmt.Sprintf("%d", config.InstanceID),
						},
						{
							Name:  "USER_ID",
							Value: fmt.Sprintf("%d", config.UserID),
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
						},
					},
				},
			},
		},
	}

	if runtimeType == "shell" {
		pod.Spec.Containers[0].Ports = nil
		pod.Spec.Containers[0].StartupProbe = nil
		pod.Spec.Containers[0].ReadinessProbe = nil
		pod.Spec.Containers[0].LivenessProbe = nil
	}

	for key, value := range config.ExtraEnv {
		pod.Spec.Containers[0].Env = append(pod.Spec.Containers[0].Env, corev1.EnvVar{
			Name:  key,
			Value: value,
		})
	}

	for _, mount := range config.ExtraPVCMounts {
		if mount.Name == "" || mount.ClaimName == "" || mount.MountPath == "" {
			continue
		}
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name: mount.Name,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: mount.ClaimName,
					ReadOnly:  mount.ReadOnly,
				},
			},
		})
		pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name:      mount.Name,
			MountPath: mount.MountPath,
			ReadOnly:  mount.ReadOnly,
		})
	}

	for index, initScript := range config.VolumeInitScripts {
		if initScript.Name == "" || initScript.MountPath == "" || initScript.Script == "" {
			continue
		}
		pod.Spec.InitContainers = append(pod.Spec.InitContainers, buildVolumeInitScriptContainer(index, config.Image, pullPolicy, initScript))
	}

	for index, fix := range config.VolumeOwnershipFixes {
		if fix.Name == "" || fix.MountPath == "" || fix.UID < 0 || fix.GID < 0 {
			continue
		}
		pod.Spec.InitContainers = append(pod.Spec.InitContainers, buildVolumeOwnershipInitContainer(index, config.Image, pullPolicy, fix))
	}

	for _, mount := range config.ConfigMapFileMounts {
		if mount.Name == "" || mount.ConfigMapName == "" || mount.Key == "" || mount.MountPath == "" {
			continue
		}
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name: mount.Name,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: mount.ConfigMapName},
					Items: []corev1.KeyToPath{
						{
							Key:  mount.Key,
							Path: mount.Key,
						},
					},
				},
			},
		})
		volumeMount := corev1.VolumeMount{
			Name:      mount.Name,
			MountPath: mount.MountPath,
			ReadOnly:  true,
		}
		if !mount.AsDirectory {
			volumeMount.SubPath = mount.Key
		}
		pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, volumeMount)
	}

	if config.SHMSizeGB > 0 {
		shmLimit := resource.MustParse(fmt.Sprintf("%dGi", config.SHMSizeGB))
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name: "shm",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					Medium:    corev1.StorageMediumMemory,
					SizeLimit: &shmLimit,
				},
			},
		})
		pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name:      "shm",
			MountPath: "/dev/shm",
		})
	}

	for _, secretName := range config.EnvFromSecretNames {
		if secretName == "" {
			continue
		}
		pod.Spec.Containers[0].EnvFrom = append(pod.Spec.Containers[0].EnvFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
			},
		})
	}

	replicas := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
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
					Annotations: copyStringMap(annotations),
					Labels:      copyStringMap(labels),
				},
				Spec: pod.Spec,
			},
		},
	}

	createdDeployment, err := s.client.Clientset.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			existingDeployment, getErr := s.client.Clientset.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
			if getErr == nil && existingDeployment != nil {
				if existingDeployment.DeletionTimestamp == nil {
					return podFromDeployment(existingDeployment), nil
				}

				if waitErr := s.waitForDeploymentDeletion(ctx, namespace, existingDeployment.Name); waitErr != nil {
					return nil, fmt.Errorf("failed waiting for deployment deletion %s: %w", existingDeployment.Name, waitErr)
				}

				createdDeployment, err = s.client.Clientset.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
				if err != nil {
					return nil, fmt.Errorf("failed to create deployment after deletion %s: %w", deploymentName, err)
				}
				return podFromDeployment(createdDeployment), nil
			}
		}
		return nil, fmt.Errorf("failed to create deployment %s: %w", deploymentName, err)
	}

	return podFromDeployment(createdDeployment), nil
}

func buildPodSecurityContext(fsGroup *int64) *corev1.PodSecurityContext {
	if fsGroup == nil || *fsGroup <= 0 {
		return nil
	}
	fsGroupValue := *fsGroup
	policy := corev1.FSGroupChangeOnRootMismatch
	return &corev1.PodSecurityContext{
		FSGroup:             &fsGroupValue,
		FSGroupChangePolicy: &policy,
	}
}

func buildContainerSecurityContext(mode PodSecurityMode) *corev1.SecurityContext {
	switch mode {
	case PodSecurityChromiumCompat:
		allowPrivilegeEscalation := true
		return &corev1.SecurityContext{
			AllowPrivilegeEscalation: &allowPrivilegeEscalation,
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeUnconfined,
			},
		}
	case PodSecurityPrivileged:
		privileged := true
		return &corev1.SecurityContext{
			Privileged: &privileged,
		}
	default:
		return nil
	}
}

func buildVolumeOwnershipInitContainer(index int, image string, pullPolicy corev1.PullPolicy, fix VolumeOwnershipFix) corev1.Container {
	rootUser := int64(0)
	return corev1.Container{
		Name:            fmt.Sprintf("fix-volume-permissions-%d", index+1),
		Image:           image,
		ImagePullPolicy: pullPolicy,
		Command: []string{
			"sh",
			"-c",
			`set -eu
target="${CLAWMANAGER_FIX_VOLUME_PATH}"
uid="${CLAWMANAGER_FIX_VOLUME_UID}"
gid="${CLAWMANAGER_FIX_VOLUME_GID}"
if [ -d "$target" ]; then
  chown -R "${uid}:${gid}" "$target" || true
  chmod -R ug+rwX "$target" || true
  find "$target" -type d -exec chmod g+s {} \; || true
fi`,
		},
		Env: []corev1.EnvVar{
			{Name: "CLAWMANAGER_FIX_VOLUME_PATH", Value: fix.MountPath},
			{Name: "CLAWMANAGER_FIX_VOLUME_UID", Value: fmt.Sprintf("%d", fix.UID)},
			{Name: "CLAWMANAGER_FIX_VOLUME_GID", Value: fmt.Sprintf("%d", fix.GID)},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:  &rootUser,
			RunAsGroup: &rootUser,
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      fix.Name,
				MountPath: fix.MountPath,
			},
		},
	}
}

func buildVolumeInitScriptContainer(index int, image string, pullPolicy corev1.PullPolicy, initScript VolumeInitScript) corev1.Container {
	rootUser := int64(0)
	return corev1.Container{
		Name:            fmt.Sprintf("init-volume-layout-%d", index+1),
		Image:           image,
		ImagePullPolicy: pullPolicy,
		Command: []string{
			"sh",
			"-c",
			initScript.Script,
		},
		Env: []corev1.EnvVar{
			{Name: "CLAWMANAGER_VOLUME_PATH", Value: initScript.MountPath},
		},
		SecurityContext: &corev1.SecurityContext{
			RunAsUser:  &rootUser,
			RunAsGroup: &rootUser,
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      initScript.Name,
				MountPath: initScript.MountPath,
			},
		},
	}
}

func normalizePodRuntimeType(runtimeType string) string {
	if runtimeType == "shell" {
		return "shell"
	}
	return "desktop"
}

func intstrFromInt32(port int32) intstr.IntOrString {
	return intstr.FromInt32(port)
}

func podFromDeployment(deployment *appsv1.Deployment) *corev1.Pod {
	if deployment == nil {
		return nil
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        deployment.Name,
			Namespace:   deployment.Namespace,
			Labels:      copyStringMap(deployment.Spec.Template.Labels),
			Annotations: copyStringMap(deployment.Spec.Template.Annotations),
		},
		Spec: deployment.Spec.Template.Spec,
	}
}

func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

// GetPod gets a pod by instance ID
func (s *PodService) GetPod(ctx context.Context, userID, instanceID int) (*corev1.Pod, error) {
	if s.client == nil {
		return nil, fmt.Errorf("k8s client not initialized")
	}

	// List pods with instance-id label
	namespace := s.client.GetNamespace(userID)
	selector := fmt.Sprintf("instance-id=%d", instanceID)

	pods, err := s.client.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("pod not found for instance %d", instanceID)
	}

	return selectCurrentPod(pods.Items), nil
}

func selectCurrentPod(pods []corev1.Pod) *corev1.Pod {
	var selected *corev1.Pod
	selectedRank := -1
	for i := range pods {
		pod := &pods[i]
		rank := podSelectionRank(pod)
		if rank > selectedRank {
			selected = pod
			selectedRank = rank
		}
	}
	if selected != nil {
		return selected
	}
	return &pods[0]
}

func podSelectionRank(pod *corev1.Pod) int {
	if pod == nil {
		return 0
	}
	if pod.DeletionTimestamp != nil {
		return 1
	}
	switch pod.Status.Phase {
	case corev1.PodRunning:
		if isPodReadyForSelection(pod) {
			return 6
		}
		return 5
	case corev1.PodPending:
		return 4
	case corev1.PodUnknown:
		return 3
	case corev1.PodSucceeded:
		return 2
	case corev1.PodFailed:
		return 2
	default:
		return 3
	}
}

func isPodReadyForSelection(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// DeletePod deletes the instance Deployment so managed Pods are not recreated.
func (s *PodService) DeletePod(ctx context.Context, userID, instanceID int) error {
	return s.DeleteDeployment(ctx, userID, instanceID)
}

// DeleteDeployment deletes all Deployments for an instance.
func (s *PodService) DeleteDeployment(ctx context.Context, userID, instanceID int) error {
	if s.client == nil {
		return fmt.Errorf("k8s client not initialized")
	}

	namespace := s.client.GetNamespace(userID)
	selector := fmt.Sprintf("instance-id=%d", instanceID)

	deployments, err := s.client.Clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		if isNotFoundError(err) {
			return nil
		}
		return fmt.Errorf("failed to list deployments: %w", err)
	}

	for _, deployment := range deployments.Items {
		propagation := metav1.DeletePropagationForeground
		err = s.client.Clientset.AppsV1().Deployments(namespace).Delete(ctx, deployment.Name, metav1.DeleteOptions{
			PropagationPolicy: &propagation,
		})
		if err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete deployment %s: %w", deployment.Name, err)
		}

		if err := s.waitForDeploymentDeletion(ctx, namespace, deployment.Name); err != nil {
			return fmt.Errorf("failed waiting for deployment %s to be deleted: %w", deployment.Name, err)
		}
	}

	pods, err := s.client.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return fmt.Errorf("failed to list pods after deployment deletion: %w", err)
	}
	for _, pod := range pods.Items {
		err = s.client.Clientset.CoreV1().Pods(namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete pod %s: %w", pod.Name, err)
		}
		if err := s.waitForPodDeletion(ctx, namespace, pod.Name); err != nil {
			return fmt.Errorf("failed waiting for pod %s to be deleted: %w", pod.Name, err)
		}
	}

	return nil
}

// GetPodStatus gets the status of a pod
func (s *PodService) GetPodStatus(ctx context.Context, userID, instanceID int) (*corev1.PodStatus, error) {
	pod, err := s.GetPod(ctx, userID, instanceID)
	if err != nil {
		return nil, err
	}
	return &pod.Status, nil
}

// GetPodIP gets the pod IP
func (s *PodService) GetPodIP(ctx context.Context, userID, instanceID int) (string, error) {
	pod, err := s.GetPod(ctx, userID, instanceID)
	if err != nil {
		return "", err
	}
	return pod.Status.PodIP, nil
}

// PodExists checks if a pod exists
func (s *PodService) PodExists(ctx context.Context, userID, instanceID int) (bool, error) {
	_, err := s.GetPod(ctx, userID, instanceID)
	if err != nil {
		if isNotFoundError(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// DeploymentExists checks if an instance Deployment exists.
func (s *PodService) DeploymentExists(ctx context.Context, userID, instanceID int) (bool, error) {
	if s.client == nil {
		return false, fmt.Errorf("k8s client not initialized")
	}

	namespace := s.client.GetNamespace(userID)
	selector := fmt.Sprintf("instance-id=%d", instanceID)
	deployments, err := s.client.Clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to list deployments: %w", err)
	}

	return len(deployments.Items) > 0, nil
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return containsSubstring(errStr, "not found") ||
		containsSubstring(errStr, "NotFound")
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func (s *PodService) waitForPodDeletion(ctx context.Context, namespace, podName string) error {
	waitCtx, cancel := context.WithTimeout(ctx, podDeletionTimeout)
	defer cancel()

	ticker := time.NewTicker(podDeletionPollInterval)
	defer ticker.Stop()

	for {
		_, err := s.client.Clientset.CoreV1().Pods(namespace).Get(waitCtx, podName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to check pod %s: %w", podName, err)
		}

		select {
		case <-waitCtx.Done():
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("timed out waiting for pod %s deletion", podName)
		case <-ticker.C:
		}
	}
}

func (s *PodService) waitForDeploymentDeletion(ctx context.Context, namespace, deploymentName string) error {
	waitCtx, cancel := context.WithTimeout(ctx, deploymentDeletionTimeout)
	defer cancel()

	ticker := time.NewTicker(podDeletionPollInterval)
	defer ticker.Stop()

	for {
		_, err := s.client.Clientset.AppsV1().Deployments(namespace).Get(waitCtx, deploymentName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to check deployment %s: %w", deploymentName, err)
		}

		select {
		case <-waitCtx.Done():
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("timed out waiting for deployment %s deletion", deploymentName)
		case <-ticker.C:
		}
	}
}
