package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// InstanceService defines the interface for instance operations
type InstanceService interface {
	Create(userID int, req CreateInstanceRequest) (*models.Instance, error)
	CreatePrevalidated(userID int, req CreateInstanceRequest) (*models.Instance, error)
	ValidateCreateRequests(userID int, requests []CreateInstanceRequest) error
	GetByID(id int) (*models.Instance, error)
	GetByUserID(userID int, offset, limit int) ([]models.Instance, int, error)
	GetAllInstances(offset, limit int) ([]models.Instance, int, error)
	Start(instanceID int) error
	Stop(instanceID int) error
	Restart(instanceID int) error
	GetEnvironmentOverrideNames(instanceID int) ([]string, error)
	RestartWithEnvironment(instanceID int, environmentOverrides map[string]string, environmentOverrideRemovals []string) error
	Delete(instanceID int) error
	Update(instanceID int, req UpdateInstanceRequest) error
	GetInstanceStatus(instanceID int) (*InstanceStatus, error)
	ForceSyncInstance(instanceID int) error
}

const OpenCodeDefaultProjectRelativePath = "starter"

// InstanceOwnerService is the owner-scoped listing capability used by the
// northbound API and its authenticated portal page.
type InstanceOwnerService interface {
	GetLiteByUserIDAndOwner(userID int, owner string, offset, limit int) ([]models.Instance, int, error)
	GetWorkbuddyProByUserIDAndOwner(userID int, owner string, offset, limit int) ([]models.Instance, int, error)
	GetProByUserIDAndOwner(userID int, owner string, offset, limit int) ([]models.Instance, int, error)
	GetNorthboundByUserIDAndOwner(userID int, owner string, offset, limit int) ([]models.Instance, int, error)
}

// IEISystemInstanceService provides the email-owner scoped supported-instance
// view after an IEI SSO session has been validated.
type IEISystemInstanceService interface {
	GetSupportedByOwnerEmail(owner string, offset, limit int) ([]models.Instance, int, error)
}

// InstanceQueryService exposes filtered caller-scoped listing and dashboard
// aggregation without widening the lifecycle-oriented InstanceService.
type InstanceQueryService interface {
	GetFilteredByUserID(userID int, filter models.InstanceListFilter, offset, limit int) ([]models.Instance, int, error)
	GetSummaryByUserID(userID int) (*models.InstanceSummary, error)
}

func (s *instanceService) ValidateCreateRequests(userID int, requests []CreateInstanceRequest) error {
	if len(requests) == 0 {
		return nil
	}
	for idx := range requests {
		requests[idx].Name = strings.TrimSpace(requests[idx].Name)
		if requests[idx].Name == "" {
			return fmt.Errorf("instance name is required")
		}
		if !isTeamDistributionCreatableType(requests[idx].Type) {
			return fmt.Errorf("instance type %q is not available in the team distribution", strings.TrimSpace(requests[idx].Type))
		}
		environmentOverrides, err := normalizeEnvironmentOverrides(requests[idx].EnvironmentOverrides)
		if err != nil {
			return err
		}
		if err := validateManagedRuntimeEnvironmentOverrides(requests[idx].Type, environmentOverrides); err != nil {
			return err
		}
		if _, err := marshalEnvironmentOverrides(environmentOverrides); err != nil {
			return err
		}
		if _, ok := normalizeDesktopStreamProfile(requests[idx].DesktopStreamProfile); !ok {
			return fmt.Errorf("invalid desktop stream profile")
		}
		if err := validateWindowsWorkbuddyRequest(requests[idx]); err != nil {
			return err
		}
		if err := validateCreateInstanceDiskGB(requests[idx], resolveCreateInstanceMode(requests[idx])); err != nil {
			return err
		}
	}

	quota, err := s.quotaRepo.GetByUserID(userID)
	if err != nil {
		return fmt.Errorf("failed to get user quota: %w", err)
	}
	if quota == nil {
		return fmt.Errorf("user quota not found")
	}

	currentCount, err := s.instanceRepo.CountByUserID(userID)
	if err != nil {
		return fmt.Errorf("failed to count instances: %w", err)
	}
	if currentCount+len(requests) > quota.MaxInstances {
		return fmt.Errorf("instance limit reached: %d/%d", currentCount+len(requests), quota.MaxInstances)
	}

	existingInstances, err := s.instanceRepo.GetByUserID(userID, 0, 1000)
	if err != nil {
		return fmt.Errorf("failed to list user instances for quota validation: %w", err)
	}

	currentCPU := 0.0
	currentMemory := 0
	currentStorage := 0
	currentGPU := 0
	existingNames := map[string]struct{}{}
	for _, existing := range existingInstances {
		if instanceModeUsesDedicatedResources(modeForExistingInstance(&existing)) {
			currentCPU += existing.CPUCores
			currentMemory += existing.MemoryGB
			currentStorage += existing.DiskGB
			if existing.GPUEnabled {
				currentGPU += existing.GPUCount
			}
		}
		existingNames[strings.TrimSpace(strings.ToLower(existing.Name))] = struct{}{}
	}

	requestedCPU := 0.0
	requestedMemory := 0
	requestedStorage := 0
	requestedGPU := 0
	requestNames := map[string]struct{}{}
	requestedModes := map[string]int{}
	for _, req := range requests {
		normalizedName := strings.TrimSpace(strings.ToLower(req.Name))
		if _, exists := existingNames[normalizedName]; exists {
			return fmt.Errorf("instance name already exists")
		}
		if _, exists := requestNames[normalizedName]; exists {
			return fmt.Errorf("instance name already exists")
		}
		requestNames[normalizedName] = struct{}{}
		if instanceModeUsesDedicatedResources(resolveCreateInstanceMode(req)) {
			requestedCPU += req.CPUCores
			requestedMemory += req.MemoryGB
			requestedStorage += req.DiskGB
			if req.GPUEnabled {
				requestedGPU += req.GPUCount
			}
		}
		requestedModes[resolveCreateInstanceMode(req)]++
	}

	if currentCPU+requestedCPU > quota.MaxCPUCores {
		return fmt.Errorf("CPU cores exceed quota: current %v, requested %v, max %v", currentCPU, requestedCPU, quota.MaxCPUCores)
	}
	if currentMemory+requestedMemory > quota.MaxMemoryGB {
		return fmt.Errorf("memory exceed quota: current %dGB, requested %dGB, max %dGB", currentMemory, requestedMemory, quota.MaxMemoryGB)
	}
	if currentStorage+requestedStorage > quota.MaxStorageGB {
		return fmt.Errorf("storage exceed quota: current %dGB, requested %dGB, max %dGB", currentStorage, requestedStorage, quota.MaxStorageGB)
	}
	if currentGPU+requestedGPU > quota.MaxGPUCount {
		return fmt.Errorf("GPU count exceed quota: current %d, requested %d, max %d", currentGPU, requestedGPU, quota.MaxGPUCount)
	}
	for _, mode := range []string{InstanceModeLite, InstanceModePro} {
		requested := requestedModes[mode]
		if requested == 0 {
			continue
		}
		capacity := loadInstanceModeLimitConfig(mode).Capacity
		if capacity == nil {
			continue
		}
		if *capacity <= 0 {
			return fmt.Errorf("%s instance mode is disabled", mode)
		}
		active, err := s.instanceRepo.CountActiveByMode(context.Background(), mode)
		if err != nil {
			return err
		}
		if active+requested > *capacity {
			return fmt.Errorf("%s instance capacity reached: %d/%d", mode, active+requested, *capacity)
		}
	}

	return nil
}

func isTeamDistributionCreatableType(instanceType string) bool {
	switch strings.ToLower(strings.TrimSpace(instanceType)) {
	case "workbuddy", RuntimeTypeCodex, RuntimeTypeClaudeCode:
		return false
	default:
		return true
	}
}

// CreateInstanceRequest holds data for creating an instance
type CreateInstanceRequest struct {
	Name                    string              `json:"name" validate:"required,min=3,max=50"`
	Owner                   *string             `json:"owner,omitempty"`
	Description             *string             `json:"description,omitempty"`
	Type                    string              `json:"type" validate:"required,oneof=openclaw ubuntu debian centos custom webtop hermes opencode deepseek-harness"`
	RuntimeVariant          string              `json:"runtime_variant,omitempty" validate:"omitempty,oneof=linux windows"`
	Mode                    string              `json:"mode" validate:"omitempty,oneof=lite pro"`
	InstanceMode            string              `json:"instance_mode" validate:"omitempty,oneof=lite pro"`
	RuntimeType             string              `json:"runtime_type" validate:"omitempty,oneof=gateway desktop shell"`
	DesktopStreamProfile    string              `json:"desktop_stream_profile,omitempty" validate:"omitempty,oneof=low standard high"`
	CPUCores                float64             `json:"cpu_cores" validate:"required,min=0.1,max=32"`
	MemoryGB                int                 `json:"memory_gb" validate:"required,min=1,max=128"`
	DiskGB                  int                 `json:"disk_gb" validate:"required,min=5,max=1000"`
	GPUEnabled              bool                `json:"gpu_enabled"`
	GPUCount                int                 `json:"gpu_count" validate:"min=0,max=4"`
	OSType                  string              `json:"os_type" validate:"required"`
	OSVersion               string              `json:"os_version" validate:"required"`
	ImageRegistry           *string             `json:"image_registry,omitempty"`
	ImageTag                *string             `json:"image_tag,omitempty"`
	EnvironmentOverrides    map[string]string   `json:"environment_overrides,omitempty"`
	StorageClass            string              `json:"storage_class"`
	OpenClawConfigPlan      *OpenClawConfigPlan `json:"openclaw_config_plan,omitempty"`
	Team                    *TeamInstanceConfig `json:"-"`
	ProvisioningOperationID string              `json:"-"`
}

type TeamInstanceConfig struct {
	Environment      map[string]string
	SecretName       string
	SharedPVCName    string
	SharedMountPath  string
	ConfigMapName    string
	ConfigMountPath  string
	PersonaConfigKey string
	SharedUID        int64
	SharedGID        int64
	SharedUmask      string
}

type instanceModeLimitConfig struct {
	Capacity     *int
	MaxCPU       *float64
	MaxMemoryGB  *int
	MaxStorageGB *int
	MaxGPUCount  *int
}

// UpdateInstanceRequest holds data for updating an instance
type UpdateInstanceRequest struct {
	Name                 *string `json:"name,omitempty" validate:"omitempty,min=3,max=50"`
	Description          *string `json:"description,omitempty"`
	DesktopStreamProfile *string `json:"desktop_stream_profile,omitempty" validate:"omitempty,oneof=low standard high"`
}

