package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

const (
	runtimeAgentPort        int32 = 19090
	runtimeWorkspaceVolume        = "workspaces"
	defaultWorkspaceMount         = "/workspaces"
	defaultGatewayPortStart       = 20000
	defaultGatewayPortEnd         = 20299
	hermesTUIDir                  = "/usr/local/lib/hermes-agent/ui-tui"
)

type RuntimeDeploymentSpec struct {
	Name                  string
	Namespace             string
	RuntimeType           string
	Image                 string
	Replicas              int32
	WorkspacePVCClaimName string
	WorkspaceNFSServer    string
	WorkspaceNFSPath      string
	WorkspaceMountPath    string
	GatewayPortStart      int
	GatewayPortEnd        int
	AgentControlToken     string
	AgentReportToken      string
	BackendURL            string
	TrustedProxyCIDRs     string
}

type RuntimeDeploymentPod struct {
	RuntimeType    string
	Namespace      string
	DeploymentName string
	PodName        string
	PodIP          *string
	NodeName       *string
	ImageRef       string
	State          string
}

type RuntimeDeploymentService interface {
	Ensure(ctx context.Context, spec RuntimeDeploymentSpec) error
	Scale(ctx context.Context, namespace, name string, replicas int32) error
	RolloutImage(ctx context.Context, namespace, name, image string, maxUnavailable, maxSurge int) error
	ListPods(ctx context.Context, namespace, runtimeType string) ([]RuntimeDeploymentPod, error)
}

type runtimeDeploymentService struct {
	client kubernetes.Interface
}

func NewRuntimeDeploymentService(client kubernetes.Interface) RuntimeDeploymentService {
	return &runtimeDeploymentService{client: client}
}

func BuildRuntimeDeployment(spec RuntimeDeploymentSpec) *appsv1.Deployment {
	labels := map[string]string{
		"app":                         spec.Name,
		"clawmanager.io/runtime-type": spec.RuntimeType,
	}
	replicas := spec.Replicas
	workspaceMountPath := runtimeWorkspaceMountPath(spec.WorkspaceMountPath)
	gatewayPortStart := runtimeGatewayPortValue(spec.GatewayPortStart, defaultGatewayPortStart)
	gatewayPortEnd := runtimeGatewayPortValue(spec.GatewayPortEnd, defaultGatewayPortEnd)
	env := buildRuntimeAgentEnv(spec, workspaceMountPath, gatewayPortStart, gatewayPortEnd)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Name,
			Namespace: spec.Namespace,
			Labels:    copyStringMap(labels),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: copyStringMap(labels),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: copyStringMap(labels),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "runtime",
							Image: spec.Image,
							Ports: []corev1.ContainerPort{
								{
									Name:          "agent",
									ContainerPort: runtimeAgentPort,
								},
							},
							Env: env,
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      runtimeWorkspaceVolume,
									MountPath: workspaceMountPath,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name:         runtimeWorkspaceVolume,
							VolumeSource: runtimeWorkspaceVolumeSource(spec),
						},
					},
				},
			},
		},
	}
}

func runtimeWorkspaceVolumeSource(spec RuntimeDeploymentSpec) corev1.VolumeSource {
	if claimName := strings.TrimSpace(spec.WorkspacePVCClaimName); claimName != "" {
		return corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: claimName,
			},
		}
	}
	if server := strings.TrimSpace(spec.WorkspaceNFSServer); server != "" {
		nfsPath := strings.TrimSpace(spec.WorkspaceNFSPath)
		if nfsPath == "" {
			nfsPath = "/"
		}
		return corev1.VolumeSource{
			NFS: &corev1.NFSVolumeSource{
				Server: server,
				Path:   nfsPath,
			},
		}
	}
	return corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}
}

