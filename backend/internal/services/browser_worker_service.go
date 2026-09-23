package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	cmk8s "clawreef/internal/services/k8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	defaultBrowserWorkerImage = "clawmanager/browser-worker:0.1.0"
	defaultCDPProxyImage      = "clawmanager/cdp-proxy:0.1.0"
)

type BrowserWorkerUpdate struct {
	Enabled         bool   `json:"enabled"`
	ResourceProfile string `json:"resource_profile,omitempty"`
	DisplayWidth    int    `json:"display_width,omitempty"`
	DisplayHeight   int    `json:"display_height,omitempty"`
	RetainProfile   *bool  `json:"retain_profile,omitempty"`
}

type BrowserWorkerService struct {
	repo      repository.BrowserWorkerRepository
	instances repository.InstanceRepository
	client    *cmk8s.Client
}

func NewBrowserWorkerService(repo repository.BrowserWorkerRepository, instances repository.InstanceRepository, client *cmk8s.Client) *BrowserWorkerService {
	return &BrowserWorkerService{repo: repo, instances: instances, client: client}
}

func browserWorkerDefaults(instanceID int) *models.InstanceBrowserWorker {
	return &models.InstanceBrowserWorker{InstanceID: instanceID, Status: models.BrowserWorkerStatusDisabled, ResourceProfile: "standard", DisplayWidth: 1440, DisplayHeight: 900, RetainProfile: true, BrowserImage: envOr("CLAWMANAGER_BROWSER_WORKER_IMAGE", defaultBrowserWorkerImage), CDPProxyImage: envOr("CLAWMANAGER_CDP_PROXY_IMAGE", defaultCDPProxyImage), Generation: 1}
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func (s *BrowserWorkerService) Get(instanceID int) (*models.InstanceBrowserWorker, error) {
	config, err := s.repo.Get(instanceID)
	if err != nil {
		return nil, err
	}
	if config == nil {
		return browserWorkerDefaults(instanceID), nil
	}
	return config, nil
}

func (s *BrowserWorkerService) SetObserved(instanceID int, status string, lastError *string) error {
	config, err := s.Get(instanceID)
	if err != nil {
		return err
	}
	return s.repo.UpdateObserved(instanceID, config.Generation, status, lastError)
}

func (s *BrowserWorkerService) Update(ctx context.Context, instance *models.Instance, update BrowserWorkerUpdate) (*models.InstanceBrowserWorker, error) {
	if !browserWorkerSupportedInstance(instance) {
		return nil, fmt.Errorf("browser worker supports OpenClaw Lite instances only")
	}
	config, err := s.Get(instance.ID)
	if err != nil {
		return nil, err
	}
	config.Enabled = update.Enabled
	if update.ResourceProfile != "" {
		config.ResourceProfile = update.ResourceProfile
	}
	if update.DisplayWidth != 0 {
		config.DisplayWidth = update.DisplayWidth
	}
	if update.DisplayHeight != 0 {
		config.DisplayHeight = update.DisplayHeight
	}
	if update.RetainProfile != nil {
		config.RetainProfile = *update.RetainProfile
	}
	if config.ResourceProfile != "standard" {
		return nil, fmt.Errorf("unsupported browser worker resource profile")
	}
	if config.DisplayWidth < 1024 || config.DisplayWidth > 2560 || config.DisplayHeight < 720 || config.DisplayHeight > 1440 {
		return nil, fmt.Errorf("invalid browser worker display size")
	}
	if config.Enabled {
		config.Status = models.BrowserWorkerStatusProvisioning
	} else {
		config.Status = models.BrowserWorkerStatusDisabled
	}
	config.LastError = nil
	if err := s.repo.Upsert(config); err != nil {
		return nil, err
	}
	config, _ = s.repo.Get(instance.ID)
	if err := s.Reconcile(ctx, instance, config); err != nil {
		message := err.Error()
		_ = s.repo.UpdateObserved(instance.ID, config.Generation, models.BrowserWorkerStatusError, &message)
		return s.Get(instance.ID)
	}
	return s.Get(instance.ID)
}

func (s *BrowserWorkerService) Reconcile(ctx context.Context, instance *models.Instance, config *models.InstanceBrowserWorker) error {
	if s.client == nil || s.client.Clientset == nil {
		return fmt.Errorf("kubernetes client is unavailable")
	}
	if !config.Enabled {
		if config.Generation != config.ObservedGeneration {
			if err := s.patchOpenClawConfig(instance, false); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		if err := s.deleteResources(ctx, instance.ID); err != nil {
			return err
		}
		return s.repo.UpdateObserved(instance.ID, config.Generation, models.BrowserWorkerStatusDisabled, nil)
	}
	if config.Generation != config.ObservedGeneration {
		if err := s.patchOpenClawConfig(instance, true); err != nil {
			return err
		}
	}
	if err := s.ensureResources(ctx, instance, config); err != nil {
		return err
	}
	status := models.BrowserWorkerStatusProvisioning
	if s.resourcesReady(ctx, instance.ID) {
		status = models.BrowserWorkerStatusReady
	}
	return s.repo.UpdateObserved(instance.ID, config.Generation, status, nil)
}

func (s *BrowserWorkerService) resourcesReady(ctx context.Context, instanceID int) bool {
	deployment, err := s.client.Clientset.AppsV1().Deployments(s.namespace()).Get(ctx, s.name(instanceID), metav1.GetOptions{})
	return err == nil && deployment.Status.AvailableReplicas > 0
}

func (s *BrowserWorkerService) ReconcileAll(ctx context.Context) {
	rows, err := s.repo.ListDesired(500)
	if err != nil {
		return
	}
	for i := range rows {
		instance, getErr := s.instances.GetByID(rows[i].InstanceID)
		if getErr != nil || instance == nil {
			continue
		}
		if err := s.Reconcile(ctx, instance, &rows[i]); err != nil {
			message := err.Error()
			_ = s.repo.UpdateObserved(instance.ID, rows[i].Generation, models.BrowserWorkerStatusError, &message)
		}
	}
}

func (s *BrowserWorkerService) DeleteForInstance(ctx context.Context, instanceID int) error {
	if s.client == nil || s.client.Clientset == nil {
		return nil
	}
	return s.deleteResources(ctx, instanceID)
}

func browserWorkerSupportedInstance(instance *models.Instance) bool {
	return instance != nil && strings.EqualFold(instance.Type, RuntimeTypeOpenClaw) && strings.EqualFold(instance.InstanceMode, InstanceModeLite)
}

func (s *BrowserWorkerService) name(instanceID int) string {
	return "clawbrowser-" + strconv.Itoa(instanceID)
}
func (s *BrowserWorkerService) namespace() string {
	if value := strings.TrimSpace(os.Getenv("CLAWMANAGER_BROWSER_WORKER_NAMESPACE")); value != "" {
		return value
	}
	return s.client.GetSystemNamespace()
}

func (s *BrowserWorkerService) ensureResources(ctx context.Context, instance *models.Instance, config *models.InstanceBrowserWorker) error {
	name, namespace := s.name(instance.ID), s.namespace()
	labels := map[string]string{"app": "clawbrowser", "instance-id": strconv.Itoa(instance.ID), "user-id": strconv.Itoa(instance.UserID), "managed-by": "clawmanager-browser-worker"}
	workspaceSubPath := fmt.Sprintf("openclaw/user-%d/instance-%d/home/.openclaw/browser-worker", instance.UserID, instance.ID)
	replicas := int32(0)
	if strings.EqualFold(strings.TrimSpace(instance.Status), "running") {
		replicas = 1
	}
	deployment := desiredBrowserWorkerDeployment(name, namespace, labels, workspaceSubPath, s.client.WorkspacePVCClaimName, replicas, config)
	deployments := s.client.Clientset.AppsV1().Deployments(namespace)
	existing, err := deployments.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if _, err = deployments.Create(ctx, deployment, metav1.CreateOptions{}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		deployment.ResourceVersion = existing.ResourceVersion
		if _, err = deployments.Update(ctx, deployment, metav1.UpdateOptions{}); err != nil {
			return err
		}
	}
	service := desiredBrowserWorkerService(name, namespace, labels, instance.ID)
	services := s.client.Clientset.CoreV1().Services(namespace)
	existingService, err := services.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = services.Create(ctx, service, metav1.CreateOptions{})
	} else if err == nil {
		service.ResourceVersion = existingService.ResourceVersion
		service.Spec.ClusterIP = existingService.Spec.ClusterIP
		service.Spec.ClusterIPs = existingService.Spec.ClusterIPs
		_, err = services.Update(ctx, service, metav1.UpdateOptions{})
	}
	return err
}

func desiredBrowserWorkerDeployment(name, namespace string, labels map[string]string, workspaceSubPath, workspacePVC string, replicas int32, config *models.InstanceBrowserWorker) *appsv1.Deployment {
	profilePath := "/workspace/" + workspaceSubPath
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32ptr(replicas),
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": labels["app"], "instance-id": labels["instance-id"]}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{{
						Name:            "prepare-profile",
						Image:           config.BrowserImage,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Command:         []string{"/bin/sh", "-c"},
						Args:            []string{fmt.Sprintf("mkdir -p %q && chown -R 1000:1000 %q", profilePath, profilePath)},
						VolumeMounts:    []corev1.VolumeMount{{Name: "browser-profile", MountPath: "/workspace"}},
					}},
					Containers: []corev1.Container{
						{
							Name:            "browser",
							Image:           config.BrowserImage,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Env: []corev1.EnvVar{
								{Name: "TZ", Value: "Asia/Shanghai"},
								{Name: "CHROMIUM_REMOTE_DEBUGGING", Value: "1"},
								{Name: "CHROMIUM_REMOTE_DEBUGGING_PORT", Value: "9223"},
								{Name: "DISPLAY_WIDTH", Value: strconv.Itoa(config.DisplayWidth)},
								{Name: "DISPLAY_HEIGHT", Value: strconv.Itoa(config.DisplayHeight)},
								{Name: "KEEP_APP_RUNNING", Value: "1"},
							},
							Ports:          []corev1.ContainerPort{{Name: "web", ContainerPort: 5800}},
							ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/", Port: intstr.FromString("web")}}, InitialDelaySeconds: 5, PeriodSeconds: 5, FailureThreshold: 12},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("1Gi")},
								Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("3Gi")},
							},
							VolumeMounts: []corev1.VolumeMount{{Name: "browser-profile", MountPath: "/config", SubPath: workspaceSubPath}, {Name: "shm", MountPath: "/dev/shm"}},
						},
						{
							Name:            "cdp-proxy",
							Image:           config.CDPProxyImage,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Env:             []corev1.EnvVar{{Name: "CDP_UPSTREAM", Value: "http://127.0.0.1:9223"}},
							Ports:           []corev1.ContainerPort{{Name: "cdp", ContainerPort: 9222}},
							ReadinessProbe:  &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromString("cdp")}}, InitialDelaySeconds: 2, PeriodSeconds: 5},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("16Mi")},
								Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("64Mi")},
							},
						},
					},
					Volumes: []corev1.Volume{
						{Name: "browser-profile", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: workspacePVC}}},
						{Name: "shm", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory, SizeLimit: resourcePtr(resource.MustParse("1Gi"))}}},
					},
				},
			},
		},
	}
}