// InstanceStatus holds the status of an instance
type InstanceStatus struct {
	InstanceID          int        `json:"instance_id"`
	Status              string     `json:"status"`
	Availability        string     `json:"availability,omitempty"`
	AgentType           string     `json:"agent_type,omitempty"`
	WorkspaceUsageBytes int64      `json:"workspace_usage_bytes,omitempty"`
	PodName             *string    `json:"pod_name,omitempty"`
	PodNamespace        *string    `json:"pod_namespace,omitempty"`
	PodIP               *string    `json:"pod_ip,omitempty"`
	PodStatus           string     `json:"pod_status,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	StartedAt           *time.Time `json:"started_at,omitempty"`
}

// instanceService implements InstanceService
type instanceService struct {
	instanceRepo          repository.InstanceRepository
	quotaRepo             repository.QuotaRepository
	llmModelRepo          repository.LLMModelRepository
	expandedModelCatalog  ExpandedLLMModelCatalog
	openClawConfigService OpenClawConfigService
	allowPrivilegedPods   bool
	runtimePodRepo        repository.RuntimePodRepository
	bindingRepo           repository.InstanceRuntimeBindingRepository
	agentClient           RuntimeAgentClient
	runtimeUpgradeGuard   RuntimeUpgradeDeletionGuard
	resourceCleanup       func(context.Context, int) error
	workspaceRoot         string
	podService            *k8s.PodService
	deploymentService     *k8s.InstanceDeploymentService
	pvcService            *k8s.PVCService
	serviceService        *k8s.ServiceService
	networkPolicyService  *k8s.NetworkPolicyService
	secretService         *k8s.SecretService
	deletionMu            sync.Mutex
	deletionsInFlight     map[int]struct{}
}

const (
	defaultGatewayTokenAliasTTL = 7 * 24 * time.Hour
	gatewayTokenAliasTTLEnv     = "CLAWMANAGER_GATEWAY_TOKEN_ALIAS_TTL_HOURS"
	workbuddyGoldenPVCEnv       = "CLAWMANAGER_WORKBUDDY_GOLDEN_PVC"
	codexGoldenPVCEnv           = "CLAWMANAGER_CODEX_GOLDEN_PVC"
	workbuddyWindowsPVCSizeGB   = 80
	workbuddyWindowsMinCPUCores = 6
	workbuddyWindowsMinMemoryGB = 12
	workbuddyWindowsNodeLabel   = "clawmanager.io/windows-runtime"
	windowsCodexBootstrapMount  = "/shared/.clawmanager"
	windowsCodexConfigKey       = "config.toml"
	windowsCodexAuthKey         = "auth.json"
)

type gatewayTokenAliasRecorder interface {
	UpsertGatewayTokenAlias(ctx context.Context, instanceID int, accessToken string, expiresAt time.Time) error
}
type InstanceServiceOption func(*instanceService)

// Shared by HTTP, batch, Team and pending-deletion paths.
func WithInstanceResourceCleanup(cleanup func(context.Context, int) error) InstanceServiceOption {
	return func(s *instanceService) { s.resourceCleanup = cleanup }
}

type RuntimeUpgradeDeletionGuard interface {
	ValidateInstanceDeletion(ctx context.Context, instanceID int) error
}

func WithPrivilegedInstancePods(allowed bool) InstanceServiceOption {
	return func(s *instanceService) {
		s.allowPrivilegedPods = allowed
	}
}

func WithExpandedLLMModelCatalog(catalog ExpandedLLMModelCatalog) InstanceServiceOption {
	return func(s *instanceService) {
		s.expandedModelCatalog = catalog
	}
}

func WithV2RuntimeLifecycle(runtimePodRepo repository.RuntimePodRepository, bindingRepo repository.InstanceRuntimeBindingRepository, agentClient RuntimeAgentClient, workspaceRoot string) InstanceServiceOption {
	return func(s *instanceService) {
		s.runtimePodRepo = runtimePodRepo
		s.bindingRepo = bindingRepo
		s.agentClient = agentClient
		if strings.TrimSpace(workspaceRoot) != "" {
			s.workspaceRoot = strings.TrimSpace(workspaceRoot)
		}
	}
}

func WithRuntimeUpgradeDeletionGuard(guard RuntimeUpgradeDeletionGuard) InstanceServiceOption {
	return func(s *instanceService) {
		s.runtimeUpgradeGuard = guard
	}
}

// NewInstanceService creates a new instance service
func NewInstanceService(instanceRepo repository.InstanceRepository, quotaRepo repository.QuotaRepository, llmModelRepo repository.LLMModelRepository, openClawConfigService OpenClawConfigService, options ...InstanceServiceOption) InstanceService {
	service := &instanceService{
		instanceRepo:          instanceRepo,
		quotaRepo:             quotaRepo,
		llmModelRepo:          llmModelRepo,
		openClawConfigService: openClawConfigService,
		workspaceRoot:         "/workspaces",
		podService:            k8s.NewPodService(),
		deploymentService:     k8s.NewInstanceDeploymentService(),
		pvcService:            k8s.NewPVCService(),
		serviceService:        k8s.NewServiceService(),
		networkPolicyService:  k8s.NewNetworkPolicyService(),
		secretService:         k8s.NewSecretService(),
		deletionsInFlight:     make(map[int]struct{}),
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

// Create creates a new instance
func (s *instanceService) Create(userID int, req CreateInstanceRequest) (*models.Instance, error) {
	return s.create(userID, req, true)
}

// CreatePrevalidated creates a new instance after the caller has already
// validated the full batch request with ValidateCreateRequests.
func (s *instanceService) CreatePrevalidated(userID int, req CreateInstanceRequest) (*models.Instance, error) {
	return s.create(userID, req, false)
}

func (s *instanceService) create(userID int, req CreateInstanceRequest, validateQuotaAndName bool) (*models.Instance, error) {
	ctx := context.Background()
	req.Name = strings.TrimSpace(req.Name)
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	if req.Owner != nil {
		owner, err := NormalizeInstanceOwner(*req.Owner)
		if err != nil {
			return nil, err
		}
		req.Owner = &owner
	}
	req.ProvisioningOperationID = strings.TrimSpace(req.ProvisioningOperationID)
	if req.ProvisioningOperationID != "" {
		if repo, ok := s.instanceRepo.(interface {
			GetByProvisioningOperationID(string) (*models.Instance, error)
		}); ok {
			existing, err := repo.GetByProvisioningOperationID(req.ProvisioningOperationID)
			if err != nil {
				return nil, err
			}
			if existing != nil {
				if existing.UserID != userID {
					return nil, fmt.Errorf("provisioning operation belongs to another user")
				}
				return existing, nil
			}
		}
	}
	// Only already-persisted northbound operations carry a provisioning
	// operation ID at this point. Keep those replayable for upgrade
	// compatibility, while rejecting every new direct or prevalidated request
	// for products that the team distribution does not ship.
	if req.ProvisioningOperationID == "" && !isTeamDistributionCreatableType(req.Type) {
		return nil, fmt.Errorf("instance type %q is not available in the team distribution", req.Type)
	}
	req.RuntimeVariant = resolveManagedRuntimeVariantForRequest(req)
	environmentOverrides, err := normalizeEnvironmentOverrides(req.EnvironmentOverrides)
	if err != nil {
		return nil, err
	}
	if err := validateManagedRuntimeEnvironmentOverrides(req.Type, environmentOverrides); err != nil {
		return nil, err
	}
	if profile, ok := normalizeDesktopStreamProfile(req.DesktopStreamProfile); !ok {
		return nil, fmt.Errorf("invalid desktop stream profile")
	} else if profile != "" {
		environmentOverrides = applyDesktopStreamProfileEnv(environmentOverrides, profile)
	}
	environmentOverridesJSON, err := marshalEnvironmentOverrides(environmentOverrides)
	if err != nil {
		return nil, err
	}

	instanceMode := resolveCreateInstanceMode(req)
	if err := validateCreateInstanceDiskGB(req, instanceMode); err != nil {
		return nil, err
	}
	if err := validateWindowsWorkbuddyRequest(req); err != nil {
		return nil, err
	}
	if requiresProInstanceMode(req.Type) && instanceMode != InstanceModePro {
		return nil, fmt.Errorf("%s is only available in pro mode", req.Type)
	}
	modeRuntimeType, _ := RuntimeTypeForInstanceMode(instanceMode)
	if !hasExplicitCreateInstanceMode(req) && normalizeInstanceRuntimeType(req.RuntimeType) == RuntimeBackendShell {
		modeRuntimeType = RuntimeBackendShell
	}

	requestedGPU := 0
	if req.GPUEnabled {
		requestedGPU = req.GPUCount
	}
	if validateQuotaAndName {
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
			if instanceModeUsesDedicatedResources(modeForExistingInstance(&existing)) {
				currentCPU += existing.CPUCores
				currentMemory += existing.MemoryGB
				currentStorage += existing.DiskGB
				if existing.GPUEnabled {
					currentGPU += existing.GPUCount
				}
			}
		}

		nameExists, err := s.instanceRepo.ExistsByUserIDAndName(userID, req.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to validate instance name: %w", err)
		}
		if nameExists {
			return nil, fmt.Errorf("instance name already exists")
		}

		if instanceModeUsesDedicatedResources(instanceMode) {
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
			if currentGPU+requestedGPU > quota.MaxGPUCount {
				return nil, fmt.Errorf("GPU count exceed quota: current %d, requested %d, max %d", currentGPU, requestedGPU, quota.MaxGPUCount)
			}
		}
	}
	if err := s.enforceInstanceModeLimits(ctx, instanceMode, req.CPUCores, req.MemoryGB, req.DiskGB, requestedGPU); err != nil {
		return nil, err
	}
	if runtimeType, isV2 := NormalizeV2RuntimeType(req.Type); isV2 && instanceMode == InstanceModeLite {
		return s.createV2Instance(ctx, userID, req, runtimeType, environmentOverridesJSON)
	}

	runtimeConfig := applyManagedRuntimeVariant(
		buildRuntimeConfig(req.Type, req.OSType, req.OSVersion, req.ImageRegistry, req.ImageTag),
		req.Type,
		req.RuntimeVariant,
		req.ImageRegistry == nil,
	)
	runtimeType := normalizeInstanceRuntimeType(req.RuntimeType)
	if modeRuntimeType != "" {
		runtimeType = modeRuntimeType
	}
	if (req.ImageRegistry == nil || strings.TrimSpace(*req.ImageRegistry) == "") && (req.ImageTag == nil || strings.TrimSpace(*req.ImageTag) == "") {
		selection, ok := runtimeImageOverride(req.Type)
		if modeRuntimeType != "" {
			selection, ok = RuntimeImageForBackend(req.Type, modeRuntimeType)
		}
		if ok {
			image := selection.Image
			req.ImageRegistry = &image
			req.ImageTag = nil
			if modeRuntimeType == "" {
				runtimeType = normalizeInstanceRuntimeType(selection.RuntimeType)
			}
			runtimeConfig = applyManagedRuntimeVariant(
				buildRuntimeConfig(req.Type, req.OSType, req.OSVersion, req.ImageRegistry, req.ImageTag),
				req.Type,
				req.RuntimeVariant,
				false,
			)
		}
	} else if req.ImageRegistry != nil {
		if selection, ok := runtimeImageOverrideForImage(req.Type, *req.ImageRegistry); ok {
			if modeRuntimeType == "" {
				runtimeType = normalizeInstanceRuntimeType(selection.RuntimeType)
			}
		}
	}

	// Check if there are any orphaned resources from previous failed creations
	fmt.Printf("Checking for orphaned resources for user %d before creating new instance...\n", userID)
	s.cleanupOrphanedResourcesByUser(ctx, userID)

	// Create instance record
	now := time.Now()
	instance := &models.Instance{
		UserID:                   userID,
		Owner:                    req.Owner,
		Name:                     req.Name,
		Description:              req.Description,
		Type:                     req.Type,
		RuntimeType:              runtimeType,
		RuntimeVariant:           req.RuntimeVariant,
		InstanceMode:             InstanceModeForRuntimeType(runtimeType),
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
		ProvisioningOperationID:  trimOptionalString(&req.ProvisioningOperationID),
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
	if isWindowsVMInstance(instance) {
		runtimeConfig.Env = windowsWorkbuddyInstanceEnv(runtimeConfig.Env, instance)
	}
	extraEnv, err := buildInstancePodEnv(instance, runtimeConfig.Env, gatewayEnv, agentEnv)
	if err != nil {
		s.instanceRepo.Delete(instance.ID)
		return nil, fmt.Errorf("failed to resolve instance environment: %w", err)
	}
	if req.Team != nil {
		extraEnv = mergeEnvMaps(extraEnv, req.Team.Environment)
	}

	var bootstrapSnapshot *models.OpenClawInjectionSnapshot
	var bootstrapSecretName string
	if snapshot, snapshotErr := s.createRuntimeBootstrapSnapshot(userID, instance, req.OpenClawConfigPlan); snapshotErr != nil {
		s.instanceRepo.Delete(instance.ID)
		return nil, fmt.Errorf("failed to compile runtime bootstrap config: %w", snapshotErr)
	} else if snapshot != nil {
		bootstrapSnapshot = snapshot
		instance.OpenClawConfigSnapshotID = &bootstrapSnapshot.ID
		instance.UpdatedAt = time.Now()
		if err := s.instanceRepo.Update(instance); err != nil {
			s.instanceRepo.Delete(instance.ID)
			return nil, fmt.Errorf("failed to persist runtime snapshot reference: %w", err)
		}

		bootstrapSecretName, err = s.openClawConfigService.EnsureSnapshotSecret(ctx, userID, instance, bootstrapSnapshot.ID)
		if err != nil {
			_ = s.openClawConfigService.MarkSnapshotFailed(bootstrapSnapshot, err)
			s.instanceRepo.Delete(instance.ID)
			return nil, fmt.Errorf("failed to provision runtime bootstrap secret: %w", err)
		}
	}

	// Create PVC
	// If storage class is not specified in request, use empty string
	// PVCService will use the default from K8s client config
	storageClass := req.StorageClass
	var instancePVC *corev1.PersistentVolumeClaim

	if isWindowsVMInstance(instance) {
		goldenPVCEnv := workbuddyGoldenPVCEnv
		goldenRuntimeName := "Windows Workbuddy"
		if isWindowsCodexInstance(instance) {
			goldenPVCEnv = codexGoldenPVCEnv
			goldenRuntimeName = "Windows Codex"
		}
		sourcePVC := strings.TrimSpace(os.Getenv(goldenPVCEnv))
		if sourcePVC == "" {
			err = fmt.Errorf("%s is required for %s instances", goldenPVCEnv, goldenRuntimeName)
		} else if isWindowsCodexInstance(instance) {
			instancePVC, err = s.pvcService.CreatePVCFromSource(ctx, userID, instance.ID, req.DiskGB, storageClass, sourcePVC)
		} else {
			instancePVC, err = s.pvcService.ClaimWorkbuddyPrewarmPVC(ctx, userID, instance.ID, req.DiskGB, storageClass, sourcePVC)
			if err == nil && instancePVC == nil {
				instancePVC, err = s.pvcService.CreatePVCFromSource(ctx, userID, instance.ID, req.DiskGB, storageClass, sourcePVC)
			}
		}
	} else {
		instancePVC, err = s.pvcService.CreatePVC(ctx, userID, instance.ID, req.DiskGB, storageClass)
	}
	if err != nil {
		// Rollback: delete instance record
		if instancePVC != nil && strings.TrimSpace(instancePVC.Name) != "" {
			_ = s.pvcService.DeletePVCByName(ctx, userID, instance.ID, instancePVC.Name)
		}
		if bootstrapSnapshot != nil {
			_ = s.openClawConfigService.MarkSnapshotFailed(bootstrapSnapshot, err)
		}
		s.instanceRepo.Delete(instance.ID)
		return nil, fmt.Errorf("failed to create PVC: %w", err)
	}
	if instancePVC == nil || strings.TrimSpace(instancePVC.Name) == "" {
		s.instanceRepo.Delete(instance.ID)
		return nil, fmt.Errorf("failed to create PVC: empty PVC result")
	}
	pvcName := strings.TrimSpace(instancePVC.Name)
	instance.PVCName = &pvcName
	instance.UpdatedAt = time.Now()
	if err := s.instanceRepo.Update(instance); err != nil {
		_ = s.pvcService.DeletePVCByName(ctx, userID, instance.ID, pvcName)
		s.instanceRepo.Delete(instance.ID)
		return nil, fmt.Errorf("failed to persist instance PVC name: %w", err)
	}
	if err := EnsureInstanceWorkspacePathForServerScan(ctx, s.instanceRepo, instance); err != nil {
		s.deleteInstancePVC(ctx, instance)
		if bootstrapSnapshot != nil {
			_ = s.openClawConfigService.MarkSnapshotFailed(bootstrapSnapshot, err)
		}
		s.instanceRepo.Delete(instance.ID)
		return nil, err
	}

	nodeSelector, err := s.pvcService.NodeSelectorForPVCName(ctx, userID, pvcName, storageClass)
	if err != nil {
		if bootstrapSnapshot != nil {
			_ = s.openClawConfigService.MarkSnapshotFailed(bootstrapSnapshot, err)
		}
		s.deleteInstancePVC(ctx, instance)
		s.instanceRepo.Delete(instance.ID)
		return nil, fmt.Errorf("failed to resolve PVC node selector: %w", err)
	}
	nodeSelector = runtimeNodeSelectorForInstance(instance, nodeSelector)

	// Managed runtime network policy: optional egress lock when enabled.
	if err := s.syncInstanceNetworkPolicy(ctx, userID, instance); err != nil {
		s.deleteInstancePVC(ctx, instance)
		if bootstrapSnapshot != nil {
			_ = s.openClawConfigService.MarkSnapshotFailed(bootstrapSnapshot, err)
		}
		s.instanceRepo.Delete(instance.ID)
		return nil, err
	}

	// Create Pod
	shmSizeGB := popSHMSizeGB(extraEnv, runtimeType, instance.MemoryGB)
	envFromSecretNames := []string{bootstrapSecretName}
	extraPVCMounts := []k8s.PVCMount{}
	configMapFileMounts := []k8s.ConfigMapFileMount{}
	secretDirectoryMounts := []k8s.SecretDirectoryMount{}
	codexBootstrapSecretName := ""
	volumeOwnershipFixes := []k8s.VolumeOwnershipFix{}
	var fsGroup *int64
	if isWindowsCodexInstance(instance) {
		codexSecretName, secretErr := s.ensureWindowsCodexBootstrapSecret(ctx, instance)
		if secretErr != nil {
			s.deleteInstancePVC(ctx, instance)
			s.instanceRepo.Delete(instance.ID)
			return nil, fmt.Errorf("failed to provision Windows Codex bootstrap: %w", secretErr)
		}
		secretDirectoryMounts = append(secretDirectoryMounts, k8s.SecretDirectoryMount{
			Name: "codex-bootstrap", SecretName: codexSecretName, MountPath: windowsCodexBootstrapMount,
		})
		codexBootstrapSecretName = codexSecretName
	}
	if req.Team != nil {
		if strings.TrimSpace(req.Team.SecretName) != "" {
			envFromSecretNames = append(envFromSecretNames, strings.TrimSpace(req.Team.SecretName))
		}
		if strings.TrimSpace(req.Team.SharedPVCName) != "" && strings.TrimSpace(req.Team.SharedMountPath) != "" {
			sharedMountPath := strings.TrimSpace(req.Team.SharedMountPath)
			extraPVCMounts = append(extraPVCMounts, k8s.PVCMount{
				Name:      "team-shared",
				ClaimName: strings.TrimSpace(req.Team.SharedPVCName),
				MountPath: sharedMountPath,
			})
			sharedUID := req.Team.SharedUID
			if sharedUID <= 0 {
				sharedUID = 1000
			}
			sharedGID := req.Team.SharedGID
			if sharedGID <= 0 {
				sharedGID = 1000
			}
			fsGroupValue := sharedGID
			fsGroup = &fsGroupValue
			volumeOwnershipFixes = append(volumeOwnershipFixes, k8s.VolumeOwnershipFix{
				Name:      "team-shared",
				MountPath: sharedMountPath,
				UID:       sharedUID,
				GID:       sharedGID,
			})
		}
		if strings.TrimSpace(req.Team.ConfigMapName) != "" && strings.TrimSpace(req.Team.ConfigMountPath) != "" {
			configMapFileMounts = append(configMapFileMounts, k8s.ConfigMapFileMount{
				Name:          "team-config",
				ConfigMapName: strings.TrimSpace(req.Team.ConfigMapName),
				Key:           "team.json",
				MountPath:     strings.TrimSpace(req.Team.ConfigMountPath),
				ReadOnly:      true,
				AsDirectory:   true,
			})
		}
		if strings.TrimSpace(req.Team.ConfigMapName) != "" && strings.TrimSpace(req.Team.PersonaConfigKey) != "" && strings.EqualFold(instance.Type, "hermes") {
			configMapFileMounts = append(configMapFileMounts, k8s.ConfigMapFileMount{
				Name:          "team-persona",
				ConfigMapName: strings.TrimSpace(req.Team.ConfigMapName),
				Key:           strings.TrimSpace(req.Team.PersonaConfigKey),
				MountPath:     teamHermesSoulMountPath,
				ReadOnly:      true,
			})
		}
	}

	podConfig := k8s.PodConfig{
		InstanceID:            instance.ID,
		InstanceName:          instance.Name,
		UserID:                userID,
		Type:                  instance.Type,
		RuntimeType:           runtimeType,
		CPUCores:              instance.CPUCores,
		MemoryGB:              instance.MemoryGB,
		GPUEnabled:            instance.GPUEnabled,
		GPUCount:              instance.GPUCount,
		Image:                 runtimeConfig.Image,
		PVCName:               pvcName,
		MountPath:             runtimeConfig.MountPath,
		ContainerPort:         runtimeConfig.Port,
		ProbePort:             runtimeProbePortForInstance(instance, runtimeConfig.Port),
		StartupProbeFailures:  runtimeStartupProbeFailuresForInstance(instance),
		TerminationGrace:      runtimeTerminationGraceForInstance(instance),
		ImagePullPolicy:       corev1.PullPolicy(defaultImagePullPolicy()),
		ExtraEnv:              extraEnv,
		EnvFromSecretNames:    envFromSecretNames,
		ExtraPVCMounts:        extraPVCMounts,
		ConfigMapFileMounts:   configMapFileMounts,
		SecretDirectoryMounts: secretDirectoryMounts,
		VolumeInitScripts:     runtimeVolumeInitScripts(instance.Type, runtimeConfig.MountPath),
		FSGroup:               fsGroup,
		NodeSelector:          nodeSelector,
		VolumeOwnershipFixes:  volumeOwnershipFixes,
		SHMSizeGB:             shmSizeGB,
		SecurityMode:          s.securityModeForRuntime(instance),
	}

	var workloadNamespace string
	var workloadName string
	if instanceUsesDesktopRuntime(instance) {
		if s.deploymentService == nil {
			if codexBootstrapSecretName != "" {
				_ = s.secretService.DeleteSecret(ctx, userID, codexBootstrapSecretName)
			}
			s.deleteInstancePVC(ctx, instance)
			if bootstrapSnapshot != nil {
				_ = s.openClawConfigService.MarkSnapshotFailed(bootstrapSnapshot, fmt.Errorf("instance deployment service is not configured"))
			}
			s.instanceRepo.Delete(instance.ID)
			return nil, fmt.Errorf("instance deployment service is not configured")
		}
		deployment, err := s.deploymentService.EnsureDeployment(ctx, podConfig, 1)
		if err != nil {
			if codexBootstrapSecretName != "" {
				_ = s.secretService.DeleteSecret(ctx, userID, codexBootstrapSecretName)
			}
			s.deleteInstancePVC(ctx, instance)
			if bootstrapSnapshot != nil {
				_ = s.openClawConfigService.MarkSnapshotFailed(bootstrapSnapshot, err)
			}
			s.instanceRepo.Delete(instance.ID)
			return nil, fmt.Errorf("failed to create deployment: %w", err)
		}
		workloadNamespace = deployment.Namespace
		workloadName = deployment.Name

		// Create Service for browser desktop access.
		serviceConfig := k8s.ServiceConfig{
			InstanceID:      instance.ID,
			InstanceName:    instance.Name,
			UserID:          userID,
			ContainerPort:   runtimeConfig.Port,
			AdditionalPorts: additionalServicePortsForInstance(instance, runtimeConfig.Port),
		}

		serviceInfo, err := s.serviceService.CreateService(ctx, serviceConfig)
		if err != nil {
			// Rollback: delete Deployment, PVC and instance record.
			_ = s.deploymentService.DeleteDeployment(ctx, userID, instance.ID)
			if codexBootstrapSecretName != "" {
				_ = s.secretService.DeleteSecret(ctx, userID, codexBootstrapSecretName)
			}
			s.deleteInstancePVC(ctx, instance)
			if bootstrapSnapshot != nil {
				_ = s.openClawConfigService.MarkSnapshotFailed(bootstrapSnapshot, err)
			}
			s.instanceRepo.Delete(instance.ID)
			return nil, fmt.Errorf("failed to create service: %w", err)
		}

		fmt.Printf("Instance %d: Service created successfully (ClusterIP: %s)\n", instance.ID, serviceInfo.ClusterIP)
	} else {
		pod, err := s.podService.CreatePod(ctx, podConfig)
		if err != nil {
			// Rollback: delete PVC and instance record.
			if codexBootstrapSecretName != "" {
				_ = s.secretService.DeleteSecret(ctx, userID, codexBootstrapSecretName)
			}
			s.deleteInstancePVC(ctx, instance)
			if bootstrapSnapshot != nil {
				_ = s.openClawConfigService.MarkSnapshotFailed(bootstrapSnapshot, err)
			}
			s.instanceRepo.Delete(instance.ID)
			return nil, fmt.Errorf("failed to create pod: %w", err)
		}
		workloadNamespace = pod.Namespace
		workloadName = pod.Name
		fmt.Printf("Instance %d: Shell runtime selected, skipping desktop service creation\n", instance.ID)
	}

	// Update instance with initial workload info. For Pro instances this is the
	// stable Deployment name; sync later records the active Pod name/IP.
	podNamespace := workloadNamespace
	podName := workloadName
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
			return nil, fmt.Errorf("failed to activate runtime bootstrap snapshot: %w", err)
		}
	}

	// Broadcast initial creating status via WebSocket. Sync service will mark it
	// running only after the pod becomes Ready.
	hydrateInstanceDesktopStreamProfile(instance)
	GetHub().BroadcastInstanceStatus(userID, instance)
	fmt.Printf("Instance %d status broadcast complete\n", instance.ID)

	return instance, nil
}

func (s *instanceService) createV2Instance(ctx context.Context, userID int, req CreateInstanceRequest, runtimeType string, environmentOverridesJSON *string) (*models.Instance, error) {
	if _, err := s.resolveGatewayModelInjection(); err != nil {
		return nil, err
	}

	now := time.Now()
	workspaceRoot := s.runtimeWorkspaceRoot()
	instance := &models.Instance{
		UserID:                   userID,
		Owner:                    req.Owner,
		Name:                     strings.TrimSpace(req.Name),
		Description:              trimOptionalString(req.Description),
		Type:                     runtimeType,
		RuntimeType:              RuntimeBackendGateway,
		InstanceMode:             InstanceModeLite,
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
		StorageClass:             strings.TrimSpace(req.StorageClass),
		MountPath:                workspaceRoot,
		RuntimeGeneration:        1,
		ProvisioningOperationID:  trimOptionalString(&req.ProvisioningOperationID),
		CreatedAt:                now,
		UpdatedAt:                now,
		StartedAt:                &now,
	}

	if err := s.instanceRepo.Create(instance); err != nil {
		return nil, fmt.Errorf("failed to create instance record: %w", err)
	}

	if _, err := s.ensureGatewayToken(instance); err != nil {
		_ = s.instanceRepo.Delete(instance.ID)
		return nil, fmt.Errorf("failed to provision lite gateway token: %w", err)
	}
	if _, err := s.ensureAgentBootstrapToken(instance); err != nil {
		_ = s.instanceRepo.Delete(instance.ID)
		return nil, fmt.Errorf("failed to provision lite agent bootstrap token: %w", err)
	}

	if snapshot, snapshotErr := s.createRuntimeBootstrapSnapshot(userID, instance, req.OpenClawConfigPlan); snapshotErr != nil {
		_ = s.instanceRepo.Delete(instance.ID)
		return nil, fmt.Errorf("failed to compile lite runtime bootstrap config: %w", snapshotErr)
	} else if snapshot != nil {
		instance.OpenClawConfigSnapshotID = &snapshot.ID
		instance.UpdatedAt = time.Now()
		if err := s.instanceRepo.Update(instance); err != nil {
			_ = s.openClawConfigService.MarkSnapshotFailed(snapshot, err)
			_ = s.instanceRepo.Delete(instance.ID)
			return nil, fmt.Errorf("failed to persist lite runtime snapshot reference: %w", err)
		}
		if err := s.openClawConfigService.MarkSnapshotActive(snapshot); err != nil {
			_ = s.instanceRepo.Delete(instance.ID)
			return nil, fmt.Errorf("failed to activate lite runtime bootstrap snapshot: %w", err)
		}
	}

	workspacePath, err := ensureRuntimeWorkspaceDirectories(workspaceRoot, runtimeType, userID, instance.ID)
	if err != nil {
		_ = s.instanceRepo.Delete(instance.ID)
		return nil, fmt.Errorf("failed to create instance workspace: %w", err)
	}
	if err := s.instanceRepo.SetWorkspacePath(ctx, instance.ID, workspacePath); err != nil {
		_ = s.instanceRepo.Delete(instance.ID)
		return nil, fmt.Errorf("failed to persist instance workspace path: %w", err)
	}
	instance.WorkspacePath = &workspacePath

	GetHub().BroadcastInstanceStatus(userID, instance)
	return instance, nil
}

// GetByID gets an instance by ID
func (s *instanceService) GetByID(id int) (*models.Instance, error) {
	instance, err := s.instanceRepo.GetByID(id)
	if err != nil {
		return nil, err
	}
	hydrateInstanceDesktopStreamProfile(instance)
	return instance, nil
}

// GetByUserID gets instances by user ID with pagination
func (s *instanceService) GetByUserID(userID int, offset, limit int) ([]models.Instance, int, error) {
	instances, err := s.instanceRepo.GetByUserID(userID, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	hydrateInstancesDesktopStreamProfile(instances)

	total, err := s.instanceRepo.CountByUserID(userID)
	if err != nil {
		return nil, 0, err
	}

	return instances, total, nil
}

func (s *instanceService) GetFilteredByUserID(userID int, filter models.InstanceListFilter, offset, limit int) ([]models.Instance, int, error) {
	repo, ok := s.instanceRepo.(repository.InstanceQueryRepository)
	if !ok {
		return nil, 0, fmt.Errorf("instance repository does not support filtered queries")
	}
	instances, err := repo.GetFilteredByUserID(userID, filter, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	hydrateInstancesDesktopStreamProfile(instances)
	total, err := repo.CountFilteredByUserID(userID, filter)
	if err != nil {
		return nil, 0, err
	}
	return instances, total, nil
}

func (s *instanceService) GetSummaryByUserID(userID int) (*models.InstanceSummary, error) {
	repo, ok := s.instanceRepo.(repository.InstanceQueryRepository)
	if !ok {
		return nil, fmt.Errorf("instance repository does not support summary queries")
	}
	return repo.SummarizeByUserID(userID)
}

// GetLiteByUserIDAndOwner returns only the caller's Lite instances whose owner
// matches exactly. Owner is normalized before it reaches the repository.
func (s *instanceService) GetLiteByUserIDAndOwner(userID int, owner string, offset, limit int) ([]models.Instance, int, error) {
	normalized, err := NormalizeInstanceOwner(owner)
	if err != nil {
		return nil, 0, err
	}
	repo, ok := s.instanceRepo.(repository.InstanceOwnerRepository)
	if !ok {
		return nil, 0, fmt.Errorf("instance repository does not support owner filtering")
	}
	instances, err := repo.GetLiteByUserIDAndOwner(userID, normalized, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	hydrateInstancesDesktopStreamProfile(instances)
	total, err := repo.CountLiteByUserIDAndOwner(userID, normalized)
	if err != nil {
		return nil, 0, err
	}
	return instances, total, nil
}

func (s *instanceService) GetWorkbuddyProByUserIDAndOwner(userID int, owner string, offset, limit int) ([]models.Instance, int, error) {
	normalized, err := NormalizeInstanceOwner(owner)
	if err != nil {
		return nil, 0, err
	}
	repo, ok := s.instanceRepo.(repository.InstanceOwnerRepository)
	if !ok {
		return nil, 0, fmt.Errorf("instance repository does not support owner filtering")
	}
	instances, err := repo.GetWorkbuddyProByUserIDAndOwner(userID, normalized, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	hydrateInstancesDesktopStreamProfile(instances)
	total, err := repo.CountWorkbuddyProByUserIDAndOwner(userID, normalized)
	if err != nil {
		return nil, 0, err
	}
	return instances, total, nil
}

func (s *instanceService) GetProByUserIDAndOwner(userID int, owner string, offset, limit int) ([]models.Instance, int, error) {
	normalized, err := NormalizeInstanceOwner(owner)
	if err != nil {
		return nil, 0, err
	}
	repo, ok := s.instanceRepo.(repository.InstanceOwnerRepository)
	if !ok {
		return nil, 0, fmt.Errorf("instance repository does not support owner filtering")
	}
	instances, err := repo.GetProByUserIDAndOwner(userID, normalized, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	hydrateInstancesDesktopStreamProfile(instances)
	total, err := repo.CountProByUserIDAndOwner(userID, normalized)
	if err != nil {
		return nil, 0, err
	}
	return instances, total, nil
}

// GetNorthboundByUserIDAndOwner returns the compatibility unified collection
// exposed by /lite-instances: managed Lite runtimes plus every Pro runtime
// supported by the northbound contract.
func (s *instanceService) GetNorthboundByUserIDAndOwner(userID int, owner string, offset, limit int) ([]models.Instance, int, error) {
	normalized, err := NormalizeInstanceOwner(owner)
	if err != nil {
		return nil, 0, err
	}
	repo, ok := s.instanceRepo.(repository.NorthboundInstanceOwnerRepository)
	if !ok {
		return nil, 0, fmt.Errorf("instance repository does not support northbound owner filtering")
	}
	instances, err := repo.GetNorthboundByUserIDAndOwner(userID, normalized, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	hydrateInstancesDesktopStreamProfile(instances)
	total, err := repo.CountNorthboundByUserIDAndOwner(userID, normalized)
	if err != nil {
		return nil, 0, err
	}
	return instances, total, nil
}

func (s *instanceService) GetSupportedByOwnerEmail(owner string, offset, limit int) ([]models.Instance, int, error) {
	normalized := strings.ToLower(strings.TrimSpace(owner))
	if normalized == "" {
		return nil, 0, fmt.Errorf("owner is required")
	}
	repo, ok := s.instanceRepo.(repository.IEISystemInstanceRepository)
	if !ok {
		return nil, 0, fmt.Errorf("instance repository does not support IEI owner filtering")
	}
	instances, err := repo.GetSupportedByOwnerEmail(normalized, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	hydrateInstancesDesktopStreamProfile(instances)
	total, err := repo.CountSupportedByOwnerEmail(normalized)
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
	hydrateInstancesDesktopStreamProfile(instances)

	total, err := s.instanceRepo.CountAll()
	if err != nil {
		return nil, 0, err
	}

	return instances, total, nil
}

func hydrateInstancesDesktopStreamProfile(instances []models.Instance) {
	for idx := range instances {
		hydrateInstanceDesktopStreamProfile(&instances[idx])
	}
}

func hydrateInstanceDesktopStreamProfile(instance *models.Instance) {
	if instance == nil {
		return
	}
	environmentOverrides, err := parseEnvironmentOverridesJSON(instance.EnvironmentOverridesJSON)
	if err != nil {
		return
	}
	instance.DesktopStreamProfile = desktopStreamProfileFromEnv(environmentOverrides)
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
	if err := s.enforceInstanceModeLimits(ctx, modeForExistingInstance(instance), instance.CPUCores, instance.MemoryGB, instance.DiskGB, instance.GPUCount); err != nil {
		return err
	}

	if runtimeType, ok := v2RuntimeTypeForInstance(instance); ok {
		if err := s.prepareV2InstanceStart(ctx, instance); err != nil {
			return err
		}
		return s.startV2Instance(ctx, instance, runtimeType)
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
	runtimeConfig := buildRuntimeConfigForInstance(instance)
	mountPath := persistentVolumeMountPath(instance)
	instance.MountPath = mountPath
	if isWindowsVMInstance(instance) {
		runtimeConfig.Env = windowsWorkbuddyInstanceEnv(runtimeConfig.Env, instance)
	}
	extraEnv, err := buildInstancePodEnv(instance, runtimeConfig.Env, gatewayEnv, agentEnv)
	if err != nil {
		return fmt.Errorf("failed to resolve instance environment: %w", err)
	}
	if err := EnsureInstanceWorkspacePathForServerScan(ctx, s.instanceRepo, instance); err != nil {
		return err
	}

	bootstrapSecretName := ""
	if supportsRuntimeConfigInjectionForInstance(instance) && s.openClawConfigService != nil && instance.OpenClawConfigSnapshotID != nil && *instance.OpenClawConfigSnapshotID > 0 {
		bootstrapSecretName, err = s.openClawConfigService.EnsureSnapshotSecret(ctx, instance.UserID, instance, *instance.OpenClawConfigSnapshotID)
		if err != nil {
			return fmt.Errorf("failed to restore runtime bootstrap secret: %w", err)
		}
	}

	// Remove legacy per-instance network policy before starting pod.
	if err := s.networkPolicyService.DeletePolicy(ctx, instance.UserID, instance.ID, instance.Name); err != nil {
		return fmt.Errorf("failed to delete network policy: %w", err)
	}

	runtimeType := normalizeInstanceRuntimeType(instance.RuntimeType)
	shmSizeGB := popSHMSizeGB(extraEnv, runtimeType, instance.MemoryGB)
	pvcName := instancePVCName(instance, s.pvcService.GetClient())
	nodeSelector, err := s.pvcService.NodeSelectorForPVCName(ctx, instance.UserID, pvcName, instance.StorageClass)
	if err != nil {
		return fmt.Errorf("failed to resolve PVC node selector: %w", err)
	}
	nodeSelector = runtimeNodeSelectorForInstance(instance, nodeSelector)
	secretDirectoryMounts := []k8s.SecretDirectoryMount{}
	if isWindowsCodexInstance(instance) {
		codexSecretName, secretErr := s.ensureWindowsCodexBootstrapSecret(ctx, instance)
		if secretErr != nil {
			return fmt.Errorf("failed to provision Windows Codex bootstrap: %w", secretErr)
		}
		secretDirectoryMounts = append(secretDirectoryMounts, k8s.SecretDirectoryMount{
			Name: "codex-bootstrap", SecretName: codexSecretName, MountPath: windowsCodexBootstrapMount,
		})
	}
	podConfig := k8s.PodConfig{
		InstanceID:            instance.ID,
		InstanceName:          instance.Name,
		UserID:                instance.UserID,
		Type:                  instance.Type,
		RuntimeType:           runtimeType,
		CPUCores:              instance.CPUCores,
		MemoryGB:              instance.MemoryGB,
		GPUEnabled:            instance.GPUEnabled,
		GPUCount:              instance.GPUCount,
		Image:                 runtimeConfig.Image,
		PVCName:               pvcName,
		MountPath:             mountPath,
		ContainerPort:         runtimeConfig.Port,
		ProbePort:             runtimeProbePortForInstance(instance, runtimeConfig.Port),
		StartupProbeFailures:  runtimeStartupProbeFailuresForInstance(instance),
		TerminationGrace:      runtimeTerminationGraceForInstance(instance),
		ImagePullPolicy:       corev1.PullPolicy(defaultImagePullPolicy()),
		ExtraEnv:              extraEnv,
		EnvFromSecretNames:    []string{bootstrapSecretName},
		SecretDirectoryMounts: secretDirectoryMounts,
		VolumeInitScripts:     runtimeVolumeInitScripts(instance.Type, mountPath),
		NodeSelector:          nodeSelector,
		SHMSizeGB:             shmSizeGB,
		SecurityMode:          s.securityModeForRuntime(instance),
	}

	var workloadNamespace string
	var workloadName string
	if instanceUsesDesktopRuntime(instance) {
		if s.deploymentService == nil {
			return fmt.Errorf("instance deployment service is not configured")
		}
		deployment, err := s.deploymentService.EnsureDeployment(ctx, podConfig, 1)
		if err != nil {
			return fmt.Errorf("failed to ensure deployment: %w", err)
		}
		workloadNamespace = deployment.Namespace
		workloadName = deployment.Name

		// Ensure Service exists (create if not exists)
		serviceExists, _ := s.serviceService.ServiceExists(ctx, instance.UserID, instance.ID)
		if !serviceExists {
			serviceConfig := k8s.ServiceConfig{
				InstanceID:      instance.ID,
				InstanceName:    instance.Name,
				UserID:          instance.UserID,
				ContainerPort:   runtimeConfig.Port,
				AdditionalPorts: additionalServicePortsForInstance(instance, runtimeConfig.Port),
			}
			_, err = s.serviceService.CreateService(ctx, serviceConfig)
			if err != nil {
				fmt.Printf("Warning: failed to create service for instance %d: %v\n", instance.ID, err)
				// Don't fail if service creation fails, pod is already running
			}
		}
	} else {
		pod, err := s.podService.CreatePod(ctx, podConfig)
		if err != nil {
			return fmt.Errorf("failed to create pod: %w", err)
		}
		workloadNamespace = pod.Namespace
		workloadName = pod.Name
	}

	// Update instance status
	now := time.Now()
	podNamespace := workloadNamespace
	podName := workloadName
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

func (s *instanceService) securityModeForInstance(instanceType string) k8s.PodSecurityMode {
	if isWindowsVMInstanceType(instanceType) {
		return k8s.PodSecurityPrivileged
	}
	if s != nil && s.allowPrivilegedPods {
		return k8s.PodSecurityPrivileged
	}
	if strings.EqualFold(strings.TrimSpace(instanceType), "openclaw") ||
		strings.EqualFold(strings.TrimSpace(instanceType), "opencode") ||
		strings.EqualFold(strings.TrimSpace(instanceType), "workbuddy") {
		return k8s.PodSecurityChromiumCompat
	}
	return k8s.PodSecurityDefault
}

func (s *instanceService) securityModeForRuntime(instance *models.Instance) k8s.PodSecurityMode {
	if isWindowsVMInstance(instance) {
		return k8s.PodSecurityPrivileged
	}
	if instance == nil {
		return k8s.PodSecurityDefault
	}
	if s != nil && s.allowPrivilegedPods {
		return k8s.PodSecurityPrivileged
	}
	if isLinuxWorkbuddyInstance(instance) {
		return k8s.PodSecurityWorkbuddyLinux
	}
	switch strings.ToLower(strings.TrimSpace(instance.Type)) {
	case "openclaw", "opencode", "workbuddy", RuntimeTypeCodex, RuntimeTypeClaudeCode:
		return k8s.PodSecurityChromiumCompat
	default:
		return k8s.PodSecurityDefault
	}
}
func (s *instanceService) ensureGatewayToken(instance *models.Instance) (string, error) {
	if instance.AccessToken != nil && strings.TrimSpace(*instance.AccessToken) != "" {
		token := strings.TrimSpace(*instance.AccessToken)
		s.refreshGatewayTokenAlias(instance.ID, token)
		return token, nil
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

	s.refreshGatewayTokenAlias(instance.ID, token)
	return token, nil
}

func (s *instanceService) refreshGatewayTokenAlias(instanceID int, token string) {
	recorder, ok := s.instanceRepo.(gatewayTokenAliasRecorder)
	if !ok {
		return
	}
	ttl := gatewayTokenAliasTTL()
	if ttl <= 0 || strings.TrimSpace(token) == "" || instanceID <= 0 {
		return
	}
	if err := recorder.UpsertGatewayTokenAlias(context.Background(), instanceID, token, time.Now().UTC().Add(ttl)); err != nil {
		fmt.Printf("Warning: failed to refresh gateway token alias for instance %d: %v\n", instanceID, err)
	}
}

func gatewayTokenAliasTTL() time.Duration {
	value := optionalIntEnv(gatewayTokenAliasTTLEnv)
	if value == nil {
		return defaultGatewayTokenAliasTTL
	}
	if *value <= 0 {
		return 0
	}
	return time.Duration(*value) * time.Hour
}

func (s *instanceService) ensureWindowsCodexBootstrapSecret(ctx context.Context, instance *models.Instance) (string, error) {
	if !isWindowsCodexInstance(instance) {
		return "", nil
	}
	if s == nil || s.secretService == nil {
		return "", fmt.Errorf("secret service is not configured")
	}

	token, err := s.ensureGatewayToken(instance)
	if err != nil {
		return "", err
	}
	baseURL, ok := defaultGatewayBaseURL()
	if !ok {
		return "", fmt.Errorf("gateway base URL is not configured")
	}
	modelInjection, err := s.resolveGatewayModelInjection()
	if err != nil {
		return "", err
	}
	model := strings.TrimSpace(modelInjection.codingAgentDefaultModel)
	if model == "" {
		model = strings.TrimSpace(modelInjection.defaultModel)
	}
	files, err := renderWindowsCodexBootstrapFiles(baseURL, model, token)
	if err != nil {
		return "", err
	}

	client := k8s.GetClient()
	if client == nil {
		return "", fmt.Errorf("k8s client not initialized")
	}
	secretName := client.GetCodexBootstrapSecretName(instance.ID, instance.Name)
	if err := s.secretService.UpsertSecret(ctx, instance.UserID, secretName, files, map[string]string{
		"app":           "clawreef",
		"instance-id":   fmt.Sprintf("%d", instance.ID),
		"instance-name": instance.Name,
		"user-id":       fmt.Sprintf("%d", instance.UserID),
		"managed-by":    "clawreef",
		"resource-type": "codex-windows-bootstrap",
	}); err != nil {
		return "", err
	}

	s.refreshGatewayTokenAlias(instance.ID, token)
	return secretName, nil
}

func renderWindowsCodexBootstrapFiles(baseURL, model, token string) (map[string]string, error) {
	baseURL = strings.TrimSpace(baseURL)
	model = strings.TrimSpace(model)
	token = strings.TrimSpace(token)
	if baseURL == "" || model == "" || token == "" {
		return nil, fmt.Errorf("base URL, model, and instance token are required")
	}
	// Codex appends /responses to the provider base URL. The ClawManager
	// Responses-compatible endpoint is exposed under /v1/responses.
	baseURL = strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}

	config := fmt.Sprintf(`model_provider = "clawmanager"
model = %s
review_model = %s
windows_wsl_setup_acknowledged = true
sandbox_mode = "danger-full-access"
approval_policy = "never"
web_search = "live"
cli_auth_credentials_store = "file"

[features]
goals = true

[model_providers.clawmanager]
name = "ClawManager"
base_url = %s
wire_api = "responses"
requires_openai_auth = true
`, strconv.Quote(model), strconv.Quote(model), strconv.Quote(baseURL))
	authJSON, err := json.MarshalIndent(map[string]string{"OPENAI_API_KEY": token}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to encode Codex auth file: %w", err)
	}

	return map[string]string{
		windowsCodexConfigKey: config,
		windowsCodexAuthKey:   string(authJSON) + "\n",
	}, nil
}

func (s *instanceService) buildGatewayEnv(instance *models.Instance) (map[string]string, error) {
	if instance == nil || instance.AccessToken == nil || strings.TrimSpace(*instance.AccessToken) == "" {
		return map[string]string{}, nil
	}
	if !supportsManagedRuntimeIntegrationForInstance(instance) {
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
	s.refreshGatewayTokenAlias(instance.ID, token)
	env := map[string]string{
		"CLAWMANAGER_LLM_BASE_URL":          baseURL,
		"CLAWMANAGER_LLM_API_KEY":           token,
		"CLAWMANAGER_LLM_MODEL":             modelInjection.modelsJSON,
		"CLAWMANAGER_LLM_PROVIDER_MODELS":   modelInjection.providerModelsJSON,
		"CLAWMANAGER_LLM_REASONING":         modelInjection.reasoningJSON,
		"CLAWMANAGER_LLM_REASONING_CONTROL": modelInjection.reasoningControlJSON,
		"CLAWMANAGER_LLM_PROVIDER":          "openai-compatible",
		"CLAWMANAGER_INSTANCE_TOKEN":        token,
		"OPENAI_BASE_URL":                   baseURL,
		"OPENAI_API_BASE":                   baseURL,
		"OPENAI_API_KEY":                    token,
		"OPENAI_MODEL":                      modelInjection.defaultModel,
	}
	if (strings.EqualFold(strings.TrimSpace(instance.Type), RuntimeTypeCodex) || strings.EqualFold(strings.TrimSpace(instance.Type), RuntimeTypeClaudeCode)) && modelInjection.codingAgentDefaultModel != "" {
		env["OPENAI_MODEL"] = modelInjection.codingAgentDefaultModel
	}
	if strings.EqualFold(strings.TrimSpace(instance.Type), RuntimeTypeOpenCode) {
		env["OPENCODE_SERVER_PASSWORD"] = token
		env["OPENCODE_SERVER_USERNAME"] = "opencode"
		configContent, err := buildOpenCodeGatewayConfig(modelInjection.providerModelsJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to build opencode gateway config: %w", err)
		}
		// The Lite runtime agent materializes this in the instance's persistent
		// OpenCode config directory before it starts `opencode web`.  Keeping the
		// credentials as env references ensures the generated file does not embed
		// a user-managed provider or a direct external API key.
		env["OPENCODE_CONFIG_CONTENT"] = configContent
		if strings.EqualFold(strings.TrimSpace(instance.InstanceMode), InstanceModeLite) {
			env["CLAWMANAGER_DEFAULT_PROJECT_RELATIVE_PATH"] = OpenCodeDefaultProjectRelativePath
			if instance.WorkspacePath != nil && strings.TrimSpace(*instance.WorkspacePath) != "" {
				env["CLAWMANAGER_DEFAULT_PROJECT_PATH"] = path.Join(strings.TrimSpace(*instance.WorkspacePath), OpenCodeDefaultProjectRelativePath)
			}
		}
	}
	return env, nil
}

func (s *instanceService) BuildGatewayEnv(instance *models.Instance) (map[string]string, error) {
	if instance == nil || !supportsManagedRuntimeIntegrationForInstance(instance) {
		return s.buildGatewayEnv(instance)
	}
	if instance.AccessToken == nil || strings.TrimSpace(*instance.AccessToken) == "" {
		if s == nil || s.instanceRepo == nil {
			return nil, fmt.Errorf("instance repository is not configured")
		}
		if _, err := s.ensureGatewayToken(instance); err != nil {
			return nil, err
		}
	}
	gatewayEnv, err := s.buildGatewayEnv(instance)
	if err != nil {
		return nil, err
	}

	bootstrapEnv, err := s.runtimeBootstrapEnv(instance)
	if err != nil {
		return nil, err
	}

	agentEnv := map[string]string{}
	if _, ok := defaultAgentControlBaseURL(); ok {
		if instance.AgentBootstrapToken == nil || strings.TrimSpace(*instance.AgentBootstrapToken) == "" {
			if s == nil || s.instanceRepo == nil {
				return nil, fmt.Errorf("instance repository is not configured")
			}
			if _, err := s.ensureAgentBootstrapToken(instance); err != nil {
				return nil, err
			}
		}
		agentEnv, err = s.buildAgentEnv(instance)
		if err != nil {
			return nil, err
		}
	}

	merged := mergeEnvMaps(gatewayEnv, bootstrapEnv)
	merged = mergeEnvMaps(merged, agentEnv)
	return buildInstanceGatewayEnv(instance, merged)
}

func (s *instanceService) runtimeBootstrapEnv(instance *models.Instance) (map[string]string, error) {
	if instance == nil || s == nil || s.openClawConfigService == nil || instance.OpenClawConfigSnapshotID == nil || *instance.OpenClawConfigSnapshotID <= 0 {
		return map[string]string{}, nil
	}
	provider, ok := s.openClawConfigService.(interface {
		RuntimeEnvForSnapshot(userID int, instanceType string, snapshotID int) (map[string]string, error)
	})
	if !ok {
		return map[string]string{}, nil
	}
	return provider.RuntimeEnvForSnapshot(instance.UserID, instance.Type, *instance.OpenClawConfigSnapshotID)
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
	if instance == nil || !supportsManagedRuntimeIntegrationForInstance(instance) {
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
		"CLAWMANAGER_AGENT_PERSISTENT_DIR":   managedRuntimePersistentDir(instance),
		"CLAWMANAGER_AGENT_PROTOCOL_VERSION": AgentProtocolVersionV1,
		"CLAWMANAGER_AGENT_RUNTIME_TYPE":     strings.ToLower(strings.TrimSpace(instance.Type)),
	}, nil
}

func supportsManagedRuntimeIntegration(instanceType string) bool {
	switch strings.ToLower(strings.TrimSpace(instanceType)) {
	case "openclaw", "hermes", "opencode", "workbuddy", RuntimeTypeDeepSeekHarness:
		return true
	default:
		return false
	}
}

func (s *instanceService) createRuntimeBootstrapSnapshot(userID int, instance *models.Instance, plan *OpenClawConfigPlan) (*models.OpenClawInjectionSnapshot, error) {
	if !supportsRuntimeConfigInjectionForInstance(instance) || s.openClawConfigService == nil {
		return nil, nil
	}
	if plan != nil && hasOpenClawConfigSelections(*plan) {
		return s.openClawConfigService.CreateSnapshotForInstance(userID, instance, plan)
	}
	if supportsManagedRuntimeIntegrationForInstance(instance) {
		return s.openClawConfigService.CreateDefaultLLMGovernanceSnapshot(userID, instance)
	}
	return nil, nil
}

func (s *instanceService) syncInstanceNetworkPolicy(ctx context.Context, userID int, instance *models.Instance) error {
	if instance == nil {
		return nil
	}
	if isLiteRuntimeInstance(instance) {
		// Lite/gateway-pool instances share runtime pods; per-instance NetworkPolicy does not apply.
		if err := s.networkPolicyService.DeletePolicy(ctx, userID, instance.ID, instance.Name); err != nil {
			return fmt.Errorf("failed to delete network policy: %w", err)
		}
		return nil
	}
	if isInstanceNetworkLockEnabled() && supportsManagedRuntimeIntegrationForInstance(instance) {
		if err := s.networkPolicyService.EnsureDefaultPolicy(ctx, userID, instance.ID, instance.Name); err != nil {
			return fmt.Errorf("failed to ensure network policy: %w", err)
		}
		return nil
	}
	if err := s.networkPolicyService.DeletePolicy(ctx, userID, instance.ID, instance.Name); err != nil {
		return fmt.Errorf("failed to delete network policy: %w", err)
	}
	return nil
}

func supportsRuntimeConfigInjection(instanceType string) bool {
	switch strings.ToLower(strings.TrimSpace(instanceType)) {
	case "openclaw", "hermes", "workbuddy":
		return true
	default:
		return false
	}
}

func managedRuntimePersistentDir(instance *models.Instance) string {
	if instance == nil {
		return "/config"
	}
	if isLiteRuntimeInstance(instance) && instance.WorkspacePath != nil && strings.TrimSpace(*instance.WorkspacePath) != "" {
		workspacePath := strings.TrimSpace(*instance.WorkspacePath)
		if strings.EqualFold(instance.Type, "hermes") {
			return path.Join(workspacePath, "home", ".hermes")
		}
		if strings.EqualFold(instance.Type, RuntimeTypeDeepSeekHarness) {
			return path.Join(workspacePath, "home", ".dsh")
		}
		if strings.EqualFold(instance.Type, "opencode") {
			return path.Join(workspacePath, "home", ".opencode")
		}
		return path.Join(workspacePath, "home", ".openclaw")
	}
	if strings.EqualFold(instance.Type, "hermes") {
		return "/config/.hermes"
	}
	if strings.EqualFold(instance.Type, RuntimeTypeDeepSeekHarness) {
		return "/config/.dsh"
	}
	if strings.EqualFold(instance.Type, "opencode") {
		return "/config/.opencode"
	}
	return persistentVolumeMountPath(instance)
}

func requiresProInstanceMode(instanceType string) bool {
	switch strings.ToLower(strings.TrimSpace(instanceType)) {
	case RuntimeTypeCodex, RuntimeTypeClaudeCode:
		return true
	default:
		return false
	}
}
func persistentVolumeMountPath(instance *models.Instance) string {
	if instance == nil {
		return "/config"
	}
	if !isWindowsVMInstance(instance) && defaultMountPathForInstanceType(instance.Type) == "/config" {
		return "/config"
	}
	if strings.TrimSpace(instance.MountPath) != "" {
		return strings.TrimSpace(instance.MountPath)
	}
	if isWindowsVMInstance(instance) {
		return buildRuntimeConfigForInstance(instance).MountPath
	}
	return defaultMountPathForInstanceType(instance.Type)
}

func runtimeVolumeInitScripts(instanceType, mountPath string) []k8s.VolumeInitScript {
	if strings.TrimSpace(mountPath) != "/config" {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(instanceType), "opencode") {
		return []k8s.VolumeInitScript{
			{
				Name:      "data",
				MountPath: "/config",
				// Some Webtop/Konsole builds do not provide a default profile.  In
				// that state Konsole attempts to execute an empty command and shows
				// a misleading warning before falling back to bash.  Persist an
				// explicit profile for every OpenCode Pro desktop.
				Script: `set -eu
base="${CLAWMANAGER_VOLUME_PATH:-/config}"
mkdir -p "$base/.config" "$base/.local/share/konsole"
cat >"$base/.config/konsolerc" <<'EOF'
[Desktop Entry]
DefaultProfile=ClawManager.profile
EOF
cat >"$base/.local/share/konsole/ClawManager.profile" <<'EOF'
[General]
Command=/bin/bash
Name=ClawManager Shell
Parent=FALLBACK/
EOF
chmod 644 "$base/.config/konsolerc" "$base/.local/share/konsole/ClawManager.profile"
chown -R 911:1001 "$base/.config" "$base/.local" || true`,
			},
		}
	}
	if !strings.EqualFold(strings.TrimSpace(instanceType), "hermes") {
		return nil
	}
	return []k8s.VolumeInitScript{
		{
			Name:      "data",
			MountPath: "/config",
			Script: `set -eu
base="${CLAWMANAGER_VOLUME_PATH:-/config}"
target="$base/.hermes"
if [ ! -d "$target" ]; then
  legacy_found=0
  for name in hermes-agent skills channels.json session.json bootstrap inventory.json; do
    if [ -e "$base/$name" ]; then legacy_found=1; fi
  done
  mkdir -p "$target"
  if [ "$legacy_found" = "1" ]; then
    for entry in "$base"/* "$base"/.[!.]* "$base"/..?*; do
      [ -e "$entry" ] || continue
      name="${entry##*/}"
      case "$name" in .|..|.hermes|Desktop|Downloads|lost+found) continue;; esac
      mv "$entry" "$target"/
    done
  fi
