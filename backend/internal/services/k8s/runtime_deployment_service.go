package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
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
	ControlUIOrigin       string
	HermesDesktopWeb      bool
}

type RuntimeDeploymentPod struct {
	RuntimeType       string
	Namespace         string
	DeploymentName    string
	PodName           string
	PodUID            string
	PodIP             *string
	NodeName          *string
	ImageRef          string
	ImageDigest       string
	State             string
	PoolRole          string
	PoolPurpose       string
	UpgradeID         string
	SourceDeployment  string
	SchedulingEnabled *bool
	DesiredReplicas   int32
}

type RuntimeDeploymentRef struct {
	Namespace string
	Name      string
}

type RuntimeDeploymentService interface {
	Ensure(ctx context.Context, spec RuntimeDeploymentSpec) error
	Scale(ctx context.Context, namespace, name string, replicas int32) error
	RolloutImage(ctx context.Context, namespace, name, image, upgradeID string, maxUnavailable, maxSurge int) error
	EnsureUpgradePool(ctx context.Context, namespace, sourceName, targetName, image, upgradeID string) error
	SetUpgradePoolActive(ctx context.Context, namespace, sourceName, targetName, upgradeID string, active bool) error
	DeleteUpgradePool(ctx context.Context, namespace, sourceName, targetName, upgradeID string) error
	ListPods(ctx context.Context, namespace, runtimeType string) ([]RuntimeDeploymentPod, error)
	ListDeploymentPods(ctx context.Context, refs []RuntimeDeploymentRef) ([]RuntimeDeploymentPod, error)
}

// RuntimeUpgradeLabDeploymentService is deliberately separate from the
// production rollout interface.  Its implementation refuses to mutate a
// Deployment unless the immutable lab ownership labels match the requested
// run, so a test cleanup cannot target a serving Runtime pool.
type RuntimeUpgradeLabDeploymentService interface {
	EnsureUpgradeLabPool(ctx context.Context, namespace, templateName, name, image, runID string, replicas int32) error
	DeleteUpgradeLabPool(ctx context.Context, namespace, name, runID string) error
}

const (
	upgradeLabPurposeLabel = "clawmanager.io/purpose"
	upgradeLabPurposeValue = "openclaw-upgrade-lab"
	upgradeLabRunLabel     = "clawmanager.io/upgrade-lab-run"
	runtimePoolRoleLabel   = "clawmanager.io/pool-role"
	runtimeUpgradeIDLabel  = "clawmanager.io/upgrade-id"
	runtimeSourceLabel     = "clawmanager.io/source-deployment"
	runtimeSchedulingLabel = "clawmanager.io/scheduling-enabled"
)