func buildRuntimeAgentEnv(spec RuntimeDeploymentSpec, workspaceRoot string, gatewayPortStart, gatewayPortEnd int) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{Name: "CLAWMANAGER_RUNTIME_TYPE", Value: spec.RuntimeType},
		{Name: "CLAWMANAGER_BACKEND_URL", Value: runtimeBackendURL(spec)},
		{Name: "CLAWMANAGER_RUNTIME_DEPLOYMENT_NAME", Value: spec.Name},
		{Name: "CLAWMANAGER_RUNTIME_IMAGE_REF", Value: spec.Image},
		{Name: "CLAWMANAGER_AGENT_PORT", Value: "19090"},
		{Name: "CLAWMANAGER_GATEWAY_PORT_START", Value: strconv.Itoa(gatewayPortStart)},
		{Name: "CLAWMANAGER_GATEWAY_PORT_END", Value: strconv.Itoa(gatewayPortEnd)},
		{Name: "CLAWMANAGER_AGENT_CONTROL_TOKEN", Value: spec.AgentControlToken},
		{Name: "CLAWMANAGER_AGENT_REPORT_TOKEN", Value: spec.AgentReportToken},
		{Name: "RUNTIME_WORKSPACE_ROOT", Value: workspaceRoot},
		{Name: "RUNTIME_AGENT_LISTEN_ADDR", Value: "0.0.0.0:19090"},
		{Name: "RUNTIME_AGENT_PUBLIC_PORT", Value: "19090"},
		{Name: "RUNTIME_AGENT_CONTROL_TOKEN", Value: spec.AgentControlToken},
		{Name: "RUNTIME_AGENT_REPORT_TOKEN", Value: spec.AgentReportToken},
		{Name: "RUNTIME_GATEWAY_PORT_START", Value: strconv.Itoa(gatewayPortStart)},
		{Name: "RUNTIME_GATEWAY_PORT_END", Value: strconv.Itoa(gatewayPortEnd)},
	}
	if strings.EqualFold(spec.RuntimeType, "hermes") {
		env = append(env, corev1.EnvVar{Name: "HERMES_TUI_DIR", Value: hermesTUIDir})
	}
	env = append(env,
		fieldRefEnv("POD_NAME", "metadata.name"),
		fieldRefEnv("POD_NAMESPACE", "metadata.namespace"),
		fieldRefEnv("POD_IP", "status.podIP"),
		fieldRefEnv("NODE_NAME", "spec.nodeName"),
	)
	if spec.TrustedProxyCIDRs != "" {
		env = append(env, corev1.EnvVar{Name: "CLAWMANAGER_TRUSTED_PROXY_CIDRS", Value: spec.TrustedProxyCIDRs})
	}
	return env
}

func runtimeBackendURL(spec RuntimeDeploymentSpec) string {
	if spec.BackendURL != "" {
		return spec.BackendURL
	}
	namespace := spec.Namespace
	if namespace == "" {
		namespace = "clawmanager-system"
	}
	return fmt.Sprintf("http://clawmanager-gateway.%s.svc.cluster.local:9001", namespace)
}

func fieldRefEnv(name, fieldPath string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: name,
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: fieldPath},
		},
	}
}

func runtimeWorkspaceMountPath(value string) string {
	if value == "" {
		return defaultWorkspaceMount
	}
	return value
}

func runtimeGatewayPortValue(value, defaultValue int) int {
	if value == 0 {
		return defaultValue
	}
	return value
}

func (s *runtimeDeploymentService) Ensure(ctx context.Context, spec RuntimeDeploymentSpec) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("k8s client not initialized")
	}

	desired := BuildRuntimeDeployment(spec)
	deployments := s.client.AppsV1().Deployments(spec.Namespace)
	existing, err := deployments.Get(ctx, spec.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		_, createErr := deployments.Create(ctx, desired, metav1.CreateOptions{})
		if createErr != nil {
			return fmt.Errorf("failed to create runtime deployment %s/%s: %w", spec.Namespace, spec.Name, createErr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get runtime deployment %s/%s: %w", spec.Namespace, spec.Name, err)
	}

	if !reflect.DeepEqual(existing.Spec.Selector, desired.Spec.Selector) {
		return fmt.Errorf("runtime deployment %s/%s selector mismatch; delete and recreate the deployment to change immutable selector", spec.Namespace, spec.Name)
	}

	updated := existing.DeepCopy()
	if updated.Labels == nil {
		updated.Labels = map[string]string{}
	}
	for key, value := range desired.Labels {
		updated.Labels[key] = value
	}
	updated.Spec.Replicas = desired.Spec.Replicas
	if updated.Spec.Template.Labels == nil {
		updated.Spec.Template.Labels = map[string]string{}
	}
	for key, value := range desired.Spec.Template.Labels {
		updated.Spec.Template.Labels[key] = value
	}
	updated.Spec.Template.Spec = desired.Spec.Template.Spec

	_, err = deployments.Update(ctx, updated, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update runtime deployment %s/%s: %w", spec.Namespace, spec.Name, err)
	}
	return nil
}

func (s *runtimeDeploymentService) Scale(ctx context.Context, namespace, name string, replicas int32) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("k8s client not initialized")
	}

	deployments := s.client.AppsV1().Deployments(namespace)
	patch, err := json.Marshal(map[string]interface{}{
		"spec": map[string]interface{}{
			"replicas": replicas,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to build runtime deployment scale patch %s/%s: %w", namespace, name, err)
	}
	if _, err := deployments.Patch(ctx, name, types.StrategicMergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("failed to scale runtime deployment %s/%s: %w", namespace, name, err)
	}
	return nil
}

func (s *runtimeDeploymentService) RolloutImage(ctx context.Context, namespace, name, image string, maxUnavailable, maxSurge int) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("k8s client not initialized")
	}
	image = strings.TrimSpace(image)
	if image == "" {
		return fmt.Errorf("runtime rollout image is required")
	}

	deployments := s.client.AppsV1().Deployments(namespace)
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		existing, err := deployments.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		updated := existing.DeepCopy()
		containerIndex := -1
		for index, container := range updated.Spec.Template.Spec.Containers {
			if container.Name == "runtime" {
				containerIndex = index
				break
			}
		}
		if containerIndex < 0 {
			return fmt.Errorf("runtime deployment %s/%s has no runtime container", namespace, name)
		}

		container := &updated.Spec.Template.Spec.Containers[containerIndex]
		container.Image = image
		upsertEnvVar(container, "CLAWMANAGER_RUNTIME_IMAGE_REF", image)

		updated.Spec.Strategy.Type = appsv1.RollingUpdateDeploymentStrategyType
		updated.Spec.Strategy.RollingUpdate = &appsv1.RollingUpdateDeployment{
			MaxUnavailable: intOrStringPtr(positiveRolloutInt(maxUnavailable)),
			MaxSurge:       intOrStringPtr(positiveRolloutInt(maxSurge)),
		}

		_, err = deployments.Update(ctx, updated, metav1.UpdateOptions{})
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to update runtime deployment %s/%s image: %w", namespace, name, err)
	}
	return nil
}

