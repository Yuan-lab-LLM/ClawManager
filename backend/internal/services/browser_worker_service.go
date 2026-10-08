package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	cmk8s "clawreef/internal/services/k8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// No unpublished registry names: the operator must supply built images.
	defaultBrowserWorkerImage = ""
	defaultCDPProxyImage      = ""
	browserWorkerManagedBy    = "clawmanager-browser-worker"
	browserWorkerAuthKey      = "access-token"
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
	mu        sync.Mutex
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

// ValidateCreate runs before creating the instance record, so invalid opt-ins
// cannot leave an orphan instance. Runtime readiness is asynchronous.
func (s *BrowserWorkerService) ValidateCreate(instance *models.Instance, update BrowserWorkerUpdate) error {
	if !browserWorkerSupportedInstance(instance) {
		return fmt.Errorf("browser worker supports OpenClaw Lite instances only")
	}
	if s.client == nil || s.client.Clientset == nil {
		return fmt.Errorf("kubernetes client is unavailable")
	}
	config := browserWorkerDefaults(instance.ID)
	if config.BrowserImage == "" || config.CDPProxyImage == "" {
		return fmt.Errorf("configure CLAWMANAGER_BROWSER_WORKER_IMAGE and CLAWMANAGER_CDP_PROXY_IMAGE before enabling Browser Worker")
	}
	if s.client.WorkspacePVCClaimName == "" {
		return fmt.Errorf("browser worker requires a shared workspace PVC")
	}
	if update.ResourceProfile != "" && update.ResourceProfile != "standard" {
		return fmt.Errorf("unsupported browser worker resource profile")
	}
	if (update.DisplayWidth != 0 && (update.DisplayWidth < 1024 || update.DisplayWidth > 2560)) ||
		(update.DisplayHeight != 0 && (update.DisplayHeight < 720 || update.DisplayHeight > 1440)) {
		return fmt.Errorf("invalid browser worker display size")
	}
	return nil
}

func (s *BrowserWorkerService) ValidateCreateRequest(req CreateInstanceRequest, update BrowserWorkerUpdate) error {
	return s.ValidateCreate(&models.Instance{Type: req.Type, InstanceMode: resolveCreateInstanceMode(req)}, update)
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
	config, err = s.repo.Get(instance.ID)
	if err != nil || config == nil {
		return nil, fmt.Errorf("reload saved browser worker config: %v", err)
	}
	if err := s.Reconcile(ctx, instance, config); err != nil {
		message := err.Error()
		_ = s.repo.UpdateStatus(instance.ID, config.Generation, models.BrowserWorkerStatusError, &message)
		return s.Get(instance.ID)
	}
	return s.Get(instance.ID)
}

func (s *BrowserWorkerService) Reconcile(ctx context.Context, instance *models.Instance, config *models.InstanceBrowserWorker) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if instance == nil || config == nil {
		return fmt.Errorf("instance and browser worker config are required")
	}
	if s.client == nil || s.client.Clientset == nil {
		return fmt.Errorf("kubernetes client is unavailable")
	}
	// A queued reconciliation may have read its row before a delete or update.
	// Re-read under the component lock instead of resurrecting stale resources.
	if s.instances != nil {
		current, err := s.instances.GetByID(instance.ID)
		if err != nil {
			return err
		}
		if current == nil {
			return s.deleteResources(ctx, instance.ID)
		}
		instance = current
	}
	currentConfig, err := s.repo.Get(instance.ID)
	if err != nil {
		return err
	}
	if currentConfig == nil {
		return s.deleteResources(ctx, instance.ID)
	}
	config = currentConfig
	if instance.Status == "deleting" {
		return s.deleteResources(ctx, instance.ID)
	}
	if !config.Enabled {
		if config.Generation != config.ObservedGeneration {
			if err := s.patchOpenClawConfig(instance, false, ""); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if err := s.deleteResources(ctx, instance.ID); err != nil {
			return err
		}
		return s.repo.UpdateObserved(instance.ID, config.Generation, models.BrowserWorkerStatusDisabled, nil)
	}
	if config.BrowserImage == "" || config.CDPProxyImage == "" {
		return fmt.Errorf("browser worker images are not configured")
	}
	token, err := s.ensureAuthSecret(ctx, instance)
	if err != nil {
		return err
	}
	if err := s.ensureResources(ctx, instance, config); err != nil {
		return err
	}
	// The runtime materializes openclaw.json after the create endpoint returns.
	// Keep the generation pending until that file has actually been patched.
	if !strings.EqualFold(instance.Status, "running") {
		return s.repo.UpdateStatus(instance.ID, config.Generation, models.BrowserWorkerStatusProvisioning, nil)
	}
	if config.Generation != config.ObservedGeneration {
		if err := s.patchOpenClawConfig(instance, true, token); err != nil {
			return err
		}
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
	if s.client == nil || s.client.Clientset == nil {
		return
	}
	_ = s.cleanupOrphans(ctx)
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
			_ = s.repo.UpdateStatus(instance.ID, rows[i].Generation, models.BrowserWorkerStatusError, &message)
		}
	}
}