func (s *runtimeDeploymentService) EnsureUpgradeLabPool(ctx context.Context, namespace, templateName, name, image, runID string, replicas int32) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("k8s client not initialized")
	}
	if strings.TrimSpace(templateName) == "" || strings.TrimSpace(name) == "" || templateName == name {
		return fmt.Errorf("distinct template and lab deployment names are required")
	}
	if runtimeImageDigest(image) == "" || strings.TrimSpace(runID) == "" || replicas <= 0 {
		return fmt.Errorf("lab pool requires an immutable image, run id and positive replicas")
	}
	deployments := s.client.AppsV1().Deployments(namespace)
	if existing, err := deployments.Get(ctx, name, metav1.GetOptions{}); err == nil {
		if existing.Labels[upgradeLabPurposeLabel] != upgradeLabPurposeValue || existing.Labels[upgradeLabRunLabel] != runID {
			return fmt.Errorf("deployment %s/%s is not owned by upgrade lab run %s", namespace, name, runID)
		}
		if runtimeContainerImage(existing.Spec.Template.Spec.Containers) != strings.TrimSpace(image) {
			return fmt.Errorf("existing upgrade lab pool %s/%s has a different image", namespace, name)
		}
		return nil
	} else if !errors.IsNotFound(err) {
		return err
	}
	template, err := deployments.Get(ctx, templateName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get upgrade lab template deployment %s/%s: %w", namespace, templateName, err)
	}
	target := template.DeepCopy()
	target.TypeMeta = metav1.TypeMeta{}
	target.ObjectMeta = metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: copyStringMap(template.Labels)}
	if target.Labels == nil {
		target.Labels = map[string]string{}
	}
	target.Labels["app"] = name
	target.Labels["clawmanager.io/pool-role"] = "upgrade-lab"
	target.Labels[upgradeLabPurposeLabel] = upgradeLabPurposeValue
	target.Labels[upgradeLabRunLabel] = runID
	selector := map[string]string{
		"app": name, "clawmanager.io/runtime-type": template.Labels["clawmanager.io/runtime-type"],
		upgradeLabPurposeLabel: upgradeLabPurposeValue, upgradeLabRunLabel: runID,
	}
	target.Spec.Selector = &metav1.LabelSelector{MatchLabels: copyStringMap(selector)}
	target.Spec.Replicas = &replicas
	target.Spec.Template.Labels = copyStringMap(selector)
	target.Spec.Template.Labels["clawmanager.io/pool-role"] = "upgrade-lab"
	target.Spec.Strategy = appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
	target.Spec.Template.Spec.TopologySpreadConstraints = []corev1.TopologySpreadConstraint{{MaxSkew: 1, TopologyKey: "kubernetes.io/hostname", WhenUnsatisfiable: corev1.ScheduleAnyway, LabelSelector: &metav1.LabelSelector{MatchLabels: copyStringMap(selector)}}}
	containerIndex := -1
	for index := range target.Spec.Template.Spec.Containers {
		if target.Spec.Template.Spec.Containers[index].Name == "runtime" {
			containerIndex = index
			break
		}
	}
	if containerIndex < 0 {
		return fmt.Errorf("upgrade lab template %s/%s has no runtime container", namespace, templateName)
	}
	container := &target.Spec.Template.Spec.Containers[containerIndex]
	container.Image = strings.TrimSpace(image)
	upsertEnvVar(container, "CLAWMANAGER_RUNTIME_DEPLOYMENT_NAME", name)
	upsertEnvVar(container, "CLAWMANAGER_DEPLOYMENT_NAME", name)
	upsertEnvVar(container, "CLAWMANAGER_RUNTIME_IMAGE_REF", strings.TrimSpace(image))
	upsertEnvVar(container, "CLAWMANAGER_RUNTIME_IMAGE_DIGEST", runtimeImageDigest(image))
	upsertEnvVar(container, "CLAWMANAGER_RUNTIME_UPGRADE_ID", runID)
	container.ReadinessProbe = nil
	if _, err := deployments.Create(ctx, target, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create upgrade lab pool %s/%s: %w", namespace, name, err)
	}
	return nil
}

func (s *runtimeDeploymentService) DeleteUpgradeLabPool(ctx context.Context, namespace, name, runID string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("k8s client not initialized")
	}
	deployments := s.client.AppsV1().Deployments(namespace)
	existing, err := deployments.Get(ctx, name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return s.waitForUpgradeLabPoolGone(ctx, namespace, name)
	}
	if err != nil {
		return err
	}
	if existing.Labels[upgradeLabPurposeLabel] != upgradeLabPurposeValue || existing.Labels[upgradeLabRunLabel] != strings.TrimSpace(runID) {
		return fmt.Errorf("refusing to delete non-lab deployment %s/%s", namespace, name)
	}
	foreground := metav1.DeletePropagationForeground
	if err := deployments.Delete(ctx, name, metav1.DeleteOptions{PropagationPolicy: &foreground}); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete upgrade lab pool %s/%s: %w", namespace, name, err)
	}
	// A successful Deployment DELETE is asynchronous. Waiting for foreground
	// deletion prevents the caller from removing NFS workspaces while an
	// orphaned process in the terminating Runtime Pod still owns file handles.
	return s.waitForUpgradeLabPoolGone(ctx, namespace, name)
}