func desiredBrowserWorkerService(name, namespace string, labels map[string]string, instanceID int) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "clawbrowser", "instance-id": strconv.Itoa(instanceID)},
			Ports: []corev1.ServicePort{
				{Name: "web", Port: 5800, TargetPort: intstr.FromString("web")},
				{Name: "cdp", Port: 9222, TargetPort: intstr.FromString("cdp")},
			},
		},
	}
}

func (s *BrowserWorkerService) deleteResources(ctx context.Context, instanceID int) error {
	name, namespace := s.name(instanceID), s.namespace()
	if err := s.client.Clientset.AppsV1().Deployments(namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if err := s.client.Clientset.CoreV1().Services(namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (s *BrowserWorkerService) patchOpenClawConfig(instance *models.Instance, enabled bool) error {
	workspace := ""
	if instance.WorkspacePath != nil {
		workspace = strings.TrimSpace(*instance.WorkspacePath)
	}
	if workspace == "" {
		workspace = RuntimeWorkspacePathWithRoot(s.client.WorkspaceRoot, RuntimeTypeOpenClaw, instance.UserID, instance.ID)
	}
	path := filepath.Join(workspace, "home", ".openclaw", "openclaw.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read OpenClaw config: %w", err)
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("parse OpenClaw config: %w", err)
	}
	host := fmt.Sprintf("%s.%s.svc.cluster.local", s.name(instance.ID), s.namespace())
	statePath := path + ".browser-worker-state"
	if enabled {
		if _, err := os.Stat(statePath); os.IsNotExist(err) {
			state := map[string]any{"had_browser": false}
			if previous, ok := root["browser"]; ok {
				state["had_browser"], state["browser"] = true, previous
			}
			if stateRaw, marshalErr := json.Marshal(state); marshalErr == nil {
				if writeErr := os.WriteFile(statePath, stateRaw, 0600); writeErr != nil {
					return writeErr
				}
			}
		}
		root["browser"] = map[string]any{"enabled": true, "defaultProfile": "interactive", "ssrfPolicy": map[string]any{"allowedHostnames": []string{host}}, "profiles": map[string]any{"interactive": map[string]any{"cdpUrl": "http://" + host + ":9222", "driver": "openclaw", "attachOnly": true}}}
	} else {
		stateRaw, readErr := os.ReadFile(statePath)
		if readErr == nil {
			var state map[string]any
			if json.Unmarshal(stateRaw, &state) == nil {
				if had, _ := state["had_browser"].(bool); had {
					root["browser"] = state["browser"]
				} else {
					delete(root, "browser")
				}
			}
		}
	}
	encoded, _ := json.MarshalIndent(root, "", "  ")
	encoded = append(encoded, '\n')
	backup := path + ".bak-browser-worker"
	if _, err := os.Stat(backup); os.IsNotExist(err) {
		_ = os.WriteFile(backup, raw, 0600)
	}
	temp := path + ".tmp-browser-worker"
	if err := os.WriteFile(temp, encoded, 0600); err != nil {
		return err
	}
	if info, statErr := os.Stat(path); statErr == nil {
		_ = os.Chmod(temp, info.Mode())
	}
	runtimeID := RuntimeLinuxID(instance.ID)
	if err := setFileOwnership(temp, runtimeID, runtimeID); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("set OpenClaw config ownership to %d:%d: %w", runtimeID, runtimeID, err)
	}
	return os.Rename(temp, path)
}

func int32ptr(value int32) *int32                            { return &value }
func resourcePtr(value resource.Quantity) *resource.Quantity { return &value }