func (s *BrowserWorkerService) DeleteForInstance(ctx context.Context, instanceID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	config, err := s.repo.Get(instanceID)
	if err != nil {
		return err
	}
	if config == nil {
		return nil
	} // Normal instance deletion has no component/API dependency.
	if s.client == nil || s.client.Clientset == nil {
		if !config.Enabled {
			return nil
		}
		return fmt.Errorf("kubernetes client is unavailable")
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

// Started only on the elected control-plane leader and cancelled on failover.
func (s *BrowserWorkerService) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	reconcile := func() {
		round, cancel := context.WithTimeout(ctx, interval)
		defer cancel()
		s.ReconcileAll(round)
	}
	reconcile()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcile()
		}
	}
}

// Namespace is shared by reconciliation, health checks and both reverse proxies.
func (s *BrowserWorkerService) Namespace() string { return s.namespace() }

func (s *BrowserWorkerService) AuthToken(ctx context.Context, instanceID int) (string, error) {
	if s.client == nil || s.client.Clientset == nil {
		return "", fmt.Errorf("kubernetes client is unavailable")
	}
	secret, err := s.client.Clientset.CoreV1().Secrets(s.namespace()).Get(ctx, s.name(instanceID), metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("browser worker credentials unavailable: %w", err)
	}
	token := string(secret.Data[browserWorkerAuthKey])
	if len(token) < 32 {
		return "", fmt.Errorf("browser worker credentials are invalid")
	}
	return token, nil
}

func (s *BrowserWorkerService) ensureAuthSecret(ctx context.Context, instance *models.Instance) (string, error) {
	secrets := s.client.Clientset.CoreV1().Secrets(s.namespace())
	if _, err := secrets.Get(ctx, s.name(instance.ID), metav1.GetOptions{}); err == nil {
		return s.AuthToken(ctx, instance.ID)
	} else if !apierrors.IsNotFound(err) {
		return "", err
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	token := hex.EncodeToString(random)
	_, err := secrets.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: s.name(instance.ID), Labels: browserWorkerLabels(instance)},
		Data:       map[string][]byte{browserWorkerAuthKey: []byte(token)},
	}, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return s.AuthToken(ctx, instance.ID)
	}
	return token, err
}

func browserWorkerLabels(instance *models.Instance) map[string]string {
	return map[string]string{"app": "clawbrowser", "instance-id": strconv.Itoa(instance.ID), "user-id": strconv.Itoa(instance.UserID), "managed-by": browserWorkerManagedBy}
}

func (s *BrowserWorkerService) ensureResources(ctx context.Context, instance *models.Instance, config *models.InstanceBrowserWorker) error {
	name, namespace := s.name(instance.ID), s.namespace()
	labels := browserWorkerLabels(instance)
	workspaceSubPath := fmt.Sprintf("openclaw/user-%d/instance-%d/home/.openclaw/browser-worker", instance.UserID, instance.ID)
	replicas := int32(0)
	if strings.EqualFold(strings.TrimSpace(instance.Status), "running") {
		replicas = 1
	}
	// Apply ingress restrictions before starting any worker containers.
	policy := desiredBrowserWorkerNetworkPolicy(name, namespace,
		envOr("POD_NAMESPACE", s.client.GetSystemNamespace()),
		envOr("RUNTIME_NAMESPACE", s.client.GetSystemNamespace()), labels)
	policies := s.client.Clientset.NetworkingV1().NetworkPolicies(namespace)
	existingPolicy, policyErr := policies.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(policyErr) {
		_, policyErr = policies.Create(ctx, policy, metav1.CreateOptions{})
	} else if policyErr == nil {
		policy.ResourceVersion = existingPolicy.ResourceVersion
		_, policyErr = policies.Update(ctx, policy, metav1.UpdateOptions{})
	}
	if policyErr != nil {
		return policyErr
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
					AutomountServiceAccountToken: boolptr(false),
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
								{Name: "CHROMIUM_CUSTOM_ARGS", Value: "--remote-debugging-address=127.0.0.1"},
								{Name: "WEB_LOCALHOST_ONLY", Value: "1"},
								{Name: "VNC_LOCALHOST_ONLY", Value: "1"},
								{Name: "DISPLAY_WIDTH", Value: strconv.Itoa(config.DisplayWidth)},
								{Name: "DISPLAY_HEIGHT", Value: strconv.Itoa(config.DisplayHeight)},
								{Name: "KEEP_APP_RUNNING", Value: "1"},
							},
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
							Env:             []corev1.EnvVar{{Name: "CDP_UPSTREAM", Value: "http://127.0.0.1:9223"}, {Name: "GUI_UPSTREAM", Value: "http://127.0.0.1:5800"}, {Name: "BROWSER_WORKER_AUTH_TOKEN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: name}, Key: browserWorkerAuthKey}}}},
							Ports:           []corev1.ContainerPort{{Name: "cdp", ContainerPort: 9222}, {Name: "web", ContainerPort: 5801}},
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
	if err := s.client.Clientset.NetworkingV1().NetworkPolicies(namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if err := s.client.Clientset.CoreV1().Secrets(namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func desiredBrowserWorkerNetworkPolicy(name, namespace, backendNamespace, runtimeNamespace string, labels map[string]string) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": labels["app"], "instance-id": labels["instance-id"]}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{From: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": backendNamespace}}, PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "clawmanager-app"}}}}, Ports: []networkingv1.NetworkPolicyPort{{Protocol: protocolPtr(corev1.ProtocolTCP), Port: intstrPtr(5801)}}},
				{From: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": runtimeNamespace}}, PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"clawmanager.io/runtime-type": "openclaw"}}}}, Ports: []networkingv1.NetworkPolicyPort{{Protocol: protocolPtr(corev1.ProtocolTCP), Port: intstrPtr(9222)}}},
			},
		},
	}
}