func (s *runtimeDeploymentService) waitForUpgradeLabPoolGone(ctx context.Context, namespace, name string) error {
	deployments := s.client.AppsV1().Deployments(namespace)
	if err := wait.PollUntilContextTimeout(ctx, 200*time.Millisecond, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		_, getErr := deployments.Get(ctx, name, metav1.GetOptions{})
		if getErr != nil && !errors.IsNotFound(getErr) {
			return false, getErr
		}
		pods, listErr := s.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labels.Set{"app": name}.AsSelector().String(),
		})
		if listErr != nil {
			return false, listErr
		}
		return errors.IsNotFound(getErr) && len(pods.Items) == 0, nil
	}); err != nil {
		return fmt.Errorf("wait for upgrade lab pool %s/%s deletion: %w", namespace, name, err)
	}
	return nil
}

func (s *runtimeDeploymentService) EnsureUpgradePool(ctx context.Context, namespace, sourceName, targetName, image, upgradeID string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("k8s client not initialized")
	}
	if strings.TrimSpace(sourceName) == "" || strings.TrimSpace(targetName) == "" || sourceName == targetName {
		return fmt.Errorf("distinct source and target runtime deployment names are required")
	}
	if runtimeImageDigest(image) == "" || strings.TrimSpace(upgradeID) == "" {
		return fmt.Errorf("upgrade pool requires an immutable image and upgrade id")
	}
	deployments := s.client.AppsV1().Deployments(namespace)
	if existing, err := deployments.Get(ctx, targetName, metav1.GetOptions{}); err == nil {
		if runtimeContainerImage(existing.Spec.Template.Spec.Containers) != strings.TrimSpace(image) || existing.Labels["clawmanager.io/upgrade-id"] != upgradeID || existing.Labels["clawmanager.io/source-deployment"] != sourceName {
			return fmt.Errorf("existing upgrade pool %s/%s does not match rollout", namespace, targetName)
		}
		return nil
	} else if !errors.IsNotFound(err) {
		return err
	}
	source, err := deployments.Get(ctx, sourceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get source runtime deployment %s/%s: %w", namespace, sourceName, err)
	}
	target := source.DeepCopy()
	target.TypeMeta = metav1.TypeMeta{}
	target.ObjectMeta = metav1.ObjectMeta{Name: targetName, Namespace: namespace, Labels: map[string]string{}}
	for key, value := range source.Labels {
		target.Labels[key] = value
	}
	target.Labels["app"] = targetName
	target.Labels["clawmanager.io/pool-role"] = "upgrade-target"
	target.Labels["clawmanager.io/upgrade-id"] = upgradeID
	target.Labels["clawmanager.io/source-deployment"] = sourceName
	target.Labels[runtimeSchedulingLabel] = "false"
	selector := map[string]string{"app": targetName, "clawmanager.io/runtime-type": source.Labels["clawmanager.io/runtime-type"], "clawmanager.io/upgrade-id": upgradeID}
	target.Spec.Selector = &metav1.LabelSelector{MatchLabels: copyStringMap(selector)}
	if target.Spec.Template.Labels == nil {
		target.Spec.Template.Labels = map[string]string{}
	}
	for key := range target.Spec.Template.Labels {
		delete(target.Spec.Template.Labels, key)
	}
	for key, value := range selector {
		target.Spec.Template.Labels[key] = value
	}
	target.Spec.Template.Labels["clawmanager.io/pool-role"] = "upgrade-target"
	target.Spec.Template.Labels[runtimeSchedulingLabel] = "false"
	target.Spec.Strategy = appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
	target.Spec.Template.Spec.TopologySpreadConstraints = []corev1.TopologySpreadConstraint{{MaxSkew: 1, TopologyKey: "kubernetes.io/hostname", WhenUnsatisfiable: corev1.ScheduleAnyway, LabelSelector: &metav1.LabelSelector{MatchLabels: copyStringMap(selector)}}}
	containerIndex := -1
	for index := range target.Spec.Template.Spec.Containers {
		if target.Spec.Template.Spec.Containers[index].Name == "runtime" {
			containerIndex = index
			break
		}
	}
	if containerIndex < 0 {
		return fmt.Errorf("source runtime deployment %s/%s has no runtime container", namespace, sourceName)
	}
	container := &target.Spec.Template.Spec.Containers[containerIndex]
	container.Image = strings.TrimSpace(image)
	upsertEnvVar(container, "CLAWMANAGER_RUNTIME_DEPLOYMENT_NAME", targetName)
	upsertEnvVar(container, "CLAWMANAGER_DEPLOYMENT_NAME", targetName)
	upsertEnvVar(container, "CLAWMANAGER_RUNTIME_IMAGE_REF", strings.TrimSpace(image))
	upsertEnvVar(container, "CLAWMANAGER_RUNTIME_IMAGE_DIGEST", runtimeImageDigest(image))
	upsertEnvVar(container, "CLAWMANAGER_RUNTIME_UPGRADE_ID", upgradeID)
	container.ReadinessProbe = &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstr.FromString("agent"), Scheme: corev1.URISchemeHTTP}}, InitialDelaySeconds: 1, PeriodSeconds: 2, TimeoutSeconds: 1, FailureThreshold: 3, SuccessThreshold: 1}
	if _, err := deployments.Create(ctx, target, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create upgrade runtime pool %s/%s: %w", namespace, targetName, err)
	}
	return nil
}

