package k8s

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const poolValidationRole = "image-validation"

type RuntimeValidationStatus struct {
	UID, PodUID, Endpoint, Reason string
	Protocol                      string
	Ready, Failed, Gone           bool
}

type RuntimePoolValidationService interface {
	EnsurePoolValidation(context.Context, RuntimeRolloutTarget, string, string, string) error
	ObservePoolValidation(context.Context, string, string, string) (*RuntimeValidationStatus, error)
	StopPoolValidation(context.Context, string, string, string) error
	RolloutPoolImage(context.Context, RuntimeRolloutTarget, string) error
	PoolRetirementReason(context.Context, RuntimeRolloutTarget, string) (string, error)
}

// A deleting Pod is not proof that its writers have exited. Ordinary inventory
// excludes terminating Pods, so pool replacement observes them explicitly.
func (s *runtimeDeploymentService) PoolRetirementReason(ctx context.Context, target RuntimeRolloutTarget, image string) (string, error) {
	d, err := s.client.AppsV1().Deployments(target.Namespace).Get(ctx, target.Name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if string(d.UID) != target.UID {
		return "", fmt.Errorf("source deployment identity changed")
	}
	selector, err := metav1.LabelSelectorAsSelector(d.Spec.Selector)
	if err != nil {
		return "", err
	}
	pods, err := s.client.CoreV1().Pods(target.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return "", err
	}
	for _, p := range pods.Items {
		owned := false
		for _, o := range p.OwnerReferences {
			if o.Kind == "ReplicaSet" {
				rs, e := s.client.AppsV1().ReplicaSets(target.Namespace).Get(ctx, o.Name, metav1.GetOptions{})
				if e != nil {
					return "", e
				}
				for _, parent := range rs.OwnerReferences {
					owned = owned || parent.UID == d.UID
				}
			}
		}
		if !owned || p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
			continue
		}
		if p.DeletionTimestamp != nil || runtimeContainerImage(p.Spec.Containers) != image {
			return "正在等待原 Runtime 进程退出：" + p.Name, nil
		}
	}
	return "", nil
}

func (s *runtimeDeploymentService) EnsurePoolValidation(ctx context.Context, source RuntimeRolloutTarget, name, image, operation string) error {
	deployments := s.client.AppsV1().Deployments(source.Namespace)
	original, err := deployments.Get(ctx, source.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if string(original.UID) != source.UID {
		return fmt.Errorf("source deployment identity changed before validation")
	}
	expected, err := buildPoolValidationDeployment(original, source, name, image, operation)
	if err != nil {
		return err
	}
	if existing, e := deployments.Get(ctx, name, metav1.GetOptions{}); e == nil {
		if existing.DeletionTimestamp != nil || existing.Labels[runtimePoolRoleLabel] != poolValidationRole || existing.Labels[runtimeUpgradeIDLabel] != operation || existing.Annotations["clawmanager.io/source-uid"] != source.UID || !reflect.DeepEqual(existing.Spec.Template, expected.Spec.Template) || !reflect.DeepEqual(existing.Spec.Selector, expected.Spec.Selector) {
			return fmt.Errorf("validation pool ownership or native template changed")
		}
		return nil
	} else if !apierrors.IsNotFound(e) {
		return e
	}
	_, err = deployments.Create(ctx, expected, metav1.CreateOptions{})
	return err
}

func buildPoolValidationDeployment(original *appsv1.Deployment, source RuntimeRolloutTarget, name, image, operation string) (*appsv1.Deployment, error) {
	d := original.DeepCopy()
	d.TypeMeta = metav1.TypeMeta{}
	identity := copyStringMap(original.Spec.Template.Labels)
	delete(identity, "pod-template-hash")
	delete(identity, runtimeSourceLabel)
	for key, value := range map[string]string{"app": name, "clawmanager.io/runtime-type": "openclaw", runtimePoolRoleLabel: poolValidationRole, runtimeSchedulingLabel: "false", runtimeUpgradeIDLabel: operation} {
		identity[key] = value
	}
	d.ObjectMeta = metav1.ObjectMeta{Name: name, Namespace: source.Namespace, Labels: copyStringMap(identity), Annotations: map[string]string{"clawmanager.io/source-uid": source.UID}}
	d.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}}
	one := int32(1)
	d.Spec.Replicas = &one
	d.Spec.Strategy = appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
	d.Spec.Template.ObjectMeta = metav1.ObjectMeta{Labels: copyStringMap(identity), Annotations: copyStringMap(original.Spec.Template.Annotations)}
	d.Spec.Template.Spec.TopologySpreadConstraints = nil
	d.Spec.Template.Spec.Affinity = nil
	d.Spec.Template.Spec.NodeName = ""
	d.Spec.Template.Spec.HostNetwork = false
	d.Spec.Template.Spec.HostPID = false
	d.Spec.Template.Spec.HostIPC = false
	// Every writable business volume is replaced, including secondary mounts.
	// Probe Gateway/file operations can never reach an instance PVC or NFS path.
	for i := range d.Spec.Template.Spec.Volumes {
		v := &d.Spec.Template.Spec.Volumes[i]
		if v.PersistentVolumeClaim != nil || v.NFS != nil || v.HostPath != nil || v.CSI != nil {
			v.VolumeSource = corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}
		}
	}
	var container *corev1.Container
	for i := range d.Spec.Template.Spec.Containers {
		if d.Spec.Template.Spec.Containers[i].Name == "runtime" {
			container = &d.Spec.Template.Spec.Containers[i]
			break
		}
	}
	if container == nil {
		return nil, fmt.Errorf("runtime container missing")
	}
	container.Image = image
	upsertEnvVar(container, "CLAWMANAGER_RUNTIME_IMAGE_REF", image)
	upsertEnvVar(container, "CLAWMANAGER_RUNTIME_IMAGE_DIGEST", runtimeImageDigest(image))
	upsertEnvVar(container, "CLAWMANAGER_RUNTIME_DEPLOYMENT_NAME", name)
	upsertEnvVar(container, "CLAWMANAGER_DEPLOYMENT_NAME", name)
	upsertEnvVar(container, "CLAWMANAGER_RUNTIME_UPGRADE_ID", "")
	upsertEnvVar(container, "POD_UID", "")
	for i := range container.Env {
		if container.Env[i].Name == "POD_UID" {
			container.Env[i] = corev1.EnvVar{Name: "POD_UID", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.uid"}}}
		}
	}
	for key, field := range map[string]string{"POD_NAME": "metadata.name", "POD_NAMESPACE": "metadata.namespace", "POD_IP": "status.podIP"} {
		upsertEnvVar(container, key, "")
		for i := range container.Env {
			if container.Env[i].Name == key {
				container.Env[i] = corev1.EnvVar{Name: key, ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: field}}}
			}
		}
	}
	syncRuntimeSlotImageAndIdentity(d, container)
	for i := range d.Spec.Template.Spec.InitContainers {
		if d.Spec.Template.Spec.InitContainers[i].Image == runtimeContainerImage(original.Spec.Template.Spec.Containers) {
			d.Spec.Template.Spec.InitContainers[i].Image = image
		}
	}
	container.ReadinessProbe = &corev1.Probe{ProbeHandler: corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(runtimeAgentPort)}}, PeriodSeconds: 2, TimeoutSeconds: 2, FailureThreshold: 3, SuccessThreshold: 1}
	if identity["clawmanager.io/runtime-protocol"] == "container-slots-v1" {
		setRuntimeSlotTLSReadiness(container)
	}
	return d, nil
}