func (s *runtimeDeploymentService) ListPods(ctx context.Context, namespace, runtimeType string) ([]RuntimeDeploymentPod, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("k8s client not initialized")
	}
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return nil, fmt.Errorf("runtime namespace is required")
	}
	runtimeType = strings.ToLower(strings.TrimSpace(runtimeType))

	listOptions := metav1.ListOptions{}
	if runtimeType != "" {
		listOptions.LabelSelector = labels.Set{"clawmanager.io/runtime-type": runtimeType}.String()
	}
	deploymentList, err := s.client.AppsV1().Deployments(namespace).List(ctx, listOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to list runtime deployments in %s: %w", namespace, err)
	}

	var pods []RuntimeDeploymentPod
	for _, deployment := range deploymentList.Items {
		deploymentRuntimeType := strings.ToLower(strings.TrimSpace(deployment.Labels["clawmanager.io/runtime-type"]))
		if deploymentRuntimeType == "" {
			continue
		}
		if runtimeType != "" && deploymentRuntimeType != runtimeType {
			continue
		}
		if deployment.Spec.Selector == nil {
			return nil, fmt.Errorf("runtime deployment %s/%s has no selector", deployment.Namespace, deployment.Name)
		}
		selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
		if err != nil {
			return nil, fmt.Errorf("runtime deployment %s/%s has invalid selector: %w", deployment.Namespace, deployment.Name, err)
		}
		podList, err := s.client.CoreV1().Pods(deployment.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
		if err != nil {
			return nil, fmt.Errorf("failed to list runtime deployment pods %s/%s: %w", deployment.Namespace, deployment.Name, err)
		}
		deploymentImage := runtimeContainerImage(deployment.Spec.Template.Spec.Containers)
		for _, pod := range podList.Items {
			image := runtimeContainerImage(pod.Spec.Containers)
			if image == "" {
				image = deploymentImage
			}
			pods = append(pods, RuntimeDeploymentPod{
				RuntimeType:    deploymentRuntimeType,
				Namespace:      pod.Namespace,
				DeploymentName: deployment.Name,
				PodName:        pod.Name,
				PodIP:          stringPtrIfNotEmpty(pod.Status.PodIP),
				NodeName:       stringPtrIfNotEmpty(pod.Spec.NodeName),
				ImageRef:       image,
				State:          runtimeK8sPodState(pod),
			})
		}
	}
	return pods, nil
}

func runtimeContainerImage(containers []corev1.Container) string {
	for _, container := range containers {
		if container.Name == "runtime" {
			return strings.TrimSpace(container.Image)
		}
	}
	if len(containers) == 1 {
		return strings.TrimSpace(containers[0].Image)
	}
	return ""
}

func runtimeK8sPodState(pod corev1.Pod) string {
	if pod.DeletionTimestamp != nil {
		return "deleted"
	}
	switch pod.Status.Phase {
	case corev1.PodPending:
		return "pending"
	case corev1.PodRunning:
		if runtimeK8sPodReady(pod) {
			return "ready"
		}
		return "pending"
	case corev1.PodSucceeded, corev1.PodFailed:
		return "unhealthy"
	default:
		return "pending"
	}
}

func runtimeK8sPodReady(pod corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func stringPtrIfNotEmpty(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func upsertEnvVar(container *corev1.Container, name, value string) {
	for index := range container.Env {
		if container.Env[index].Name == name {
			container.Env[index].Value = value
			container.Env[index].ValueFrom = nil
			return
		}
	}
	container.Env = append(container.Env, corev1.EnvVar{Name: name, Value: value})
}

func positiveRolloutInt(value int) int {
	if value <= 0 {
		return 1
	}
	return value
}

func intOrStringPtr(value int) *intstr.IntOrString {
	result := intstr.FromInt(value)
	return &result
}
