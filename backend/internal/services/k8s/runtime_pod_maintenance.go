package k8s

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const runtimeManagedPoolLabel = "clawmanager.io/managed-pool"

type RuntimeResourceBudget struct {
	CPURequest    string `json:"cpu_request"`
	MemoryRequest string `json:"memory_request"`
	CPULimit      string `json:"cpu_limit"`
	MemoryLimit   string `json:"memory_limit"`
}

func runtimeResourceBudget(r corev1.ResourceRequirements) RuntimeResourceBudget {
	return RuntimeResourceBudget{CPURequest: r.Requests.Cpu().String(), MemoryRequest: r.Requests.Memory().String(), CPULimit: r.Limits.Cpu().String(), MemoryLimit: r.Limits.Memory().String()}
}

type RuntimeManagedPool struct {
	Namespace      string `json:"namespace"`
	DeploymentName string `json:"deployment_name"`
	Replicas       int32  `json:"replicas"`
	DeploymentUID  string `json:"deployment_uid"`
}
type RuntimePodMaintenanceDeploymentService interface {
	ListManagedRuntimePools(context.Context, string) ([]RuntimeManagedPool, error)
	DeleteManagedRuntimePod(context.Context, string, string, string, string) error
	InspectManagedRuntimePool(context.Context, string, string, string) (*runtimeManagedPool, error)
	DeleteManagedRuntimePool(context.Context, string, string, string) error
}