func (s *runtimeDeploymentService) ObservePoolValidation(ctx context.Context, namespace, name, operation string) (*RuntimeValidationStatus, error) {
	d, err := s.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return &RuntimeValidationStatus{Gone: true}, nil
	}
	if err != nil {
		return nil, err
	}
	if d.Labels[runtimePoolRoleLabel] != poolValidationRole || d.Labels[runtimeUpgradeIDLabel] != operation {
		return nil, fmt.Errorf("validation deployment ownership changed")
	}
	pods, err := s.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labels.Set{"app": name}.String()})
	if err != nil {
		return nil, err
	}
	state := &RuntimeValidationStatus{UID: string(d.UID), Protocol: d.Spec.Template.Labels["clawmanager.io/runtime-protocol"], Reason: "正在启动验证 Runtime", Gone: len(pods.Items) == 0 && d.Spec.Replicas != nil && *d.Spec.Replicas == 0}
	for _, p := range pods.Items {
		if p.DeletionTimestamp != nil {
			continue
		}
		// app selector is unique and immutable; additionally verify the ReplicaSet.
		owned := false
		for _, o := range p.OwnerReferences {
			if o.Kind == "ReplicaSet" {
				rs, e := s.client.AppsV1().ReplicaSets(namespace).Get(ctx, o.Name, metav1.GetOptions{})
				if e != nil {
					return nil, e
				}
				for _, parent := range rs.OwnerReferences {
					if parent.UID == d.UID {
						owned = true
					}
				}
			}
		}
		if !owned {
			continue
		}
		for _, c := range p.Status.ContainerStatuses {
			if c.Name != "runtime" {
				continue
			}
			if c.State.Waiting != nil {
				state.Reason = "Runtime 启动等待：" + c.State.Waiting.Reason
				if c.State.Waiting.Reason == "InvalidImageName" || c.State.Waiting.Reason == "CreateContainerConfigError" {
					state.Failed = true
				}
			}
			if c.RestartCount >= 2 && c.LastTerminationState.Terminated != nil {
				state.Failed = true
				state.Reason = fmt.Sprintf("Runtime 连续启动失败，退出码 %d；请检查镜像启动配置", c.LastTerminationState.Terminated.ExitCode)
			}
			if c.Ready && c.State.Running != nil && p.Status.PodIP != "" {
				state.Ready = true
				state.PodUID = string(p.UID)
				scheme := "http"
				if state.Protocol == "container-slots-v1" {
					scheme = "https"
				}
				state.Endpoint = scheme + "://" + p.Status.PodIP + ":19090"
				state.Reason = ""
			}
		}
	}
	return state, nil
}

func (s *runtimeDeploymentService) StopPoolValidation(ctx context.Context, namespace, name, operation string) error {
	d, err := s.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if d.Labels[runtimePoolRoleLabel] != poolValidationRole || d.Labels[runtimeUpgradeIDLabel] != operation {
		return fmt.Errorf("refusing to stop unrelated deployment")
	}
	zero := int32(0)
	if d.Spec.Replicas != nil && *d.Spec.Replicas == 0 {
		return nil
	}
	d.Spec.Replicas = &zero
	_, err = s.client.AppsV1().Deployments(namespace).Update(ctx, d, metav1.UpdateOptions{})
	return err
}

func (s *runtimeDeploymentService) RolloutPoolImage(ctx context.Context, target RuntimeRolloutTarget, image string) error {
	if strings.TrimSpace(target.UID) == "" {
		return fmt.Errorf("deployment UID required")
	}
	return s.rolloutImage(ctx, target.Namespace, target.Name, image, "", 0, 1, false, target.UID)
}