func (s *runtimeDeploymentService) SetUpgradePoolActive(ctx context.Context, namespace, sourceName, targetName, upgradeID string, active bool) error {
	return s.setUpgradePoolActive(ctx, namespace, sourceName, targetName, upgradeID, active, false)
}

// A scoped upgrade must keep the old pool serving its unselected instances.
// Scheduling affinity in the controller prevents crossing the data-format boundary.
func (s *runtimeDeploymentService) SetUpgradePoolActivePreservingSource(ctx context.Context, namespace, sourceName, targetName, upgradeID string) error {
	return s.setUpgradePoolActive(ctx, namespace, sourceName, targetName, upgradeID, true, true)
}

func (s *runtimeDeploymentService) setUpgradePoolActive(ctx context.Context, namespace, sourceName, targetName, upgradeID string, active, preserveSource bool) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("k8s client not initialized")
	}
	namespace = strings.TrimSpace(namespace)
	sourceName = strings.TrimSpace(sourceName)
	targetName = strings.TrimSpace(targetName)
	upgradeID = strings.TrimSpace(upgradeID)
	if namespace == "" || sourceName == "" || targetName == "" || sourceName == targetName || upgradeID == "" {
		return fmt.Errorf("valid source, target and upgrade id are required")
	}
	deployments := s.client.AppsV1().Deployments(namespace)
	setScheduling := func(name, value string, validateTarget bool) error {
		return retry.RetryOnConflict(retry.DefaultRetry, func() error {
			deployment, err := deployments.Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if validateTarget && (deployment.Labels[runtimePoolRoleLabel] != "upgrade-target" || deployment.Labels[runtimeUpgradeIDLabel] != upgradeID || deployment.Labels[runtimeSourceLabel] != sourceName) {
				return fmt.Errorf("deployment %s/%s is not upgrade target %s for source %s", namespace, name, upgradeID, sourceName)
			}
			if deployment.Labels == nil {
				deployment.Labels = map[string]string{}
			}
			deployment.Labels[runtimeSchedulingLabel] = value
			_, err = deployments.Update(ctx, deployment, metav1.UpdateOptions{})
			return err
		})
	}
	if active {
		if err := setScheduling(targetName, "true", true); err != nil {
			return fmt.Errorf("enable upgrade target scheduling: %w", err)
		}
		if preserveSource {
			return nil
		}
		if err := setScheduling(sourceName, "false", false); err != nil {
			_ = setScheduling(targetName, "false", true)
			return fmt.Errorf("disable upgrade source scheduling: %w", err)
		}
		return nil
	}
	if err := setScheduling(sourceName, "true", false); err != nil {
		return fmt.Errorf("restore upgrade source scheduling: %w", err)
	}
	if err := setScheduling(targetName, "false", true); err != nil {
		return fmt.Errorf("disable failed upgrade target scheduling: %w", err)
	}
	return nil
}