func (s *runtimeDeploymentService) maintenanceSource(ctx context.Context, namespace, podName, uid string) (*appsv1.Deployment, *corev1.Pod, string, error) {
	pod, err := s.client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, nil, "", err
	}
	if uid == "" || string(pod.UID) != uid || pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
		return nil, nil, "", fmt.Errorf("fresh healthy Pod UID required")
	}
	ready := false
	for _, c := range pod.Status.Conditions {
		if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
			ready = true
		}
	}
	if !ready {
		return nil, nil, "", fmt.Errorf("template Pod is not ready")
	}
	owner := metav1.GetControllerOf(pod)
	if owner == nil || owner.Kind != "ReplicaSet" {
		return nil, nil, "", fmt.Errorf("template Pod must belong to a Deployment")
	}
	rs, err := s.client.AppsV1().ReplicaSets(namespace).Get(ctx, owner.Name, metav1.GetOptions{})
	if err != nil {
		return nil, nil, "", err
	}
	if rs.UID != owner.UID {
		return nil, nil, "", fmt.Errorf("ReplicaSet UID changed")
	}
	deploymentOwner := metav1.GetControllerOf(rs)
	if deploymentOwner == nil || deploymentOwner.Kind != "Deployment" {
		return nil, nil, "", fmt.Errorf("ReplicaSet must belong to a Deployment")
	}
	deployment, err := s.client.AppsV1().Deployments(namespace).Get(ctx, deploymentOwner.Name, metav1.GetOptions{})
	if err != nil {
		return nil, nil, "", err
	}
	if deployment.UID != deploymentOwner.UID || deployment.DeletionTimestamp != nil || deployment.Labels[upgradeLabPurposeLabel] == upgradeLabPurposeValue || deployment.Labels[runtimeSchedulingLabel] == "false" || deployment.Labels[runtimePoolRoleLabel] == "upgrade-standby" {
		return nil, nil, "", fmt.Errorf("template Deployment is not an active ordinary pool")
	}
	if deployment.Spec.Template.Labels["clawmanager.io/runtime-protocol"] != "container-slots-v1" || len(pod.Spec.Containers) < 2 || pod.Spec.Containers[0].Name != "runtime" {
		return nil, nil, "", fmt.Errorf("template requires native container slots")
	}
	role := deployment.Labels[runtimePoolRoleLabel]
	if role != "" && !(role == "upgrade-target" && deployment.Labels[runtimeSchedulingLabel] == "true") {
		return nil, nil, "", fmt.Errorf("template requires an ordinary pool or an explicitly activated upgrade target")
	}
	for _, metadata := range []map[string]string{pod.Labels, deployment.Spec.Template.Labels} {
		actualRole := metadata[runtimePoolRoleLabel]
		if metadata[upgradeLabPurposeLabel] == upgradeLabPurposeValue || (actualRole != "" && !(actualRole == "upgrade-target" && role == "upgrade-target" && deployment.Labels[runtimeSchedulingLabel] == "true")) {
			return nil, nil, "", fmt.Errorf("template Pod metadata does not identify an active native serving pool")
		}
	}
	literal := func(c corev1.Container, key string) string {
		value := ""
		count := 0
		for _, e := range c.Env {
			if e.Name == key {
				count++
				if e.ValueFrom == nil {
					value = e.Value
				}
			}
		}
		if count != 1 {
			return ""
		}
		return value
	}
	root := literal(pod.Spec.Containers[0], "RUNTIME_WORKSPACE_ROOT")
	if root == "" || root == "/" || !path.IsAbs(root) || path.Clean(root) != root || literal(pod.Spec.Containers[0], "RUNTIME_AGENT_ROLE") != "pool" || literal(pod.Spec.Containers[0], "RUNTIME_SLOT_COUNT") != strconv.Itoa(len(pod.Spec.Containers)-1) {
		return nil, nil, "", fmt.Errorf("template coordinator must be a native slot pool with a canonical workspace root")
	}
	for i, c := range pod.Spec.Containers {
		if literal(c, "CLAWMANAGER_RUNTIME_TYPE") != deployment.Labels["clawmanager.io/runtime-type"] || literal(c, "RUNTIME_WORKSPACE_ROOT") != root {
			return nil, nil, "", fmt.Errorf("template runtime identity or workspace root differs between containers")
		}
		if i > 0 && (c.Name != fmt.Sprintf("slot-%d", i-1) || literal(c, "RUNTIME_AGENT_ROLE") != "slot" || literal(c, "RUNTIME_SLOT_INDEX") != strconv.Itoa(i-1) || literal(c, "RUNTIME_GATEWAY_CAPACITY") != "1") {
			return nil, nil, "", fmt.Errorf("template slot role, index or capacity is invalid")
		}
		mounts := 0
		for _, mount := range c.VolumeMounts {
			if mount.Name == runtimeWorkspaceVolume {
				mounts++
				if mount.ReadOnly || mount.MountPath != root || mount.SubPath != "" || mount.SubPathExpr != "" {
					return nil, nil, "", fmt.Errorf("every native container requires the same writable whole workspace PVC mount")
				}
			}
		}
		if mounts != 1 {
			return nil, nil, "", fmt.Errorf("native container workspace mount is missing or duplicated")
		}
	}
	var claimName string
	for _, volume := range pod.Spec.Volumes {
		if volume.Name == runtimeWorkspaceVolume {
			if claimName != "" || volume.PersistentVolumeClaim == nil || volume.PersistentVolumeClaim.ReadOnly || volume.PersistentVolumeClaim.ClaimName == "" {
				return nil, nil, "", fmt.Errorf("canonical workspace volume must be a writable PVC")
			}
			claimName = volume.PersistentVolumeClaim.ClaimName
		}
	}
	if claimName == "" {
		return nil, nil, "", fmt.Errorf("canonical workspace PVC is missing")
	}
	claim, err := s.client.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, claimName, metav1.GetOptions{})
	if err != nil {
		return nil, nil, "", fmt.Errorf("inspect template workspace PVC: %w", err)
	}
	if claim.UID == "" || claim.DeletionTimestamp != nil || claim.Status.Phase != corev1.ClaimBound || claim.Spec.VolumeName == "" || runtimeWorkspaceStorageIdentity(*pod, claim) == "" {
		return nil, nil, "", fmt.Errorf("template workspace storage identity is unavailable")
	}
	live := deployment.DeepCopy()
	live.Spec.Template.Spec = *pod.Spec.DeepCopy()
	if err := validateRuntimeSlotTLSDeployment(live); err != nil {
		return nil, nil, "", err
	}
	image := ""
	for _, container := range pod.Spec.Containers {
		if container.Name != "runtime" && !strings.HasPrefix(container.Name, "slot-") {
			return nil, nil, "", fmt.Errorf("template contains an unsupported sidecar")
		}
		found := false
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name == container.Name && status.Ready {
				ref := strings.TrimPrefix(strings.TrimPrefix(status.ImageID, "docker-pullable://"), "containerd://")
				digest := runtimeImageDigest(ref)
				if digest == "" {
					continue
				}
				if image == "" {
					if strings.Contains(ref, "@sha256:") {
						image = ref
					} else {
						image = strings.Split(container.Image, "@")[0] + "@" + digest
					}
				}
				if runtimeImageDigest(image) != digest {
					return nil, nil, "", fmt.Errorf("template containers run different images")
				}
				found = true
			}
		}
		if !found {
			return nil, nil, "", fmt.Errorf("immutable running image unavailable")
		}
	}

	return deployment, pod, image, nil
}

// runtimeManagedPool contains only server-side inventory; Pod specs are never serialized.
type runtimeManagedPool struct {
	Deployment *appsv1.Deployment
	Pods       []corev1.Pod
}