fi
chown -R 1000:1000 "$target" || true`,
		},
	}
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

func (s *instanceService) startV2Instance(ctx context.Context, instance *models.Instance, runtimeType string) error {
	if err := s.ensureV2Workspace(ctx, instance, runtimeType); err != nil {
		return err
	}
	nextGeneration := instance.RuntimeGeneration + 1
	if nextGeneration <= 0 {
		nextGeneration = 1
	}
	if err := s.instanceRepo.UpdateRuntimeState(ctx, instance.ID, "creating", nextGeneration, nil); err != nil {
		return fmt.Errorf("failed to mark v2 instance creating: %w", err)
	}
	instance.Status = "creating"
	instance.RuntimeGeneration = nextGeneration
	instance.RuntimeErrorMessage = nil
	now := time.Now()
	instance.StartedAt = &now
	instance.UpdatedAt = now
	GetHub().BroadcastInstanceStatus(instance.UserID, instance)
	return nil
}

// prepareV2InstanceStart makes an explicit start retry idempotent. A failed or
// older binding belongs to the previous gateway attempt and must not be allowed
// to overwrite the new runtime generation during scheduler reconciliation.
func (s *instanceService) prepareV2InstanceStart(ctx context.Context, instance *models.Instance) error {
	if s.bindingRepo != nil {
		binding, err := s.bindingRepo.GetByInstanceID(ctx, instance.ID)
		if err != nil {
			return fmt.Errorf("failed to get v2 runtime binding before start: %w", err)
		}
		if binding != nil {
			if binding.Generation > instance.RuntimeGeneration {
				return fmt.Errorf("v2 runtime binding generation %d is newer than instance generation %d", binding.Generation, instance.RuntimeGeneration)
			}
			state := strings.ToLower(strings.TrimSpace(binding.State))
			if binding.Generation == instance.RuntimeGeneration && (state == "running" || state == "ready" || state == "healthy") {
				return fmt.Errorf("instance is already running")
			}
			if err := s.cleanupV2GatewayBinding(ctx, instance); err != nil {
				return err
			}
		}
	}
	runtimeType, runtimeTypeOK := NormalizeV2RuntimeType(instance.Type)
	if runtimeTypeOK && runtimeType == RuntimeTypeOpenClaw && instance.WorkspacePath != nil {
		if _, err := quarantineCorruptLegacyOpenClawTaskState(strings.TrimSpace(*instance.WorkspacePath), instance.RuntimeGeneration, instance.RuntimeErrorMessage); err != nil {
			return fmt.Errorf("failed to quarantine corrupt legacy OpenClaw task state: %w", err)
		}
	}
	return nil
}

func (s *instanceService) stopV2Instance(ctx context.Context, instance *models.Instance) error {
	if err := s.instanceRepo.UpdateRuntimeState(ctx, instance.ID, "stopped", instance.RuntimeGeneration, nil); err != nil {
		return fmt.Errorf("failed to mark v2 instance stopped: %w", err)
	}
	now := time.Now()
	instance.Status = "stopped"
	instance.StoppedAt = &now
	instance.PodName = nil
	instance.PodNamespace = nil
	instance.PodIP = nil
	instance.UpdatedAt = now
	GetHub().BroadcastInstanceStatus(instance.UserID, instance)
	return s.cleanupV2GatewayBinding(ctx, instance)
}

func (s *instanceService) deleteV2Instance(ctx context.Context, instance *models.Instance) error {
	if instance.Status != "deleting" {
		now := time.Now()
		instance.Status = "deleting"
		instance.UpdatedAt = now
		if err := s.instanceRepo.Update(instance); err != nil {
			return fmt.Errorf("failed to mark v2 instance as deleting: %w", err)
		}
		GetHub().BroadcastInstanceStatus(instance.UserID, instance)
	}

	if s.resourceCleanup != nil {
		if err := s.resourceCleanup(ctx, instance.ID); err != nil {
			return fmt.Errorf("failed to clean instance components: %w", err)
		}
	}
	cleanupErr := s.cleanupV2GatewayBinding(ctx, instance)
	if cleanupErr != nil {
		return cleanupErr
	}
	if err := s.instanceRepo.Delete(instance.ID); err != nil {
		return fmt.Errorf("failed to delete v2 instance record: %w", err)
	}
	return nil
}

func (s *instanceService) cleanupV2GatewayBinding(ctx context.Context, instance *models.Instance) error {
	if s.bindingRepo == nil {
		return nil
	}
	binding, err := s.bindingRepo.GetByInstanceID(ctx, instance.ID)
	if err != nil {
		return fmt.Errorf("failed to get v2 runtime binding: %w", err)
	}
	if binding == nil {
		return nil
	}

	if s.runtimePodRepo != nil {
		pod, podErr := s.runtimePodRepo.GetByID(ctx, binding.RuntimePodID)
		if podErr != nil {
			return fmt.Errorf("failed to get runtime pod %d for v2 cleanup: %w", binding.RuntimePodID, podErr)
		} else if pod == nil {
			return fmt.Errorf("runtime pod %d is not available for v2 cleanup", binding.RuntimePodID)
		} else if pod != nil && pod.AgentEndpoint != nil && strings.TrimSpace(*pod.AgentEndpoint) != "" && s.agentClient != nil && binding.GatewayID != "" {
			if err := s.agentClient.DeleteGateway(ctx, strings.TrimSpace(*pod.AgentEndpoint), binding.GatewayID); err != nil && !errors.Is(err, ErrRuntimeAgentNotFound) {
				return fmt.Errorf("failed to delete v2 gateway: %w", err)
			}
		}
	}

	if err := s.bindingRepo.DeleteByInstanceIDAndReleaseSlot(ctx, instance.ID, binding.RuntimePodID); err != nil {
		return fmt.Errorf("failed to delete v2 runtime binding and release slot: %w", err)
	}
	return nil
}

func (s *instanceService) ensureV2Workspace(ctx context.Context, instance *models.Instance, runtimeType string) error {
	if instance.WorkspacePath != nil && strings.TrimSpace(*instance.WorkspacePath) != "" {
		return nil
	}
	workspacePath, err := ensureRuntimeWorkspaceDirectories(s.runtimeWorkspaceRoot(), runtimeType, instance.UserID, instance.ID)
	if err != nil {
		return fmt.Errorf("failed to create instance workspace: %w", err)
	}
	if err := s.instanceRepo.SetWorkspacePath(ctx, instance.ID, workspacePath); err != nil {
		return fmt.Errorf("failed to persist instance workspace path: %w", err)
	}
	instance.WorkspacePath = &workspacePath
	return nil
}

func ensureRuntimeWorkspaceDirectories(root, runtimeType string, userID, instanceID int) (string, error) {
	workspacePath := RuntimeWorkspacePathWithRoot(root, runtimeType, userID, instanceID)
	if err := os.MkdirAll(workspacePath, 0750); err != nil {
		return "", err
	}

	// Allow the isolated gateway UID to traverse to its own workspace without
	// granting read/list access to sibling user or instance directories.
	userRoot := path.Dir(workspacePath)
	runtimeRoot := path.Dir(userRoot)
	for _, dir := range []string{runtimeRoot, userRoot} {
		if err := os.Chmod(dir, 0711); err != nil {
			return "", err
		}
	}
	if err := os.Chmod(workspacePath, 0750); err != nil {
		return "", err
	}
	return workspacePath, nil
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

	if _, ok := v2RuntimeTypeForInstance(instance); ok {
		return s.stopV2Instance(ctx, instance)
	}

	if instance.Status != "running" {
		return fmt.Errorf("instance is not running")
	}

	if instanceUsesDesktopRuntime(instance) {
		if s.deploymentService == nil {
			return fmt.Errorf("instance deployment service is not configured")
		}
		if err := s.deploymentService.ScaleDeployment(ctx, instance.UserID, instance.ID, 0); err != nil {
			fmt.Printf("Warning: failed to stop deployment for instance %d, falling back to pod delete: %v\n", instance.ID, err)
			if podErr := s.podService.DeletePod(ctx, instance.UserID, instance.ID); podErr != nil {
				return fmt.Errorf("failed to stop deployment: %w", err)
			}
		}
	} else {
		// Delete shell pod
		if err := s.podService.DeletePod(ctx, instance.UserID, instance.ID); err != nil {
			return fmt.Errorf("failed to delete pod: %w", err)
		}
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
	ctx := context.Background()
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return fmt.Errorf("failed to get instance: %w", err)
	}
	if instance == nil {
		return fmt.Errorf("instance not found")
	}

	_, isV2 := v2RuntimeTypeForInstance(instance)
	waitForDesktopPods := !isV2 && instanceUsesDesktopRuntime(instance)

	if err := s.Stop(instanceID); err != nil {
		return fmt.Errorf("failed to stop instance: %w", err)
	}

	if waitForDesktopPods {
		if s.deploymentService == nil {
			return fmt.Errorf("instance deployment service is not configured")
		}
		if err := s.deploymentService.WaitForDeploymentPodsDeleted(ctx, instance.UserID, instance.ID); err != nil {
			return fmt.Errorf("failed waiting for desktop pods to stop: %w", err)
		}
	}

	if err := s.Start(instanceID); err != nil {
		return fmt.Errorf("failed to start instance: %w", err)
	}

	return nil
}

// InstanceResetService is deliberately separate from InstanceService so that
// consumers must opt in to the destructive factory reset operation.
type InstanceResetService interface {
	Reset(instanceID int) error
}

// InstanceReplacementResetService implements a factory reset by provisioning a
// clean instance first. The source remains untouched until the replacement is
// healthy, which makes reset safe for already-broken runtimes as well.
type InstanceReplacementResetService interface {
	CreateResetReplacement(sourceInstanceID int, operationID string) (*models.Instance, error)
	FinalizeResetReplacement(sourceInstanceID, replacementInstanceID int) (cleanupPending bool, warning string, err error)
	DiscardResetReplacement(replacementInstanceID int) error
}

const (
	factoryResetStagingOwner    = "factory-reset-staging@clawmanager.local"
	factoryResetQuarantineOwner = "admin@clawmanager.local"
)

func factoryResetStagingName(sourceID int, operationID string) string {
	suffix := strings.TrimPrefix(strings.TrimSpace(operationID), "op_")
	if len(suffix) > 12 {
		suffix = suffix[len(suffix)-12:]
	}
	if suffix == "" {
		suffix = strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return fmt.Sprintf("reset-%d-%s", sourceID, suffix)
}

func factoryResetQuarantineName(sourceID int) string {
	return fmt.Sprintf("cleanup-pending-%d", sourceID)
}

func factoryResetString(value string) *string { return &value }

// CreateResetReplacement deliberately uses the normal instance provisioning
// path and current system image selection. It copies only deployment shape;
// workspace contents, runtime-local configuration, tokens and snapshots are
// never copied from the source.
func (s *instanceService) CreateResetReplacement(sourceInstanceID int, operationID string) (*models.Instance, error) {
	source, err := s.GetByID(sourceInstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to load factory-reset source: %w", err)
	}
	if source == nil {
		return nil, fmt.Errorf("factory-reset source instance not found")
	}
	if err := s.ValidateDelete(sourceInstanceID); err != nil {
		return nil, err
	}
	req, err := resetReplacementCreateRequest(source, operationID)
	if err != nil {
		return nil, err
	}

	// Replacement is a one-for-one internal operation. The staging name avoids
	// a duplicate-name conflict; user quota is not charged twice while both
	// records coexist. Global mode capacity and runtime validation still apply.
	replacement, err := s.create(source.UserID, req, false)
	if err != nil {
		return nil, fmt.Errorf("failed to provision clean replacement: %w", err)
	}
	return replacement, nil
}

func resetReplacementCreateRequest(source *models.Instance, operationID string) (CreateInstanceRequest, error) {
	if source == nil {
		return CreateInstanceRequest{}, fmt.Errorf("factory-reset source instance not found")
	}
	mode, ok := NormalizeInstanceMode(source.InstanceMode)
	if !ok || (mode != InstanceModeLite && mode != InstanceModePro) {
		return CreateInstanceRequest{}, fmt.Errorf("factory reset is supported only for managed Lite and Pro instances")
	}
	if mode == InstanceModeLite {
		if _, ok := v2RuntimeTypeForInstance(source); !ok {
			return CreateInstanceRequest{}, fmt.Errorf("factory-reset source is not a managed Lite runtime")
		}
	} else if !instanceUsesDesktopRuntime(source) {
		return CreateInstanceRequest{}, fmt.Errorf("factory-reset source is not a managed Pro desktop runtime")
	}

	stagingOwner := factoryResetStagingOwner
	return CreateInstanceRequest{
		Name:                    factoryResetStagingName(source.ID, operationID),
		Owner:                   &stagingOwner,
		Description:             source.Description,
		Type:                    source.Type,
		RuntimeVariant:          source.RuntimeVariant,
		Mode:                    mode,
		InstanceMode:            mode,
		RuntimeType:             source.RuntimeType,
		DesktopStreamProfile:    source.DesktopStreamProfile,
		CPUCores:                source.CPUCores,
		MemoryGB:                source.MemoryGB,
		DiskGB:                  source.DiskGB,
		GPUEnabled:              source.GPUEnabled,
		GPUCount:                source.GPUCount,
		OSType:                  source.OSType,
		OSVersion:               source.OSVersion,
		StorageClass:            source.StorageClass,
		ProvisioningOperationID: "reset_" + strings.TrimSpace(operationID),
	}, nil
}

// FinalizeResetReplacement atomically hides the source and exposes the healthy
// replacement before attempting destructive cleanup. Cleanup failure therefore
// cannot take the newly delivered instance away from the user.
func (s *instanceService) FinalizeResetReplacement(sourceInstanceID, replacementInstanceID int) (bool, string, error) {
	ctx := context.Background()
	if err := s.ValidateDelete(sourceInstanceID); err != nil {
		return false, "", err
	}
	replacement, err := s.instanceRepo.GetByID(replacementInstanceID)
	if err != nil {
		return false, "", fmt.Errorf("failed to reload factory-reset replacement: %w", err)
	}
	if replacement == nil || strings.ToLower(strings.TrimSpace(replacement.Status)) != "running" {
		return false, "", fmt.Errorf("factory-reset replacement is not running")
	}
	source, err := s.instanceRepo.GetByID(sourceInstanceID)
	if err != nil {
		return false, "", fmt.Errorf("failed to reload factory-reset source: %w", err)
	}
	if source == nil {
		// The old row is deleted only after a successful atomic cutover. If the
		// worker stopped before it could persist operation success, a retry must
		// treat the already-promoted replacement as success instead of failing or
		// trying to discard the user's new instance.
		return false, "", nil
	}
	stagingNamePrefix := fmt.Sprintf("reset-%d-", sourceInstanceID)
	quarantineName := factoryResetQuarantineName(source.ID)
	alreadyPromoted := source.Owner != nil &&
		strings.EqualFold(strings.TrimSpace(*source.Owner), factoryResetQuarantineOwner) &&
		source.Name == quarantineName

	if alreadyPromoted {
		if replacement.Owner == nil || strings.TrimSpace(*replacement.Owner) == "" || strings.HasPrefix(replacement.Name, stagingNamePrefix) {
			return false, "", fmt.Errorf("factory-reset replacement cutover state is inconsistent")
		}
		// The network-policy name for Pro instances includes the original
		// instance name. After cutover that name lives on the replacement.
		source.Name = replacement.Name
	} else {
		if source.Owner == nil || strings.TrimSpace(*source.Owner) == "" {
			return false, "", fmt.Errorf("factory-reset source identity is unavailable")
		}
		originalOwner := strings.TrimSpace(*source.Owner)
		originalName := source.Name

		promoter, ok := s.instanceRepo.(repository.InstanceResetReplacementRepository)
		if !ok {
			return false, "", fmt.Errorf("instance repository does not support replacement reset")
		}
		reason := fmt.Sprintf("factory-reset source replaced by instance %d; cleanup pending", replacement.ID)
		if err := promoter.PromoteResetReplacement(
			ctx,
			source.ID,
			replacement.ID,
			originalOwner,
			originalName,
			factoryResetQuarantineOwner,
			quarantineName,
			reason,
		); err != nil {
			return false, "", err
		}

		replacement.Owner = factoryResetString(originalOwner)
		replacement.Name = originalName
	}

	// Keep source.Name unchanged for resource cleanup: Pro network-policy names
	// include it. Broadcast a copy carrying the quarantined database identity.
	quarantinedSource := *source
	quarantinedSource.Owner = factoryResetString(factoryResetQuarantineOwner)
	quarantinedSource.Name = quarantineName
	quarantinedSource.Status = "stopped"
	GetHub().BroadcastInstanceStatus(source.UserID, &quarantinedSource)
	GetHub().BroadcastInstanceStatus(replacement.UserID, replacement)

	if err := s.cleanupResetSource(ctx, source); err != nil {
		warning := fmt.Sprintf("New instance is ready; old instance %d cleanup requires administrator attention: %v", source.ID, err)
		return true, warning, nil
	}
	return false, "", nil
}

// DiscardResetReplacement is used only before owner cutover. The source is not
// touched even when cleanup of a failed staging instance needs admin follow-up.
func (s *instanceService) DiscardResetReplacement(replacementInstanceID int) error {
	ctx := context.Background()
	if err := s.ValidateDelete(replacementInstanceID); err != nil {
		return err
	}
	replacement, err := s.instanceRepo.GetByID(replacementInstanceID)
	if err != nil || replacement == nil {
		return err
	}
	replacement.Owner = factoryResetString(factoryResetQuarantineOwner)
	replacement.Status = "stopped"
	replacement.UpdatedAt = time.Now().UTC()
	if err := s.instanceRepo.Update(replacement); err != nil {
		return err
	}
	return s.cleanupResetSource(ctx, replacement)
}

func (s *instanceService) cleanupResetSource(ctx context.Context, instance *models.Instance) error {
	if instance == nil {
		return fmt.Errorf("factory-reset cleanup instance is missing")
	}
	if err := s.ValidateDelete(instance.ID); err != nil {
		return err
	}
	if runtimeType, ok := v2RuntimeTypeForInstance(instance); ok {
		if err := s.cleanupV2GatewayBinding(ctx, instance); err != nil {
			return err
		}
		if err := s.eraseV2Workspace(instance, runtimeType); err != nil {
			return err
		}
	} else {
		if s.deploymentService == nil || s.pvcService == nil {
			return fmt.Errorf("Pro cleanup services are not configured")
		}
		pvcName := instancePVCName(instance, s.pvcService.GetClient())
		oldPVName := ""
		if pvc, err := s.pvcService.GetPVCByName(ctx, instance.UserID, pvcName); err == nil && pvc != nil {
			oldPVName = strings.TrimSpace(pvc.Spec.VolumeName)
		} else if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to inspect old Pro workspace: %w", err)
		}
		if err := s.deploymentService.DeleteDeployment(ctx, instance.UserID, instance.ID); err != nil {
			return fmt.Errorf("failed to delete old Pro deployment: %w", err)
		}
		if err := s.deploymentService.WaitForDeploymentPodsDeleted(ctx, instance.UserID, instance.ID); err != nil {
			return fmt.Errorf("failed waiting for old Pro pods: %w", err)
		}
		if s.serviceService != nil {
			if err := s.serviceService.DeleteService(ctx, instance.UserID, instance.ID); err != nil {
				return fmt.Errorf("failed to delete old Pro service: %w", err)
			}
		}
		if s.networkPolicyService != nil {
			if err := s.networkPolicyService.DeletePolicy(ctx, instance.UserID, instance.ID, instance.Name); err != nil {
				return fmt.Errorf("failed to delete old Pro network policy: %w", err)
			}
		}
		if err := s.pvcService.DeletePVCByName(ctx, instance.UserID, instance.ID, pvcName); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to delete old Pro workspace: %w", err)
		}
		if err := s.pvcService.WaitForPVCDeleted(ctx, instance.UserID, pvcName, 0); err != nil {
			return err
		}
		if oldPVName != "" {
			if err := s.pvcService.WaitForPVDeleted(ctx, oldPVName, 0); err != nil {
				return err
			}
		}
		// Remove non-persistent labelled leftovers after the data-bearing
		// resources have been synchronously verified as deleted.
		if cleanup := k8s.NewCleanupService(); cleanup != nil {
			_ = cleanup.DeleteAllInstanceResources(ctx, instance.UserID, instance.ID)
		}
	}
	if err := s.resetInstanceRuntimeData(ctx, instance); err != nil {
		return err
	}
	if err := s.instanceRepo.Delete(instance.ID); err != nil {
		return fmt.Errorf("failed to delete old factory-reset record: %w", err)
	}
	return nil
}

// InstanceLifecycleFailureService lets an asynchronous lifecycle worker turn a
// stale creating state into a recoverable error without deleting any runtime
// resource or persistent data.
type InstanceLifecycleFailureService interface {
	MarkLifecycleFailure(instanceID int) error
}

// Reset preserves the instance identity but permanently replaces its runtime
// workspace and clears instance-local runtime state. Callers must obtain an
// explicit data-loss confirmation before invoking this service.
func (s *instanceService) Reset(instanceID int) error {
	ctx := context.Background()
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return fmt.Errorf("failed to get instance: %w", err)
	}
	if instance == nil {
		return fmt.Errorf("instance not found")
	}
	if err := s.ValidateDelete(instanceID); err != nil {
		return err
	}

	if runtimeType, ok := v2RuntimeTypeForInstance(instance); ok {
		if err := s.claimReset(instanceID); err != nil {
			return err
		}
		if err := s.cleanupV2GatewayBinding(ctx, instance); err != nil {
			s.markResetError(instanceID)
			return fmt.Errorf("failed to stop existing Lite runtime: %w", err)
		}
		if err := s.resetV2Workspace(instance, runtimeType); err != nil {
			s.markResetError(instanceID)
			return err
		}
		if err := s.resetInstanceRuntimeData(ctx, instance); err != nil {
			s.markResetError(instanceID)
			return err
		}
		if err := s.prepareFreshRuntimeCredentials(instance); err != nil {
			s.markResetError(instanceID)
			return err
		}
		if err := s.Start(instanceID); err != nil {
			s.markResetError(instanceID)
			return fmt.Errorf("failed to create fresh Lite runtime: %w", err)
		}
		return nil
	}
	if !instanceUsesDesktopRuntime(instance) {
		return fmt.Errorf("runtime reset is supported only for managed Lite and Pro desktop instances")
	}
	if s.pvcService == nil || s.deploymentService == nil {
		return fmt.Errorf("instance reset services are not configured")
	}

	pvcName := instancePVCName(instance, s.pvcService.GetClient())
	var oldPVCUID types.UID
	oldPVName := ""
	storageClass := strings.TrimSpace(instance.StorageClass)
	pvc, err := s.pvcService.GetPVCByName(ctx, instance.UserID, pvcName)
	if err == nil {
		if pvc.Status.Phase != corev1.ClaimBound || strings.TrimSpace(pvc.Spec.VolumeName) == "" {
			return fmt.Errorf("persistent workspace is not bound; runtime was not changed")
		}
		if err := s.pvcService.ValidatePVCDataDeletionPolicy(ctx, pvc); err != nil {
			return fmt.Errorf("persistent workspace cannot be safely erased: %w", err)
		}
		oldPVCUID = pvc.UID
		oldPVName = strings.TrimSpace(pvc.Spec.VolumeName)
		if pvc.Spec.StorageClassName != nil && strings.TrimSpace(*pvc.Spec.StorageClassName) != "" {
			storageClass = strings.TrimSpace(*pvc.Spec.StorageClassName)
		}
	} else if !apierrors.IsNotFound(err) {
		return fmt.Errorf("persistent workspace is unavailable; runtime was not changed: %w", err)
	}
	if err := s.claimReset(instanceID); err != nil {
		return err
	}

	if err := s.deploymentService.DeleteDeployment(ctx, instance.UserID, instance.ID); err != nil {
		s.markResetError(instanceID)
		return fmt.Errorf("failed to delete runtime deployment: %w", err)
	}
	if err := s.deploymentService.WaitForDeploymentPodsDeleted(ctx, instance.UserID, instance.ID); err != nil {
		s.markResetError(instanceID)
		return fmt.Errorf("failed waiting for runtime pods to stop: %w", err)
	}

	if pvc != nil {
		if err := s.pvcService.DeletePVCByName(ctx, instance.UserID, instance.ID, pvcName); err != nil {
			s.markResetError(instanceID)
			return fmt.Errorf("failed to erase persistent workspace: %w", err)
		}
		if err := s.pvcService.WaitForPVCDeleted(ctx, instance.UserID, pvcName, 0); err != nil {
			s.markResetError(instanceID)
			return err
		}
		if err := s.pvcService.WaitForPVDeleted(ctx, oldPVName, 0); err != nil {
			s.markResetError(instanceID)
			return err
		}
	}
	replacementPVC, err := s.pvcService.CreatePVC(ctx, instance.UserID, instance.ID, instance.DiskGB, storageClass)
	if err != nil {
		s.markResetError(instanceID)
		return fmt.Errorf("failed to create fresh persistent workspace: %w", err)
	}
	if replacementPVC == nil || strings.TrimSpace(replacementPVC.Name) != pvcName {
		s.markResetError(instanceID)
		return fmt.Errorf("fresh persistent workspace name does not match instance record")
	}
	boundPVC, err := s.pvcService.WaitForPVCBoundByName(ctx, instance.UserID, pvcName, 0)
	if err != nil {
		s.markResetError(instanceID)
		return fmt.Errorf("fresh persistent workspace did not become ready: %w", err)
	}
	if oldPVCUID != "" && boundPVC.UID == oldPVCUID {
		s.markResetError(instanceID)
		return fmt.Errorf("persistent workspace was not replaced")
	}
	if err := s.resetInstanceRuntimeData(ctx, instance); err != nil {
		s.markResetError(instanceID)
		return err
	}
	if err := s.prepareFreshRuntimeCredentials(instance); err != nil {
		s.markResetError(instanceID)
		return err
	}
	if err := s.Start(instanceID); err != nil {
		s.markResetError(instanceID)
		return fmt.Errorf("failed to create fresh Pro runtime: %w", err)
	}
	return nil
}

func (s *instanceService) resetV2Workspace(instance *models.Instance, runtimeType string) error {
	if err := s.eraseV2Workspace(instance, runtimeType); err != nil {
		return err
	}
	root := filepath.Clean(s.runtimeWorkspaceRoot())
	expected := filepath.Clean(RuntimeWorkspacePathWithRoot(root, runtimeType, instance.UserID, instance.ID))
	created, err := ensureRuntimeWorkspaceDirectories(root, runtimeType, instance.UserID, instance.ID)
	if err != nil {
		return fmt.Errorf("failed to create fresh Lite workspace: %w", err)
	}
	if filepath.Clean(created) != expected {
		return fmt.Errorf("fresh Lite workspace path does not match instance record")
	}
	return nil
}

func (s *instanceService) eraseV2Workspace(instance *models.Instance, runtimeType string) error {
	if instance == nil || instance.WorkspacePath == nil {
		return fmt.Errorf("Lite workspace path is missing")
	}
	root := filepath.Clean(s.runtimeWorkspaceRoot())
	expected := filepath.Clean(RuntimeWorkspacePathWithRoot(root, runtimeType, instance.UserID, instance.ID))
	actual := filepath.Clean(strings.TrimSpace(*instance.WorkspacePath))
	if actual == "." || actual == root || actual != expected || !isPathWithin(root, actual) {
		return fmt.Errorf("Lite workspace path failed factory-reset safety validation")
	}
	if info, err := os.Lstat(actual); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("Lite workspace path must not be a symbolic link")
		}
		resolvedRoot, rootErr := filepath.EvalSymlinks(root)
		resolvedParent, parentErr := filepath.EvalSymlinks(filepath.Dir(actual))
		if rootErr != nil || parentErr != nil || !isPathWithin(resolvedRoot, resolvedParent) {
			return fmt.Errorf("Lite workspace parent failed factory-reset safety validation")
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to inspect Lite workspace: %w", err)
	}
	if err := os.RemoveAll(actual); err != nil {
		return fmt.Errorf("failed to erase Lite workspace: %w", err)
	}
	return nil
}

func (s *instanceService) resetInstanceRuntimeData(ctx context.Context, instance *models.Instance) error {
	resetter, ok := s.instanceRepo.(repository.InstanceFactoryResetRepository)
	if !ok {
		return fmt.Errorf("instance repository does not support factory reset")
	}
	if err := resetter.ResetInstanceRuntimeData(ctx, instance.ID); err != nil {
		return fmt.Errorf("failed to clear instance runtime data: %w", err)
	}
	instance.Status = "stopped"
	instance.AccessURL = nil
	instance.AccessToken = nil
	instance.AgentBootstrapToken = nil
	instance.PodName = nil
	instance.PodNamespace = nil
	instance.PodIP = nil
	instance.RuntimeErrorMessage = nil
	instance.WorkspaceUsageBytes = 0
	instance.StoppedAt = nil
	return nil
}

func (s *instanceService) prepareFreshRuntimeCredentials(instance *models.Instance) error {
	if _, err := s.ensureGatewayToken(instance); err != nil {
		return fmt.Errorf("failed to create fresh gateway token: %w", err)
	}
	if _, err := s.ensureAgentBootstrapToken(instance); err != nil {
		return fmt.Errorf("failed to create fresh agent bootstrap token: %w", err)
	}
	return nil
}

func (s *instanceService) claimReset(instanceID int) error {
	claimer, ok := s.instanceRepo.(repository.InstanceLifecycleStatusRepository)
	if !ok {
		return fmt.Errorf("instance repository does not support safe lifecycle claims")
	}
	// Move the instance out of the scheduler's desired-running set before any
	// persistent data is erased. The durable northbound operation remains the
	// cross-replica lifecycle lock.
	claimed, err := claimer.ClaimLifecycleStatus(context.Background(), instanceID, []string{"running", "stopped", "error"}, "stopped")
	if err != nil {
		return err
	}
	if !claimed {
		return fmt.Errorf("instance lifecycle operation is already in progress")
	}
	return nil
}

func (s *instanceService) markResetError(instanceID int) {
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil || instance == nil {
		return
	}
	instance.Status = "error"
	instance.UpdatedAt = time.Now()
	_ = s.instanceRepo.Update(instance)
	GetHub().BroadcastInstanceStatus(instance.UserID, instance)
}

func (s *instanceService) MarkLifecycleFailure(instanceID int) error {
	claimer, ok := s.instanceRepo.(repository.InstanceLifecycleStatusRepository)
	if !ok {
		return fmt.Errorf("instance repository does not support safe lifecycle transitions")
	}
	changed, err := claimer.ClaimLifecycleStatus(context.Background(), instanceID, []string{"creating"}, "error")
	if err != nil || !changed {
		return err
	}
	if instance, getErr := s.instanceRepo.GetByID(instanceID); getErr == nil && instance != nil {
		GetHub().BroadcastInstanceStatus(instance.UserID, instance)
	}
	return nil
}

// GetEnvironmentOverrideNames returns sorted configured names without exposing
// stored values.
func (s *instanceService) GetEnvironmentOverrideNames(instanceID int) ([]string, error) {
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}
	if instance == nil {
		return nil, fmt.Errorf("instance not found")
	}

	overrides, err := parseEnvironmentOverridesJSON(instance.EnvironmentOverridesJSON)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(overrides))
	for name := range overrides {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// RestartWithEnvironment merges additions and explicit removals into the
// instance desired state before restarting it. Persisting first keeps the DB as
// the source of truth and allows a failed restart to be retried with the same
// desired configuration.
func (s *instanceService) RestartWithEnvironment(instanceID int, environmentOverrides map[string]string, environmentOverrideRemovals []string) error {
	if len(environmentOverrides) == 0 && len(environmentOverrideRemovals) == 0 {
		return s.Restart(instanceID)
	}

	additions, err := normalizeEnvironmentOverrides(environmentOverrides)
	if err != nil {
		return err
	}
	removals, err := normalizeEnvironmentOverrideRemovals(environmentOverrideRemovals)
	if err != nil {
		return err
	}
	for _, name := range removals {
		if _, exists := additions[name]; exists {
			return fmt.Errorf("%w: environment variable %s cannot be both set and removed", ErrInvalidEnvironmentOverrides, name)
		}
	}

	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return fmt.Errorf("failed to get instance: %w", err)
	}
	if instance == nil {
		return fmt.Errorf("instance not found")
	}

	current, err := parseEnvironmentOverridesJSON(instance.EnvironmentOverridesJSON)
	if err != nil {
		return err
	}
	for _, name := range removals {
		delete(current, name)
	}
	merged := mergeEnvMaps(current, additions)
	if err := validateManagedRuntimeEnvironmentOverrides(instance.Type, merged); err != nil {
		return err
	}
	encoded, err := marshalEnvironmentOverrides(merged)
	if err != nil {
		return err
	}

	instance.EnvironmentOverridesJSON = encoded
	instance.UpdatedAt = time.Now()
	if err := s.instanceRepo.Update(instance); err != nil {
		return fmt.Errorf("failed to persist instance environment overrides: %w", err)
	}

	return s.Restart(instanceID)
}

// Delete starts deleting an instance and all associated K8s resources.
func (s *instanceService) Delete(instanceID int) error {
	instance, err := s.instanceRepo.GetByID(instanceID)
	if err != nil {
		return fmt.Errorf("failed to get instance: %w", err)
	}

	if instance == nil {
		return fmt.Errorf("instance not found")
	}
	if err := s.ValidateDelete(instanceID); err != nil {
		return err
	}

	if _, ok := v2RuntimeTypeForInstance(instance); ok {
		if !s.beginDeletion(instanceID) {
			return fmt.Errorf("instance %d deletion is already in progress", instanceID)
		}
		defer s.finishDeletion(instanceID)
		return s.deleteV2Instance(context.Background(), instance)
	}

	if instance.Status != "deleting" {
		now := time.Now()
		instance.Status = "deleting"
		instance.UpdatedAt = now

		if err := s.instanceRepo.Update(instance); err != nil {
			return fmt.Errorf("failed to mark instance as deleting: %w", err)
		}

		GetHub().BroadcastInstanceStatus(instance.UserID, instance)
	}

	s.scheduleLegacyDeletion(instance.UserID, instance.ID)

	return nil
}

func (s *instanceService) beginDeletion(instanceID int) bool {
	s.deletionMu.Lock()
	defer s.deletionMu.Unlock()
	if s.deletionsInFlight == nil {
		s.deletionsInFlight = make(map[int]struct{})
	}
	if _, exists := s.deletionsInFlight[instanceID]; exists {
		return false
	}
	s.deletionsInFlight[instanceID] = struct{}{}
	return true
}

func (s *instanceService) finishDeletion(instanceID int) {
	s.deletionMu.Lock()
	delete(s.deletionsInFlight, instanceID)
	s.deletionMu.Unlock()
}

func (s *instanceService) scheduleLegacyDeletion(userID, instanceID int) {
	if !s.beginDeletion(instanceID) {
		return
	}
	go func() {
		defer s.finishDeletion(instanceID)
		s.completeDeletion(userID, instanceID)
	}()
}

// ResumePendingDeletions retries only instances already marked deleting. It
// never selects running or stopped instances and therefore cannot create new
// deletion intent. Active data-safe rollouts remain protected by the same
// fail-closed guard used by the HTTP and Team deletion paths.
func (s *instanceService) ResumePendingDeletions(ctx context.Context) (int, error) {
	repo, ok := s.instanceRepo.(repository.PendingInstanceDeletionRepository)
	if !ok {
		return 0, nil
	}
	instances, err := repo.GetByStatus(ctx, "deleting", 0)
	if err != nil {
		return 0, err
	}
	retried := 0
	var failures []string
	for idx := range instances {
		instanceID := instances[idx].ID
		if err := s.Delete(instanceID); err != nil {
			failures = append(failures, fmt.Sprintf("instance %d: %v", instanceID, err))
			continue
		}
		retried++
	}
	if len(failures) > 0 {
		return retried, fmt.Errorf("pending instance deletion retry failed: %s", strings.Join(failures, "; "))
	}
	return retried, nil
}

// RunPendingDeletionReconciler resumes explicit, partially completed deletes
// immediately after leader election and periodically thereafter.
func (s *instanceService) RunPendingDeletionReconciler(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	run := func() {
		retried, err := s.ResumePendingDeletions(ctx)
		if err != nil {
			fmt.Printf("Warning: pending instance deletion reconciliation: %v\n", err)
			return
		}
		if retried > 0 {
			fmt.Printf("Retried %d pending instance deletion(s)\n", retried)
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

// ValidateDelete performs the non-mutating lifecycle checks shared by direct
// instance deletion and Team deletion preflight.
func (s *instanceService) ValidateDelete(instanceID int) error {
	if s.runtimeUpgradeGuard == nil {
		return nil
	}
	if err := s.runtimeUpgradeGuard.ValidateInstanceDeletion(context.Background(), instanceID); err != nil {
		return err
	}
	return nil
}

func (s *instanceService) completeDeletion(userID, instanceID int) {
	ctx := context.Background()

	fmt.Printf("Starting background deletion of instance %d (user %d)\n", instanceID, userID)

	// Use CleanupService to delete ALL resources for this instance (including duplicates)
	cleanupService := k8s.NewCleanupService()
	if err := cleanupService.DeleteAllInstanceResources(ctx, userID, instanceID); err != nil {
		fmt.Printf("Warning: error during resource cleanup for instance %d: %v\n", instanceID, err)
	}

	// Delete instance record from database after background cleanup finishes.
	fmt.Printf("Deleting instance %d from database...\n", instanceID)
	if err := s.instanceRepo.Delete(instanceID); err != nil {
		fmt.Printf("Error: failed to delete instance %d record: %v\n", instanceID, err)
		return
	}

	fmt.Printf("Instance %d deleted successfully\n", instanceID)
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

	deployments, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("instance-id=%s", instanceLabel),
	})
	if err == nil && len(deployments.Items) > 0 {
		for _, deployment := range deployments.Items {
			fmt.Printf("Deleting orphaned Deployment %s\n", deployment.Name)
			propagation := metav1.DeletePropagationForeground
			client.AppsV1().Deployments(namespace).Delete(ctx, deployment.Name, metav1.DeleteOptions{
				PropagationPolicy: &propagation,
			})
		}
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

	deployments, err := client.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "managed-by=clawreef",
	})
	if err != nil {
		fmt.Printf("Warning: failed to list deployments in namespace %s: %v\n", namespace, err)
	} else {
		for _, deployment := range deployments.Items {
			instanceIDStr := deployment.Labels["instance-id"]
			if instanceIDStr == "" {
				continue
			}

			instanceID := 0
			fmt.Sscanf(instanceIDStr, "%d", &instanceID)

			instance, err := s.instanceRepo.GetByID(instanceID)
			if err != nil || instance == nil {
				fmt.Printf("Found orphaned deployment %s (instance-id: %s), deleting...\n", deployment.Name, instanceIDStr)
				propagation := metav1.DeletePropagationForeground
				if err := client.AppsV1().Deployments(namespace).Delete(ctx, deployment.Name, metav1.DeleteOptions{
					PropagationPolicy: &propagation,
				}); err != nil {
					fmt.Printf("Warning: failed to delete orphaned deployment %s: %v\n", deployment.Name, err)
				}
			}
		}
	}

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
	if req.DesktopStreamProfile != nil {
		profile, ok := normalizeDesktopStreamProfile(*req.DesktopStreamProfile)
		if !ok || profile == "" {
			return fmt.Errorf("invalid desktop stream profile")
		}
		environmentOverrides, err := parseEnvironmentOverridesJSON(instance.EnvironmentOverridesJSON)
		if err != nil {
			return err
		}
		environmentOverrides = applyDesktopStreamProfileEnv(environmentOverrides, profile)
		if err := validateManagedRuntimeEnvironmentOverrides(instance.Type, environmentOverrides); err != nil {
			return err
		}
		environmentOverridesJSON, err := marshalEnvironmentOverrides(environmentOverrides)
		if err != nil {
			return err
		}
		instance.EnvironmentOverridesJSON = environmentOverridesJSON
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

	if runtimeType, ok := v2RuntimeTypeForInstance(instance); ok {
		return &InstanceStatus{
			InstanceID:          instance.ID,
			Status:              instance.Status,
			Availability:        s.v2InstanceAvailability(ctx, instance),
			AgentType:           runtimeType,
			WorkspaceUsageBytes: instance.WorkspaceUsageBytes,
			CreatedAt:           instance.CreatedAt,
			StartedAt:           instance.StartedAt,
		}, nil
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

	if _, ok := v2RuntimeTypeForInstance(instance); ok {
		return nil
	}
	if instanceUsesDesktopRuntime(instance) {
		return s.forceSyncDeploymentInstance(ctx, instance)
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

		deploymentExists, deploymentErr := s.podService.DeploymentExists(ctx, instance.UserID, instance.ID)
		if deploymentErr != nil {
			fmt.Printf("Instance %d: failed to check deployment while pod was missing: %v\n", instanceID, deploymentErr)
		}
		if deploymentExists {
			fmt.Printf("Instance %d: Deployment exists but no pod is available yet, updating to creating\n", instanceID)
			if instance.Status != "creating" {
				instance.Status = "creating"
				instance.PodName = nil
				instance.PodNamespace = nil
				instance.PodIP = nil
				instance.UpdatedAt = time.Now()

				if err := s.instanceRepo.Update(instance); err != nil {
					return fmt.Errorf("failed to update instance status: %w", err)
				}

				GetHub().BroadcastInstanceStatus(instance.UserID, instance)
			}
			return nil
		}

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

func (s *instanceService) forceSyncDeploymentInstance(ctx context.Context, instance *models.Instance) error {
	if s.deploymentService == nil {
		return fmt.Errorf("instance deployment service is not configured")
	}
	deployment, err := s.deploymentService.GetDeployment(ctx, instance.UserID, instance.ID)
	if err != nil {
		if instance.Status == "running" || instance.Status == "creating" {
			nextStatus := "stopped"
			if instance.Status == "creating" {
				nextStatus = "error"
			}
			instance.Status = nextStatus
			instance.PodName = nil
			instance.PodNamespace = nil
			instance.PodIP = nil
			instance.UpdatedAt = time.Now()
			if err := s.instanceRepo.Update(instance); err != nil {
				return fmt.Errorf("failed to update instance status: %w", err)
			}
			GetHub().BroadcastInstanceStatus(instance.UserID, instance)
		}
		return nil
	}

	needsUpdate := false
	desiredStatus := mapDeploymentToInstanceStatus(deployment)
	if instance.Status != desiredStatus {
		instance.Status = desiredStatus
		needsUpdate = true
	}
	if pod, podErr := s.deploymentService.GetActivePod(ctx, instance.UserID, instance.ID); podErr == nil && pod != nil {
		if pod.Status.PodIP != "" && (instance.PodIP == nil || *instance.PodIP != pod.Status.PodIP) {
			instance.PodIP = &pod.Status.PodIP
			needsUpdate = true
		}
		if instance.PodName == nil || *instance.PodName != pod.Name {
			instance.PodName = &pod.Name
			needsUpdate = true
		}
		if instance.PodNamespace == nil || *instance.PodNamespace != pod.Namespace {
			instance.PodNamespace = &pod.Namespace
			needsUpdate = true
		}
	}
	if needsUpdate {
		instance.UpdatedAt = time.Now()
		if err := s.instanceRepo.Update(instance); err != nil {
			return fmt.Errorf("failed to update instance: %w", err)
		}
		GetHub().BroadcastInstanceStatus(instance.UserID, instance)
	}
	return nil
}

func additionalServicePorts(instanceType string, primaryPort int32) []int32 {
	if isWindowsVMInstanceType(instanceType) {
		return []int32{3389}
	}
	if primaryPort == 3000 || primaryPort == 8082 {
		return []int32{3000, 8082}
	}

	return nil
}

func additionalServicePortsForInstance(instance *models.Instance, primaryPort int32) []int32 {
	if isWindowsVMInstance(instance) {
		return []int32{3389}
	}
	if primaryPort == 3000 || primaryPort == 8082 {
		return []int32{3000, 8082}
	}
	return nil
}

func isWindowsWorkbuddy(instanceType string) bool {
	return strings.EqualFold(strings.TrimSpace(instanceType), "workbuddy")
}

func isWindowsVMInstanceType(instanceType string) bool {
	return isWindowsWorkbuddy(instanceType) || strings.EqualFold(strings.TrimSpace(instanceType), RuntimeTypeCodex)
}

func instancePVCName(instance *models.Instance, client *k8s.Client) string {
	if instance != nil && instance.PVCName != nil && strings.TrimSpace(*instance.PVCName) != "" {
		return strings.TrimSpace(*instance.PVCName)
	}
	if instance != nil && client != nil {
		return client.GetPVCName(instance.ID)
	}
	return ""
}

func (s *instanceService) deleteInstancePVC(ctx context.Context, instance *models.Instance) {
	if s == nil || s.pvcService == nil || instance == nil {
		return
	}
	_ = s.pvcService.DeletePVCByName(ctx, instance.UserID, instance.ID, instancePVCName(instance, s.pvcService.GetClient()))
}

func validateWindowsWorkbuddyRequest(req CreateInstanceRequest) error {
	isWorkbuddy := strings.EqualFold(strings.TrimSpace(req.Type), "workbuddy")
	isCodex := strings.EqualFold(strings.TrimSpace(req.Type), RuntimeTypeCodex)
	if !isWorkbuddy && !isCodex {
		return nil
	}
	if strings.TrimSpace(req.RuntimeVariant) != "" && normalizeWorkbuddyRuntimeVariant(req.RuntimeVariant) == "" {
		return fmt.Errorf("invalid %s runtime variant", req.Type)
	}
	if resolveManagedRuntimeVariantForRequest(req) != WorkbuddyRuntimeWindows {
		return nil
	}
	runtimeName := "Windows Workbuddy"
	if isCodex {
		runtimeName = "Windows Codex"
	}
	if resolveCreateInstanceMode(req) != InstanceModePro {
		return fmt.Errorf("%s is available only in Pro mode", runtimeName)
	}
	if req.CPUCores < workbuddyWindowsMinCPUCores {
		return fmt.Errorf("%s requires at least %d CPU cores", runtimeName, workbuddyWindowsMinCPUCores)
	}
	if req.MemoryGB < workbuddyWindowsMinMemoryGB {
		return fmt.Errorf("%s requires at least %dGB memory", runtimeName, workbuddyWindowsMinMemoryGB)
	}
	if req.DiskGB != workbuddyWindowsPVCSizeGB {
		return fmt.Errorf("%s requires an %dGB disk to match the golden PVC", runtimeName, workbuddyWindowsPVCSizeGB)
	}
	return nil
}

func windowsWorkbuddyInstanceEnv(base map[string]string, instance *models.Instance) map[string]string {
	env := mergeEnvMaps(base, nil)
	if instance == nil {
		return env
	}
	guestMemoryGB := instance.MemoryGB - 2
	if guestMemoryGB < 4 {
		guestMemoryGB = 4
	}
	cpuCores := int(instance.CPUCores)
	if cpuCores < workbuddyWindowsMinCPUCores {
		cpuCores = workbuddyWindowsMinCPUCores
	}
	env["RAM_SIZE"] = fmt.Sprintf("%dG", guestMemoryGB)
	env["CPU_CORES"] = strconv.Itoa(cpuCores)
	return env
}

func runtimeProbePort(instanceType string, primaryPort int32) int32 {
	if isWindowsVMInstanceType(instanceType) {
		return 3389
	}
	return primaryPort
}

func runtimeProbePortForInstance(instance *models.Instance, primaryPort int32) int32 {
	if isWindowsVMInstance(instance) {
		return 3389
	}
	return primaryPort
}

func runtimeStartupProbeFailures(instanceType string) int32 {
	if isWindowsVMInstanceType(instanceType) {
		return 120
	}
	return 30
}

func runtimeStartupProbeFailuresForInstance(instance *models.Instance) int32 {
	if isWindowsVMInstance(instance) {
		return 120
	}
	return 30
}

func runtimeTerminationGrace(instanceType string) int64 {
	if isWindowsVMInstanceType(instanceType) {
		return 120
	}
	return 0
}

func runtimeTerminationGraceForInstance(instance *models.Instance) int64 {
	if isWindowsVMInstance(instance) {
		return 120
	}
	return 0
}

func runtimeNodeSelector(instanceType string, existing map[string]string) map[string]string {
	selector := mergeEnvMaps(existing, nil)
	if isWindowsVMInstanceType(instanceType) {
		selector[workbuddyWindowsNodeLabel] = "true"
	}
	return selector
}

func runtimeNodeSelectorForInstance(instance *models.Instance, existing map[string]string) map[string]string {
	selector := mergeEnvMaps(existing, nil)
	if isWindowsVMInstance(instance) {
		selector[workbuddyWindowsNodeLabel] = "true"
	}
	return selector
}

func normalizeInstanceRuntimeType(runtimeType string) string {
	switch strings.ToLower(strings.TrimSpace(runtimeType)) {
	case RuntimeBackendGateway:
		return RuntimeBackendGateway
	case "shell":
		return RuntimeBackendShell
	default:
		return RuntimeBackendDesktop
	}
}

func instanceUsesDesktopRuntime(instance *models.Instance) bool {
	if instance == nil {
		return true
	}
	return normalizeInstanceRuntimeType(instance.RuntimeType) == RuntimeBackendDesktop
}

func resolveCreateInstanceMode(req CreateInstanceRequest) string {
	if mode, ok := NormalizeInstanceMode(req.Mode); ok {
		return mode
	}
	if mode, ok := NormalizeInstanceMode(req.InstanceMode); ok {
		return mode
	}
	if strings.TrimSpace(req.RuntimeType) == "" {
		return InstanceModeLite
	}
	return InstanceModeForRuntimeType(normalizeInstanceRuntimeType(req.RuntimeType))
}

func hasExplicitCreateInstanceMode(req CreateInstanceRequest) bool {
	if _, ok := NormalizeInstanceMode(req.Mode); ok {
		return true
	}
	if _, ok := NormalizeInstanceMode(req.InstanceMode); ok {
		return true
	}
	return false
}

func modeForExistingInstance(instance *models.Instance) string {
	if instance == nil {
		return InstanceModeLite
	}
	if mode, ok := NormalizeInstanceMode(instance.InstanceMode); ok {
		return mode
	}
	return InstanceModeForRuntimeType(normalizeInstanceRuntimeType(instance.RuntimeType))
}

func instanceModeUsesDedicatedResources(mode string) bool {
	normalized, ok := NormalizeInstanceMode(mode)
	return ok && normalized == InstanceModePro
}

func validateCreateInstanceDiskGB(req CreateInstanceRequest, mode string) error {
	normalizedMode, ok := NormalizeInstanceMode(mode)
	if !ok {
		return fmt.Errorf("unsupported instance mode %q", mode)
	}
	minimum := DefaultLiteDiskGB
	if normalizedMode == InstanceModePro {
		minimum = MinimumProDiskGB
	}
	if req.DiskGB < minimum {
		return fmt.Errorf("%s disk must be at least %dGB", normalizedMode, minimum)
	}
	if req.DiskGB > 1000 {
		return fmt.Errorf("disk must not exceed 1000GB")
	}
	return nil
}

func (s *instanceService) enforceInstanceModeLimits(ctx context.Context, mode string, cpuCores float64, memoryGB, storageGB, gpuCount int) error {
	normalizedMode, ok := NormalizeInstanceMode(mode)
	if !ok {
		return fmt.Errorf("unsupported instance mode %q", mode)
	}
	limits := loadInstanceModeLimitConfig(normalizedMode)
	if limits.Capacity != nil {
		if *limits.Capacity <= 0 {
			return fmt.Errorf("%s instance mode is disabled", normalizedMode)
		}
		if s == nil || s.instanceRepo == nil {
			return fmt.Errorf("instance repository is not configured")
		}
		count, err := s.instanceRepo.CountActiveByMode(ctx, normalizedMode)
		if err != nil {
			return err
		}
		if count >= *limits.Capacity {
			return fmt.Errorf("%s instance capacity reached: %d/%d", normalizedMode, count, *limits.Capacity)
		}
	}
	if !instanceModeUsesDedicatedResources(normalizedMode) {
		return nil
	}
	if limits.MaxCPU != nil && cpuCores > *limits.MaxCPU {
		return fmt.Errorf("%s CPU cores exceed mode limit: requested %g, max %g", normalizedMode, cpuCores, *limits.MaxCPU)
	}
	if limits.MaxMemoryGB != nil && memoryGB > *limits.MaxMemoryGB {
		return fmt.Errorf("%s memory exceeds mode limit: requested %dGB, max %dGB", normalizedMode, memoryGB, *limits.MaxMemoryGB)
	}
	if limits.MaxStorageGB != nil && storageGB > *limits.MaxStorageGB {
		return fmt.Errorf("%s storage exceeds mode limit: requested %dGB, max %dGB", normalizedMode, storageGB, *limits.MaxStorageGB)
	}
	if limits.MaxGPUCount != nil && gpuCount > *limits.MaxGPUCount {
		return fmt.Errorf("%s GPU count exceeds mode limit: requested %d, max %d", normalizedMode, gpuCount, *limits.MaxGPUCount)
	}
	return nil
}

func loadInstanceModeLimitConfig(mode string) instanceModeLimitConfig {
	prefix := "CLAWMANAGER_" + strings.ToUpper(mode) + "_"
	return instanceModeLimitConfig{
		Capacity:     optionalIntEnv(prefix + "CAPACITY"),
		MaxCPU:       optionalFloatEnv(prefix + "MAX_CPU_CORES"),
		MaxMemoryGB:  optionalIntEnv(prefix + "MAX_MEMORY_GB"),
		MaxStorageGB: optionalIntEnv(prefix + "MAX_STORAGE_GB"),
		MaxGPUCount:  optionalIntEnv(prefix + "MAX_GPU_COUNT"),
	}
}

func optionalIntEnv(key string) *int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return nil
	}
	return &parsed
}

func optionalFloatEnv(key string) *float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil
	}
	return &parsed
}

func (s *instanceService) runtimeWorkspaceRoot() string {
	if s != nil && strings.TrimSpace(s.workspaceRoot) != "" {
		return strings.TrimSpace(s.workspaceRoot)
	}
	return "/workspaces"
}

func v2RuntimeTypeForInstance(instance *models.Instance) (string, bool) {
	if instance == nil {
		return "", false
	}
	runtimeType, ok := NormalizeV2RuntimeType(instance.Type)
	if !ok {
		return "", false
	}
	if strings.EqualFold(strings.TrimSpace(instance.RuntimeType), RuntimeBackendGateway) {
		return runtimeType, true
	}
	if mode, ok := NormalizeInstanceMode(instance.InstanceMode); ok && mode == InstanceModeLite {
		return runtimeType, true
	}
	return "", false
}

func availabilityForStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running":
		return "available"
	case "creating":
		return "starting"
	default:
		return "unavailable"
	}
}

func (s *instanceService) v2InstanceAvailability(ctx context.Context, instance *models.Instance) string {
	base := availabilityForStatus(instance.Status)
	if base != "available" {
		return base
	}
	if s == nil || s.bindingRepo == nil || s.runtimePodRepo == nil {
		return "unavailable"
	}
	binding, err := s.bindingRepo.GetRunningByInstanceID(ctx, instance.ID)
	if err != nil || binding == nil || binding.GatewayPort <= 0 {
		return "unavailable"
	}
	pod, err := s.runtimePodRepo.GetByID(ctx, binding.RuntimePodID)
	if err != nil || pod == nil || pod.PodIP == nil || strings.TrimSpace(*pod.PodIP) == "" {
		return "unavailable"
	}
	return "available"
}

func trimOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// NormalizeInstanceOwner applies the canonical storage and lookup rules for
// northbound owner identifiers.
func NormalizeInstanceOwner(value string) (string, error) {
	owner := strings.TrimSpace(value)
	if owner == "" {
		return "", fmt.Errorf("owner is required")
	}
	if !utf8.ValidString(owner) || len(owner) > 128 {
		return "", fmt.Errorf("owner must contain at most 128 UTF-8 bytes")
	}
	for _, character := range owner {
		if unicode.IsControl(character) {
			return "", fmt.Errorf("owner must not contain control characters")
		}
	}
	return owner, nil
}