func (s *runtimeDeploymentService) DeleteUpgradePool(ctx context.Context, namespace, sourceName, targetName, upgradeID string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("k8s client not initialized")
	}
	namespace = strings.TrimSpace(namespace)
	sourceName = strings.TrimSpace(sourceName)
	targetName = strings.TrimSpace(targetName)
	upgradeID = strings.TrimSpace(upgradeID)
	deployments := s.client.AppsV1().Deployments(namespace)
	target, err := deployments.Get(ctx, targetName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if target.Labels[runtimePoolRoleLabel] != "upgrade-target" || target.Labels[runtimeUpgradeIDLabel] != upgradeID || target.Labels[runtimeSourceLabel] != sourceName {
		return fmt.Errorf("refusing to delete deployment %s/%s without matching upgrade ownership", namespace, targetName)
	}
	if target.Spec.Replicas != nil && *target.Spec.Replicas != 0 {
		return fmt.Errorf("refusing to delete upgrade target %s/%s before it is scaled to zero", namespace, targetName)
	}
	policy := metav1.DeletePropagationBackground
	if err := deployments.Delete(ctx, targetName, metav1.DeleteOptions{PropagationPolicy: &policy}); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete failed upgrade pool %s/%s: %w", namespace, targetName, err)
	}
	return nil
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
		{Name: "CLAWMANAGER_DEPLOYMENT_NAME", Value: spec.Name},
		{Name: "CLAWMANAGER_RUNTIME_IMAGE_REF", Value: spec.Image},
		{Name: "CLAWMANAGER_RUNTIME_IMAGE_DIGEST", Value: runtimeImageDigest(spec.Image)},
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
	if spec.RuntimeType == "hermes" && spec.HermesDesktopWeb {
		container := &desired.Spec.Template.Spec.Containers[0]
		upsertEnvVar(container, "CLAWMANAGER_CONTROL_UI_ORIGIN", spec.ControlUIOrigin)
		env, err := s.hermesWebEnvironment(ctx, spec.Namespace, *container)
		if err != nil {
			return err
		}
		for key, value := range env {
			upsertEnvVar(container, key, value)
		}
	}
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

func (s *runtimeDeploymentService) RolloutImage(ctx context.Context, namespace, name, image, upgradeID string, maxUnavailable, maxSurge int) error {
	return s.rolloutImage(ctx, namespace, name, image, upgradeID, maxUnavailable, maxSurge, false)
}