func (s *runtimeDeploymentService) InspectManagedRuntimePool(ctx context.Context, namespace, name, uid string) (*runtimeManagedPool, error) {
	deployment, err := s.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if deployment.Labels[runtimeManagedPoolLabel] != "true" || deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 || deployment.Labels[runtimeUpgradeIDLabel] != "" || deployment.Labels[upgradeLabPurposeLabel] == upgradeLabPurposeValue || deployment.DeletionTimestamp != nil {
		return nil, fmt.Errorf("deletion requires an owned single-replica managed pool; shared deployments require coordinated maintenance")
	}
	if uid != "" && string(deployment.UID) != uid {
		return nil, fmt.Errorf("deployment UID changed")
	}
	pods, err := s.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "app=" + name})
	if err != nil {
		return nil, err
	}
	if len(pods.Items) > 1 {
		return nil, fmt.Errorf("pool has multiple Pods; retry after controller settles")
	}
	maps, err := s.client.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{LabelSelector: "clawmanager.io/purpose=lite-instance"})
	if err != nil {
		return nil, err
	}
	for _, pod := range pods.Items {
		if pod.UID == "" || pod.DeletionTimestamp != nil {
			return nil, fmt.Errorf("fresh stable Pod UID required")
		}
		owner := metav1.GetControllerOf(&pod)
		if owner == nil || owner.Kind != "ReplicaSet" {
			return nil, fmt.Errorf("Pod controller is not a ReplicaSet")
		}
		rs, err := s.client.AppsV1().ReplicaSets(namespace).Get(ctx, owner.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		ref := metav1.GetControllerOf(rs)
		if rs.UID != owner.UID || ref == nil || ref.Kind != "Deployment" || ref.UID != deployment.UID || ref.Name != name {
			return nil, fmt.Errorf("Pod ownership changed")
		}
		for _, cm := range maps.Items {
			if cm.Data["desired_state"] == "stopped" {
				continue
			}
			endpoint := cm.Data["endpoint"]
			if cm.Data["pod_uid"] == string(pod.UID) || (pod.Status.PodIP != "" && strings.Contains(endpoint, "://"+pod.Status.PodIP+":")) {
				return nil, fmt.Errorf("Pod has a native runtime assignment")
			}
		}
	}
	return &runtimeManagedPool{Deployment: deployment, Pods: pods.Items}, nil
}
func (s *runtimeDeploymentService) DeleteManagedRuntimePool(ctx context.Context, namespace, name, uid string) error {
	if uid == "" {
		return fmt.Errorf("deployment_uid required")
	}
	pool, err := s.InspectManagedRuntimePool(ctx, namespace, name, uid)
	if apierrors.IsNotFound(err) {
		pods, listErr := s.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "app=" + name})
		if listErr != nil {
			return listErr
		}
		if len(pods.Items) != 0 {
			return fmt.Errorf("Runtime controller is gone but its Pods remain; await confirmed cleanup")
		}
		return nil
	}
	if err != nil {
		return err
	}
	deployment := pool.Deployment
	if deployment.UID == "" || deployment.ResourceVersion == "" {
		return fmt.Errorf("deployment UID and resourceVersion required")
	}
	foreground := metav1.DeletePropagationForeground
	version := deployment.ResourceVersion
	return s.client.AppsV1().Deployments(namespace).Delete(ctx, name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &deployment.UID, ResourceVersion: &version}, PropagationPolicy: &foreground})
}
func (s *runtimeDeploymentService) DeleteManagedRuntimePod(ctx context.Context, namespace, deploymentName, podName, uid string) error {
	pool, err := s.InspectManagedRuntimePool(ctx, namespace, deploymentName, "")
	if err != nil {
		return err
	}
	if len(pool.Pods) != 1 || pool.Pods[0].Name != podName || uid == "" || string(pool.Pods[0].UID) != uid {
		return fmt.Errorf("selected Pod UID changed")
	}
	return s.DeleteManagedRuntimePool(ctx, namespace, deploymentName, string(pool.Deployment.UID))
}

func (s *runtimeDeploymentService) ListManagedRuntimePools(ctx context.Context, namespace string) ([]RuntimeManagedPool, error) {
	deployments, err := s.client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{LabelSelector: runtimeManagedPoolLabel + "=true"})
	if err != nil {
		return nil, err
	}
	result := make([]RuntimeManagedPool, 0, len(deployments.Items))
	for _, deployment := range deployments.Items {
		replicas := int32(0)
		if deployment.Spec.Replicas != nil {
			replicas = *deployment.Spec.Replicas
		}
		result = append(result, RuntimeManagedPool{Namespace: namespace, DeploymentName: deployment.Name, DeploymentUID: string(deployment.UID), Replicas: replicas})
	}
	return result, nil
}
