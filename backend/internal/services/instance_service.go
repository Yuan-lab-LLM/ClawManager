package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InstanceService defines the interface for instance operations
type InstanceService interface {
	Create(userID int, req CreateInstanceRequest) (*models.Instance, error)
	GetByID(id int) (*models.Instance, error)
	GetByUserID(userID int, offset, limit int) ([]models.Instance, int, error)
	GetAllInstances(offset, limit int) ([]models.Instance, int, error)
	Start(instanceID int) error
	Stop(instanceID int) error
	Restart(instanceID int) error
	Delete(instanceID int) error
	Update(instanceID int, req UpdateInstanceRequest) error
	GetInstanceStatus(instanceID int) (*InstanceStatus, error)
	ForceSyncInstance(instanceID int) error
}

// CreateInstanceRequest holds data for creating an instance
type CreateInstanceRequest struct {
	Name                 string              `json:"name" validate:"required,min=3,max=50"`
	Description          *string             `json:"description,omitempty"`
	Type                 string              `json:"type" validate:"required,oneof=openclaw ubuntu debian centos custom webtop"`
	CPUCores             float64             `json:"cpu_cores" validate:"required,min=0.1,max=32"`
	MemoryGB             int                 `json:"memory_gb" validate:"required,min=1,max=128"`
	DiskGB               int                 `json:"disk_gb" validate:"required,min=10,max=1000"`
	GPUEnabled           bool                `json:"gpu_enabled"`
	GPUCount             int                 `json:"gpu_count" validate:"min=0,max=4"`
	OSType               string              `json:"os_type" validate:"required"`
	OSVersion            string              `json:"os_version" validate:"required"`
	ImageRegistry        *string             `json:"image_registry,omitempty"`
	ImageTag             *string             `json:"image_tag,omitempty"`
	EnvironmentOverrides map[string]string   `json:"environment_overrides,omitempty"`
	StorageClass         string              `json:"storage_class"`
	OpenClawConfigPlan   *OpenClawConfigPlan `json:"openclaw_config_plan,omitempty"`
}

// UpdateInstanceRequest holds data for updating an instance
type UpdateInstanceRequest struct {
	Name        *string `json:"name,omitempty" validate:"omitempty,min=3,max=50"`
	Description *string `json:"description,omitempty"`
}