func (s *runtimeDeploymentService) rolloutImage(ctx context.Context, namespace, name, image, upgradeID string, maxUnavailable, maxSurge int, hermesWeb bool) error {
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
		if hermesWeb {
			if existing.Labels["clawmanager.io/runtime-type"] != "hermes" {
				return fmt.Errorf("Hermes Web rollout cannot target another runtime")
			}
			env, err := s.hermesWebEnvironment(ctx, namespace, *container)
			if err != nil {
				return err
			}
			for key, value := range env {
				upsertEnvVar(container, key, value)
			}
		} else if existing.Labels["clawmanager.io/runtime-type"] == "hermes" {
			// Only remove the Web mode we injected, not administrator overrides.
			for _, e := range container.Env {
				if e.Name == "CLAWMANAGER_HERMES_PROXY_SOURCE" {
					removeEnvVar(container, "CLAWMANAGER_HERMES_DESKTOP_WEB_ENABLED")
					break
				}
			}
		}
		container.Image = image
		upsertEnvVar(container, "CLAWMANAGER_RUNTIME_IMAGE_REF", image)
		upsertEnvVar(container, "CLAWMANAGER_RUNTIME_IMAGE_DIGEST", runtimeImageDigest(image))
		upgradeID = strings.TrimSpace(upgradeID)
		if upgradeID != "" {
			upsertEnvVar(container, "CLAWMANAGER_RUNTIME_UPGRADE_ID", upgradeID)
			container.ReadinessProbe = &corev1.Probe{
				ProbeHandler:        corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstr.FromString("agent"), Scheme: corev1.URISchemeHTTP}},
				InitialDelaySeconds: 1,
				PeriodSeconds:       2,
				TimeoutSeconds:      1,
				FailureThreshold:    3,
				SuccessThreshold:    1,
			}
			maxUnavailable = 0
			if updated.Spec.Replicas != nil && *updated.Spec.Replicas > 0 {
				maxSurge = int(*updated.Spec.Replicas)
			}
		} else {
			removeEnvVar(container, "CLAWMANAGER_RUNTIME_UPGRADE_ID")
			if container.ReadinessProbe != nil && container.ReadinessProbe.HTTPGet != nil && container.ReadinessProbe.HTTPGet.Path == "/readyz" {
				container.ReadinessProbe = nil
			}
		}

		updated.Spec.Strategy.Type = appsv1.RollingUpdateDeploymentStrategyType
		updated.Spec.Strategy.RollingUpdate = &appsv1.RollingUpdateDeployment{
			MaxUnavailable: intOrStringPtr(nonNegativeRolloutInt(maxUnavailable)),
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
		// A retained zero-replica pool has no serving pod and must not add a
		// Kubernetes API dependency to unrelated scheduling or rollout work.
		if deployment.Spec.Replicas != nil && *deployment.Spec.Replicas == 0 && deployment.Status.Replicas == 0 {
			continue
		}
		deploymentPods, err := s.listDeploymentPods(ctx, &deployment)
		if err != nil {
			return nil, err
		}
		pods = append(pods, deploymentPods...)
	}
	return pods, nil
}

// ListDeploymentPods returns only the explicitly named pools. Data-safe
// OpenClaw reconciliation uses this path so a stale or unhealthy unrelated
// Runtime Deployment cannot fail or redirect the current rollout.
func (s *runtimeDeploymentService) ListDeploymentPods(ctx context.Context, refs []RuntimeDeploymentRef) ([]RuntimeDeploymentPod, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("k8s client not initialized")
	}
	seen := make(map[string]struct{}, len(refs))
	var pods []RuntimeDeploymentPod
	for _, ref := range refs {
		namespace := strings.TrimSpace(ref.Namespace)
		name := strings.TrimSpace(ref.Name)
		if namespace == "" || name == "" {
			return nil, fmt.Errorf("runtime deployment namespace and name are required")
		}
		key := namespace + "/" + name
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deployment, err := s.client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to get runtime deployment %s: %w", key, err)
		}
		if deployment.Spec.Replicas != nil && *deployment.Spec.Replicas == 0 && deployment.Status.Replicas == 0 {
			continue
		}
		deploymentPods, err := s.listDeploymentPods(ctx, deployment)
		if err != nil {
			return nil, err
		}
		pods = append(pods, deploymentPods...)
	}
	return pods, nil
}