// Recover leftovers from any deletion path, including an interrupted cleanup.
func (s *BrowserWorkerService) cleanupOrphans(ctx context.Context) error {
	if s.instances == nil {
		return fmt.Errorf("instance repository is unavailable")
	}
	options := metav1.ListOptions{LabelSelector: "managed-by=" + browserWorkerManagedBy}
	ids := map[int]struct{}{}
	collect := func(labels map[string]string) {
		if id, err := strconv.Atoi(labels["instance-id"]); err == nil && id > 0 {
			ids[id] = struct{}{}
		}
	}
	deployments, err := s.client.Clientset.AppsV1().Deployments(s.namespace()).List(ctx, options)
	if err != nil {
		return err
	}
	for _, item := range deployments.Items {
		collect(item.Labels)
	}
	services, err := s.client.Clientset.CoreV1().Services(s.namespace()).List(ctx, options)
	if err != nil {
		return err
	}
	for _, item := range services.Items {
		collect(item.Labels)
	}
	secrets, err := s.client.Clientset.CoreV1().Secrets(s.namespace()).List(ctx, options)
	if err != nil {
		return err
	}
	for _, item := range secrets.Items {
		collect(item.Labels)
	}
	policies, err := s.client.Clientset.NetworkingV1().NetworkPolicies(s.namespace()).List(ctx, options)
	if err != nil {
		return err
	}
	for _, item := range policies.Items {
		collect(item.Labels)
	}
	for id := range ids {
		instance, err := s.instances.GetByID(id)
		if err != nil {
			continue
		} // Fail closed on database errors.
		if instance == nil {
			// Cascading deletion may already have removed the configuration row.
			s.mu.Lock()
			err := s.deleteResources(ctx, id)
			s.mu.Unlock()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *BrowserWorkerService) patchOpenClawConfig(instance *models.Instance, enabled bool, token string) error {
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
	if root == nil {
		return fmt.Errorf("OpenClaw config must be a JSON object")
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
		if len(token) < 32 {
			return fmt.Errorf("browser worker credentials are invalid")
		}
		cdpURL := &url.URL{Scheme: "http", Host: host + ":9222", User: url.UserPassword("openclaw", token)}
		root["browser"] = map[string]any{"enabled": true, "defaultProfile": "interactive", "ssrfPolicy": map[string]any{"allowedHostnames": []string{host}}, "profiles": map[string]any{"interactive": map[string]any{"cdpUrl": cdpURL.String(), "driver": "openclaw", "attachOnly": true}}}
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
	file, err := os.CreateTemp(filepath.Dir(path), ".browser-worker-*")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer os.Remove(temp)
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	// The patched file contains CDP credentials. CreateTemp's 0600 permissions
	// deliberately replace any broader permissions on the original file.
	runtimeID := RuntimeLinuxID(instance.ID)
	if err := setFileOwnership(temp, runtimeID, runtimeID); err != nil {
		_ = os.Remove(temp)
		return fmt.Errorf("set OpenClaw config ownership to %d:%d: %w", runtimeID, runtimeID, err)
	}
	return os.Rename(temp, path)
}

func int32ptr(value int32) *int32                            { return &value }
func boolptr(value bool) *bool                               { return &value }
func resourcePtr(value resource.Quantity) *resource.Quantity { return &value }
func protocolPtr(value corev1.Protocol) *corev1.Protocol     { return &value }
func intstrPtr(value int) *intstr.IntOrString                { port := intstr.FromInt(value); return &port }