// InstanceStatus holds the status of an instance
type InstanceStatus struct {
	InstanceID   int        `json:"instance_id"`
	Status       string     `json:"status"`
	PodName      *string    `json:"pod_name,omitempty"`
	PodNamespace *string    `json:"pod_namespace,omitempty"`
	PodIP        *string    `json:"pod_ip,omitempty"`
	PodStatus    string     `json:"pod_status,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
}

// instanceService implements InstanceService
type instanceService struct {
	instanceRepo          repository.InstanceRepository
	quotaRepo             repository.QuotaRepository
	llmModelRepo          repository.LLMModelRepository
	openClawConfigService OpenClawConfigService
	podService            *k8s.PodService
	pvcService            *k8s.PVCService
	serviceService        *k8s.ServiceService
	networkPolicyService  *k8s.NetworkPolicyService
}

type gatewayModelInjection struct {
	defaultModel string
	modelsJSON   string
}

// NewInstanceService creates a new instance service
func NewInstanceService(instanceRepo repository.InstanceRepository, quotaRepo repository.QuotaRepository, llmModelRepo repository.LLMModelRepository, openClawConfigService OpenClawConfigService) InstanceService {
	return &instanceService{
		instanceRepo:          instanceRepo,
		quotaRepo:             quotaRepo,
		llmModelRepo:          llmModelRepo,
		openClawConfigService: openClawConfigService,
		podService:            k8s.NewPodService(),
		pvcService:            k8s.NewPVCService(),
		serviceService:        k8s.NewServiceService(),
		networkPolicyService:  k8s.NewNetworkPolicyService(),
	}
}

// Create creates a new instance
func (s *instanceService) Create(userID int, req CreateInstanceRequest) (*models.Instance, error) {
	ctx := context.Background()
	req.Name = strings.TrimSpace(req.Name)
	environmentOverrides, err := normalizeEnvironmentOverrides(req.EnvironmentOverrides)
	if err != nil {
		return nil, err
	}
	environmentOverridesJSON, err := marshalEnvironmentOverrides(environmentOverrides)
	if err != nil {
		return nil, err
	}

	// Check user quota
	quota, err := s.quotaRepo.GetByUserID(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user quota: %w", err)
	}

	if quota == nil {
		return nil, fmt.Errorf("user quota not found")
	}

	// Check instance count limit
	currentCount, err := s.instanceRepo.CountByUserID(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to count instances: %w", err)
	}

	if currentCount >= quota.MaxInstances {
		return nil, fmt.Errorf("instance limit reached: %d/%d", currentCount, quota.MaxInstances)
	}

	existingInstances, err := s.instanceRepo.GetByUserID(userID, 0, 1000)
	if err != nil {
		return nil, fmt.Errorf("failed to list user instances for quota validation: %w", err)
	}

	currentCPU := 0.0
	currentMemory := 0
	currentStorage := 0
	currentGPU := 0
	for _, existing := range existingInstances {
		currentCPU += existing.CPUCores
		currentMemory += existing.MemoryGB
		currentStorage += existing.DiskGB
		if existing.GPUEnabled {
			currentGPU += existing.GPUCount
		}
	}

	nameExists, err := s.instanceRepo.ExistsByUserIDAndName(userID, req.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to validate instance name: %w", err)
	}
	if nameExists {
		return nil, fmt.Errorf("instance name already exists")
	}

	// Check CPU limit
	if currentCPU+req.CPUCores > quota.MaxCPUCores {
		return nil, fmt.Errorf("CPU cores exceed quota: current %v, requested %v, max %v", currentCPU, req.CPUCores, quota.MaxCPUCores)
	}

	// Check memory limit
	if currentMemory+req.MemoryGB > quota.MaxMemoryGB {
		return nil, fmt.Errorf("memory exceed quota: current %dGB, requested %dGB, max %dGB", currentMemory, req.MemoryGB, quota.MaxMemoryGB)
	}

	// Check storage limit
	if currentStorage+req.DiskGB > quota.MaxStorageGB {
		return nil, fmt.Errorf("storage exceed quota: current %dGB, requested %dGB, max %dGB", currentStorage, req.DiskGB, quota.MaxStorageGB)
	}

	// Check GPU limit
	requestedGPU := 0
	if req.GPUEnabled {
		requestedGPU = req.GPUCount
	}
	if currentGPU+requestedGPU > quota.MaxGPUCount {
		return nil, fmt.Errorf("GPU count exceed quota: current %d, requested %d, max %d", currentGPU, requestedGPU, quota.MaxGPUCount)
	}

	runtimeConfig := buildRuntimeConfig(req.Type, req.OSType, req.OSVersion, req.ImageRegistry, req.ImageTag)
	if (req.ImageRegistry == nil || strings.TrimSpace(*req.ImageRegistry) == "") && (req.ImageTag == nil || strings.TrimSpace(*req.ImageTag) == "") {
		if image, ok := runtimeImageOverride(req.Type); ok {
			req.ImageRegistry = &image
			req.ImageTag = nil
			runtimeConfig = buildRuntimeConfig(req.Type, req.OSType, req.OSVersion, req.ImageRegistry, req.ImageTag)
		}
	}

	// Check if there are any orphaned resources from previous failed creations
	fmt.Printf("Checking for orphaned resources for user %d before creating new instance...\n", userID)
	s.cleanupOrphanedResourcesByUser(ctx, userID)

	// Create instance record
	now := time.Now()
	instance := &models.Instance{
		UserID:                   userID,
		Name:                     req.Name,
		Description:              req.Description,
		Type:                     req.Type,
		Status:                   "creating",
		CPUCores:                 req.CPUCores,
		MemoryGB:                 req.MemoryGB,
		DiskGB:                   req.DiskGB,
		GPUEnabled:               req.GPUEnabled,
		GPUCount:                 req.GPUCount,
		OSType:                   req.OSType,
		OSVersion:                req.OSVersion,
		ImageRegistry:            req.ImageRegistry,
		ImageTag:                 req.ImageTag,
		EnvironmentOverridesJSON: environmentOverridesJSON,
		StorageClass:             req.StorageClass,
		MountPath:                runtimeConfig.MountPath,
		CreatedAt:                now,
		UpdatedAt:                now,
	}

	if err := s.instanceRepo.Create(instance); err != nil {
		return nil, fmt.Errorf("failed to create instance record: %w", err)
	}

	if _, err := s.ensureGatewayToken(instance); err != nil {
		s.instanceRepo.Delete(instance.ID)
		return nil, fmt.Errorf("failed to provision instance gateway token: %w", err)
	}
	if _, err := s.ensureAgentBootstrapToken(instance); err != nil {
		s.instanceRepo.Delete(instance.ID)
		return nil, fmt.Errorf("failed to provision instance agent bootstrap token: %w", err)
	}

	gatewayEnv, err := s.buildGatewayEnv(instance)
	if err != nil {
		s.instanceRepo.Delete(instance.ID)
		return nil, fmt.Errorf("failed to build instance gateway config: %w", err)
	}
	agentEnv, err := s.buildAgentEnv(instance)
	if err != nil {
		s.instanceRepo.Delete(instance.ID)
		return nil, fmt.Errorf("failed to build instance agent config: %w", err)
	}
	extraEnv, err := buildInstancePodEnv(instance, runtimeConfig.Env, gatewayEnv, agentEnv)
	if err != nil {
		s.instanceRepo.Delete(instance.ID)
		return nil, fmt.Errorf("failed to resolve instance environment: %w", err)
	}

	var bootstrapSnapshot *models.OpenClawInjectionSnapshot
	var bootstrapSecretName string
	if strings.EqualFold(instance.Type, "openclaw") && s.openClawConfigService != nil && req.OpenClawConfigPlan != nil && hasOpenClawConfigSelections(*req.OpenClawConfigPlan) {
		bootstrapSnapshot, err = s.openClawConfigService.CreateSnapshotForInstance(userID, instance, req.OpenClawConfigPlan)
		if err != nil {
			s.instanceRepo.Delete(instance.ID)
			return nil, fmt.Errorf("failed to compile openclaw bootstrap config: %w", err)
		}
		if bootstrapSnapshot != nil {
			instance.OpenClawConfigSnapshotID = &bootstrapSnapshot.ID
			instance.UpdatedAt = time.Now()
			if err := s.instanceRepo.Update(instance); err != nil {
				s.instanceRepo.Delete(instance.ID)
				return nil, fmt.Errorf("failed to persist openclaw snapshot reference: %w", err)
			}

			bootstrapSecretName, err = s.openClawConfigService.EnsureSnapshotSecret(ctx, userID, instance, bootstrapSnapshot.ID)
			if err != nil {
				_ = s.openClawConfigService.MarkSnapshotFailed(bootstrapSnapshot, err)
				s.instanceRepo.Delete(instance.ID)
				return nil, fmt.Errorf("failed to provision openclaw bootstrap secret: %w", err)
			}
		}
	}

	// Create PVC
	// If storage class is not specified in request, use empty string
	// PVCService will use the default from K8s client config
	storageClass := req.StorageClass

	_, err = s.pvcService.CreatePVC(ctx, userID, instance.ID, req.DiskGB, storageClass)
	if err != nil {
		// Rollback: delete instance record
		if bootstrapSnapshot != nil {
			_ = s.openClawConfigService.MarkSnapshotFailed(bootstrapSnapshot, err)
		}
		s.instanceRepo.Delete(instance.ID)
		return nil, fmt.Errorf("failed to create PVC: %w", err)
	}

	if requiresRestrictedNetwork(instance.Type) {
		if err := s.networkPolicyService.EnsureDefaultPolicy(ctx, userID, instance.ID, instance.Name); err != nil {
			s.pvcService.DeletePVC(ctx, userID, instance.ID)
			if bootstrapSnapshot != nil {
				_ = s.openClawConfigService.MarkSnapshotFailed(bootstrapSnapshot, err)
			}
			s.instanceRepo.Delete(instance.ID)
			return nil, fmt.Errorf("failed to create network policy: %w", err)
		}
	}

	// Create Pod
	podConfig := k8s.PodConfig{
		InstanceID:         instance.ID,
		InstanceName:       instance.Name,
		UserID:             userID,
		Type:               instance.Type,
		CPUCores:           instance.CPUCores,
		MemoryGB:           instance.MemoryGB,
		GPUEnabled:         instance.GPUEnabled,
		GPUCount:           instance.GPUCount,
		Image:              runtimeConfig.Image,
		MountPath:          runtimeConfig.MountPath,
		ContainerPort:      runtimeConfig.Port,
		ExtraEnv:           extraEnv,
		EnvFromSecretNames: []string{bootstrapSecretName},
	}

	pod, err := s.podService.CreatePod(ctx, podConfig)
	if err != nil {
		// Rollback: delete PVC and instance record
		if requiresRestrictedNetwork(instance.Type) {
			s.networkPolicyService.DeletePolicy(ctx, userID, instance.ID, instance.Name)
		}
		s.pvcService.DeletePVC(ctx, userID, instance.ID)
		if bootstrapSnapshot != nil {
			_ = s.openClawConfigService.MarkSnapshotFailed(bootstrapSnapshot, err)
		}
		s.instanceRepo.Delete(instance.ID)
		return nil, fmt.Errorf("failed to create pod: %w", err)
	}

	// Create Service for the instance
	serviceConfig := k8s.ServiceConfig{
		InstanceID:      instance.ID,
		InstanceName:    instance.Name,
		UserID:          userID,
		ContainerPort:   runtimeConfig.Port,
		AdditionalPorts: additionalServicePorts(runtimeConfig.Port),
	}

	serviceInfo, err := s.serviceService.CreateService(ctx, serviceConfig)
	if err != nil {
		// Rollback: delete pod, PVC and instance record
		s.podService.DeletePod(ctx, userID, instance.ID)
		if requiresRestrictedNetwork(instance.Type) {
			s.networkPolicyService.DeletePolicy(ctx, userID, instance.ID, instance.Name)
		}
		s.pvcService.DeletePVC(ctx, userID, instance.ID)
		if bootstrapSnapshot != nil {
			_ = s.openClawConfigService.MarkSnapshotFailed(bootstrapSnapshot, err)
		}
		s.instanceRepo.Delete(instance.ID)
		return nil, fmt.Errorf("failed to create service: %w", err)
	}

	fmt.Printf("Instance %d: Service created successfully (ClusterIP: %s)\n", instance.ID, serviceInfo.ClusterIP)

	// Update instance with pod info
	podNamespace := pod.Namespace
	podName := pod.Name
	instance.PodNamespace = &podNamespace
	instance.PodName = &podName
	instance.Status = "creating"
	instance.StartedAt = &now
	instance.UpdatedAt = now

	fmt.Printf("Instance %d created successfully, updating database with status 'creating'\n", instance.ID)
	if err := s.instanceRepo.Update(instance); err != nil {
		if bootstrapSnapshot != nil {
			_ = s.openClawConfigService.MarkSnapshotFailed(bootstrapSnapshot, err)
		}
		return nil, fmt.Errorf("failed to update instance with pod info: %w", err)
	}
	fmt.Printf("Instance %d database updated, broadcasting status via WebSocket\n", instance.ID)

	if bootstrapSnapshot != nil {
		if err := s.openClawConfigService.MarkSnapshotActive(bootstrapSnapshot); err != nil {
			return nil, fmt.Errorf("failed to activate openclaw bootstrap snapshot: %w", err)
		}
	}

	// Broadcast initial creating status via WebSocket. Sync service will mark it
	// running only after the pod becomes Ready.
	GetHub().BroadcastInstanceStatus(userID, instance)
	fmt.Printf("Instance %d status broadcast complete\n", instance.ID)

	return instance, nil
}

// GetByID gets an instance by ID
func (s *instanceService) GetByID(id int) (*models.Instance, error) {
	return s.instanceRepo.GetByID(id)
}

// GetByUserID gets instances by user ID with pagination
func (s *instanceService) GetByUserID(userID int, offset, limit int) ([]models.Instance, int, error) {
	instances, err := s.instanceRepo.GetByUserID(userID, offset, limit)
	if err != nil {
		return nil, 0, err
	}

	total, err := s.instanceRepo.CountByUserID(userID)
	if err != nil {
		return nil, 0, err
	}

	return instances, total, nil
}

func (s *instanceService) GetAllInstances(offset, limit int) ([]models.Instance, int, error) {
	instances, err := s.instanceRepo.GetAll(offset, limit)
	if err != nil {
		return nil, 0, err
	}

	total, err := s.instanceRepo.CountAll()
	if err != nil {
		return nil, 0, err
	}

	return instances, total, nil
}

// Start starts an instance
func (s *instanceService) Start(instanceID int) error {
	ctx := context.Background()

	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return fmt.Errorf("failed to get instance: %w", err)
	}

	if instance == nil {
		return fmt.Errorf("instance not found")
	}

	if instance.Status == "running" {
		return fmt.Errorf("instance is already running")
	}

	if _, err := s.ensureGatewayToken(instance); err != nil {
		return fmt.Errorf("failed to provision instance gateway token: %w", err)
	}
	if _, err := s.ensureAgentBootstrapToken(instance); err != nil {
		return fmt.Errorf("failed to provision instance agent bootstrap token: %w", err)
	}

	gatewayEnv, err := s.buildGatewayEnv(instance)
	if err != nil {
		return fmt.Errorf("failed to build instance gateway config: %w", err)
	}
	agentEnv, err := s.buildAgentEnv(instance)
	if err != nil {
		return fmt.Errorf("failed to build instance agent config: %w", err)
	}
	runtimeConfig := buildRuntimeConfig(instance.Type, instance.OSType, instance.OSVersion, instance.ImageRegistry, instance.ImageTag)
	extraEnv, err := buildInstancePodEnv(instance, runtimeConfig.Env, gatewayEnv, agentEnv)
	if err != nil {
		return fmt.Errorf("failed to resolve instance environment: %w", err)
	}

	bootstrapSecretName := ""
	if strings.EqualFold(instance.Type, "openclaw") && s.openClawConfigService != nil && instance.OpenClawConfigSnapshotID != nil && *instance.OpenClawConfigSnapshotID > 0 {
		bootstrapSecretName, err = s.openClawConfigService.EnsureSnapshotSecret(ctx, instance.UserID, instance, *instance.OpenClawConfigSnapshotID)
		if err != nil {
			return fmt.Errorf("failed to restore openclaw bootstrap secret: %w", err)
		}
	}

	// Create new pod
	if requiresRestrictedNetwork(instance.Type) {
		if err := s.networkPolicyService.EnsureDefaultPolicy(ctx, instance.UserID, instance.ID, instance.Name); err != nil {
			return fmt.Errorf("failed to create network policy: %w", err)
		}
	}

	podConfig := k8s.PodConfig{
		InstanceID:         instance.ID,
		InstanceName:       instance.Name,
		UserID:             instance.UserID,
		Type:               instance.Type,
		CPUCores:           instance.CPUCores,
		MemoryGB:           instance.MemoryGB,
		GPUEnabled:         instance.GPUEnabled,
		GPUCount:           instance.GPUCount,
		Image:              runtimeConfig.Image,
		MountPath:          instance.MountPath,
		ContainerPort:      runtimeConfig.Port,
		ExtraEnv:           extraEnv,
		EnvFromSecretNames: []string{bootstrapSecretName},
	}

	pod, err := s.podService.CreatePod(ctx, podConfig)
	if err != nil {
		return fmt.Errorf("failed to create pod: %w", err)
	}

	// Ensure Service exists (create if not exists)
	serviceExists, _ := s.serviceService.ServiceExists(ctx, instance.UserID, instance.ID)
	if !serviceExists {
		serviceConfig := k8s.ServiceConfig{
			InstanceID:      instance.ID,
			InstanceName:    instance.Name,
			UserID:          instance.UserID,
			ContainerPort:   runtimeConfig.Port,
			AdditionalPorts: additionalServicePorts(runtimeConfig.Port),
		}
		_, err = s.serviceService.CreateService(ctx, serviceConfig)
		if err != nil {
			fmt.Printf("Warning: failed to create service for instance %d: %v\n", instance.ID, err)
			// Don't fail if service creation fails, pod is already running
		}
	}

	// Update instance status
	now := time.Now()
	podNamespace := pod.Namespace
	podName := pod.Name
	instance.PodNamespace = &podNamespace
	instance.PodName = &podName
	instance.Status = "creating"
	instance.StartedAt = &now
	instance.UpdatedAt = now

	if err := s.instanceRepo.Update(instance); err != nil {
		return fmt.Errorf("failed to update instance status: %w", err)
	}

	// Broadcast status update via WebSocket
	GetHub().BroadcastInstanceStatus(instance.UserID, instance)

	return nil
}

func requiresRestrictedNetwork(instanceType string) bool {
	return strings.TrimSpace(instanceType) != ""
}

func (s *instanceService) ensureGatewayToken(instance *models.Instance) (string, error) {
	if instance.AccessToken != nil && strings.TrimSpace(*instance.AccessToken) != "" {
		return strings.TrimSpace(*instance.AccessToken), nil
	}

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("failed to generate instance gateway token: %w", err)
	}

	token := "igt_" + hex.EncodeToString(tokenBytes)
	instance.AccessToken = &token
	instance.UpdatedAt = time.Now()
	if err := s.instanceRepo.Update(instance); err != nil {
		return "", fmt.Errorf("failed to persist instance gateway token: %w", err)
	}

	return token, nil
}

func (s *instanceService) buildGatewayEnv(instance *models.Instance) (map[string]string, error) {
	if instance == nil || instance.AccessToken == nil || strings.TrimSpace(*instance.AccessToken) == "" {
		return map[string]string{}, nil
	}
	if !strings.EqualFold(strings.TrimSpace(instance.Type), "openclaw") {
		return map[string]string{}, nil
	}

	baseURL, ok := defaultGatewayBaseURL()
	if !ok {
		return nil, fmt.Errorf("gateway base URL is not configured")
	}

	modelInjection, err := s.resolveGatewayModelInjection()
	if err != nil {
		return nil, err
	}

	token := strings.TrimSpace(*instance.AccessToken)
	return map[string]string{
		"CLAWMANAGER_LLM_BASE_URL":   baseURL,
		"CLAWMANAGER_LLM_API_KEY":    token,
		"CLAWMANAGER_LLM_MODEL":      modelInjection.modelsJSON,
		"CLAWMANAGER_LLM_PROVIDER":   "openai-compatible",
		"CLAWMANAGER_INSTANCE_TOKEN": token,
		"OPENAI_BASE_URL":            baseURL,
		"OPENAI_API_BASE":            baseURL,
		"OPENAI_API_KEY":             token,
		"OPENAI_MODEL":               modelInjection.defaultModel,
	}, nil
}

func (s *instanceService) ensureAgentBootstrapToken(instance *models.Instance) (string, error) {
	if instance.AgentBootstrapToken != nil && strings.TrimSpace(*instance.AgentBootstrapToken) != "" {
		return strings.TrimSpace(*instance.AgentBootstrapToken), nil
	}

	token, err := generatePrefixedToken("agt_boot")
	if err != nil {
		return "", fmt.Errorf("failed to generate instance agent bootstrap token: %w", err)
	}
	instance.AgentBootstrapToken = &token
	instance.UpdatedAt = time.Now()
	if err := s.instanceRepo.Update(instance); err != nil {
		return "", fmt.Errorf("failed to persist instance agent bootstrap token: %w", err)
	}
	return token, nil
}

func (s *instanceService) buildAgentEnv(instance *models.Instance) (map[string]string, error) {
	if instance == nil || !strings.EqualFold(strings.TrimSpace(instance.Type), "openclaw") {
		return map[string]string{}, nil
	}
	if instance.AgentBootstrapToken == nil || strings.TrimSpace(*instance.AgentBootstrapToken) == "" {
		return nil, fmt.Errorf("instance agent bootstrap token is not configured")
	}

	baseURL, ok := defaultAgentControlBaseURL()
	if !ok {
		return nil, fmt.Errorf("agent control base URL is not configured")
	}

	diskLimitBytes := int64(instance.DiskGB) * 1024 * 1024 * 1024

	return map[string]string{
		"CLAWMANAGER_AGENT_ENABLED":          "true",
		"CLAWMANAGER_AGENT_BASE_URL":         baseURL,
		"CLAWMANAGER_AGENT_BOOTSTRAP_TOKEN":  strings.TrimSpace(*instance.AgentBootstrapToken),
		"CLAWMANAGER_AGENT_DISK_LIMIT_BYTES": strconv.FormatInt(diskLimitBytes, 10),
		"CLAWMANAGER_AGENT_INSTANCE_ID":      fmt.Sprintf("%d", instance.ID),
		"CLAWMANAGER_AGENT_PERSISTENT_DIR":   "/config",
		"CLAWMANAGER_AGENT_PROTOCOL_VERSION": AgentProtocolVersionV1,
	}, nil
}

func (s *instanceService) resolveGatewayModelInjection() (*gatewayModelInjection, error) {
	if s.llmModelRepo == nil {
		return nil, fmt.Errorf("llm model repository not configured")
	}

	items, err := s.llmModelRepo.ListActive()
	if err != nil {
		return nil, fmt.Errorf("failed to list active models: %w", err)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no active models are configured")
	}

	modelsForInjection := []string{"auto"}
	seen := map[string]struct{}{
		"auto": {},
	}

	for _, item := range items {
		displayName := strings.TrimSpace(item.DisplayName)
		if displayName == "" {
			displayName = strings.TrimSpace(item.ProviderModelName)
		}
		if displayName == "" {
			continue
		}

		normalizedName := strings.ToLower(displayName)
		if _, exists := seen[normalizedName]; exists {
			continue
		}
		seen[normalizedName] = struct{}{}
		modelsForInjection = append(modelsForInjection, displayName)
	}

	rawModels, err := json.Marshal(modelsForInjection)
	if err != nil {
		return nil, fmt.Errorf("failed to encode gateway model list: %w", err)
	}

	return &gatewayModelInjection{
		defaultModel: "auto",
		modelsJSON:   string(rawModels),
	}, nil
}

func mergeEnvMaps(base map[string]string, overlay map[string]string) map[string]string {
	merged := map[string]string{}
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range overlay {
		merged[key] = value
	}
	return merged
}

// Stop stops an instance
func (s *instanceService) Stop(instanceID int) error {
	ctx := context.Background()

	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return fmt.Errorf("failed to get instance: %w", err)
	}

	if instance == nil {
		return fmt.Errorf("instance not found")
	}

	if instance.Status != "running" {
		return fmt.Errorf("instance is not running")
	}

	// Delete pod
	if err := s.podService.DeletePod(ctx, instance.UserID, instance.ID); err != nil {
		return fmt.Errorf("failed to delete pod: %w", err)
	}

	// Update instance status
	now := time.Now()
	instance.Status = "stopped"
	instance.StoppedAt = &now
	instance.PodName = nil
	instance.PodNamespace = nil
	instance.PodIP = nil
	instance.UpdatedAt = now

	if err := s.instanceRepo.Update(instance); err != nil {
		return fmt.Errorf("failed to update instance status: %w", err)
	}

	// Broadcast status update via WebSocket
	GetHub().BroadcastInstanceStatus(instance.UserID, instance)

	return nil
}

// Restart restarts an instance
func (s *instanceService) Restart(instanceID int) error {
	if err := s.Stop(instanceID); err != nil {
		return fmt.Errorf("failed to stop instance: %w", err)
	}

	if err := s.Start(instanceID); err != nil {
		return fmt.Errorf("failed to start instance: %w", err)
	}

	return nil
}

// Delete deletes an instance and all associated K8s resources
func (s *instanceService) Delete(instanceID int) error {
	ctx := context.Background()

	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return fmt.Errorf("failed to get instance: %w", err)
	}

	if instance == nil {
		// Instance not in DB, but we should still try to clean up K8s resources
		fmt.Printf("Instance %d not found in database, attempting to clean up any orphaned K8s resources\n", instanceID)
		// Try to clean up with userID=0 (will need to scan all namespaces)
		cleanupService := k8s.NewCleanupService()
		cleanupService.DeleteAllInstanceResources(ctx, 0, instanceID)
		return fmt.Errorf("instance not found")
	}

	fmt.Printf("Starting deletion of instance %d (user %d)\n", instanceID, instance.UserID)

	// Use CleanupService to delete ALL resources for this instance (including duplicates)
	cleanupService := k8s.NewCleanupService()
	if err := cleanupService.DeleteAllInstanceResources(ctx, instance.UserID, instance.ID); err != nil {
		fmt.Printf("Warning: error during resource cleanup for instance %d: %v\n", instanceID, err)
	}

	// 4. Delete instance record from database
	fmt.Printf("Deleting instance %d from database...\n", instanceID)
	if err := s.instanceRepo.Delete(instanceID); err != nil {
		return fmt.Errorf("failed to delete instance record: %w", err)
	}

	fmt.Printf("Instance %d deleted successfully\n", instanceID)
	return nil
}

// cleanupOrphanedResources cleans up any orphaned K8s resources for an instance
func (s *instanceService) cleanupOrphanedResources(ctx context.Context, userID, instanceID int) error {
	namespace := s.pvcService.GetClient().GetNamespace(userID)
	instanceLabel := fmt.Sprintf("%d", instanceID)
	client := s.pvcService.GetClient().Clientset

	// Check if namespace has other instances' pods
	allPods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "managed-by=clawreef",
	})
	if err == nil {
		otheInstanceCount := 0
		for _, pod := range allPods.Items {
			if pod.Labels["instance-id"] != instanceLabel {
				otheInstanceCount++
			}
		}
		fmt.Printf("Namespace %s has %d other instance(s), will not delete namespace\n", namespace, otheInstanceCount)
	}

	// List and delete ConfigMaps with instance label
	configMaps, err := client.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("instance-id=%s", instanceLabel),
	})
	if err == nil && len(configMaps.Items) > 0 {
		for _, cm := range configMaps.Items {
			fmt.Printf("Deleting orphaned ConfigMap %s\n", cm.Name)
			client.CoreV1().ConfigMaps(namespace).Delete(ctx, cm.Name, metav1.DeleteOptions{})
		}
	}

	// List and delete Secrets with instance label
	secrets, err := client.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("instance-id=%s", instanceLabel),
	})
	if err == nil && len(secrets.Items) > 0 {
		for _, secret := range secrets.Items {
			fmt.Printf("Deleting orphaned Secret %s\n", secret.Name)
			client.CoreV1().Secrets(namespace).Delete(ctx, secret.Name, metav1.DeleteOptions{})
		}
	}

	// List and delete Services with instance label
	services, err := client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("instance-id=%s", instanceLabel),
	})
	if err == nil && len(services.Items) > 0 {
		for _, svc := range services.Items {
			fmt.Printf("Deleting orphaned Service %s\n", svc.Name)
			client.CoreV1().Services(namespace).Delete(ctx, svc.Name, metav1.DeleteOptions{})
		}
	}

	return nil
}

// cleanupOrphanedResourcesByUser cleans up any orphaned resources for a user that don't have corresponding DB records
func (s *instanceService) cleanupOrphanedResourcesByUser(ctx context.Context, userID int) {
	namespace := s.pvcService.GetClient().GetNamespace(userID)
	client := s.pvcService.GetClient().Clientset

	// Get all pods in the namespace with clawreef label
	pods, err := client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "managed-by=clawreef",
	})
	if err != nil {
		fmt.Printf("Warning: failed to list pods in namespace %s: %v\n", namespace, err)
		return
	}

	// For each pod, check if corresponding instance exists in DB
	for _, pod := range pods.Items {
		instanceIDStr := pod.Labels["instance-id"]
		if instanceIDStr == "" {
			continue
		}

		instanceID := 0
		fmt.Sscanf(instanceIDStr, "%d", &instanceID)

		// Check if instance exists in DB
		instance, err := s.instanceRepo.GetByID(instanceID)
		if err != nil || instance == nil {
			// Instance doesn't exist, this is an orphaned pod
			fmt.Printf("Found orphaned pod %s (instance-id: %s), deleting...\n", pod.Name, instanceIDStr)
			if err := client.CoreV1().Pods(namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{}); err != nil {
				fmt.Printf("Warning: failed to delete orphaned pod %s: %v\n", pod.Name, err)
			}
		}
	}

	// Also check PVCs
	pvcs, err := client.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "managed-by=clawreef",
	})
	if err != nil {
		fmt.Printf("Warning: failed to list PVCs in namespace %s: %v\n", namespace, err)
		return
	}

	for _, pvc := range pvcs.Items {
		instanceIDStr := pvc.Labels["instance-id"]
		if instanceIDStr == "" {
			continue
		}

		instanceID := 0
		fmt.Sscanf(instanceIDStr, "%d", &instanceID)

		// Check if instance exists in DB
		instance, err := s.instanceRepo.GetByID(instanceID)
		if err != nil || instance == nil {
			// Instance doesn't exist, this is an orphaned PVC
			fmt.Printf("Found orphaned PVC %s (instance-id: %s), deleting...\n", pvc.Name, instanceIDStr)
			if err := client.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, pvc.Name, metav1.DeleteOptions{}); err != nil {
				fmt.Printf("Warning: failed to delete orphaned PVC %s: %v\n", pvc.Name, err)
			}
			// Also try to delete the associated PV
			if pvc.Spec.VolumeName != "" {
				client.CoreV1().PersistentVolumes().Delete(ctx, pvc.Spec.VolumeName, metav1.DeleteOptions{})
			}
		}
	}

	networkPolicies, err := client.NetworkingV1().NetworkPolicies(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "managed-by=clawreef",
	})
	if err != nil {
		fmt.Printf("Warning: failed to list network policies in namespace %s: %v\n", namespace, err)
		return
	}

	for _, policy := range networkPolicies.Items {
		instanceIDStr := policy.Labels["instance-id"]
		if instanceIDStr == "" {
			continue
		}

		instanceID := 0
		fmt.Sscanf(instanceIDStr, "%d", &instanceID)

		instance, err := s.instanceRepo.GetByID(instanceID)
		if err != nil || instance == nil {
			fmt.Printf("Found orphaned NetworkPolicy %s (instance-id: %s), deleting...\n", policy.Name, instanceIDStr)
			if err := client.NetworkingV1().NetworkPolicies(namespace).Delete(ctx, policy.Name, metav1.DeleteOptions{}); err != nil {
				fmt.Printf("Warning: failed to delete orphaned NetworkPolicy %s: %v\n", policy.Name, err)
			}
		}
	}
}

// Update updates an instance
func (s *instanceService) Update(instanceID int, req UpdateInstanceRequest) error {
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return fmt.Errorf("failed to get instance: %w", err)
	}

	if instance == nil {
		return fmt.Errorf("instance not found")
	}

	// Update fields
	if req.Name != nil {
		instance.Name = *req.Name
	}
	if req.Description != nil {
		instance.Description = req.Description
	}

	instance.UpdatedAt = time.Now()

	if err := s.instanceRepo.Update(instance); err != nil {
		return fmt.Errorf("failed to update instance: %w", err)
	}

	return nil
}

// GetInstanceStatus gets the detailed status of an instance
func (s *instanceService) GetInstanceStatus(instanceID int) (*InstanceStatus, error) {
	ctx := context.Background()

	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}

	if instance == nil {
		return nil, fmt.Errorf("instance not found")
	}

	status := &InstanceStatus{
		InstanceID:   instance.ID,
		Status:       instance.Status,
		PodName:      instance.PodName,
		PodNamespace: instance.PodNamespace,
		PodIP:        instance.PodIP,
		CreatedAt:    instance.CreatedAt,
		StartedAt:    instance.StartedAt,
	}

	// Get pod status if running
	if instance.Status == "running" || instance.Status == "creating" {
		podStatus, err := s.podService.GetPodStatus(ctx, instance.UserID, instance.ID)
		if err == nil && podStatus != nil {
			status.PodStatus = string(podStatus.Phase)
		}
	}

	return status, nil
}

// ForceSyncInstance forces a status sync for a single instance
func (s *instanceService) ForceSyncInstance(instanceID int) error {
	ctx := context.Background()

	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return fmt.Errorf("failed to get instance: %w", err)
	}

	if instance == nil {
		return fmt.Errorf("instance not found")
	}

	fmt.Printf("Force syncing instance %d (current status: %s, user: %d)\n", instanceID, instance.Status, instance.UserID)

	// First try direct lookup by instance ID
	pod, err := s.podService.GetPod(ctx, instance.UserID, instance.ID)
	if err != nil {
		// Pod not found by instance ID, try to find by namespace scan
		fmt.Printf("Instance %d: Pod not found by ID, scanning namespace for any matching pods...\n", instanceID)

		namespace := s.pvcService.GetClient().GetNamespace(instance.UserID)
		pods, listErr := s.pvcService.GetClient().Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: "managed-by=clawreef",
		})

		if listErr == nil && len(pods.Items) > 0 {
			// Try to find a pod that might belong to this instance by name pattern
			for _, p := range pods.Items {
				// Check if pod name contains instance ID
				if p.Labels["instance-id"] == fmt.Sprintf("%d", instanceID) {
					fmt.Printf("Instance %d: Found matching pod %s by label scan\n", instanceID, p.Name)
					pod = &p
					err = nil
					break
				}
			}
		}
	}

	if err != nil {
		fmt.Printf("Instance %d: Pod not found in K8s: %v\n", instanceID, err)

		// If instance thinks it's running or creating but pod doesn't exist, update to stopped
		if instance.Status == "running" || instance.Status == "creating" {
			fmt.Printf("Instance %d: Updating status from %s to stopped\n", instanceID, instance.Status)
			instance.Status = "stopped"
			instance.PodName = nil
			instance.PodNamespace = nil
			instance.PodIP = nil
			instance.UpdatedAt = time.Now()

			if err := s.instanceRepo.Update(instance); err != nil {
				return fmt.Errorf("failed to update instance status: %w", err)
			}

			// Broadcast status update
			GetHub().BroadcastInstanceStatus(instance.UserID, instance)
		}
		return nil
	}

	// Pod exists, sync status
	fmt.Printf("Instance %d: Pod found - %s (Status: %s, IP: %s)\n",
		instanceID, pod.Name, pod.Status.Phase, pod.Status.PodIP)

	needsUpdate := false

	// Check pod status
	if pod.Status.Phase == "Running" && instance.Status != "running" {
		fmt.Printf("Instance %d: Status mismatch - Pod Running but instance %s, updating\n", instanceID, instance.Status)
		instance.Status = "running"
		needsUpdate = true
	} else if pod.Status.Phase == "Pending" && instance.Status != "creating" {
		fmt.Printf("Instance %d: Status mismatch - Pod Pending but instance %s, updating\n", instanceID, instance.Status)
		instance.Status = "creating"
		needsUpdate = true
	}

	// Update Pod info if changed
	if instance.PodName == nil || *instance.PodName != pod.Name {
		instance.PodName = &pod.Name
		needsUpdate = true
	}
	if instance.PodNamespace == nil || *instance.PodNamespace != pod.Namespace {
		instance.PodNamespace = &pod.Namespace
		needsUpdate = true
	}
	if pod.Status.PodIP != "" && (instance.PodIP == nil || *instance.PodIP != pod.Status.PodIP) {
		instance.PodIP = &pod.Status.PodIP
		needsUpdate = true
	}

	if needsUpdate {
		instance.UpdatedAt = time.Now()
		if err := s.instanceRepo.Update(instance); err != nil {
			return fmt.Errorf("failed to update instance: %w", err)
		}

		fmt.Printf("Instance %d: Status updated to %s, broadcasting\n", instanceID, instance.Status)
		// Broadcast status update
		GetHub().BroadcastInstanceStatus(instance.UserID, instance)
	} else {
		fmt.Printf("Instance %d: Status already in sync (%s)\n", instanceID, instance.Status)
	}

	return nil
}

func additionalServicePorts(primaryPort int32) []int32 {
	if primaryPort == 3000 || primaryPort == 8082 {
		return []int32{3000, 8082}
	}

	return nil
}