func (s *runtimeDeploymentService) listDeploymentPods(ctx context.Context, deployment *appsv1.Deployment) ([]RuntimeDeploymentPod, error) {
	if deployment == nil || deployment.Spec.Selector == nil {
		if deployment == nil {
			return nil, fmt.Errorf("runtime deployment is required")
		}
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
	deploymentRuntimeType := strings.ToLower(strings.TrimSpace(deployment.Labels["clawmanager.io/runtime-type"]))
	deploymentImage := runtimeContainerImage(deployment.Spec.Template.Spec.Containers)
	var schedulingEnabled *bool
	if raw, ok := deployment.Labels[runtimeSchedulingLabel]; ok {
		if parsed, parseErr := strconv.ParseBool(strings.TrimSpace(raw)); parseErr == nil {
			schedulingEnabled = &parsed
		}
	}
	result := make([]RuntimeDeploymentPod, 0, len(podList.Items))
	desiredReplicas := int32(0)
	if deployment.Spec.Replicas != nil {
		desiredReplicas = *deployment.Spec.Replicas
	}
	for _, pod := range podList.Items {
		if pod.DeletionTimestamp != nil || pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
			continue
		}
		image := runtimeContainerImage(pod.Spec.Containers)
		if image == "" {
			image = deploymentImage
		}
		result = append(result, RuntimeDeploymentPod{
			RuntimeType:       deploymentRuntimeType,
			Namespace:         pod.Namespace,
			DeploymentName:    deployment.Name,
			PodName:           pod.Name,
			PodUID:            string(pod.UID),
			PodIP:             stringPtrIfNotEmpty(pod.Status.PodIP),
			NodeName:          stringPtrIfNotEmpty(pod.Spec.NodeName),
			ImageRef:          image,
			ImageDigest:       runtimeContainerImageID(pod.Status.ContainerStatuses),
			State:             runtimeK8sPodState(pod),
			PoolRole:          strings.TrimSpace(deployment.Labels[runtimePoolRoleLabel]),
			PoolPurpose:       strings.TrimSpace(deployment.Labels[upgradeLabPurposeLabel]),
			UpgradeID:         strings.TrimSpace(deployment.Labels[runtimeUpgradeIDLabel]),
			SourceDeployment:  strings.TrimSpace(deployment.Labels[runtimeSourceLabel]),
			SchedulingEnabled: schedulingEnabled,
			DesiredReplicas:   desiredReplicas,
		})
	}
	return result, nil
}

func runtimeContainerImageID(statuses []corev1.ContainerStatus) string {
	for _, status := range statuses {
		if status.Name == "runtime" {
			return normalizeRuntimeImageID(status.ImageID)
		}
	}
	if len(statuses) == 1 {
		return normalizeRuntimeImageID(statuses[0].ImageID)
	}
	return ""
}

func normalizeRuntimeImageID(value string) string {
	value = strings.TrimSpace(value)
	if index := strings.LastIndex(value, "sha256:"); index >= 0 {
		return value[index:]
	}
	return ""
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

func removeEnvVar(container *corev1.Container, name string) {
	if container == nil {
		return
	}
	filtered := container.Env[:0]
	for _, env := range container.Env {
		if env.Name != name {
			filtered = append(filtered, env)
		}
	}
	container.Env = filtered
}

func runtimeImageDigest(image string) string {
	marker := "@sha256:"
	index := strings.LastIndex(strings.TrimSpace(image), marker)
	if index < 0 {
		return ""
	}
	digest := strings.TrimSpace(image)[index+1:]
	if len(digest) != len("sha256:")+64 {
		return ""
	}
	for _, char := range strings.TrimPrefix(digest, "sha256:") {
		if !strings.ContainsRune("0123456789abcdefABCDEF", char) {
			return ""
		}
	}
	return strings.ToLower(digest)
}

func positiveRolloutInt(value int) int {
	if value <= 0 {
		return 1
	}
	return value
}

func nonNegativeRolloutInt(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func intOrStringPtr(value int) *intstr.IntOrString {
	result := intstr.FromInt(value)
	return &result
}
